//go:build wailsdesktop

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	desktopMainWindowStateFile = "main-window.json"
	maxDesktopMainWindowWidth  = 3840
	maxDesktopMainWindowHeight = 2400
)

type desktopMainWindowState struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type desktopWindowArea struct {
	X      int
	Y      int
	Width  int
	Height int
}

func desktopMainWindowStatePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mistermorph", "desktop", desktopMainWindowStateFile), nil
}

func loadDesktopMainWindowState(path string) (desktopMainWindowState, bool) {
	if strings.TrimSpace(path) == "" {
		return desktopMainWindowState{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 4096 {
		return desktopMainWindowState{}, false
	}
	var state desktopMainWindowState
	if err := json.Unmarshal(raw, &state); err != nil {
		return desktopMainWindowState{}, false
	}
	return normalizeDesktopMainWindowState(state)
}

func saveDesktopMainWindowState(path string, state desktopMainWindowState) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty desktop window state path")
	}
	normalized, ok := normalizeDesktopMainWindowState(state)
	if !ok {
		return fmt.Errorf("invalid desktop window state")
	}
	raw, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func saveDesktopMainWindowStateFromWindow(path string, window desktopWindowLifecycleTarget) error {
	width, height := window.Size()
	x, y := window.Position()
	return saveDesktopMainWindowState(path, desktopMainWindowState{
		X:      x,
		Y:      y,
		Width:  width,
		Height: height,
	})
}

func normalizeDesktopMainWindowState(state desktopMainWindowState) (desktopMainWindowState, bool) {
	if state.Width <= 0 || state.Height <= 0 {
		return desktopMainWindowState{}, false
	}
	state.Width = clampDesktopWindowDimension(
		state.Width,
		defaultDesktopMainWindowWidth,
		defaultDesktopMainWindowMinWidth,
		maxDesktopMainWindowWidth,
	)
	state.Height = clampDesktopWindowDimension(
		state.Height,
		defaultDesktopMainWindowHeight,
		defaultDesktopMainWindowMinHeight,
		maxDesktopMainWindowHeight,
	)
	return state, true
}

func constrainDesktopMainWindowStateToArea(state desktopMainWindowState, area desktopWindowArea) (desktopMainWindowState, bool) {
	if area.Width <= 0 || area.Height <= 0 || state.Width <= 0 || state.Height <= 0 {
		return desktopMainWindowState{}, false
	}
	if state.Width > area.Width {
		state.Width = area.Width
	}
	if state.Height > area.Height {
		state.Height = area.Height
	}

	maxX := area.X + area.Width - state.Width
	if state.X < area.X {
		state.X = area.X
	}
	if state.X > maxX {
		state.X = maxX
	}

	maxY := area.Y + area.Height - state.Height
	if state.Y < area.Y {
		state.Y = area.Y
	}
	if state.Y > maxY {
		state.Y = maxY
	}
	return state, true
}
