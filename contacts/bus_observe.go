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
	TelegramUserID      int64
	TelegramChatID      int64
	TelegramChatType    string
	TelegramIsSender    bool
	LineUserID          string
	LineChatIDs         []string
	LarkOpenID          string
	LarkChatIDs         []string
	MixinUserID         string
	MixinIdentityNumber string
	MixinChatIDs        []string
	SlackTeamID         string
	SlackUserID         string
	SlackDMChannelID    string
	SlackChannelIDs     []string
}

type ObserveInboundResult struct {
	SenderContactID string
}

// ObserveInboundBusMessage inspects inbound bus messages and updates contacts.
// It is best-effort for object extraction and follows merge rules for bus-driven contact updates.
func (s *Service) ObserveInboundBusMessage(ctx context.Context, msg busruntime.BusMessage, now time.Time) error {
	_, err := s.ObserveInboundBusMessageWithResult(ctx, msg, now)
	return err
}

// ObserveInboundBusMessageWithResult also returns the stored sender identity.
func (s *Service) ObserveInboundBusMessageWithResult(ctx context.Context, msg busruntime.BusMessage, now time.Time) (ObserveInboundResult, error) {
	if s == nil || !s.ready() {
		return ObserveInboundResult{}, fmt.Errorf("nil contacts service")
	}
	if ctx == nil {
		return ObserveInboundResult{}, fmt.Errorf("context is required")
	}
	now = normalizeNow(now)
	if msg.Direction != busruntime.DirectionInbound {
		return ObserveInboundResult{}, nil
	}

	switch msg.Channel {
	case busruntime.ChannelConsole:
		return ObserveInboundResult{}, s.observeConsoleInboundBusMessage(ctx, msg, now)
	case busruntime.ChannelTelegram:
		return s.observeTelegramInboundBusMessage(ctx, msg, now)
	case busruntime.ChannelSlack:
		return ObserveInboundResult{}, s.observeSlackInboundBusMessage(ctx, msg, now)
	case busruntime.ChannelLine:
		return ObserveInboundResult{}, s.observeLineInboundBusMessage(ctx, msg, now)
	case busruntime.ChannelLark:
		return ObserveInboundResult{}, s.observeLarkInboundBusMessage(ctx, msg, now)
	case busruntime.ChannelMixin:
		return ObserveInboundResult{}, s.observeMixinInboundBusMessage(ctx, msg, now)
	default:
		return ObserveInboundResult{}, nil
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

func (s *Service) observeTelegramInboundBusMessage(ctx context.Context, msg busruntime.BusMessage, now time.Time) (ObserveInboundResult, error) {
	chatID, err := telegramChatIDFromConversationKey(msg.ConversationKey)
	if err != nil {
		return ObserveInboundResult{}, err
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
		kind := KindHuman
		if msg.Extensions.FromIsAgent {
			kind = KindAgent
		}
		candidate := observedContactCandidate{
			PrimaryContactID: senderContactID,
			Kind:             kind,
			Channel:          ChannelTelegram,
			Nickname:         nickname,
			TGUsername:       fromUsername,
			TelegramUserID:   fromUserID,
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

	contacts, err := s.applyObservedCandidatesWithResult(ctx, candidates, now)
	if err != nil || len(contacts) == 0 {
		return ObserveInboundResult{}, err
	}
	return ObserveInboundResult{SenderContactID: contacts[0].ContactID}, nil
}

func (s *Service) observeSlackInboundBusMessage(ctx context.Context, msg busruntime.BusMessage, now time.Time) error {
	teamID, channelID, err := slackConversationPartsFromKey(msg.ConversationKey)
	if err != nil {
		return err
	}
	chatType := normalizeSlackChatType(msg.Extensions.ChatType, channelID)
	fromUserID := strings.TrimSpace(msg.Extensions.FromUserRef)
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
		kind := KindHuman
		if msg.Extensions.FromIsAgent {
			kind = KindAgent
		}
		candidate := observedContactCandidate{
			PrimaryContactID: senderContactID,
			Kind:             kind,
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
		userID := strings.TrimSpace(rawMention)
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

func (s *Service) observeMixinInboundBusMessage(ctx context.Context, msg busruntime.BusMessage, now time.Time) error {
	conversationID, err := busruntime.ParseMixinConversationKey(msg.ConversationKey)
	if err != nil {
		return err
	}
	userID := refid.NormalizeMixinID(msg.Extensions.FromUserRef)
	if userID == "" {
		userID = refid.NormalizeMixinID(msg.ParticipantKey)
	}
	if userID == "" {
		return nil
	}
	identityNumber := strings.TrimSpace(msg.Extensions.FromUsername)
	nickname := strings.TrimSpace(msg.Extensions.FromDisplayName)
	if nickname == "" && identityNumber != "" {
		nickname = "@" + strings.TrimPrefix(identityNumber, "@")
	}
	kind := KindHuman
	if msg.Extensions.FromIsAgent {
		kind = KindAgent
	}
	return s.applyObservedCandidates(ctx, []observedContactCandidate{{
		PrimaryContactID:    "mixin:" + userID,
		Kind:                kind,
		Channel:             ChannelMixin,
		Nickname:            nickname,
		MixinUserID:         userID,
		MixinIdentityNumber: identityNumber,
		MixinChatIDs:        []string{conversationID},
	}}, now)
}

func slackContactIDFromUser(teamID, userID string) string {
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
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
	chatID, _, err := busruntime.ParseTelegramConversationKey(conversationKey)
	if err != nil {
		return 0, err
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
	_, err := s.applyObservedCandidatesWithResult(ctx, candidates, now)
	return err
}

func (s *Service) applyObservedCandidatesWithResult(ctx context.Context, candidates []observedContactCandidate, now time.Time) ([]Contact, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	contacts := make([]Contact, 0, len(candidates))
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
		contact, err := s.upsertObservedCandidate(ctx, candidate, now)
		if err != nil {
			return nil, err
		}
		contacts = append(contacts, contact)
	}
	return contacts, nil
}

func (s *Service) upsertObservedCandidate(ctx context.Context, candidate observedContactCandidate, now time.Time) (Contact, error) {
	now = normalizeNow(now)
	existing, found, err := s.findObservedExistingContact(ctx, candidate)
	if err != nil {
		return Contact{}, err
	}

	lastInteraction := now.UTC()
	if found {
		if existing.Kind != KindAgent || candidate.Kind == KindAgent {
			existing.Kind = candidate.Kind
		}
		existing.Channel = strings.TrimSpace(candidate.Channel)
		if nickname := strings.TrimSpace(candidate.Nickname); nickname != "" {
			replaceNickname := existing.Channel != ChannelTelegram
			if !replaceNickname {
				replaceNickname = shouldReplaceTelegramNickname(existing.ContactNickname, existing.TGUsername, candidate.TGUsername)
			}
			if replaceNickname {
				existing.ContactNickname = nickname
			}
		}
		if username := normalizeTelegramUsername(candidate.TGUsername); username != "" {
			existing.TGUsername = username
		}
		applyObservedTelegramMerge(&existing, candidate)
		applyObservedLineMerge(&existing, candidate)
		applyObservedLarkMerge(&existing, candidate)
		applyObservedMixinMerge(&existing, candidate)
		applyObservedSlackMerge(&existing, candidate)
		existing.LastInteractionAt = &lastInteraction
		return s.UpsertContact(ctx, existing, now)
	}

	contact := Contact{
		ContactID:           strings.TrimSpace(candidate.PrimaryContactID),
		Kind:                candidate.Kind,
		Channel:             strings.TrimSpace(candidate.Channel),
		ContactNickname:     strings.TrimSpace(candidate.Nickname),
		TGUsername:          normalizeTelegramUsername(candidate.TGUsername),
		LineUserID:          refid.NormalizeLineID(candidate.LineUserID),
		LineChatIDs:         normalizeStringSlice(candidate.LineChatIDs),
		LarkOpenID:          refid.NormalizeLarkID(candidate.LarkOpenID),
		LarkChatIDs:         normalizeStringSlice(candidate.LarkChatIDs),
		MixinUserID:         refid.NormalizeMixinID(candidate.MixinUserID),
		MixinIdentityNumber: strings.TrimSpace(candidate.MixinIdentityNumber),
		MixinChatIDs:        normalizeMixinIDs(candidate.MixinChatIDs),
		SlackTeamID:         strings.TrimSpace(candidate.SlackTeamID),
		SlackUserID:         strings.TrimSpace(candidate.SlackUserID),
		SlackDMChannelID:    strings.TrimSpace(candidate.SlackDMChannelID),
		SlackChannelIDs:     normalizeStringSlice(candidate.SlackChannelIDs),
		LastInteractionAt:   &lastInteraction,
	}
	applyObservedTelegramMerge(&contact, candidate)
	applyObservedLineMerge(&contact, candidate)
	applyObservedLarkMerge(&contact, candidate)
	applyObservedMixinMerge(&contact, candidate)
	applyObservedSlackMerge(&contact, candidate)
	return s.UpsertContact(ctx, contact, now)
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
	if candidate.TelegramIsSender && candidate.TelegramUserID > 0 {
		contact.TGUserID = candidate.TelegramUserID
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
	teamID := strings.TrimSpace(candidate.SlackTeamID)
	if teamID != "" && strings.TrimSpace(contact.SlackTeamID) == "" {
		contact.SlackTeamID = teamID
	}
	userID := strings.TrimSpace(candidate.SlackUserID)
	if userID != "" && strings.TrimSpace(contact.SlackUserID) == "" {
		contact.SlackUserID = userID
	}
	dmChannelID := strings.TrimSpace(candidate.SlackDMChannelID)
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

func applyObservedMixinMerge(contact *Contact, candidate observedContactCandidate) {
	if contact == nil {
		return
	}
	if userID := refid.NormalizeMixinID(candidate.MixinUserID); userID != "" && contact.MixinUserID == "" {
		contact.MixinUserID = userID
	}
	if identity := strings.TrimSpace(candidate.MixinIdentityNumber); identity != "" && contact.MixinIdentityNumber == "" {
		contact.MixinIdentityNumber = identity
	}
	contact.MixinChatIDs = mergeMixinChatIDs(contact.MixinChatIDs, candidate.MixinChatIDs...)
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
		out[i] = strings.TrimSpace(out[i])
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

func mergeMixinChatIDs(base []string, chatIDs ...string) []string {
	return normalizeMixinIDs(append(append([]string(nil), base...), chatIDs...))
}

func normalizeMixinIDs(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, raw := range items {
		id := refid.NormalizeMixinID(raw)
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
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
	teamID := strings.TrimSpace(parts[0])
	channelID := strings.TrimSpace(parts[1])
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
	teamID := strings.TrimSpace(parts[0])
	userID := strings.TrimSpace(parts[1])
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
