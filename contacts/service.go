package contacts

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	refid "github.com/quailyquaily/mistermorph/internal/entryutil/refid"
	"github.com/quailyquaily/mistermorph/internal/idempotency"
)

const (
	defaultFailureCooldown = 72 * time.Hour
)

type Sender interface {
	Send(ctx context.Context, contact Contact, decision ShareDecision) (accepted bool, deduped bool, err error)
}

type ServiceOptions struct {
	FailureCooldown time.Duration
}

type EnsureStore interface {
	Ensure(ctx context.Context) error
}

type ContactStore interface {
	GetContact(ctx context.Context, contactID string) (Contact, bool, error)
	PutContact(ctx context.Context, contact Contact) error
	ListContacts(ctx context.Context, status Status) ([]Contact, error)
}

type OutboxStore interface {
	GetBusOutboxRecord(ctx context.Context, channel string, idempotencyKey string) (BusOutboxRecord, bool, error)
	PutBusOutboxRecord(ctx context.Context, record BusOutboxRecord) error
}

type ServiceDeps struct {
	Ensure   EnsureStore
	Contacts ContactStore
	Outbox   OutboxStore
}

type Service struct {
	ensureStore     EnsureStore
	contactStore    ContactStore
	outboxStore     OutboxStore
	failureCooldown time.Duration
}

func NewService(store Store) *Service {
	return NewServiceWithOptions(store, ServiceOptions{})
}

func NewServiceWithOptions(store Store, opts ServiceOptions) *Service {
	return NewServiceWithDeps(ServiceDeps{
		Ensure:   store,
		Contacts: store,
		Outbox:   store,
	}, opts)
}

func NewServiceWithDeps(deps ServiceDeps, opts ServiceOptions) *Service {
	opts = normalizeServiceOptions(opts)
	return &Service{
		ensureStore:     deps.Ensure,
		contactStore:    deps.Contacts,
		outboxStore:     deps.Outbox,
		failureCooldown: opts.FailureCooldown,
	}
}

func (s *Service) ready() bool {
	return s != nil &&
		s.ensureStore != nil &&
		s.contactStore != nil &&
		s.outboxStore != nil
}

func normalizeServiceOptions(opts ServiceOptions) ServiceOptions {
	if opts.FailureCooldown <= 0 {
		opts.FailureCooldown = defaultFailureCooldown
	}
	return opts
}

func (s *Service) UpsertContact(ctx context.Context, contact Contact, now time.Time) (Contact, error) {
	if s == nil || !s.ready() {
		return Contact{}, fmt.Errorf("nil contacts service")
	}
	now = normalizeNow(now)
	if err := s.ensureStore.Ensure(ctx); err != nil {
		return Contact{}, err
	}

	input := contact
	contact = normalizeContact(contact, now)
	if strings.TrimSpace(contact.ContactID) == "" {
		contact.ContactID = deriveContactID(contact)
	}
	if strings.TrimSpace(contact.ContactID) == "" {
		return Contact{}, fmt.Errorf("contact_id is required")
	}

	existing, ok, err := s.contactStore.GetContact(ctx, contact.ContactID)
	if err != nil {
		return Contact{}, err
	}
	if ok {
		if input.Kind == "" {
			contact.Kind = existing.Kind
		}
		if strings.TrimSpace(input.Channel) == "" {
			contact.Channel = strings.TrimSpace(existing.Channel)
		}
		if strings.TrimSpace(contact.ContactNickname) == "" && strings.TrimSpace(existing.ContactNickname) != "" {
			contact.ContactNickname = strings.TrimSpace(existing.ContactNickname)
		}
		if strings.TrimSpace(contact.PersonaBrief) == "" && strings.TrimSpace(existing.PersonaBrief) != "" {
			contact.PersonaBrief = strings.TrimSpace(existing.PersonaBrief)
		}
		if strings.TrimSpace(contact.TGUsername) == "" && strings.TrimSpace(existing.TGUsername) != "" {
			contact.TGUsername = strings.TrimSpace(existing.TGUsername)
		}
		if contact.TGPrivateChatID == 0 && existing.TGPrivateChatID != 0 {
			contact.TGPrivateChatID = existing.TGPrivateChatID
		}
		if len(contact.TGGroupChatIDs) == 0 && len(existing.TGGroupChatIDs) > 0 {
			contact.TGGroupChatIDs = append([]int64(nil), existing.TGGroupChatIDs...)
		}
		if len(contact.TopicPreferences) == 0 && len(existing.TopicPreferences) > 0 {
			contact.TopicPreferences = append([]string(nil), existing.TopicPreferences...)
		}
		if contact.CooldownUntil == nil && existing.CooldownUntil != nil {
			ts := existing.CooldownUntil.UTC()
			contact.CooldownUntil = &ts
		}
		if contact.LastInteractionAt == nil && existing.LastInteractionAt != nil {
			ts := existing.LastInteractionAt.UTC()
			contact.LastInteractionAt = &ts
		}
	}

	contact = normalizeContact(contact, now)
	if strings.TrimSpace(contact.ContactID) == "" {
		contact.ContactID = deriveContactID(contact)
	}
	if strings.TrimSpace(contact.ContactID) == "" {
		return Contact{}, fmt.Errorf("contact_id is required")
	}
	if err := s.contactStore.PutContact(ctx, contact); err != nil {
		return Contact{}, err
	}
	return contact, nil
}

