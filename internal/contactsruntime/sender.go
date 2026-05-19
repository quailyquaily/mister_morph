package contactsruntime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	linebus "github.com/quailyquaily/mistermorph/internal/bus/adapters/line"
	slackbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/slack"
	telegrambus "github.com/quailyquaily/mistermorph/internal/bus/adapters/telegram"
	refid "github.com/quailyquaily/mistermorph/internal/entryutil/refid"
	larkapi "github.com/quailyquaily/mistermorph/internal/larkapi"
	"github.com/quailyquaily/mistermorph/internal/slackclient"
)

const defaultTelegramBaseURL = "https://api.telegram.org"
const defaultSlackBaseURL = "https://slack.com/api"
const defaultLineBaseURL = "https://api.line.me"
const defaultLarkBaseURL = "https://open.feishu.cn/open-apis"

type SenderOptions struct {
	TelegramBotToken string
	TelegramBaseURL  string
	SlackBotToken    string
	SlackBaseURL     string
	LineChannelToken string
	LineBaseURL      string
	LarkAppID        string
	LarkAppSecret    string
	LarkBaseURL      string
	BusMaxInFlight   int
	Logger           *slog.Logger
}

type RoutingSender struct {
	bus              *busruntime.Inproc
	telegramDelivery *telegrambus.DeliveryAdapter
	slackDelivery    *slackbus.DeliveryAdapter
	lineDelivery     *linebus.DeliveryAdapter
	telegramClient   *http.Client
	lineClient       *http.Client
	larkClient       *http.Client
	slackPoster      *slackclient.Client
	telegramBaseURL  string
	telegramBotToken string
	lineBaseURL      string
	lineToken        string
	larkBaseURL      string
	larkTokenClient  *larkapi.TenantTokenClient
	logger           *slog.Logger
	pendingMu        sync.Mutex
	pending          map[string]chan deliveryResult
	closeOnce        sync.Once
}

type deliveryResult struct {
	accepted bool
	deduped  bool
	err      error
}

type larkSendTarget struct {
	ReceiveIDType string
	ReceiveID     string
}

type larkSendMessageRequest struct {
	ReceiveID string `json:"receive_id"`
	MsgType   string `json:"msg_type"`
	Content   string `json:"content"`
	UUID      string `json:"uuid,omitempty"`
}

type larkReplyMessageRequest struct {
	Content string `json:"content"`
	MsgType string `json:"msg_type"`
	UUID    string `json:"uuid,omitempty"`
}

type larkMessageResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func NewRoutingSender(ctx context.Context, opts SenderOptions) (*RoutingSender, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))
	}

	baseURL := strings.TrimSpace(opts.TelegramBaseURL)
	if baseURL == "" {
		baseURL = defaultTelegramBaseURL
	}
	slackBaseURL := strings.TrimSpace(opts.SlackBaseURL)
	if slackBaseURL == "" {
		slackBaseURL = defaultSlackBaseURL
	}
	lineBaseURL := strings.TrimSpace(opts.LineBaseURL)
	if lineBaseURL == "" {
		lineBaseURL = defaultLineBaseURL
	}
	larkBaseURL := strings.TrimSpace(opts.LarkBaseURL)
	if larkBaseURL == "" {
		larkBaseURL = defaultLarkBaseURL
	}

	maxInFlight := opts.BusMaxInFlight
	if maxInFlight <= 0 {
		maxInFlight = 64
	}
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: maxInFlight,
		Logger:      logger,
		Component:   "contactsruntime_sender",
	})
	if err != nil {
		return nil, err
	}

	sender := &RoutingSender{
		bus:              inprocBus,
		telegramClient:   &http.Client{Timeout: 30 * time.Second},
		lineClient:       &http.Client{Timeout: 30 * time.Second},
		larkClient:       &http.Client{Timeout: 30 * time.Second},
		telegramBaseURL:  baseURL,
		telegramBotToken: strings.TrimSpace(opts.TelegramBotToken),
		lineBaseURL:      lineBaseURL,
		lineToken:        strings.TrimSpace(opts.LineChannelToken),
		larkBaseURL:      larkBaseURL,
		slackPoster:      slackclient.New(&http.Client{Timeout: 30 * time.Second}, slackBaseURL, strings.TrimSpace(opts.SlackBotToken)),
		logger:           logger,
		pending:          make(map[string]chan deliveryResult),
	}
	sender.larkTokenClient = larkapi.NewTenantTokenClient(sender.larkClient, larkBaseURL, strings.TrimSpace(opts.LarkAppID), strings.TrimSpace(opts.LarkAppSecret))
	sender.telegramDelivery, err = telegrambus.NewDeliveryAdapter(telegrambus.DeliveryAdapterOptions{
		SendText: sender.sendTelegramTarget,
	})
	if err != nil {
		_ = sender.Close()
		return nil, err
	}
	sender.slackDelivery, err = slackbus.NewDeliveryAdapter(slackbus.DeliveryAdapterOptions{
		SendText: sender.sendSlackTarget,
	})
	if err != nil {
		_ = sender.Close()
		return nil, err
	}
	sender.lineDelivery, err = linebus.NewDeliveryAdapter(linebus.DeliveryAdapterOptions{
		SendText: sender.sendLineTarget,
	})
	if err != nil {
		_ = sender.Close()
		return nil, err
	}

	busHandler := func(deliverCtx context.Context, msg busruntime.BusMessage) error {
		switch msg.Direction {
		case busruntime.DirectionOutbound:
		default:
			deliverErr := fmt.Errorf("unsupported direction: %s", msg.Direction)
			if err := sender.completePending(msg.ID, deliveryResult{err: deliverErr}); err != nil {
				return err
			}
			return deliverErr
		}
		var (
			accepted   bool
			deduped    bool
			deliverErr error
		)
		switch msg.Channel {
		case busruntime.ChannelTelegram:
			accepted, deduped, deliverErr = sender.telegramDelivery.Deliver(deliverCtx, msg)
		case busruntime.ChannelSlack:
			accepted, deduped, deliverErr = sender.slackDelivery.Deliver(deliverCtx, msg)
		case busruntime.ChannelLine:
			accepted, deduped, deliverErr = sender.lineDelivery.Deliver(deliverCtx, msg)
		default:
			deliverErr = fmt.Errorf("unsupported outbound channel: %s", msg.Channel)
		}
		if err := sender.completePending(msg.ID, deliveryResult{
			accepted: accepted,
			deduped:  deduped,
			err:      deliverErr,
		}); err != nil {
			return err
		}
		return deliverErr
	}
	for _, topic := range busruntime.AllTopics() {
		if err := inprocBus.Subscribe(topic, busHandler); err != nil {
			_ = sender.Close()
			return nil, err
		}
	}

	return sender, nil
}

