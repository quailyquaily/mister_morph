package mixinapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestBlazeRunListsPendingHandlesMessageAndAcknowledges(t *testing.T) {
	var gotListPending atomic.Bool
	var gotAck atomic.Bool
	ackDone := make(chan struct{})
	upgrader := websocket.Upgrader{Subprotocols: []string{blazeSubprotocol}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Error("missing authorization")
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		var request BlazeEnvelope
		if err := readBlazeEnvelope(conn, &request); err != nil {
			t.Error(err)
			return
		}
		if request.Action != blazeActionListPending {
			t.Errorf("first action = %q", request.Action)
			return
		}
		gotListPending.Store(true)
		if err := writeBlazeEnvelope(conn, BlazeEnvelope{
			ID:     "server-event",
			Action: blazeActionCreateMessage,
			Data: mustJSONRaw(t, MessageView{
				ConversationID: "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34",
				UserID:         "773e5e77-4107-45c2-b648-8fc722ed77f5",
				MessageID:      "a4ec1e53-f147-439a-82cd-2e5e4a95a152",
				Category:       MessageCategoryPlainText,
				DataBase64:     base64.RawURLEncoding.EncodeToString([]byte("hello")),
				Status:         "SENT",
			}),
		}); err != nil {
			t.Error(err)
			return
		}
		if err := readBlazeEnvelope(conn, &request); err != nil {
			t.Error(err)
			return
		}
		if request.Action != blazeActionAcknowledge || request.Params["message_id"] != "a4ec1e53-f147-439a-82cd-2e5e4a95a152" || request.Params["status"] != "READ" {
			t.Errorf("ack = %#v", request)
			return
		}
		gotAck.Store(true)
		close(ackDone)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client, err := NewBlazeClient(testCredentials(), BlazeOptions{
		URL:        "ws" + strings.TrimPrefix(server.URL, "http"),
		MinBackoff: time.Millisecond,
		MaxBackoff: 2 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Run(ctx, func(_ context.Context, msg MessageView) error {
			if msg.MessageID != "a4ec1e53-f147-439a-82cd-2e5e4a95a152" {
				t.Errorf("message = %#v", msg)
			}
			return nil
		})
	}()
	select {
	case <-ackDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for acknowledgement")
	}
	cancel()
	err = <-errCh
	if err != nil && err != context.Canceled {
		t.Fatalf("Run() error = %v", err)
	}
	if !gotListPending.Load() || !gotAck.Load() {
		t.Fatalf("list_pending=%v ack=%v", gotListPending.Load(), gotAck.Load())
	}
}

var errTestHandler = errors.New("test handler failed")

func mustJSONRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestBlazeRunDoesNotAcknowledgeFailedHandler(t *testing.T) {
	var gotAck atomic.Bool
	upgrader := websocket.Upgrader{Subprotocols: []string{blazeSubprotocol}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var request BlazeEnvelope
		_ = readBlazeEnvelope(conn, &request)
		_ = writeBlazeEnvelope(conn, BlazeEnvelope{
			ID: "event", Action: blazeActionCreateMessage,
			Data: mustJSONRaw(t, MessageView{MessageID: "a4ec1e53-f147-439a-82cd-2e5e4a95a152", Category: MessageCategoryPlainText}),
		})
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
		if readBlazeEnvelope(conn, &request) == nil && request.Action == blazeActionAcknowledge {
			gotAck.Store(true)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	client, err := NewBlazeClient(testCredentials(), BlazeOptions{
		URL:        "ws" + strings.TrimPrefix(server.URL, "http"),
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Run(ctx, func(context.Context, MessageView) error { return errTestHandler })
	if gotAck.Load() {
		t.Fatal("failed handler message was acknowledged")
	}
}

func TestBlazeRunStopsOnUnauthorizedHandshake(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := NewBlazeClient(testCredentials(), BlazeOptions{URL: "ws" + strings.TrimPrefix(server.URL, "http")})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Run(context.Background(), func(context.Context, MessageView) error { return nil })
	if !IsUnauthorized(err) {
		t.Fatalf("error = %v", err)
	}
}

func TestBlazeRunReconnectsAfterDisconnect(t *testing.T) {
	var connections atomic.Int64
	upgrader := websocket.Upgrader{Subprotocols: []string{blazeSubprotocol}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		current := connections.Add(1)
		var request BlazeEnvelope
		_ = readBlazeEnvelope(conn, &request)
		if current == 1 {
			return
		}
		_ = writeBlazeEnvelope(conn, BlazeEnvelope{
			ID: "event", Action: blazeActionCreateMessage,
			Data: mustJSONRaw(t, MessageView{MessageID: "a4ec1e53-f147-439a-82cd-2e5e4a95a152", Category: MessageCategoryPlainText}),
		})
		_ = readBlazeEnvelope(conn, &request)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client, err := NewBlazeClient(testCredentials(), BlazeOptions{
		URL:        "ws" + strings.TrimPrefix(server.URL, "http"),
		MinBackoff: time.Millisecond,
		MaxBackoff: 2 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Run(ctx, func(context.Context, MessageView) error {
		cancel()
		return nil
	})
	if err != nil && err != context.Canceled {
		t.Fatalf("Run() error = %v", err)
	}
	if connections.Load() < 2 {
		t.Fatalf("connections = %d", connections.Load())
	}
}

func TestBlazeRunReconnectsAfterListPendingError(t *testing.T) {
	var connections atomic.Int64
	upgrader := websocket.Upgrader{Subprotocols: []string{blazeSubprotocol}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		current := connections.Add(1)
		var request BlazeEnvelope
		if err := readBlazeEnvelope(conn, &request); err != nil {
			return
		}
		if current == 1 {
			_ = writeBlazeEnvelope(conn, BlazeEnvelope{
				ID: request.ID, Error: &BlazeError{Status: http.StatusInternalServerError, Code: http.StatusInternalServerError, Description: "temporary"},
			})
			_, _, _ = conn.NextReader()
			return
		}
		_ = writeBlazeEnvelope(conn, BlazeEnvelope{
			ID: "event", Action: blazeActionCreateMessage,
			Data: mustJSONRaw(t, MessageView{MessageID: "a4ec1e53-f147-439a-82cd-2e5e4a95a152", Category: MessageCategoryPlainText}),
		})
		_ = readBlazeEnvelope(conn, &request)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client, err := NewBlazeClient(testCredentials(), BlazeOptions{
		URL: "ws" + strings.TrimPrefix(server.URL, "http"), MinBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Run(ctx, func(context.Context, MessageView) error {
		cancel()
		return nil
	})
	if err != nil && err != context.Canceled {
		t.Fatalf("Run() error = %v", err)
	}
	if connections.Load() < 2 {
		t.Fatalf("connections = %d, want reconnect after LIST_PENDING error", connections.Load())
	}
}

func TestBlazeRunSendsHeartbeat(t *testing.T) {
	var pings atomic.Int64
	pingSeen := make(chan struct{}, 1)
	upgrader := websocket.Upgrader{Subprotocols: []string{blazeSubprotocol}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var request BlazeEnvelope
		if err := readBlazeEnvelope(conn, &request); err != nil {
			return
		}
		conn.SetPingHandler(func(data string) error {
			pings.Add(1)
			select {
			case pingSeen <- struct{}{}:
			default:
			}
			return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(50*time.Millisecond))
		})
		for {
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client, err := NewBlazeClient(testCredentials(), BlazeOptions{URL: "ws" + strings.TrimPrefix(server.URL, "http")})
	if err != nil {
		t.Fatal(err)
	}
	client.pingPeriod = 10 * time.Millisecond
	client.pongWait = 40 * time.Millisecond
	errCh := make(chan error, 1)
	go func() { errCh <- client.Run(ctx, func(context.Context, MessageView) error { return nil }) }()
	select {
	case <-pingSeen:
	case <-time.After(250 * time.Millisecond):
		cancel()
		t.Fatal("timed out waiting for Blaze heartbeat")
	}
	cancel()
	err = <-errCh
	if err != nil && err != context.Canceled {
		t.Fatalf("Run() error = %v", err)
	}
	if pings.Load() == 0 {
		t.Fatal("Blaze client sent no ping")
	}
}
