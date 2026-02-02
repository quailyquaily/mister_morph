package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mister_morph/agent"
	"github.com/quailyquaily/mister_morph/llm"
	"github.com/quailyquaily/mister_morph/providers/openai"
	"github.com/quailyquaily/mister_morph/skills"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run an agent task",
		RunE: func(cmd *cobra.Command, args []string) error {
			task := strings.TrimSpace(flagOrViperString(cmd, "task", "task"))
			if task == "" {
				data, err := os.ReadFile("/dev/stdin")
				if err == nil {
					task = strings.TrimSpace(string(data))
				}
			}
			if task == "" {
				return fmt.Errorf("missing --task (or stdin)")
			}

			provider := llmProviderFromViper()
			if cmd.Flags().Changed("provider") {
				provider = strings.TrimSpace(flagOrViperString(cmd, "provider", ""))
			}
			endpoint := llmEndpointFromViper()
			if cmd.Flags().Changed("endpoint") {
				endpoint = strings.TrimSpace(flagOrViperString(cmd, "endpoint", ""))
			}
			apiKey := llmAPIKeyFromViper()
			if cmd.Flags().Changed("api-key") {
				apiKey = strings.TrimSpace(flagOrViperString(cmd, "api-key", ""))
			}
			client, err := llmClientFromConfig(llmClientConfig{
				Provider:       provider,
				Endpoint:       endpoint,
				APIKey:         apiKey,
				RequestTimeout: flagOrViperDuration(cmd, "llm-request-timeout", "llm.request_timeout"),
			})
			if err != nil {
				return err
			}

			model := llmModelFromViper()
			if cmd.Flags().Changed("model") {
				model = strings.TrimSpace(flagOrViperString(cmd, "model", ""))
			}

			timeout := flagOrViperDuration(cmd, "timeout", "timeout")
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			logger, err := loggerFromViper()
			if err != nil {
				return err
			}
			slog.SetDefault(logger)

			logOpts := logOptionsFromViper()

			promptSpec, _, skillAuthProfiles, err := promptSpecWithSkills(ctx, logger, logOpts, task, client, model, skillsConfigFromRunCmd(cmd, model))
			if err != nil {
				return err
			}

			var hook agent.Hook
			if flagOrViperBool(cmd, "interactive", "interactive") {
				hook, err = newInteractiveHook()
				if err != nil {
					return err
				}
			}

			var opts []agent.Option
			if hook != nil {
				opts = append(opts, agent.WithHook(hook))
			}
			opts = append(opts, agent.WithLogger(logger))
			opts = append(opts, agent.WithLogOptions(logOpts))
			opts = append(opts, agent.WithSkillAuthProfiles(skillAuthProfiles, viper.GetBool("secrets.require_skill_profiles")))
			if g := guardFromViper(logger); g != nil {
				opts = append(opts, agent.WithGuard(g))
			}

			engine := agent.New(
				client,
				registryFromViper(),
				agent.Config{
					MaxSteps:       flagOrViperInt(cmd, "max-steps", "max_steps"),
					ParseRetries:   flagOrViperInt(cmd, "parse-retries", "parse_retries"),
					MaxTokenBudget: flagOrViperInt(cmd, "max-token-budget", "max_token_budget"),
					PlanMode:       strings.TrimSpace(flagOrViperString(cmd, "plan-mode", "plan.mode")),
				},
				promptSpec,
				opts...,
			)

			final, runCtx, err := engine.Run(ctx, task, agent.RunOptions{Model: model})
			if err != nil {
				if errors.Is(err, errAbortedByUser) {
					return nil
				}
				return err
			}

			logger.Info("run_done",
				"steps", len(runCtx.Steps),
				"llm_rounds", runCtx.Metrics.LLMRounds,
				"total_tokens", runCtx.Metrics.TotalTokens,
				"parse_retries", runCtx.Metrics.ParseRetries,
			)

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(final)
		},
	}

	cmd.Flags().String("task", "", "Task to run (if empty, reads from stdin).")
	cmd.Flags().String("provider", "openai", "Provider: openai.")
	cmd.Flags().String("endpoint", "https://api.openai.com", "Base URL for provider.")
	cmd.Flags().String("model", "gpt-4o-mini", "Model name.")
	cmd.Flags().String("api-key", "", "API key.")
	cmd.Flags().Duration("llm-request-timeout", 90*time.Second, "Per-LLM HTTP request timeout (0 uses provider default).")
	cmd.Flags().Bool("interactive", false, "Ctrl-C pauses and lets you inject extra context, then continues.")
	cmd.Flags().StringArray("skills-dir", nil, "Skills root directory (repeatable). Defaults: ~/.codex/skills, ~/.claude/skills")
	cmd.Flags().StringArray("skill", nil, "Skill(s) to load by name or id (repeatable).")
	cmd.Flags().Bool("skills-auto", true, "Auto-load skills referenced in task via $SkillName.")
	cmd.Flags().String("skills-mode", "smart", "Skills mode: off|explicit|smart.")
	cmd.Flags().Int("skills-max-load", 3, "Max skills to load in smart mode.")
	cmd.Flags().Int64("skills-preview-bytes", 2048, "Bytes to preview per skill during smart selection.")
	cmd.Flags().Int("skills-catalog-limit", 200, "Max number of discovered skills to include in smart selection catalog.")
	cmd.Flags().Duration("skills-select-timeout", 10*time.Second, "Timeout for smart skills selection call.")

	cmd.Flags().Int("max-steps", 15, "Max tool-call steps.")
	cmd.Flags().Int("parse-retries", 2, "Max JSON parse retries.")
	cmd.Flags().Int("max-token-budget", 0, "Max cumulative token budget (0 disables).")
	cmd.Flags().String("plan-mode", "auto", "Planning mode: off|auto|always (auto enables planning for complex tasks).")

	cmd.Flags().Duration("timeout", 10*time.Minute, "Overall timeout.")

	return cmd
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

