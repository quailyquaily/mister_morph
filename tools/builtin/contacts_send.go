package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/chatinfo"
	"github.com/quailyquaily/mistermorph/internal/contactsruntime"
	refid "github.com/quailyquaily/mistermorph/internal/entryutil/refid"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

const (
	contactsSendContentType = "application/json"
)

type ContactsSendToolOptions struct {
	Enabled          bool
	ContactsDir      string
	TelegramBotToken string
	TelegramBaseURL  string
	SlackBotToken    string
	SlackBaseURL     string
	LineChannelToken string
	LineBaseURL      string
	LarkAppID        string
	LarkAppSecret    string
	LarkBaseURL      string
	FailureCooldown  time.Duration
}

type ContactsSendTool struct {
	opts ContactsSendToolOptions
}

func NewContactsSendTool(opts ContactsSendToolOptions) *ContactsSendTool {
	return &ContactsSendTool{opts: opts}
}

func (t *ContactsSendTool) Name() string { return "contacts_send" }

func (t *ContactsSendTool) Description() string {
	return `Sends a message to one or more contacts.
		IF sending to multiple contacts THEN pass comma-separated contact_id values.
	  Message routes automatically across Slack, Telegram, LINE, and Lark based on chat_id/contact reachability.
		NEVER send message to people who is talking with you, or the people in the chat history.`
}

func (t *ContactsSendTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"contact_id": map[string]any{
				"type":        "string",
				"description": "Target contact_id. Multiple contacts may be provided as a comma-separated list. e.g.: slack:<team_id>:<user_id>, tg:@<username>, tg:<chat_id>, line_user:<user_id>, line:<chat_id>, lark_user:<open_id>, lark:<chat_id>.",
			},
			"message_text": map[string]any{
				"type":        "string",
				"description": "Plain text body; tool wraps it into envelope JSON.",
			},
			"message_base64": map[string]any{
				"type":        "string",
				"description": "Optional base64 JSON envelope payload when message_text is not used.",
			},
			"chat_id": map[string]any{
				"type":        "string",
				"description": "Optional chat id hint. e.g. slack:<team_id>:<channel_id>, tg:<chat_id>, line:<chat_id>, or lark:<chat_id>.",
			},
			"content_type": map[string]any{
				"type":        "string",
				"description": "Optional Payload type (default application/json).",
			},
			"session_id": map[string]any{
				"type":        "string",
				"description": "Optional UUIDv7 session_id; auto-generated when omitted.",
			},
			"reply_to": map[string]any{
				"type":        "string",
				"description": "Optional reply_to message_id.",
			},
		},
		"required": []string{"contact_id"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *ContactsSendTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || !t.opts.Enabled {
		return "", fmt.Errorf("contacts_send tool is disabled")
	}
	contactIDs, err := parseContactsSendContactIDs(params)
	if err != nil {
		return "", err
	}
	chatID, err := parseContactsSendChatID(params)
	if err != nil {
		return "", err
	}
	if len(contactIDs) == 1 {
		if runtimeCtx, ok := ContactsSendRuntimeContextFromContext(ctx); ok {
			if field, target, blocked := contactsSendBlockedTarget(contactIDs[0], chatID, runtimeCtx); blocked {
				return "", fmt.Errorf("contacts_send blocked: %s %q matches current conversation counterpart", field, target)
			}
		}
	}
	contactsDir := pathutil.ExpandHomePath(strings.TrimSpace(t.opts.ContactsDir))
	if contactsDir == "" {
		return "", fmt.Errorf("contacts dir is not configured")
	}

	sender, err := contactsruntime.NewRoutingSender(ctx, contactsruntime.SenderOptions{
		TelegramBotToken: strings.TrimSpace(t.opts.TelegramBotToken),
		TelegramBaseURL:  strings.TrimSpace(t.opts.TelegramBaseURL),
		SlackBotToken:    strings.TrimSpace(t.opts.SlackBotToken),
		SlackBaseURL:     strings.TrimSpace(t.opts.SlackBaseURL),
		LineChannelToken: strings.TrimSpace(t.opts.LineChannelToken),
		LineBaseURL:      strings.TrimSpace(t.opts.LineBaseURL),
		LarkAppID:        strings.TrimSpace(t.opts.LarkAppID),
		LarkAppSecret:    strings.TrimSpace(t.opts.LarkAppSecret),
		LarkBaseURL:      strings.TrimSpace(t.opts.LarkBaseURL),
	})
	if err != nil {
		return "", err
	}
	defer sender.Close()

	svc := contacts.NewServiceWithOptions(
		contacts.NewFileStore(contactsDir),
		contacts.ServiceOptions{
			FailureCooldown: t.opts.FailureCooldown,
		},
	)
	return executeContactsSendResolved(ctx, params, contactIDs, chatID, svc, sender, time.Now().UTC())
}

