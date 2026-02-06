package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	uniaiProvider "github.com/quailyquaily/mistermorph/providers/uniai"
	"github.com/quailyquaily/mistermorph/tools"
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

func main() {
	var (
		task     = flag.String("task", "List files and summarize the project.", "Task to run.")
		model    = flag.String("model", "gpt-5.2", "Model name.")
		endpoint = flag.String("endpoint", "https://api.openai.com", "OpenAI-compatible base URL.")
		apiKey   = flag.String("api-key", os.Getenv("OPENAI_API_KEY"), "API key (defaults to OPENAI_API_KEY).")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	client := uniaiProvider.New(uniaiProvider.Config{
		Provider: "openai",
		Endpoint: *endpoint,
		APIKey:   *apiKey,
		Model:    *model,
	})

	reg := tools.NewRegistry()
	reg.Register(&ListDirTool{Root: "."})

	spec := agent.DefaultPromptSpec()
	spec.Identity = "You are an agent embedded inside another Go app. Use tools to inspect the local project when helpful."

	engine := agent.New(
		client,
		reg,
		agent.Config{MaxSteps: 8, ParseRetries: 2},
		spec,
		agent.WithLogger(logger),
		agent.WithLogOptions(agent.LogOptions{
			IncludeThoughts:   true,
			IncludeToolParams: true,
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	final, _, err := engine.Run(ctx, *task, agent.RunOptions{Model: *model})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(final)
}
