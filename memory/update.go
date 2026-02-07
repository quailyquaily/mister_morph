package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultShortSummary = "Session updated."
	defaultLongSummary  = "Long-term memory."
	longTermMaxItems    = 100
	longTermMaxChars    = 3000
)

type WriteMeta struct {
	SessionID       string
	Source          string
	Channel         string
	Usernames       []string
	SubjectID       string
	ContactID       string
	ContactNickname string
}

func (m *Manager) UpdateShortTerm(date time.Time, draft SessionDraft, meta WriteMeta) (string, error) {
	if m == nil {
		return "", fmt.Errorf("nil memory manager")
	}
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

	merged := MergeShortTerm(content, draft)
	merged = ensureShortTermDefaults(merged, draft)
	merged = m.attachRelatedLink(merged, date)

	bodyOut := BuildShortTermBody(date.UTC().Format("2006-01-02"), merged)
	summary := strings.TrimSpace(draft.Summary)
	if summary == "" {
		summary = fallbackShortSummary(draft)
	}
	fm = applyShortTermFrontmatter(fm, summary, meta, m.nowUTC(), merged)

	if err := writeMemoryFile(abs, RenderFrontmatter(fm)+"\n"+bodyOut); err != nil {
		return "", err
	}
	return rel, nil
}

func (m *Manager) WriteShortTerm(date time.Time, content ShortTermContent, summary string, meta WriteMeta) (string, error) {
	if m == nil {
		return "", fmt.Errorf("nil memory manager")
	}
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

	content = m.attachRelatedLink(content, date)
	bodyOut := BuildShortTermBody(date.UTC().Format("2006-01-02"), content)
	summary = strings.TrimSpace(summary)
	if summary == "" {
		summary = fallbackShortSummaryFromContent(content)
	}
	fm = applyShortTermFrontmatter(fm, summary, meta, m.nowUTC(), content)
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

func (m *Manager) UpdateRecentTaskStatuses(updates []TaskItem, excludeSessionID string) (int, error) {
	if m == nil {
		return 0, fmt.Errorf("nil memory manager")
	}
	if len(updates) == 0 {
		return 0, nil
	}
	updateMap := make(map[string]bool)
	for _, it := range updates {
		if !it.Done {
			continue
		}
		text := strings.TrimSpace(it.Text)
		if text == "" {
			continue
		}
		key := strings.ToLower(text)
		updateMap[key] = true
	}
	if len(updateMap) == 0 {
		return 0, nil
	}

	excludeName := ""
	if strings.TrimSpace(excludeSessionID) != "" {
		excludeName = SanitizeSubjectID(excludeSessionID) + ".md"
	}

	now := m.nowUTC()
	updated := 0
	for i := 0; i < m.ShortTermDays; i++ {
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
			return updated, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".md") {
				continue
			}
			if excludeName != "" && name == excludeName {
				continue
			}
			abs := filepath.Join(dayAbs, name)
			fm, body, ok, err := readMemoryFile(abs)
			if err != nil {
				return updated, err
			}
			content := ShortTermContent{}
			if ok {
				content = ParseShortTermContent(body)
			} else {
				content = ParseShortTermContent(body)
			}
			changed := updateTaskList(&content.Tasks, updateMap)
			if updateTaskList(&content.FollowUps, updateMap) {
				changed = true
			}
			if !changed {
				continue
			}
			fm.UpdatedAt = now.UTC().Format(time.RFC3339)
			if strings.TrimSpace(fm.CreatedAt) == "" {
				fm.CreatedAt = now.UTC().Format(time.RFC3339)
			}
			if strings.TrimSpace(fm.Summary) == "" {
				fm.Summary = fallbackShortSummaryFromContent(content)
			}
			tDone, tTotal := taskCounts(content.Tasks)
			fDone, fTotal := taskCounts(content.FollowUps)
			fm.Tasks = formatTaskRatio(tDone, tTotal)
			fm.FollowUps = formatTaskRatio(fDone, fTotal)
			bodyOut := BuildShortTermBody(date.UTC().Format("2006-01-02"), content)
			if err := writeMemoryFile(abs, RenderFrontmatter(fm)+"\n"+bodyOut); err != nil {
				return updated, err
			}
			updated++
		}
	}
	return updated, nil
}

