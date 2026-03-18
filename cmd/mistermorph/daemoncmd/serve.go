package daemoncmd

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	heartbeatruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/heartbeat"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/heartbeatutil"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type ServeDependencies struct {
	RegistryFromViper func() *tools.Registry
	GuardFromViper    func(*slog.Logger) *guard.Guard
}

func NewServeCmd(deps ServeDependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:        "serve",
		Short:      "Run as a local daemon that accepts tasks over HTTP",
		Deprecated: "use channel-specific runtimes or explicit runtime endpoints instead; standalone daemon mode is deprecated",
		RunE: func(cmd *cobra.Command, args []string) error {
			listen := strings.TrimSpace(configutil.FlagOrViperString(cmd, "server-listen", "server.listen"))
			if listen == "" {
				listen = "127.0.0.1:8787"
			}
			auth := configutil.FlagOrViperString(cmd, "server-auth-token", "server.auth_token")
			if strings.TrimSpace(auth) == "" {
				return fmt.Errorf("missing server.auth_token (set via --server-auth-token or MISTER_MORPH_SERVER_AUTH_TOKEN)")
			}

			maxQueue := configutil.FlagOrViperInt(cmd, "server-max-queue", "server.max_queue")
			taskView, err := daemonruntime.NewTaskViewForTarget("serve", maxQueue)
			if err != nil {
				return err
			}
			store := NewTaskStore(maxQueue, taskView)

			logger, err := logutil.LoggerFromViper()
			if err != nil {
				return err
			}
			slog.SetDefault(logger)

			llmValues := llmutil.RuntimeValuesFromViper()
			mainRoute, err := llmutil.ResolveRoute(llmValues, llmutil.RoutePurposeMainLoop)
			if err != nil {
				return err
			}
			baseClient, err := llmutil.ClientFromConfigWithValues(mainRoute.ClientConfig, mainRoute.Values)
			if err != nil {
				return err
			}
			client := llmstats.WrapRuntimeClient(
				baseClient,
				mainRoute.ClientConfig.Provider,
				mainRoute.ClientConfig.Endpoint,
				mainRoute.ClientConfig.Model,
				logger,
			)
			var requestInspector *llminspect.RequestInspector
			if configutil.FlagOrViperBool(cmd, "inspect-request", "") {
				requestInspector, err = llminspect.NewRequestInspector(llminspect.Options{
					Mode:            "daemon",
					Task:            "serve",
					TimestampFormat: "20060102_150405",
				})
				if err != nil {
					return err
				}
				defer func() { _ = requestInspector.Close() }()
			}
			var promptInspector *llminspect.PromptInspector
			if configutil.FlagOrViperBool(cmd, "inspect-prompt", "") {
				promptInspector, err = llminspect.NewPromptInspector(llminspect.Options{
					Mode:            "daemon",
					Task:            "serve",
					TimestampFormat: "20060102_150405",
				})
				if err != nil {
					return err
				}
				defer func() { _ = promptInspector.Close() }()
			}
			client = llminspect.WrapClient(client, llminspect.ClientOptions{
				PromptInspector:  promptInspector,
				RequestInspector: requestInspector,
				APIBase:          mainRoute.ClientConfig.Endpoint,
				Model:            mainRoute.ClientConfig.Model,
			})
			mainModel := strings.TrimSpace(mainRoute.ClientConfig.Model)
			mainProvider := strings.TrimSpace(mainRoute.ClientConfig.Provider)
			var reg *tools.Registry
			if deps.RegistryFromViper != nil {
				reg = deps.RegistryFromViper()
			}
			if reg == nil {
				reg = tools.NewRegistry()
			}
			planClient := client
			planModel := mainModel
			planRoute, err := llmutil.ResolveRoute(llmValues, llmutil.RoutePurposePlanCreate)
			if err != nil {
				return err
			}
			if !planRoute.SameProfile(mainRoute) {
				planBaseClient, err := llmutil.ClientFromConfigWithValues(planRoute.ClientConfig, planRoute.Values)
				if err != nil {
					return err
				}
				planClient = llmstats.WrapRuntimeClient(planBaseClient, planRoute.ClientConfig.Provider, planRoute.ClientConfig.Endpoint, planRoute.ClientConfig.Model, logger)
				planClient = llminspect.WrapClient(planClient, llminspect.ClientOptions{
					PromptInspector:  promptInspector,
					RequestInspector: requestInspector,
					APIBase:          planRoute.ClientConfig.Endpoint,
					Model:            planRoute.ClientConfig.Model,
				})
			}
			planModel = strings.TrimSpace(planRoute.ClientConfig.Model)
			toolsutil.RegisterRuntimeTools(reg, toolsutil.LoadRuntimeToolsRegisterConfigFromViper(), toolsutil.RuntimeToolLLMOptions{
				DefaultClient:    client,
				DefaultModel:     strings.TrimSpace(mainRoute.ClientConfig.Model),
				PlanCreateClient: planClient,
				PlanCreateModel:  planModel,
			})

			logOpts := logutil.LogOptionsFromViper()
			skillsCfg := skillsutil.SkillsConfigFromViper()

			baseCfg := agent.Config{
				MaxSteps:        viper.GetInt("max_steps"),
				ParseRetries:    viper.GetInt("parse_retries"),
				MaxTokenBudget:  viper.GetInt("max_token_budget"),
				ToolRepeatLimit: viper.GetInt("tool_repeat_limit"),
			}

			var sharedGuard *guard.Guard
			if deps.GuardFromViper != nil {
				sharedGuard = deps.GuardFromViper(logger)
			}
			hbState := &heartbeatutil.State{}

			// Worker: process tasks sequentially.
			go func() {
				for {
					qt := store.Next()
					if qt == nil {
						continue
					}
					id := qt.taskID
					resumeApprovalID := strings.TrimSpace(qt.resumeApprovalID)
					started := time.Now()
					store.Update(id, func(info *daemonruntime.TaskInfo) {
						info.Status = daemonruntime.TaskRunning
						info.PendingAt = nil
						if resumeApprovalID != "" {
							info.ResumedAt = &started
						} else if info.StartedAt == nil {
							info.StartedAt = &started
						}
					})

					var (
						final  *agent.Final
						runCtx *agent.Context
						runErr error
					)

					if resumeApprovalID != "" {
						qt.resumeApprovalID = ""
						final, runCtx, runErr = resumeOneTask(qt.ctx, logger, logOpts, client, reg, baseCfg, sharedGuard, resumeApprovalID)
					} else {
						final, runCtx, runErr = runOneTask(qt.ctx, logger, logOpts, client, reg, baseCfg, sharedGuard, qt.task, qt.model, qt.meta, skillsCfg)
					}

					if pendingID, ok := pendingApprovalID(final); ok && runErr == nil {
						qt.approvalID = pendingID
						if qt.isHeartbeat && qt.heartbeatState != nil {
							alert, msg := qt.heartbeatState.EndFailure(fmt.Errorf("heartbeat pending approval"))
							if alert {
								logger.Warn("heartbeat_alert", "message", msg)
							}
						}
						pendingAt := time.Now()
						store.Update(id, func(info *daemonruntime.TaskInfo) {
							info.Status = daemonruntime.TaskPending
							info.PendingAt = &pendingAt
							info.ApprovalRequestID = pendingID
							info.Result = map[string]any{
								"final":   final,
								"metrics": runCtx.Metrics,
								"steps":   summarizeSteps(runCtx),
							}
						})
						// Don't cancel: task remains resumable until approval timeout or task timeout.
						continue
					}
					qt.approvalID = ""

					finished := time.Now()
					displayErr := strings.TrimSpace(outputfmt.FormatErrorForDisplay(runErr))
					if displayErr == "" && runErr != nil {
						displayErr = strings.TrimSpace(runErr.Error())
					}
					store.Update(id, func(info *daemonruntime.TaskInfo) {
						info.FinishedAt = &finished
						if runErr != nil {
							if errorsIsContextDeadline(qt.ctx, runErr) {
								info.Status = daemonruntime.TaskCanceled
							} else {
								info.Status = daemonruntime.TaskFailed
							}
							info.Error = displayErr
							return
						}
						info.Status = daemonruntime.TaskDone
						info.Result = map[string]any{
							"final":   final,
							"metrics": runCtx.Metrics,
							"steps":   summarizeSteps(runCtx),
						}
					})
					if qt.isHeartbeat && qt.heartbeatState != nil {
						if runErr != nil {
							alert, msg := qt.heartbeatState.EndFailure(errors.New(displayErr))
							if alert {
								logger.Warn("heartbeat_alert", "message", msg)
							} else {
								logger.Warn("heartbeat_error", "error", displayErr)
							}
						} else {
							qt.heartbeatState.EndSuccess(finished)
							out := outputfmt.FormatFinalOutput(final)
							if strings.TrimSpace(out) != "" {
								logger.Info("heartbeat_summary", "message", out)
							} else {
								logger.Info("heartbeat_summary", "message", "empty")
							}
						}
					}
					qt.cancel()
				}
			}()

			hbEnabled := viper.GetBool("heartbeat.enabled")
			hbInterval := viper.GetDuration("heartbeat.interval")
			hbChecklist := statepaths.HeartbeatChecklistPath()
			var runHeartbeatTick func(input daemonruntime.PokeInput) heartbeatutil.TickResult
			if hbEnabled && hbInterval > 0 {
				runHeartbeatTick = func(input daemonruntime.PokeInput) heartbeatutil.TickResult {
					extra := map[string]any{
						"queue_len": store.QueueLen(),
					}
					if pokeMeta := input.MetaValue(); pokeMeta != nil {
						extra["poke"] = pokeMeta
					}
					result := heartbeatutil.Tick(
						hbState,
						func() (string, bool, error) {
							return heartbeatutil.BuildHeartbeatTask(hbChecklist)
						},
						func(task string, checklistEmpty bool) string {
							meta := heartbeatutil.BuildHeartbeatMeta("daemon", hbInterval, hbChecklist, checklistEmpty, hbState, extra)
							timeout := viper.GetDuration("timeout")
							if _, err := store.EnqueueHeartbeat(context.Background(), task, mainModel, timeout, meta, hbState); err != nil {
								return err.Error()
							}
							return ""
						},
					)
					switch result.Outcome {
					case heartbeatutil.TickBuildError:
						if strings.TrimSpace(result.AlertMessage) != "" {
							logger.Warn("heartbeat_alert", "message", result.AlertMessage)
						} else {
							logger.Warn("heartbeat_task_error", "error", result.BuildError.Error())
						}
					case heartbeatutil.TickSkipped:
						logger.Debug("heartbeat_skip", "reason", result.SkipReason)
					}
					return result
				}
				go func() {
					initialTimer := time.NewTimer(15 * time.Second)
					defer initialTimer.Stop()
					select {
					case <-cmd.Context().Done():
						return
					case <-initialTimer.C:
					}
					runHeartbeatTick(daemonruntime.PokeInput{})

					ticker := time.NewTicker(hbInterval)
					defer ticker.Stop()
					for {
						select {
						case <-cmd.Context().Done():
							return
						case <-ticker.C:
							_ = runHeartbeatTick(daemonruntime.PokeInput{})
						}
					}
				}()
			}

			mux := http.NewServeMux()
			daemonruntime.RegisterRoutes(mux, daemonruntime.RoutesOptions{
				Mode:          "serve",
				AgentNameFunc: func() string { return personautil.LoadAgentName(statepaths.FileStateDir()) },
				AuthToken:     auth,
				TaskReader:    store,
				Overview: func(ctx context.Context) (map[string]any, error) {
					return map[string]any{
						"llm": map[string]any{
							"provider": mainProvider,
							"model":    mainModel,
						},
						"channel": map[string]any{
							"configured":          false,
							"telegram_configured": false,
							"slack_configured":    false,
							"running":             "none",
							"telegram_running":    false,
							"slack_running":       false,
						},
					}, nil
				},
				HealthEnabled: true,
				Poke: func(ctx context.Context, input daemonruntime.PokeInput) error {
					if runHeartbeatTick == nil {
						return fmt.Errorf("heartbeat poke is unavailable")
					}
					return heartbeatruntime.ErrorFromTickResult(runHeartbeatTick(input))
				},
				Submit: func(ctx context.Context, req daemonruntime.SubmitTaskRequest) (daemonruntime.SubmitTaskResponse, error) {
					timeout := viper.GetDuration("timeout")
					if strings.TrimSpace(req.Timeout) != "" {
						if d, err := time.ParseDuration(req.Timeout); err == nil && d > 0 {
							timeout = d
						} else if err != nil {
							return daemonruntime.SubmitTaskResponse{}, daemonruntime.BadRequest("invalid timeout (use Go duration like 2m, 30s)")
						}
					}
					taskModel := strings.TrimSpace(req.Model)
					if taskModel == "" {
						taskModel = strings.TrimSpace(mainRoute.ClientConfig.Model)
					}
					trigger := normalizeServeTrigger(req.Trigger, daemonruntime.TaskTrigger{
						Source: "api",
						Event:  "http_submit",
						Ref:    "daemon/serve",
					})
					info, err := store.EnqueueWithTrigger(context.Background(), req.Task, taskModel, timeout, trigger)
					if err != nil {
						return daemonruntime.SubmitTaskResponse{}, err
					}
					return daemonruntime.SubmitTaskResponse{ID: info.ID, Status: info.Status}, nil
				},
			})

			mux.HandleFunc("/approvals/", func(w http.ResponseWriter, r *http.Request) {
				if !checkAuth(r, auth) {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				if sharedGuard == nil || !sharedGuard.Enabled() {
					http.Error(w, "guard is not enabled", http.StatusBadRequest)
					return
				}
				path := strings.TrimPrefix(r.URL.Path, "/approvals/")
				path = strings.Trim(path, "/")
				if path == "" {
					http.Error(w, "missing approval id", http.StatusBadRequest)
					return
				}
				parts := strings.Split(path, "/")
				id := strings.TrimSpace(parts[0])
				if id == "" {
					http.Error(w, "missing approval id", http.StatusBadRequest)
					return
				}

				type resolveReq struct {
					Actor   string `json:"actor,omitempty"`
					Comment string `json:"comment,omitempty"`
				}

				switch {
				case r.Method == http.MethodGet && len(parts) == 1:
					rec, ok, err := sharedGuard.GetApproval(r.Context(), id)
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					if !ok {
						http.NotFound(w, r)
						return
					}
					// Never return resume_state in the daemon API.
					out := map[string]any{
						"id":                      rec.ID,
						"run_id":                  rec.RunID,
						"created_at":              rec.CreatedAt,
						"expires_at":              rec.ExpiresAt,
						"resolved_at":             rec.ResolvedAt,
						"status":                  rec.Status,
						"actor":                   rec.Actor,
						"comment":                 rec.Comment,
						"action_type":             rec.ActionType,
						"tool_name":               rec.ToolName,
						"action_hash":             rec.ActionHash,
						"risk_level":              rec.RiskLevel,
						"decision":                rec.Decision,
						"reasons":                 rec.Reasons,
						"action_summary_redacted": rec.ActionSummaryRedacted,
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(out)
					return

				case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "approve":
					var req resolveReq
					_ = json.NewDecoder(r.Body).Decode(&req)
					if err := sharedGuard.ResolveApproval(r.Context(), id, guard.ApprovalApproved, req.Actor, req.Comment); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "status": "approved"})
					return

				case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "deny":
					var req resolveReq
					_ = json.NewDecoder(r.Body).Decode(&req)
					if err := sharedGuard.ResolveApproval(r.Context(), id, guard.ApprovalDenied, req.Actor, req.Comment); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					taskID, _ := store.FailPendingByApprovalID(id, "approval denied")
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "status": "denied", "task_id": taskID})
					return

				case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "resume":
					rec, ok, err := sharedGuard.GetApproval(r.Context(), id)
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					if !ok {
						http.NotFound(w, r)
						return
					}
					if rec.Status != guard.ApprovalApproved {
						http.Error(w, "approval is not approved", http.StatusConflict)
						return
					}
					taskID, err := store.EnqueueResumeByApprovalID(id)
					if err != nil {
						http.Error(w, err.Error(), http.StatusConflict)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "status": "queued", "task_id": taskID})
					return
				default:
					http.Error(w, "not found", http.StatusNotFound)
					return
				}
			})

			addr := listen
			srv := &http.Server{
				Addr:              addr,
				Handler:           mux,
				ReadHeaderTimeout: 5 * time.Second,
			}
			logger.Info("server_start", "addr", addr, "max_queue", maxQueue)
			return srv.ListenAndServe()
		},
	}

	cmd.Flags().String("server-listen", "127.0.0.1:8787", "HTTP listen address (host:port).")
	cmd.Flags().String("server-auth-token", "", "Bearer token required for all non-/health endpoints.")
	cmd.Flags().Int("server-max-queue", 100, "Max queued tasks in memory.")
	cmd.Flags().Bool("inspect-prompt", false, "Dump prompts (messages) to ./dump/prompt_daemon_YYYYMMDD_HHmmss.md.")
	cmd.Flags().Bool("inspect-request", false, "Dump LLM request/response payloads to ./dump/request_daemon_YYYYMMDD_HHmmss.md.")
	_ = cmd.Flags().MarkDeprecated("server-listen", "use channel-specific *.serve_listen or explicit --server-url instead")

	return cmd
}

