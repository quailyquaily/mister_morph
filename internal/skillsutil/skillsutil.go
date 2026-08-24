package skillsutil

import (
	"context"
	"log/slog"
	"path"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/caprefs"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/skills"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type ConfigReader interface {
	GetString(string) string
	GetStringSlice(string) []string
	GetBool(string) bool
	IsSet(string) bool
}

type SkillsConfig struct {
	Roots     []string
	DirName   string
	Enabled   bool
	Requested []string
	Trace     bool
}

func SkillsConfigFromReader(r ConfigReader) SkillsConfig {
	if r == nil {
		r = viper.GetViper()
	}
	cfg := SkillsConfig{
		Roots:     skillsRootsFromReader(r),
		DirName:   strings.TrimSpace(r.GetString("skills.dir_name")),
		Enabled:   skillsEnabledFromReader(r),
		Requested: append([]string{}, r.GetStringSlice("skills.load")...),
		Trace:     strings.EqualFold(strings.TrimSpace(r.GetString("logging.level")), "debug"),
	}
	cfg.DirName = normalizeSkillsDirName(cfg.DirName)
	return cfg
}

func skillsRootsFromReader(r ConfigReader) []string {
	if r == nil {
		return statepaths.DefaultSkillsRoots()
	}
	return []string{pathutil.ResolveStateChildDir(r.GetString("file_state_dir"), r.GetString("skills.dir_name"), "skills")}
}

func SkillsConfigFromViper() SkillsConfig {
	return SkillsConfigFromReader(viper.GetViper())
}

func SkillsConfigFromRunCmd(cmd *cobra.Command) SkillsConfig {
	cfg := SkillsConfigFromViper()

	// Local flags override config/env.
	roots, _ := cmd.Flags().GetStringArray("skills-dir")
	if cmd.Flags().Changed("skills-dir") {
		cfg.Roots = roots
	}

	cfg.Enabled = configutil.FlagOrViperBool(cmd, "skills-enabled", "skills.enabled")

	if cmd.Flags().Changed("skill") {
		cfg.Requested, _ = cmd.Flags().GetStringArray("skill")
	}

	return cfg
}

func PromptSpecWithSkills(ctx context.Context, log *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, cfg SkillsConfig) (agent.PromptSpec, []string, error) {
	if log == nil {
		log = slog.Default()
	}
	dirName := normalizeSkillsDirName(cfg.DirName)

	spec := agent.DefaultPromptSpec()
	var loadedOrdered []string

	referenceNames := caprefs.Names(task)
	if !cfg.Enabled && len(referenceNames) == 0 {
		return spec, nil, nil
	}

	discovered, err := skills.Discover(skills.DiscoverOptions{Roots: cfg.Roots})
	if err != nil {
		if cfg.Trace {
			log.Warn("skills_discover_warning", "error", err.Error())
		}
	}

	loadedSkillIDs := make(map[string]bool)

	referenced := resolveReferencedSkillIDs(referenceNames, discovered)
	requested := append([]string{}, referenced...)
	if cfg.Enabled {
		requested = append(append([]string{}, cfg.Requested...), referenced...)
	}

	uniq := make(map[string]bool, len(requested))
	var finalReq []string
	loadAll := false
	for _, r := range requested {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if r == "*" {
			loadAll = true
		}
		k := strings.ToLower(r)
		if uniq[k] {
			continue
		}
		uniq[k] = true
		finalReq = append(finalReq, r)
	}
	if len(finalReq) == 0 {
		if !cfg.Enabled {
			return spec, nil, nil
		}
		loadAll = true
	}
	if loadAll {
		finalReq = finalReq[:0]
		for _, s := range discovered {
			finalReq = append(finalReq, s.ID)
		}
		log.Info("skills_load_all_requested", "count", len(finalReq))
	}

	// Configured skills require enabled mode; task references are always explicit.
	for _, q := range finalReq {
		s, err := skills.Resolve(discovered, q)
		if err != nil {
			log.Warn("skill_ignored", "skills_enabled", cfg.Enabled, "query", q, "reason", err.Error())
			continue
		}
		if loadedSkillIDs[strings.ToLower(s.ID)] {
			continue
		}
		skillLoaded, err := skills.LoadFrontmatter(s, 64*1024)
		if err != nil {
			return agent.PromptSpec{}, nil, err
		}
		loadedSkillIDs[strings.ToLower(skillLoaded.ID)] = true
		loadedOrdered = append(loadedOrdered, skillLoaded.ID)
		name := strings.TrimSpace(skillLoaded.Name)
		if name == "" {
			name = strings.TrimSpace(skillLoaded.ID)
		}
		desc := strings.TrimSpace(skillLoaded.Description)
		if desc == "" {
			desc = "(not provided)"
		}
		authProfiles := make([]string, 0, len(skillLoaded.AuthProfiles))
		for _, profile := range skillLoaded.AuthProfiles {
			profile = strings.TrimSpace(profile)
			if profile == "" {
				continue
			}
			authProfiles = append(authProfiles, profile)
		}
		spec.Skills = append(spec.Skills, agent.PromptSkill{
			Name:         name,
			FilePath:     skillPromptFilePath(skillLoaded.ID, dirName),
			Description:  desc,
			AuthProfiles: authProfiles,
		})

		log.Info("skill_loaded", "skills_enabled", cfg.Enabled, "name", name, "id", skillLoaded.ID, "path", skillLoaded.SkillMD)
	}
	log.Info("skills_loaded", "skills_enabled", cfg.Enabled, "count", len(spec.Skills))
	return spec, loadedOrdered, nil
}

func ResolveTaskSkillRefs(task string, cfg SkillsConfig) map[string]bool {
	referenceNames := caprefs.Names(task)
	if len(referenceNames) == 0 {
		return nil
	}
	discovered, err := skills.Discover(skills.DiscoverOptions{Roots: cfg.Roots})
	if err != nil && len(discovered) == 0 {
		return nil
	}
	return resolveReferencedSkillNames(referenceNames, discovered)
}

func skillsEnabledFromReader(r ConfigReader) bool {
	if r == nil {
		return true
	}
	if r.IsSet("skills.enabled") {
		return r.GetBool("skills.enabled")
	}
	return true
}

func normalizeSkillsDirName(raw string) string {
	dirName := strings.TrimSpace(raw)
	if dirName == "" {
		return "skills"
	}
	return dirName
}

func skillPromptFilePath(skillID string, dirName string) string {
	dirName = normalizeSkillsDirName(dirName)
	id := strings.Trim(strings.TrimSpace(skillID), "/\\")
	if id == "" {
		return path.Join("file_state_dir", dirName, "SKILL.md")
	}
	return path.Join("file_state_dir", dirName, id, "SKILL.md")
}

func resolveReferencedSkillIDs(referenceNames []string, discovered []skills.Skill) []string {
	out := make([]string, 0, len(referenceNames))
	for _, q := range referenceNames {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		s, err := skills.Resolve(discovered, q)
		if err != nil {
			continue
		}
		id := strings.TrimSpace(s.ID)
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	return out
}

func resolveReferencedSkillNames(referenceNames []string, discovered []skills.Skill) map[string]bool {
	out := make(map[string]bool, len(referenceNames))
	for _, q := range referenceNames {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		if _, err := skills.Resolve(discovered, q); err != nil {
			continue
		}
		out[strings.ToLower(q)] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