func updateTaskList(tasks *[]TaskItem, updates map[string]bool) bool {
	if len(updates) == 0 || tasks == nil {
		return false
	}
	changed := false
	for i := range *tasks {
		text := strings.TrimSpace((*tasks)[i].Text)
		if text == "" {
			continue
		}
		key := strings.ToLower(text)
		if done, ok := updates[key]; ok && done && !(*tasks)[i].Done {
			(*tasks)[i].Done = true
			changed = true
		}
	}
	return changed
}

func (m *Manager) UpdateLongTerm(subjectID string, promote PromoteDraft) (bool, error) {
	if m == nil {
		return false, fmt.Errorf("nil memory manager")
	}
	if len(promote.GoalsProjects) == 0 && len(promote.KeyFacts) == 0 {
		return false, nil
	}
	abs, _ := m.LongTermPath(subjectID)
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

	merged := MergeLongTerm(content, promote, m.nowUTC())
	merged = enforceLongTermLimits(merged)
	tasksLines, followLines := extractTodoSections(body)
	tDone, tTotal := taskCounts(parseTodoSection(tasksLines))
	fDone, fTotal := taskCounts(parseTodoSection(followLines))
	bodyOut := BuildLongTermBody(merged)
	bodyOut = appendTodoSections(bodyOut, tasksLines, followLines)
	fm = applyLongTermFrontmatter(fm, subjectID, merged, m.nowUTC(), tDone, tTotal, fDone, fTotal)

	if err := writeMemoryFile(abs, RenderFrontmatter(fm)+"\n"+bodyOut); err != nil {
		return false, err
	}
	return true, nil
}

func (m *Manager) attachRelatedLink(content ShortTermContent, date time.Time) ShortTermContent {
	_ = date
	return content
}

func applyShortTermFrontmatter(existing Frontmatter, summary string, meta WriteMeta, now time.Time, content ShortTermContent) Frontmatter {
	existing.CreatedAt = chooseTimestamp(existing.CreatedAt, now)
	existing.UpdatedAt = now.UTC().Format(time.RFC3339)
	existing.Summary = summary
	tDone, tTotal := taskCounts(content.Tasks)
	fDone, fTotal := taskCounts(content.FollowUps)
	existing.Tasks = formatTaskRatio(tDone, tTotal)
	existing.FollowUps = formatTaskRatio(fDone, fTotal)
	if strings.TrimSpace(meta.SessionID) != "" {
		existing.SessionID = strings.TrimSpace(meta.SessionID)
	}
	if strings.TrimSpace(meta.Source) != "" {
		existing.Source = strings.TrimSpace(meta.Source)
	}
	if strings.TrimSpace(meta.Channel) != "" {
		existing.Channel = strings.TrimSpace(meta.Channel)
	}
	if len(meta.Usernames) > 0 {
		existing.Usernames = mergeUsernames(existing.Usernames, meta.Usernames)
	}
	if strings.TrimSpace(meta.ContactID) != "" {
		existing.ContactID = strings.TrimSpace(meta.ContactID)
	}
	if strings.TrimSpace(meta.ContactNickname) != "" {
		existing.ContactNickname = strings.TrimSpace(meta.ContactNickname)
	}
	return existing
}

