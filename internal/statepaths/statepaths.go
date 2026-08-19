package statepaths

import (
	"path/filepath"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/spf13/viper"
)

const (
	HeartbeatChecklistFilename = "HEARTBEAT.md"
	CronFilename               = "cron.yaml"
	PersonaDirName             = "persona"
	IdentityFilename           = "identity.yaml"
	SoulFilename               = "soul.md"
	AvatarFilename             = "avatar.webp"
)

func FileStateDir() string {
	return pathutil.ResolveStateDir(viper.GetString("file_state_dir"))
}

func SkillsDir() string {
	return pathutil.ResolveStateChildDir(
		viper.GetString("file_state_dir"),
		viper.GetString("skills.dir_name"),
		"skills",
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

func JournalDir() string {
	return pathutil.ResolveStateChildDir(
		viper.GetString("file_state_dir"),
		viper.GetString("journal.dir_name"),
		"journal",
	)
}

func LLMUsageJournalDir() string {
	return filepath.Clean(filepath.Join(FileStateDir(), "stats", "llm_usage"))
}

func HeartbeatChecklistPath() string {
	return pathutil.ResolveStateFile(viper.GetString("file_state_dir"), HeartbeatChecklistFilename)
}

func TopicContextPath() string {
	return pathutil.ResolveStateFile(viper.GetString("file_state_dir"), "topic_context.json")
}

func DefaultSkillsRoots() []string {
	return []string{SkillsDir()}
}
