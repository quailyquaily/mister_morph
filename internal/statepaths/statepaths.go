package statepaths

import (
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/spf13/viper"
)

const (
	HeartbeatChecklistFilename = "HEARTBEAT.md"
	ScriptsNotesFilename       = "SCRIPTS.md"
	TODOWIPFilename            = "TODO.md"
	TODODONEFilename           = "TODO.DONE.md"
)

func FileStateDir() string {
	return pathutil.ResolveStateDir(viper.GetString("file_state_dir"))
}

func MemoryDir() string {
	return pathutil.ResolveStateChildDir(
		viper.GetString("file_state_dir"),
		viper.GetString("memory.dir_name"),
		"memory",
	)
}

func SkillsDir() string {
	return pathutil.ResolveStateChildDir(
		viper.GetString("file_state_dir"),
		viper.GetString("skills.dir_name"),
		"skills",
	)
}

func ContactsDir() string {
	return pathutil.ResolveStateChildDir(
		viper.GetString("file_state_dir"),
		viper.GetString("contacts.dir_name"),
		"contacts",
	)
}

func TasksDir() string {
	return pathutil.ResolveStateChildDir(
		viper.GetString("file_state_dir"),
		viper.GetString("tasks.dir_name"),
		"tasks",
	)
}

func TaskTargetDir(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		target = "tasks"
	}
	return filepath.Clean(filepath.Join(TasksDir(), target))
}

func StatsDir() string {
	return filepath.Clean(filepath.Join(FileStateDir(), "stats"))
}

func LLMUsageJournalDir() string {
	return filepath.Clean(filepath.Join(StatsDir(), "llm_usage"))
}

func LLMUsageProjectionPath() string {
	return filepath.Clean(filepath.Join(StatsDir(), "llm_usage_projection.json"))
}

func HeartbeatChecklistPath() string {
	return pathutil.ResolveStateFile(viper.GetString("file_state_dir"), HeartbeatChecklistFilename)
}

func ScriptsNotesPath() string {
	return pathutil.ResolveStateFile(viper.GetString("file_state_dir"), ScriptsNotesFilename)
}

func TODOWIPPath() string {
	return pathutil.ResolveStateFile(viper.GetString("file_state_dir"), TODOWIPFilename)
}

func TODODONEPath() string {
	return pathutil.ResolveStateFile(viper.GetString("file_state_dir"), TODODONEFilename)
}

func WorkspaceAttachmentsPath() string {
	return pathutil.ResolveStateFile(viper.GetString("file_state_dir"), "workspace_attachments.json")
}

func DefaultSkillsRoots() []string {
	return dedupeNonEmptyStrings([]string{SkillsDir()})
}

func dedupeNonEmptyStrings(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
