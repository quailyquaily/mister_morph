package toolsutil

import (
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
	"github.com/spf13/viper"
)

type PlanCreateRegisterConfig struct {
	Enabled  bool
	MaxSteps int
}

type planRegisterConfigReader interface {
	GetBool(string) bool
	GetInt(string) int
	IsSet(string) bool
}

func BuildPlanCreateRegisterConfig(enabled bool, maxSteps int) PlanCreateRegisterConfig {
	if maxSteps <= 0 {
		maxSteps = 6
	}
	return PlanCreateRegisterConfig{
		Enabled:  enabled,
		MaxSteps: maxSteps,
	}
}

func LoadPlanCreateRegisterConfigFromViper() PlanCreateRegisterConfig {
	return LoadPlanCreateRegisterConfigFromReader(viper.GetViper())
}

func LoadPlanCreateRegisterConfigFromReader(r planRegisterConfigReader) PlanCreateRegisterConfig {
	if r == nil {
		return BuildPlanCreateRegisterConfig(true, 6)
	}
	enabled := true
	if r.IsSet("tools.plan_create.enabled") {
		enabled = r.GetBool("tools.plan_create.enabled")
	}
	return BuildPlanCreateRegisterConfig(
		enabled,
		r.GetInt("tools.plan_create.max_steps"),
	)
}

func RegisterPlanTool(reg *tools.Registry, cfg PlanCreateRegisterConfig, client llm.Client, defaultModel string) {
	if reg == nil {
		return
	}
	if !cfg.Enabled {
		return
	}
	names := toolNames(reg)
	names = append(names, "plan_create")
	reg.Register(builtin.NewPlanCreateTool(client, defaultModel, names, cfg.MaxSteps))
}

func toolNames(reg *tools.Registry) []string {
	all := reg.All()
	out := make([]string, 0, len(all))
	for _, t := range all {
		out = append(out, t.Name())
	}
	return out
}
