package line

import (
	"context"
	"fmt"
	"strings"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	linebus "github.com/quailyquaily/mistermorph/internal/bus/adapters/line"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/workspace"
)

func maybeHandleLineCommand(ctx context.Context, d Dependencies, inprocBus *busruntime.Inproc, store *workspace.Store, conversationKey string, inbound linebus.InboundMessage) (bool, error) {
	reg := chatcommands.NewRuntimeRegistry(chatcommands.RuntimeRegistryOptions{
		ModelCommand:   d.HandleModelCommand,
		WorkspaceStore: store,
		WorkspaceKey:   conversationKey,
	})
	result, handled, err := reg.Dispatch(ctx, inbound.Text)
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
	correlationID := fmt.Sprintf("line:command:%s:%s", inbound.ChatID, inbound.MessageID)
	_, publishErr := publishLineBusOutbound(ctx, inprocBus, inbound.ChatID, output, inbound.ReplyToken, correlationID)
	return true, publishErr
}
