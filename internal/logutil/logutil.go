package logutil

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/spf13/viper"
)

const (
	DefaultFileLogMaxAge = 7 * 24 * time.Hour
	fileLogDateLayout    = "2006-01-02"
	fileLogPrefix        = "mistermorph-"
	fileLogSuffix        = ".jsonl"
)

type ConfigReader interface {
	GetString(string) string
	GetBool(string) bool
	GetInt(string) int
	GetStringSlice(string) []string
	IsSet(string) bool
}

type LoggerConfig struct {
	Level        string
	Format       string
	AddSource    bool
	FileDir      string
	FileMaxAge   string
	FileStateDir string
}

type LogOptionsConfig struct {
	IncludeThoughts      bool
	IncludeToolParams    bool
	IncludeSkillContents bool
	MaxThoughtChars      int
	MaxJSONBytes         int
	MaxStringValueChars  int
	MaxSkillContentChars int
	RedactKeys           []string
	RedactKeysSet        bool
}

func LoggerConfigFromReader(r ConfigReader) LoggerConfig {
	if r == nil {
		return LoggerConfig{}
	}
	return LoggerConfig{
		Level:        r.GetString("logging.level"),
		Format:       r.GetString("logging.format"),
		AddSource:    r.GetBool("logging.add_source"),
		FileDir:      r.GetString("logging.file.dir"),
		FileMaxAge:   r.GetString("logging.file.max_age"),
		FileStateDir: r.GetString("file_state_dir"),
	}
}

func LoggerConfigFromViper() LoggerConfig {
	return LoggerConfigFromReader(viper.GetViper())
}

func LoggerFromViper() (*slog.Logger, error) {
	return LoggerFromConfig(LoggerConfigFromViper())
}

func LogOptionsConfigFromReader(r ConfigReader) LogOptionsConfig {
	if r == nil {
		return LogOptionsConfig{}
	}
	return LogOptionsConfig{
		IncludeThoughts:      r.GetBool("logging.include_thoughts"),
		IncludeToolParams:    r.GetBool("logging.include_tool_params"),
		IncludeSkillContents: r.GetBool("logging.include_skill_contents"),
		MaxThoughtChars:      r.GetInt("logging.max_thought_chars"),
		MaxJSONBytes:         r.GetInt("logging.max_json_bytes"),
		MaxStringValueChars:  r.GetInt("logging.max_string_value_chars"),
		MaxSkillContentChars: r.GetInt("logging.max_skill_content_chars"),
		RedactKeys:           append([]string(nil), r.GetStringSlice("logging.redact_keys")...),
		RedactKeysSet:        r.IsSet("logging.redact_keys"),
	}
}

func LogOptionsConfigFromViper() LogOptionsConfig {
	return LogOptionsConfigFromReader(viper.GetViper())
}

func LogOptionsFromConfig(cfg LogOptionsConfig) agent.LogOptions {
	logOpts := agent.DefaultLogOptions()
	logOpts.IncludeThoughts = cfg.IncludeThoughts
	logOpts.IncludeToolParams = cfg.IncludeToolParams
	logOpts.IncludeSkillContents = cfg.IncludeSkillContents
	logOpts.MaxThoughtChars = cfg.MaxThoughtChars
	logOpts.MaxJSONBytes = cfg.MaxJSONBytes
	logOpts.MaxStringValueChars = cfg.MaxStringValueChars
	logOpts.MaxSkillContentChars = cfg.MaxSkillContentChars
	if cfg.RedactKeysSet && len(cfg.RedactKeys) > 0 {
		logOpts.RedactKeys = append([]string(nil), cfg.RedactKeys...)
	}
	return logOpts
}

func LogOptionsFromViper() agent.LogOptions {
	return LogOptionsFromConfig(LogOptionsConfigFromViper())
}

func LoggerFromConfig(cfg LoggerConfig) (*slog.Logger, error) {
	level, err := parseSlogLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
	}

	var h slog.Handler
	switch strings.ToLower(strings.TrimSpace(cfg.Format)) {
	case "", "text":
		h = slog.NewTextHandler(os.Stderr, opts)
	case "json":
		h = slog.NewJSONHandler(os.Stderr, opts)
	default:
		return nil, fmt.Errorf("unknown logging.format: %s", cfg.Format)
	}

	fileMaxAge, err := ParseFileLogMaxAge(cfg.FileMaxAge)
	if err != nil {
		return nil, err
	}
	fileWriter, err := newDailyLogWriter(dailyLogWriterConfig{
		Dir:      ResolveFileLogDir(cfg.FileStateDir, cfg.FileDir),
		MaxAge:   fileMaxAge,
		FileBase: fileLogPrefix,
	})
	if err != nil {
		return nil, err
	}
	h = multiHandler{slog.NewJSONHandler(fileWriter, opts), h}

	return slog.New(h), nil
}

