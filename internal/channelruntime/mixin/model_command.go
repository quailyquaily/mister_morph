package mixin

import (
	"context"
	"fmt"
	"strings"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	mixinbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/mixin"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/internal/workspace"
)

func maybeHandleMixinCommand(ctx context.Context, d Dependencies, bus *busruntime.Inproc, store *workspace.Store, conversationKey string, inbound mixinbus.InboundMessage, currentSkills []string, reset func(context.Context) error) (bool, error) {
	registry := chatcommands.NewRuntimeRegistry(chatcommands.RuntimeRegistryOptions{
		ModelCommand:        d.HandleModelCommand,
		SkillCommand:        skillCommandForMixin(d.HandleSkillCommand, currentSkills),
		ContextCommand:      topiccontext.NewStore(d.RuntimePaths.TopicContextPath).CommandFunc(conversationKey),
		WorkspaceStore:      store,
		WorkspaceKey:        conversationKey,
		DefaultWorkspaceDir: d.DefaultWorkspaceDir,
	})
	registry.Register("/id", "show the current Mixin conversation id", func(context.Context, string) (*chatcommands.Result, error) {
		return &chatcommands.Result{Reply: fmt.Sprintf("conversation_id=%s type=%s", strings.TrimSpace(inbound.ConversationID), strings.TrimSpace(inbound.ChatType))}, nil
	})
	if reset != nil {
		registry.Register("/reset", "clear conversation history and sticky skills", func(commandCtx context.Context, _ string) (*chatcommands.Result, error) {
			if err := reset(commandCtx); err != nil {
				return nil, err
			}
			return &chatcommands.Result{Reply: "ok (reset)"}, nil
		})
	}
	result, handled, err := registry.Dispatch(ctx, inbound.Text)
	if !handled {
		return false, nil
	}
	if result != nil && result.Action == chatcommands.ActionContextCompact {
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
	_, publishErr := publishMixinBusOutbound(ctx, bus, inbound.ConversationID, inbound.FromUserID, output, inbound.MessageID, fmt.Sprintf("mixin:command:%s:%s", inbound.ConversationID, inbound.MessageID))
	return true, publishErr
}

func skillCommandForMixin(fn HandleSkillCommandFunc, currentSkills []string) chatcommands.SkillCommandFunc {
	if fn == nil {
		return nil
	}
	snapshot := append([]string(nil), currentSkills...)
	return func() (string, error) { return fn(snapshot) }
}

func isMixinStopCommand(text string) bool {
	command, _ := chatcommands.ParseCommand(text)
	return chatcommands.NormalizeCommand(command) == "/stop"
}
