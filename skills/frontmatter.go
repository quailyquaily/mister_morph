package skills

import (
	"bufio"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Frontmatter struct {
	AuthProfiles []string `yaml:"auth_profiles"`
}

func ParseFrontmatter(contents string) (Frontmatter, bool) {
	// Minimal frontmatter support: YAML between leading --- ... ---.
	r := strings.NewReader(contents)
	sc := bufio.NewScanner(r)
	if !sc.Scan() {
		return Frontmatter{}, false
	}
	if strings.TrimSpace(sc.Text()) != "---" {
		return Frontmatter{}, false
	}

	var yamlLines []string
	foundEnd := false
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			foundEnd = true
			break
		}
		yamlLines = append(yamlLines, line)
	}
	if !foundEnd {
		return Frontmatter{}, false
	}

	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(strings.Join(yamlLines, "\n")), &fm); err != nil {
		return Frontmatter{}, false
	}

	if len(fm.AuthProfiles) == 0 {
		return Frontmatter{}, true
	}
	uniq := make(map[string]bool, len(fm.AuthProfiles))
	var out []string
	for _, p := range fm.AuthProfiles {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if uniq[p] {
			continue
		}
		uniq[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	fm.AuthProfiles = out
	return fm, true
}
