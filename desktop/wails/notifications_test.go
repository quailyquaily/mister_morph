//go:build wailsdesktop

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

type stubDesktopNotificationBackend struct {
	startupErr error
	sent       notifications.NotificationOptions
}

func (s *stubDesktopNotificationBackend) ServiceStartup(context.Context, application.ServiceOptions) error {
	return s.startupErr
}

func (s *stubDesktopNotificationBackend) ServiceShutdown() error { return nil }
func (s *stubDesktopNotificationBackend) RequestNotificationAuthorization() (bool, error) {
	return true, nil
}
func (s *stubDesktopNotificationBackend) SendNotification(options notifications.NotificationOptions) error {
	s.sent = options
	return nil
}

func TestDesktopNotificationManagerDegradesWhenStartupFails(t *testing.T) {
	manager := newDesktopNotificationManager(&stubDesktopNotificationBackend{startupErr: errors.New("no notification service")})
	if err := manager.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatalf("ServiceStartup() error = %v, want nil", err)
	}
	if _, err := manager.RequestNotificationAuthorization(); err == nil || !strings.Contains(err.Error(), "no notification service") {
		t.Fatalf("RequestNotificationAuthorization() error = %v", err)
	}
}

func TestDesktopNotificationManagerSendsAfterStartup(t *testing.T) {
	backend := &stubDesktopNotificationBackend{}
	manager := newDesktopNotificationManager(backend)
	if err := manager.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatalf("ServiceStartup() error = %v", err)
	}
	if err := manager.SendNotification(notifications.NotificationOptions{ID: "run-1", Title: "Task", Body: "Done"}); err != nil {
		t.Fatalf("SendNotification() error = %v", err)
	}
	if backend.sent.ID != "run-1" || backend.sent.Title != "Task" || backend.sent.Body != "Done" {
		t.Fatalf("unexpected sent notification: %#v", backend.sent)
	}
}
