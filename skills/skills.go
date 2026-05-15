package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/statepaths"
)

type Skill struct {
	ID           string
	Name         string
	Description  string
	RootDir      string
	RootRank     int
	Dir          string
	SkillMD      string
	Contents     string
	Requirements []string
	AuthProfiles []string
}

func applyFrontmatter(skill Skill, fm Frontmatter) Skill {
	if strings.TrimSpace(fm.Name) != "" {
		skill.Name = strings.TrimSpace(fm.Name)
	}
	skill.Description = strings.TrimSpace(fm.Description)
	skill.Requirements = append([]string{}, fm.Requirements...)
	skill.AuthProfiles = append([]string{}, fm.AuthProfiles...)
	return skill
}

type DiscoverOptions struct {
	Roots []string
}

func DefaultRoots() []string {
	return statepaths.DefaultSkillsRoots()
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

		walkRoot := root
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			walkRoot = resolved
		}

		err = filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if d.Name() != "SKILL.md" {
				return nil
			}

			appendDiscoveredSkill(&out, seenByID, rootRank, root, walkRoot, filepath.Dir(path))
			return nil
		})
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if err := discoverLinkedSkillDirs(&out, seenByID, rootRank, root, walkRoot); err != nil && firstErr == nil {
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

func discoverLinkedSkillDirs(out *[]Skill, seenByID map[string]bool, rootRank int, root, walkRoot string) error {
	entries, err := os.ReadDir(walkRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink == 0 {
			continue
		}
		dir := filepath.Join(walkRoot, entry.Name())
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		skillMD := filepath.Join(dir, "SKILL.md")
		info, err = os.Stat(skillMD)
		if err != nil || info.IsDir() {
			continue
		}
		appendDiscoveredSkill(out, seenByID, rootRank, root, walkRoot, dir)
	}
	return nil
}

func appendDiscoveredSkill(out *[]Skill, seenByID map[string]bool, rootRank int, root, walkRoot, dir string) {
	rel, relErr := filepath.Rel(walkRoot, dir)
	if relErr != nil {
		return
	}
	rel = filepath.ToSlash(rel)

	logicalDir := dir
	if rel != "." && rel != "" {
		logicalDir = filepath.Join(root, filepath.FromSlash(rel))
	} else if walkRoot != root {
		logicalDir = root
	}

	id := rel
	if id == "." || id == "" {
		id = filepath.Base(logicalDir)
	}

	name := filepath.Base(logicalDir)
	idKey := strings.ToLower(id)
	if seenByID[idKey] {
		return
	}
	seenByID[idKey] = true

	*out = append(*out, Skill{
		ID:       id,
		Name:     name,
		RootDir:  root,
		RootRank: rootRank,
		Dir:      logicalDir,
		SkillMD:  filepath.Join(logicalDir, "SKILL.md"),
	})
}

// LoadFrontmatter loads only metadata parsed from SKILL.md frontmatter.
// It does not populate Skill.Contents.
func LoadFrontmatter(skill Skill, maxBytes int64) (Skill, error) {
	data, err := os.ReadFile(skill.SkillMD)
	if err != nil {
		return Skill{}, err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		data = data[:maxBytes]
	}
	if fm, ok := ParseFrontmatter(string(data)); ok {
		skill = applyFrontmatter(skill, fm)
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

	defaultRoots := DefaultRoots()
	if len(defaultRoots) == 0 {
		return expanded
	}
	preferred := strings.ToLower(filepath.Clean(defaultRoots[0]))
	idx := -1
	for i, r := range expanded {
		if strings.ToLower(filepath.Clean(r)) == preferred {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return expanded
	}
	out := make([]string, 0, len(expanded))
	out = append(out, expanded[idx])
	out = append(out, expanded[:idx]...)
	out = append(out, expanded[idx+1:]...)
	return out
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
