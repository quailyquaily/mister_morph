package contacts

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	refid "github.com/quailyquaily/mistermorph/internal/entryutil/refid"
)

type observedContactCandidate struct {
	PrimaryContactID    string
	AlternateContactIDs []string
	Kind                Kind
	Channel             string
	Nickname            string
	TGUsername          string
	TelegramChatID      int64
	TelegramChatType    string
	TelegramIsSender    bool
	LineUserID          string
	LineChatIDs         []string
	LarkOpenID          string
	LarkChatIDs         []string
	SlackTeamID         string
	SlackUserID         string
	SlackDMChannelID    string
	SlackChannelIDs     []string
}

// ObserveInboundBusMessage inspects inbound bus messages and updates contacts.
// It is best-effort for object extraction and follows merge rules for bus-driven contact updates.
func (s *Service) ObserveInboundBusMessage(ctx context.Context, msg busruntime.BusMessage, now time.Time) error {
	if s == nil || !s.ready() {
		return fmt.Errorf("nil contacts service")
	}
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	now = normalizeNow(now)
	if msg.Direction != busruntime.DirectionInbound {
		return nil
	}

	switch msg.Channel {
	case busruntime.ChannelConsole:
		return s.observeConsoleInboundBusMessage(ctx, msg, now)
	case busruntime.ChannelTelegram:
		return s.observeTelegramInboundBusMessage(ctx, msg, now)
	case busruntime.ChannelSlack:
		return s.observeSlackInboundBusMessage(ctx, msg, now)
	case busruntime.ChannelLine:
		return s.observeLineInboundBusMessage(ctx, msg, now)
	case busruntime.ChannelLark:
		return s.observeLarkInboundBusMessage(ctx, msg, now)
	default:
		return nil
	}
}

func (s *Service) observeConsoleInboundBusMessage(ctx context.Context, msg busruntime.BusMessage, now time.Time) error {
	participantKey := strings.TrimSpace(msg.ParticipantKey)
	if participantKey == "" {
		participantKey = "console:user"
	}
	nickname := strings.TrimSpace(msg.Extensions.FromDisplayName)
	if nickname == "" {
		nickname = strings.TrimSpace(msg.Extensions.FromUsername)
	}
	if nickname == "" {
		nickname = "Console User"
	}
	return s.applyObservedCandidates(ctx, []observedContactCandidate{
		{
			PrimaryContactID: participantKey,
			Kind:             KindHuman,
			Channel:          ChannelConsole,
			Nickname:         nickname,
		},
	}, now)
}

func (s *Service) observeTelegramInboundBusMessage(ctx context.Context, msg busruntime.BusMessage, now time.Time) error {
	chatID, err := telegramChatIDFromConversationKey(msg.ConversationKey)
	if err != nil {
		return err
	}
	chatType := normalizeTelegramChatType(msg.Extensions.ChatType, chatID)
	fromUserID := msg.Extensions.FromUserID
	fromUsername := normalizeTelegramUsername(msg.Extensions.FromUsername)
	nickname := strings.TrimSpace(msg.Extensions.FromDisplayName)
	if nickname == "" {
		nickname = strings.TrimSpace(strings.Join([]string{msg.Extensions.FromFirstName, msg.Extensions.FromLastName}, " "))
	}

	candidates := make([]observedContactCandidate, 0, len(msg.Extensions.MentionUsers)+1)
	if senderContactID := telegramContactIDFromUser(fromUsername, fromUserID); senderContactID != "" {
		candidate := observedContactCandidate{
			PrimaryContactID: senderContactID,
			Kind:             KindHuman,
			Channel:          ChannelTelegram,
			Nickname:         nickname,
			TGUsername:       fromUsername,
			TelegramChatID:   chatID,
			TelegramChatType: chatType,
			TelegramIsSender: true,
		}
		if fromUsername != "" && fromUserID > 0 {
			candidate.AlternateContactIDs = append(candidate.AlternateContactIDs, "tg:"+strconv.FormatInt(fromUserID, 10))
		}
		candidates = append(candidates, candidate)
	}

	for _, rawMention := range msg.Extensions.MentionUsers {
		username := normalizeTelegramUsername(rawMention)
		if username == "" {
			continue
		}
		candidates = append(candidates, observedContactCandidate{
			PrimaryContactID: "tg:@" + username,
			Kind:             KindHuman,
			Channel:          ChannelTelegram,
			TGUsername:       username,
			TelegramChatID:   chatID,
			TelegramChatType: chatType,
			TelegramIsSender: false,
		})
	}

	return s.applyObservedCandidates(ctx, candidates, now)
}

