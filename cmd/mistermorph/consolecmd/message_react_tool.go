package consolecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
)

type consoleMessageReactTool struct {
	lastEmoji string
}

func newConsoleMessageReactTool() *consoleMessageReactTool {
	return &consoleMessageReactTool{}
}

func (t *consoleMessageReactTool) Name() string { return "message_react" }

func (t *consoleMessageReactTool) Description() string {
	return "Sends the emoji as a normal text message in Console. Use for lightweight acknowledgements."
}

func (t *consoleMessageReactTool) ParameterSchema() string {
	s := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"emoji": map[string]any{
				"type":        "string",
				"description": "Emoji to send as the Console reply text.",
			},
		},
		"required": []string{"emoji"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *consoleMessageReactTool) Execute(_ context.Context, params map[string]any) (string, error) {
	if t == nil {
		return "", fmt.Errorf("message_react is disabled")
	}
	emoji, _ := params["emoji"].(string)
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return "", fmt.Errorf("missing required param: emoji")
	}
	t.lastEmoji = emoji
	return fmt.Sprintf("sent emoji message: %s", emoji), nil
}

func (t *consoleMessageReactTool) LastEmoji() string {
	if t == nil {
		return ""
	}
	return strings.TrimSpace(t.lastEmoji)
}

func applyConsoleMessageReactionFinal(final *agent.Final, emoji string) *agent.Final {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return final
	}
	if final == nil {
		return &agent.Final{Output: emoji}
	}
	if final.IsLightweight || final.Output == nil || strings.TrimSpace(fmt.Sprint(final.Output)) == "" {
		final.Output = emoji
		final.IsLightweight = false
	}
	return final
}
