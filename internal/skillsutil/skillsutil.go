package skillsutil

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/skills"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type SkillsConfig struct {
	Roots         []string
	Mode          string
	Requested     []string
	Auto          bool
	MaxLoad       int
	PreviewBytes  int64
	CatalogLimit  int
	SelectTimeout time.Duration
	SelectorModel string
	Trace         bool
}

func SkillsConfigFromViper(model string) SkillsConfig {
	cfg := SkillsConfig{
		Roots: statepaths.DefaultSkillsRoots(),
		Mode:  strings.TrimSpace(viper.GetString("skills.mode")),
		Auto:  skillsAutoFromViper(),
		Requested: append(
			append([]string{}, viper.GetStringSlice("skill")...), // legacy
			viper.GetStringSlice("skills")...,                    // legacy
		),
		MaxLoad:       viper.GetInt("skills.max_load"),
		PreviewBytes:  viper.GetInt64("skills.preview_bytes"),
		CatalogLimit:  viper.GetInt("skills.catalog_limit"),
		SelectTimeout: viper.GetDuration("llm.request_timeout"),
		SelectorModel: strings.TrimSpace(viper.GetString("skills.selector_model")),
		Trace:         strings.EqualFold(strings.TrimSpace(viper.GetString("logging.level")), "debug"),
	}
	cfg.Requested = append(cfg.Requested, getStringSlice("skills.load")...)
	if strings.TrimSpace(cfg.Mode) == "" {
		cfg.Mode = "smart"
	}
	if strings.TrimSpace(cfg.SelectorModel) == "" {
		cfg.SelectorModel = model
	}
	return cfg
}

func SkillsConfigFromRunCmd(cmd *cobra.Command, model string) SkillsConfig {
	cfg := SkillsConfigFromViper(model)

	// Local flags override config/env.
	roots, _ := cmd.Flags().GetStringArray("skills-dir")
	if cmd.Flags().Changed("skills-dir") {
		cfg.Roots = roots
	}

	cfg.Mode = strings.TrimSpace(configutil.FlagOrViperString(cmd, "skills-mode", "skills.mode"))
	cfg.Auto = configutil.FlagOrViperBool(cmd, "skills-auto", "skills.auto")

	if cmd.Flags().Changed("skill") {
		cfg.Requested, _ = cmd.Flags().GetStringArray("skill")
	}

	cfg.MaxLoad = configutil.FlagOrViperInt(cmd, "skills-max-load", "skills.max_load")
	cfg.PreviewBytes = configutil.FlagOrViperInt64(cmd, "skills-preview-bytes", "skills.preview_bytes")
	cfg.CatalogLimit = configutil.FlagOrViperInt(cmd, "skills-catalog-limit", "skills.catalog_limit")
	cfg.SelectTimeout = configutil.FlagOrViperDuration(cmd, "llm-request-timeout", "llm.request_timeout")

	if strings.TrimSpace(cfg.Mode) == "" {
		cfg.Mode = "smart"
	}
	if strings.TrimSpace(cfg.SelectorModel) == "" {
		cfg.SelectorModel = model
	}

	return cfg
}

