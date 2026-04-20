package chatcmd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
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

	reg.Register("/workspace", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		cmd, err := workspace.ParseCommandArgs(args)
		if err != nil {
			return &chatcommands.Result{Reply: err.Error()}, nil
		}
		switch cmd.Action {
		case workspace.CommandStatus:
			return &chatcommands.Result{Reply: workspace.StatusText(sess.workspaceDir)}, nil
		case workspace.CommandAttach:
			dir, err := workspace.ValidateDir(cmd.Dir, nil)
			if err != nil {
				return &chatcommands.Result{Reply: "error: " + err.Error()}, nil
			}
			oldDir := sess.workspaceDir
			sess.workspaceDir = dir
			sess.refreshProjectScope()
			if err := sess.rebuildRuntimeState(); err != nil {
				sess.workspaceDir = oldDir
				sess.refreshProjectScope()
				_ = sess.rebuildRuntimeState()
				return nil, err
			}
			return &chatcommands.Result{Reply: workspace.AttachText(oldDir, dir, oldDir != "")}, nil
		case workspace.CommandDetach:
			oldDir := sess.workspaceDir
			sess.workspaceDir = ""
			sess.refreshProjectScope()
			if err := sess.rebuildRuntimeState(); err != nil {
				sess.workspaceDir = oldDir
				sess.refreshProjectScope()
				_ = sess.rebuildRuntimeState()
				return nil, err
			}
			return &chatcommands.Result{Reply: workspace.DetachText(oldDir, oldDir != "")}, nil
		default:
			return &chatcommands.Result{Reply: "error: unsupported workspace command"}, nil
		}
	})

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
		projectDir := sess.projectDir()
		agentsPath := filepath.Join(projectDir, "AGENTS.md")
		if handleInitRead(writer, agentsPath) {
			return &chatcommands.Result{}, nil
		}
		newHistory, ok := handleAgentsGenerate(writer, "/init", projectDir, sess.timeout, sess.engine, sess.mainCfg.Model, *history)
		if ok {
			*history = newHistory
		}
		return &chatcommands.Result{}, nil
	})

	reg.Register("/update", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		newHistory, ok := handleAgentsGenerate(writer, "/update", sess.projectDir(), sess.timeout, sess.engine, sess.mainCfg.Model, *history)
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
	_, _ = fmt.Fprintln(writer, "Commands: /exit, /quit, /reset, /memory, /remember <content>, /model, /workspace, /init, /update, /help")
}

func chatBuiltinCommandsBlock() string {
	return "## Built-in Chat Commands\n\n" +
		"The user can type these special commands at any time:\n" +
		"- `/exit` or `/quit` — exit the chat session\n" +
		"- `/reset` — reset the current conversation (clear history, keep memory)\n" +
		"- `/memory` — display the current project memory\n" +
		"- `/remember <content>` — add a long-term memory item for the current project\n" +
		"- `/model` — inspect or change the current model selection for this session\n" +
		"- `/workspace` — show the current workspace attachment\n" +
		"- `/workspace attach <dir>` — attach or replace the current workspace directory\n" +
		"- `/workspace detach` — detach the current workspace directory\n" +
		"- `/init` — generate an AGENTS.md file for the current project\n" +
		"- `/update` — regenerate AGENTS.md, overwriting the existing file\n" +
		"If the user asks about any of these commands, explain what they do."
}
