//go:build wailsdesktop

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	assetserver "github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

const desktopLinuxWebviewGPUEnv = "MISTERMORPH_DESKTOP_WEBVIEW_GPU_POLICY"

func main() {
	if handled, err := maybeRunDesktopConsoleServe(os.Args[1:]); handled {
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "desktop console host failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	host := NewDesktopHost(DesktopHostConfig{
		ConsoleBasePath: defaultConsoleBasePath,
		ConfigPath:      extractConfigPathFromArgs(os.Args[1:]),
	})
	if err := host.Start(context.Background()); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to start desktop host: %v\n", err)
		os.Exit(1)
	}

	app := NewApp(host)
	err := wails.Run(&options.App{
		Title:     "MisterMorph",
		Width:     1360,
		Height:    860,
		MinWidth:  1000,
		MinHeight: 680,
		Linux: &linux.Options{
			WebviewGpuPolicy: resolveLinuxWebviewGpuPolicy(),
			ProgramName:      "MisterMorph",
		},
		AssetServer: &assetserver.Options{
			Handler: host.ProxyHandler(),
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []interface{}{app},
	})
	host.Stop()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "desktop app exited with error: %v\n", err)
		os.Exit(1)
	}
}

func resolveLinuxWebviewGpuPolicy() linux.WebviewGpuPolicy {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(desktopLinuxWebviewGPUEnv))) {
	case "", "ondemand", "on_demand", "on-demand":
		return linux.WebviewGpuPolicyOnDemand
	case "always":
		return linux.WebviewGpuPolicyAlways
	case "never", "off", "disabled":
		return linux.WebviewGpuPolicyNever
	default:
		return linux.WebviewGpuPolicyOnDemand
	}
}
