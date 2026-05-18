//go:build wailsdesktop

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/quailyquaily/mistermorph/internal/updatecheck"
)

var desktopVersion = "dev"

type DesktopUpdateCheckResult = updatecheck.Result

func runDesktopCheckUpdateCommand(ctx context.Context, cfg desktopRuntimeConfig, out io.Writer) error {
	result, err := updatecheck.Check(ctx, newDesktopUpdateCheckOptions(cfg.AutoUpdate.Enabled))
	if err != nil {
		return err
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func startDesktopAutoUpdateCheck(ctx context.Context, cfg desktopAutoUpdateConfig, logWriter io.Writer) {
	if !cfg.Enabled {
		return
	}

	go func() {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()

		result, err := updatecheck.Check(checkCtx, newDesktopUpdateCheckOptions(true))
		if err != nil {
			logDesktopUpdateEvent(logWriter, "auto_update_failed error=%q", compactDesktopLogValue(err.Error(), 1000))
			return
		}
		logDesktopUpdateEvent(
			logWriter,
			"auto_update_checked status=%q current=%q latest=%q platform=%q downloaded=%t",
			result.Status,
			result.CurrentVersion,
			result.LatestVersion,
			result.Platform,
			result.Downloaded,
		)
	}()
}

func logDesktopUpdateEvent(logWriter io.Writer, format string, args ...any) {
	line := "desktop_update " + fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintln(os.Stderr, line)
	if logWriter != nil {
		_, _ = fmt.Fprintln(logWriter, line)
	}
}

func newDesktopUpdateCheckOptions(autoDownload bool) updatecheck.Options {
	opts := updatecheck.Options{
		AutoDownload:   autoDownload,
		CurrentVersion: desktopVersion,
		UserAgent:      desktopBackendHTTPUserAgent,
	}
	if autoDownload {
		cacheDir, err := desktopUpdateCacheDir()
		if err != nil {
			return opts
		}
		opts.CacheDir = cacheDir
	}
	return opts
}

func desktopUpdateCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mistermorph", "desktop", "updates"), nil
}
