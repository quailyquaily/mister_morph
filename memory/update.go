package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/entryutil"
)

const (
	defaultShortSummary = "Session updated."
	defaultLongSummary  = "Most important and stable long-term facts and project state."
	longTermMaxItems    = 100
	longTermMaxChars    = 3000
)

type WriteMeta struct {
	SessionID        string
	ContactIDs       []string
	ContactNicknames []string
}

func (m *Manager) UpdateShortTerm(date time.Time, draft SessionDraft, meta WriteMeta) (string, error) {
	if m == nil {
		return "", fmt.Errorf("nil memory manager")
	}
	meta.SessionID = strings.TrimSpace(meta.SessionID)
	abs, rel := m.ShortTermSessionPath(date, meta.SessionID)
	if abs == "" {
		return "", fmt.Errorf("memory dir not set")
	}

	fm, body, ok, err := readMemoryFile(abs)
	if err != nil {
		return "", err
	}
	content := ShortTermContent{}
	if ok {
		content = ParseShortTermContent(body)
	}

	createdAt := date.UTC().Format(entryutil.TimestampLayout)
	merged := MergeShortTerm(content, draft, createdAt)

	bodyOut := BuildShortTermBody(merged)
	summary := fallbackShortSummaryFromContent(merged)
	fm = applyShortTermFrontmatter(fm, summary, meta, m.nowUTC())

	if err := writeMemoryFile(abs, RenderFrontmatter(fm)+"\n"+bodyOut); err != nil {
		return "", err
	}
	return rel, nil
}

func (m *Manager) WriteShortTerm(date time.Time, content ShortTermContent, meta WriteMeta) (string, error) {
	if m == nil {
		return "", fmt.Errorf("nil memory manager")
	}
	meta.SessionID = strings.TrimSpace(meta.SessionID)
	abs, rel := m.ShortTermSessionPath(date, meta.SessionID)
	if abs == "" {
		return "", fmt.Errorf("memory dir not set")
	}

	fm, _, ok, err := readMemoryFile(abs)
	if err != nil {
		return "", err
	}
	if !ok {
		fm = Frontmatter{}
	}

	content = NormalizeShortTermContent(content)
	bodyOut := BuildShortTermBody(content)
	summary := fallbackShortSummaryFromContent(content)
	fm = applyShortTermFrontmatter(fm, summary, meta, m.nowUTC())
	if err := writeMemoryFile(abs, RenderFrontmatter(fm)+"\n"+bodyOut); err != nil {
		return "", err
	}
	return rel, nil
}

func (m *Manager) LoadShortTerm(date time.Time, sessionID string) (Frontmatter, ShortTermContent, bool, error) {
	if m == nil {
		return Frontmatter{}, ShortTermContent{}, false, fmt.Errorf("nil memory manager")
	}
	abs, _ := m.ShortTermSessionPath(date, sessionID)
	if abs == "" {
		return Frontmatter{}, ShortTermContent{}, false, fmt.Errorf("memory dir not set")
	}
	fm, body, ok, err := readMemoryFile(abs)
	if err != nil {
		return Frontmatter{}, ShortTermContent{}, false, err
	}
	if !ok {
		return Frontmatter{}, ShortTermContent{}, false, nil
	}
	return fm, ParseShortTermContent(body), true, nil
}

func (m *Manager) UpdateLongTerm(subjectID string, promote PromoteDraft) (bool, error) {
	if m == nil {
		return false, fmt.Errorf("nil memory manager")
	}
	abs, err := m.ensureLongTermIndex(subjectID)
	if err != nil {
		return false, err
	}
	if len(promote.GoalsProjects) == 0 && len(promote.KeyFacts) == 0 {
		return false, nil
	}
	if abs == "" {
		return false, fmt.Errorf("memory dir not set")
	}

	fm, body, ok, err := readMemoryFile(abs)
	if err != nil {
		return false, err
	}
	content := LongTermContent{}
	if ok {
		content = ParseLongTermContent(body)
	}

	now := m.nowUTC()
	merged := MergeLongTerm(content, promote, now)
	merged = enforceLongTermLimits(merged)
	bodyOut := BuildLongTermBody(merged)
	fm = applyLongTermFrontmatter(fm, merged, now)

	if err := writeMemoryFile(abs, RenderFrontmatter(fm)+"\n"+bodyOut); err != nil {
		return false, err
	}
	return true, nil
}

