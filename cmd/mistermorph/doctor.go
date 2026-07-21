package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var requiredSlackBotScopes = []string{
	"app_mentions:read",
	"channels:history",
	"channels:read",
	"chat:write",
	"emoji:read",
	"files:read",
	"files:write",
	"groups:history",
	"groups:read",
	"im:history",
	"im:read",
	"mpim:history",
	"mpim:read",
	"reactions:write",
	"users:read",
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "doctor",
		Short:         "Check local configuration",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := resolveConfigFile()
			return runDoctor(cmd.OutOrStdout(), path, &http.Client{Timeout: 15 * time.Second})
		},
	}
}

func runDoctor(out io.Writer, configPath string, httpClient *http.Client) error {
	formattedOut := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	defer formattedOut.Flush()
	out = formattedOut

	_, _ = fmt.Fprintln(out, "MisterMorph doctor")
	_, _ = fmt.Fprintln(out)

	if strings.TrimSpace(configPath) == "" {
		err := fmt.Errorf("config file not found; pass --config")
		_, _ = fmt.Fprintln(out, "Config")
		_, _ = fmt.Fprintln(out, "  Path\tMISSING")
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "Summary")
		_, _ = fmt.Fprintln(out, "  Status\tFAILED")
		_, _ = fmt.Fprintf(out, "  Error\t%v\n", err)
		return err
	}
	checks, err := configutil.InspectConfigEnvRefs(configPath)
	if err != nil {
		err = fmt.Errorf("inspect config: %w", err)
		_, _ = fmt.Fprintln(out, "Config")
		_, _ = fmt.Fprintf(out, "  Path\t%s\n", configPath)
		_, _ = fmt.Fprintln(out, "  Load\tERROR")
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "Summary")
		_, _ = fmt.Fprintln(out, "  Status\tFAILED")
		_, _ = fmt.Fprintf(out, "  Error\t%v\n", err)
		return err
	}

	settings := viper.New()
	configdefaults.Apply(settings)
	settings.SetEnvPrefix(envPrefix)
	settings.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	settings.AutomaticEnv()
	if err := configutil.ReadExpandedConfig(settings, configPath, nil); err != nil {
		err = fmt.Errorf("load config: %w", err)
		_, _ = fmt.Fprintln(out, "Config")
		_, _ = fmt.Fprintf(out, "  Path\t%s\n", configPath)
		_, _ = fmt.Fprintln(out, "  Load\tERROR")
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "Summary")
		_, _ = fmt.Fprintln(out, "  Status\tFAILED")
		_, _ = fmt.Fprintf(out, "  Error\t%v\n", err)
		return err
	}

	_, _ = fmt.Fprintln(out, "Config")
	_, _ = fmt.Fprintf(out, "  Path\t%s\n", configPath)
	_, _ = fmt.Fprintln(out, "  Load\tOK")
	_, _ = fmt.Fprintln(out)

	_, _ = fmt.Fprintln(out, "Environment references")
	problems := 0
	if len(checks) == 0 {
		_, _ = fmt.Fprintln(out, "  Status\tnone")
	} else {
		_, _ = fmt.Fprintln(out, "  Name\tStatus")
		for _, check := range checks {
			status := "OK"
			switch check.Status {
			case configutil.EnvRefEmpty:
				status = "EMPTY"
			case configutil.EnvRefMissing:
				status = "MISSING"
			}
			_, _ = fmt.Fprintf(out, "  %s\t%s\n", check.Name, status)
			if check.Status != configutil.EnvRefSet {
				problems++
			}
		}
	}
	_, _ = fmt.Fprintln(out)

	problems += checkDoctorSlack(out, settings, httpClient)
	_, _ = fmt.Fprintln(out)

	_, _ = fmt.Fprintln(out, "Summary")
	if problems == 0 {
		_, _ = fmt.Fprintln(out, "  Status\tOK")
		_, _ = fmt.Fprintln(out, "  Problems\t0")
		return nil
	}

	_, _ = fmt.Fprintln(out, "  Status\tFAILED")
	_, _ = fmt.Fprintf(out, "  Problems\t%d\n", problems)
	return fmt.Errorf("doctor found %d config problem(s)", problems)
}

