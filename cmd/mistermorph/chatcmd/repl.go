package chatcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/llm"
)

func runREPL(sess *chatSession) error {
	userPrompt := buildUserPrompt(sess.compactMode, sess.userName)

	autoComplete := readline.NewPrefixCompleter(
		readline.PcItem("/exit"),
		readline.PcItem("/quit"),
		readline.PcItem("/reset"),
		readline.PcItem("/memory"),
		readline.PcItem("/remember "),
		readline.PcItem("/init"),
		readline.PcItem("/update"),
		readline.PcItem("/model"),
		readline.PcItem("/workspace"),
		readline.PcItem("/workspace attach "),
		readline.PcItem("/workspace detach"),
		readline.PcItem("/help"),
	)

	rl, err := readline.NewEx(&readline.Config{
		Prompt:       userPrompt,
		HistoryFile:  filepath.Join(os.Getenv("HOME"), ".mistermorph_chat_history"),
		AutoComplete: autoComplete,
		Stdout:       sess.cmd.OutOrStdout(),
		Stderr:       sess.cmd.OutOrStderr(),
	})
	if err != nil {
		return err
	}
	defer rl.Close()

	sess.setWriter(rl.Stdout())
	writer := sess.currentWriter()

	printChatSessionHeader(writer, strings.TrimSpace(sess.mainCfg.Model), sess.workspaceDir, sess.fileCacheDir)

	reg := chatcommands.NewRegistry()
	history := make([]llm.Message, 0, 32)
	registerChatCommands(reg, sess, &history)

	turn := 0
	for {
		input, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				if len(input) == 0 {
					_, _ = fmt.Fprintln(writer, "\nBye! 👋")
					return nil
				}
				continue
			}
			if err == io.EOF {
				_, _ = fmt.Fprintln(writer)
				return nil
			}
			return err
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Try dispatching as a slash command
		cmd, _ := chatcommands.ParseCommand(input)
		if cmd != "" {
			result, handled, err := reg.Dispatch(context.Background(), input)
			if err != nil {
				_, _ = fmt.Fprintf(writer, "error: %v\n", err)
				continue
			}
			if handled {
				if result != nil && result.Quit {
					return nil
				}
				if result != nil && result.Reply != "" {
					_, _ = fmt.Fprintln(writer, result.Reply)
				}
				continue
			}
		}

		// Not a command — run an agent turn
		turnCtx, turnCancel := context.WithCancel(context.Background())
		turnCtx = pathroots.WithWorkspaceDir(turnCtx, sess.workspaceDir)
		go func() {
			<-time.After(sess.timeout)
			turnCancel()
		}()
		runID := llmstats.NewSyntheticRunID("chat")
		turnCtx = llmstats.WithRunID(turnCtx, runID)
		sess.startThinkingAnimation()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		go func() {
			select {
			case <-sigCh:
				turnCancel()
			case <-turnCtx.Done():
			}
			signal.Stop(sigCh)
		}()

		memoryContext, memErr := prepareTurnMemoryContext(sess.memOrchestrator, sess.subjectID)
		if memErr != nil {
			sess.logger.Warn("chat_memory_injection_failed", "error", memErr.Error())
		}

		final, runCtx, err := sess.engine.Run(turnCtx, input, agent.RunOptions{
			Model:         strings.TrimSpace(sess.mainCfg.Model),
			Scene:         "chat.interactive",
			History:       append([]llm.Message(nil), history...),
			MemoryContext: memoryContext,
		})

		sess.stopThinkingAnimation()
		turnCancel()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				_, _ = fmt.Fprintln(writer, "\n\033[33m⚡ Interrupted.\033[0m")
				continue
			}
			displayErr := strings.TrimSpace(outputfmt.FormatErrorForDisplay(err))
			if displayErr == "" {
				displayErr = strings.TrimSpace(err.Error())
			}
			_, _ = fmt.Fprintf(writer, "error: %s\n", displayErr)
			continue
		}

		output := formatChatOutput(final)
		if sess.compactMode {
			_, _ = fmt.Fprintf(writer, "%s\n", output)
		} else {
			_, _ = fmt.Fprintf(writer, "\033[48;5;208m\033[30m %s> \033[0m %s\n", sess.agentName, output)
		}

		history = append(history,
			llm.Message{Role: "user", Content: input},
			llm.Message{Role: "assistant", Content: output},
		)

		sess.logger.Info("chat_turn_done",
			"turn", turn,
			"steps", len(runCtx.Steps),
			"llm_rounds", runCtx.Metrics.LLMRounds,
			"total_tokens", runCtx.Metrics.TotalTokens,
		)

		// Auto-update memory if there were tool calls
		autoUpdateMemory(writer, sess.logger, sess.memOrchestrator, sess.memWorker, sess.subjectID, runID, input, output, runCtx.Steps)

		turn++
	}
}
