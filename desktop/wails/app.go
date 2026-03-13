//go:build wailsdesktop

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx        context.Context
	host       *DesktopHost
	restartMu  sync.Mutex
	restarting bool
}

func NewApp(host *DesktopHost) *App {
	return &App{host: host}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(ctx context.Context) {
	if a.host != nil {
		a.host.Stop()
	}
}

// RestartApp relaunches the current executable and quits the current process.
func (a *App) RestartApp() error {
	a.restartMu.Lock()
	if a.restarting {
		a.restartMu.Unlock()
		return nil
	}
	a.restarting = true
	a.restartMu.Unlock()

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if wd, wdErr := os.Getwd(); wdErr == nil {
		cmd.Dir = wd
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start new app process: %w", err)
	}

	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
	return nil
}
