package llm

import (
	"embed"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed model_context_windows.yaml
var modelContextWindowFS embed.FS

type ModelContextWindow struct {
	Provider            string
	Model               string
	NormalizedModel     string
	ContextWindowTokens int64
	Sources             []ModelContextWindowSource
}

type ModelContextWindowSource struct {
	URL       string `yaml:"url"`
	CheckedAt string `yaml:"checked_at"`
	Note      string `yaml:"note"`
}

type modelContextWindowCatalogItem struct {
	Provider            string                     `yaml:"provider"`
	Model               string                     `yaml:"model"`
	Aliases             []string                   `yaml:"aliases"`
	ContextWindowTokens int64                      `yaml:"context_window_tokens"`
	Sources             []ModelContextWindowSource `yaml:"sources"`
}

var (
	modelContextWindowOnce sync.Once
	modelContextWindowMap  map[string]ModelContextWindow
)

func ResolveModelContextWindow(model string) (ModelContextWindow, bool) {
	key := normalizeContextWindowModelName(model)
	if key == "" {
		return ModelContextWindow{}, false
	}
	modelContextWindowOnce.Do(loadModelContextWindows)
	for _, candidate := range contextWindowLookupCandidates(key) {
		if entry, ok := modelContextWindowMap[candidate]; ok {
			return entry, true
		}
	}
	return ModelContextWindow{}, false
}

func loadModelContextWindows() {
	modelContextWindowMap = map[string]ModelContextWindow{}
	data, err := modelContextWindowFS.ReadFile("model_context_windows.yaml")
	if err != nil {
		return
	}
	var catalog struct {
		Version int                             `yaml:"version"`
		Models  []modelContextWindowCatalogItem `yaml:"models"`
	}
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return
	}
	for _, item := range catalog.Models {
		model := normalizeContextWindowModelName(item.Model)
		if model == "" || item.ContextWindowTokens <= 0 {
			continue
		}
		entry := ModelContextWindow{
			Provider:            strings.TrimSpace(item.Provider),
			Model:               strings.TrimSpace(item.Model),
			NormalizedModel:     model,
			ContextWindowTokens: item.ContextWindowTokens,
			Sources:             append([]ModelContextWindowSource(nil), item.Sources...),
		}
		modelContextWindowMap[model] = entry
		for _, alias := range item.Aliases {
			alias = normalizeContextWindowModelName(alias)
			if alias != "" {
				modelContextWindowMap[alias] = entry
			}
		}
	}
}

func normalizeContextWindowModelName(model string) string {
	model = normalizeModelName(model)
	model = strings.TrimPrefix(model, "models/")
	return model
}

func contextWindowLookupCandidates(model string) []string {
	model = normalizeContextWindowModelName(model)
	if model == "" {
		return nil
	}
	out := []string{model}
	if slash := strings.LastIndexByte(model, '/'); slash >= 0 && slash+1 < len(model) {
		out = append(out, model[slash+1:])
	}
	if base := stripDatedModelSuffix(model); base != model {
		out = append(out, base)
		if slash := strings.LastIndexByte(base, '/'); slash >= 0 && slash+1 < len(base) {
			out = append(out, base[slash+1:])
		}
	}
	return dedupeModelNames(out)
}

func stripDatedModelSuffix(model string) string {
	if len(model) < len("-2006-01-02") {
		return model
	}
	suffix := model[len(model)-len("-2006-01-02"):]
	if suffix[0] != '-' {
		return model
	}
	for i := 1; i < len(suffix); i++ {
		switch i {
		case 5, 8:
			if suffix[i] != '-' {
				return model
			}
		default:
			if suffix[i] < '0' || suffix[i] > '9' {
				return model
			}
		}
	}
	return model[:len(model)-len(suffix)]
}

func dedupeModelNames(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