func (s *Service) ListContacts(ctx context.Context, status Status) ([]Contact, error) {
	if s == nil || !s.ready() {
		return nil, fmt.Errorf("nil contacts service")
	}
	return s.contactStore.ListContacts(ctx, status)
}

func (s *Service) GetContact(ctx context.Context, contactID string) (Contact, bool, error) {
	if s == nil || !s.ready() {
		return Contact{}, false, fmt.Errorf("nil contacts service")
	}
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return Contact{}, false, fmt.Errorf("contact_id is required")
	}
	return s.contactStore.GetContact(ctx, contactID)
}

func (s *Service) ResolveSendContact(ctx context.Context, contactID string) (Contact, error) {
	if s == nil || !s.ready() {
		return Contact{}, fmt.Errorf("nil contacts service")
	}
	if err := s.ensureStore.Ensure(ctx); err != nil {
		return Contact{}, err
	}
	contact, ok, err := s.resolveSendContact(ctx, contactID)
	if err != nil {
		return Contact{}, err
	}
	if !ok {
		return Contact{}, contactNotFoundError(contactID)
	}
	return contact, nil
}

func (s *Service) SendDecision(ctx context.Context, now time.Time, decision ShareDecision, sender Sender) (ShareOutcome, error) {
	if s == nil || !s.ready() {
		return ShareOutcome{}, fmt.Errorf("nil contacts service")
	}
	if sender == nil {
		return ShareOutcome{}, fmt.Errorf("sender is required")
	}
	now = normalizeNow(now)
	if err := s.ensureStore.Ensure(ctx); err != nil {
		return ShareOutcome{}, err
	}

	decision.ContactID = strings.TrimSpace(decision.ContactID)
	if decision.ContactID == "" {
		return ShareOutcome{}, fmt.Errorf("contact_id is required")
	}
	contact, ok, err := s.resolveSendContact(ctx, decision.ContactID)
	if err != nil {
		return ShareOutcome{}, err
	}
	if !ok {
		return ShareOutcome{}, contactNotFoundError(decision.ContactID)
	}
	decision.ContactID = contact.ContactID
	decision.ContentType = strings.TrimSpace(decision.ContentType)
	if decision.ContentType == "" {
		decision.ContentType = "application/json"
	}
	decision.PayloadBase64 = strings.TrimSpace(decision.PayloadBase64)
	if decision.PayloadBase64 == "" {
		return ShareOutcome{}, fmt.Errorf("payload_base64 is required")
	}
	if _, err := base64.RawURLEncoding.DecodeString(decision.PayloadBase64); err != nil {
		return ShareOutcome{}, fmt.Errorf("payload_base64 decode failed: %w", err)
	}
	decision.ItemID = strings.TrimSpace(decision.ItemID)
	if decision.ItemID == "" {
		decision.ItemID = "manual_" + uuid.NewString()
	}
	decision.IdempotencyKey = strings.TrimSpace(decision.IdempotencyKey)
	if decision.IdempotencyKey == "" {
		decision.IdempotencyKey = idempotency.ManualContactKey(contact.ContactID)
	}
	recipientContacts, err := s.resolveDecisionRecipientContacts(ctx, contact, decision.RecipientContactIDs)
	if err != nil {
		return ShareOutcome{}, err
	}

	outcome, attempted, err := s.sendWithBusOutbox(ctx, now, contact, decision, sender)
	if err != nil {
		return ShareOutcome{}, err
	}
	if attempted {
		if err := s.applySendOutcomeToContacts(ctx, now, recipientContacts, outcome); err != nil {
			return ShareOutcome{}, err
		}
	}
	return outcome, nil
}

