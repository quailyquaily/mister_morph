package consolecmd

import (
	"context"
	"crypto/rand"
	"encoding/base64"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/codexauth"
)

func randomOpaqueID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *server) setCodexAsDefaultLLM(ctx context.Context) error {
	provider := codexauth.ProviderName
	model := codexauth.DefaultModel
	empty := ""
	update := agentsettings.AgentSettingsUpdate{
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
	}
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		return err
	}
	owner := agentsettings.NewFileOwner(agentsettings.FileOwnerOptions{ConfigPath: configPath, Reader: s.currentRuntimeConfigReader()})
	_, err = owner.Update(ctx, update)
	return err
}