func (s *RoutingSender) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.bus != nil {
			_ = s.bus.Close()
		}
		s.pendingMu.Lock()
		for id, ch := range s.pending {
			delete(s.pending, id)
			select {
			case ch <- deliveryResult{err: fmt.Errorf("sender is closed")}:
			default:
			}
		}
		s.pendingMu.Unlock()
	})
	return nil
}

func (s *RoutingSender) Send(ctx context.Context, contact contacts.Contact, decision contacts.ShareDecision) (bool, bool, error) {
	if s == nil {
		return false, false, fmt.Errorf("nil routing sender")
	}
	if ctx == nil {
		return false, false, fmt.Errorf("context is required")
	}
	channel, err := contacts.ResolveDecisionChannel(contact, decision)
	if err != nil {
		return false, false, err
	}
	switch channel {
	case contacts.ChannelSlack:
		target, _, _, resolveErr := ResolveSlackTargetWithChatID(contact, decision.ChatID)
		if resolveErr != nil {
			return false, false, resolveErr
		}
		return s.publishSlack(ctx, target, decision)
	case contacts.ChannelTelegram:
		target, _, resolveErr := ResolveTelegramTargetWithChatID(contact, decision.ChatID)
		if resolveErr != nil {
			return false, false, resolveErr
		}
		return s.publishTelegram(ctx, target, decision)
	case contacts.ChannelLine:
		target, resolveErr := ResolveLineTargetWithChatID(contact, decision.ChatID)
		if resolveErr != nil {
			return false, false, resolveErr
		}
		return s.publishLine(ctx, target, decision)
	case contacts.ChannelLark:
		target, resolveErr := ResolveLarkTargetWithChatID(contact, decision.ChatID)
		if resolveErr != nil {
			return false, false, resolveErr
		}
		return s.publishLark(ctx, target, decision)
	default:
		return false, false, fmt.Errorf("unsupported delivery channel: %s", channel)
	}
}

func (s *RoutingSender) publishTelegram(ctx context.Context, target any, decision contacts.ShareDecision) (bool, bool, error) {
	if s == nil || s.bus == nil {
		return false, false, fmt.Errorf("sender bus is not configured")
	}
	idempotencyKey := strings.TrimSpace(decision.IdempotencyKey)
	if idempotencyKey == "" {
		return false, false, fmt.Errorf("idempotency_key is required")
	}
	topic := contacts.ShareTopic
	now := time.Now().UTC()
	payloadRaw, err := buildEnvelopePayload(decision, decision.ContentType, decision.PayloadBase64, now)
	if err != nil {
		return false, false, err
	}
	payloadBase64 := base64.RawURLEncoding.EncodeToString(payloadRaw)
	conversationKey, participantKey, err := telegramConversationFromTarget(target)
	if err != nil {
		return false, false, err
	}
	msg := busruntime.BusMessage{
		ID:              "bus_" + uuid.NewString(),
		Direction:       busruntime.DirectionOutbound,
		Channel:         busruntime.ChannelTelegram,
		Topic:           topic,
		ConversationKey: conversationKey,
		ParticipantKey:  participantKey,
		IdempotencyKey:  idempotencyKey,
		CorrelationID:   "contactsruntime:telegram:" + idempotencyKey,
		PayloadBase64:   payloadBase64,
		CreatedAt:       now,
	}
	return s.publishAndAwait(ctx, msg)
}

func (s *RoutingSender) publishSlack(ctx context.Context, target any, decision contacts.ShareDecision) (bool, bool, error) {
	if s == nil || s.bus == nil {
		return false, false, fmt.Errorf("sender bus is not configured")
	}
	idempotencyKey := strings.TrimSpace(decision.IdempotencyKey)
	if idempotencyKey == "" {
		return false, false, fmt.Errorf("idempotency_key is required")
	}
	topic := contacts.ShareTopic
	now := time.Now().UTC()
	payloadRaw, err := buildEnvelopePayload(decision, decision.ContentType, decision.PayloadBase64, now)
	if err != nil {
		return false, false, err
	}
	payloadBase64 := base64.RawURLEncoding.EncodeToString(payloadRaw)
	conversationKey, participantKey, resolvedTarget, err := slackConversationFromTarget(target)
	if err != nil {
		return false, false, err
	}
	msg := busruntime.BusMessage{
		ID:              "bus_" + uuid.NewString(),
		Direction:       busruntime.DirectionOutbound,
		Channel:         busruntime.ChannelSlack,
		Topic:           topic,
		ConversationKey: conversationKey,
		ParticipantKey:  participantKey,
		IdempotencyKey:  idempotencyKey,
		CorrelationID:   "contactsruntime:slack:" + idempotencyKey,
		PayloadBase64:   payloadBase64,
		CreatedAt:       now,
		Extensions: busruntime.MessageExtensions{
			TeamID:    strings.TrimSpace(resolvedTarget.TeamID),
			ChannelID: strings.TrimSpace(resolvedTarget.ChannelID),
		},
	}
	return s.publishAndAwait(ctx, msg)
}

