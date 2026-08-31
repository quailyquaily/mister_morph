package consolecmd

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/spf13/viper"
)

type llmSettingsPayload = agentsettings.LLMSettingsPayload
type agentSettingsTestResult = agentsettings.ConnectionTestResult

type agentSettingsConnectionTestOptions struct {
	InspectPrompt     bool
	InspectRequest    bool
	RequestTimeoutRaw string
	Reader            agentsettings.Reader
}

func (s *server) handleAgentSettings(w http.ResponseWriter, r *http.Request) {
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	owner := agentsettings.NewFileOwner(agentsettings.FileOwnerOptions{
		ConfigPath: configPath,
		Reader:     s.currentRuntimeConfigReader(),
		OSStore:    s.secretStore,
	})
	agentsettings.NewHandler(agentsettings.HandlerOptions{Owner: owner}).Settings(w, r)
}

func (s *server) handleAgentSettingsModels(w http.ResponseWriter, r *http.Request) {
	owner := agentsettings.NewFileOwner(agentsettings.FileOwnerOptions{Reader: s.currentRuntimeConfigReader()})
	agentsettings.NewHandler(agentsettings.HandlerOptions{Owner: owner}).Models(w, r)
}

func (s *server) handleAgentSettingsTest(w http.ResponseWriter, r *http.Request) {
	reader := s.currentRuntimeConfigReader()
	connectionTest := defaultAgentSettingsConnectionTest
	if s != nil && s.agentSettingsConnectionTest != nil {
		connectionTest = s.agentSettingsConnectionTest
	}
	owner := agentsettings.NewFileOwner(agentsettings.FileOwnerOptions{Reader: reader})
	handler := agentsettings.NewHandler(agentsettings.HandlerOptions{
		Owner: owner,
		ConnectionTest: func(ctx context.Context, settings agentsettings.LLMSettingsPayload, current agentsettings.Reader, _ agentsettings.ConnectionTestOptions) (agentsettings.ConnectionTestResult, error) {
			return connectionTest(ctx, settings, agentSettingsConnectionTestOptions{
				InspectPrompt:     s != nil && s.cfg.inspectPrompt,
				InspectRequest:    s != nil && s.cfg.inspectRequest,
				RequestTimeoutRaw: strings.TrimSpace(current.GetString("llm.request_timeout")),
				Reader:            current,
			})
		},
	})
	handler.Test(w, r)
}

func resolveConsoleConfigPath() (string, error) {
	explicit := strings.TrimSpace(viper.GetString("config"))
	if explicit != "" {
		return pathutil.ExpandHomePath(explicit), nil
	}
	return pathutil.DefaultConfigPath(), nil
}

func defaultAgentSettingsConnectionTest(ctx context.Context, settings llmSettingsPayload, opts agentSettingsConnectionTestOptions) (agentSettingsTestResult, error) {
	if opts.Reader == nil {
		return agentSettingsTestResult{}, fmt.Errorf("config reader is nil")
	}
	values, err := agentsettings.ResolveConnectionTestValues(
		opts.Reader,
		settings,
		llmutil.RouteProfileDefault,
		configutil.SecretRefSourceFromReader(opts.Reader),
	)
	if err != nil {
		return agentSettingsTestResult{}, err
	}
	return agentsettings.RunConnectionTest(ctx, values, agentsettings.ConnectionTestOptions{
		InspectPrompt:  opts.InspectPrompt,
		InspectRequest: opts.InspectRequest,
	})
}