func ResolveFileLogDir(fileStateDir, configuredDir string) string {
	configuredDir = strings.TrimSpace(configuredDir)
	if configuredDir != "" {
		return pathutil.ExpandHomePath(configuredDir)
	}
	return filepath.Clean(filepath.Join(pathutil.ResolveStateDir(fileStateDir), "logs"))
}

func ParseFileLogMaxAge(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultFileLogMaxAge, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid logging.file.max_age: %w", err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid logging.file.max_age: must be positive")
	}
	return d, nil
}

func parseSlogLevel(s string) (slog.Level, error) {
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

type multiHandler []slog.Handler

func (h multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, child := range h {
		if child != nil && child.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h multiHandler) Handle(ctx context.Context, record slog.Record) error {
	var firstErr error
	for _, child := range h {
		if child == nil || !child.Enabled(ctx, record.Level) {
			continue
		}
		if err := child.Handle(ctx, record.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(multiHandler, 0, len(h))
	for _, child := range h {
		if child != nil {
			out = append(out, child.WithAttrs(attrs))
		}
	}
	return out
}

func (h multiHandler) WithGroup(name string) slog.Handler {
	out := make(multiHandler, 0, len(h))
	for _, child := range h {
		if child != nil {
			out = append(out, child.WithGroup(name))
		}
	}
	return out
}

type dailyLogWriterConfig struct {
	Dir      string
	MaxAge   time.Duration
	Now      func() time.Time
	FileBase string
}

type dailyLogWriter struct {
	mu       sync.Mutex
	dir      string
	maxAge   time.Duration
	now      func() time.Time
	fileBase string
	date     string
	file     *os.File
}

func newDailyLogWriter(cfg dailyLogWriterConfig) (*dailyLogWriter, error) {
	dir := strings.TrimSpace(cfg.Dir)
	if dir == "" {
		return nil, fmt.Errorf("logging.file.dir resolved to empty path")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	maxAge := cfg.MaxAge
	if maxAge <= 0 {
		maxAge = DefaultFileLogMaxAge
	}
	fileBase := strings.TrimSpace(cfg.FileBase)
	if fileBase == "" {
		fileBase = fileLogPrefix
	}
	w := &dailyLogWriter{
		dir:      filepath.Clean(dir),
		maxAge:   maxAge,
		now:      now,
		fileBase: fileBase,
	}
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	if err := w.cleanupLocked(now()); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *dailyLogWriter) Write(p []byte) (int, error) {
	if w == nil {
		return 0, fmt.Errorf("log writer is nil")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	if err := w.ensureFileLocked(now); err != nil {
		return 0, err
	}
	n, err := w.file.Write(p)
	if err != nil {
		return n, err
	}
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

func (w *dailyLogWriter) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeLocked()
}

func (w *dailyLogWriter) ensureFileLocked(now time.Time) error {
	date := now.Local().Format(fileLogDateLayout)
	if w.file != nil && w.date == date {
		return nil
	}
	if err := w.closeLocked(); err != nil {
		return err
	}
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	filePath := filepath.Join(w.dir, w.fileBase+date+fileLogSuffix)
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	w.file = f
	w.date = date
	if err := w.cleanupLocked(now); err != nil {
		return err
	}
	return nil
}

func (w *dailyLogWriter) closeLocked() error {
	if w.file == nil {
		return nil
	}
	f := w.file
	w.file = nil
	w.date = ""
	if err := f.Close(); err != nil {
		return fmt.Errorf("close log file: %w", err)
	}
	return nil
}

func (w *dailyLogWriter) cleanupLocked(now time.Time) error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return fmt.Errorf("read log dir: %w", err)
	}
	cutoff := now.Local().Add(-w.maxAge)
	cutoffDate := time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, cutoff.Location())
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		date, ok := parseLogFilenameDate(w.fileBase, entry.Name(), cutoff.Location())
		if !ok || !date.Before(cutoffDate) {
			continue
		}
		if err := os.Remove(filepath.Join(w.dir, entry.Name())); err != nil {
			fmt.Fprintf(os.Stderr, "warn: remove old log file failed file=%q error=%v\n", entry.Name(), err)
		}
	}
	return nil
}

func parseLogFilenameDate(fileBase, name string, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.Local
	}
	if !strings.HasPrefix(name, fileBase) || !strings.HasSuffix(name, fileLogSuffix) {
		return time.Time{}, false
	}
	rawDate := strings.TrimSuffix(strings.TrimPrefix(name, fileBase), fileLogSuffix)
	date, err := time.ParseInLocation(fileLogDateLayout, rawDate, loc)
	if err != nil {
		return time.Time{}, false
	}
	return date, true
}