func (s *Service) observeSlackInboundBusMessage(ctx context.Context, msg busruntime.BusMessage, now time.Time) error {
	teamID, channelID, err := slackConversationPartsFromKey(msg.ConversationKey)
	if err != nil {
		return err
	}
	chatType := normalizeSlackChatType(msg.Extensions.ChatType, channelID)
	fromUserID := normalizeSlackID(msg.Extensions.FromUserRef)
	if fromUserID == "" {
		participantTeamID, participantUserID, parseErr := parseSlackParticipantKey(msg.ParticipantKey)
		if parseErr == nil && strings.EqualFold(participantTeamID, teamID) {
			fromUserID = participantUserID
		}
	}
	nickname := strings.TrimSpace(msg.Extensions.FromDisplayName)
	if nickname == "" {
		nickname = strings.TrimSpace(msg.Extensions.FromUsername)
	}

	candidates := make([]observedContactCandidate, 0, len(msg.Extensions.MentionUsers)+1)
	if senderContactID := slackContactIDFromUser(teamID, fromUserID); senderContactID != "" {
		candidate := observedContactCandidate{
			PrimaryContactID: senderContactID,
			Kind:             KindHuman,
			Channel:          ChannelSlack,
			Nickname:         nickname,
			SlackTeamID:      teamID,
			SlackUserID:      fromUserID,
		}
		switch chatType {
		case "im":
			candidate.SlackDMChannelID = channelID
		case "channel", "private_channel", "mpim":
			candidate.SlackChannelIDs = append(candidate.SlackChannelIDs, channelID)
		}
		candidates = append(candidates, candidate)
	}

	for _, rawMention := range msg.Extensions.MentionUsers {
		userID := normalizeSlackID(rawMention)
		if userID == "" {
			continue
		}
		candidate := observedContactCandidate{
			PrimaryContactID: slackContactIDFromUser(teamID, userID),
			Kind:             KindHuman,
			Channel:          ChannelSlack,
			SlackTeamID:      teamID,
			SlackUserID:      userID,
		}
		if chatType == "channel" || chatType == "private_channel" || chatType == "mpim" {
			candidate.SlackChannelIDs = append(candidate.SlackChannelIDs, channelID)
		}
		candidates = append(candidates, candidate)
	}

	return s.applyObservedCandidates(ctx, candidates, now)
}

func (s *Service) observeLineInboundBusMessage(ctx context.Context, msg busruntime.BusMessage, now time.Time) error {
	chatID, err := lineChatIDFromConversationKey(msg.ConversationKey)
	if err != nil {
		return err
	}
	fromUserID := refid.NormalizeLineID(msg.Extensions.FromUserRef)
	if fromUserID == "" {
		fromUserID = refid.NormalizeLineID(msg.ParticipantKey)
	}
	nickname := strings.TrimSpace(msg.Extensions.FromDisplayName)
	if nickname == "" {
		nickname = strings.TrimSpace(msg.Extensions.FromUsername)
	}

	candidates := make([]observedContactCandidate, 0, len(msg.Extensions.MentionUsers)+1)
	if senderContactID := lineContactIDFromUser(fromUserID); senderContactID != "" {
		candidates = append(candidates, observedContactCandidate{
			PrimaryContactID: senderContactID,
			Kind:             KindHuman,
			Channel:          ChannelLine,
			Nickname:         nickname,
			LineUserID:       fromUserID,
			LineChatIDs:      []string{chatID},
		})
	}

	for _, rawMention := range msg.Extensions.MentionUsers {
		userID := refid.NormalizeLineID(rawMention)
		if userID == "" {
			continue
		}
		candidates = append(candidates, observedContactCandidate{
			PrimaryContactID: lineContactIDFromUser(userID),
			Kind:             KindHuman,
			Channel:          ChannelLine,
			LineUserID:       userID,
			LineChatIDs:      []string{chatID},
		})
	}

	return s.applyObservedCandidates(ctx, candidates, now)
}

