package contextbudget

import (
	_ "embed"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const budgetRatio = 0.8

//go:embed context_windows.yaml
var contextWindowsYAML []byte

type catalogDocument struct {
	Models map[string]catalogEntry `yaml:"models"`
}

type catalogEntry struct {
	ContextWindowTokens int    `yaml:"context_window_tokens"`
	Source              string `yaml:"source"`
}

type CatalogModel struct {
	Key           string
	ContextWindow int
	Source        string
}

type ResolvedBudget struct {
	Provider        string
	Model           string
	ContextWindow   int
	MaxTokenBudget  int
	BudgetSource    string
	ContextSource   string
	KnownModel      bool
	ExplicitBudget  bool
	CompressionOn   bool
}

var (
	catalogOnce sync.Once
	catalogData map[string]CatalogModel
	catalogErr  error

	dateSuffixRE      = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)
	compactDateRE     = regexp.MustCompile(`-\d{8}$`)
	xaiVersionSuffix  = regexp.MustCompile(`-\d{4}(?:-[a-z0-9]+)?$`)
)

func ResolveBudget(explicitBudget *int, explicitSource string, provider string, model string) (ResolvedBudget, error) {
	provider = normalizeProvider(provider)
	model = strings.TrimSpace(model)
	resolved := ResolvedBudget{
		Provider:      provider,
		Model:         model,
		BudgetSource:  strings.TrimSpace(explicitSource),
		ExplicitBudget: explicitBudget != nil && *explicitBudget > 0,
		CompressionOn: true,
	}
	catalogModel, ok, err := LookupModel(model)
	if err != nil {
		return ResolvedBudget{}, err
	}
	if ok {
		resolved.KnownModel = true
		resolved.ContextWindow = catalogModel.ContextWindow
		resolved.ContextSource = catalogModel.Source
	}
	if explicitBudget != nil && *explicitBudget > 0 {
		resolved.MaxTokenBudget = *explicitBudget
		if resolved.ContextWindow <= 0 {
			resolved.ContextWindow = *explicitBudget
			resolved.ContextSource = strings.TrimSpace(explicitSource)
		}
		return resolved, nil
	}
	if !ok || catalogModel.ContextWindow <= 0 {
		return resolved, nil
	}
	resolved.MaxTokenBudget = int(math.Floor(float64(catalogModel.ContextWindow) * budgetRatio))
	resolved.BudgetSource = "builtin_context_window:" + catalogModel.Key
	return resolved, nil
}

func LookupModel(model string) (CatalogModel, bool, error) {
	data, err := loadCatalog()
	if err != nil {
		return CatalogModel{}, false, err
	}
	for _, key := range modelLookupCandidates(model) {
		entry, ok := data[key]
		if ok {
			return entry, true, nil
		}
	}
	return CatalogModel{}, false, nil
}

func loadCatalog() (map[string]CatalogModel, error) {
	catalogOnce.Do(func() {
		var doc catalogDocument
		if err := yaml.Unmarshal(contextWindowsYAML, &doc); err != nil {
			catalogErr = fmt.Errorf("parse embedded context window catalog: %w", err)
			return
		}
		loaded := make(map[string]CatalogModel, len(doc.Models))
		for rawKey, entry := range doc.Models {
			key := normalizeModelKey(rawKey)
			if key == "" || entry.ContextWindowTokens <= 0 {
				continue
			}
			loaded[key] = CatalogModel{
				Key:           key,
				ContextWindow: entry.ContextWindowTokens,
				Source:        strings.TrimSpace(entry.Source),
			}
		}
		catalogData = loaded
	})
	if catalogErr != nil {
		return nil, catalogErr
	}
	return catalogData, nil
}

func modelLookupCandidates(model string) []string {
	normalized := normalizeModelKey(model)
	if normalized == "" {
		return nil
	}
	add := func(out *[]string, value string, seen map[string]bool) {
		value = normalizeModelKey(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		*out = append(*out, value)
	}
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	add(&out, normalized, seen)
	if idx := strings.Index(normalized, "/models/"); idx >= 0 {
		add(&out, normalized[idx+len("/models/"):], seen)
	}
	if idx := strings.LastIndexByte(normalized, '/'); idx >= 0 && idx+1 < len(normalized) {
		add(&out, normalized[idx+1:], seen)
	}
	for i := 0; i < len(out); i++ {
		candidate := out[i]
		add(&out, strings.TrimSuffix(candidate, "-latest"), seen)
		add(&out, dateSuffixRE.ReplaceAllString(candidate, ""), seen)
		add(&out, compactDateRE.ReplaceAllString(candidate, ""), seen)
		if strings.HasPrefix(candidate, "grok-") {
			add(&out, xaiVersionSuffix.ReplaceAllString(candidate, ""), seen)
		}
	}
	return out
}

func normalizeModelKey(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func normalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "openai"
	}
	return provider
}
