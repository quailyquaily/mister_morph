package skills

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Skill struct {
	ID           string
	Name         string
	RootDir      string
	RootRank     int
	Dir          string
	SkillMD      string
	Contents     string
	AuthProfiles []string
}

type DiscoverOptions struct {
	Roots []string
}

func DefaultRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".morph", "skills"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".codex", "skills"),
	}
}

func Discover(opts DiscoverOptions) ([]Skill, error) {
	roots := normalizeRoots(opts.Roots)

	var out []Skill
	var firstErr error
	seenByID := make(map[string]bool)

	for rootRank, root := range roots {
		root = strings.TrimSpace(expandHome(root))
		if root == "" {
			continue
		}
		info, err := os.Stat(root)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			continue
		}

		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if d.Name() != "SKILL.md" {
				return nil
			}

			dir := filepath.Dir(path)
			rel, relErr := filepath.Rel(root, dir)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			id := rel
			if id == "." || id == "" {
				id = filepath.Base(dir)
			}

			name := filepath.Base(dir)
			idKey := strings.ToLower(id)
			if seenByID[idKey] {
				return nil
			}
			seenByID[idKey] = true
			out = append(out, Skill{
				ID:       id,
				Name:     name,
				RootDir:  root,
				RootRank: rootRank,
				Dir:      dir,
				SkillMD:  path,
			})
			return nil
		})
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			if out[i].RootRank == out[j].RootRank {
				return out[i].ID < out[j].ID
			}
			return out[i].RootRank < out[j].RootRank
		}
		return out[i].Name < out[j].Name
	})

	return out, firstErr
}

func Load(skill Skill, maxBytes int64) (Skill, error) {
	data, err := os.ReadFile(skill.SkillMD)
	if err != nil {
		return Skill{}, err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		data = data[:maxBytes]
	}
	skill.Contents = string(data)
	if fm, ok := ParseFrontmatter(skill.Contents); ok {
		skill.AuthProfiles = fm.AuthProfiles
	}
	return skill, nil
}

func LoadPreview(skill Skill, maxBytes int64) (Skill, error) {
	f, err := os.Open(skill.SkillMD)
	if err != nil {
		return Skill{}, err
	}
	defer f.Close()

	if maxBytes <= 0 {
		maxBytes = 2048
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes))
	if err != nil {
		return Skill{}, err
	}
	skill.Contents = string(data)
	if fm, ok := ParseFrontmatter(skill.Contents); ok {
		skill.AuthProfiles = fm.AuthProfiles
	}
	return skill, nil
}

func Resolve(skills []Skill, query string) (Skill, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return Skill{}, fmt.Errorf("empty skill query")
	}

	lower := strings.ToLower(q)
	for _, s := range skills {
		if strings.ToLower(s.ID) == lower {
			return s, nil
		}
	}

	var (
		best    Skill
		bestSet bool
	)
	for _, s := range skills {
		if strings.ToLower(s.Name) != lower {
			continue
		}
		if !bestSet || s.RootRank < best.RootRank {
			best = s
			bestSet = true
		} else if s.RootRank == best.RootRank && s.ID < best.ID {
			best = s
			bestSet = true
		}
	}
	if bestSet {
		return best, nil
	}
	return Skill{}, fmt.Errorf("skill not found: %s", query)
}

var dollarSkillRe = regexp.MustCompile(`\$(?P<name>[A-Za-z0-9_.-]+)`)

func ReferencedSkillNames(task string) []string {
	matches := dollarSkillRe.FindAllStringSubmatch(task, -1)
	if len(matches) == 0 {
		return nil
	}
	uniq := make(map[string]bool, len(matches))
	var out []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if uniq[key] {
			continue
		}
		uniq[key] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func normalizeRoots(roots []string) []string {
	roots = append([]string{}, roots...)
	if len(roots) == 0 {
		roots = DefaultRoots()
	}

	expanded := make([]string, 0, len(roots))
	seen := make(map[string]bool, len(roots))
	for _, r := range roots {
		r = strings.TrimSpace(expandHome(r))
		if r == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(r))
		if seen[key] {
			continue
		}
		seen[key] = true
		expanded = append(expanded, r)
	}

	// Enforce priority order for the well-known roots (even if provided out of order).
	home, _ := os.UserHomeDir()
	if strings.TrimSpace(home) == "" {
		return expanded
	}
	want := []string{
		filepath.Join(home, ".morph", "skills"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".codex", "skills"),
	}
	wantKeys := map[string]int{}
	for i, w := range want {
		wantKeys[strings.ToLower(filepath.Clean(w))] = i
	}

	var prioritized []string
	rest := make([]string, 0, len(expanded))
	tmp := make([]string, len(want))
	for _, r := range expanded {
		k := strings.ToLower(filepath.Clean(r))
		if idx, ok := wantKeys[k]; ok {
			tmp[idx] = r
			continue
		}
		rest = append(rest, r)
	}
	for _, r := range tmp {
		if strings.TrimSpace(r) != "" {
			prioritized = append(prioritized, r)
		}
	}
	prioritized = append(prioritized, rest...)
	return prioritized
}

func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return filepath.Clean(p)
		}
		if p == "~" {
			return home
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~/"))
	}
	return filepath.Clean(p)
}