func (s *RoutingSender) publishLine(ctx context.Context, target any, decision contacts.ShareDecision) (bool, bool, error) {
	if s == nil || s.bus == nil {
		return false, false, fmt.Errorf("sender bus is not configured")
	}
	idempotencyKey := strings.TrimSpace(decision.IdempotencyKey)
	if idempotencyKey == "" {
		return false, false, fmt.Errorf("idempotency_key is required")
	}
	topic := contacts.ShareTopic
	now := time.Now().UTC()
	payloadRaw, err := buildEnvelopePayload(decision, decision.ContentType, decision.PayloadBase64, now)
	if err != nil {
		return false, false, err
	}
	payloadBase64 := base64.RawURLEncoding.EncodeToString(payloadRaw)
	conversationKey, participantKey, resolvedTarget, err := lineConversationFromTarget(target)
	if err != nil {
		return false, false, err
	}
	msg := busruntime.BusMessage{
		ID:              "bus_" + uuid.NewString(),
		Direction:       busruntime.DirectionOutbound,
		Channel:         busruntime.ChannelLine,
		Topic:           topic,
		ConversationKey: conversationKey,
		ParticipantKey:  participantKey,
		IdempotencyKey:  idempotencyKey,
		CorrelationID:   "contactsruntime:line:" + idempotencyKey,
		PayloadBase64:   payloadBase64,
		CreatedAt:       now,
		Extensions: busruntime.MessageExtensions{
			ChannelID: strings.TrimSpace(resolvedTarget.ChatID),
		},
	}
	return s.publishAndAwait(ctx, msg)
}

func (s *RoutingSender) publishLark(ctx context.Context, target larkSendTarget, decision contacts.ShareDecision) (bool, bool, error) {
	if s == nil {
		return false, false, fmt.Errorf("sender is required")
	}
	if ctx == nil {
		return false, false, fmt.Errorf("context is required")
	}
	idempotencyKey := strings.TrimSpace(decision.IdempotencyKey)
	if idempotencyKey == "" {
		return false, false, fmt.Errorf("idempotency_key is required")
	}
	payloadRaw, err := buildEnvelopePayload(decision, decision.ContentType, decision.PayloadBase64, time.Now().UTC())
	if err != nil {
		return false, false, err
	}
	var env busruntime.MessageEnvelope
	if err := json.Unmarshal(payloadRaw, &env); err != nil {
		return false, false, fmt.Errorf("decode lark envelope: %w", err)
	}
	if err := env.Validate(contacts.ShareTopic); err != nil {
		return false, false, err
	}
	if err := s.sendLarkTarget(ctx, target, env); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func (s *RoutingSender) publishAndAwait(ctx context.Context, msg busruntime.BusMessage) (bool, bool, error) {
	if s == nil || s.bus == nil {
		return false, false, fmt.Errorf("sender bus is not configured")
	}
	if ctx == nil {
		return false, false, fmt.Errorf("context is required")
	}
	msgID := strings.TrimSpace(msg.ID)
	if msgID == "" {
		return false, false, fmt.Errorf("message id is required")
	}

	resultCh := make(chan deliveryResult, 1)
	if err := s.registerPending(msgID, resultCh); err != nil {
		return false, false, err
	}

	if err := s.bus.PublishValidated(ctx, msg); err != nil {
		s.dropPending(msgID)
		return false, false, err
	}

	select {
	case result := <-resultCh:
		return result.accepted, result.deduped, result.err
	case <-ctx.Done():
		return false, false, ctx.Err()
	}
}

func (s *RoutingSender) registerPending(msgID string, resultCh chan deliveryResult) error {
	if s == nil {
		return fmt.Errorf("sender is required")
	}
	if strings.TrimSpace(msgID) == "" {
		return fmt.Errorf("message id is required")
	}
	if resultCh == nil {
		return fmt.Errorf("result channel is required")
	}
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if _, exists := s.pending[msgID]; exists {
		return fmt.Errorf("pending delivery already exists: %s", msgID)
	}
	s.pending[msgID] = resultCh
	return nil
}

func (s *RoutingSender) completePending(msgID string, result deliveryResult) error {
	if s == nil {
		return fmt.Errorf("sender is required")
	}
	msgID = strings.TrimSpace(msgID)
	if msgID == "" {
		return fmt.Errorf("message id is required")
	}
	s.pendingMu.Lock()
	resultCh, ok := s.pending[msgID]
	if ok {
		delete(s.pending, msgID)
	}
	s.pendingMu.Unlock()
	if !ok {
		return fmt.Errorf("pending delivery not found: %s", msgID)
	}
	resultCh <- result
	return nil
}

func (s *RoutingSender) dropPending(msgID string) {
	if s == nil {
		return
	}
	s.pendingMu.Lock()
	delete(s.pending, strings.TrimSpace(msgID))
	s.pendingMu.Unlock()
}

func buildEnvelopePayload(decision contacts.ShareDecision, contentType string, payloadBase64 string, now time.Time) ([]byte, error) {
	text, extras, err := decodeEnvelopeTextAndExtras(contentType, payloadBase64)
	if err != nil {
		return nil, err
	}
	messageID := strings.TrimSpace(decision.ItemID)
	if messageID == "" {
		messageID = "msg_" + uuid.NewString()
	}
	payload := map[string]any{
		"message_id": messageID,
		"text":       text,
		"sent_at":    now.Format(time.RFC3339),
	}
	sessionID := strings.TrimSpace(extras["session_id"])
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required for dialogue topics")
	}
	if sessionID != "" {
		if err := validateSessionID(sessionID); err != nil {
			return nil, err
		}
		payload["session_id"] = sessionID
	}
	if replyTo := strings.TrimSpace(extras["reply_to"]); replyTo != "" {
		payload["reply_to"] = replyTo
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal envelope payload: %w", err)
	}
	return raw, nil
}

