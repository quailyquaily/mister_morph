package slack

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/agentpair"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	slackbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/slack"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imagehistory"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
	"github.com/quailyquaily/mistermorph/internal/textutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	slacktools "github.com/quailyquaily/mistermorph/tools/slack"
)

func (s *slackRuntimeState) resolveUserIdentity(ctx context.Context, teamID, userID string) (string, string, error) {
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	if teamID == "" || userID == "" {
		return "", "", fmt.Errorf("slack user identity requires team_id and user_id")
	}
	cacheKey := strings.ToUpper(teamID) + ":" + strings.ToUpper(userID)
	now := time.Now().UTC()

	s.mu.Lock()
	if cached, ok := s.userIdentityCache[cacheKey]; ok && cached.ExpiresAt.After(now) {
		s.mu.Unlock()
		username := strings.TrimSpace(cached.Username)
		displayName := strings.TrimSpace(cached.DisplayName)
		if username != "" && displayName != "" {
			return username, displayName, nil
		}
		return "", "", fmt.Errorf("slack user identity cache entry is incomplete")
	}
	s.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	identity, err := s.api.userIdentity(lookupCtx, userID)
	if err != nil {
		return "", "", err
	}
	username := strings.TrimSpace(identity.Username)
	displayName := strings.TrimSpace(identity.DisplayName)
	if username == "" || displayName == "" {
		return "", "", fmt.Errorf("slack users.info returned empty username/display_name")
	}
	s.mu.Lock()
	s.userIdentityCache[cacheKey] = slackUserIdentityCacheEntry{
		UserID:      userID,
		Username:    username,
		DisplayName: displayName,
		ExpiresAt:   now.Add(slackUserIdentityCacheTTL),
	}
	s.mu.Unlock()
	return username, displayName, nil
}

func (s *slackRuntimeState) resolveAgentIdentity(ctx context.Context, teamID, botID string) (slackUserIdentity, error) {
	teamID = strings.TrimSpace(teamID)
	botID = strings.TrimSpace(botID)
	if teamID == "" || botID == "" {
		return slackUserIdentity{}, fmt.Errorf("slack agent identity requires team_id and bot_id")
	}
	cacheKey := strings.ToUpper(teamID) + ":" + strings.ToUpper(botID)
	now := time.Now().UTC()
	s.mu.Lock()
	if cached, ok := s.userIdentityCache[cacheKey]; ok && cached.ExpiresAt.After(now) {
		s.mu.Unlock()
		return slackUserIdentity{
			UserID:      cached.UserID,
			Username:    cached.Username,
			DisplayName: cached.DisplayName,
		}, nil
	}
	s.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	identity, err := s.api.botIdentity(lookupCtx, botID)
	if err != nil {
		return slackUserIdentity{}, err
	}
	s.mu.Lock()
	s.userIdentityCache[cacheKey] = slackUserIdentityCacheEntry{
		UserID:      identity.UserID,
		Username:    identity.Username,
		DisplayName: identity.DisplayName,
		ExpiresAt:   now.Add(slackUserIdentityCacheTTL),
	}
	s.mu.Unlock()
	return identity, nil
}

