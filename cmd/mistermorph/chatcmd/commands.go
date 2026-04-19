package chatcmd

import (
	"context"
	"fmt"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/llm"
	"io"
	"path/filepath"
)

// registerChatCommands binds all slash commands into the given registry.
// Each handler receives the mutable session so it can update client/engine state
// when necessary (e.g. /model).
func registerChatCommands(reg *chatcommands.Registry, sess *chatSession, history *[]llm.Message) {
	writer := sess.writer

	reg.Register("/exit", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		_, _ = fmt.Fprintln(writer, "Bye! 👋")
		return &chatcommands.Result{Quit: true}, nil
	})

	reg.Register("/quit", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		_, _ = fmt.Fprintln(writer, "Bye! 👋")
		return &chatcommands.Result{Quit: true}, nil
	})

	reg.Register("/reset", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		*history = nil
		return &chatcommands.Result{Reply: "Session reset."}, nil
	})

	reg.Register("/memory", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		handleMemory(writer, sess.memOrchestrator, sess.subjectID)
		return &chatcommands.Result{}, nil
	})

	reg.Register("/help", chatcommands.HelpHandler(reg, "Available commands:"))

	reg.Register("/remember", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		if args == "" {
			return &chatcommands.Result{Reply: "Usage: /remember <content>"}, nil
		}
		handleRemember(writer, "/remember "+args, sess.memManager, sess.subjectID)
		return &chatcommands.Result{}, nil
	})

	reg.Register("/model", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		text := "/model"
		if args != "" {
			text = "/model " + args
		}
		newClient, newCfg, handled := handleModelCommand(writer, text, sess.llmValues, sess.sessionStore, sess.buildClient)
		if handled {
			oldClient := sess.client
			oldCfg := sess.mainCfg
			oldEngine := sess.engine
			oldRegistry := sess.toolRegistry

			sess.client = newClient
			sess.mainCfg = newCfg
			if err := sess.rebuildRuntimeState(); err != nil {
				sess.client = oldClient
				sess.mainCfg = oldCfg
				sess.engine = oldEngine
				sess.toolRegistry = oldRegistry
				return nil, err
			}
		}
		return &chatcommands.Result{}, nil
	})

	reg.Register("/init", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		agentsPath := filepath.Join(sess.chatFileCacheDir, "AGENTS.md")
		if handleInitRead(writer, agentsPath) {
			return &chatcommands.Result{}, nil
		}
		newHistory, ok := handleAgentsGenerate(writer, "/init", sess.chatFileCacheDir, sess.timeout, sess.engine, sess.mainCfg.Model, *history)
		if ok {
			*history = newHistory
		}
		return &chatcommands.Result{}, nil
	})

	reg.Register("/update", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		newHistory, ok := handleAgentsGenerate(writer, "/update", sess.chatFileCacheDir, sess.timeout, sess.engine, sess.mainCfg.Model, *history)
		if ok {
			*history = newHistory
		}
		return &chatcommands.Result{}, nil
	})
}

// handleExit prints the exit message.
func handleExit(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, "Bye! 👋")
}

// handleHelp prints the help text.
func handleHelp(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, "Commands: /exit, /quit, /reset, /memory, /remember <content>, /model, /init, /update, /help")
}
