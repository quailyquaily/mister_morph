package toolsutil

import (
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/todo"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
	"github.com/spf13/viper"
)

type TodoUpdateRegisterConfig struct {
	Enabled      bool
	TODOPathWIP  string
	TODOPathDone string
	ContactsDir  string
}

type todoRegisterConfigReader interface {
	GetBool(string) bool
	GetString(string) string
}

func BuildTodoUpdateRegisterConfig(enabled bool, fileStateDir, contactsDirName string) TodoUpdateRegisterConfig {
	fileStateDir = strings.TrimSpace(fileStateDir)
	return TodoUpdateRegisterConfig{
		Enabled:      enabled,
		TODOPathWIP:  pathutil.ResolveStateFile(fileStateDir, statepaths.TODOWIPFilename),
		TODOPathDone: pathutil.ResolveStateFile(fileStateDir, statepaths.TODODONEFilename),
		ContactsDir:  pathutil.ResolveStateChildDir(fileStateDir, strings.TrimSpace(contactsDirName), "contacts"),
	}
}

func LoadTodoUpdateRegisterConfigFromViper() TodoUpdateRegisterConfig {
	return LoadTodoUpdateRegisterConfigFromReader(viper.GetViper())
}

func LoadTodoUpdateRegisterConfigFromReader(r todoRegisterConfigReader) TodoUpdateRegisterConfig {
	if r == nil {
		return BuildTodoUpdateRegisterConfig(false, "", "")
	}
	return BuildTodoUpdateRegisterConfig(
		r.GetBool("tools.todo_update.enabled"),
		r.GetString("file_state_dir"),
		r.GetString("contacts.dir_name"),
	)
}

func RegisterTodoUpdateTool(reg *tools.Registry, cfg TodoUpdateRegisterConfig, client llm.Client, model string) {
	if reg == nil {
		return
	}
	if !cfg.Enabled {
		return
	}
	reg.Register(builtin.NewTodoUpdateToolWithLLM(
		true,
		cfg.TODOPathWIP,
		cfg.TODOPathDone,
		cfg.ContactsDir,
		client,
		model,
	))
}

func SetTodoUpdateToolAddContext(reg *tools.Registry, ctx todo.AddResolveContext) {
	if reg == nil {
		return
	}
	raw, ok := reg.Get("todo_update")
	if !ok {
		return
	}
	tool, ok := raw.(*builtin.TodoUpdateTool)
	if !ok || tool == nil {
		return
	}
	tool.SetAddContext(ctx)
}
