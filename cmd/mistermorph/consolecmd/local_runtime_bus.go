package consolecmd

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/idempotency"
)

const (
	consoleParticipantKey = "console:user"
	consoleUsername       = "console"
	consoleDisplayName    = "Console User"
)

func (r *consoleLocalRuntime) submitTaskViaBus(ctx context.Context, task string, model string, timeout time.Duration, topicID string, topicTitle string, trigger daemonruntime.TaskTrigger) (daemonruntime.SubmitTaskResponse, error) {
	job, resp, err := r.acceptTask(task, model, timeout, topicID, topicTitle, trigger)
	if err != nil {
		return daemonruntime.SubmitTaskResponse{}, err
	}
	if err := r.publishConsoleInbound(ctx, job); err != nil {
		runtimecore.MarkTaskFailed(r.store, job.TaskID, strings.TrimSpace(err.Error()), daemonruntime.IsContextDeadline(ctx, err))
		return daemonruntime.SubmitTaskResponse{}, err
	}
	return resp, nil
}

func (r *consoleLocalRuntime) acceptTask(task string, model string, timeout time.Duration, topicID string, topicTitle string, trigger daemonruntime.TaskTrigger) (consoleLocalTaskJob, daemonruntime.SubmitTaskResponse, error) {
	if r == nil || r.store == nil {
		return consoleLocalTaskJob{}, daemonruntime.SubmitTaskResponse{}, fmt.Errorf("console runtime is not initialized")
	}
	now := time.Now().UTC()
	seq := r.seq.Add(1)
	taskID := daemonruntime.BuildTaskID("console", now.UnixNano(), seq, rand.Uint64())
	topicID = strings.TrimSpace(topicID)
	explicitTopicTitle := strings.TrimSpace(topicTitle)
	autoRenameTopic := topicID == "" && explicitTopicTitle == ""
	topicTitle = seedConsoleTopicTitle(task, topicTitle)
	if topicID == "" {
		topic, err := r.store.CreateTopic(topicTitle)
		if err != nil {
			return consoleLocalTaskJob{}, daemonruntime.SubmitTaskResponse{}, err
		}
		topicID = topic.ID
		if strings.TrimSpace(topicTitle) == "" {
			topicTitle = strings.TrimSpace(topic.Title)
		}
	}
	conversationKey := buildConsoleConversationKey(topicID)
	if err := r.store.UpsertWithTrigger(daemonruntime.TaskInfo{
		ID:        taskID,
		Status:    daemonruntime.TaskQueued,
		Task:      strings.TrimSpace(task),
		Model:     model,
		Timeout:   timeout.String(),
		CreatedAt: now,
		TopicID:   topicID,
	}, trigger, topicTitle); err != nil {
		return consoleLocalTaskJob{}, daemonruntime.SubmitTaskResponse{}, err
	}
	job := consoleLocalTaskJob{
		TaskID:          taskID,
		ConversationKey: conversationKey,
		TopicID:         topicID,
		Task:            strings.TrimSpace(task),
		Model:           model,
		Timeout:         timeout,
		CreatedAt:       now,
		Trigger:         trigger,
		AutoRenameTopic: autoRenameTopic,
	}
	return job, daemonruntime.SubmitTaskResponse{
		ID:      taskID,
		Status:  daemonruntime.TaskQueued,
		TopicID: topicID,
	}, nil
}

func consoleBusSessionID(topicID string) string {
	topicID = strings.TrimSpace(topicID)
	if id, err := uuid.Parse(topicID); err == nil && id.Version() == uuid.Version(7) {
		return id.String()
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

func (r *consoleLocalRuntime) publishConsoleInbound(ctx context.Context, job consoleLocalTaskJob) error {
	if r == nil || r.bus == nil {
		return fmt.Errorf("console bus is not initialized")
	}
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	payloadBase64, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: strings.TrimSpace(job.TaskID),
		Text:      strings.TrimSpace(job.Task),
		SentAt:    job.CreatedAt.UTC().Format(time.RFC3339),
		SessionID: consoleBusSessionID(job.TopicID),
	})
	if err != nil {
		return err
	}
	msg := busruntime.BusMessage{
		ID:              "bus_" + uuid.NewString(),
		Direction:       busruntime.DirectionInbound,
		Channel:         busruntime.ChannelConsole,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: strings.TrimSpace(job.ConversationKey),
		ParticipantKey:  consoleParticipantKey,
		IdempotencyKey:  idempotency.MessageEnvelopeKey(job.TaskID),
		CorrelationID:   strings.TrimSpace(job.TaskID),
		PayloadBase64:   payloadBase64,
		CreatedAt:       job.CreatedAt.UTC(),
		Extensions: busruntime.MessageExtensions{
			SessionID:       consoleBusSessionID(job.TopicID),
			ChatType:        "private",
			FromUserRef:     consoleParticipantKey,
			FromUsername:    consoleUsername,
			FromDisplayName: consoleDisplayName,
		},
	}
	if err := r.bus.PublishValidated(ctx, msg); err != nil {
		return err
	}
	return nil
}

