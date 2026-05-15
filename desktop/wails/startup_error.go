//go:build wailsdesktop

package main

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type desktopStartupErrorKind string

const (
	desktopStartupErrorUnknown        desktopStartupErrorKind = "unknown"
	desktopStartupErrorBackendMissing desktopStartupErrorKind = "backend_missing"
	desktopStartupErrorBackendExited  desktopStartupErrorKind = "backend_exited"
	desktopStartupErrorBackendTimeout desktopStartupErrorKind = "backend_timeout"
	desktopStartupErrorBackendStart   desktopStartupErrorKind = "backend_start_failed"
	desktopStartupErrorBackendURL     desktopStartupErrorKind = "backend_url_failed"
	desktopStartupErrorLoopback       desktopStartupErrorKind = "loopback_failed"
)

type desktopStartupErrorInfo struct {
	Kind    desktopStartupErrorKind `json:"kind"`
	Title   string                  `json:"title"`
	Message string                  `json:"message"`
	Detail  string                  `json:"detail"`
	LogPath string                  `json:"log_path,omitempty"`
}

func newDesktopStartupErrorWindow(app *application.App, info desktopStartupErrorInfo) *application.WebviewWindow {
	return app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "MisterMorph Startup Error",
		Width:     760,
		Height:    520,
		MinWidth:  620,
		MinHeight: 420,
		HTML:      buildDesktopStartupErrorHTML(info),
		JS:        desktopRuntimeJavaScript,
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: resolveLinuxWebviewGPUPolicy(),
		},
	})
}

func classifyDesktopStartupError(err error) desktopStartupErrorInfo {
	detail := strings.TrimSpace(errorString(err))
	info := desktopStartupErrorInfo{
		Kind:    desktopStartupErrorUnknown,
		Title:   "MisterMorph could not start",
		Message: "The desktop app could not start its local backend.",
		Detail:  detail,
	}
	if err == nil {
		return info
	}
	switch {
	case strings.Contains(detail, "cannot find runnable mistermorph backend binary"):
		info.Kind = desktopStartupErrorBackendMissing
		info.Message = "The bundled backend binary was not found or is not executable."
	case strings.Contains(detail, "exited before readiness"):
		info.Kind = desktopStartupErrorBackendExited
		info.Message = "The backend process exited before it became ready."
	case strings.Contains(detail, "did not become ready before timeout"):
		info.Kind = desktopStartupErrorBackendTimeout
		info.Message = "The backend process started but did not become ready in time."
	case strings.Contains(detail, "start desktop console host"):
		info.Kind = desktopStartupErrorBackendStart
		info.Message = "The desktop app could not launch the backend process."
	case strings.Contains(detail, "build console url"):
		info.Kind = desktopStartupErrorBackendURL
		info.Message = "The desktop app could not build the local backend URL."
	case strings.Contains(detail, "listen") || strings.Contains(detail, "loopback"):
		info.Kind = desktopStartupErrorLoopback
		info.Message = "The desktop app could not reserve a local loopback port."
	}
	return info
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func buildDesktopStartupErrorHTML(info desktopStartupErrorInfo) string {
	diagnostics := startupDiagnosticsJSON(info)
	hasLogPath := strings.TrimSpace(info.LogPath) != ""
	return fmt.Sprintf(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    :root {
      color-scheme: light;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #f8f9f7;
      color: #1f251f;
    }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
    }
    main {
      width: min(620px, calc(100vw - 48px));
    }
    h1 {
      margin: 0 0 12px;
      font-size: 20px;
      line-height: 1.2;
      font-weight: 650;
    }
    p {
      margin: 0 0 18px;
      color: #566052;
      font-size: 14px;
      line-height: 1.55;
    }
    pre {
      max-height: 190px;
      margin: 0 0 18px;
      padding: 12px;
      overflow: auto;
      border: 1px solid rgba(31, 37, 31, 0.14);
      background: #fff;
      color: #2c332b;
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: 12px;
      line-height: 1.45;
      white-space: pre-wrap;
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
    }
    button {
      min-height: 34px;
      padding: 0 12px;
      border: 1px solid rgba(31, 37, 31, 0.16);
      border-radius: 6px;
      background: #fff;
      color: #1f251f;
      font: inherit;
      font-size: 13px;
    }
    button.primary {
      border-color: #2f6b45;
      background: #2f6b45;
      color: #fff;
    }
    button:disabled {
      cursor: default;
      opacity: 0.52;
    }
  </style>
</head>
<body>
  <main>
    <h1>%s</h1>
    <p>%s</p>
    <pre>%s</pre>
    <div class="actions">
      <button class="primary" id="restart">Restart</button>
      <button id="open-log">Open Log</button>
      <button id="copy">Copy Diagnostics</button>
      <button id="quit">Quit</button>
    </div>
  </main>
  <script>
    const diagnostics = %s;
    const hasLogPath = %t;
    function bindingName(method) {
      const bindings = window.__MISTERMORPH_DESKTOP_BINDINGS__;
      if (bindings && typeof bindings === "object" && typeof bindings[method] === "string" && bindings[method].trim()) {
        return bindings[method].trim();
      }
      return "main.App." + method;
    }
    async function call(method) {
      if (window.wails && window.wails.Call && window.wails.Call.ByName) {
        await window.wails.Call.ByName(bindingName(method));
      }
    }
    const logButton = document.getElementById("open-log");
    if (hasLogPath) {
      logButton.addEventListener("click", () => call("OpenDesktopLog"));
    } else {
      logButton.disabled = true;
      logButton.textContent = "Log Unavailable";
    }
    document.getElementById("restart").addEventListener("click", () => call("RestartApp"));
    document.getElementById("quit").addEventListener("click", () => call("QuitApp"));
    document.getElementById("copy").addEventListener("click", async (event) => {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(diagnostics);
      } else {
        const textArea = document.createElement("textarea");
        textArea.value = diagnostics;
        textArea.style.position = "fixed";
        textArea.style.opacity = "0";
        document.body.appendChild(textArea);
        textArea.focus();
        textArea.select();
        document.execCommand("copy");
        textArea.remove();
      }
      event.currentTarget.textContent = "Copied";
    });
  </script>
</body>
</html>`,
		html.EscapeString(info.Title),
		html.EscapeString(info.Message),
		html.EscapeString(info.Detail),
		diagnostics,
		hasLogPath,
	)
}

func startupDiagnosticsJSON(info desktopStartupErrorInfo) string {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		data = []byte("failed to encode startup diagnostics")
	}
	encoded, err := json.Marshal(string(data))
	if err != nil {
		return `""`
	}
	return string(encoded)
}
