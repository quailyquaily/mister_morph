package depsutil

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
)

func TestCommonDependenciesValidate(t *testing.T) {
	t.Parallel()

	valid := CommonDependencies{
		Logger: func() (*slog.Logger, error) { return slog.Default(), nil },
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolvedRoute{}, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) { return nil, nil },
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{}, nil, nil
		},
	}

	tests := []struct {
		name string
		edit func(*CommonDependencies)
		want string
	}{
		{name: "valid", edit: func(*CommonDependencies) {}},
		{name: "logger", edit: func(d *CommonDependencies) { d.Logger = nil }, want: "Logger"},
		{name: "route resolver", edit: func(d *CommonDependencies) { d.ResolveLLMRoute = nil }, want: "ResolveLLMRoute"},
		{name: "client factory", edit: func(d *CommonDependencies) { d.CreateLLMClient = nil }, want: "CreateLLMClient"},
		{name: "prompt spec", edit: func(d *CommonDependencies) { d.PromptSpec = nil }, want: "PromptSpec"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := valid
			tt.edit(&d)
			err := d.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want dependency name %q", err, tt.want)
			}
		})
	}
}
