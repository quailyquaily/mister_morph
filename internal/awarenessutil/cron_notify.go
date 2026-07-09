package awarenessutil

import (
	"regexp"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/chatinfo"
	refid "github.com/quailyquaily/mistermorph/internal/entryutil/refid"
)

var cronNotifyMarkdownRefPattern = regexp.MustCompile(`\[([^\[\]\r\n]+)\]\(([^)\s]+)\)`)

func BuildCronNotifyTarget(task string, chatID string, info *chatinfo.Info) map[string]any {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil
	}
	target := map[string]any{
		"chat_id": chatID,
		"people":  cronNotifyPeople(task),
	}
	if info != nil {
		chat := cronNotifyChatProfile(*info)
		if len(chat) > 0 {
			target["chat_profile"] = chat
		}
	}
	return target
}

func cronNotifyPeople(task string) []map[string]string {
	matches := cronNotifyMarkdownRefPattern.FindAllStringSubmatch(task, -1)
	if len(matches) == 0 {
		return []map[string]string{}
	}
	seen := map[string]bool{}
	out := make([]map[string]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		label := sanitizeCronNotifyLabel(match[1])
		contactID := strings.TrimSpace(match[2])
		if label == "" || !refid.IsValid(contactID) {
			continue
		}
		key := strings.ToLower(contactID)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, map[string]string{
			"contact_id": contactID,
			"label":      label,
			"ref":        "[" + label + "](" + contactID + ")",
		})
	}
	return out
}

func cronNotifyChatProfile(info chatinfo.Info) map[string]string {
	out := map[string]string{}
	if v := strings.TrimSpace(info.Platform); v != "" {
		out["platform"] = v
	}
	if v := strings.TrimSpace(info.Type); v != "" {
		out["type"] = v
	}
	if v := strings.TrimSpace(info.Name); v != "" {
		out["name"] = v
	}
	return out
}

func sanitizeCronNotifyLabel(raw string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(strings.TrimSpace(raw)), " "))
}
