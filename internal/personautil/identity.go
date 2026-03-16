package personautil

import (
	"os"
	"path/filepath"
	"strings"

	markdownutil "github.com/quailyquaily/mistermorph/internal/markdown"
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
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
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
