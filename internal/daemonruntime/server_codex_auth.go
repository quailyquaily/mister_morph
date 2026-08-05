package daemonruntime

import (
	"context"
	"fmt"
	"net/http"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/codexauth"
)

func registerRuntimeCodexAuthRoutes(
	mux *http.ServeMux,
	authToken string,
	stateDir string,
	settingsOwner agentsettings.Owner,
) {
	handler := codexauth.NewHTTPHandler(codexauth.HTTPHandlerOptions{
		StateDir: stateDir,
		SetDefault: func(ctx context.Context) error {
			return setRuntimeCodexAsDefaultLLM(ctx, settingsOwner)
		},
	})
	register := func(path string, serve func(http.ResponseWriter, *http.Request)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if !checkAuth(r, authToken) {
				writeRuntimeAuthError(w)
				return
			}
			serve(w, r)
		})
	}
	register("/auth/codex/status", handler.Status)
	register("/auth/codex/refresh", handler.Refresh)
	register("/auth/codex/login/start", handler.LoginStart)
	register("/auth/codex/login/poll", handler.LoginPoll)
	register("/auth/codex/logout", handler.Logout)
}

func setRuntimeCodexAsDefaultLLM(ctx context.Context, owner agentsettings.Owner) error {
	if owner == nil {
		return fmt.Errorf("agent settings are unavailable")
	}
	provider := codexauth.ProviderName
	model := codexauth.DefaultModel
	empty := ""
	_, err := owner.Update(ctx, agentsettings.AgentSettingsUpdate{
		LLM: agentsettings.LLMSettingsUpdate{
			LLMConfigFieldsUpdate: agentsettings.LLMConfigFieldsUpdate{
				InferenceProvider:   &provider,
				Provider:            &provider,
				Model:               &model,
				Endpoint:            &empty,
				APIKey:              &empty,
				CloudflareAPIToken:  &empty,
				CloudflareAccountID: &empty,
				BedrockAWSKey:       &empty,
				BedrockAWSSecret:    &empty,
				BedrockRegion:       &empty,
				BedrockModelARN:     &empty,
			},
		},
	})
	return err
}