type llmClientConfig struct {
	Provider       string
	Endpoint       string
	APIKey         string
	RequestTimeout time.Duration
}

func llmClientFromConfig(cfg llmClientConfig) (llm.Client, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "openai":
		c := openai.New(strings.TrimSpace(cfg.Endpoint), strings.TrimSpace(cfg.APIKey))
		if cfg.RequestTimeout > 0 && c.HTTP != nil {
			c.HTTP.Timeout = cfg.RequestTimeout
		}
		return c, nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider)
	}
}

var errAbortedByUser = errors.New("aborted by user")

func promptSpecWithSkills(ctx context.Context, log *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, cfg skillsConfig) (agent.PromptSpec, []string, []string, error) {
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
	selectCtx, cancel := context.WithTimeout(ctx, selectTimeout)
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

func newInteractiveHook() (agent.Hook, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("interactive mode requires /dev/tty: %w", err)
	}

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)

	r := bufio.NewReader(tty)

	return func(ctx context.Context, step int, agentCtx *agent.Context, messages *[]llm.Message) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-interrupts:
			_, _ = fmt.Fprintf(os.Stderr, "\n[interactive] paused at step=%d. Enter extra context (end with empty line).\n", step)
			_, _ = fmt.Fprintln(os.Stderr, "[interactive] commands: /continue (no-op), /abort (stop run)")
			note, err := readMultiline(r)
			if err != nil {
				return err
			}
			note = strings.TrimSpace(note)
			switch note {
			case "", "/continue":
				return nil
			case "/abort":
				return errAbortedByUser
			default:
				*messages = append(*messages, llm.Message{
					Role:    "user",
					Content: "Operator context:\n" + note,
				})
				_, _ = fmt.Fprintln(os.Stderr, "[interactive] context injected; continuing.")
				return nil
			}
		default:
			return nil
		}
	}, nil
}

func readMultiline(r *bufio.Reader) (string, error) {
	var lines []string
	for {
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" {
			break
		}
		lines = append(lines, line)
		if err != nil {
			break
		}
	}
	return strings.Join(lines, "\n"), nil
}
