package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/integration"
)

// ListDirTool is an example of a project-specific tool you provide to the agent.
// It intentionally stays simple for demo purposes.
type ListDirTool struct {
	Root string
}

func (t *ListDirTool) Name() string { return "list_dir" }

func (t *ListDirTool) Description() string {
	return "Lists files under a directory (relative to a configured root)."
}

func (t *ListDirTool) ParameterSchema() string {
	return `{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Relative path under the configured root (default: .)."}
  }
}`
}

func (t *ListDirTool) Execute(_ context.Context, params map[string]any) (string, error) {
	rel, _ := params["path"].(string)
	if rel == "" {
		rel = "."
	}
	root, err := filepath.Abs(t.Root)
	if err != nil {
		return "", err
	}
	p := filepath.Join(root, rel)
	p, err = filepath.Abs(p)
	if err != nil {
		return "", err
	}
	// Basic containment check.
	if len(p) < len(root) || p[:len(root)] != root {
		return "", fmt.Errorf("path escapes root")
	}

	entries, err := os.ReadDir(p)
	if err != nil {
		return "", err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		out = append(out, name)
	}
	b, _ := json.MarshalIndent(map[string]any{
		"root":  root,
		"path":  rel,
		"files": out,
	}, "", "  ")
	return string(b), nil
}

type GetWeatherTool struct {
}

func (t *GetWeatherTool) Name() string { return "get_weather" }

func (t *GetWeatherTool) Description() string {
	return "Gets current weather for a city from a configured weather API endpoint."
}

func (t *GetWeatherTool) ParameterSchema() string {
	return `{
  "type": "object",
  "properties": {
    "city": {"type": "string", "description": "City name, e.g. San Francisco."}
  },
  "required": ["city"]
}`
}

func (t *GetWeatherTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	_ = ctx
	city, _ := params["city"].(string)
	city = strings.TrimSpace(city)
	if city == "" {
		return "", fmt.Errorf("city is required")
	}
	out, _ := json.MarshalIndent(map[string]any{
		"source":         "mock_weather_tool",
		"city":           city,
		"condition":      "raining",
		"temperature_c":  18,
		"humidity_pct":   82,
		"updated_at_utc": "2026-02-13T09:00:00Z",
	}, "", "  ")
	return string(out), nil
}

func main() {
	var (
		mode            = flag.String("mode", "task", "Run mode: task|telegram|slack.")
		task            = flag.String("task", "List files and summarize the project.", "Task to run in --mode task.")
		model           = flag.String("model", "gpt-5.2", "Model name.")
		apiKey          = flag.String("api-key", os.Getenv("OPENAI_API_KEY"), "API key (defaults to OPENAI_API_KEY).")
		maxSteps        = flag.Int("max-steps", 15, "Agent max tool-call steps.")
		toolRepeatLimit = flag.Int("tool-repeat-limit", 3, "Force final when the same successful tool call repeats this many times.")
		inspectPrompt   = flag.Bool("inspect-prompt", false, "Dump prompts to ./dump.")
		inspectRequest  = flag.Bool("inspect-request", false, "Dump request/response payloads to ./dump.")

		telegramBotToken = flag.String("telegram-bot-token", os.Getenv("TG_BOT_TOKEN"), "Telegram bot token (or TG_BOT_TOKEN).")

		slackBotToken = flag.String("slack-bot-token", os.Getenv("SLACK_BOT_TOKEN"), "Slack bot token xoxb-... (or SLACK_BOT_TOKEN).")
		slackAppToken = flag.String("slack-app-token", os.Getenv("SLACK_APP_TOKEN"), "Slack app token xapp-... (or SLACK_APP_TOKEN).")
	)
	flag.Parse()

	cfg := integration.DefaultConfig()
	cfg.Inspect.Prompt = *inspectPrompt
	cfg.Inspect.Request = *inspectRequest
	cfg.BuiltinToolNames = []string{"read_file", "url_fetch", "todo_update"}
	cfg.Set("llm.provider", "openai")
	cfg.Set("llm.api_key", strings.TrimSpace(*apiKey))
	cfg.Set("llm.model", strings.TrimSpace(*model))
	cfg.Set("tools.todo_update.enabled", true)
	cfg.Set("max_steps", *maxSteps)
	cfg.Set("tool_repeat_limit", *toolRepeatLimit)

	rt := integration.New(cfg)

	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "task":
		runTaskMode(rt, *task, *model)
	case "telegram":
		runner, err := rt.NewTelegramBot(integration.TelegramOptions{
			BotToken: strings.TrimSpace(*telegramBotToken),
		})
		if err != nil {
			exitErr(err)
		}
		runBotMode(runner)
	case "slack":
		runner, err := rt.NewSlackBot(integration.SlackOptions{
			BotToken: strings.TrimSpace(*slackBotToken),
			AppToken: strings.TrimSpace(*slackAppToken),
		})
		if err != nil {
			exitErr(err)
		}
		runBotMode(runner)
	default:
		exitErr(fmt.Errorf("invalid --mode %q (expected: task|telegram|slack)", *mode))
	}
}

func runTaskMode(rt *integration.Runtime, task string, model string) {
	reg := rt.NewRegistry()
	reg.Register(&ListDirTool{Root: "."})
	reg.Register(&GetWeatherTool{})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prepared, err := rt.NewRunEngineWithRegistry(ctx, task, reg)
	if err != nil {
		exitErr(err)
	}
	defer func() { _ = prepared.Cleanup() }()

	runModel := strings.TrimSpace(model)
	if runModel == "" {
		runModel = strings.TrimSpace(prepared.Model)
	}
	final, _, err := prepared.Engine.Run(ctx, task, agent.RunOptions{
		Model:               runModel,
		ContextWindowTokens: prepared.ContextWindowTokens,
	})
	if err != nil {
		exitErr(err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(final)
}

func runBotMode(runner integration.BotRunner) {
	defer func() { _ = runner.Close() }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		exitErr(err)
	}
}

func exitErr(err error) {
	if err == nil {
		os.Exit(1)
	}
	_, _ = fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}