func (s *Service) observeLarkInboundBusMessage(ctx context.Context, msg busruntime.BusMessage, now time.Time) error {
	chatID, err := larkChatIDFromConversationKey(msg.ConversationKey)
	if err != nil {
		return err
	}
	fromOpenID := refid.NormalizeLarkID(msg.Extensions.FromUserRef)
	if fromOpenID == "" {
		fromOpenID = refid.NormalizeLarkID(msg.ParticipantKey)
	}
	nickname := strings.TrimSpace(msg.Extensions.FromDisplayName)
	if nickname == "" {
		nickname = strings.TrimSpace(msg.Extensions.FromUsername)
	}

	candidates := make([]observedContactCandidate, 0, len(msg.Extensions.MentionUsers)+1)
	if senderContactID := larkContactIDFromUser(fromOpenID); senderContactID != "" {
		candidates = append(candidates, observedContactCandidate{
			PrimaryContactID: senderContactID,
			Kind:             KindHuman,
			Channel:          ChannelLark,
			Nickname:         nickname,
			LarkOpenID:       fromOpenID,
			LarkChatIDs:      []string{chatID},
		})
	}

	for _, rawMention := range msg.Extensions.MentionUsers {
		openID := refid.NormalizeLarkID(rawMention)
		if openID == "" {
			continue
		}
		candidates = append(candidates, observedContactCandidate{
			PrimaryContactID: larkContactIDFromUser(openID),
			Kind:             KindHuman,
			Channel:          ChannelLark,
			LarkOpenID:       openID,
			LarkChatIDs:      []string{chatID},
		})
	}

	return s.applyObservedCandidates(ctx, candidates, now)
}

func slackContactIDFromUser(teamID, userID string) string {
	teamID = normalizeSlackID(teamID)
	userID = normalizeSlackID(userID)
	if teamID == "" || userID == "" {
		return ""
	}
	return "slack:" + teamID + ":" + userID
}

func telegramContactIDFromUser(username string, userID int64) string {
	username = normalizeTelegramUsername(username)
	if username != "" {
		return "tg:@" + username
	}
	if userID > 0 {
		return "tg:" + strconv.FormatInt(userID, 10)
	}
	return ""
}

func lineContactIDFromUser(userID string) string {
	userID = refid.NormalizeLineID(userID)
	if userID == "" {
		return ""
	}
	return "line_user:" + userID
}

func larkContactIDFromUser(openID string) string {
	openID = refid.NormalizeLarkID(openID)
	if openID == "" {
		return ""
	}
	return "lark_user:" + openID
}

func telegramChatIDFromConversationKey(conversationKey string) (int64, error) {
	key := strings.TrimSpace(conversationKey)
	if !strings.HasPrefix(strings.ToLower(key), "tg:") {
		return 0, fmt.Errorf("telegram conversation key is invalid")
	}
	chatIDText := strings.TrimSpace(key[len("tg:"):])
	if chatIDText == "" {
		return 0, fmt.Errorf("telegram chat id is required")
	}
	chatID, err := strconv.ParseInt(chatIDText, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("telegram chat id is invalid: %w", err)
	}
	if chatID == 0 {
		return 0, fmt.Errorf("telegram chat id is required")
	}
	return chatID, nil
}

func normalizeTelegramChatType(chatType string, chatID int64) string {
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	switch chatType {
	case "private", "group", "supergroup":
		return chatType
	}
	if chatID < 0 {
		return "supergroup"
	}
	return "private"
}

