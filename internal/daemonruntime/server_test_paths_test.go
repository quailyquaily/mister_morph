package daemonruntime

import (
	"path/filepath"

	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
)

func testRuntimePaths(stateDir string) runtimepaths.Paths {
	cacheDir := filepath.Join(stateDir, "cache")
	journalDir := filepath.Join(stateDir, "journal")
	memoryDir := filepath.Join(stateDir, "memory")
	contactsDir := filepath.Join(stateDir, "contacts")
	tasksDir := filepath.Join(stateDir, "tasks")
	personaDir := filepath.Join(stateDir, statepaths.PersonaDirName)
	statsDir := filepath.Join(stateDir, "stats")
	return runtimepaths.Paths{
		StateDir:                 stateDir,
		CacheDir:                 cacheDir,
		JournalDir:               journalDir,
		MemoryDir:                memoryDir,
		ContactsDir:              contactsDir,
		TasksDir:                 tasksDir,
		WorkspaceAttachmentsPath: filepath.Join(stateDir, "workspace_attachments.json"),
		CheckpointRoot:           stateDir,
		PersonaDir:               personaDir,
		HeartbeatPath:            filepath.Join(stateDir, statepaths.HeartbeatChecklistFilename),
		CronPath:                 filepath.Join(stateDir, statepaths.CronFilename),
		AuditPath:                filepath.Join(stateDir, "guard", "audit", "guard_audit.jsonl"),
		LogDir:                   filepath.Join(stateDir, "logs"),
		LLMUsageJournalDir:       filepath.Join(statsDir, "llm_usage"),
		LLMUsageProjectionPath:   filepath.Join(statsDir, "llm_usage_projection.json"),
		TopicContextPath:         filepath.Join(stateDir, "topic_context.json"),
	}
}