func (s *slackRuntimeState) enqueueInbound(ctx context.Context, msg busruntime.BusMessage) error {
	if ctx == nil {
		ctx = s.workersCtx
	}
	inbound, err := slackbus.InboundMessageFromBusMessage(msg)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(inbound.Text)
	if text == "" {
		return fmt.Errorf("slack inbound text is required")
	}
	historyScopeKey, err := buildSlackHistoryScopeKey(inbound.TeamID, inbound.ChannelID, inbound.ThreadTS)
	if err != nil {
		return err
	}
	if inbound.FromIsAgent && !s.agentInteractions.Allow(historyScopeKey, time.Now().UTC()) {
		s.logger.Warn("agent_interaction_rate_limited",
			"channel", "slack",
			"conversation_key", historyScopeKey,
			"limit", runtimecore.AgentInteractionLimit,
			"window", runtimecore.AgentInteractionWindow,
		)
		return nil
	}
	contextCompactionOnly := chatcommands.IsContextCompactCommand(text)
	if !contextCompactionOnly && len(inbound.ImageAttachments) == 0 {
		controlKey := slackRunControlConversationKeyForInbound(inbound)
		if controlKey == "" {
			controlKey = msg.ConversationKey
		}
		if result := s.runControl.Steer("slack", controlKey, text); result.Found {
			correlationID := fmt.Sprintf("slack:steer:%s:%s", inbound.ChannelID, inbound.MessageTS)
			_, publishErr := publishSlackBusOutbound(ctx, s.inprocBus, inbound.TeamID, inbound.ChannelID, runtimecontrol.SteerFeedback(result.Found, result.Queued), inbound.ThreadTS, correlationID)
			return publishErr
		}
	}
	workspaceResolution, err := workspace.Resolve(s.workspaceStore, msg.ConversationKey, s.dependencies.DefaultWorkspaceDir)
	if err != nil {
		return err
	}
	workspaceDir := workspaceResolution.WorkspaceDir
	imagePaths := busruntime.ImagePathsFromAttachments(inbound.ImageAttachments)
	images := imagehistory.BuildFromAttachments(inbound.ImageAttachments, pathroots.New(workspaceDir, s.fileCacheDir, ""))
	jobTaskID := slackTaskID(inbound.TeamID, inbound.ChannelID, inbound.MessageTS)
	generationLease, runtimeBundle, err := s.captureRuntimeGeneration()
	if err != nil {
		return err
	}
	transferredGeneration := false
	defer func() {
		if !transferredGeneration && generationLease != nil {
			generationLease.Release()
		}
	}()
	taskRoute, err := runtimeBundle.TaskRuntime.ResolveTaskRouteForRun(llmstats.WithRunID(ctx, jobTaskID), text)
	if err != nil {
		return err
	}
	buildJob := func(version uint64) slackJob {
		admittedRoute := taskRoute
		return slackJob{
			TaskID:          jobTaskID,
			ConversationKey: msg.ConversationKey,
			TeamID:          inbound.TeamID,
			ChannelID:       inbound.ChannelID,
			ChatType:        inbound.ChatType,
			MessageTS:       inbound.MessageTS,
			ThreadTS:        inbound.ThreadTS,
			UserID:          inbound.UserID,
			Username:        inbound.Username,
			DisplayName:     inbound.DisplayName,
			FromIsAgent:     inbound.FromIsAgent,
			Text:            text,
			ImagePaths:      imagePaths,
			Images:          append([]chathistory.ChatHistoryImage(nil), images...),
			WorkspaceDir:    workspaceDir,
			Route:           &admittedRoute,
			SentAt:          inbound.SentAt,
			Version:         version,
			MentionUsers:    append([]string(nil), inbound.MentionUsers...),
			Generation:      generationLease,
		}
	}
	if s.taskStore != nil {
		createdAt := inbound.SentAt.UTC()
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		topicID, topicTitle := slackManagedTopicInfo(inbound.TeamID, inbound.ChannelID, inbound.ThreadTS, inbound.MessageTS)
		if err := recordSlackQueuedTask(s.taskStore, daemonruntime.TaskInfo{
			ID:        jobTaskID,
			Status:    daemonruntime.TaskQueued,
			Task:      textutil.TruncateRunes(text, 2000),
			Model:     strings.TrimSpace(taskRoute.ClientConfig.Model),
			Timeout:   s.taskTimeout.String(),
			CreatedAt: createdAt,
			TopicID:   topicID,
			Result: map[string]any{
				"source":            "slack",
				"slack_team_id":     inbound.TeamID,
				"slack_channel_id":  inbound.ChannelID,
				"slack_message_ts":  inbound.MessageTS,
				"slack_thread_ts":   inbound.ThreadTS,
				"slack_from_userID": inbound.UserID,
			},
		}, daemonruntime.TaskTrigger{
			Source: "webhook",
			Event:  "webhook_inbound",
			Ref:    fmt.Sprintf("slack/%s/%s/%s", inbound.TeamID, inbound.ChannelID, inbound.MessageTS),
		}, topicTitle); err != nil {
			return err
		}
	}
	if err := s.runner.Enqueue(ctx, msg.ConversationKey, buildJob); err != nil {
		if stateErr := runtimecore.MarkTaskFailed(s.taskStore, jobTaskID, strings.TrimSpace(err.Error()), taskdomain.EndedByCancellation(ctx, err)); stateErr != nil {
			return fmt.Errorf("enqueue slack task: %v; persist failed state: %w", err, stateErr)
		}
		return err
	}
	transferredGeneration = true
	callInboundHook(ctx, s.logger, s.options.Hooks, InboundEvent{
		ConversationKey: msg.ConversationKey,
		TeamID:          inbound.TeamID,
		ChannelID:       inbound.ChannelID,
		ChatType:        inbound.ChatType,
		MessageTS:       inbound.MessageTS,
		ThreadTS:        inbound.ThreadTS,
		UserID:          inbound.UserID,
		Username:        inbound.Username,
		DisplayName:     inbound.DisplayName,
		Text:            text,
		MentionUsers:    append([]string(nil), inbound.MentionUsers...),
	})
	return nil
}

