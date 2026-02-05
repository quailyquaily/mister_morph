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

	"github.com/quailyquaily/mistermorph/agent"
	telegramcmd "github.com/quailyquaily/mistermorph/cmd/telegram"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
	uniaiProvider "github.com/quailyquaily/mistermorph/providers/uniai"
	"github.com/quailyquaily/mistermorph/skills"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run an agent task",
		RunE: func(cmd *cobra.Command, args []string) error {
			isHeartbeat := flagOrViperBool(cmd, "heartbeat", "")
			task := ""
			var runMeta map[string]any
			if isHeartbeat {
				hbChecklist := strings.TrimSpace(viper.GetString("heartbeat.checklist_path"))
				var hbSnapshot string
				if viper.GetBool("memory.enabled") {
					hbMgr := memory.NewManager(viper.GetString("memory.dir"), viper.GetInt("memory.short_term_days"))
					snap, err := buildHeartbeatProgressSnapshot(hbMgr, viper.GetInt("memory.injection.max_items"))
					if err != nil {
						return err
					}
					hbSnapshot = snap
				}
				hbTask, checklistEmpty, err := buildHeartbeatTask(hbChecklist, hbSnapshot)
				if err != nil {
					return err
				}
				task = hbTask
				runMeta = map[string]any{
					"trigger": "heartbeat",
					"heartbeat": buildHeartbeatMeta(
						"cli",
						viper.GetDuration("heartbeat.interval"),
						hbChecklist,
						checklistEmpty,
						nil,
						nil,
					),
				}
			} else {
				task = strings.TrimSpace(flagOrViperString(cmd, "task", "task"))
				if task == "" {
					data, err := os.ReadFile("/dev/stdin")
					if err == nil {
						task = strings.TrimSpace(string(data))
					}
				}
				if task == "" {
					return fmt.Errorf("missing --task (or stdin)")
				}
			}

			provider := llmProviderFromViper()
			if cmd.Flags().Changed("provider") {
				provider = strings.TrimSpace(flagOrViperString(cmd, "provider", ""))
			}
			endpoint := llmEndpointForProvider(provider)
			if cmd.Flags().Changed("endpoint") {
				endpoint = strings.TrimSpace(flagOrViperString(cmd, "endpoint", ""))
			}
			apiKey := llmAPIKeyForProvider(provider)
			if cmd.Flags().Changed("api-key") {
				apiKey = strings.TrimSpace(flagOrViperString(cmd, "api-key", ""))
			}
			model := llmModelForProvider(provider)
			if cmd.Flags().Changed("model") {
				model = strings.TrimSpace(flagOrViperString(cmd, "model", ""))
			}

			client, err := llmClientFromConfig(llmClientConfig{
				Provider:       provider,
				Endpoint:       endpoint,
				APIKey:         apiKey,
				Model:          model,
				RequestTimeout: flagOrViperDuration(cmd, "llm-request-timeout", "llm.request_timeout"),
			})
			if err != nil {
				return err
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

			if flagOrViperBool(cmd, "inspect-request", "") {
				inspector, err := newRequestInspector(task)
				if err != nil {
					return err
				}
				defer func() { _ = inspector.Close() }()
				setter, ok := client.(interface {
					SetDebugFn(func(label, payload string))
				})
				if !ok {
					return fmt.Errorf("inspect-request requires uniai provider client")
				}
				setter.SetDebugFn(inspector.Dump)
			}

			if flagOrViperBool(cmd, "inspect-prompt", "") {
				inspector, err := newPromptInspector(task)
				if err != nil {
					return err
				}
				defer func() { _ = inspector.Close() }()
				client = &inspectClient{base: client, inspector: inspector}
			}

			promptSpec, _, skillAuthProfiles, err := promptSpecWithSkills(ctx, logger, logOpts, task, client, model, skillsConfigFromRunCmd(cmd, model))
			if err != nil {
				return err
			}

			var memManager *memory.Manager
			var memIdentity memory.Identity
			if viper.GetBool("memory.enabled") {
				id := resolveRunMemoryIdentity()
				if id.Enabled && strings.TrimSpace(id.SubjectID) != "" {
					memIdentity = id
					memManager = memory.NewManager(viper.GetString("memory.dir"), viper.GetInt("memory.short_term_days"))
					if viper.GetBool("memory.injection.enabled") {
						maxItems := viper.GetInt("memory.injection.max_items")
						snap, err := memManager.BuildInjection(id.SubjectID, memory.ContextPrivate, maxItems)
						if err != nil {
							return fmt.Errorf("memory injection: %w", err)
						}
						if strings.TrimSpace(snap) != "" {
							promptSpec.Blocks = append(promptSpec.Blocks, agent.PromptBlock{
								Title:   "Memory Summaries",
								Content: snap,
							})
						}
					}
				}
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

			reg := registryFromViper()
			registerPlanTool(reg, client, model)

			engine := agent.New(
				client,
				reg,
				agent.Config{
					MaxSteps:         flagOrViperInt(cmd, "max-steps", "max_steps"),
					ParseRetries:     flagOrViperInt(cmd, "parse-retries", "parse_retries"),
					MaxTokenBudget:   flagOrViperInt(cmd, "max-token-budget", "max_token_budget"),
					IntentEnabled:    viper.GetBool("intent.enabled"),
					IntentTimeout:    viper.GetDuration("intent.timeout"),
					IntentMaxHistory: viper.GetInt("intent.max_history"),
				},
				promptSpec,
				opts...,
			)

			final, runCtx, err := engine.Run(ctx, task, agent.RunOptions{Model: model, Meta: runMeta})
			if err != nil {
				if errors.Is(err, errAbortedByUser) {
					return nil
				}
				return err
			}

			if !isHeartbeat && memManager != nil && memIdentity.Enabled && strings.TrimSpace(memIdentity.SubjectID) != "" {
				if err := updateRunMemory(ctx, logger, client, model, memManager, memIdentity, task, final); err != nil {
					logger.Warn("memory_update_error", "error", err.Error())
				}
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
	cmd.Flags().Bool("heartbeat", false, "Run a single heartbeat check (ignores --task and stdin).")
	cmd.Flags().String("provider", "openai", "Provider: openai|openai_custom|deepseek|xai|gemini|azure|anthropic|bedrock|susanoo.")
	cmd.Flags().String("endpoint", "https://api.openai.com", "Base URL for provider.")
	cmd.Flags().String("model", "gpt-4o-mini", "Model name.")
	cmd.Flags().String("api-key", "", "API key.")
	cmd.Flags().Duration("llm-request-timeout", 90*time.Second, "Per-LLM HTTP request timeout (0 uses provider default).")
	cmd.Flags().Bool("interactive", false, "Ctrl-C pauses and lets you inject extra context, then continues.")
	cmd.Flags().Bool("inspect-prompt", false, "Dump prompts (messages) to ./dump/prompt_YYYYMMDD_HHmm.md.")
	cmd.Flags().Bool("inspect-request", false, "Dump LLM request/response payloads to ./dump/request_YYYYMMDD_HHmm.md.")
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

func resolveRunMemoryIdentity() memory.Identity {
	return memory.Identity{
		Enabled:     true,
		ExternalKey: "CLI_USER",
		SubjectID:   "CLI_USER",
	}
}

func updateRunMemory(ctx context.Context, logger *slog.Logger, client llm.Client, model string, mgr *memory.Manager, id memory.Identity, task string, final *agent.Final) error {
	if mgr == nil || client == nil {
		return nil
	}
	output := formatFinalOutput(final)
	meta := memory.WriteMeta{
		SessionID: "cli",
		Source:    "cli",
		Channel:   "local",
		SubjectID: id.SubjectID,
	}
	date := time.Now().UTC()
	_, existingContent, hasExisting, err := mgr.LoadShortTerm(date, meta.SessionID)
	if err != nil {
		return err
	}

	memCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	ctxInfo := telegramcmd.MemoryDraftContext{
		SessionID:        meta.SessionID,
		ChatType:         "cli",
		CounterpartyName: strings.TrimSpace(os.Getenv("USER")),
		TimestampUTC:     date.Format(time.RFC3339),
	}
	if ctxInfo.CounterpartyName == "" {
		ctxInfo.CounterpartyName = strings.TrimSpace(os.Getenv("USERNAME"))
	}

	draft, err := telegramcmd.BuildMemoryDraft(memCtx, client, model, nil, task, output, existingContent, ctxInfo)
	if err != nil {
		return err
	}
	draft.Promote = telegramcmd.EnforceLongTermPromotionRules(draft.Promote, nil, task)

	summary := strings.TrimSpace(draft.Summary)
	var mergedContent memory.ShortTermContent
	if hasExisting && telegramcmd.HasDraftContent(draft) {
		semantic, semanticSummary, mergeErr := telegramcmd.SemanticMergeShortTerm(memCtx, client, model, existingContent, draft)
		if mergeErr == nil {
			mergedContent = semantic
			summary = semanticSummary
		} else {
			mergedContent = memory.MergeShortTerm(existingContent, draft)
		}
	} else {
		mergedContent = memory.MergeShortTerm(existingContent, draft)
	}

	if _, err := mgr.WriteShortTerm(date, mergedContent, summary, meta); err != nil {
		return err
	}

	updates := append([]memory.TaskItem{}, mergedContent.Tasks...)
	updates = append(updates, mergedContent.FollowUps...)
	if updated, err := mgr.UpdateRecentTaskStatuses(updates, meta.SessionID); err != nil {
		if logger != nil {
			logger.Warn("memory_task_sync_error", "error", err.Error())
		}
	} else if updated > 0 {
		if logger != nil {
			logger.Debug("memory_task_sync_ok", "updated", updated)
		}
	}

	if _, err := mgr.UpdateLongTerm(id.SubjectID, draft.Promote); err != nil {
		return err
	}
	if logger != nil {
		logger.Debug("memory_update_ok", "subject_id", id.SubjectID)
	}
	return nil
}

type llmClientConfig struct {
	Provider       string
	Endpoint       string
	APIKey         string
	Model          string
	RequestTimeout time.Duration
}

func llmClientFromConfig(cfg llmClientConfig) (llm.Client, error) {
	toolsEmulationMode, err := toolsEmulationModeFromViper()
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "openai", "openai_custom", "deepseek", "xai", "gemini", "azure", "anthropic", "bedrock", "susanoo":
		c := uniaiProvider.New(uniaiProvider.Config{
			Provider:           strings.ToLower(strings.TrimSpace(cfg.Provider)),
			Endpoint:           strings.TrimSpace(cfg.Endpoint),
			APIKey:             strings.TrimSpace(cfg.APIKey),
			Model:              strings.TrimSpace(cfg.Model),
			RequestTimeout:     cfg.RequestTimeout,
			ToolsEmulationMode: toolsEmulationMode,
			AzureAPIKey:        firstNonEmpty(viper.GetString("llm.azure.api_key"), viper.GetString("llm.api_key")),
			AzureEndpoint:      firstNonEmpty(viper.GetString("llm.azure.endpoint"), viper.GetString("llm.endpoint")),
			AzureDeployment:    firstNonEmpty(viper.GetString("llm.azure.deployment"), viper.GetString("llm.model")),
			AwsKey:             firstNonEmpty(viper.GetString("llm.bedrock.aws_key"), viper.GetString("llm.aws.key")),
			AwsSecret:          firstNonEmpty(viper.GetString("llm.bedrock.aws_secret"), viper.GetString("llm.aws.secret")),
			AwsRegion:          firstNonEmpty(viper.GetString("llm.bedrock.region"), viper.GetString("llm.aws.region")),
			AwsBedrockModelArn: firstNonEmpty(viper.GetString("llm.bedrock.model_arn"), viper.GetString("llm.aws.bedrock_model_arn")),
		})
		return c, nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider)
	}
}

func toolsEmulationModeFromViper() (string, error) {
	mode := strings.ToLower(strings.TrimSpace(viper.GetString("llm.tools_emulation_mode")))
	if mode == "" {
		return "off", nil
	}
	switch mode {
	case "off", "fallback", "force":
		return mode, nil
	default:
		return "", fmt.Errorf("invalid llm.tools_emulation_mode %q (expected off|fallback|force)", mode)
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
