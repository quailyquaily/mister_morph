package consolecmd

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	awarenessloop "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
)

type consoleNotificationHub struct {
	mu   sync.RWMutex
	subs map[chan awarenessloop.CronNotification]struct{}
}

func newConsoleNotificationHub() *consoleNotificationHub {
	return &consoleNotificationHub{subs: map[chan awarenessloop.CronNotification]struct{}{}}
}

func (h *consoleNotificationHub) Subscribe() (<-chan awarenessloop.CronNotification, func()) {
	if h == nil {
		ch := make(chan awarenessloop.CronNotification)
		close(ch)
		return ch, func() {}
	}
	ch := make(chan awarenessloop.CronNotification, 8)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

func (h *consoleNotificationHub) Publish(notification awarenessloop.CronNotification) {
	if h == nil || strings.TrimSpace(notification.ID) == "" {
		return
	}
	notification.ID = strings.TrimSpace(notification.ID)
	notification.Title = strings.TrimSpace(notification.Title)
	notification.Body = strings.TrimSpace(notification.Body)
	h.mu.RLock()
	subs := make([]chan awarenessloop.CronNotification, 0, len(h.subs))
	for sub := range h.subs {
		subs = append(subs, sub)
	}
	h.mu.RUnlock()
	for _, sub := range subs {
		select {
		case sub <- notification:
		default:
			select {
			case <-sub:
			default:
			}
			select {
			case sub <- notification:
			default:
			}
		}
	}
}

func (s *server) handleNotificationWebSocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s == nil || s.localRuntime == nil || s.localRuntime.notificationHub == nil {
		writeError(w, http.StatusServiceUnavailable, "notifications are unavailable")
		return
	}

	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	if ticket == "" {
		writeError(w, http.StatusBadRequest, "missing ticket")
		return
	}
	if _, ok := s.streamTickets.Validate(ticket); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	s.streamTickets.Delete(ticket)
	events, unsubscribe := s.localRuntime.notificationHub.Subscribe()
	defer unsubscribe()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return sameOriginRequest(r)
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	})

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	pingTicker := time.NewTicker(25 * time.Second)
	defer pingTicker.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(event); err != nil {
				return
			}
		case <-pingTicker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-readDone:
			return
		case <-r.Context().Done():
			return
		}
	}
}
