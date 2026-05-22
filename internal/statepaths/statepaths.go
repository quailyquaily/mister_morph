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
	CronFilename               = "cron.yaml"
	PersonaDirName             = "persona"
	IdentityFilename           = "identity.yaml"
	SoulFilename               = "soul.md"
	AvatarFilename             = "avatar.webp"
	LegacyIdentityFilename     = "IDENTITY.md"
	LegacySoulFilename         = "SOUL.md"
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

func PersonaDir() string {
	return filepath.Clean(filepath.Join(FileStateDir(), PersonaDirName))
}

func PersonaIdentityPath() string {
	return filepath.Clean(filepath.Join(PersonaDir(), IdentityFilename))
}

func PersonaSoulPath() string {
	return filepath.Clean(filepath.Join(PersonaDir(), SoulFilename))
}

func PersonaAvatarPath() string {
	return filepath.Clean(filepath.Join(PersonaDir(), AvatarFilename))
}

func LegacyPersonaIdentityPath() string {
	return filepath.Clean(filepath.Join(PersonaDir(), LegacyIdentityFilename))
}

func LegacyPersonaSoulPath() string {
	return filepath.Clean(filepath.Join(PersonaDir(), LegacySoulFilename))
}

func LegacyRootIdentityPath() string {
	return pathutil.ResolveStateFile(viper.GetString("file_state_dir"), LegacyIdentityFilename)
}

func LegacyRootSoulPath() string {
	return pathutil.ResolveStateFile(viper.GetString("file_state_dir"), LegacySoulFilename)
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

func CronPath() string {
	return pathutil.ResolveStateFile(viper.GetString("file_state_dir"), CronFilename)
}

func WorkspaceAttachmentsPath() string {
	return pathutil.ResolveStateFile(viper.GetString("file_state_dir"), "workspace_attachments.json")
}

func TopicContextPath() string {
	return pathutil.ResolveStateFile(viper.GetString("file_state_dir"), "topic_context.json")
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