func (r *consoleLocalRuntime) handleConsoleBusMessage(ctx context.Context, msg busruntime.BusMessage) error {
	if r == nil {
		return fmt.Errorf("console runtime is not initialized")
	}
	switch msg.Direction {
	case busruntime.DirectionInbound:
		return r.handleConsoleBusInbound(ctx, msg)
	case busruntime.DirectionOutbound:
		if msg.Channel != busruntime.ChannelConsole {
			return fmt.Errorf("unsupported outbound channel: %s", msg.Channel)
		}
		return nil
	default:
		return fmt.Errorf("unsupported direction: %s", msg.Direction)
	}
}

func (r *consoleLocalRuntime) handleConsoleBusInbound(ctx context.Context, msg busruntime.BusMessage) error {
	if msg.Channel != busruntime.ChannelConsole {
		return fmt.Errorf("unsupported inbound channel: %s", msg.Channel)
	}
	if r.contactsSvc != nil {
		if err := r.contactsSvc.ObserveInboundBusMessage(context.Background(), msg, time.Now().UTC()); err != nil {
			r.logger.Warn("contacts_observe_bus_error", "channel", msg.Channel, "idempotency_key", msg.IdempotencyKey, "error", err.Error())
		}
	}
	taskID := strings.TrimSpace(msg.CorrelationID)
	if taskID == "" {
		envelope, err := msg.Envelope()
		if err != nil {
			return err
		}
		taskID = strings.TrimSpace(envelope.MessageID)
	}
	stored, exists := r.store.Get(taskID)
	if !exists || stored == nil {
		return fmt.Errorf("console task %q not found", taskID)
	}
	trigger, ok := r.store.GetTrigger(taskID)
	if !ok {
		trigger = daemonruntime.TaskTrigger{
			Source: "ui",
			Event:  "chat_submit",
			Ref:    "web/console",
		}
	}
	autoRename := false
	if topic, ok := r.store.GetTopic(stored.TopicID); ok && topic != nil {
		autoRename = shouldAutoRenameConsoleTopic(stored.TopicID, strings.TrimSpace(stored.Task), strings.TrimSpace(topic.Title), r.store.HeartbeatTopicID())
	}
	job := consoleLocalTaskJob{
		TaskID:          stored.ID,
		ConversationKey: buildConsoleConversationKey(stored.TopicID),
		TopicID:         stored.TopicID,
		Task:            stored.Task,
		Model:           stored.Model,
		Timeout:         parseConsoleTaskTimeout(stored.Timeout, r.defaultTimeout),
		CreatedAt:       stored.CreatedAt,
		Trigger:         trigger,
		AutoRenameTopic: autoRename,
	}
	if err := r.runner.Enqueue(ctx, job.ConversationKey, func(version uint64) consoleLocalTaskJob {
		job.Version = version
		return job
	}); err != nil {
		runtimecore.MarkTaskFailed(r.store, job.TaskID, strings.TrimSpace(err.Error()), daemonruntime.IsContextDeadline(ctx, err))
		return err
	}
	return nil
}

func parseConsoleTaskTimeout(raw string, fallback time.Duration) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		return fallback
	}
	return timeout
}

func shouldAutoRenameConsoleTopic(topicID string, task string, currentTitle string, heartbeatTopicID string) bool {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" || topicID == daemonruntime.ConsoleDefaultTopicID || topicID == strings.TrimSpace(heartbeatTopicID) {
		return false
	}
	task = strings.TrimSpace(task)
	currentTitle = strings.TrimSpace(currentTitle)
	if task == "" || currentTitle == "" {
		return false
	}
	return currentTitle == seedConsoleTopicTitle(task, "")
}