func executeContactsSendResolved(
	ctx context.Context,
	params map[string]any,
	contactIDs []string,
	chatID string,
	svc *contacts.Service,
	sender contacts.Sender,
	now time.Time,
) (string, error) {
	if svc == nil {
		return "", fmt.Errorf("contacts service is required")
	}
	if sender == nil {
		return "", fmt.Errorf("contacts sender is required")
	}
	if len(contactIDs) == 1 {
		return executeContactsSendSingle(ctx, params, strings.TrimSpace(contactIDs[0]), chatID, svc, sender, now)
	}
	baseText, err := contactsSendBaseMessageText(params)
	if err != nil {
		return "", err
	}
	recipients, err := resolveContactsSendRecipients(ctx, svc, contactIDs)
	if err != nil {
		return "", err
	}
	plan, err := planContactsSendBatch(recipients, chatID)
	if err != nil {
		return "", err
	}
	if runtimeCtx, ok := ContactsSendRuntimeContextFromContext(ctx); ok {
		if err := checkContactsSendPlanBlocked(plan, runtimeCtx); err != nil {
			return "", err
		}
	}

	outcomes := make([]map[string]any, 0, len(plan))
	for _, item := range plan {
		sendParams := contactsSendParamsWithText(params, item.Text(baseText))
		contentType, payload, err := resolveSendPayload(sendParams, now)
		if err != nil {
			return "", err
		}
		decision := contacts.ShareDecision{
			ContactID:           item.ContactID,
			RecipientContactIDs: append([]string(nil), item.RecipientContactIDs...),
			ChatID:              item.ChatID,
			ContentType:         contentType,
			PayloadBase64:       payload,
		}
		decision.ItemID = "manual_" + uuid.NewString()
		decision.IdempotencyKey = "manual:" + uuid.NewString()

		outcome, err := svc.SendDecision(ctx, now, decision, sender)
		if err != nil {
			return "", err
		}
		outcomes = append(outcomes, map[string]any{
			"chat_id":    item.ChatID,
			"mentions":   append([]string(nil), item.Mentions...),
			"recipients": append([]string(nil), item.RecipientContactIDs...),
			"outcome":    outcome,
		})
	}
	out, _ := json.MarshalIndent(map[string]any{
		"outcomes": outcomes,
	}, "", "  ")
	return string(out), nil
}

func executeContactsSendSingle(
	ctx context.Context,
	params map[string]any,
	contactID string,
	chatID string,
	svc *contacts.Service,
	sender contacts.Sender,
	now time.Time,
) (string, error) {
	resolvedChatID, err := resolveContactsSendChatTargetHint(contactID, chatID)
	if err != nil {
		return "", err
	}
	sendParams, err := contactsSendParamsWithSingleMention(ctx, params, contactID, resolvedChatID, svc)
	if err != nil {
		return "", err
	}
	contentType, payload, err := resolveSendPayload(sendParams, now)
	if err != nil {
		return "", err
	}
	decision := contacts.ShareDecision{
		ContactID:     contactID,
		ChatID:        resolvedChatID,
		ContentType:   contentType,
		PayloadBase64: payload,
	}
	decision.ItemID = "manual_" + uuid.NewString()
	decision.IdempotencyKey = "manual:" + uuid.NewString()

	outcome, err := svc.SendDecision(ctx, now, decision, sender)
	if err != nil {
		return "", err
	}
	out, _ := json.MarshalIndent(map[string]any{
		"outcome": outcome,
	}, "", "  ")
	return string(out), nil
}

