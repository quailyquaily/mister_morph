package chatcmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
)

func newChatRuntimeCommandRegistry(sess *chatSession) *chatcommands.Registry {
	return chatcommands.NewRuntimeRegistry(chatcommands.RuntimeRegistryOptions{
		ModelCommand: chatModelCommand(sess),
		SkillCommand: func() (string, error) {
			return skillsutil.RenderSkillStatus(skillsutil.SkillsConfigFromRunCmd(sess.cmd), sess.loadedSkills)
		},
		ContextCommand: func() (string, error) {
			return topiccontext.RenderCommandText(sess.conversationKey())
		},
		WorkspaceCommand: chatWorkspaceCommand(sess),
	})
}

// registerChatCommands binds all slash commands into the given registry.
// Each handler receives the mutable session so it can update client/engine state
// when necessary (e.g. /models).
func registerChatCommands(reg *chatcommands.Registry, sess *chatSession, history *[]llm.Message) {
	writer := sess.writer

	reg.Register("/exit", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		return &chatcommands.Result{Quit: true}, nil
	})

	reg.Register("/quit", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		return &chatcommands.Result{Quit: true}, nil
	})

	reg.Register("/reset", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		*history = nil
		return &chatcommands.Result{Reply: "Session reset."}, nil
	})

	reg.Register("/stop", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		return &chatcommands.Result{Reply: "当前没有正在运行的任务。"}, nil
	})

	reg.Register("/memory", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		handleMemory(writer, sess.memOrchestrator, sess.subjectID)
		return &chatcommands.Result{}, nil
	})

	reg.Register("/remember", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		if args == "" {
			return &chatcommands.Result{Reply: "Usage: /remember <content>"}, nil
		}
		handleRemember(writer, "/remember "+args, sess.memManager, sess.subjectID)
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

func chatModelCommand(sess *chatSession) chatcommands.ModelCommandFunc {
	return func(text string) (string, bool, error) {
		if sess == nil {
			return "", true, fmt.Errorf("chat session is not initialized")
		}
		if sess.sessionStore == nil {
			sess.sessionStore = llmselect.NewStore()
		}
		prev := sess.sessionStore.Get()
		output, handled, err := llmselect.ExecuteCommandText(sess.llmValues, sess.sessionStore, text)
		if !handled || err != nil {
			return output, handled, err
		}
		sel := sess.sessionStore.Get()
		if sel == prev {
			return output, true, nil
		}

		newRoute, err := llmselect.ResolveMainRoute(sess.llmValues, sel)
		if err != nil {
			return output, true, fmt.Errorf("resolving route: %w", err)
		}
		newCfg := newRoute.ClientConfig
		newClient, err := sess.buildClient(newRoute, &newCfg)
		if err != nil {
			return output, true, fmt.Errorf("rebuilding client: %w", err)
		}

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
			return output, true, err
		}

		output = strings.TrimSpace(output)
		if output != "" {
			output += "\n"
		}
		output += fmt.Sprintf("\033[33m[active model: %s]\033[0m", newCfg.Model)
		return output, true, nil
	}
}

func chatWorkspaceCommand(sess *chatSession) chatcommands.WorkspaceCommandFunc {
	return func(args string) (string, error) {
		if sess == nil {
			return "", fmt.Errorf("chat session is not initialized")
		}
		cmd, err := workspace.ParseCommandArgs(args)
		if err != nil {
			return err.Error(), nil
		}
		switch cmd.Action {
		case workspace.CommandStatus:
			return workspace.StatusText(sess.workspaceDir), nil
		case workspace.CommandAttach:
			dir, err := workspace.ValidateDir(cmd.Dir, nil)
			if err != nil {
				return "error: " + err.Error(), nil
			}
			oldDir := sess.workspaceDir
			sess.workspaceDir = dir
			sess.refreshProjectScope()
			if err := sess.rebuildRuntimeState(); err != nil {
				sess.workspaceDir = oldDir
				sess.refreshProjectScope()
				_ = sess.rebuildRuntimeState()
				return "", err
			}
			return workspace.AttachText(oldDir, dir, oldDir != ""), nil
		case workspace.CommandDetach:
			oldDir := sess.workspaceDir
			sess.workspaceDir = ""
			sess.refreshProjectScope()
			if err := sess.rebuildRuntimeState(); err != nil {
				sess.workspaceDir = oldDir
				sess.refreshProjectScope()
				_ = sess.rebuildRuntimeState()
				return "", err
			}
			return workspace.DetachText(oldDir, oldDir != ""), nil
		default:
			return "error: unsupported workspace command", nil
		}
	}
}

func chatBuiltinCommandsBlock() string {
	return "## Built-in Chat Commands\n\n" +
		"The user can type these special commands at any time:\n" +
		"- `/exit` or `/quit` — exit the chat session\n" +
		"- `/stop` — stop the current running turn\n" +
		"- `/reset` — reset the current conversation (clear history, keep memory)\n" +
		"- `/memory` — display the current project memory\n" +
		"- `/remember <content>` — add a long-term memory item for the current project\n" +
		"- `/think <task>` — run one task through the think route with xhigh reasoning effort\n" +
		"- `/skills` — show loaded and not loaded skills\n" +
		"- `/models` — inspect or change the current model selection for this session\n" +
		"- `/ctx` — show context-window usage for the current chat topic\n" +
		"- `/workspace` — show the current workspace attachment\n" +
		"- `/workspace attach <dir>` — attach or replace the current workspace directory\n" +
		"- `/workspace detach` — detach the current workspace directory\n" +
		"- `/init` — generate an AGENTS.md file for the current project\n" +
		"- `/update` — regenerate AGENTS.md, overwriting the existing file\n" +
		"If the user asks about any of these commands, explain what they do."
}