func checkAuth(r *http.Request, token string) bool {
	got := strings.TrimSpace(r.Header.Get("Authorization"))
	want := "Bearer " + strings.TrimSpace(token)
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func normalizeServeTrigger(in *daemonruntime.TaskTrigger, fallback daemonruntime.TaskTrigger) daemonruntime.TaskTrigger {
	if in == nil {
		return fallback
	}
	trigger := daemonruntime.TaskTrigger{
		Source: strings.TrimSpace(in.Source),
		Event:  strings.TrimSpace(in.Event),
		Ref:    strings.TrimSpace(in.Ref),
	}
	if strings.TrimSpace(trigger.Source) == "" &&
		strings.TrimSpace(trigger.Event) == "" &&
		strings.TrimSpace(trigger.Ref) == "" {
		return fallback
	}
	if strings.TrimSpace(trigger.Source) == "" {
		trigger.Source = fallback.Source
	}
	if strings.TrimSpace(trigger.Event) == "" {
		trigger.Event = fallback.Event
	}
	if strings.TrimSpace(trigger.Ref) == "" {
		trigger.Ref = fallback.Ref
	}
	return trigger
}

func errorsIsContextDeadline(ctx context.Context, err error) bool {
	return daemonruntime.IsContextDeadline(ctx, err)
}

func runOneTask(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, client llm.Client, registry *tools.Registry, baseCfg agent.Config, sharedGuard *guard.Guard, task string, model string, meta map[string]any, skillsCfg skillsutil.SkillsConfig) (*agent.Final, *agent.Context, error) {
	ctx = llmstats.WithRunID(ctx, llmstats.NewSyntheticRunID("daemon"))
	skillsCfg.Roots = append([]string(nil), skillsCfg.Roots...)
	skillsCfg.Requested = append([]string(nil), skillsCfg.Requested...)
	promptSpec, _, err := skillsutil.PromptSpecWithSkills(ctx, logger, logOpts, task, client, model, skillsCfg)
	if err != nil {
		return nil, nil, err
	}
	promptprofile.ApplyPersonaIdentity(&promptSpec, logger)
	promptprofile.AppendLocalToolNotesBlock(&promptSpec, logger)
	promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, registry)
	promptprofile.AppendTodoWorkflowBlock(&promptSpec, registry)
	if wakeSignal, ok := daemonruntime.PokeInputFromMeta(meta); ok {
		promptprofile.AppendWakeSignalBlock(&promptSpec, wakeSignal)
	}
	engine := agent.New(
		client,
		registry,
		baseCfg,
		promptSpec,
		agent.WithLogger(logger),
		agent.WithLogOptions(logOpts),
		agent.WithGuard(sharedGuard),
	)
	return engine.Run(ctx, task, agent.RunOptions{Model: model, Scene: "daemon.loop", Meta: meta})
}

