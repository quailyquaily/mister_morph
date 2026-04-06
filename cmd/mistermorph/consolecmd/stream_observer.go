package consolecmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/llm"
)

type consoleLLMObserver struct {
	runtime *taskruntime.Runtime
	model   string
	logger  *slog.Logger
}

func newConsoleLLMObserver(rt *taskruntime.Runtime, model string, logger *slog.Logger) consoleSemanticObserver {
	if rt == nil {
		return nil
	}
	return &consoleLLMObserver{
		runtime: rt,
		model:   strings.TrimSpace(model),
		logger:  logger,
	}
}

func (o *consoleLLMObserver) Summarize(ctx context.Context, req consoleObserveRequest) (string, error) {
	if o == nil || o.runtime == nil {
		return "", fmt.Errorf("console observer runtime unavailable")
	}
	route, err := o.runtime.ResolveMainRouteForRun()
	if err != nil {
		return "", err
	}
	client, err := o.runtime.CreateClientForRoute(route)
	if err != nil {
		return "", err
	}
	defer closeConsoleObserverClient(o.logger, client)

	model := strings.TrimSpace(o.model)
	if model == "" {
		model = strings.TrimSpace(route.ClientConfig.Model)
	}

	result, err := client.Chat(ctx, llm.Request{
		Model: model,
		Scene: "console.observe",
		Messages: []llm.Message{
			{
				Role: "system",
				Content: "You summarize noisy tool or subtask progress into one short plain-text progress update. " +
					"Write at most two short sentences. Do not repeat raw HTML or long logs. Do not use markdown lists.",
			},
			{
				Role:    "user",
				Content: buildConsoleObservePrompt(req),
			},
		},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Text), nil
}

func buildConsoleObservePrompt(req consoleObserveRequest) string {
	var b strings.Builder
	b.WriteString("profile=")
	b.WriteString(strings.TrimSpace(string(req.Profile)))
	b.WriteString("\ntrigger=")
	b.WriteString(strings.TrimSpace(req.Trigger))
	b.WriteString("\n\nCurrent task preview snapshot:\n")
	b.WriteString(strings.TrimSpace(req.Snapshot))
	b.WriteString("\n\nReturn a short user-facing progress update.")
	return b.String()
}

func closeConsoleObserverClient(logger *slog.Logger, client llm.Client) {
	if client == nil {
		return
	}
	closer, ok := client.(io.Closer)
	if !ok {
		return
	}
	if err := closer.Close(); err != nil && logger != nil {
		logger.Warn("console_observer_client_close_failed", "error", err.Error())
	}
}