func validateSessionID(sessionID string) error {
	id, err := uuid.Parse(strings.TrimSpace(sessionID))
	if err != nil {
		return fmt.Errorf("session_id must be uuid_v7")
	}
	if id.Version() != uuid.Version(7) {
		return fmt.Errorf("session_id must be uuid_v7")
	}
	return nil
}

func decodeEnvelopeTextAndExtras(contentType string, payloadBase64 string) (string, map[string]string, error) {
	payloadBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(payloadBase64))
	if err != nil {
		return "", nil, fmt.Errorf("decode payload_base64: %w", err)
	}
	extras := map[string]string{}
	lowerType := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(lowerType, "application/json") {
		var obj map[string]any
		if err := json.Unmarshal(payloadBytes, &obj); err == nil {
			for _, key := range []string{"text", "message", "content", "prompt"} {
				if v, ok := obj[key].(string); ok && strings.TrimSpace(v) != "" {
					if session, ok := obj["session_id"].(string); ok {
						extras["session_id"] = strings.TrimSpace(session)
					}
					if replyTo, ok := obj["reply_to"].(string); ok {
						extras["reply_to"] = strings.TrimSpace(replyTo)
					}
					return strings.TrimSpace(v), extras, nil
				}
			}
			normalized, err := json.Marshal(obj)
			if err == nil {
				return strings.TrimSpace(string(normalized)), extras, nil
			}
		}
	}
	text := strings.TrimSpace(string(payloadBytes))
	if text == "" {
		text = "(empty)"
	}
	return text, extras, nil
}

func telegramConversationFromTarget(target any) (string, string, error) {
	resolvedTarget, err := normalizeTelegramSendTarget(target)
	if err != nil {
		return "", "", err
	}
	chatIDText := strconv.FormatInt(resolvedTarget.ChatID, 10)
	conversationKey, err := busruntime.BuildTelegramTopicConversationKey(chatIDText, resolvedTarget.MessageThreadID)
	if err != nil {
		return "", "", err
	}
	participantKey := chatIDText
	if resolvedTarget.MessageThreadID > 0 {
		participantKey += "_" + strconv.FormatInt(resolvedTarget.MessageThreadID, 10)
	}
	return conversationKey, participantKey, nil
}

func slackConversationFromTarget(target any) (string, string, slackbus.DeliveryTarget, error) {
	resolvedTarget, err := normalizeSlackSendTarget(target)
	if err != nil {
		return "", "", slackbus.DeliveryTarget{}, err
	}
	conversationID := strings.TrimSpace(resolvedTarget.TeamID) + ":" + strings.TrimSpace(resolvedTarget.ChannelID)
	conversationKey, err := busruntime.BuildSlackChannelConversationKey(conversationID)
	if err != nil {
		return "", "", slackbus.DeliveryTarget{}, err
	}
	participantKey := strings.TrimSpace(resolvedTarget.TeamID) + ":" + strings.TrimSpace(resolvedTarget.ChannelID)
	return conversationKey, participantKey, resolvedTarget, nil
}

func lineConversationFromTarget(target any) (string, string, linebus.DeliveryTarget, error) {
	resolvedTarget, err := normalizeLineSendTarget(target)
	if err != nil {
		return "", "", linebus.DeliveryTarget{}, err
	}
	chatID := strings.TrimSpace(resolvedTarget.ChatID)
	conversationKey, err := busruntime.BuildLineConversationKey(chatID)
	if err != nil {
		return "", "", linebus.DeliveryTarget{}, err
	}
	return conversationKey, chatID, resolvedTarget, nil
}

func normalizeTelegramSendTarget(target any) (telegrambus.DeliveryTarget, error) {
	switch value := target.(type) {
	case telegrambus.DeliveryTarget:
		if value.ChatID == 0 {
			return telegrambus.DeliveryTarget{}, fmt.Errorf("telegram chat id is required")
		}
		if value.MessageThreadID < 0 {
			return telegrambus.DeliveryTarget{}, fmt.Errorf("telegram message_thread_id is invalid")
		}
		return value, nil
	case int64:
		if value == 0 {
			return telegrambus.DeliveryTarget{}, fmt.Errorf("telegram chat id is required")
		}
		return telegrambus.DeliveryTarget{ChatID: value}, nil
	case int:
		if value == 0 {
			return telegrambus.DeliveryTarget{}, fmt.Errorf("telegram chat id is required")
		}
		return telegrambus.DeliveryTarget{ChatID: int64(value)}, nil
	case string:
		targetText := strings.TrimSpace(value)
		if targetText == "" {
			return telegrambus.DeliveryTarget{}, fmt.Errorf("telegram target is required")
		}
		if strings.HasPrefix(strings.ToLower(targetText), "tg:") {
			chatID, messageThreadID, err := busruntime.ParseTelegramConversationKey(targetText)
			if err != nil {
				return telegrambus.DeliveryTarget{}, fmt.Errorf("telegram target is invalid: %s", targetText)
			}
			return telegrambus.DeliveryTarget{ChatID: chatID, MessageThreadID: messageThreadID}, nil
		}
		chatID, err := strconv.ParseInt(targetText, 10, 64)
		if err != nil || chatID == 0 {
			return telegrambus.DeliveryTarget{}, fmt.Errorf("telegram target is invalid: %s", targetText)
		}
		return telegrambus.DeliveryTarget{ChatID: chatID}, nil
	default:
		return telegrambus.DeliveryTarget{}, fmt.Errorf("unsupported telegram target type: %T", target)
	}
}

