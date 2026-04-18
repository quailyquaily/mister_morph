package toolsutil

import (
	"strings"

	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

type RuntimeToolsRegisterConfig struct {
	PlanCreate PlanCreateRegisterConfig
	TodoUpdate TodoUpdateRegisterConfig
}

type runtimeRegisterConfigReader interface {
	GetBool(string) bool
	GetInt(string) int
	GetString(string) string
	IsSet(string) bool
}

type RuntimeToolLLMOptions struct {
	DefaultClient    llm.Client
	DefaultModel     string
	PlanCreateClient llm.Client
	PlanCreateModel  string
}

func LoadRuntimeToolsRegisterConfigFromViper() RuntimeToolsRegisterConfig {
	return LoadRuntimeToolsRegisterConfigFromReader(viper.GetViper())
}

func LoadRuntimeToolsRegisterConfigFromReader(r runtimeRegisterConfigReader) RuntimeToolsRegisterConfig {
	return RuntimeToolsRegisterConfig{
		PlanCreate: LoadPlanCreateRegisterConfigFromReader(r),
		TodoUpdate: LoadTodoUpdateRegisterConfigFromReader(r),
	}
}

func RegisterRuntimeTools(reg *tools.Registry, cfg RuntimeToolsRegisterConfig, opts RuntimeToolLLMOptions) {
	if reg == nil {
		return
	}
	planClient := opts.PlanCreateClient
	if planClient == nil {
		planClient = opts.DefaultClient
	}
	planModel := opts.PlanCreateModel
	if strings.TrimSpace(planModel) == "" {
		planModel = strings.TrimSpace(opts.DefaultModel)
	}
	RegisterPlanTool(reg, cfg.PlanCreate, planClient, planModel)
	RegisterTodoUpdateTool(reg, cfg.TodoUpdate, opts.DefaultClient, opts.DefaultModel)
}
