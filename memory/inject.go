package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (m *Manager) BuildInjection(subjectID string, reqCtx RequestContext, maxItems int) (string, error) {
	if m == nil {
		return "", fmt.Errorf("nil memory manager")
	}
	if maxItems <= 0 {
		maxItems = 50
	}

	longSummary := ""
	if reqCtx == ContextPrivate {
		ls, err := m.LoadLongTermSummary(subjectID)
		if err != nil {
			return "", err
		}
		longSummary = ls
	}

	shortSummaries, err := m.LoadShortTermSummaries(m.ShortTermDays)
	if err != nil {
		return "", err
	}

	return formatInjection(longSummary, shortSummaries, maxItems), nil
}

func (m *Manager) LoadLongTermSummary(subjectID string) (string, error) {
	abs, err := m.ensureLongTermIndex(subjectID)
	if err != nil {
		return "", err
	}
	if abs == "" {
		return "", nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	fm, body, ok := ParseFrontmatter(string(data))
	content := ParseLongTermContent(body)
	if len(content.Goals) == 0 && len(content.Facts) == 0 {
		return "", nil
	}
	if ok && strings.TrimSpace(fm.Summary) != "" {
		return strings.TrimSpace(fm.Summary), nil
	}
	summary := summarizeLongTerm(content)
	return strings.TrimSpace(summary), nil
}

func (m *Manager) ensureLongTermIndex(subjectID string) (string, error) {
	abs, _ := m.LongTermPath(subjectID)
	if abs == "" {
		return "", nil
	}
	_, err := os.Stat(abs)
	if err == nil {
		return abs, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	now := m.nowUTC()
	fm := Frontmatter{
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
		Summary:   defaultLongSummary,
		Tasks:     "0/0",
	}
	body := BuildLongTermBody(LongTermContent{})
	if err := writeMemoryFile(abs, RenderFrontmatter(fm)+"\n"+body); err != nil {
		return "", err
	}
	return abs, nil
}

func (m *Manager) LoadShortTermSummaries(days int) ([]ShortTermSummary, error) {
	if m == nil {
		return nil, nil
	}
	if days <= 0 {
		days = 7
	}
	now := m.nowUTC()
	out := make([]ShortTermSummary, 0, days)
	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i)
		dayAbs, dayRel := m.ShortTermDayDir(date)
		if dayAbs == "" || dayRel == "" {
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
			summary := ""
			if ok {
				summary = strings.TrimSpace(fm.Summary)
			}
			if summary == "" {
				content := ParseShortTermContent(body)
				if len(content.SummaryItems) > 0 {
					summary = strings.TrimSpace(content.SummaryItems[0].Content)
				}
			}
			if summary == "" {
				continue
			}
			rel := filepath.ToSlash(filepath.Join(dayRel, name))
			out = append(out, ShortTermSummary{
				Date:    date.UTC().Format("2006-01-02"),
				Summary: summary,
				RelPath: filepath.ToSlash(rel),
			})
		}
	}
	return out, nil
}

func formatInjection(longSummary string, shortSummaries []ShortTermSummary, maxItems int) string {
	lines := make([]string, 0, 8)
	count := 0
	if strings.TrimSpace(longSummary) != "" && count < maxItems {
		lines = append(lines, "<Memory:LongTerm:Summary>")
		lines = append(lines, "- "+strings.TrimSpace(longSummary))
		count++
	}

	if len(shortSummaries) > 0 && count < maxItems {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "<Memory:ShortTerm:Recent>")
		summaries := append([]ShortTermSummary{}, shortSummaries...)
		sort.SliceStable(summaries, func(i, j int) bool {
			return summaries[i].Date > summaries[j].Date
		})
		for _, s := range summaries {
			if count >= maxItems {
				break
			}
			line := fmt.Sprintf("- %s: %s (%s)", s.Date, strings.TrimSpace(s.Summary), strings.TrimSpace(s.RelPath))
			lines = append(lines, line)
			count++
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}
