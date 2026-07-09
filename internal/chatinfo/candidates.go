package chatinfo

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/quailyquaily/mistermorph/contacts"
	refid "github.com/quailyquaily/mistermorph/internal/entryutil/refid"
)

func ActiveContactCandidateIDs(ctx context.Context, contactsDir string) ([]string, error) {
	rows, err := contacts.NewFileStore(contactsDir).ListContacts(ctx, contacts.StatusActive)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	add := func(raw string) {
		chatID, err := NormalizeChatID(raw)
		if err != nil {
			return
		}
		key := strings.ToLower(chatID)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, chatID)
	}
	for _, contact := range rows {
		addContactChatID(contact, add)
	}
	sort.Strings(out)
	return out, nil
}

func addContactChatID(contact contacts.Contact, add func(string)) {
	if add == nil {
		return
	}
	if chatID, ok := chatIDCandidateFromContactID(contact.ContactID); ok {
		add(chatID)
	}
	if contact.TGPrivateChatID != 0 {
		add("tg:" + strconv.FormatInt(contact.TGPrivateChatID, 10))
	}
	for _, chatID := range contact.TGGroupChatIDs {
		if chatID != 0 {
			add("tg:" + strconv.FormatInt(chatID, 10))
		}
	}
	for _, chatID := range contact.LineChatIDs {
		add("line:" + strings.TrimSpace(chatID))
	}
	for _, chatID := range contact.LarkChatIDs {
		add("lark:" + strings.TrimSpace(chatID))
	}

	teamID := strings.TrimSpace(contact.SlackTeamID)
	if teamID == "" {
		parsedTeamID, _, ok := slackContactIDParts(contact.ContactID)
		if ok {
			teamID = parsedTeamID
		}
	}
	if teamID != "" {
		if isSlackConversationID(contact.SlackDMChannelID) {
			add("slack:" + teamID + ":" + strings.TrimSpace(contact.SlackDMChannelID))
		}
		for _, channelID := range contact.SlackChannelIDs {
			if isSlackConversationID(channelID) {
				add("slack:" + teamID + ":" + strings.TrimSpace(channelID))
			}
		}
	}
}

func chatIDCandidateFromContactID(contactID string) (string, bool) {
	value := strings.TrimSpace(contactID)
	if value == "" || strings.HasPrefix(strings.ToLower(value), "tg:@") {
		return "", false
	}
	if strings.HasPrefix(strings.ToLower(value), "tg:") {
		if _, _, err := refid.ParseTelegramChatIDHint(value); err == nil {
			return value, true
		}
		return "", false
	}
	if teamID, channelID, ok, err := refid.ParseSlackChatIDHint(value); err == nil && ok && isSlackConversationID(channelID) {
		return "slack:" + teamID + ":" + channelID, true
	}
	if chatID, ok, err := refid.ParseLineChatIDHint(value); err == nil && ok {
		return "line:" + chatID, true
	}
	if chatID, ok, err := refid.ParseLarkChatIDHint(value); err == nil && ok {
		return "lark:" + chatID, true
	}
	return "", false
}

func slackContactIDParts(raw string) (string, string, bool) {
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

func isSlackConversationID(raw string) bool {
	id := strings.ToUpper(strings.TrimSpace(raw))
	return strings.HasPrefix(id, "C") || strings.HasPrefix(id, "G") || strings.HasPrefix(id, "D")
}