func resumeOneTask(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, client llm.Client, registry *tools.Registry, baseCfg agent.Config, sharedGuard *guard.Guard, approvalRequestID string) (*agent.Final, *agent.Context, error) {
	promptSpec := agent.DefaultPromptSpec()
	promptprofile.ApplyPersonaIdentity(&promptSpec, logger)
	promptprofile.AppendLocalToolNotesBlock(&promptSpec, logger)
	promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, registry)
	promptprofile.AppendTodoWorkflowBlock(&promptSpec, registry)
	engine := agent.New(
		client,
		registry,
		baseCfg,
		promptSpec,
		agent.WithLogger(logger),
		agent.WithLogOptions(logOpts),
		agent.WithGuard(sharedGuard),
	)
	return engine.Resume(ctx, approvalRequestID)
}

func pendingApprovalID(final *agent.Final) (string, bool) {
	if final == nil || final.Output == nil {
		return "", false
	}
	switch v := final.Output.(type) {
	case agent.PendingOutput:
		if strings.EqualFold(strings.TrimSpace(v.Status), "pending") && strings.TrimSpace(v.ApprovalRequestID) != "" {
			return strings.TrimSpace(v.ApprovalRequestID), true
		}
	case *agent.PendingOutput:
		if v != nil && strings.EqualFold(strings.TrimSpace(v.Status), "pending") && strings.TrimSpace(v.ApprovalRequestID) != "" {
			return strings.TrimSpace(v.ApprovalRequestID), true
		}
	case map[string]any:
		st, _ := v["status"].(string)
		id, _ := v["approval_request_id"].(string)
		if strings.EqualFold(strings.TrimSpace(st), "pending") && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id), true
		}
	}
	return "", false
}