func resolveContactsSendChatTargetHint(contactID string, chatID string) (string, error) {
	chatID = strings.TrimSpace(chatID)
	contactChatID, err := chatinfo.NormalizeChatID(contactID)
	if err != nil {
		return chatID, nil
	}
	if chatID == "" {
		return contactChatID, nil
	}
	targetChatID, err := chatinfo.NormalizeChatID(chatID)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(contactChatID, targetChatID) {
		return "", fmt.Errorf("chat_id must be empty or match contact_id when contact_id is a chat target")
	}
	return targetChatID, nil
}

func contactsSendParamsWithSingleMention(ctx context.Context, params map[string]any, contactID string, chatID string, svc *contacts.Service) (map[string]any, error) {
	if svc == nil {
		return nil, fmt.Errorf("contacts service is required")
	}
	contact, err := svc.ResolveSendContact(ctx, contactID)
	if err != nil {
		return nil, err
	}
	channel, err := contacts.ResolveDecisionChannel(contact, contacts.ShareDecision{
		ContactID: contact.ContactID,
		ChatID:    chatID,
	})
	if err != nil {
		return nil, err
	}
	mention := contactsSendMentionForContact(contact, channel)
	if mention == "" {
		return params, nil
	}
	baseText, err := contactsSendBaseMessageText(params)
	if err != nil {
		return nil, err
	}
	return contactsSendParamsWithText(params, contactsSendPlanItem{
		Mentions: []string{mention},
	}.Text(baseText)), nil
}

func parseContactsSendContactIDs(params map[string]any) ([]string, error) {
	raw, ok := params["contact_id"].(string)
	if !ok {
		return nil, fmt.Errorf("missing required param: contact_id")
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("missing required param: contact_id")
	}
	return out, nil
}

type contactsSendRecipient struct {
	ContactID string
	Contact   contacts.Contact
}

type contactsSendPlanItem struct {
	ContactID           string
	ChatID              string
	RecipientContactIDs []string
	Mentions            []string
}

func (i contactsSendPlanItem) Text(baseText string) string {
	baseText = strings.TrimSpace(baseText)
	mentions := make([]string, 0, len(i.Mentions))
	seen := map[string]bool{}
	for _, raw := range i.Mentions {
		mention := strings.TrimSpace(raw)
		if mention == "" {
			continue
		}
		if seen[mention] {
			continue
		}
		seen[mention] = true
		mentions = append(mentions, mention)
	}
	if len(mentions) == 0 {
		return baseText
	}
	if baseText == "" {
		return strings.Join(mentions, " ")
	}
	return strings.Join(mentions, " ") + " " + baseText
}

type contactsSendRouteCandidate struct {
	Channel string
	ChatID  string
	Mention string
	Key     string
}

func resolveContactsSendRecipients(ctx context.Context, svc *contacts.Service, contactIDs []string) ([]contactsSendRecipient, error) {
	recipients := make([]contactsSendRecipient, 0, len(contactIDs))
	for _, contactID := range contactIDs {
		contact, err := svc.ResolveSendContact(ctx, contactID)
		if err != nil {
			return nil, err
		}
		if err := validateContactsSendDefaultRoute(contact); err != nil {
			return nil, err
		}
		recipients = append(recipients, contactsSendRecipient{
			ContactID: strings.TrimSpace(contactID),
			Contact:   contact,
		})
	}
	return recipients, nil
}

func validateContactsSendDefaultRoute(contact contacts.Contact) error {
	channel, err := contacts.ResolveDecisionChannel(contact, contacts.ShareDecision{ContactID: contact.ContactID})
	if err != nil {
		return err
	}
	switch channel {
	case contacts.ChannelTelegram:
		_, _, err = contactsruntime.ResolveTelegramTarget(contact)
	case contacts.ChannelSlack:
		_, _, err = contactsruntime.ResolveSlackTarget(contact)
	case contacts.ChannelLine:
		_, err = contactsruntime.ResolveLineTarget(contact)
	case contacts.ChannelLark:
		_, err = contactsruntime.ResolveLarkTarget(contact)
	default:
		err = fmt.Errorf("unsupported delivery channel: %s", channel)
	}
	return err
}

