package chatcmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/contextcheckpoint"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
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
			return sess.topicContextStore.RenderCommandText(sess.conversationKey())
		},
		WorkspaceCommand: chatWorkspaceCommand(sess),
	})
}

// registerChatCommands binds all slash commands into the given registry.
// Each handler receives the mutable session so it can update client/engine state
// when necessary (e.g. /models).
func registerChatCommands(reg *chatcommands.Registry, sess *chatSession, history *[]llm.Message, historyBoundaries *[]string) {
	writer := sess.writer
	runAgentsCommand := func(ctx context.Context, input, activity, projectDir string) (*chatcommands.Result, error) {
		commandCtx, finish := sess.beginForegroundCommand(ctx)
		defer finish()
		sess.setActivity(activity, false)
		defer sess.clearActivity()
		prepared, err := prepareChatCommandRuntime(commandCtx, sess, input)
		if err != nil {
			return nil, err
		}
		defer func() {
			if closeErr := prepared.Cleanup(); closeErr != nil && sess.logger != nil {
				sess.logger.Warn("chat_runtime_client_close_failed", "error", closeErr.Error())
			}
		}()
		newHistory, ok := handleAgentsGenerate(commandCtx, writer, input, projectDir, sess.timeout, prepared.Engine, prepared.Model, *history)
		if ok {
			replaceChatHistory(history, historyBoundaries, newHistory)
		}
		return &chatcommands.Result{}, nil
	}

	reg.Register("/status", "show full session details", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		output, err := formatChatStatus(sess)
		if err != nil {
			return nil, err
		}
		return &chatcommands.Result{Reply: output}, nil
	})

	reg.Register("/approve", "approve the pending action", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		return &chatcommands.Result{Reply: "No approval is pending."}, nil
	})

	reg.Register("/deny", "deny the pending action", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		return &chatcommands.Result{Reply: "No approval is pending."}, nil
	})

	reg.Register("/exit", "exit the chat session", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		return &chatcommands.Result{Quit: true}, nil
	})

	reg.Register("/quit", "exit the chat session", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		return &chatcommands.Result{Quit: true}, nil
	})

	reg.Register("/reset", "reset the conversation", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		if err := contextcheckpoint.Reset(ctx, sess.contextCheckpointRoot(), sess.conversationKey()); err != nil {
			return nil, fmt.Errorf("reset context checkpoint: %w", err)
		}
		*history = nil
		if historyBoundaries != nil {
			*historyBoundaries = nil
		}
		return &chatcommands.Result{Reply: "Session reset."}, nil
	})

	reg.Register("/stop", "stop the current turn", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		return &chatcommands.Result{Reply: runtimecontrol.StopFeedback(false)}, nil
	})

	reg.Register("/init", "create AGENTS.md for this project", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		projectDir := sess.projectDir()
		agentsPath := filepath.Join(projectDir, "AGENTS.md")
		if handleInitRead(writer, agentsPath) {
			return &chatcommands.Result{}, nil
		}
		return runAgentsCommand(ctx, "/init", "generating AGENTS.md", projectDir)
	})

	reg.Register("/update", "regenerate this project's AGENTS.md", func(ctx context.Context, args string) (*chatcommands.Result, error) {
		return runAgentsCommand(ctx, "/update", "updating AGENTS.md", sess.projectDir())
	})
}

func prepareChatCommandRuntime(ctx context.Context, sess *chatSession, task string) (*taskruntime.PreparedEngine, error) {
	if sess == nil {
		return nil, fmt.Errorf("chat session is not initialized")
	}
	return sess.prepareRuntimeForTaskRoute(ctx, task, llmutil.RoutePurposeMainLoop, "", llmstats.NewSyntheticRunID("chat-command"))
}

