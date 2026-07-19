package consolecmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	awarenessloop "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
)

func TestConsoleNotificationHubPublishesToSubscribers(t *testing.T) {
	hub := newConsoleNotificationHub()
	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	hub.Publish(awarenessloop.CronNotification{
		ID:    "run-1",
		Title: "Daily review",
		Body:  "Review complete.",
	})

	select {
	case got := <-events:
		if got.ID != "run-1" || got.Title != "Daily review" || got.Body != "Review complete." {
			t.Fatalf("unexpected notification frame: %#v", got)
		}
		raw, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal notification: %v", err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatalf("unmarshal notification fields: %v", err)
		}
		if len(fields) != 3 {
			t.Fatalf("notification fields = %#v, want id, title, body", fields)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notification")
	}
}

func TestConsoleNotificationHubDropsEventsWithoutSubscribers(t *testing.T) {
	hub := newConsoleNotificationHub()
	hub.Publish(awarenessloop.CronNotification{ID: "run-1", Title: "Task"})

	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	select {
	case got := <-events:
		t.Fatalf("received stale notification: %#v", got)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestNotificationWebSocketDeliversCronEvent(t *testing.T) {
	hub := newConsoleNotificationHub()
	srv := &server{
		streamTickets: newSessionStore(""),
		localRuntime:  &consoleLocalRuntime{notificationHub: hub},
	}
	ticket, _, err := srv.streamTickets.Create(time.Minute)
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleNotificationWebSocket))
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "?ticket=" + ticket
	header := http.Header{"Origin": []string{httpServer.URL}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial notification websocket: %v", err)
	}
	defer conn.Close()

	hub.Publish(awarenessloop.CronNotification{
		ID:    "run-ws",
		Title: "WebSocket task",
		Body:  "Delivered.",
	})
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var got map[string]any
	if err := conn.ReadJSON(&got); err != nil {
		t.Fatalf("read notification websocket: %v", err)
	}
	if len(got) != 3 || got["id"] != "run-ws" || got["title"] != "WebSocket task" || got["body"] != "Delivered." {
		t.Fatalf("unexpected notification frame: %#v", got)
	}
}