func planContactsSendBatch(recipients []contactsSendRecipient, chatID string) ([]contactsSendPlanItem, error) {
	if len(recipients) == 0 {
		return nil, fmt.Errorf("contact_id is required")
	}
	if strings.TrimSpace(chatID) != "" {
		return planContactsSendExplicitChatBatch(recipients, chatID)
	}
	routesByIndex := make(map[int][]contactsSendRouteCandidate, len(recipients))
	for i, recipient := range recipients {
		routesByIndex[i] = contactsSendRouteCandidates(recipient.Contact)
	}
	remaining := make(map[int]bool, len(recipients))
	for i := range recipients {
		remaining[i] = true
	}

	plan := make([]contactsSendPlanItem, 0, len(recipients))
	for {
		routeGroups := map[string]struct {
			route   contactsSendRouteCandidate
			members []int
		}{}
		for i := range remaining {
			seenRoutes := map[string]bool{}
			for _, route := range routesByIndex[i] {
				if route.Key == "" || route.Mention == "" || seenRoutes[route.Key] {
					continue
				}
				seenRoutes[route.Key] = true
				group := routeGroups[route.Key]
				group.route = route
				group.members = append(group.members, i)
				routeGroups[route.Key] = group
			}
		}

		bestKey := ""
		var best struct {
			route   contactsSendRouteCandidate
			members []int
		}
		for key, group := range routeGroups {
			if len(group.members) < 2 {
				continue
			}
			sort.Ints(group.members)
			if bestKey == "" || len(group.members) > len(best.members) || (len(group.members) == len(best.members) && key < bestKey) {
				bestKey = key
				best = group
			}
		}
		if bestKey == "" {
			break
		}
		plan = append(plan, contactsSendPlanItemForRoute(recipients, best.members, best.route))
		for _, idx := range best.members {
			delete(remaining, idx)
		}
	}

	left := make([]int, 0, len(remaining))
	for idx := range remaining {
		left = append(left, idx)
	}
	sort.Ints(left)
	for _, idx := range left {
		routes := routesByIndex[idx]
		if len(routes) > 0 {
			plan = append(plan, contactsSendPlanItemForRoute(recipients, []int{idx}, routes[0]))
			continue
		}
		recipient := recipients[idx]
		plan = append(plan, contactsSendPlanItem{
			ContactID:           strings.TrimSpace(recipient.Contact.ContactID),
			RecipientContactIDs: []string{strings.TrimSpace(recipient.Contact.ContactID)},
		})
	}
	return plan, nil
}

func planContactsSendExplicitChatBatch(recipients []contactsSendRecipient, chatID string) ([]contactsSendPlanItem, error) {
	targetChatID, err := chatinfo.NormalizeChatID(chatID)
	if err != nil {
		return nil, err
	}
	matchChatID := targetChatID
	if telegramChatID, hasTelegramHint, parseErr := refid.ParseTelegramChatIDHint(targetChatID); parseErr != nil {
		return nil, parseErr
	} else if hasTelegramHint {
		matchChatID = "tg:" + strconv.FormatInt(telegramChatID, 10)
	}

	memberIndexes := make([]int, 0, len(recipients))
	var selectedRoute contactsSendRouteCandidate
	for i, recipient := range recipients {
		matched := false
		for _, route := range contactsSendRouteCandidates(recipient.Contact) {
			if !strings.EqualFold(route.ChatID, matchChatID) {
				continue
			}
			if selectedRoute.Channel == "" {
				selectedRoute = route
			}
			memberIndexes = append(memberIndexes, i)
			matched = true
			break
		}
		if !matched {
			return nil, fmt.Errorf("chat_id %q is unavailable for contact_id %q", targetChatID, strings.TrimSpace(recipient.ContactID))
		}
	}
	selectedRoute.ChatID = targetChatID
	selectedRoute.Key = selectedRoute.Channel + "|" + targetChatID
	return []contactsSendPlanItem{contactsSendPlanItemForRoute(recipients, memberIndexes, selectedRoute)}, nil
}