func applyShortTermFrontmatter(existing Frontmatter, summary string, meta WriteMeta, now time.Time) Frontmatter {
	existing.CreatedAt = chooseTimestamp(existing.CreatedAt, now)
	existing.UpdatedAt = now.UTC().Format(time.RFC3339)
	existing.Summary = summary
	if strings.TrimSpace(meta.SessionID) != "" {
		existing.SessionID = strings.TrimSpace(meta.SessionID)
	}
	if len(meta.ContactIDs) > 0 {
		existing.ContactIDs = StringList(mergePlainStrings([]string(existing.ContactIDs), meta.ContactIDs))
	}
	if len(meta.ContactNicknames) > 0 {
		existing.ContactNicknames = StringList(mergePlainStrings([]string(existing.ContactNicknames), meta.ContactNicknames))
	}
	return existing
}

func mergePlainStrings(existing []string, incoming []string) []string {
	if len(incoming) == 0 {
		return existing
	}
	seen := make(map[string]bool, len(existing)+len(incoming))
	out := make([]string, 0, len(existing)+len(incoming))
	add := func(raw string) {
		value := strings.TrimSpace(raw)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, value)
	}
	for _, value := range existing {
		add(value)
	}
	for _, value := range incoming {
		add(value)
	}
	return out
}

func applyLongTermFrontmatter(existing Frontmatter, content LongTermContent, now time.Time) Frontmatter {
	existing.CreatedAt = chooseTimestamp(existing.CreatedAt, now)
	existing.UpdatedAt = now.UTC().Format(time.RFC3339)
	existing.Summary = summarizeLongTerm(content)
	existing.Tasks = longTermTaskProgress(content.Goals)
	return existing
}

func fallbackShortSummaryFromContent(content ShortTermContent) string {
	if len(content.SummaryItems) > 0 {
		val := strings.TrimSpace(content.SummaryItems[0].Content)
		if val != "" {
			return truncateSummary(val)
		}
	}
	return defaultShortSummary
}

func summarizeLongTerm(content LongTermContent) string {
	parts := make([]string, 0, 2)
	if len(content.Goals) > 0 {
		parts = append(parts, "Goals: "+goalSummary(content.Goals[0]))
	}
	if len(content.Facts) > 0 {
		parts = append(parts, "Facts: "+kvSummary(content.Facts[0]))
	}
	if len(parts) == 0 {
		return defaultLongSummary
	}
	return truncateSummary(strings.Join(parts, "; "))
}

func enforceLongTermLimits(content LongTermContent) LongTermContent {
	for len(content.Goals)+len(content.Facts) > longTermMaxItems {
		content = dropOldestLongTerm(content)
	}
	for len(BuildLongTermBody(content)) > longTermMaxChars {
		content = dropOldestLongTerm(content)
		if len(content.Goals) == 0 && len(content.Facts) == 0 {
			break
		}
	}
	return content
}

func dropOldestLongTerm(content LongTermContent) LongTermContent {
	if len(content.Goals) == 0 && len(content.Facts) == 0 {
		return content
	}
	if len(content.Goals) >= len(content.Facts) {
		if len(content.Goals) > 0 {
			content.Goals = content.Goals[1:]
		}
		return content
	}
	if len(content.Facts) > 0 {
		content.Facts = content.Facts[1:]
	}
	return content
}

func kvSummary(item KVItem) string {
	title := strings.TrimSpace(item.Title)
	val := strings.TrimSpace(item.Value)
	if title == "" {
		return truncateSummary(val)
	}
	if val == "" {
		return truncateSummary(title)
	}
	return truncateSummary(title + ": " + val)
}

func goalSummary(goal LongTermGoal) string {
	content := strings.TrimSpace(goal.Content)
	if content == "" {
		return ""
	}
	if goal.Done {
		return truncateSummary("Done: " + content)
	}
	return truncateSummary(content)
}

func longTermTaskProgress(goals []LongTermGoal) string {
	total := len(goals)
	done := 0
	for _, goal := range goals {
		if goal.Done {
			done++
		}
	}
	return fmt.Sprintf("%d/%d", done, total)
}

func truncateSummary(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 200 {
		return s
	}
	return strings.TrimSpace(s[:200])
}

func chooseTimestamp(existing string, now time.Time) string {
	existing = strings.TrimSpace(existing)
	if existing != "" {
		return existing
	}
	return now.UTC().Format(time.RFC3339)
}

func readMemoryFile(path string) (Frontmatter, string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Frontmatter{}, "", false, nil
		}
		return Frontmatter{}, "", false, err
	}
	fm, body, ok := ParseFrontmatter(string(data))
	if !ok {
		return Frontmatter{}, string(data), true, nil
	}
	return fm, body, true, nil
}

func writeMemoryFile(path string, content string) error {
	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func (m *Manager) nowUTC() time.Time {
	if m != nil && m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}
