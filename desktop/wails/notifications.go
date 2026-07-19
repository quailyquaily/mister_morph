//go:build wailsdesktop

package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

type desktopNotificationBackend interface {
	ServiceStartup(context.Context, application.ServiceOptions) error
	ServiceShutdown() error
	RequestNotificationAuthorization() (bool, error)
	SendNotification(notifications.NotificationOptions) error
}

type desktopNotificationManager struct {
	backend desktopNotificationBackend

	mu         sync.RWMutex
	started    bool
	startupErr error
}

func newDesktopNotificationManager(backend desktopNotificationBackend) *desktopNotificationManager {
	if backend == nil {
		backend = notifications.New()
	}
	return &desktopNotificationManager{backend: backend}
}

func (m *desktopNotificationManager) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if m == nil || m.backend == nil {
		return nil
	}
	err := m.backend.ServiceStartup(ctx, options)
	m.mu.Lock()
	m.started = err == nil
	m.startupErr = err
	m.mu.Unlock()
	return nil
}

func (m *desktopNotificationManager) ServiceShutdown() error {
	if err := m.available(); err != nil {
		return nil
	}
	return m.backend.ServiceShutdown()
}

func (m *desktopNotificationManager) RequestNotificationAuthorization() (bool, error) {
	if err := m.available(); err != nil {
		return false, err
	}
	return m.backend.RequestNotificationAuthorization()
}

func (m *desktopNotificationManager) SendNotification(options notifications.NotificationOptions) error {
	if err := m.available(); err != nil {
		return err
	}
	return m.backend.SendNotification(options)
}

func (m *desktopNotificationManager) available() error {
	if m == nil || m.backend == nil {
		return fmt.Errorf("desktop notification service is unavailable")
	}
	m.mu.RLock()
	started := m.started
	startupErr := m.startupErr
	m.mu.RUnlock()
	if startupErr != nil {
		return fmt.Errorf("desktop notification service is unavailable: %w", startupErr)
	}
	if !started {
		return fmt.Errorf("desktop notification service has not started")
	}
	return nil
}