func contactsSendPlanItemForRoute(recipients []contactsSendRecipient, memberIndexes []int, route contactsSendRouteCandidate) contactsSendPlanItem {
	sort.Ints(memberIndexes)
	item := contactsSendPlanItem{
		ChatID:              strings.TrimSpace(route.ChatID),
		RecipientContactIDs: make([]string, 0, len(memberIndexes)),
		Mentions:            make([]string, 0, len(memberIndexes)),
	}
	for _, idx := range memberIndexes {
		recipient := recipients[idx]
		contactID := strings.TrimSpace(recipient.Contact.ContactID)
		if item.ContactID == "" {
			item.ContactID = contactID
		}
		item.RecipientContactIDs = append(item.RecipientContactIDs, contactID)
		if mention := contactsSendMentionForContact(recipient.Contact, route.Channel); mention != "" {
			item.Mentions = append(item.Mentions, mention)
		}
	}
	return item
}

func contactsSendRouteCandidates(contact contacts.Contact) []contactsSendRouteCandidate {
	var out []contactsSendRouteCandidate
	seen := map[string]bool{}
	add := func(channel, chatID, mention string) {
		channel = strings.TrimSpace(channel)
		chatID = strings.TrimSpace(chatID)
		mention = strings.TrimSpace(mention)
		if channel == "" || chatID == "" || mention == "" {
			return
		}
		key := channel + "|" + chatID
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, contactsSendRouteCandidate{
			Channel: channel,
			ChatID:  chatID,
			Mention: mention,
			Key:     key,
		})
	}

	if mention := contactsSendMentionForContact(contact, contacts.ChannelTelegram); mention != "" {
		groupIDs := append([]int64(nil), contact.TGGroupChatIDs...)
		sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
		for _, chatID := range groupIDs {
			if chatID != 0 {
				add(contacts.ChannelTelegram, "tg:"+strconv.FormatInt(chatID, 10), mention)
			}
		}
		if chatID, ok := contactsSendTelegramChatIDFromContactID(contact.ContactID); ok && chatID < 0 {
			add(contacts.ChannelTelegram, "tg:"+strconv.FormatInt(chatID, 10), mention)
		}
	}

	if mention := contactsSendMentionForContact(contact, contacts.ChannelSlack); mention != "" {
		teamID := strings.TrimSpace(contact.SlackTeamID)
		if contactTeamID, _, ok := contactsSendSlackContactParts(contact.ContactID); teamID == "" && ok {
			teamID = contactTeamID
		}
		if teamID != "" {
			channelIDs := append([]string(nil), contact.SlackChannelIDs...)
			sort.Strings(channelIDs)
			for _, raw := range channelIDs {
				channelID := strings.TrimSpace(raw)
				if contactsSendSlackChannelCanMention(channelID) {
					add(contacts.ChannelSlack, "slack:"+teamID+":"+channelID, mention)
				}
			}
			if _, channelID, ok := contactsSendSlackContactParts(contact.ContactID); ok && contactsSendSlackChannelCanMention(channelID) {
				add(contacts.ChannelSlack, "slack:"+teamID+":"+channelID, mention)
			}
		}
	}

	if mention := contactsSendMentionForContact(contact, contacts.ChannelLark); mention != "" {
		chatIDs := append([]string(nil), contact.LarkChatIDs...)
		sort.Strings(chatIDs)
		for _, raw := range chatIDs {
			if chatID := refid.NormalizeLarkID(raw); chatID != "" {
				add(contacts.ChannelLark, "lark:"+chatID, mention)
			}
		}
		if chatID, ok := refid.ParseLarkChatContactID(contact.ContactID); ok {
			add(contacts.ChannelLark, "lark:"+chatID, mention)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func contactsSendMentionForContact(contact contacts.Contact, channel string) string {
	switch channel {
	case contacts.ChannelTelegram:
		username := strings.TrimSpace(contact.TGUsername)
		if username == "" {
			contactID := strings.TrimSpace(contact.ContactID)
			if strings.HasPrefix(strings.ToLower(contactID), "tg:@") {
				username = strings.TrimSpace(contactID[len("tg:@"):])
			}
		}
		username = strings.TrimPrefix(strings.TrimSpace(username), "@")
		if username == "" || strings.ContainsAny(username, " \t\r\n") {
			return ""
		}
		return "@" + username
	case contacts.ChannelSlack:
		userID := strings.TrimSpace(contact.SlackUserID)
		if userID == "" {
			_, id, ok := contactsSendSlackContactParts(contact.ContactID)
			if ok && contactsSendSlackUserCanMention(id) {
				userID = id
			}
		}
		if !contactsSendSlackUserCanMention(userID) {
			return ""
		}
		return "<@" + strings.TrimSpace(userID) + ">"
	case contacts.ChannelLark:
		openID := refid.NormalizeLarkID(contact.LarkOpenID)
		if openID == "" {
			if parsed, ok := refid.ParseLarkUserContactID(contact.ContactID); ok {
				openID = parsed
			}
		}
		if openID == "" {
			return ""
		}
		name := strings.TrimSpace(contact.ContactNickname)
		if name == "" {
			name = openID
		}
		return `<at user_id="` + contactsSendEscapeLarkText(openID) + `">` + contactsSendEscapeLarkText(name) + `</at>`
	default:
		return ""
	}
}

func contactsSendTelegramChatIDFromContactID(raw string) (int64, bool) {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(value), "tg:@") || !strings.HasPrefix(strings.ToLower(value), "tg:") {
		return 0, false
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(value[len("tg:"):]), 10, 64)
	return chatID, err == nil && chatID != 0
}

func contactsSendSlackContactParts(raw string) (string, string, bool) {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(value), "slack:") {
		return "", "", false
	}
	parts := strings.Split(strings.TrimSpace(value[len("slack:"):]), ":")
	if len(parts) != 2 {
		return "", "", false
	}
	teamID := strings.TrimSpace(parts[0])
	id := strings.TrimSpace(parts[1])
	if teamID == "" || id == "" {
		return "", "", false
	}
	return teamID, id, true
}

