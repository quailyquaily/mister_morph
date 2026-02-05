package memory

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// LoadRecentTelegramChatIDs scans recent short-term memory files and returns
// unique Telegram chat IDs inferred from session_id (telegram:<chat_id>).
func (m *Manager) LoadRecentTelegramChatIDs(days int) ([]int64, error) {
	if m == nil {
		return nil, nil
	}
	if days <= 0 {
		days = m.ShortTermDays
	}
	if days <= 0 {
		days = 7
	}
	now := m.nowUTC()
	seen := map[int64]bool{}
	out := make([]int64, 0)
	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i)
		dayAbs, _ := m.ShortTermDayDir(date)
		if strings.TrimSpace(dayAbs) == "" {
			continue
		}
		entries, err := os.ReadDir(dayAbs)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".md") {
				continue
			}
			abs := filepath.Join(dayAbs, name)
			data, err := os.ReadFile(abs)
			if err != nil {
				return nil, err
			}
			fm, _, ok := ParseFrontmatter(string(data))
			if !ok {
				continue
			}
			chatID, ok := parseTelegramChatID(fm.SessionID)
			if !ok || chatID == 0 {
				continue
			}
			if seen[chatID] {
				continue
			}
			seen[chatID] = true
			out = append(out, chatID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// LoadTelegramChatsWithPendingTasks scans recent short-term memory files and returns
// Telegram chat IDs that have pending tasks or follow-ups in frontmatter/body.
func (m *Manager) LoadTelegramChatsWithPendingTasks(days int) ([]int64, error) {
	if m == nil {
		return nil, nil
	}
	if days <= 0 {
		days = m.ShortTermDays
	}
	if days <= 0 {
		days = 7
	}
	now := m.nowUTC()
	seen := map[int64]bool{}
	out := make([]int64, 0)
	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i)
		dayAbs, _ := m.ShortTermDayDir(date)
		if strings.TrimSpace(dayAbs) == "" {
			continue
		}
		entries, err := os.ReadDir(dayAbs)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".md") {
				continue
			}
			abs := filepath.Join(dayAbs, name)
			data, err := os.ReadFile(abs)
			if err != nil {
				return nil, err
			}
			fm, body, ok := ParseFrontmatter(string(data))
			if !ok {
				continue
			}
			chatID, ok := parseTelegramChatID(fm.SessionID)
			if !ok || chatID == 0 {
				continue
			}
			if !hasPendingTasks(fm, body) {
				continue
			}
			if seen[chatID] {
				continue
			}
			seen[chatID] = true
			out = append(out, chatID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func hasPendingTasks(fm Frontmatter, body string) bool {
	pending, known := pendingFromFrontmatter(fm)
	if known {
		return pending
	}
	content := ParseShortTermContent(body)
	tDone, tTotal := taskCounts(content.Tasks)
	fDone, fTotal := taskCounts(content.FollowUps)
	return tTotal > tDone || fTotal > fDone
}

func pendingFromFrontmatter(fm Frontmatter) (pending bool, known bool) {
	if done, total, ok := parseTaskRatio(fm.Tasks); ok {
		known = true
		if total > done {
			pending = true
		}
	}
	if done, total, ok := parseTaskRatio(fm.FollowUps); ok {
		known = true
		if total > done {
			pending = true
		}
	}
	return pending, known
}

func parseTelegramChatID(sessionID string) (int64, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0, false
	}
	if !strings.HasPrefix(sessionID, "telegram:") {
		return 0, false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(sessionID, "telegram:"))
	if raw == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	if id == 0 {
		return 0, false
	}
	return id, true
}