func (s *Service) applyObservedCandidates(ctx context.Context, candidates []observedContactCandidate, now time.Time) error {
	if len(candidates) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		primaryID := strings.TrimSpace(candidate.PrimaryContactID)
		if primaryID == "" {
			continue
		}
		key := strings.ToLower(primaryID)
		if seen[key] {
			continue
		}
		seen[key] = true
		if err := s.upsertObservedCandidate(ctx, candidate, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) upsertObservedCandidate(ctx context.Context, candidate observedContactCandidate, now time.Time) error {
	now = normalizeNow(now)
	existing, found, err := s.findObservedExistingContact(ctx, candidate)
	if err != nil {
		return err
	}

	lastInteraction := now.UTC()
	if found {
		existing.Kind = candidate.Kind
		existing.Channel = strings.TrimSpace(candidate.Channel)
		if nickname := strings.TrimSpace(candidate.Nickname); nickname != "" {
			existing.ContactNickname = nickname
		}
		if username := normalizeTelegramUsername(candidate.TGUsername); username != "" {
			existing.TGUsername = username
		}
		applyObservedTelegramMerge(&existing, candidate)
		applyObservedLineMerge(&existing, candidate)
		applyObservedLarkMerge(&existing, candidate)
		applyObservedSlackMerge(&existing, candidate)
		existing.LastInteractionAt = &lastInteraction
		_, err := s.UpsertContact(ctx, existing, now)
		return err
	}

	contact := Contact{
		ContactID:         strings.TrimSpace(candidate.PrimaryContactID),
		Kind:              candidate.Kind,
		Channel:           strings.TrimSpace(candidate.Channel),
		ContactNickname:   strings.TrimSpace(candidate.Nickname),
		TGUsername:        normalizeTelegramUsername(candidate.TGUsername),
		LineUserID:        refid.NormalizeLineID(candidate.LineUserID),
		LineChatIDs:       normalizeStringSlice(candidate.LineChatIDs),
		LarkOpenID:        refid.NormalizeLarkID(candidate.LarkOpenID),
		LarkChatIDs:       normalizeStringSlice(candidate.LarkChatIDs),
		SlackTeamID:       normalizeSlackID(candidate.SlackTeamID),
		SlackUserID:       normalizeSlackID(candidate.SlackUserID),
		SlackDMChannelID:  normalizeSlackID(candidate.SlackDMChannelID),
		SlackChannelIDs:   normalizeStringSlice(candidate.SlackChannelIDs),
		LastInteractionAt: &lastInteraction,
	}
	applyObservedTelegramMerge(&contact, candidate)
	applyObservedLineMerge(&contact, candidate)
	applyObservedLarkMerge(&contact, candidate)
	applyObservedSlackMerge(&contact, candidate)
	_, err = s.UpsertContact(ctx, contact, now)
	return err
}

func (s *Service) findObservedExistingContact(ctx context.Context, candidate observedContactCandidate) (Contact, bool, error) {
	ids := append([]string{candidate.PrimaryContactID}, candidate.AlternateContactIDs...)
	seen := map[string]bool{}
	for _, raw := range ids {
		contactID := strings.TrimSpace(raw)
		if contactID == "" {
			continue
		}
		key := strings.ToLower(contactID)
		if seen[key] {
			continue
		}
		seen[key] = true
		existing, ok, err := s.GetContact(ctx, contactID)
		if err != nil {
			return Contact{}, false, err
		}
		if ok {
			return existing, true, nil
		}
	}
	return Contact{}, false, nil
}

func applyObservedTelegramMerge(contact *Contact, candidate observedContactCandidate) {
	if contact == nil {
		return
	}
	chatID := candidate.TelegramChatID
	if chatID == 0 {
		return
	}
	chatType := normalizeTelegramChatType(candidate.TelegramChatType, chatID)
	if chatType == "group" || chatType == "supergroup" {
		contact.TGGroupChatIDs = mergeObservedTGGroupChatIDs(contact.TGGroupChatIDs, chatID)
		return
	}
	if chatType == "private" && candidate.TelegramIsSender {
		if contact.TGPrivateChatID == 0 {
			contact.TGPrivateChatID = chatID
		}
	}
}

func applyObservedSlackMerge(contact *Contact, candidate observedContactCandidate) {
	if contact == nil {
		return
	}
	teamID := normalizeSlackID(candidate.SlackTeamID)
	if teamID != "" && strings.TrimSpace(contact.SlackTeamID) == "" {
		contact.SlackTeamID = teamID
	}
	userID := normalizeSlackID(candidate.SlackUserID)
	if userID != "" && strings.TrimSpace(contact.SlackUserID) == "" {
		contact.SlackUserID = userID
	}
	dmChannelID := normalizeSlackID(candidate.SlackDMChannelID)
	if dmChannelID != "" && strings.TrimSpace(contact.SlackDMChannelID) == "" {
		contact.SlackDMChannelID = dmChannelID
	}
	if len(candidate.SlackChannelIDs) > 0 {
		contact.SlackChannelIDs = mergeSlackChannelIDs(contact.SlackChannelIDs, candidate.SlackChannelIDs...)
	}
}

func applyObservedLineMerge(contact *Contact, candidate observedContactCandidate) {
	if contact == nil {
		return
	}
	userID := refid.NormalizeLineID(candidate.LineUserID)
	if userID != "" && strings.TrimSpace(contact.LineUserID) == "" {
		contact.LineUserID = userID
	}
	if len(candidate.LineChatIDs) > 0 {
		contact.LineChatIDs = mergeLineChatIDs(contact.LineChatIDs, candidate.LineChatIDs...)
	}
	if strings.TrimSpace(contact.ContactID) == "" && userID != "" {
		contact.ContactID = "line_user:" + userID
	}
	if strings.TrimSpace(contact.ContactID) == "" && len(contact.LineChatIDs) > 0 {
		contact.ContactID = "line:" + contact.LineChatIDs[0]
	}
}

func applyObservedLarkMerge(contact *Contact, candidate observedContactCandidate) {
	if contact == nil {
		return
	}
	openID := refid.NormalizeLarkID(candidate.LarkOpenID)
	if openID != "" && strings.TrimSpace(contact.LarkOpenID) == "" {
		contact.LarkOpenID = openID
	}
	if len(candidate.LarkChatIDs) > 0 {
		contact.LarkChatIDs = mergeLarkChatIDs(contact.LarkChatIDs, candidate.LarkChatIDs...)
	}
	if strings.TrimSpace(contact.ContactID) == "" && openID != "" {
		contact.ContactID = "lark_user:" + openID
	}
	if strings.TrimSpace(contact.ContactID) == "" && len(contact.LarkChatIDs) > 0 {
		contact.ContactID = "lark:" + contact.LarkChatIDs[0]
	}
}

func mergeObservedTGGroupChatIDs(base []int64, chatID int64) []int64 {
	if chatID == 0 {
		return normalizeInt64Slice(base)
	}
	out := append([]int64(nil), base...)
	out = append(out, chatID)
	return normalizeInt64Slice(out)
}

func mergeSlackChannelIDs(base []string, channelIDs ...string) []string {
	out := append([]string(nil), base...)
	out = append(out, channelIDs...)
	for i := range out {
		out[i] = normalizeSlackID(out[i])
	}
	return normalizeStringSlice(out)
}

func mergeLineChatIDs(base []string, chatIDs ...string) []string {
	out := append([]string(nil), base...)
	out = append(out, chatIDs...)
	for i := range out {
		out[i] = refid.NormalizeLineID(out[i])
	}
	return normalizeStringSlice(out)
}

func mergeLarkChatIDs(base []string, chatIDs ...string) []string {
	out := append([]string(nil), base...)
	out = append(out, chatIDs...)
	for i := range out {
		out[i] = refid.NormalizeLarkID(out[i])
	}
	return normalizeStringSlice(out)
}

func slackConversationPartsFromKey(conversationKey string) (string, string, error) {
	const prefix = "slack:"
	key := strings.TrimSpace(conversationKey)
	if !strings.HasPrefix(strings.ToLower(key), prefix) {
		return "", "", fmt.Errorf("slack conversation key is invalid")
	}
	raw := strings.TrimSpace(key[len(prefix):])
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("slack conversation key is invalid")
	}
	teamID := normalizeSlackID(parts[0])
	channelID := normalizeSlackID(parts[1])
	if teamID == "" || channelID == "" {
		return "", "", fmt.Errorf("slack conversation key is invalid")
	}
	return teamID, channelID, nil
}