func (s *slackRuntimeState) handleBusMessage(ctx context.Context, msg busruntime.BusMessage) error {
	switch msg.Direction {
	case busruntime.DirectionInbound:
		if msg.Channel != busruntime.ChannelSlack {
			return fmt.Errorf("unsupported inbound channel: %s", msg.Channel)
		}
		if err := s.contactsService.ObserveInboundBusMessage(context.Background(), msg, time.Now().UTC()); err != nil {
			s.logger.Warn("contacts_observe_bus_error", "channel", msg.Channel, "idempotency_key", msg.IdempotencyKey, "error", err.Error())
		}
		return s.enqueueInbound(ctx, msg)
	case busruntime.DirectionOutbound:
		return s.deliverOutbound(ctx, msg)
	default:
		return fmt.Errorf("unsupported direction: %s", msg.Direction)
	}
}

func (s *slackRuntimeState) deliverOutbound(ctx context.Context, msg busruntime.BusMessage) error {
	if msg.Channel != busruntime.ChannelSlack {
		return fmt.Errorf("unsupported outbound channel: %s", msg.Channel)
	}
	_, _, err := s.deliveryAdapter.Deliver(ctx, msg)
	if err != nil {
		callErrorHook(ctx, s.logger, s.options.Hooks, ErrorEvent{
			Stage:           ErrorStageDeliverOutbound,
			ConversationKey: msg.ConversationKey,
			Err:             err,
		})
		return err
	}
	event, eventErr := slackOutboundEventFromBusMessage(msg)
	if eventErr != nil {
		callErrorHook(ctx, s.logger, s.options.Hooks, ErrorEvent{
			Stage:           ErrorStageDeliverOutbound,
			ConversationKey: msg.ConversationKey,
			Err:             eventErr,
		})
	} else {
		callOutboundHook(ctx, s.logger, s.options.Hooks, event)
	}
	return nil
}

func (s *slackRuntimeState) appendIgnoredInboundHistory(event slackInboundEvent) {
	conversationKey, err := buildSlackConversationKey(event.TeamID, event.ChannelID)
	if err != nil {
		return
	}
	historyScopeKey, err := buildSlackHistoryScopeKey(event.TeamID, event.ChannelID, event.ThreadTS)
	if err != nil {
		return
	}
	s.mu.Lock()
	currentHistory := s.history[historyScopeKey]
	currentHistory = append(currentHistory, newSlackInboundHistoryItem(slackJob{
		ConversationKey: conversationKey,
		TeamID:          event.TeamID,
		ChannelID:       event.ChannelID,
		ChatType:        event.ChatType,
		MessageTS:       event.MessageTS,
		ThreadTS:        event.ThreadTS,
		UserID:          event.UserID,
		Username:        event.Username,
		DisplayName:     event.DisplayName,
		FromIsAgent:     event.IsAgent,
		Text:            event.Text,
		SentAt:          event.SentAt,
		MentionUsers:    append([]string(nil), event.MentionUsers...),
		ImagePaths:      busruntime.ImagePathsFromAttachments(event.ImageAttachments),
	}))
	s.history[historyScopeKey] = trimChatHistoryItems(currentHistory, s.historyCap)
	s.mu.Unlock()
}

