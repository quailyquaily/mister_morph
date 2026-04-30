package slack

import (
	"context"
	"fmt"
	"strings"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/workspace"
)

func maybeHandleSlackCommand(ctx context.Context, d Dependencies, inprocBus *busruntime.Inproc, store *workspace.Store, conversationKey string, event slackInboundEvent, botUserID string) (bool, error) {
	if isSlackGroupChat(event.ChatType) && !slackCommandExplicitlyAddressed(event.Text, botUserID) {
		return false, nil
	}
	text := normalizeSlackCommandText(event.Text, botUserID)
	reg := chatcommands.NewRuntimeRegistry(chatcommands.RuntimeRegistryOptions{
		ModelCommand:   d.HandleModelCommand,
		WorkspaceStore: store,
		WorkspaceKey:   conversationKey,
	})
	result, handled, err := reg.Dispatch(ctx, text)
	if !handled {
		return false, nil
	}
	output := ""
	if err != nil {
		output = "error: " + strings.TrimSpace(err.Error())
	} else if result != nil {
		output = strings.TrimSpace(result.Reply)
	}
	if output == "" {
		return true, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	correlationID := fmt.Sprintf("slack:command:%s:%s", event.ChannelID, event.MessageTS)
	_, publishErr := publishSlackBusOutbound(
		ctx,
		inprocBus,
		event.TeamID,
		event.ChannelID,
		output,
		event.ThreadTS,
		correlationID,
	)
	return true, publishErr
}

func normalizeSlackCommandText(text string, botUserID string) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return ""
	}
	mention := "<@" + strings.TrimSpace(botUserID) + ">"
	if strings.TrimSpace(botUserID) != "" && fields[0] == mention {
		fields = fields[1:]
	}
	return strings.Join(fields, " ")
}

func slackCommandExplicitlyAddressed(text string, botUserID string) bool {
	if strings.TrimSpace(botUserID) == "" {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(text), "<@"+strings.TrimSpace(botUserID)+">")
}
