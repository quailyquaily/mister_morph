package consolecmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// consoleLogRouter is a global log router that can dynamically route logs to different tasks.
// It wraps the default stderr output and optionally forwards logs to the console stream hub.
type consoleLogRouter struct {
	baseHandler slog.Handler
	level       slog.Level
	
	mu       sync.RWMutex
	hub      *consoleStreamHub
	taskID   string
	enabled  bool
}

var (
	// globalLogRouter is the singleton instance used by all console logging.
	globalLogRouter *consoleLogRouter
	// routerInitOnce ensures the router is initialized only once.
	routerInitOnce sync.Once
)

// initGlobalLogRouter initializes the global log router with the given configuration.
// This should be called once during runtime initialization.
func initGlobalLogRouter(level slog.Level, format string) *consoleLogRouter {
	routerInitOnce.Do(func() {
		opts := &slog.HandlerOptions{
			Level:     level,
			AddSource: false,
		}
		
		var base slog.Handler
		switch strings.ToLower(strings.TrimSpace(format)) {
		case "json":
			base = slog.NewJSONHandler(os.Stderr, opts)
		default:
			base = slog.NewTextHandler(os.Stderr, opts)
		}
		
		globalLogRouter = &consoleLogRouter{
			baseHandler: base,
			level:       level,
			enabled:     true,
		}
	})
	return globalLogRouter
}

// SetHub sets the stream hub for log forwarding.
func (r *consoleLogRouter) SetHub(hub *consoleStreamHub) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hub = hub
}

// SetTaskID sets the current task ID for log routing.
// When set, logs will be forwarded to the stream hub for this task.
func (r *consoleLogRouter) SetTaskID(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.taskID = strings.TrimSpace(taskID)
}

// ClearTaskID clears the current task ID, stopping log forwarding to the stream hub.
func (r *consoleLogRouter) ClearTaskID() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.taskID = ""
}

// Enabled implements slog.Handler.
func (r *consoleLogRouter) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= r.level
}

// Handle implements slog.Handler.
func (r *consoleLogRouter) Handle(ctx context.Context, record slog.Record) error {
	// Always write to stderr via base handler
	if err := r.baseHandler.Handle(ctx, record); err != nil {
		return err
	}
	
	// Forward to stream hub if conditions are met
	r.mu.RLock()
	hub := r.hub
	taskID := r.taskID
	enabled := r.enabled
	r.mu.RUnlock()
	
	if !enabled || hub == nil || taskID == "" {
		return nil
	}
	
	// Only forward info level and above to avoid UI flooding
	if record.Level < slog.LevelInfo {
		return nil
	}
	
	// Format and send to frontend
	msg := formatLogForFrontend(record)
	if msg != "" {
		hub.PublishSnapshot(taskID, msg)
	}
	
	return nil
}

// WithAttrs implements slog.Handler.
func (r *consoleLogRouter) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &consoleLogRouter{
		baseHandler: r.baseHandler.WithAttrs(attrs),
		level:       r.level,
		hub:         r.hub,
		taskID:      r.taskID,
		enabled:     r.enabled,
	}
}

// WithGroup implements slog.Handler.
func (r *consoleLogRouter) WithGroup(name string) slog.Handler {
	return &consoleLogRouter{
		baseHandler: r.baseHandler.WithGroup(name),
		level:       r.level,
		hub:         r.hub,
		taskID:      r.taskID,
		enabled:     r.enabled,
	}
}

// formatLogForFrontend formats a slog record into a user-friendly message for the frontend.
func formatLogForFrontend(r slog.Record) string {
	// Skip certain internal/verbose logs
	if shouldSkipLogMessage(r.Message) {
		return ""
	}
	
	var b strings.Builder
	
	// Add level emoji
	switch r.Level {
	case slog.LevelError:
		b.WriteString("❌ ")
	case slog.LevelWarn:
		b.WriteString("⚠️ ")
	case slog.LevelInfo:
		b.WriteString("ℹ️ ")
	default:
		b.WriteString("📝 ")
	}
	
	// Format message: convert snake_case to readable text
	msg := formatMessage(r.Message)
	b.WriteString(msg)
	
	// Add relevant attributes (filtered)
	attrs := formatAttrs(r)
	if attrs != "" {
		b.WriteString(" ")
		b.WriteString(attrs)
	}
	
	return b.String()
}

// shouldSkipLogMessage returns true if the log message should not be sent to frontend.
func shouldSkipLogMessage(msg string) bool {
	// Skip overly verbose internal logs
	skippedPrefixes := []string{
		"console_stream_",
		"llm_stats_",
		"heartbeat_",
		"memory_",
		"mcp_",
		"guard_",
		"bus_",
		"inspector_",
	}
	
	msgLower := strings.ToLower(msg)
	for _, prefix := range skippedPrefixes {
		if strings.HasPrefix(msgLower, prefix) {
			return true
		}
	}
	
	return false
}

// formatMessage converts technical log messages to user-friendly text.
func formatMessage(msg string) string {
	// Map common log messages to user-friendly versions
	messageMap := map[string]string{
		"console_llm_credentials_missing":    "正在配置 LLM 凭据...",
		"task_started":                       "开始执行任务",
		"task_completed":                     "任务完成",
		"task_failed":                        "任务失败",
		"tool_execution_started":             "开始执行工具",
		"tool_execution_completed":           "工具执行完成",
		"llm_request_started":                "正在请求 AI...",
		"llm_request_completed":              "AI 响应完成",
		"skill_loaded":                       "技能已加载",
		"skill_executed":                     "执行技能",
	}
	
	if friendly, ok := messageMap[msg]; ok {
		return friendly
	}
	
	// Default: just return the original message
	return msg
}

// formatAttrs extracts and formats relevant attributes from a log record.
func formatAttrs(r slog.Record) string {
	var parts []string
	
	r.Attrs(func(a slog.Attr) bool {
		key := a.Key
		val := a.Value.String()
		
		// Skip internal fields
		switch key {
		case "task_id", "conversation_key", "timestamp", "level":
			return true
		}
		
		// Truncate long values
		if len(val) > 80 {
			val = val[:77] + "..."
		}
		
		// Format specific fields nicely
		switch key {
		case "provider":
			parts = append(parts, "提供商="+val)
		case "model":
			parts = append(parts, "模型="+val)
		case "tool":
			parts = append(parts, "工具="+val)
		case "skill":
			parts = append(parts, "技能="+val)
		case "error":
			parts = append(parts, "错误="+val)
		case "hint":
			parts = append(parts, "提示="+val)
		case "duration_ms":
			parts = append(parts, "耗时="+val+"ms")
		default:
			// Only include if value is short and readable
			if len(val) < 40 && len(parts) < 3 {
				parts = append(parts, key+"="+val)
			}
		}
		
		return len(parts) < 4 // Limit number of attributes
	})
	
	if len(parts) == 0 {
		return ""
	}
	
	return "(" + strings.Join(parts, ", ") + ")"
}

// atomicBool is a thread-safe boolean type.
type atomicBool struct {
	value atomic.Bool
}

func (b *atomicBool) Load() bool {
	return b.value.Load()
}

func (b *atomicBool) Store(v bool) {
	b.value.Store(v)
}

// parseSlogLevelInternal parses a slog level string (duplicated from logutil to avoid import cycles).
func parseSlogLevelInternal(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown logging.level: %s", s)
	}
}