func checkDoctorSlack(out io.Writer, settings *viper.Viper, httpClient *http.Client) int {
	botToken := strings.TrimSpace(settings.GetString("slack.bot_token"))
	appToken := strings.TrimSpace(settings.GetString("slack.app_token"))
	managed := false
	for _, item := range settings.GetStringSlice("console.managed_runtimes") {
		for _, name := range strings.FieldsFunc(item, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
		}) {
			if strings.EqualFold(strings.TrimSpace(name), "slack") {
				managed = true
				break
			}
		}
	}
	if !managed && botToken == "" && appToken == "" {
		_, _ = fmt.Fprintln(out, "Slack")
		_, _ = fmt.Fprintln(out, "  Status\tdisabled")
		return 0
	}

	enabledBy := "configured"
	if managed {
		enabledBy = "managed"
	}
	_, _ = fmt.Fprintln(out, "Slack")
	_, _ = fmt.Fprintf(out, "  Status\tenabled (%s)\n", enabledBy)
	problems := 0
	baseURL := strings.TrimRight(strings.TrimSpace(settings.GetString("slack.base_url")), "/")
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	if botToken == "" {
		_, _ = fmt.Fprintln(out, "  Bot token\tMISSING")
		_, _ = fmt.Fprintln(out, "  Bot scopes\tUNAVAILABLE")
		_, _ = fmt.Fprintln(out, "    Bot token is missing")
		problems++
	} else {
		headers, err := probeSlackAPI(httpClient, baseURL, "/auth.test", botToken, false)
		if err != nil {
			_, _ = fmt.Fprintln(out, "  Bot token\tERROR")
			_, _ = fmt.Fprintf(out, "    %v\n", err)
			_, _ = fmt.Fprintln(out, "  Bot scopes\tUNAVAILABLE")
			_, _ = fmt.Fprintln(out, "    Bot token check failed")
			problems++
		} else {
			_, _ = fmt.Fprintln(out, "  Bot token\tOK")
			missing, available := missingSlackBotScopes(headers.Get("X-OAuth-Scopes"))
			switch {
			case !available:
				_, _ = fmt.Fprintln(out, "  Bot scopes\tUNAVAILABLE")
				_, _ = fmt.Fprintln(out, "    X-OAuth-Scopes response header is missing")
				problems++
			case len(missing) > 0:
				_, _ = fmt.Fprintln(out, "  Bot scopes\tMISSING")
				for _, scope := range missing {
					_, _ = fmt.Fprintf(out, "    - %s\n", scope)
				}
				problems++
			default:
				_, _ = fmt.Fprintln(out, "  Bot scopes\tOK")
			}
		}
	}

	if appToken == "" {
		_, _ = fmt.Fprintln(out, "  App token\tMISSING")
		problems++
	} else if _, err := probeSlackAPI(httpClient, baseURL, "/apps.connections.open", appToken, true); err != nil {
		_, _ = fmt.Fprintln(out, "  App token\tERROR")
		_, _ = fmt.Fprintf(out, "    %v\n", err)
		problems++
	} else {
		_, _ = fmt.Fprintln(out, "  App token\tOK")
	}

	return problems
}

func missingSlackBotScopes(header string) ([]string, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, false
	}
	granted := make(map[string]struct{})
	for _, scope := range strings.Split(header, ",") {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			granted[scope] = struct{}{}
		}
	}
	missing := make([]string, 0)
	for _, scope := range requiredSlackBotScopes {
		if _, ok := granted[scope]; !ok {
			missing = append(missing, scope)
		}
	}
	sort.Strings(missing)
	return missing, true
}

func probeSlackAPI(httpClient *http.Client, baseURL, path, token string, requireURL bool) (http.Header, error) {
	req, err := http.NewRequest(http.MethodPost, baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid Slack API URL")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		URL   string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid JSON response")
	}
	if !result.OK {
		code := strings.TrimSpace(result.Error)
		if code == "" {
			code = "unknown_error"
		}
		return nil, fmt.Errorf("slack API error: %s", code)
	}
	if requireURL && strings.TrimSpace(result.URL) == "" {
		return nil, fmt.Errorf("slack API returned an empty socket URL")
	}
	return resp.Header, nil
}