func (s *slackRuntimeState) handleSocketEnvelope(ctx context.Context, envelope slackSocketEnvelope) error {
	if ctx == nil {
		ctx = context.Background()
	}
	approvalAction, approvalOK, err := parseSlackApprovalAction(envelope)
	if err != nil {
		return err
	}
	if approvalOK {
		s.handleApprovalAction(ctx, approvalAction)
		return nil
	}
	event, ok, err := parseSlackInboundEvent(envelope, s.botUserID)
	if err != nil || !ok {
		return err
	}
	if event.IsAgent && strings.EqualFold(strings.TrimSpace(event.BotID), strings.TrimSpace(s.botID)) {
		return nil
	}
	s.logger.Info("slack_inbound_event",
		"event_type", event.EventType,
		"event_subtype", event.EventSubtype,
		"event_id", event.EventID,
		"team_id", event.TeamID,
		"channel_id", event.ChannelID,
		"chat_type", event.ChatType,
		"user_id", event.UserID,
		"bot_id", event.BotID,
		"from_is_agent", event.IsAgent,
		"message_ts", event.MessageTS,
		"thread_ts", event.ThreadTS,
		"image_file_count", len(event.ImageFiles),
		"is_app_mention", event.IsAppMention,
		"is_thread_message", event.IsThreadMessage,
	)
	isGroup := isSlackGroupChat(event.ChatType)
	normalizedCommandText := normalizeSlackCommandText(event.Text, s.botUserID)
	commandWord, commandArgs := chatcommands.ParseCommand(normalizedCommandText)
	normalizedCommand := chatcommands.NormalizeCommand(commandWord)
	if agentpair.IsControlMessage(event.Text) {
		if s.pairManager == nil || isGroup || !event.IsAgent {
			s.logger.Warn("agent_pair_failed",
				"channel", "slack",
				"team_id", event.TeamID,
				"channel_id", event.ChannelID,
				"message_ts", event.MessageTS,
				"reason", "invalid_control_sender_or_scope",
			)
			return nil
		}
		identity, resolveErr := s.resolveAgentIdentity(ctx, event.TeamID, event.BotID)
		if resolveErr != nil {
			s.logger.Warn("agent_pair_failed", "channel", "slack", "team_id", event.TeamID, "channel_id", event.ChannelID, "message_ts", event.MessageTS, "reason", "sender_identity_failed", "error", resolveErr.Error())
			return nil
		}
		peer := slackInboundAgentPeer(event.TeamID, identity.UserID, identity.Username, identity.DisplayName, event.ChannelID)
		_, handled, pairErr := s.pairManager.Handle(ctx, peer, event.Text)
		if pairErr != nil {
			s.logger.Warn("agent_pair_failed", "channel", "slack", "team_id", event.TeamID, "channel_id", event.ChannelID, "message_ts", event.MessageTS, "peer_agent_id", peer.ID, "reason", "offer_rejected", "error", pairErr.Error())
		}
		if handled {
			return nil
		}
	}
	if normalizedCommand == "/pair" {
		if isGroup || event.IsAgent || s.pairManager == nil {
			s.logger.Warn("agent_pair_failed",
				"channel", "slack",
				"team_id", event.TeamID,
				"channel_id", event.ChannelID,
				"message_ts", event.MessageTS,
				"reason", "pair_command_requires_private_human_sender",
			)
			return nil
		}
		target, targetErr := slackPairTarget(commandArgs, event.TeamID)
		var status agentpair.Status
		if targetErr == nil {
			adminID := "slack:" + strings.TrimSpace(event.TeamID) + ":" + strings.TrimSpace(event.UserID)
			status, targetErr = s.pairManager.Start(ctx, adminID, target, "")
		} else {
			s.logger.Warn("agent_pair_failed", "channel", "slack", "team_id", event.TeamID, "channel_id", event.ChannelID, "message_ts", event.MessageTS, "reason", "invalid_target", "error", targetErr.Error())
		}
		sendSlackPairReply(ctx, s.api, event.ChannelID, status, targetErr)
		return nil
	}
	pairedAgent := false
	agentIdentityResolved := false
	agentUsername := ""
	agentDisplayName := ""
	if event.IsAgent && !isGroup && s.pairManager != nil {
		identity, resolveErr := s.resolveAgentIdentity(ctx, event.TeamID, event.BotID)
		if resolveErr != nil {
			s.logger.Warn("slack_sender_identity_enrichment_failed", "team_id", event.TeamID, "channel_id", event.ChannelID, "bot_id", event.BotID, "error", resolveErr.Error())
			return nil
		}
		event.UserID = identity.UserID
		agentUsername = identity.Username
		agentDisplayName = identity.DisplayName
		agentIdentityResolved = true
		peer := slackInboundAgentPeer(event.TeamID, identity.UserID, identity.Username, identity.DisplayName, event.ChannelID)
		pairedAgent, err = s.pairManager.IsPaired(ctx, peer)
		if err != nil {
			s.logger.Warn("slack_agent_pair_lookup_failed", "team_id", event.TeamID, "channel_id", event.ChannelID, "message_ts", event.MessageTS, "peer_agent_id", peer.ID, "error", err.Error())
			pairedAgent = false
		}
	}
	if !slackChatAuthorized(s.allowedTeams, s.allowedChannels, event.TeamID, event.ChannelID, event.ChatType, event.IsAgent, pairedAgent) {
		return nil
	}
	conversationKey, err := buildSlackConversationKey(event.TeamID, event.ChannelID)
	if err != nil {
		return err
	}
	historyScopeKey, err := buildSlackHistoryScopeKey(event.TeamID, event.ChannelID, event.ThreadTS)
	if err != nil {
		return err
	}
	firstMentionFound, firstMentionTargetsSelf := slackFirstBodyMentionTargetsSelf(event.MentionUsers, s.botUserID)
	if shouldIgnoreSlackFirstMention(event.ChatType, event.IsAgent, firstMentionFound, firstMentionTargetsSelf) {
		s.logger.Info("slack_message_ignored_first_mention",
			"team_id", event.TeamID,
			"channel_id", event.ChannelID,
			"message_ts", event.MessageTS,
			"from_is_agent", event.IsAgent,
			"first_mention_found", firstMentionFound,
		)
		if strings.EqualFold(s.groupTriggerMode, "talkative") {
			s.appendIgnoredInboundHistory(event)
		}
		if s.untriggeredRecorder != nil && !event.IsAgent {
			if recordErr := s.untriggeredRecorder.Record(runtimecore.UntriggeredMessage{
				Channel:         string(busruntime.ChannelSlack),
				ConversationKey: historyScopeKey,
				MessageID:       event.MessageTS,
				SenderID:        event.UserID,
				SentAt:          event.SentAt,
				Text:            event.Text,
				HasAttachment:   len(event.ImageFiles) > 0,
			}); recordErr != nil {
				s.logger.Error("slack_untriggered_journal_append_error", "channel_id", event.ChannelID, "message_ts", event.MessageTS, "error", recordErr.Error())
			}
		}
		return nil
	}
	var username, displayName string
	var identityErr error
	if agentIdentityResolved {
		username = agentUsername
		displayName = agentDisplayName
	} else if event.IsAgent {
		identity, resolveErr := s.resolveAgentIdentity(ctx, event.TeamID, event.BotID)
		identityErr = resolveErr
		if resolveErr == nil {
			event.UserID = identity.UserID
			username = identity.Username
			displayName = identity.DisplayName
		}
	} else {
		username, displayName, identityErr = s.resolveUserIdentity(ctx, event.TeamID, event.UserID)
	}
	if identityErr != nil {
		s.logger.Warn("slack_sender_identity_enrichment_failed",
			"conversation_key", conversationKey,
			"team_id", event.TeamID,
			"channel_id", event.ChannelID,
			"user_id", event.UserID,
			"error", identityErr.Error(),
		)
		callErrorHook(ctx, s.logger, s.options.Hooks, ErrorEvent{
			Stage:           ErrorStageIdentityEnrich,
			ConversationKey: conversationKey,
			TeamID:          event.TeamID,
			ChannelID:       event.ChannelID,
			MessageTS:       event.MessageTS,
			Err:             identityErr,
		})
		return nil
	}
	event.Username = username
	event.DisplayName = displayName
	untriggeredText := event.Text
	if len(event.ImageFiles) > 0 && strings.TrimSpace(event.Text) == "" {
		event.Text = "User sent an image."
	}
	s.mu.Lock()
	currentSkills := append([]string(nil), s.stickySkillsByConv[historyScopeKey]...)
	s.mu.Unlock()
	contextCompactionOnly := chatcommands.IsContextCompactCommand(normalizedCommandText)
	if contextCompactionOnly && isSlackGroupChat(event.ChatType) && !slackCommandExplicitlyAddressed(event.Text, s.botUserID) {
		return nil
	}
	if isSlackStopCommand(event, s.botUserID) {
		controlKey := slackRunControlConversationKeyForEvent(event)
		if controlKey == "" {
			controlKey = conversationKey
		}
		result := s.runControl.Stop("slack", controlKey, "/stop")
		correlationID := fmt.Sprintf("slack:stop:%s:%s", event.ChannelID, event.MessageTS)
		if _, publishErr := publishSlackBusOutbound(ctx, s.inprocBus, event.TeamID, event.ChannelID, runtimecontrol.StopFeedback(result.Found), event.ThreadTS, correlationID); publishErr != nil {
			s.logger.Warn("slack_bus_publish_error", "channel_id", event.ChannelID, "message_ts", event.MessageTS, "bus_error_code", string(busruntime.ErrorCodeOf(publishErr)), "error", publishErr.Error())
		}
		return nil
	}
	handledCommand, commandErr := maybeHandleSlackCommand(ctx, s.dependencies, s.inprocBus, s.workspaceStore, conversationKey, event, s.botUserID, currentSkills)
	if commandErr != nil {
		s.logger.Warn("slack_command_error",
			"conversation_key", conversationKey,
			"team_id", event.TeamID,
			"channel_id", event.ChannelID,
			"message_ts", event.MessageTS,
			"error", commandErr.Error(),
		)
		callErrorHook(ctx, s.logger, s.options.Hooks, ErrorEvent{
			Stage:           ErrorStagePublishOutbound,
			ConversationKey: conversationKey,
			TeamID:          event.TeamID,
			ChannelID:       event.ChannelID,
			MessageTS:       event.MessageTS,
			Err:             commandErr,
		})
		return nil
	}
	if handledCommand {
		return nil
	}
	if contextCompactionOnly {
		event.Text = normalizedCommandText
	}

	if isGroup && !contextCompactionOnly {
		s.mu.Lock()
		historySnapshot := append([]chathistory.ChatHistoryItem(nil), s.history[historyScopeKey]...)
		s.mu.Unlock()
		decisionCtx := llmstats.WithRunID(ctx, slackTaskID(event.TeamID, event.ChannelID, event.MessageTS))
		var addressingReactionTool *slacktools.ReactTool
		if s.api != nil && strings.TrimSpace(event.ChannelID) != "" && strings.TrimSpace(event.MessageTS) != "" {
			addressingReactionTool = slacktools.NewReactTool(newSlackToolAPI(s.api), event.ChannelID, event.MessageTS, s.allowedChannels, s.availableEmojiNames)
		}
		generationLease, runtimeBundle, captureErr := s.captureRuntimeGeneration()
		if captureErr != nil {
			s.logger.Warn("slack_runtime_generation_unavailable", "error", captureErr.Error())
			return nil
		}
		addressingTimeout := runtimeBundle.AddressingRoute.ClientConfig.RequestTimeout
		if addressingTimeout <= 0 {
			addressingTimeout = s.options.RequestTimeout
		}
		decision, accepted, err := decideSlackGroupTrigger(
			decisionCtx,
			runtimeBundle.AddressingClient,
			runtimeBundle.AddressingModel,
			event,
			s.botUserID,
			s.availableEmojiList,
			s.groupTriggerMode,
			addressingTimeout,
			s.addressingConfidenceThreshold,
			s.addressingInterjectThreshold,
			historySnapshot,
			addressingReactionTool,
			s.dependencies.RuntimePaths.PersonaDir,
		)
		if generationLease != nil {
			generationLease.Release()
		}
		if addressingReactionTool != nil {
			if reaction := addressingReactionTool.LastReaction(); reaction != nil {
				s.logger.Info("slack_group_addressing_reaction_applied",
					"channel_id", reaction.ChannelID,
					"message_ts", reaction.MessageTS,
					"emoji", reaction.Emoji,
					"source", reaction.Source,
				)
			}
		}
		if err != nil {
			s.logger.Warn("slack_addressing_llm_error", "channel_id", event.ChannelID, "error", err.Error())
			callErrorHook(ctx, s.logger, s.options.Hooks, ErrorEvent{
				Stage:           ErrorStageGroupTrigger,
				ConversationKey: conversationKey,
				TeamID:          event.TeamID,
				ChannelID:       event.ChannelID,
				MessageTS:       event.MessageTS,
				Err:             err,
			})
			return nil
		}
		if !accepted {
			s.logger.Info("slack_group_ignored",
				"team_id", event.TeamID,
				"channel_id", event.ChannelID,
				"message_ts", event.MessageTS,
				"thread_ts", event.ThreadTS,
				"text_len", len(event.Text),
				"image_file_count", len(event.ImageFiles),
				"llm_attempted", decision.AddressingLLMAttempted,
				"llm_ok", decision.AddressingLLMOK,
				"llm_addressed", decision.Addressing.Addressed,
				"confidence", decision.Addressing.Confidence,
				"wanna_interject", decision.Addressing.WannaInterject,
				"interject", decision.Addressing.Interject,
				"impulse", decision.Addressing.Impulse,
				"is_lightweight", decision.Addressing.IsLightweight,
				"reason", decision.Reason,
			)
			if strings.EqualFold(s.groupTriggerMode, "talkative") {
				s.appendIgnoredInboundHistory(event)
			}
			if s.untriggeredRecorder != nil {
				if recordErr := s.untriggeredRecorder.Record(runtimecore.UntriggeredMessage{
					Channel:         string(busruntime.ChannelSlack),
					ConversationKey: historyScopeKey,
					MessageID:       event.MessageTS,
					SenderID:        event.UserID,
					SentAt:          event.SentAt,
					Text:            untriggeredText,
					HasAttachment:   len(event.ImageFiles) > 0 || len(event.ImageAttachments) > 0,
				}); recordErr != nil {
					s.logger.Error("slack_untriggered_journal_append_error", "channel_id", event.ChannelID, "message_ts", event.MessageTS, "error", recordErr.Error())
				}
			}
			return nil
		}
		event.ThreadTS = quoteReplyThreadTSForGroupTrigger(event, decision)
	}
	workspaceResolution, err := workspace.Resolve(s.workspaceStore, conversationKey, s.dependencies.DefaultWorkspaceDir)
	if err != nil {
		return err
	}
	workspaceDir := workspaceResolution.WorkspaceDir
	if len(event.ImageFiles) > 0 {
		imageCacheDir, dirErr := imagehistory.DownloadDir(s.fileCacheDir, workspaceDir, chathistory.ChannelSlack)
		if dirErr != nil {
			return dirErr
		}
		for _, file := range event.ImageFiles {
			if len(event.ImageAttachments) >= slackLLMMaxImages {
				break
			}
			imageCtx, cancelImage := slackImageDownloadContext(ctx)
			path, imageErr := downloadSlackImageToCache(imageCtx, s.api, imageCacheDir, file, slackLLMMaxImageBytes)
			cancelImage()
			if imageErr != nil {
				s.logger.Warn("slack_image_download_failed",
					"team_id", event.TeamID,
					"channel_id", event.ChannelID,
					"message_ts", event.MessageTS,
					"file_id", strings.TrimSpace(file.ID),
					"error", imageErr.Error(),
				)
				continue
			}
			event.ImageAttachments = append(event.ImageAttachments, busruntime.ImageAttachment{
				Path:               path,
				SourceMessageID:    strings.TrimSpace(event.MessageTS),
				SourceAttachmentID: strings.TrimSpace(file.ID),
				MIMEType:           strings.TrimSpace(slackFileMIMEType(file)),
			})
		}
		if len(event.ImageAttachments) == 0 {
			event.Text = appendSlackImageReadFailure(event.Text)
		}
	}

	accepted, err := s.inboundAdapter.HandleInboundMessage(ctx, slackbus.InboundMessage{
		TeamID:           event.TeamID,
		ChannelID:        event.ChannelID,
		ChatType:         event.ChatType,
		MessageTS:        event.MessageTS,
		ThreadTS:         event.ThreadTS,
		UserID:           event.UserID,
		Username:         event.Username,
		DisplayName:      event.DisplayName,
		FromIsAgent:      event.IsAgent,
		Text:             event.Text,
		SentAt:           event.SentAt,
		MentionUsers:     append([]string(nil), event.MentionUsers...),
		EventID:          event.EventID,
		ImageAttachments: append([]busruntime.ImageAttachment(nil), event.ImageAttachments...),
	})
	if err != nil {
		s.logger.Warn("slack_bus_publish_error", "channel_id", event.ChannelID, "message_ts", event.MessageTS, "bus_error_code", string(busruntime.ErrorCodeOf(err)), "error", err.Error())
		callErrorHook(ctx, s.logger, s.options.Hooks, ErrorEvent{
			Stage:           ErrorStagePublishInbound,
			ConversationKey: conversationKey,
			TeamID:          event.TeamID,
			ChannelID:       event.ChannelID,
			MessageTS:       event.MessageTS,
			Err:             err,
		})
		return nil
	}
	if !accepted {
		s.logger.Debug("slack_bus_inbound_deduped", "channel_id", event.ChannelID, "message_ts", event.MessageTS)
	}
	return nil
}

