package skillsutil

import (
	"strings"
	"text/template"

	"github.com/quailyquaily/mistermorph/skills"
)

type SkillStatus struct {
	Enabled   bool
	Loaded    []SkillStatusItem
	Available []SkillStatusItem
}

type SkillStatusItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type skillStatusTemplateData struct {
	Sections []skillStatusTemplateSection
}

type skillStatusTemplateSection struct {
	Title string
	Count int
	Items []skillStatusTemplateItem
}

type skillStatusTemplateItem struct {
	Name        string
	Description string
}

const skillStatusEnabledTemplateText = `{{range .Sections}}**{{.Title}} ({{.Count}})**

{{range .Items}}- ` + "`{{.Name}}`\n" + `: {{.Description}}
{{end}}
{{end}}`

const skillStatusDisabledTemplateText = `> Skill is disabled`

var (
	skillStatusEnabledTemplate  = template.Must(template.New("skill-status-enabled").Parse(skillStatusEnabledTemplateText))
	skillStatusDisabledTemplate = template.Must(template.New("skill-status-disabled").Parse(skillStatusDisabledTemplateText))
)

func BuildSkillStatus(cfg SkillsConfig, currentLoaded []string) (SkillStatus, error) {
	status := SkillStatus{Enabled: cfg.Enabled}
	discovered, err := skills.Discover(skills.DiscoverOptions{Roots: cfg.Roots})
	if err != nil {
		return status, err
	}
	for i, sk := range discovered {
		sk, err := skills.LoadFrontmatter(sk, 64*1024)
		if err != nil {
			return status, err
		}
		discovered[i] = sk
	}

	loadedIDs := map[string]bool{}
	if cfg.Enabled {
		requested := append([]string{}, cfg.Requested...)
		requested = append(requested, currentLoaded...)
		finalReq, loadAll := normalizeSkillStatusRequests(requested)
		if len(finalReq) == 0 {
			loadAll = true
		}
		if loadAll {
			for _, sk := range discovered {
				loadedIDs[strings.ToLower(strings.TrimSpace(sk.ID))] = true
			}
		} else {
			for _, query := range finalReq {
				sk, err := skills.Resolve(discovered, query)
				if err != nil {
					continue
				}
				loadedIDs[strings.ToLower(strings.TrimSpace(sk.ID))] = true
			}
		}
	}

	for _, sk := range discovered {
		name := strings.TrimSpace(sk.Name)
		if name == "" {
			name = strings.TrimSpace(sk.ID)
		}
		item := SkillStatusItem{
			ID:          strings.TrimSpace(sk.ID),
			Name:        name,
			Description: strings.TrimSpace(sk.Description),
		}
		key := strings.ToLower(item.ID)
		if loadedIDs[key] {
			status.Loaded = append(status.Loaded, item)
		} else {
			status.Available = append(status.Available, item)
		}
	}
	return status, nil
}

func RenderSkillStatus(cfg SkillsConfig, currentLoaded []string) (string, error) {
	if !cfg.Enabled {
		var b strings.Builder
		if err := skillStatusDisabledTemplate.Execute(&b, nil); err != nil {
			return "", err
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
	status, err := BuildSkillStatus(cfg, currentLoaded)
	if err != nil {
		return "", err
	}
	data := skillStatusTemplateData{
		Sections: skillStatusSections(status),
	}
	tmpl := skillStatusDisabledTemplate
	if status.Enabled {
		tmpl = skillStatusEnabledTemplate
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", err
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func normalizeSkillStatusRequests(requested []string) ([]string, bool) {
	seen := map[string]bool{}
	var out []string
	loadAll := false
	for _, raw := range requested {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if item == "*" {
			loadAll = true
		}
		key := strings.ToLower(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out, loadAll
}

func skillStatusSections(status SkillStatus) []skillStatusTemplateSection {
	var sections []skillStatusTemplateSection
	if section, ok := skillStatusSection("Loaded Skills", status.Loaded); ok {
		sections = append(sections, section)
	}
	if section, ok := skillStatusSection("Available Skills", status.Available); ok {
		sections = append(sections, section)
	}
	return sections
}

func skillStatusSection(title string, items []SkillStatusItem) (skillStatusTemplateSection, bool) {
	out := make([]skillStatusTemplateItem, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = strings.TrimSpace(item.ID)
		}
		if name == "" {
			continue
		}
		desc := strings.TrimSpace(item.Description)
		if desc != "" {
			desc = "\n  " + strings.ReplaceAll(desc, "\n", "\n  ")
		}
		out = append(out, skillStatusTemplateItem{Name: name, Description: desc})
	}
	if len(out) == 0 {
		return skillStatusTemplateSection{}, false
	}
	return skillStatusTemplateSection{Title: title, Count: len(out), Items: out}, true
}
