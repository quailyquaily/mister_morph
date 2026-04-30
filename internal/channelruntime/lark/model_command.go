package lark

import (
	"context"
	"fmt"
	"strings"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	larkbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/lark"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/workspace"
)

func maybeHandleLarkCommand(ctx context.Context, d Dependencies, inprocBus *busruntime.Inproc, store *workspace.Store, conversationKey string, inbound larkbus.InboundMessage) (bool, error) {
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
	correlationID := fmt.Sprintf("lark:command:%s:%s", inbound.ChatID, inbound.MessageID)
	_, publishErr := publishLarkBusOutbound(ctx, inprocBus, inbound.ChatID, output, inbound.MessageID, correlationID)
	return true, publishErr
}