func normalizeSlackSendTarget(target any) (slackbus.DeliveryTarget, error) {
	switch value := target.(type) {
	case slackbus.DeliveryTarget:
		teamID := strings.TrimSpace(value.TeamID)
		channelID := strings.TrimSpace(value.ChannelID)
		if teamID == "" || channelID == "" {
			return slackbus.DeliveryTarget{}, fmt.Errorf("slack team_id and channel_id are required")
		}
		return slackbus.DeliveryTarget{
			TeamID:    teamID,
			ChannelID: channelID,
		}, nil
	default:
		return slackbus.DeliveryTarget{}, fmt.Errorf("unsupported slack target type: %T", target)
	}
}

func normalizeLineSendTarget(target any) (linebus.DeliveryTarget, error) {
	switch value := target.(type) {
	case linebus.DeliveryTarget:
		chatID := strings.TrimSpace(value.ChatID)
		if chatID == "" {
			return linebus.DeliveryTarget{}, fmt.Errorf("line chat_id is required")
		}
		return linebus.DeliveryTarget{ChatID: chatID}, nil
	case string:
		chatID := strings.TrimSpace(value)
		if chatID == "" {
			return linebus.DeliveryTarget{}, fmt.Errorf("line chat_id is required")
		}
		return linebus.DeliveryTarget{ChatID: chatID}, nil
	default:
		return linebus.DeliveryTarget{}, fmt.Errorf("unsupported line target type: %T", target)
	}
}

func normalizeLarkSendTarget(target any) (larkSendTarget, error) {
	switch value := target.(type) {
	case larkSendTarget:
		receiveIDType := strings.TrimSpace(value.ReceiveIDType)
		receiveID := strings.TrimSpace(value.ReceiveID)
		if receiveIDType == "" || receiveID == "" {
			return larkSendTarget{}, fmt.Errorf("lark receive_id_type and receive_id are required")
		}
		return larkSendTarget{
			ReceiveIDType: receiveIDType,
			ReceiveID:     receiveID,
		}, nil
	default:
		return larkSendTarget{}, fmt.Errorf("unsupported lark target type: %T", target)
	}
}

func (s *RoutingSender) sendLarkTarget(ctx context.Context, target larkSendTarget, env busruntime.MessageEnvelope) error {
	if s == nil {
		return fmt.Errorf("sender is required")
	}
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if s.larkClient == nil || s.larkTokenClient == nil {
		return fmt.Errorf("lark sender is not configured")
	}
	resolvedTarget, err := normalizeLarkSendTarget(target)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(env.Text)
	if text == "" {
		return fmt.Errorf("lark text is required")
	}
	replyTo := strings.TrimSpace(env.ReplyTo)
	if replyTo != "" {
		replyErr := s.larkReplyText(ctx, replyTo, text)
		if replyErr == nil {
			return nil
		}
		if resolvedTarget.ReceiveIDType != "chat_id" {
			return replyErr
		}
		if sendErr := s.larkSendText(ctx, resolvedTarget.ReceiveIDType, resolvedTarget.ReceiveID, text); sendErr != nil {
			return fmt.Errorf("lark reply failed: %v; fallback send failed: %w", replyErr, sendErr)
		}
		return nil
	}
	return s.larkSendText(ctx, resolvedTarget.ReceiveIDType, resolvedTarget.ReceiveID, text)
}

func (s *RoutingSender) sendTelegramTarget(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
	if s == nil {
		return fmt.Errorf("sender is required")
	}
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	token := strings.TrimSpace(s.telegramBotToken)
	if token == "" {
		return fmt.Errorf("telegram sender is not configured")
	}
	if s.telegramClient == nil {
		return fmt.Errorf("telegram client is not configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("telegram text is required")
	}

	resolvedTarget, err := normalizeTelegramSendTarget(target)
	if err != nil {
		return err
	}

	body := map[string]any{
		"chat_id":                  resolvedTarget.ChatID,
		"text":                     text,
		"disable_web_page_preview": true,
	}
	messageThreadID := opts.MessageThreadID
	if messageThreadID <= 0 {
		messageThreadID = resolvedTarget.MessageThreadID
	}
	if messageThreadID > 0 {
		body["message_thread_id"] = messageThreadID
	}
	replyToRaw := strings.TrimSpace(opts.ReplyTo)
	if replyToRaw != "" {
		replyToMessageID, parseErr := strconv.ParseInt(replyToRaw, 10, 64)
		if parseErr != nil || replyToMessageID <= 0 {
			return fmt.Errorf("telegram reply_to is invalid")
		}
		body["reply_to_message_id"] = replyToMessageID
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}

	url := strings.TrimRight(strings.TrimSpace(s.telegramBaseURL), "/") + "/bot" + token + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.telegramClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respRaw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram http %d: %s", resp.StatusCode, strings.TrimSpace(string(respRaw)))
	}
	var out struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(respRaw, &out); err != nil {
		return fmt.Errorf("decode telegram response: %w", err)
	}
	if !out.OK {
		desc := strings.TrimSpace(out.Description)
		if desc == "" {
			desc = "ok=false"
		}
		return fmt.Errorf("telegram sendMessage failed: %s", desc)
	}
	return nil
}

