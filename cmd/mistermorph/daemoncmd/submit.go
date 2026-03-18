package daemoncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewSubmitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit a task to a running runtime endpoint",
		RunE: func(cmd *cobra.Command, args []string) error {
			task, _ := cmd.Flags().GetString("task")
			task = strings.TrimSpace(task)
			if task == "" {
				task = strings.TrimSpace(viper.GetString("task"))
			}
			if task == "" {
				data, err := os.ReadFile("/dev/stdin")
				if err == nil {
					task = strings.TrimSpace(string(data))
				}
			}
			if task == "" {
				return fmt.Errorf("missing --task (or stdin)")
			}

			serverURL, _ := cmd.Flags().GetString("server-url")
			serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
			if serverURL == "" {
				serverListen := strings.TrimSpace(viper.GetString("server.listen"))
				if serverListen == "" {
					serverListen = "127.0.0.1:8787"
				}
				serverURL = "http://" + serverListen
			}
			auth, _ := cmd.Flags().GetString("auth-token")
			auth = strings.TrimSpace(auth)
			if auth == "" {
				auth = strings.TrimSpace(viper.GetString("server.auth_token"))
			}
			if auth == "" {
				return fmt.Errorf("missing server.auth_token (set via --auth-token or MISTER_MORPH_SERVER_AUTH_TOKEN)")
			}

			model, _ := cmd.Flags().GetString("model")
			model = strings.TrimSpace(model)
			if model == "" {
				model = llmutil.ModelFromViper()
			}
			reqBody := daemonruntime.SubmitTaskRequest{
				Task:    task,
				Model:   model,
				Timeout: strings.TrimSpace(configutil.FlagOrViperString(cmd, "submit-timeout", "submit.timeout")),
			}
			b, _ := json.Marshal(reqBody)

			httpReq, err := http.NewRequest(http.MethodPost, serverURL+"/tasks", bytes.NewReader(b))
			if err != nil {
				return err
			}
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Authorization", "Bearer "+auth)

			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Do(httpReq)
			if err != nil {
				return err
			}
			raw, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return fmt.Errorf("server http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
			}

			var submitResp daemonruntime.SubmitTaskResponse
			if err := json.Unmarshal(raw, &submitResp); err != nil {
				return fmt.Errorf("failed to parse server response: %w", err)
			}

			wait := configutil.FlagOrViperBool(cmd, "wait", "submit.wait")
			if !wait {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(submitResp)
			}

			interval := configutil.FlagOrViperDuration(cmd, "poll-interval", "submit.poll_interval")
			if interval <= 0 {
				interval = 1 * time.Second
			}

			for {
				time.Sleep(interval)
				info, err := fetchTaskInfo(client, serverURL, auth, submitResp.ID)
				if err != nil {
					return err
				}
				switch info.Status {
				case daemonruntime.TaskQueued, daemonruntime.TaskRunning:
					continue
				case daemonruntime.TaskPending:
					// Print the final object if present (it should include status=pending + approval_request_id).
					if m, ok := info.Result.(map[string]any); ok {
						if final, ok := m["final"]; ok {
							enc := json.NewEncoder(os.Stdout)
							enc.SetIndent("", "  ")
							return enc.Encode(final)
						}
					}
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(info)
				case daemonruntime.TaskDone:
					// Prefer printing the final object if present.
					if m, ok := info.Result.(map[string]any); ok {
						if final, ok := m["final"]; ok {
							enc := json.NewEncoder(os.Stdout)
							enc.SetIndent("", "  ")
							return enc.Encode(final)
						}
					}
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(info)
				case daemonruntime.TaskFailed, daemonruntime.TaskCanceled:
					return fmt.Errorf("%s: %s", info.Status, info.Error)
				default:
					return fmt.Errorf("unknown status: %s", info.Status)
				}
			}
		},
	}

	cmd.Flags().String("task", "", "Task to submit (if empty, reads from stdin).")
	cmd.Flags().String("server-url", "", "Runtime base URL (recommended; deprecated fallback: http://<server.listen> or http://127.0.0.1:8787).")
	cmd.Flags().String("auth-token", "", "Bearer token for daemon auth.")
	cmd.Flags().String("model", "", "Model name override (optional).")
	cmd.Flags().String("submit-timeout", "", "Per-task timeout override (e.g. 2m, 30s).")
	cmd.Flags().Bool("wait", false, "Wait for completion and print the final JSON.")
	cmd.Flags().Duration("poll-interval", 1*time.Second, "Polling interval when --wait is set.")

	return cmd
}

func fetchTaskInfo(client *http.Client, serverURL, auth, id string) (*daemonruntime.TaskInfo, error) {
	httpReq, err := http.NewRequest(http.MethodGet, strings.TrimRight(serverURL, "/")+"/tasks/"+id, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+auth)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("server http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var info daemonruntime.TaskInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("failed to parse task info: %w", err)
	}
	return &info, nil
}