func PromptSpecWithSkills(ctx context.Context, log *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, cfg SkillsConfig) (agent.PromptSpec, []string, []string, error) {
	if log == nil {
		log = slog.Default()
	}
	spec := agent.DefaultPromptSpec()
	var loadedOrdered []string
	declaredAuthProfiles := make(map[string]bool)

	discovered, err := skills.Discover(skills.DiscoverOptions{Roots: cfg.Roots})
	if err != nil {
		if cfg.Trace {
			log.Warn("skills_discover_warning", "error", err.Error())
		}
	}

	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = "smart"
	}
	switch mode {
	case "off", "none", "disabled":
		return spec, nil, nil, nil
	}

	loadedSkillIDs := make(map[string]bool)

	requested := append([]string{}, cfg.Requested...)

	if cfg.Auto {
		requested = append(requested, skills.ReferencedSkillNames(task)...)
	}

	uniq := make(map[string]bool, len(requested))
	var finalReq []string
	for _, r := range requested {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		k := strings.ToLower(r)
		if uniq[k] {
			continue
		}
		uniq[k] = true
		finalReq = append(finalReq, r)
	}

	if len(finalReq) == 0 {
		// continue: smart mode can still auto-select
	}

	// Explicit load: strict (user/config requested)
	for _, q := range finalReq {
		s, err := skills.Resolve(discovered, q)
		if err != nil {
			return agent.PromptSpec{}, nil, nil, err
		}
		if loadedSkillIDs[strings.ToLower(s.ID)] {
			continue
		}
		skillLoaded, err := skills.Load(s, 512*1024)
		if err != nil {
			return agent.PromptSpec{}, nil, nil, err
		}
		for _, ap := range skillLoaded.AuthProfiles {
			declaredAuthProfiles[ap] = true
		}
		loadedSkillIDs[strings.ToLower(skillLoaded.ID)] = true
		loadedOrdered = append(loadedOrdered, skillLoaded.ID)
		spec.Blocks = append(spec.Blocks, agent.PromptBlock{
			Title:   fmt.Sprintf("%s (%s)", skillLoaded.Name, skillLoaded.ID),
			Content: skillLoaded.Contents,
		})

		log.Info("skill_loaded", "mode", mode, "name", skillLoaded.Name, "id", skillLoaded.ID, "path", skillLoaded.SkillMD, "bytes", len(skillLoaded.Contents))
		if logOpts.IncludeSkillContents {
			log.Debug("skill_contents", "id", skillLoaded.ID, "content", truncateString(skillLoaded.Contents, logOpts.MaxSkillContentChars))
		}
	}

	if mode == "explicit" {
		ap := mapKeysSorted(declaredAuthProfiles)
		if len(ap) > 0 {
			spec.Blocks = append(spec.Blocks, agent.PromptBlock{
				Title: "Auth Profiles (declared by loaded skills)",
				Content: "Declared auth_profile ids:\n- " + strings.Join(ap, "\n- ") + "\n\n" +
					"Rules:\n" +
					"- Never ask the user to paste API keys/tokens.\n" +
					"- For authenticated HTTP APIs, call url_fetch with auth_profile set to one of the declared ids.\n" +
					"- If a secret is missing, instruct the user to set the required environment variable(s) and restart the service (do not request the secret value in chat).\n" +
					"- If the user wants a PDF in Telegram, use url_fetch.download_path to save it under file_cache_dir, then telegram_send_file.",
			})
		}
		if len(ap) > 0 {
			log.Info("skills_auth_profiles_declared", "count", len(ap), "profiles", ap)
		}
		log.Info("skills_loaded", "mode", mode, "count", len(spec.Blocks))
		return spec, loadedOrdered, mapKeysSorted(declaredAuthProfiles), nil
	}

	// Smart selection: non-strict (model may suggest none or unknown ids)
	maxLoad := cfg.MaxLoad
	previewBytes := cfg.PreviewBytes
	catalogLimit := cfg.CatalogLimit
	selectTimeout := cfg.SelectTimeout
	selectorModel := strings.TrimSpace(cfg.SelectorModel)

	log.Info("skills_select_start",
		"mode", mode,
		"model", selectorModel,
		"max_load", maxLoad,
		"preview_bytes", previewBytes,
		"catalog_limit", catalogLimit,
		"timeout", selectTimeout.String(),
	)

	if selectTimeout <= 0 {
		selectTimeout = 10 * time.Second
	}
	selectCtx := ctx
	cancel := func() {}
	if selectTimeout > 0 {
		selectCtx, cancel = context.WithTimeout(ctx, selectTimeout)
	}
	defer cancel()

	selection, err := skills.Select(selectCtx, client, task, discovered, skills.SelectOptions{
		Model:        selectorModel,
		MaxLoad:      maxLoad,
		PreviewBytes: previewBytes,
		CatalogLimit: catalogLimit,
	})
	if err != nil {
		log.Warn("skills_select_error", "error", err.Error())
	}
	if err == nil {
		log.Info("skills_selected", "mode", mode, "selected", selection.SkillsToLoad)
		if logOpts.IncludeThoughts {
			log.Info("skills_selected_reasoning", "reasoning", truncateString(selection.Reasoning, logOpts.MaxThoughtChars))
		}
		if logOpts.IncludeThoughts {
			log.Debug("skills_selected_reasoning", "reasoning", truncateString(selection.Reasoning, logOpts.MaxThoughtChars))
		}
	}

	if err == nil && len(selection.SkillsToLoad) > 0 {
		for _, q := range selection.SkillsToLoad {
			s, err := skills.Resolve(discovered, q)
			if err != nil {
				continue
			}
			if loadedSkillIDs[strings.ToLower(s.ID)] {
				continue
			}
			skillLoaded, err := skills.Load(s, 512*1024)
			if err != nil {
				continue
			}
			for _, ap := range skillLoaded.AuthProfiles {
				declaredAuthProfiles[ap] = true
			}
			loadedSkillIDs[strings.ToLower(skillLoaded.ID)] = true
			loadedOrdered = append(loadedOrdered, skillLoaded.ID)
			spec.Blocks = append(spec.Blocks, agent.PromptBlock{
				Title:   fmt.Sprintf("%s (%s)", skillLoaded.Name, skillLoaded.ID),
				Content: skillLoaded.Contents,
			})
			log.Info("skill_loaded", "mode", mode, "name", skillLoaded.Name, "id", skillLoaded.ID, "path", skillLoaded.SkillMD, "bytes", len(skillLoaded.Contents))
			if logOpts.IncludeSkillContents {
				log.Debug("skill_contents", "id", skillLoaded.ID, "content", truncateString(skillLoaded.Contents, logOpts.MaxSkillContentChars))
			}
		}
	}

	ap := mapKeysSorted(declaredAuthProfiles)
	if len(ap) > 0 {
		spec.Blocks = append(spec.Blocks, agent.PromptBlock{
			Title: "Auth Profiles (declared by loaded skills)",
			Content: "Declared auth_profile ids:\n- " + strings.Join(ap, "\n- ") + "\n\n" +
				"Rules:\n" +
				"- Never ask the user to paste API keys/tokens.\n" +
				"- For authenticated HTTP APIs, call url_fetch with auth_profile set to one of the declared ids.\n" +
				"- If a secret is missing, instruct the user to set the required environment variable(s) and restart the service (do not request the secret value in chat).\n" +
				"- If the user wants a PDF in Telegram, use url_fetch.download_path to save it under file_cache_dir, then telegram_send_file.",
		})
	}
	if len(ap) > 0 {
		log.Info("skills_auth_profiles_declared", "count", len(ap), "profiles", ap)
	}
	log.Info("skills_loaded", "mode", mode, "count", len(spec.Blocks))
	return spec, loadedOrdered, ap, nil
}

func skillsAutoFromViper() bool {
	if viper.IsSet("skills.auto") {
		return viper.GetBool("skills.auto")
	}
	if viper.IsSet("skills_auto") {
		return viper.GetBool("skills_auto")
	}
	return true
}

func getStringSlice(keys ...string) []string {
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if viper.IsSet(key) {
			return viper.GetStringSlice(key)
		}
	}
	return nil
}

func truncateString(s string, max int) string {
	if max <= 0 {
		return s
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func mapKeysSorted(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