func (s *RoutingSender) sendSlackTarget(ctx context.Context, target any, text string, opts slackbus.SendTextOptions) error {
	if s == nil {
		return fmt.Errorf("sender is required")
	}
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if s.slackPoster == nil {
		return fmt.Errorf("slack sender is not configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("slack text is required")
	}

	resolvedTarget, err := normalizeSlackSendTarget(target)
	if err != nil {
		return err
	}
	return s.slackPoster.PostMessage(ctx, resolvedTarget.ChannelID, text, strings.TrimSpace(opts.ThreadTS))
}

type lineTextMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type linePushRequest struct {
	To       string            `json:"to"`
	Messages []lineTextMessage `json:"messages"`
}

func (s *RoutingSender) sendLineTarget(ctx context.Context, target any, text string, opts linebus.SendTextOptions) error {
	_ = opts
	if s == nil {
		return fmt.Errorf("sender is required")
	}
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if s.lineClient == nil {
		return fmt.Errorf("line client is not configured")
	}
	token := strings.TrimSpace(s.lineToken)
	if token == "" {
		return fmt.Errorf("line sender is not configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("line text is required")
	}

	resolvedTarget, err := normalizeLineSendTarget(target)
	if err != nil {
		return err
	}

	raw, err := json.Marshal(linePushRequest{
		To: strings.TrimSpace(resolvedTarget.ChatID),
		Messages: []lineTextMessage{
			{Type: "text", Text: text},
		},
	})
	if err != nil {
		return fmt.Errorf("marshal line payload: %w", err)
	}

	url := strings.TrimRight(strings.TrimSpace(s.lineBaseURL), "/") + "/v2/bot/message/push"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.lineClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respRaw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("line http %d: %s", resp.StatusCode, strings.TrimSpace(string(respRaw)))
	}
	return nil
}

func (s *RoutingSender) larkSendText(ctx context.Context, receiveIDType, receiveID, text string) error {
	contentRaw, err := json.Marshal(map[string]string{"text": strings.TrimSpace(text)})
	if err != nil {
		return fmt.Errorf("marshal lark content: %w", err)
	}
	endpoint := strings.TrimRight(strings.TrimSpace(s.larkBaseURL), "/") + "/im/v1/messages?receive_id_type=" + url.QueryEscape(strings.TrimSpace(receiveIDType))
	return s.larkPostJSON(ctx, endpoint, larkSendMessageRequest{
		ReceiveID: strings.TrimSpace(receiveID),
		MsgType:   "text",
		Content:   string(contentRaw),
		UUID:      uuid.NewString(),
	})
}

func (s *RoutingSender) larkReplyText(ctx context.Context, messageID, text string) error {
	contentRaw, err := json.Marshal(map[string]string{"text": strings.TrimSpace(text)})
	if err != nil {
		return fmt.Errorf("marshal lark content: %w", err)
	}
	endpoint := strings.TrimRight(strings.TrimSpace(s.larkBaseURL), "/") + "/im/v1/messages/" + url.PathEscape(strings.TrimSpace(messageID)) + "/reply"
	return s.larkPostJSON(ctx, endpoint, larkReplyMessageRequest{
		Content: string(contentRaw),
		MsgType: "text",
		UUID:    uuid.NewString(),
	})
}

func (s *RoutingSender) larkPostJSON(ctx context.Context, endpoint string, payload any) error {
	if s == nil {
		return fmt.Errorf("sender is required")
	}
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if s.larkClient == nil || s.larkTokenClient == nil {
		return fmt.Errorf("lark sender is not configured")
	}
	bodyRaw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	token, err := s.larkTokenClient.Token(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyRaw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")

	resp, err := s.larkClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respRaw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("lark http %d: %s", resp.StatusCode, strings.TrimSpace(string(respRaw)))
	}
	var out larkMessageResponse
	if err := json.Unmarshal(respRaw, &out); err != nil {
		return fmt.Errorf("decode lark response: %w", err)
	}
	if out.Code != 0 {
		return fmt.Errorf("lark api code %d: %s", out.Code, strings.TrimSpace(out.Msg))
	}
	return nil
}

func ResolveTelegramTarget(contact contacts.Contact) (any, string, error) {
	if chatID, chatType, ok := preferredChat(contact); ok {
		return chatID, chatType, nil
	}
	value := strings.TrimSpace(contact.ContactID)
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "tg:@") {
		return nil, "", fmt.Errorf("telegram username target is not sendable: %s", value)
	}
	if strings.HasPrefix(lower, "tg:") {
		idText := strings.TrimSpace(value[len("tg:"):])
		chatID, err := strconv.ParseInt(idText, 10, 64)
		if err != nil {
			return nil, "", fmt.Errorf("invalid telegram id in %q", value)
		}
		return chatID, chatTypeFromChatID(chatID), nil
	}
	return nil, "", fmt.Errorf("telegram target not found in tg_private_chat_id/tg_group_chat_ids/contact_id")
}

