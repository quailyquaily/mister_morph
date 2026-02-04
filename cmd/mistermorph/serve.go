package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run as a local daemon that accepts tasks over HTTP",
		RunE: func(cmd *cobra.Command, args []string) error {
			bind := strings.TrimSpace(flagOrViperString(cmd, "server-bind", "server.bind"))
			if bind == "" {
				bind = "127.0.0.1"
			}
			port := flagOrViperInt(cmd, "server-port", "server.port")
			if port <= 0 {
				port = 8787
			}
			auth := flagOrViperString(cmd, "server-auth-token", "server.auth_token")
			if strings.TrimSpace(auth) == "" {
				return fmt.Errorf("missing server.auth_token (set via --server-auth-token or MISTER_MORPH_SERVER_AUTH_TOKEN)")
			}

			maxQueue := flagOrViperInt(cmd, "server-max-queue", "server.max_queue")
			store := NewTaskStore(maxQueue)

			logger, err := loggerFromViper()
			if err != nil {
				return err
			}
			slog.SetDefault(logger)

			client, err := llmClientFromConfig(llmClientConfig{
				Provider:       llmProviderFromViper(),
				Endpoint:       llmEndpointFromViper(),
				APIKey:         llmAPIKeyFromViper(),
				Model:          llmModelFromViper(),
				RequestTimeout: viper.GetDuration("llm.request_timeout"),
			})
			if err != nil {
				return err
			}
			reg := registryFromViper()
			registerPlanTool(reg, client, llmModelFromViper())

			logOpts := logOptionsFromViper()

			baseCfg := agent.Config{
				MaxSteps:       viper.GetInt("max_steps"),
				ParseRetries:   viper.GetInt("parse_retries"),
				MaxTokenBudget: viper.GetInt("max_token_budget"),
			}

			sharedGuard := guardFromViper(logger)

			// Worker: process tasks sequentially.
			go func() {
				for {
					qt := store.Next()
					if qt == nil || qt.info == nil {
						continue
					}
					id := qt.info.ID
					resumeApprovalID := strings.TrimSpace(qt.resumeApprovalID)
					started := time.Now()
					store.Update(id, func(info *TaskInfo) {
						info.Status = TaskRunning
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
						final, runCtx, runErr = runOneTask(qt.ctx, logger, logOpts, client, reg, baseCfg, sharedGuard, qt.info.Task, qt.info.Model, nil)
					}

					if pendingID, ok := pendingApprovalID(final); ok && runErr == nil {
						pendingAt := time.Now()
						store.Update(id, func(info *TaskInfo) {
							info.Status = TaskPending
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

					finished := time.Now()
					store.Update(id, func(info *TaskInfo) {
						info.FinishedAt = &finished
						if runErr != nil {
							if errorsIsContextDeadline(qt.ctx, runErr) {
								info.Status = TaskCanceled
							} else {
								info.Status = TaskFailed
							}
							info.Error = runErr.Error()
							return
						}
						info.Status = TaskDone
						info.Result = map[string]any{
							"final":   final,
							"metrics": runCtx.Metrics,
							"steps":   summarizeSteps(runCtx),
						}
					})
					qt.cancel()
				}
			}()

			mux := http.NewServeMux()
			mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":   true,
					"time": time.Now().Format(time.RFC3339Nano),
				})
			})
			mux.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					return
				}
				if !checkAuth(r, auth) {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				var req SubmitTaskRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid json", http.StatusBadRequest)
					return
				}
				req.Task = strings.TrimSpace(req.Task)
				if req.Task == "" {
					http.Error(w, "missing task", http.StatusBadRequest)
					return
				}

				timeout := viper.GetDuration("timeout")
				if strings.TrimSpace(req.Timeout) != "" {
					if d, err := time.ParseDuration(req.Timeout); err == nil && d > 0 {
						timeout = d
					} else if err != nil {
						http.Error(w, "invalid timeout (use Go duration like 2m, 30s)", http.StatusBadRequest)
						return
					}
				}
				model := strings.TrimSpace(req.Model)
				if model == "" {
					model = llmModelFromViper()
				}

				info, err := store.Enqueue(context.Background(), req.Task, model, timeout)
				if err != nil {
					http.Error(w, err.Error(), http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(SubmitTaskResponse{ID: info.ID, Status: info.Status})
			})
			mux.HandleFunc("/tasks/", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					return
				}
				if !checkAuth(r, auth) {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				id := strings.TrimPrefix(r.URL.Path, "/tasks/")
				id = strings.TrimSpace(id)
				if id == "" {
					http.Error(w, "missing id", http.StatusBadRequest)
					return
				}
				info, ok := store.Get(id)
				if !ok {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(info)
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

			addr := bind + ":" + strconv.Itoa(port)
			srv := &http.Server{
				Addr:              addr,
				Handler:           mux,
				ReadHeaderTimeout: 5 * time.Second,
			}
			logger.Info("server_start", "addr", addr, "max_queue", maxQueue)
			return srv.ListenAndServe()
		},
	}

	cmd.Flags().String("server-bind", "127.0.0.1", "Bind address (default: 127.0.0.1).")
	cmd.Flags().Int("server-port", 8787, "HTTP port to listen on.")
	cmd.Flags().String("server-auth-token", "", "Bearer token required for all non-/health endpoints.")
	cmd.Flags().Int("server-max-queue", 100, "Max queued tasks in memory.")

	return cmd
}

func checkAuth(r *http.Request, token string) bool {
	got := strings.TrimSpace(r.Header.Get("Authorization"))
	want := "Bearer " + strings.TrimSpace(token)
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func errorsIsContextDeadline(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "context deadline exceeded")
}

func runOneTask(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, client llm.Client, registry *tools.Registry, baseCfg agent.Config, sharedGuard *guard.Guard, task string, model string, meta map[string]any) (*agent.Final, *agent.Context, error) {
	promptSpec, _, skillAuthProfiles, err := promptSpecWithSkills(ctx, logger, logOpts, task, client, model, skillsConfigFromViper(model))
	if err != nil {
		return nil, nil, err
	}
	engine := agent.New(
		client,
		registry,
		baseCfg,
		promptSpec,
		agent.WithLogger(logger),
		agent.WithLogOptions(logOpts),
		agent.WithSkillAuthProfiles(skillAuthProfiles, viper.GetBool("secrets.require_skill_profiles")),
		agent.WithGuard(sharedGuard),
	)
	return engine.Run(ctx, task, agent.RunOptions{Model: model, Meta: meta})
}

func resumeOneTask(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, client llm.Client, registry *tools.Registry, baseCfg agent.Config, sharedGuard *guard.Guard, approvalRequestID string) (*agent.Final, *agent.Context, error) {
	engine := agent.New(
		client,
		registry,
		baseCfg,
		agent.DefaultPromptSpec(),
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