func parseSlackParticipantKey(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("slack participant key is invalid")
	}
	teamID := normalizeSlackID(parts[0])
	userID := normalizeSlackID(parts[1])
	if teamID == "" || userID == "" {
		return "", "", fmt.Errorf("slack participant key is invalid")
	}
	return teamID, userID, nil
}

func lineChatIDFromConversationKey(conversationKey string) (string, error) {
	const prefix = "line:"
	key := strings.TrimSpace(conversationKey)
	if !strings.HasPrefix(strings.ToLower(key), prefix) {
		return "", fmt.Errorf("line conversation key is invalid")
	}
	chatID := refid.NormalizeLineID(key[len(prefix):])
	if chatID == "" {
		return "", fmt.Errorf("line chat id is required")
	}
	return chatID, nil
}

func larkChatIDFromConversationKey(conversationKey string) (string, error) {
	const prefix = "lark:"
	key := strings.TrimSpace(conversationKey)
	if !strings.HasPrefix(strings.ToLower(key), prefix) {
		return "", fmt.Errorf("lark conversation key is invalid")
	}
	chatID := refid.NormalizeLarkID(key[len(prefix):])
	if chatID == "" {
		return "", fmt.Errorf("lark chat id is required")
	}
	return chatID, nil
}

func normalizeSlackChatType(chatType string, channelID string) string {
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	switch chatType {
	case "im", "channel", "private_channel", "mpim":
		return chatType
	}
	switch {
	case strings.HasPrefix(strings.ToUpper(strings.TrimSpace(channelID)), "D"):
		return "im"
	case strings.HasPrefix(strings.ToUpper(strings.TrimSpace(channelID)), "C"):
		return "channel"
	case strings.HasPrefix(strings.ToUpper(strings.TrimSpace(channelID)), "G"):
		return "private_channel"
	default:
		return "channel"
	}
}