func replaceChatHistory(history *[]llm.Message, boundaries *[]string, next []llm.Message) {
	if history == nil {
		return
	}
	previousBoundaries := []string(nil)
	if boundaries != nil {
		previousBoundaries = append(previousBoundaries, (*boundaries)...)
	}
	*history = next
	if boundaries == nil {
		return
	}
	nextBoundaries := make([]string, len(next))
	copy(nextBoundaries, previousBoundaries)
	boundaryRunID := llmstats.NewSyntheticRunID("chat-history")
	for index := range nextBoundaries {
		if strings.TrimSpace(nextBoundaries[index]) == "" {
			nextBoundaries[index] = fmt.Sprintf("chat:v1:%s:%d", boundaryRunID, index)
		}
	}
	*boundaries = nextBoundaries
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

		previousOverridesEnabled := sess.clientOverridesEnabled
		sess.clientOverridesEnabled = false
		ctx, cancel := chatTimeoutContext(sess.rootContext, sess.timeout)
		err = sess.rebuildRuntimeState(ctx)
		cancel()
		if err != nil {
			restoreChatModelSelection(sess.sessionStore, prev)
			sess.clientOverridesEnabled = previousOverridesEnabled
			return output, true, err
		}

		output = strings.TrimSpace(output)
		if output != "" {
			output += "\n"
		}
		output += fmt.Sprintf("\033[33m[active model: %s]\033[0m", sess.mainCfg.Model)
		return output, true, nil
	}
}

func restoreChatModelSelection(store *llmselect.Store, selection llmselect.MainSelection) {
	if store == nil {
		return
	}
	selection = llmselect.NormalizeSelection(selection)
	if selection.Mode == llmselect.ModeManual {
		store.SetProfile(selection.ManualProfile)
		return
	}
	store.Reset()
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
			ctx, cancel := chatTimeoutContext(sess.rootContext, sess.timeout)
			err = sess.rebuildRuntimeState(ctx)
			cancel()
			if err != nil {
				sess.workspaceDir = oldDir
				sess.refreshProjectScope()
				return "", err
			}
			return workspace.AttachText(oldDir, dir, oldDir != ""), nil
		case workspace.CommandDetach:
			oldDir := sess.workspaceDir
			sess.workspaceDir = sess.defaultWorkspaceDir
			sess.refreshProjectScope()
			ctx, cancel := chatTimeoutContext(sess.rootContext, sess.timeout)
			err := sess.rebuildRuntimeState(ctx)
			cancel()
			if err != nil {
				sess.workspaceDir = oldDir
				sess.refreshProjectScope()
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
		"- `/reset` — reset the current conversation\n" +
		"- `/think <task>` — run one task through the think route with xhigh reasoning effort\n" +
		"- `/skills` — show loaded and not loaded skills\n" +
		"- `/models` — inspect or change the current model selection for this session\n" +
		"- `/ctx` — show context-window usage for the current chat topic\n" +
		"- `/ctx compact` — compact older conversation context into a checkpoint now\n" +
		"- `/status` — show full session paths, model, context usage, and version\n" +
		"- `/workspace` — show the current workspace attachment\n" +
		"- `/workspace attach <dir>` — attach or replace the current workspace directory\n" +
		"- `/workspace detach` — detach the current workspace directory\n" +
		"- `/init` — generate an AGENTS.md file for the current project\n" +
		"- `/update` — regenerate AGENTS.md, overwriting the existing file\n" +
		"If the user asks about any of these commands, explain what they do."
}

func formatChatStatus(sess *chatSession) (string, error) {
	if sess == nil {
		return "", fmt.Errorf("chat session is not initialized")
	}
	contextUsage := "unknown"
	if sess.topicContextStore != nil {
		item, found, err := sess.topicContextStore.Get(sess.conversationKey())
		if err != nil {
			return "", fmt.Errorf("read context usage: %w", err)
		}
		if found && item.ContextWindowTokens > 0 {
			contextUsage = fmt.Sprintf("%.1f%%", item.UsageRatio*100)
		}
	}
	lines := []string{
		"Chat status",
		"Model: " + strings.TrimSpace(sess.mainCfg.Model),
		"Workspace: " + strings.TrimSpace(sess.workspaceDir),
		"File state: " + strings.TrimSpace(sess.fileStateDir),
		"File cache: " + strings.TrimSpace(sess.fileCacheDir),
		"Context: " + contextUsage,
	}
	if version := strings.TrimSpace(sess.version); version != "" {
		lines = append(lines, "Version: "+version)
	}
	return strings.Join(lines, "\n"), nil
}
