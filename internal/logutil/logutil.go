package logutil

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/spf13/viper"
)

type loggerConfig struct {
	Level     string
	Format    string
	AddSource bool
}

func LoggerFromViper() (*slog.Logger, error) {
	logCfg := loggerConfig{
		Level:     viper.GetString("logging.level"),
		Format:    viper.GetString("logging.format"),
		AddSource: viper.GetBool("logging.add_source"),
	}
	return newLoggerFromConfig(logCfg)
}

func LogOptionsFromViper() agent.LogOptions {
	logOpts := agent.DefaultLogOptions()
	logOpts.IncludeThoughts = viper.GetBool("logging.include_thoughts")
	logOpts.IncludeToolParams = viper.GetBool("logging.include_tool_params")
	logOpts.IncludeSkillContents = viper.GetBool("logging.include_skill_contents")
	logOpts.MaxThoughtChars = viper.GetInt("logging.max_thought_chars")
	logOpts.MaxJSONBytes = viper.GetInt("logging.max_json_bytes")
	logOpts.MaxStringValueChars = viper.GetInt("logging.max_string_value_chars")
	logOpts.MaxSkillContentChars = viper.GetInt("logging.max_skill_content_chars")
	if viper.IsSet("logging.redact_keys") {
		keys := viper.GetStringSlice("logging.redact_keys")
		if len(keys) > 0 {
			logOpts.RedactKeys = keys
		}
	}
	return logOpts
}

func newLoggerFromConfig(cfg loggerConfig) (*slog.Logger, error) {
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

	return slog.New(h), nil
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
