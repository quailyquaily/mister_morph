package chatcmd

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/llm"
)

// programWriter buffers output by line and forwards each complete line to the
// bubbletea program as a tuiOutputMsg. This lets engine callbacks write through
// the standard io.Writer interface while bubbletea owns the terminal.
type programWriter struct {
	p      *tea.Program
	mu     sync.Mutex
	buffer strings.Builder
}

func (w *programWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buffer.Write(p)
	data := w.buffer.String()

	for {
		idx := strings.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := data[:idx] // exclude newline; tea.Println adds its own
		func() {
			defer func() { recover() }() // guard against closed program
			w.p.Send(tuiOutputMsg{output: line})
		}()
		data = data[idx+1:]
	}

	w.buffer.Reset()
	w.buffer.WriteString(data)
	return len(p), nil
}

func safeSend(p *tea.Program, msg tea.Msg) {
	defer func() { recover() }()
	p.Send(msg)
}

func runREPL(sess *chatSession) error {
	model := newChatModel(sess)
	if err := model.loadHistory(); err != nil {
		sess.logger.Warn("chat_history_load_failed", "error", err.Error())
	}

	p := tea.NewProgram(model)

	printChatSessionHeader(sess.cmd.OutOrStdout(), sess.compactMode, strings.TrimSpace(sess.mainCfg.Model), sess.workspaceDir, sess.fileCacheDir)

	sess.sendMsg = func(msg any) { safeSend(p, msg) }
	sess.setWriter(&programWriter{p: p})

	reg := chatcommands.NewRegistry()
	history := make([]llm.Message, 0, 32)
	registerChatCommands(reg, sess, &history)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Agent turn processing goroutine
	go func() {
		turn := 0
		for {
			select {
			case <-ctx.Done():
				return
			case input := <-model.submitted:
				input = strings.TrimSpace(input)
				if input == "" {
					continue
				}

				// Try dispatching as a slash command
				cmd, _ := chatcommands.ParseCommand(input)
				if cmd != "" {
					result, handled, err := reg.Dispatch(ctx, input)
					if err != nil {
						safeSend(p,agentResultMsg{err: err})
						continue
					}
					if handled {
						if result != nil && result.Quit {
							safeSend(p,quitMsg{})
							return
						}
						if result != nil && result.Reply != "" {
							safeSend(p,agentResultMsg{output: result.Reply})
						}
						continue
					}
				}

				// Not a command — run an agent turn
				safeSend(p,thinkingMsg{on: true})

				turnCtx, turnCancel := context.WithCancel(ctx)
				turnCtx = pathroots.WithWorkspaceDir(turnCtx, sess.workspaceDir)
				go func() {
					<-time.After(sess.timeout)
					turnCancel()
				}()
				runID := llmstats.NewSyntheticRunID("chat")
				turnCtx = llmstats.WithRunID(turnCtx, runID)

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

				safeSend(p,thinkingMsg{on: false})
				turnCancel()

				if err != nil {
					if errors.Is(err, context.Canceled) {
						safeSend(p,agentResultMsg{err: err})
						continue
					}
					displayErr := strings.TrimSpace(outputfmt.FormatErrorForDisplay(err))
					if displayErr == "" {
						displayErr = strings.TrimSpace(err.Error())
					}
					safeSend(p,agentResultMsg{output: displayErr})
					continue
				}

				rawOutput := formatRawChatOutput(final)
				displayOutput := formatChatOutput(final)
				safeSend(p, agentResultMsg{output: displayOutput})

				history = append(history,
					llm.Message{Role: "user", Content: input},
					llm.Message{Role: "assistant", Content: rawOutput},
				)

				sess.logger.Info("chat_turn_done",
					"turn", turn,
					"steps", len(runCtx.Steps),
					"llm_rounds", runCtx.Metrics.LLMRounds,
					"total_tokens", runCtx.Metrics.TotalTokens,
				)

				autoUpdateMemory(io.Discard, sess.logger, sess.memOrchestrator, sess.memWorker, sess.subjectID, runID, input, rawOutput, runCtx.Steps)
				turn++
			}
		}
	}()

	_, err := p.Run()
	cancel()
	return err
}
