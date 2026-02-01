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

	"github.com/quailyquaily/mister_morph/agent"
	"github.com/quailyquaily/mister_morph/llm"
	"github.com/quailyquaily/mister_morph/tools"
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
				RequestTimeout: viper.GetDuration("llm.request_timeout"),
			})
			if err != nil {
				return err
			}
			reg := registryFromViper()

			logOpts := logOptionsFromViper()

			baseCfg := agent.Config{
				MaxSteps:       viper.GetInt("max_steps"),
				ParseRetries:   viper.GetInt("parse_retries"),
				MaxTokenBudget: viper.GetInt("max_token_budget"),
				PlanMode:       viper.GetString("plan.mode"),
			}

			// Worker: process tasks sequentially.
			go func() {
				for {
					qt := store.Next()
					if qt == nil || qt.info == nil {
						continue
					}
					id := qt.info.ID
					started := time.Now()
					store.Update(id, func(info *TaskInfo) {
						info.Status = TaskRunning
						info.StartedAt = &started
					})

					final, runCtx, runErr := runOneTask(qt.ctx, logger, logOpts, client, reg, baseCfg, qt.info.Task, qt.info.Model)
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

func runOneTask(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, client llm.Client, registry *tools.Registry, baseCfg agent.Config, task string, model string) (*agent.Final, *agent.Context, error) {
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
	)
	return engine.Run(ctx, task, agent.RunOptions{Model: model})
}