func contactsSendSlackChannelCanMention(channelID string) bool {
	id := strings.ToUpper(strings.TrimSpace(channelID))
	return strings.HasPrefix(id, "C") || strings.HasPrefix(id, "G")
}

func contactsSendSlackUserCanMention(userID string) bool {
	id := strings.ToUpper(strings.TrimSpace(userID))
	return strings.HasPrefix(id, "U") || strings.HasPrefix(id, "W")
}

func contactsSendEscapeLarkText(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)
	return replacer.Replace(strings.TrimSpace(value))
}

func contactsSendBaseMessageText(params map[string]any) (string, error) {
	if text, ok := params["message_text"].(string); ok {
		text = strings.TrimSpace(text)
		if text != "" {
			return text, nil
		}
	}
	raw, ok := params["message_base64"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("message_text or message_base64 is required")
	}
	if _, _, err := resolveSendPayload(params, time.Now().UTC()); err != nil {
		return "", err
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("message_base64 decode failed: %w", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(payloadBytes, &envelope); err != nil {
		return "", fmt.Errorf("message_base64 must be envelope json")
	}
	text, _ := envelope["text"].(string)
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("message envelope missing text")
	}
	return text, nil
}

func contactsSendParamsWithText(params map[string]any, text string) map[string]any {
	out := make(map[string]any, len(params)+1)
	for key, value := range params {
		out[key] = value
	}
	out["message_text"] = strings.TrimSpace(text)
	delete(out, "message_base64")
	if _, hasSession := out["session_id"]; !hasSession {
		if sessionID := contactsSendSessionIDFromMessageBase64(params); sessionID != "" {
			out["session_id"] = sessionID
		}
	}
	return out
}

func contactsSendSessionIDFromMessageBase64(params map[string]any) string {
	raw, ok := params["message_base64"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return ""
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	var envelope map[string]any
	if err := json.Unmarshal(payloadBytes, &envelope); err != nil {
		return ""
	}
	sessionID, _ := envelope["session_id"].(string)
	return strings.TrimSpace(sessionID)
}

func checkContactsSendPlanBlocked(plan []contactsSendPlanItem, runtimeCtx ContactsSendRuntimeContext) error {
	for _, item := range plan {
		for _, contactID := range item.RecipientContactIDs {
			if field, target, blocked := contactsSendBlockedTarget(contactID, item.ChatID, runtimeCtx); blocked {
				return fmt.Errorf("contacts_send blocked: %s %q matches current conversation counterpart", field, target)
			}
		}
	}
	return nil
}

func parseContactsSendChatID(params map[string]any) (string, error) {
	raw, exists := params["chat_id"]
	if !exists || raw == nil {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("chat_id must be a string")
	}
	return strings.TrimSpace(value), nil
}

func resolveSendPayload(params map[string]any, now time.Time) (string, string, error) {
	contentType, _ := params["content_type"].(string)
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		contentType = contactsSendContentType
	}
	if !strings.HasPrefix(strings.ToLower(contentType), contactsSendContentType) {
		return "", "", fmt.Errorf("content_type must be application/json envelope")
	}

	sessionID, _ := params["session_id"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		if err := validateUUIDv7SessionID(sessionID); err != nil {
			return "", "", fmt.Errorf("session_id must be uuid_v7")
		}
	}
	replyTo, _ := params["reply_to"].(string)
	replyTo = strings.TrimSpace(replyTo)

	if text, ok := params["message_text"].(string); ok {
		text = strings.TrimSpace(text)
		if text != "" {
			resolvedSessionID := sessionID
			if resolvedSessionID == "" {
				generatedSessionID, err := generateUUIDv7SessionID()
				if err != nil {
					return "", "", err
				}
				resolvedSessionID = generatedSessionID
			}
			envelope := map[string]any{
				"message_id": "msg_" + uuid.NewString(),
				"text":       text,
				"sent_at":    now.UTC().Format(time.RFC3339),
				"session_id": resolvedSessionID,
			}
			if replyTo != "" {
				envelope["reply_to"] = replyTo
			}
			raw, err := json.Marshal(envelope)
			if err != nil {
				return "", "", err
			}
			return contentType, base64.RawURLEncoding.EncodeToString(raw), nil
		}
	}

	if raw, ok := params["message_base64"].(string); ok {
		raw = strings.TrimSpace(raw)
		if raw != "" {
			payloadBytes, err := base64.RawURLEncoding.DecodeString(raw)
			if err != nil {
				return "", "", fmt.Errorf("message_base64 decode failed: %w", err)
			}
			var envelope map[string]any
			if err := json.Unmarshal(payloadBytes, &envelope); err != nil {
				return "", "", fmt.Errorf("message_base64 must be envelope json")
			}
			if _, ok := envelope["message_id"].(string); !ok {
				return "", "", fmt.Errorf("message envelope missing message_id")
			}
			if _, ok := envelope["text"].(string); !ok {
				return "", "", fmt.Errorf("message envelope missing text")
			}
			sentAt, ok := envelope["sent_at"].(string)
			if !ok || strings.TrimSpace(sentAt) == "" {
				return "", "", fmt.Errorf("message envelope missing sent_at")
			}
			if _, err := time.Parse(time.RFC3339, strings.TrimSpace(sentAt)); err != nil {
				return "", "", fmt.Errorf("message envelope sent_at must be RFC3339")
			}
			sessionRaw, _ := envelope["session_id"].(string)
			sessionRaw = strings.TrimSpace(sessionRaw)
			if sessionRaw == "" {
				sessionRaw = sessionID
			}
			if sessionRaw == "" {
				generatedSessionID, err := generateUUIDv7SessionID()
				if err != nil {
					return "", "", err
				}
				sessionRaw = generatedSessionID
			}
			if err := validateUUIDv7SessionID(sessionRaw); err != nil {
				return "", "", fmt.Errorf("message envelope session_id must be uuid_v7")
			}
			envelope["session_id"] = sessionRaw
			normalizedRaw, err := json.Marshal(envelope)
			if err != nil {
				return "", "", fmt.Errorf("marshal message envelope: %w", err)
			}
			return contentType, base64.RawURLEncoding.EncodeToString(normalizedRaw), nil
		}
	}
	return "", "", fmt.Errorf("message_text or message_base64 is required")
}

func validateUUIDv7SessionID(sessionID string) error {
	parsed, err := uuid.Parse(strings.TrimSpace(sessionID))
	if err != nil || parsed.Version() != uuid.Version(7) {
		return fmt.Errorf("session_id must be uuid_v7")
	}
	return nil
}

func generateUUIDv7SessionID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate session_id: %w", err)
	}
	return id.String(), nil
}