func (s *slackRuntimeState) runSocketLoop() error {
	for {
		if s.ctx.Err() != nil {
			s.logger.Info("slack_stop", "reason", "context_canceled")
			return nil
		}
		conn, err := s.api.connectSocket(s.ctx)
		if err != nil {
			if s.ctx.Err() != nil {
				s.logger.Info("slack_stop", "reason", "context_canceled")
				return nil
			}
			s.logger.Warn("slack_socket_connect_error", "error", err.Error())
			callErrorHook(s.ctx, s.logger, s.options.Hooks, ErrorEvent{Stage: ErrorStageSocketConnect, Err: err})
			if err := sleepWithContext(s.ctx, 2*time.Second); err != nil {
				return nil
			}
			continue
		}
		s.logger.Info("slack_socket_connected")
		readErr := consumeSlackSocket(s.ctx, conn, func(envelope slackSocketEnvelope) error {
			return s.handleSocketEnvelope(s.ctx, envelope)
		})
		_ = conn.Close()
		if readErr != nil && !errors.Is(readErr, context.Canceled) && !errors.Is(readErr, context.DeadlineExceeded) {
			s.logger.Warn("slack_socket_read_error", "error", readErr.Error())
			callErrorHook(s.ctx, s.logger, s.options.Hooks, ErrorEvent{Stage: ErrorStageSocketRead, Err: readErr})
		}
	}
}
