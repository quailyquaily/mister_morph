package statepaths

import (
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/spf13/viper"
)

const HeartbeatChecklistFilename = "HEARTBEAT.md"

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

func MAEPDir() string {
	return pathutil.ResolveStateChildDir(
		viper.GetString("file_state_dir"),
		viper.GetString("maep.dir_name"),
		"maep",
	)
}

func HeartbeatChecklistPath() string {
	return pathutil.ResolveStateFile(viper.GetString("file_state_dir"), HeartbeatChecklistFilename)
}

func DefaultSkillsRoots() []string {
	roots := []string{
		SkillsDir(),
		pathutil.ExpandHomePath("~/.claude/skills"),
		pathutil.ExpandHomePath("~/.codex/skills"),
	}
	return dedupeNonEmptyStrings(roots)
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
