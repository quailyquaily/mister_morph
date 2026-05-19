package personautil

import (
	"os"
	"path/filepath"
	"strings"

	markdownutil "github.com/quailyquaily/mistermorph/internal/markdown"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"gopkg.in/yaml.v3"
)

const IdentityFilename = statepaths.IdentityFilename

func LoadAgentName(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	for _, candidate := range []struct {
		path string
		yaml bool
	}{
		{path: filepath.Join(stateDir, statepaths.PersonaDirName, statepaths.IdentityFilename), yaml: true},
		{path: filepath.Join(stateDir, statepaths.PersonaDirName, statepaths.LegacyIdentityFilename), yaml: false},
		{path: filepath.Join(stateDir, statepaths.LegacyIdentityFilename), yaml: false},
	} {
		raw, err := os.ReadFile(candidate.path)
		if err != nil {
			continue
		}
		if !candidate.yaml && strings.EqualFold(markdownutil.FrontmatterStatus(string(raw)), "draft") {
			continue
		}
		if candidate.yaml {
			if value := parseIdentityNameFromYAML(string(raw)); value != "" {
				return value
			}
			continue
		}
		if value := ParseIdentityName(markdownutil.StripFrontmatter(string(raw))); value != "" {
			return value
		}
	}
	return ""
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
	return parseIdentityNameFromYAML(block)
}

func parseIdentityNameFromYAML(raw string) string {
	var profile struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal([]byte(raw), &profile); err != nil {
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
