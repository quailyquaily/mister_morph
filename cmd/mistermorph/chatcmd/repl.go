package chatcmd

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/contextcheckpoint"
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
	cancel                context.CancelCauseFunc
	timeoutCancel         context.CancelFunc
	steerQueue            *runtimecontrol.SteerQueue
	prepared              *taskruntime.PreparedEngine
	stopAcknowledged      bool
	runInput              string
	runID                 string
	checkpointStore       agent.ContextCheckpointStore
	userBoundary          string
	contextCompactionOnly bool
}

type pendingChatApproval struct {
	id     string
	record guard.ApprovalRecord
	turn   *activeChatTurn
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

func cancelAndWaitActiveChatTurn(active *activeChatTurn, resultCh <-chan chatTurnResult) {
	if active == nil {
		return
	}
	if active.cancel != nil {
		active.cancel(context.Canceled)
	}
	result := <-resultCh
	if result.turn == nil {
		return
	}
	if result.turn.timeoutCancel != nil {
		result.turn.timeoutCancel()
	}
	if result.turn.cancel != nil {
		result.turn.cancel(nil)
	}
}

func cleanupPreparedChatTurn(sess *chatSession, turn *activeChatTurn) {
	if turn == nil || turn.prepared == nil {
		return
	}
	prepared := turn.prepared
	turn.prepared = nil
	if err := prepared.Cleanup(); err != nil && sess != nil && sess.logger != nil {
		sess.logger.Warn("chat_runtime_client_close_failed", "error", err.Error())
	}
}

func expirePendingChatApproval(ctx context.Context, sess *chatSession, pending *pendingChatApproval, actor, comment string) error {
	if pending == nil {
		return nil
	}
	defer cleanupPreparedChatTurn(sess, pending.turn)
	if sess == nil || sess.taskRuntime == nil || sess.taskRuntime.SharedGuard == nil {
		return errors.New("approvals are unavailable")
	}
	_, _, err := runtimecore.ResolveApprovalCommit(ctx, sess.taskRuntime.SharedGuard, pending.id, guard.ApprovalExpired, actor, comment)
	return err
}

func getChatApproval(ctx context.Context, sess *chatSession, approvalID string) (guard.ApprovalRecord, error) {
	if sess == nil || sess.taskRuntime == nil || sess.taskRuntime.SharedGuard == nil {
		return guard.ApprovalRecord{}, errors.New("approvals are unavailable")
	}
	record, found, err := sess.taskRuntime.SharedGuard.GetApproval(ctx, approvalID)
	if err != nil {
		return guard.ApprovalRecord{}, err
	}
	if !found {
		return guard.ApprovalRecord{}, guard.ErrApprovalNotFound
	}
	return record, nil
}

func startChatTurn(
	sess *chatSession,
	turn *activeChatTurn,
	turnCtx context.Context,
	resultCh chan<- chatTurnResult,
	run func(context.Context) (*agent.Final, *agent.Context, error),
) {
	go func() {
		final, runCtx, err := run(turnCtx)
		if _, waiting := runtimecore.PendingApprovalID(final); err != nil || !waiting {
			cleanupPreparedChatTurn(sess, turn)
		}
		resultCh <- chatTurnResult{
			turn:   turn,
			final:  final,
			runCtx: runCtx,
			err:    err,
			cause:  context.Cause(turnCtx),
		}
	}()
}

func runREPL(sess *chatSession) error {
	model := newChatModel(sess)
	if err := model.loadHistory(); err != nil {
		sess.logger.Warn("chat_history_load_failed", "error", err.Error())
	}

	rootCtx := sess.rootContext
	if rootCtx == nil {
		rootCtx = context.Background()
	}
	p := tea.NewProgram(model, tea.WithInput(sess.cmd.InOrStdin()), tea.WithOutput(sess.cmd.OutOrStdout()), tea.WithContext(rootCtx))

	printChatSessionHeader(sess.cmd.OutOrStdout(), sess.compactMode, strings.TrimSpace(sess.mainCfg.Model), sess.workspaceDir)

	sess.sendMsg = func(msg any) { safeSend(p, msg) }
	sess.setWriter(&programWriter{p: p})

	history := make([]llm.Message, 0, 32)
	historyBoundaries := make([]string, 0, 32)
	reg := newChatRuntimeCommandRegistry(sess)
	registerChatCommands(reg, sess, &history, &historyBoundaries)
	model.commandRegistry = reg

	ctx, cancel := context.WithCancel(rootCtx)
	processorDone := make(chan struct{})

	// Agent turn processing goroutine
	go func() {
		defer close(processorDone)
		turn := 0
		var active *activeChatTurn
		var pending *pendingChatApproval
		resultCh := make(chan chatTurnResult, 1)
		finishCanceledApproval := func(approval *pendingChatApproval, output string) {
			if approval == nil {
				return
			}
			cleanupPreparedChatTurn(sess, approval.turn)
			safeSend(p, approvalClearedMsg{})
			safeSend(p, agentResultMsg{output: output})
			if approval.turn != nil && !approval.turn.contextCompactionOnly {
				history = append(history,
					llm.Message{Role: "user", Content: approval.turn.runInput},
					llm.Message{Role: "assistant", Content: output},
				)
				historyBoundaries = append(historyBoundaries,
					approval.turn.userBoundary,
					"chat:v1:"+approval.turn.runID+":assistant",
				)
			}
			if approval.turn != nil && approval.turn.checkpointStore != nil {
				var loadErr error
				history, historyBoundaries, loadErr = reconcileChatHistoryWithCheckpoint(
					ctx,
					approval.turn.checkpointStore,
					history,
					historyBoundaries,
					"",
				)
				if loadErr != nil {
					sess.logger.Warn("chat_context_checkpoint_load_failed", "error", loadErr.Error())
				}
			}
			turn++
		}
		for {
			select {
			case <-ctx.Done():
				cancelAndWaitActiveChatTurn(active, resultCh)
				cleanupPreparedChatTurn(sess, active)
				if pending != nil {
					if err := expirePendingChatApproval(context.Background(), sess, pending, "chat:session", "chat session closed"); err != nil && !errors.Is(err, guard.ErrApprovalNotPending) && sess.logger != nil {
						sess.logger.Warn("chat_approval_expire_failed", "approval_id", pending.id, "error", err.Error())
					}
				}
				return
			case result := <-resultCh:
				if active == result.turn {
					active = nil
				}
				safeSend(p, thinkingMsg{on: false})
				safeSend(p, sessionStatusMsg{status: chatSessionStatusFromSession(sess)})
				if result.turn != nil {
					if result.turn.timeoutCancel != nil {
						result.turn.timeoutCancel()
					}
					if result.turn.cancel != nil {
						result.turn.cancel(nil)
					}
				}

				if result.err != nil {
					if result.turn != nil && result.turn.checkpointStore != nil {
						var loadErr error
						history, historyBoundaries, loadErr = reconcileChatHistoryWithCheckpoint(
							ctx,
							result.turn.checkpointStore,
							history,
							historyBoundaries,
							result.turn.userBoundary,
						)
						if loadErr != nil {
							sess.logger.Warn("chat_context_checkpoint_load_failed", "error", loadErr.Error())
						}
					}
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
					safeSend(p, agentResultMsg{err: errors.New(displayErr)})
					continue
				}

				if approvalID, ok := runtimecore.PendingApprovalID(result.final); ok {
					record, approvalErr := getChatApproval(ctx, sess, approvalID)
					if approvalErr != nil {
						cleanupPreparedChatTurn(sess, result.turn)
						safeSend(p, agentResultMsg{err: approvalErr})
						continue
					}
					pending = &pendingChatApproval{id: approvalID, record: record, turn: result.turn}
					safeSend(p, approvalMsg{record: record})
					continue
				}

				rawOutput := formatRawChatOutput(result.final)
				displayOutput := formatChatOutput(result.final)
				safeSend(p, agentResultMsg{output: displayOutput})

				runInput := ""
				contextCompactionOnly := false
				if result.turn != nil {
					runInput = result.turn.runInput
					contextCompactionOnly = result.turn.contextCompactionOnly
				}
				if !contextCompactionOnly {
					history = append(history,
						llm.Message{Role: "user", Content: runInput},
						llm.Message{Role: "assistant", Content: rawOutput},
					)
				}
				if result.turn != nil {
					if !contextCompactionOnly {
						historyBoundaries = append(historyBoundaries,
							result.turn.userBoundary,
							"chat:v1:"+result.turn.runID+":assistant",
						)
					}
					if result.turn.checkpointStore != nil {
						var loadErr error
						history, historyBoundaries, loadErr = reconcileChatHistoryWithCheckpoint(
							ctx,
							result.turn.checkpointStore,
							history,
							historyBoundaries,
							"",
						)
						if loadErr != nil {
							sess.logger.Warn("chat_context_checkpoint_load_failed", "error", loadErr.Error())
						}
					}
				}

				if result.runCtx != nil {
					sess.logger.Info("chat_turn_done",
						"turn", turn,
						"steps", len(result.runCtx.Steps),
						"llm_rounds", result.runCtx.Metrics.LLMRounds,
						"total_tokens", result.runCtx.Metrics.TotalTokens,
					)
				}

				turn++
			case input := <-model.submitted:
				input = strings.TrimSpace(input)
				if input == "" {
					continue
				}
				if pending != nil {
					command, _ := chatcommands.ParseCommand(input)
					switch chatcommands.NormalizeCommand(command) {
					case "/exit", "/quit":
						if err := expirePendingChatApproval(ctx, sess, pending, "chat:session", "chat session closed"); err != nil && !errors.Is(err, guard.ErrApprovalNotPending) && sess.logger != nil {
							sess.logger.Warn("chat_approval_expire_failed", "approval_id", pending.id, "error", err.Error())
						}
						pending = nil
						safeSend(p, quitMsg{})
						return
					}

					var approvalGuard *guard.Guard
					if sess.taskRuntime != nil {
						approvalGuard = sess.taskRuntime.SharedGuard
					}
					decision, commitState, approvalErr := resolveChatApprovalInput(
						ctx,
						approvalGuard,
						pending.id,
						input,
						"chat:user",
						time.Now().UTC(),
					)
					if decision == chatApprovalUndecided {
						safeSend(p, agentResultMsg{output: "An approval is pending. Enter /approve or y to continue; /deny or n to cancel."})
						continue
					}
					if approvalErr != nil {
						if commitState != runtimecore.ApprovalCommitPending {
							cleanupPreparedChatTurn(sess, pending.turn)
							pending = nil
							safeSend(p, approvalClearedMsg{})
						}
						safeSend(p, agentResultMsg{err: approvalErr})
						continue
					}
					if decision == chatApprovalExpired {
						approval := pending
						pending = nil
						finishCanceledApproval(approval, formatChatApprovalOutcome("Approval expired", approval.record)+". Task canceled.")
						continue
					}

					approval := pending
					pending = nil
					if decision == chatApprovalDeny {
						finishCanceledApproval(approval, formatChatApprovalOutcome("Approval denied", approval.record)+". Task canceled.")
						continue
					}

					currentTurn := approval.turn
					if currentTurn == nil || currentTurn.prepared == nil || currentTurn.prepared.Engine == nil {
						cleanupPreparedChatTurn(sess, currentTurn)
						safeSend(p, approvalClearedMsg{})
						safeSend(p, agentResultMsg{err: errors.New("approval resume state is unavailable")})
						continue
					}
					stopCtx, stopCancel := context.WithCancelCause(ctx)
					resumeCtx, timeoutCancel := chatTimeoutContext(stopCtx, sess.timeout)
					resumeCtx = pathroots.WithWorkspaceDir(resumeCtx, sess.workspaceDir)
					resumeCtx = llmstats.WithRunID(resumeCtx, currentTurn.runID)
					resumeCtx = topiccontext.WithScope(resumeCtx, topiccontext.Scope{
						Runtime:         "chat",
						ConversationKey: sess.conversationKey(),
						TopicID:         sess.projectID,
					})
					resumeCtx = taskruntime.WithContextCompactionNotification(resumeCtx, sess.logger, func(_ context.Context, _ agent.Event, text string) error {
						safeSend(p, agentResultMsg{output: text, keepThinking: true})
						return nil
					})
					steerQueue := runtimecontrol.NewSteerQueue(0)
					currentTurn.cancel = stopCancel
					currentTurn.timeoutCancel = timeoutCancel
					currentTurn.steerQueue = steerQueue
					currentTurn.stopAcknowledged = false
					active = currentTurn
					safeSend(p, approvalClearedMsg{})
					safeSend(p, thinkingMsg{on: true})
					safeSend(p, agentResultMsg{output: formatChatApprovalOutcome("Approved", approval.record) + ". Resuming task.", keepThinking: true})

					startChatTurn(sess, currentTurn, resumeCtx, resultCh, func(resumeCtx context.Context) (*agent.Final, *agent.Context, error) {
						prepared := currentTurn.prepared
						return prepared.Engine.ResumeWithOptions(resumeCtx, approval.id, agent.RunOptions{
							Model:                  strings.TrimSpace(prepared.Model),
							Scene:                  "chat.loop",
							SteerSource:            steerQueue,
							ContextWindowTokens:    prepared.ContextWindowTokens,
							ContextCheckpointStore: currentTurn.checkpointStore,
							ContextCompactionOnly:  currentTurn.contextCompactionOnly,
						})
					})
					continue
				}
				contextCompactionOnly := chatcommands.IsContextCompactCommand(input)
				if active != nil {
					cmdWord, _ := chatcommands.ParseCommand(input)
					if chatcommands.NormalizeCommand(cmdWord) == "/stop" {
						active.requestStop()
						safeSend(p, agentResultMsg{output: runtimecontrol.StopFeedback(true), keepThinking: true})
						continue
					}
					if contextCompactionOnly {
						safeSend(p, agentResultMsg{output: "Wait for the current turn to finish before running /ctx compact.", keepThinking: true})
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
				if cmd != "" && !contextCompactionOnly {
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
						safeSend(p, sessionStatusMsg{status: chatSessionStatusFromSession(sess)})
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
				runID := llmstats.NewSyntheticRunID("chat")
				checkpointStore, checkpointErr := contextcheckpoint.NewFileStore(sess.contextCheckpointRoot(), sess.conversationKey())
				if checkpointErr != nil {
					safeSend(p, agentResultMsg{err: checkpointErr})
					continue
				}
				checkpoint, found, checkpointErr := checkpointStore.Load(ctx)
				if checkpointErr != nil {
					safeSend(p, agentResultMsg{err: checkpointErr})
					continue
				}
				if found {
					history, historyBoundaries = contextcheckpoint.FilterMessageHistory(history, historyBoundaries, checkpoint.CoveredThrough)
				}
				stopCtx, stopCancel := context.WithCancelCause(ctx)
				turnCtx, timeoutCancel := chatTimeoutContext(stopCtx, sess.timeout)
				turnCtx = pathroots.WithWorkspaceDir(turnCtx, sess.workspaceDir)
				userBoundary := "chat:v1:" + runID + ":user"
				turnCtx = llmstats.WithRunID(turnCtx, runID)
				turnCtx = topiccontext.WithScope(turnCtx, topiccontext.Scope{
					Runtime:         "chat",
					ConversationKey: sess.conversationKey(),
					TopicID:         sess.projectID,
				})
				turnCtx = taskruntime.WithContextCompactionNotification(turnCtx, sess.logger, func(_ context.Context, _ agent.Event, text string) error {
					safeSend(p, agentResultMsg{output: text, keepThinking: true})
					return nil
				})
				prepared, err := sess.prepareRuntimeForTaskRoute(turnCtx, runInput, routePurpose, reasoningEffort, runID)
				if err != nil {
					timeoutCancel()
					stopCancel(nil)
					safeSend(p, agentResultMsg{err: err})
					continue
				}
				safeSend(p, thinkingMsg{on: true})

				steerQueue := runtimecontrol.NewSteerQueue(0)
				active = &activeChatTurn{
					cancel:                stopCancel,
					timeoutCancel:         timeoutCancel,
					steerQueue:            steerQueue,
					prepared:              prepared,
					runInput:              runInput,
					runID:                 runID,
					checkpointStore:       checkpointStore,
					userBoundary:          userBoundary,
					contextCompactionOnly: contextCompactionOnly,
				}
				currentTurn := active
				historySnapshot := append([]llm.Message(nil), history...)
				historyBoundarySnapshot := append([]string(nil), historyBoundaries...)
				startChatTurn(sess, currentTurn, turnCtx, resultCh, func(turnCtx context.Context) (*agent.Final, *agent.Context, error) {
					return prepared.Engine.Run(turnCtx, runInput, agent.RunOptions{
						Model:                  strings.TrimSpace(prepared.Model),
						Scene:                  "chat.loop",
						History:                historySnapshot,
						SteerSource:            steerQueue,
						ContextWindowTokens:    prepared.ContextWindowTokens,
						ContextCheckpointStore: checkpointStore,
						HistoryBoundaries:      historyBoundarySnapshot,
						CurrentMessageBoundary: userBoundary,
						ContextCompactionOnly:  contextCompactionOnly,
					})
				})
			}
		}
	}()

	_, err := p.Run()
	cancel()
	<-processorDone
	return err
}

func reconcileChatHistoryWithCheckpoint(
	ctx context.Context,
	checkpointStore agent.ContextCheckpointStore,
	history []llm.Message,
	boundaries []string,
	clearWhenCoveredThrough string,
) ([]llm.Message, []string, error) {
	if checkpointStore == nil {
		return history, boundaries, nil
	}
	checkpoint, found, err := checkpointStore.Load(ctx)
	if err != nil || !found {
		return history, boundaries, err
	}
	if clearWhenCoveredThrough != "" && checkpoint.CoveredThrough == clearWhenCoveredThrough {
		return nil, nil, nil
	}
	filteredHistory, filteredBoundaries := contextcheckpoint.FilterMessageHistory(history, boundaries, checkpoint.CoveredThrough)
	return filteredHistory, filteredBoundaries, nil
}
