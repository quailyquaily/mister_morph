package personautil

import (
	"os"
	"path/filepath"
	"strings"

	markdownutil "github.com/quailyquaily/mistermorph/internal/markdown"
	"gopkg.in/yaml.v3"
)

const IdentityFilename = "IDENTITY.md"

func LoadAgentName(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, IdentityFilename))
	if err != nil {
		return ""
	}
	if strings.EqualFold(markdownutil.FrontmatterStatus(string(raw)), "draft") {
		return ""
	}
	return ParseIdentityName(markdownutil.StripFrontmatter(string(raw)))
}

func ParseIdentityName(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if value := parseIdentityNameFromYAMLBlock(raw); value != "" {
		return value
	}
	lines := strings.Split(raw, "\n")
	const prefix = "- **Name:**"
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		if value := cleanIdentityNameValue(strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))); value != "" {
			return value
		}
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimSpace(lines[j])
			if next == "" {
				continue
			}
			if strings.HasPrefix(next, "- **") || strings.HasPrefix(next, "#") || next == "---" {
				return ""
			}
			return cleanIdentityNameValue(next)
		}
		return ""
	}
	return ""
}

func parseIdentityNameFromYAMLBlock(raw string) string {
	block := firstFencedYAMLBlock(raw)
	if strings.TrimSpace(block) == "" {
		return ""
	}
	var profile struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal([]byte(block), &profile); err != nil {
		return ""
	}
	return cleanIdentityNameValue(profile.Name)
}

func firstFencedYAMLBlock(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	inYAML := false
	yamlLines := make([]string, 0, 16)

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if !inYAML && strings.HasPrefix(line, "```") {
			lowerFence := strings.ToLower(line)
			if strings.HasPrefix(lowerFence, "```yaml") || strings.HasPrefix(lowerFence, "```yml") {
				inYAML = true
				yamlLines = yamlLines[:0]
			}
			continue
		}
		if inYAML && strings.HasPrefix(line, "```") {
			return strings.Join(yamlLines, "\n")
		}
		if inYAML {
			yamlLines = append(yamlLines, rawLine)
		}
	}
	if inYAML && len(yamlLines) > 0 {
		return strings.Join(yamlLines, "\n")
	}
	return ""
}

func cleanIdentityNameValue(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, "*_`")
	if value == "" {
		return ""
	}
	switch strings.ToLower(value) {
	case "(pick one)", "pick one":
		return ""
	default:
		return value
	}
}