func ResolveTelegramTargetWithChatID(contact contacts.Contact, chatIDHint string) (any, string, error) {
	hintTarget, hasHint, err := parseTelegramDeliveryTargetHint(chatIDHint)
	if err != nil {
		return nil, "", err
	}
	if !hasHint {
		return ResolveTelegramTarget(contact)
	}
	hintID := hintTarget.ChatID
	if chatID, chatType, ok := contactTelegramChatMatch(contact, hintID); ok {
		if hintTarget.MessageThreadID > 0 {
			return telegrambus.DeliveryTarget{ChatID: chatID, MessageThreadID: hintTarget.MessageThreadID}, chatType, nil
		}
		return chatID, chatType, nil
	}
	if contact.TGPrivateChatID != 0 {
		return contact.TGPrivateChatID, "private", nil
	}
	return nil, "", fmt.Errorf("telegram chat_id %d not found in tg_private_chat_id/tg_group_chat_ids and no tg_private_chat_id fallback", hintID)
}

func parseTelegramDeliveryTargetHint(chatIDHint string) (telegrambus.DeliveryTarget, bool, error) {
	value := strings.TrimSpace(chatIDHint)
	if value == "" {
		return telegrambus.DeliveryTarget{}, false, nil
	}
	if !strings.HasPrefix(strings.ToLower(value), "tg:") {
		_, hasHint, err := refid.ParseTelegramChatIDHint(value)
		return telegrambus.DeliveryTarget{}, hasHint, err
	}
	chatID, messageThreadID, err := busruntime.ParseTelegramConversationKey(value)
	if err != nil {
		return telegrambus.DeliveryTarget{}, true, fmt.Errorf("invalid chat_id: %s", value)
	}
	return telegrambus.DeliveryTarget{ChatID: chatID, MessageThreadID: messageThreadID}, true, nil
}

func ResolveLineTarget(contact contacts.Contact) (linebus.DeliveryTarget, error) {
	if userID := strings.TrimSpace(contact.LineUserID); userID != "" {
		return linebus.DeliveryTarget{ChatID: userID}, nil
	}
	chatIDs := append([]string(nil), contact.LineChatIDs...)
	sort.Slice(chatIDs, func(i, j int) bool { return chatIDs[i] < chatIDs[j] })
	for _, raw := range chatIDs {
		chatID := strings.TrimSpace(raw)
		if chatID == "" {
			continue
		}
		return linebus.DeliveryTarget{ChatID: chatID}, nil
	}
	if chatID, ok := refid.ParseLineChatContactID(contact.ContactID); ok {
		return linebus.DeliveryTarget{ChatID: chatID}, nil
	}
	if userID, ok := refid.ParseLineUserContactID(contact.ContactID); ok {
		return linebus.DeliveryTarget{ChatID: userID}, nil
	}
	return linebus.DeliveryTarget{}, fmt.Errorf("line target not found in line_user_id/line_chat_ids/contact_id")
}

func ResolveLineTargetWithChatID(contact contacts.Contact, chatIDHint string) (linebus.DeliveryTarget, error) {
	chatID, hasHint, err := refid.ParseLineChatIDHint(chatIDHint)
	if err != nil {
		return linebus.DeliveryTarget{}, err
	}
	if hasHint {
		chatID = strings.TrimSpace(chatID)
		if chatID == "" {
			return linebus.DeliveryTarget{}, fmt.Errorf("invalid chat_id: %s", strings.TrimSpace(chatIDHint))
		}
		if strings.EqualFold(strings.TrimSpace(contact.LineUserID), chatID) {
			return linebus.DeliveryTarget{ChatID: chatID}, nil
		}
		for _, raw := range contact.LineChatIDs {
			if strings.EqualFold(strings.TrimSpace(raw), chatID) {
				return linebus.DeliveryTarget{ChatID: chatID}, nil
			}
		}
		if userID := strings.TrimSpace(contact.LineUserID); userID != "" {
			return linebus.DeliveryTarget{ChatID: userID}, nil
		}
		return linebus.DeliveryTarget{}, fmt.Errorf("line chat_id %q not found in line_chat_ids and no line_user_id fallback", chatID)
	}
	return ResolveLineTarget(contact)
}

func ResolveLarkTarget(contact contacts.Contact) (larkSendTarget, error) {
	if openID := refid.NormalizeLarkID(contact.LarkOpenID); openID != "" {
		return larkSendTarget{ReceiveIDType: "open_id", ReceiveID: openID}, nil
	}
	chatIDs := append([]string(nil), contact.LarkChatIDs...)
	sort.Slice(chatIDs, func(i, j int) bool { return chatIDs[i] < chatIDs[j] })
	for _, raw := range chatIDs {
		chatID := refid.NormalizeLarkID(raw)
		if chatID == "" {
			continue
		}
		return larkSendTarget{ReceiveIDType: "chat_id", ReceiveID: chatID}, nil
	}
	if openID, ok := refid.ParseLarkUserContactID(contact.ContactID); ok {
		return larkSendTarget{ReceiveIDType: "open_id", ReceiveID: openID}, nil
	}
	if chatID, ok := refid.ParseLarkChatContactID(contact.ContactID); ok {
		return larkSendTarget{ReceiveIDType: "chat_id", ReceiveID: chatID}, nil
	}
	return larkSendTarget{}, fmt.Errorf("lark target not found in lark_open_id/lark_chat_ids/contact_id")
}

