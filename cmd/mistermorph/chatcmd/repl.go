package chatcmd

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
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

type activeChatTurn struct {
	cancel           context.CancelCauseFunc
	timeoutCancel    context.CancelFunc
	signalCh         chan os.Signal
	steerQueue       *runtimecontrol.SteerQueue
	stopAcknowledged bool
	runInput         string
	runID            string
	oldState         *chatRuntimeState
	temporaryClient  llm.Client
}

type chatTurnResult struct {
	turn   *activeChatTurn
	final  *agent.Final
	runCtx *agent.Context
	err    error
	cause  error
}

func shouldSendChatStopFeedback(result chatTurnResult) bool {
	if !errors.Is(result.cause, runtimecontrol.ErrStoppedByUser) {
		return false
	}
	return result.turn == nil || !result.turn.stopAcknowledged
}

func (t *activeChatTurn) requestStop() {
	if t == nil {
		return
	}
	t.stopAcknowledged = true
	if t.steerQueue != nil {
		t.steerQueue.Close()
	}
	if t.cancel != nil {
		t.cancel(runtimecontrol.ErrStoppedByUser)
	}
}

func runREPL(sess *chatSession) error {
	model := newChatModel(sess)
	if err := model.loadHistory(); err != nil {
		sess.logger.Warn("chat_history_load_failed", "error", err.Error())
	}

	p := tea.NewProgram(model, tea.WithInput(sess.cmd.InOrStdin()), tea.WithOutput(sess.cmd.OutOrStdout()))

	printChatSessionHeader(sess.cmd.OutOrStdout(), sess.compactMode, strings.TrimSpace(sess.mainCfg.Model), sess.workspaceDir, sess.fileCacheDir)

	sess.sendMsg = func(msg any) { safeSend(p, msg) }
	sess.setWriter(&programWriter{p: p})

	history := make([]llm.Message, 0, 32)
	reg := newChatRuntimeCommandRegistry(sess)
	registerChatCommands(reg, sess, &history)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Agent turn processing goroutine
	go func() {
		turn := 0
		var active *activeChatTurn
		resultCh := make(chan chatTurnResult, 1)
		for {
			select {
			case <-ctx.Done():
				if active != nil && active.cancel != nil {
					active.cancel(context.Canceled)
				}
				return
			case result := <-resultCh:
				if active == result.turn {
					active = nil
				}
				safeSend(p, thinkingMsg{on: false})
				if result.turn != nil {
					if result.turn.signalCh != nil {
						signal.Stop(result.turn.signalCh)
					}
					if result.turn.timeoutCancel != nil {
						result.turn.timeoutCancel()
					}
					if result.turn.cancel != nil {
						result.turn.cancel(nil)
					}
					if result.turn.oldState != nil {
						restoreChatRuntimeState(sess, result.turn.oldState, result.turn.temporaryClient)
					}
				}

				if result.err != nil {
					if errors.Is(result.cause, runtimecontrol.ErrStoppedByUser) {
						if shouldSendChatStopFeedback(result) {
							safeSend(p, agentResultMsg{output: runtimecontrol.StopFeedback(true)})
						}
						continue
					}
					if errors.Is(result.err, context.Canceled) {
						safeSend(p, agentResultMsg{err: result.err})
						continue
					}
					displayErr := strings.TrimSpace(outputfmt.FormatErrorForDisplay(result.err))
					if displayErr == "" {
						displayErr = strings.TrimSpace(result.err.Error())
					}
					safeSend(p, agentResultMsg{output: displayErr})
					continue
				}

				rawOutput := formatRawChatOutput(result.final)
				displayOutput := formatChatOutput(result.final)
				safeSend(p, agentResultMsg{output: displayOutput})

				runInput := ""
				runID := ""
				if result.turn != nil {
					runInput = result.turn.runInput
					runID = result.turn.runID
				}
				history = append(history,
					llm.Message{Role: "user", Content: runInput},
					llm.Message{Role: "assistant", Content: rawOutput},
				)

				steps := []agent.Step(nil)
				if result.runCtx != nil {
					steps = result.runCtx.Steps
					sess.logger.Info("chat_turn_done",
						"turn", turn,
						"steps", len(result.runCtx.Steps),
						"llm_rounds", result.runCtx.Metrics.LLMRounds,
						"total_tokens", result.runCtx.Metrics.TotalTokens,
					)
				}

				autoUpdateMemory(io.Discard, sess.logger, sess.memOrchestrator, sess.memWorker, sess.subjectID, runID, runInput, rawOutput, steps)
				turn++
			case input := <-model.submitted:
				input = strings.TrimSpace(input)
				if input == "" {
					continue
				}
				if active != nil {
					cmdWord, _ := chatcommands.ParseCommand(input)
					if chatcommands.NormalizeCommand(cmdWord) == "/stop" {
						active.requestStop()
						safeSend(p, agentResultMsg{output: runtimecontrol.StopFeedback(true), keepThinking: true})
						continue
					}
					if active.stopAcknowledged || active.steerQueue == nil {
						safeSend(p, agentResultMsg{output: runtimecontrol.SteerFeedback(true, false), keepThinking: true})
						continue
					}
					if _, err := active.steerQueue.Push(input); err != nil {
						safeSend(p, agentResultMsg{err: err, keepThinking: true})
						continue
					}
					safeSend(p, agentResultMsg{output: runtimecontrol.SteerFeedback(true, true), keepThinking: true})
					continue
				}

				// Try dispatching as a slash command
				cmd, _ := chatcommands.ParseCommand(input)
				if cmd != "" {
					result, handled, err := reg.Dispatch(ctx, input)
					if err != nil {
						safeSend(p, agentResultMsg{err: err})
						continue
					}
					if handled {
						if result != nil && result.Quit {
							safeSend(p, quitMsg{})
							return
						}
						if result != nil && result.Reply != "" {
							safeSend(p, agentResultMsg{output: result.Reply})
						}
						continue
					}
				}

				// Not a command — run an agent turn
				runInput := input
				routePurpose := ""
				reasoningEffort := ""
				if thinkTask, ok := chatcommands.ExtractThinkTask(input); ok {
					runInput = strings.TrimSpace(thinkTask)
					routePurpose = llmutil.RoutePurposeThink
					reasoningEffort = llmutil.ReasoningEffortXHigh
					if runInput == "" {
						safeSend(p, agentResultMsg{err: errors.New("empty task")})
						continue
					}
				}
				var oldState *chatRuntimeState
				if routePurpose == llmutil.RoutePurposeThink {
					oldState = captureChatRuntimeState(sess)
				}
				if err := sess.rebuildRuntimeStateForTaskRoute(runInput, routePurpose, reasoningEffort); err != nil {
					if oldState != nil {
						restoreChatRuntimeState(sess, oldState, nil)
					}
					safeSend(p, agentResultMsg{err: err})
					continue
				}
				var temporaryClient llm.Client
				if oldState != nil {
					temporaryClient = sess.client
				}
				safeSend(p, thinkingMsg{on: true})

				stopCtx, stopCancel := context.WithCancelCause(ctx)
				turnCtx, timeoutCancel := context.WithTimeout(stopCtx, sess.timeout)
				turnCtx = pathroots.WithWorkspaceDir(turnCtx, sess.workspaceDir)
				runID := llmstats.NewSyntheticRunID("chat")
				turnCtx = llmstats.WithRunID(turnCtx, runID)
				turnCtx = topiccontext.WithScope(turnCtx, topiccontext.Scope{
					Runtime:         "chat",
					ConversationKey: sess.conversationKey(),
					TopicID:         sess.subjectID,
				})

				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, os.Interrupt)
				steerQueue := runtimecontrol.NewSteerQueue(0)
				active = &activeChatTurn{
					cancel:          stopCancel,
					timeoutCancel:   timeoutCancel,
					signalCh:        sigCh,
					steerQueue:      steerQueue,
					runInput:        runInput,
					runID:           runID,
					oldState:        oldState,
					temporaryClient: temporaryClient,
				}
				go func() {
					select {
					case <-sigCh:
						stopCancel(runtimecontrol.ErrStoppedByUser)
					case <-turnCtx.Done():
					}
				}()

				memoryContext, memErr := prepareTurnMemoryContext(sess.memOrchestrator, sess.subjectID)
				if memErr != nil {
					sess.logger.Warn("chat_memory_injection_failed", "error", memErr.Error())
				}

				currentTurn := active
				historySnapshot := append([]llm.Message(nil), history...)
				go func() {
					final, runCtx, err := sess.engine.Run(turnCtx, runInput, agent.RunOptions{
						Model:         strings.TrimSpace(sess.mainCfg.Model),
						Scene:         "chat.loop",
						History:       historySnapshot,
						MemoryContext: memoryContext,
						SteerSource:   steerQueue,
					})
					resultCh <- chatTurnResult{
						turn:   currentTurn,
						final:  final,
						runCtx: runCtx,
						err:    err,
						cause:  context.Cause(turnCtx),
					}
				}()
			}
		}
	}()

	_, err := p.Run()
	cancel()
	return err
}
