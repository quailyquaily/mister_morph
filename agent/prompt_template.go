package agent

import (
	_ "embed"
	"sort"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/prompttmpl"
	"github.com/quailyquaily/mistermorph/tools"
)

//go:embed prompts/system.md
var systemPromptTemplateSource string

var systemPromptTemplate = prompttmpl.MustParse("agent_system_prompt", systemPromptTemplateSource, nil)

type systemPromptTemplateBlock struct {
	Content string
}

type systemPromptTemplateSkill struct {
	Name         string
	FilePath     string
	Description  string
	AuthProfiles []string
}

type systemPromptTemplateData struct {
	Identity      string
	Skills        []systemPromptTemplateSkill
	Blocks        []systemPromptTemplateBlock
	ToolSummaries string
	HasPlanCreate bool
	Rules         []string
}

func renderSystemPrompt(registry *tools.Registry, spec PromptSpec) (string, error) {
	data := systemPromptTemplateData{
		Identity: spec.Identity,
		Skills:   make([]systemPromptTemplateSkill, 0, len(spec.Skills)),
		Blocks:   make([]systemPromptTemplateBlock, 0, len(spec.Blocks)),
		Rules:    make([]string, 0, len(spec.Rules)),
	}
	for _, sk := range spec.Skills {
		name := strings.TrimSpace(sk.Name)
		filePath := strings.TrimSpace(sk.FilePath)
		desc := strings.TrimSpace(sk.Description)
		authProfiles := make([]string, 0, len(sk.AuthProfiles))
		for _, profile := range sk.AuthProfiles {
			profile = strings.TrimSpace(profile)
			if profile == "" {
				continue
			}
			authProfiles = append(authProfiles, profile)
		}
		if name == "" {
			name = "unknown-skill"
		}
		if filePath == "" {
			filePath = "(unknown)"
		}
		if desc == "" {
			desc = "(not provided)"
		}
		data.Skills = append(data.Skills, systemPromptTemplateSkill{
			Name:         name,
			FilePath:     filePath,
			Description:  desc,
			AuthProfiles: authProfiles,
		})
	}
	for _, blk := range spec.Blocks {
		content := strings.TrimSpace(blk.Content)
		if content == "" {
			continue
		}
		data.Blocks = append(data.Blocks, systemPromptTemplateBlock{
			Content: content,
		})
	}
	for _, rule := range spec.Rules {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		data.Rules = append(data.Rules, rule)
	}
	if registry != nil {
		data.ToolSummaries = registry.FormatToolSummaries()
		_, data.HasPlanCreate = registry.Get("plan_create")
	}
	return prompttmpl.Render(systemPromptTemplate, data)
}

func availableShellToolName(registry *tools.Registry) string {
	names := availableShellToolNames(registry)
	if len(names) != 1 {
		return ""
	}
	return names[0]
}

func availableShellToolNames(registry *tools.Registry) []string {
	if registry == nil {
		return nil
	}
	names := make([]string, 0, 2)
	if _, ok := registry.Get("bash"); ok {
		names = append(names, "bash")
	}
	if _, ok := registry.Get("powershell"); ok {
		names = append(names, "powershell")
	}
	sort.Strings(names)
	return names
}