func (s *Service) resolveDecisionRecipientContacts(ctx context.Context, primary Contact, recipientContactIDs []string) ([]Contact, error) {
	out := make([]Contact, 0, 1+len(recipientContactIDs))
	seen := map[string]bool{}
	add := func(contact Contact) {
		contactID := strings.TrimSpace(contact.ContactID)
		if contactID == "" {
			return
		}
		key := strings.ToLower(contactID)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, contact)
	}
	add(primary)
	for _, raw := range recipientContactIDs {
		contactID := strings.TrimSpace(raw)
		if contactID == "" {
			continue
		}
		contact, ok, err := s.resolveSendContact(ctx, contactID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, contactNotFoundError(contactID)
		}
		add(contact)
	}
	return out, nil
}

func (s *Service) applySendOutcomeToContacts(ctx context.Context, now time.Time, recipientContacts []Contact, outcome ShareOutcome) error {
	for _, contact := range recipientContacts {
		if contact.Synthetic {
			continue
		}
		if outcome.Error != "" {
			cooldown := now.Add(s.failureCooldown)
			contact.CooldownUntil = &cooldown
		} else {
			ts := now
			contact.LastInteractionAt = &ts
			contact.CooldownUntil = nil
		}
		if err := s.contactStore.PutContact(ctx, contact); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) resolveSendContact(ctx context.Context, contactID string) (Contact, bool, error) {
	if s == nil || s.contactStore == nil {
		return Contact{}, false, fmt.Errorf("contact store is required")
	}
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return Contact{}, false, fmt.Errorf("contact_id is required")
	}
	item, ok, err := s.contactStore.GetContact(ctx, contactID)
	if err != nil || ok {
		return item, ok, err
	}
	if contact, syntheticOK, syntheticErr := syntheticChatContact(contactID); syntheticErr != nil {
		return Contact{}, false, syntheticErr
	} else if syntheticOK {
		return contact, true, nil
	}
	username := extractTelegramUsernameRef(contactID)
	if username == "" {
		return Contact{}, false, nil
	}
	records, err := s.contactStore.ListContacts(ctx, "")
	if err != nil {
		return Contact{}, false, err
	}
	matches := make([]Contact, 0, 1)
	for _, candidate := range records {
		if candidate.TGPrivateChatID == 0 {
			continue
		}
		if !strings.EqualFold(telegramUsernameOfContact(candidate), username) {
			continue
		}
		matches = append(matches, candidate)
	}
	switch len(matches) {
	case 0:
		return Contact{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		ids := make([]string, 0, len(matches))
		for _, candidate := range matches {
			ids = append(ids, strings.TrimSpace(candidate.ContactID))
		}
		sort.Strings(ids)
		return Contact{}, false, fmt.Errorf("telegram username %q matches multiple contacts with private chat id: %s", username, strings.Join(ids, ", "))
	}
}

func contactNotFoundError(contactID string) error {
	contactID = strings.TrimSpace(contactID)
	if protocol, id, ok := refid.Parse(contactID); ok {
		switch protocol {
		case "tg", "slack", "line", "line_user", "lark", "lark_user":
			return fmt.Errorf("contact not found: %s", contactID)
		default:
			return fmt.Errorf("hint: protocol '%q' is not mapped. Try to find other ways to send to '%s' in protocol/tool '%s'.", protocol, id, protocol)
		}
	}
	return fmt.Errorf("contact not found: %s", contactID)
}

func syntheticChatContact(contactID string) (Contact, bool, error) {
	value := strings.TrimSpace(contactID)
	if value == "" {
		return Contact{}, false, nil
	}
	if strings.HasPrefix(strings.ToLower(value), "tg:@") {
		return Contact{}, false, nil
	}
	if strings.HasPrefix(strings.ToLower(value), "tg:") {
		chatID, _, err := refid.ParseTelegramChatIDHint(value)
		if err != nil {
			return Contact{}, false, err
		}
		contact := Contact{
			ContactID: value,
			Synthetic: true,
			Kind:      KindHuman,
			Channel:   ChannelTelegram,
		}
		if chatID > 0 {
			contact.TGPrivateChatID = chatID
		} else {
			contact.TGGroupChatIDs = []int64{chatID}
		}
		return contact, true, nil
	}
	if teamID, channelID, ok, err := refid.ParseSlackChatIDHint(value); err != nil {
		return Contact{}, false, err
	} else if ok {
		return Contact{
			ContactID:       value,
			Synthetic:       true,
			Kind:            KindHuman,
			Channel:         ChannelSlack,
			SlackTeamID:     teamID,
			SlackChannelIDs: []string{channelID},
		}, true, nil
	}
	if chatID, ok, err := refid.ParseLineChatIDHint(value); err != nil {
		return Contact{}, false, err
	} else if ok {
		return Contact{
			ContactID:   value,
			Synthetic:   true,
			Kind:        KindHuman,
			Channel:     ChannelLine,
			LineChatIDs: []string{chatID},
		}, true, nil
	}
	if chatID, ok, err := refid.ParseLarkChatIDHint(value); err != nil {
		return Contact{}, false, err
	} else if ok {
		return Contact{
			ContactID:   value,
			Synthetic:   true,
			Kind:        KindHuman,
			Channel:     ChannelLark,
			LarkChatIDs: []string{chatID},
		}, true, nil
	}
	return Contact{}, false, nil
}

func extractTelegramUsernameRef(contactID string) string {
	contactID = strings.TrimSpace(contactID)
	if !strings.HasPrefix(strings.ToLower(contactID), "tg:@") {
		return ""
	}
	return normalizeTelegramUsername(contactID[len("tg:@"):])
}

func telegramUsernameOfContact(contact Contact) string {
	if username := normalizeTelegramUsername(contact.TGUsername); username != "" {
		return username
	}
	contactID := strings.TrimSpace(contact.ContactID)
	if strings.HasPrefix(strings.ToLower(contactID), "tg:@") {
		return normalizeTelegramUsername(contactID[len("tg:@"):])
	}
	return ""
}

func ResolveDecisionChannel(contact Contact, decision ShareDecision) (string, error) {
	if channel, hasHint, err := resolveChannelFromChatIDHint(decision.ChatID); hasHint || err != nil {
		return channel, err
	}
	switch normalizeContactChannel(contact.Channel) {
	case ChannelSlack:
		if hasSlackTarget(contact) {
			return ChannelSlack, nil
		}
	case ChannelTelegram:
		if hasTelegramTarget(contact) {
			return ChannelTelegram, nil
		}
	case ChannelLine:
		if hasLineTarget(contact) {
			return ChannelLine, nil
		}
	case ChannelLark:
		if hasLarkTarget(contact) {
			return ChannelLark, nil
		}
	}
	if hasSlackTarget(contact) {
		return ChannelSlack, nil
	}
	if hasTelegramTarget(contact) {
		return ChannelTelegram, nil
	}
	if hasLineTarget(contact) {
		return ChannelLine, nil
	}
	if hasLarkTarget(contact) {
		return ChannelLark, nil
	}
	return "", fmt.Errorf("unable to resolve delivery channel for contact_id=%s", contact.ContactID)
}

func (s *Service) sendWithBusOutbox(ctx context.Context, now time.Time, contact Contact, decision ShareDecision, sender Sender) (ShareOutcome, bool, error) {
	if s == nil || s.outboxStore == nil {
		return ShareOutcome{}, false, fmt.Errorf("outbox store is required")
	}
	channel, err := ResolveDecisionChannel(contact, decision)
	if err != nil {
		return ShareOutcome{}, false, err
	}
	if _, keyErr := busOutboxRecordKey(channel, decision.IdempotencyKey); keyErr != nil {
		return ShareOutcome{}, false, keyErr
	}

	outcome := ShareOutcome{
		ContactID:      decision.ContactID,
		PeerID:         decision.PeerID,
		ItemID:         decision.ItemID,
		IdempotencyKey: decision.IdempotencyKey,
		SentAt:         now,
	}

	existing, exists, err := s.outboxStore.GetBusOutboxRecord(ctx, channel, decision.IdempotencyKey)
	if err != nil {
		return ShareOutcome{}, false, err
	}
	if exists {
		if existing.Status == BusDeliveryStatusSent {
			outcome.Accepted = existing.Accepted
			outcome.Deduped = true
			if existing.SentAt != nil {
				outcome.SentAt = existing.SentAt.UTC()
			}
			return outcome, false, nil
		}
	}

	baseRecord := BusOutboxRecord{
		Channel:        channel,
		IdempotencyKey: decision.IdempotencyKey,
		ContactID:      decision.ContactID,
		PeerID:         decision.PeerID,
		ItemID:         decision.ItemID,
		ContentType:    decision.ContentType,
		PayloadBase64:  decision.PayloadBase64,
	}
	var current *BusOutboxRecord
	if exists {
		current = &existing
	}
	pendingRecord, err := NextOutboxRecord(current, baseRecord, OutboxTransition{
		Type: OutboxTransitionStartAttempt,
	}, now)
	if err != nil {
		return ShareOutcome{}, false, err
	}
	if err := s.outboxStore.PutBusOutboxRecord(ctx, pendingRecord); err != nil {
		return ShareOutcome{}, false, err
	}

	accepted, deduped, sendErr := sender.Send(ctx, contact, decision)
	if sendErr != nil {
		outcome.Error = sendErr.Error()
		failedRecord, err := NextOutboxRecord(&pendingRecord, baseRecord, OutboxTransition{
			Type:      OutboxTransitionMarkFailed,
			ErrorText: outcome.Error,
		}, now)
		if err != nil {
			return ShareOutcome{}, false, err
		}
		if err := s.outboxStore.PutBusOutboxRecord(ctx, failedRecord); err != nil {
			return ShareOutcome{}, false, err
		}
		return outcome, true, nil
	}

	outcome.Accepted = accepted
	outcome.Deduped = deduped
	sentRecord, err := NextOutboxRecord(&pendingRecord, baseRecord, OutboxTransition{
		Type:     OutboxTransitionMarkSent,
		Accepted: accepted,
		Deduped:  deduped,
	}, now)
	if err != nil {
		return ShareOutcome{}, false, err
	}
	if err := s.outboxStore.PutBusOutboxRecord(ctx, sentRecord); err != nil {
		return ShareOutcome{}, false, err
	}
	return outcome, true, nil
}

func hasTelegramTarget(contact Contact) bool {
	if contact.TGPrivateChatID != 0 {
		return true
	}
	if len(contact.TGGroupChatIDs) > 0 {
		return true
	}
	v := strings.TrimSpace(strings.ToLower(contact.ContactID))
	if strings.HasPrefix(v, "tg:@") {
		return true
	}
	if strings.HasPrefix(v, "tg:") && !strings.HasPrefix(v, "tg:@") {
		_, err := strconv.ParseInt(strings.TrimSpace(contact.ContactID[len("tg:"):]), 10, 64)
		return err == nil
	}
	return false
}

func hasSlackTarget(contact Contact) bool {
	if strings.TrimSpace(contact.SlackDMChannelID) != "" {
		return true
	}
	for _, raw := range contact.SlackChannelIDs {
		if strings.TrimSpace(raw) != "" {
			return true
		}
	}
	if _, userOrChannelID, ok := parseSlackContactID(contact.ContactID); ok {
		idUpper := strings.ToUpper(userOrChannelID)
		return strings.HasPrefix(idUpper, "C") || strings.HasPrefix(idUpper, "G") || strings.HasPrefix(idUpper, "D")
	}
	return false
}

func hasLineTarget(contact Contact) bool {
	if strings.TrimSpace(contact.LineUserID) != "" {
		return true
	}
	for _, raw := range contact.LineChatIDs {
		if refid.NormalizeLineID(raw) != "" {
			return true
		}
	}
	if userID, ok := refid.ParseLineUserContactID(contact.ContactID); ok && userID != "" {
		return true
	}
	chatID, ok := refid.ParseLineChatContactID(contact.ContactID)
	return ok && chatID != ""
}

func hasLarkTarget(contact Contact) bool {
	if refid.NormalizeLarkID(contact.LarkOpenID) != "" {
		return true
	}
	for _, raw := range contact.LarkChatIDs {
		if refid.NormalizeLarkID(raw) != "" {
			return true
		}
	}
	if openID, ok := refid.ParseLarkUserContactID(contact.ContactID); ok && openID != "" {
		return true
	}
	chatID, ok := refid.ParseLarkChatContactID(contact.ContactID)
	return ok && chatID != ""
}

func deriveContactID(contact Contact) string {
	if v := strings.TrimSpace(contact.ContactID); v != "" {
		return v
	}
	if contact.TGPrivateChatID > 0 {
		return "tg:" + strconv.FormatInt(contact.TGPrivateChatID, 10)
	}
	if v := normalizeTelegramUsername(contact.TGUsername); v != "" {
		return "tg:@" + v
	}
	if teamID, userID := strings.TrimSpace(contact.SlackTeamID), strings.TrimSpace(contact.SlackUserID); teamID != "" && userID != "" {
		return "slack:" + teamID + ":" + userID
	}
	if teamID, dmChannelID := strings.TrimSpace(contact.SlackTeamID), strings.TrimSpace(contact.SlackDMChannelID); teamID != "" && dmChannelID != "" {
		return "slack:" + teamID + ":" + dmChannelID
	}
	if strings.EqualFold(strings.TrimSpace(contact.Channel), ChannelSlack) {
		teamID := strings.TrimSpace(contact.SlackTeamID)
		if teamID != "" {
			for _, raw := range contact.SlackChannelIDs {
				channelID := strings.TrimSpace(raw)
				if channelID != "" {
					return "slack:" + teamID + ":" + channelID
				}
			}
		}
	}
	if userID := refid.NormalizeLineID(contact.LineUserID); userID != "" {
		return "line_user:" + userID
	}
	for _, raw := range normalizeStringSlice(contact.LineChatIDs) {
		chatID := refid.NormalizeLineID(raw)
		if chatID != "" {
			return "line:" + chatID
		}
	}
	if strings.EqualFold(strings.TrimSpace(contact.Channel), ChannelLine) {
		for _, raw := range normalizeStringSlice(contact.LineChatIDs) {
			chatID := refid.NormalizeLineID(raw)
			if chatID != "" {
				return "line:" + chatID
			}
		}
	}
	if openID := refid.NormalizeLarkID(contact.LarkOpenID); openID != "" {
		return "lark_user:" + openID
	}
	for _, raw := range normalizeStringSlice(contact.LarkChatIDs) {
		chatID := refid.NormalizeLarkID(raw)
		if chatID != "" {
			return "lark:" + chatID
		}
	}
	if strings.EqualFold(strings.TrimSpace(contact.Channel), ChannelLark) {
		for _, raw := range normalizeStringSlice(contact.LarkChatIDs) {
			chatID := refid.NormalizeLarkID(raw)
			if chatID != "" {
				return "lark:" + chatID
			}
		}
	}
	if contact.Channel == ChannelTelegram {
		ids := append([]int64(nil), contact.TGGroupChatIDs...)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			if id != 0 {
				return "tg:" + strconv.FormatInt(id, 10)
			}
		}
	}
	return ""
}

func normalizeNow(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func resolveChannelFromChatIDHint(chatID string) (string, bool, error) {
	value := strings.TrimSpace(chatID)
	if value == "" {
		return "", false, nil
	}
	if protocol, _, ok := refid.Parse(value); ok {
		switch protocol {
		case "slack":
			_, _, _, err := refid.ParseSlackChatIDHint(value)
			return ChannelSlack, true, err
		case "line":
			_, _, err := refid.ParseLineChatIDHint(value)
			return ChannelLine, true, err
		case "lark":
			_, _, err := refid.ParseLarkChatIDHint(value)
			return ChannelLark, true, err
		case "tg":
			_, _, err := refid.ParseTelegramChatIDHint(value)
			return ChannelTelegram, true, err
		default:
			return "", true, fmt.Errorf("invalid chat_id: %s", value)
		}
	}
	return "", true, fmt.Errorf("invalid chat_id: %s", value)
}