func mergeUsernames(existing []string, incoming []string) []string {
	if len(incoming) == 0 {
		return existing
	}
	seen := make(map[string]bool, len(existing)+len(incoming))
	out := make([]string, 0, len(existing)+len(incoming))
	add := func(username string) {
		username = strings.TrimSpace(username)
		if username == "" {
			return
		}
		if strings.HasPrefix(username, "@") {
			username = strings.TrimSpace(username[1:])
		}
		if username == "" {
			return
		}
		key := strings.ToLower(username)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, "@"+username)
	}
	for _, username := range existing {
		add(username)
	}
	for _, username := range incoming {
		add(username)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func applyLongTermFrontmatter(existing Frontmatter, subjectID string, content LongTermContent, now time.Time, tasksDone int, tasksTotal int, followDone int, followTotal int) Frontmatter {
	existing.CreatedAt = chooseTimestamp(existing.CreatedAt, now)
	existing.UpdatedAt = now.UTC().Format(time.RFC3339)
	if strings.TrimSpace(existing.Summary) == "" {
		existing.Summary = summarizeLongTerm(content)
	} else {
		existing.Summary = summarizeLongTerm(content)
	}
	existing.Tasks = formatTaskRatio(tasksDone, tasksTotal)
	existing.FollowUps = formatTaskRatio(followDone, followTotal)
	existing.SubjectID = strings.TrimSpace(subjectID)
	return existing
}

func fallbackShortSummary(draft SessionDraft) string {
	for _, item := range draft.SessionSummary {
		val := strings.TrimSpace(item.Value)
		if val != "" {
			return truncateSummary(val)
		}
	}
	for _, item := range draft.TemporaryFacts {
		val := strings.TrimSpace(item.Value)
		if val != "" {
			return truncateSummary(val)
		}
	}
	for _, item := range draft.Tasks {
		val := strings.TrimSpace(item.Text)
		if val != "" {
			return truncateSummary("Task: " + val)
		}
	}
	return defaultShortSummary
}

func fallbackShortSummaryFromContent(content ShortTermContent) string {
	if len(content.SessionSummary) > 0 {
		val := strings.TrimSpace(content.SessionSummary[0].Value)
		if val != "" {
			return truncateSummary(val)
		}
	}
	if len(content.TemporaryFacts) > 0 {
		val := strings.TrimSpace(content.TemporaryFacts[0].Value)
		if val != "" {
			return truncateSummary(val)
		}
	}
	for _, task := range content.Tasks {
		val := strings.TrimSpace(task.Text)
		if val != "" {
			return truncateSummary("Task: " + val)
		}
	}
	return defaultShortSummary
}

func summarizeLongTerm(content LongTermContent) string {
	parts := make([]string, 0, 2)
	if len(content.Goals) > 0 {
		parts = append(parts, "Goals: "+kvSummary(content.Goals[0]))
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

func ensureShortTermDefaults(content ShortTermContent, draft SessionDraft) ShortTermContent {
	if len(content.SessionSummary) == 0 {
		summary := strings.TrimSpace(draft.Summary)
		if summary != "" {
			content.SessionSummary = []KVItem{{Title: "Summary", Value: summary}}
		}
	}
	return content
}

func extractTodoSections(body string) (tasks []string, followUps []string) {
	var current string
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "## ") {
			current = strings.TrimSpace(strings.TrimPrefix(trim, "## "))
			continue
		}
		if current == "" || trim == "" {
			continue
		}
		switch current {
		case sectionTasks:
			tasks = append(tasks, trim)
		case sectionFollowUps:
			followUps = append(followUps, trim)
		}
	}
	return tasks, followUps
}

func appendTodoSections(body string, tasks []string, followUps []string) string {
	b := strings.TrimSpace(body)
	if len(tasks) == 0 && len(followUps) == 0 {
		if b == "" {
			return ""
		}
		return b + "\n"
	}
	var out strings.Builder
	if b != "" {
		out.WriteString(b)
		out.WriteString("\n\n")
	}
	if len(tasks) > 0 {
		out.WriteString("## ")
		out.WriteString(sectionTasks)
		out.WriteString("\n")
		for _, line := range tasks {
			out.WriteString(line)
			out.WriteString("\n")
		}
		out.WriteString("\n")
	}
	if len(followUps) > 0 {
		out.WriteString("## ")
		out.WriteString(sectionFollowUps)
		out.WriteString("\n")
		for _, line := range followUps {
			out.WriteString(line)
			out.WriteString("\n")
		}
		out.WriteString("\n")
	}
	return strings.TrimSpace(out.String()) + "\n"
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
