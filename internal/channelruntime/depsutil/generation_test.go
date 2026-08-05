package depsutil

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/spf13/viper"
)

func TestBuildGenerationDependenciesUsesCandidateReader(t *testing.T) {
	reader := viper.New()
	configdefaults.Apply(reader)
	reader.Set("llm.provider", "openai")
	reader.Set("llm.endpoint", "https://api.example.test/v1")
	reader.Set("llm.model", "candidate-model")
	reader.Set("llm.api_key", "candidate-key")
	reader.Set("tools.bash.enabled", false)
	reader.Set("skills.enabled", false)

	base := CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.New(slog.NewTextHandler(io.Discard, nil)), nil
		},
		AgentSettingsOwner: agentsettings.NewFileOwner(agentsettings.FileOwnerOptions{Reader: reader}),
	}
	deps, cleanup, err := BuildGenerationDependencies(context.Background(), base, agentsettings.NewReaderSnapshot(reader))
	if err != nil {
		t.Fatalf("BuildGenerationDependencies() error = %v", err)
	}
	defer cleanup()

	route, err := deps.ResolveLLMRoute(llmutil.RoutePurposeMainLoop)
	if err != nil {
		t.Fatalf("ResolveLLMRoute() error = %v", err)
	}
	if route.ClientConfig.Model != "candidate-model" {
		t.Fatalf("route model = %q, want candidate-model", route.ClientConfig.Model)
	}
	if deps.AgentSettingsOwner != base.AgentSettingsOwner {
		t.Fatal("settings owner was not preserved")
	}
	if deps.AgentSettingsReader.GetString("llm.model") != "candidate-model" {
		t.Fatalf("settings reader model = %q", deps.AgentSettingsReader.GetString("llm.model"))
	}
	if registry := deps.Registry(); registry != nil {
		if _, ok := registry.Get("bash"); ok {
			t.Fatal("disabled bash tool was registered")
		}
	}
}
