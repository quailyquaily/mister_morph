package slack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestConsumeSlackSocketStopsIdleReadOnContextCancellation(t *testing.T) {
	pongReceived := make(chan struct{})
	releaseServer := make(chan struct{})
	serverDone := make(chan struct{})
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn.SetPongHandler(func(string) error {
			close(pongReceived)
			return nil
		})
		if err := conn.WriteControl(websocket.PingMessage, []byte("idle-read-check"), time.Now().Add(time.Second)); err != nil {
			_ = conn.Close()
			close(serverDone)
			return
		}
		go func() {
			<-releaseServer
			_ = conn.Close()
		}()
		_, _, _ = conn.ReadMessage()
		close(serverDone)
	}))
	defer server.Close()
	defer func() {
		close(releaseServer)
		<-serverDone
	}()

	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- consumeSlackSocket(ctx, conn, nil)
	}()
	select {
	case <-pongReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for websocket idle read")
	}

	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("consumeSlackSocket() error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		_ = conn.Close()
		<-result
		t.Fatal("consumeSlackSocket() did not stop after context cancellation")
	}
}
