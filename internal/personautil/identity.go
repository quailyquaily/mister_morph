package personautil

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"gopkg.in/yaml.v3"
)

const IdentityFilename = statepaths.IdentityFilename

func LoadAgentName(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, statepaths.PersonaDirName, statepaths.IdentityFilename))
	if err != nil {
		return ""
	}
	if value := parseIdentityNameFromYAML(string(raw)); value != "" {
		return value
	}
	return ""
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