func ResolveLarkTargetWithChatID(contact contacts.Contact, chatIDHint string) (larkSendTarget, error) {
	chatID, hasHint, err := refid.ParseLarkChatIDHint(chatIDHint)
	if err != nil {
		return larkSendTarget{}, err
	}
	if hasHint {
		chatID = refid.NormalizeLarkID(chatID)
		if chatID == "" {
			return larkSendTarget{}, fmt.Errorf("invalid chat_id: %s", strings.TrimSpace(chatIDHint))
		}
		for _, raw := range contact.LarkChatIDs {
			if strings.EqualFold(refid.NormalizeLarkID(raw), chatID) {
				return larkSendTarget{ReceiveIDType: "chat_id", ReceiveID: chatID}, nil
			}
		}
		if contactChatID, ok := refid.ParseLarkChatContactID(contact.ContactID); ok && strings.EqualFold(contactChatID, chatID) {
			return larkSendTarget{ReceiveIDType: "chat_id", ReceiveID: chatID}, nil
		}
		if openID := refid.NormalizeLarkID(contact.LarkOpenID); openID != "" {
			return larkSendTarget{ReceiveIDType: "open_id", ReceiveID: openID}, nil
		}
		if openID, ok := refid.ParseLarkUserContactID(contact.ContactID); ok {
			return larkSendTarget{ReceiveIDType: "open_id", ReceiveID: openID}, nil
		}
		return larkSendTarget{}, fmt.Errorf("lark chat_id %q not found in lark_chat_ids and no lark_open_id fallback", chatID)
	}
	return ResolveLarkTarget(contact)
}

func ResolveSlackTarget(contact contacts.Contact) (slackbus.DeliveryTarget, string, error) {
	teamID := strings.TrimSpace(contact.SlackTeamID)
	if teamID != "" {
		if channelID := strings.TrimSpace(contact.SlackDMChannelID); channelID != "" {
			return slackbus.DeliveryTarget{TeamID: teamID, ChannelID: channelID}, "im", nil
		}
		channelIDs := append([]string(nil), contact.SlackChannelIDs...)
		sort.Slice(channelIDs, func(i, j int) bool { return channelIDs[i] < channelIDs[j] })
		for _, raw := range channelIDs {
			channelID := strings.TrimSpace(raw)
			if channelID == "" {
				continue
			}
			return slackbus.DeliveryTarget{
				TeamID:    teamID,
				ChannelID: channelID,
			}, slackChatTypeFromChannelID(channelID), nil
		}
	}
	if contactIDTeam, userOrChannelID, ok := parseSlackContactID(contact.ContactID); ok {
		idUpper := strings.ToUpper(userOrChannelID)
		if strings.HasPrefix(idUpper, "C") || strings.HasPrefix(idUpper, "G") || strings.HasPrefix(idUpper, "D") {
			return slackbus.DeliveryTarget{
				TeamID:    contactIDTeam,
				ChannelID: userOrChannelID,
			}, slackChatTypeFromChannelID(userOrChannelID), nil
		}
	}
	return slackbus.DeliveryTarget{}, "", fmt.Errorf("slack target not found in slack_dm_channel_id/slack_channel_ids/contact_id")
}

func ResolveSlackTargetWithChatID(contact contacts.Contact, chatIDHint string) (slackbus.DeliveryTarget, string, bool, error) {
	teamID, channelID, hasHint, err := refid.ParseSlackChatIDHint(chatIDHint)
	if err != nil {
		return slackbus.DeliveryTarget{}, "", hasHint, err
	}
	if hasHint {
		return slackbus.DeliveryTarget{
			TeamID:    teamID,
			ChannelID: channelID,
		}, slackChatTypeFromChannelID(channelID), true, nil
	}
	target, chatType, err := ResolveSlackTarget(contact)
	return target, chatType, false, err
}

func parseSlackContactID(raw string) (string, string, bool) {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(value), "slack:") {
		return "", "", false
	}
	parts := strings.Split(strings.TrimSpace(value[len("slack:"):]), ":")
	if len(parts) != 2 {
		return "", "", false
	}
	teamID := strings.TrimSpace(parts[0])
	userOrChannelID := strings.TrimSpace(parts[1])
	if teamID == "" || userOrChannelID == "" {
		return "", "", false
	}
	return teamID, userOrChannelID, true
}

func contactTelegramChatMatch(contact contacts.Contact, chatID int64) (int64, string, bool) {
	if chatID == 0 {
		return 0, "", false
	}
	if contact.TGPrivateChatID == chatID {
		return chatID, "private", true
	}
	for _, groupID := range contact.TGGroupChatIDs {
		if groupID == chatID {
			return chatID, chatTypeFromChatID(chatID), true
		}
	}
	return 0, "", false
}

func slackChatTypeFromChannelID(channelID string) string {
	channelID = strings.ToUpper(strings.TrimSpace(channelID))
	switch {
	case strings.HasPrefix(channelID, "D"):
		return "im"
	case strings.HasPrefix(channelID, "G"):
		return "private_channel"
	default:
		return "channel"
	}
}

func preferredChat(contact contacts.Contact) (int64, string, bool) {
	privateChatID := contact.TGPrivateChatID
	groupIDs := append([]int64(nil), contact.TGGroupChatIDs...)
	sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
	if privateChatID != 0 {
		return privateChatID, "private", true
	}
	for _, groupID := range groupIDs {
		if groupID != 0 {
			return groupID, chatTypeFromChatID(groupID), true
		}
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(contact.ContactID)), "tg:") && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(contact.ContactID)), "tg:@") {
		idText := strings.TrimSpace(strings.TrimSpace(contact.ContactID)[len("tg:"):])
		if chatID, err := strconv.ParseInt(idText, 10, 64); err == nil && chatID != 0 {
			return chatID, chatTypeFromChatID(chatID), true
		}
	}
	return 0, "", false
}

func chatTypeFromChatID(chatID int64) string {
	if chatID < 0 {
		return "supergroup"
	}
	return "private"
}
