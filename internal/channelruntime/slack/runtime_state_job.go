package slack

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imagehistory"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
)

func (s *slackRuntimeState) runJob(workerCtx context.Context, conversationKey string, job slackJob) {
	retainGeneration := false
	defer func() {
		if !retainGeneration {
			job.releaseGeneration()
		}
	}()
	if workerCtx.Err() != nil {
		s.finalizeRuntimeClosedJob(conversationKey, job)
		return
	}
	runtimeBundle := job.runtimeBundle(&s.runtimeBundle)
	if runtimeBundle == nil || runtimeBundle.TaskRuntime == nil {
		s.finalizeAcceptedTask(job.TaskID, daemonruntime.TaskFailed, "slack runtime generation is unavailable")
		return
	}
	historyScopeKey := slackHistoryScopeKeyForJob(job)
	if historyScopeKey == "" {
		historyScopeKey = conversationKey
	}
	s.mu.Lock()
	history := append([]chathistory.ChatHistoryItem(nil), s.history[historyScopeKey]...)
	stickySkills := append([]string(nil), s.stickySkillsByConv[historyScopeKey]...)
	s.mu.Unlock()
	currentVersion := s.runner.CurrentVersion(conversationKey)
	if job.Version != currentVersion {
		history = nil
	}
	if err := runtimecore.MarkTaskRunning(s.taskStore, job.TaskID); err != nil {
		s.logger.Error("slack_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskRunning, "error", err.Error())
		return
	}
	defer func() {
		if workerCtx.Err() != nil {
			s.finalizeRuntimeClosedJob(conversationKey, job)
		}
	}()
	workingMessage := startSlackWorkingMessageWithDelay(workerCtx, s.logger, s.api, job, slackWorkingMessageDelay)
	planUpdateHook := func(runCtx *agent.Context, _ agent.PlanStepUpdate) {
		if workingMessage == nil || runCtx == nil || runCtx.Plan == nil {
			return
		}
		planText := renderSlackPlanProgressText(runCtx.Plan)
		text, blocks := buildSlackPlanProgressBlocks(runCtx.Plan, true)
		if strings.TrimSpace(text) == "" || len(blocks) == 0 {
			return
		}
		updated, err := workingMessage.UpdateBlocks(workerCtx, text, blocks)
		if err != nil {
			s.logger.Warn("slack_plan_progress_update_error", "channel_id", job.ChannelID, "message_ts", job.MessageTS, "error", err.Error())
			callErrorHook(workerCtx, s.logger, s.options.Hooks, ErrorEvent{
				Stage:           ErrorStagePublishOutbound,
				ConversationKey: job.ConversationKey,
				TeamID:          job.TeamID,
				ChannelID:       job.ChannelID,
				MessageTS:       job.MessageTS,
				Err:             err,
			})
			return
		}
		if updated {
			callSlackDirectOutboundHook(workerCtx, s.logger, s.options.Hooks, job, planText, fmt.Sprintf("slack:plan:%s:%s", job.ChannelID, job.MessageTS))
		}
	}
	runControlKey := slackRunControlConversationKeyForJob(job)
	if runControlKey == "" {
		runControlKey = conversationKey
	}
	lease, err := s.runControl.StartLease(workerCtx, s.taskTimeout, runtimecontrol.ActiveRun{
		Runtime:         "slack",
		ConversationKey: runControlKey,
		TopicID:         slackContextTopicID(job),
		TaskID:          job.TaskID,
		RunID:           job.TaskID,
	})
	if err != nil {
		if stateErr := runtimecore.MarkTaskFailed(s.taskStore, job.TaskID, strings.TrimSpace(err.Error()), false); stateErr != nil {
			s.logger.Error("slack_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskFailed, "error", stateErr.Error())
		}
		return
	}
	runCtx := taskruntime.WithContextCompactionNotification(lease.Context, s.logger, func(notifyCtx context.Context, event agent.Event, text string) error {
		correlationID := fmt.Sprintf("slack:context-compaction:%s:%d", job.TaskID, event.Step)
		_, notifyErr := publishSlackBusOutbound(notifyCtx, s.inprocBus, job.TeamID, job.ChannelID, text, job.ThreadTS, correlationID)
		return notifyErr
	})
	runtimeOpts := runtimeTaskOptions{
		FileCacheDir: s.options.FileCacheDir,
	}
	final, agentCtx, loadedSkills, reaction, runErr := runSlackTask(
		runCtx,
		runtimeBundle.TaskRuntime,
		s.api,
		job,
		history,
		stickySkills,
		s.allowedChannels,
		s.availableEmojiNames,
		s.fileCacheDir,
		runtimeOpts,
		lease.SteerQueue,
		planUpdateHook,
	)
	userStopped := lease.UserStopped()
	lease.Finish()

	if workerCtx.Err() != nil {
		return
	}
	planPreserved := finalizeSlackPlanProgressMessage(workerCtx, s.logger, s.options.Hooks, job, workingMessage, agentCtx)
	if runErr != nil {
		displayErr := depsutil.FormatRuntimeError(runErr)
		if userStopped {
			displayErr = "stopped by user"
		}
		if stateErr := runtimecore.MarkTaskFailed(s.taskStore, job.TaskID, displayErr, isSlackTaskContextCanceled(runErr) || userStopped); stateErr != nil {
			s.logger.Error("slack_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskFailed, "error", stateErr.Error())
		}
		callErrorHook(workerCtx, s.logger, s.options.Hooks, ErrorEvent{
			Stage:           ErrorStageRunTask,
			ConversationKey: job.ConversationKey,
			TeamID:          job.TeamID,
			ChannelID:       job.ChannelID,
			MessageTS:       job.MessageTS,
			Err:             runErr,
		})
		if userStopped {
			if !planPreserved {
				if _, updateErr := workingMessage.Update(workerCtx, slackDoneMessageText); updateErr != nil {
					s.logger.Warn("slack_working_message_update_error", "channel", busruntime.ChannelSlack, "channel_id", job.ChannelID, "message_ts", job.MessageTS, "error", updateErr.Error())
				}
			}
			return
		}
		errorText := "error: " + displayErr
		errorCorrelationID := fmt.Sprintf("slack:error:%s:%s", job.ChannelID, job.MessageTS)
		if !planPreserved {
			if updated, updateErr := workingMessage.Update(workerCtx, errorText); updated {
				if updateErr == nil {
					callSlackDirectOutboundHook(workerCtx, s.logger, s.options.Hooks, job, errorText, errorCorrelationID)
					return
				}
				s.logger.Warn("slack_working_message_update_error", "channel", busruntime.ChannelSlack, "channel_id", job.ChannelID, "message_ts", job.MessageTS, "error", updateErr.Error())
				callErrorHook(workerCtx, s.logger, s.options.Hooks, ErrorEvent{
					Stage:           ErrorStagePublishErrorReply,
					ConversationKey: job.ConversationKey,
					TeamID:          job.TeamID,
					ChannelID:       job.ChannelID,
					MessageTS:       job.MessageTS,
					Err:             updateErr,
				})
			}
		}
		_, err := publishSlackBusOutbound(
			workerCtx,
			s.inprocBus,
			job.TeamID,
			job.ChannelID,
			errorText,
			job.ThreadTS,
			errorCorrelationID,
		)
		if err != nil {
			s.logger.Warn("slack_bus_publish_error", "channel", busruntime.ChannelSlack, "channel_id", job.ChannelID, "bus_error_code", string(busruntime.ErrorCodeOf(err)), "error", err.Error())
			callErrorHook(workerCtx, s.logger, s.options.Hooks, ErrorEvent{
				Stage:           ErrorStagePublishErrorReply,
				ConversationKey: job.ConversationKey,
				TeamID:          job.TeamID,
				ChannelID:       job.ChannelID,
				MessageTS:       job.MessageTS,
				Err:             err,
			})
		}
		return
	}

	if pendingID, ok := runtimecore.PendingApprovalID(final); ok {
		pendingAt := time.Now().UTC()
		if s.taskStore != nil {
			if err := s.taskStore.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
				info.Status = daemonruntime.TaskPending
				info.PendingAt = &pendingAt
				info.ApprovalRequestID = pendingID
				info.Result = map[string]any{
					"source": "slack",
					"final":  final,
				}
			}); err != nil {
				s.logger.Error("slack_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskPending, "error", err.Error())
				return
			}
		}
		if err := s.registerPendingApproval(pendingID, job); err != nil {
			applied, stateErr := runtimecore.FailPendingApprovalTask(s.taskStore, job.TaskID, pendingID, runtimecore.ApprovalRegistrationFailedTaskError)
			if stateErr != nil {
				err = errors.Join(err, stateErr)
			}
			s.logger.Error("slack_approval_register_error", "approval_request_id", pendingID, "task_id", job.TaskID, "task_failed", applied, "error", err.Error())
			return
		}
		if !planPreserved {
			if updated, updateErr := workingMessage.Update(workerCtx, "approval required."); updated && updateErr != nil {
				s.logger.Warn("slack_working_message_update_error", "channel", busruntime.ChannelSlack, "channel_id", job.ChannelID, "message_ts", job.MessageTS, "error", updateErr.Error())
			}
		}
		if err := s.notifyPendingApproval(context.Background(), pendingID, job); err != nil {
			s.logger.Warn("slack_approval_notify_error", "approval_request_id", pendingID, "channel_id", job.ChannelID, "error", err.Error())
		}
		retainGeneration = true
		return
	}

	outText := strings.TrimSpace(outputfmt.FormatFinalOutput(final))
	if err := runtimecore.MarkTaskDone(s.taskStore, job.TaskID, outText); err != nil {
		s.logger.Error("slack_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskDone, "error", err.Error())
		return
	}
	if outText != "" {
		outCorrelationID := fmt.Sprintf("slack:message:%s:%s", job.ChannelID, job.MessageTS)
		deliveredByUpdate := false
		if !planPreserved {
			if updated, updateErr := workingMessage.Update(workerCtx, outText); updated {
				if updateErr == nil {
					callSlackDirectOutboundHook(workerCtx, s.logger, s.options.Hooks, job, outText, outCorrelationID)
					deliveredByUpdate = true
				} else {
					s.logger.Warn("slack_working_message_update_error", "channel", busruntime.ChannelSlack, "channel_id", job.ChannelID, "message_ts", job.MessageTS, "error", updateErr.Error())
					callErrorHook(workerCtx, s.logger, s.options.Hooks, ErrorEvent{
						Stage:           ErrorStagePublishOutbound,
						ConversationKey: job.ConversationKey,
						TeamID:          job.TeamID,
						ChannelID:       job.ChannelID,
						MessageTS:       job.MessageTS,
						Err:             updateErr,
					})
				}
			}
		}
		if !deliveredByUpdate {
			_, err := publishSlackBusOutbound(workerCtx, s.inprocBus, job.TeamID, job.ChannelID, outText, job.ThreadTS, outCorrelationID)
			if err != nil {
				s.logger.Warn("slack_bus_publish_error", "channel", busruntime.ChannelSlack, "channel_id", job.ChannelID, "bus_error_code", string(busruntime.ErrorCodeOf(err)), "error", err.Error())
				callErrorHook(workerCtx, s.logger, s.options.Hooks, ErrorEvent{
					Stage:           ErrorStagePublishOutbound,
					ConversationKey: job.ConversationKey,
					TeamID:          job.TeamID,
					ChannelID:       job.ChannelID,
					MessageTS:       job.MessageTS,
					Err:             err,
				})
			}
		}
	} else if !planPreserved {
		if updated, updateErr := workingMessage.Update(workerCtx, slackDoneMessageText); updated && updateErr != nil {
			s.logger.Warn("slack_working_message_update_error", "channel", busruntime.ChannelSlack, "channel_id", job.ChannelID, "message_ts", job.MessageTS, "error", updateErr.Error())
			callErrorHook(workerCtx, s.logger, s.options.Hooks, ErrorEvent{
				Stage:           ErrorStagePublishOutbound,
				ConversationKey: job.ConversationKey,
				TeamID:          job.TeamID,
				ChannelID:       job.ChannelID,
				MessageTS:       job.MessageTS,
				Err:             updateErr,
			})
		}
	}

	s.mu.Lock()
	latestVersion := s.runner.CurrentVersion(conversationKey)
	contextCompactionOnly := chatcommands.IsContextCompactCommand(job.Text)
	if latestVersion != currentVersion {
		s.history[historyScopeKey] = nil
		s.stickySkillsByConv[historyScopeKey] = nil
	}
	if !contextCompactionOnly && latestVersion == currentVersion && len(loadedSkills) > 0 {
		s.stickySkillsByConv[historyScopeKey] = capUniqueStrings(loadedSkills, slackStickySkillsCap)
	}
	if !contextCompactionOnly {
		currentHistory := s.history[historyScopeKey]
		inboundHistory := newSlackInboundHistoryItem(job)
		if outText != "" {
			inboundHistory.Images = imagehistory.WithDescription(inboundHistory.Images, outText, "agent_final")
		}
		currentHistory = append(currentHistory, inboundHistory)
		if reaction != nil {
			note := "[reacted]"
			if emoji := strings.TrimSpace(reaction.Emoji); emoji != "" {
				note = "[reacted: :" + emoji + ":]"
			}
			currentHistory = append(currentHistory, newSlackOutboundReactionHistoryItem(job, note, reaction.Emoji, time.Now().UTC(), s.botUserID))
		}
		if outText != "" {
			currentHistory = append(currentHistory, newSlackOutboundAgentHistoryItem(job, outText, time.Now().UTC(), s.botUserID))
		}
		s.history[historyScopeKey] = trimChatHistoryItems(currentHistory, s.historyCap)
	}
	s.mu.Unlock()
}
