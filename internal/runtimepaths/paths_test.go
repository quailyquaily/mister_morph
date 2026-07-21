package runtimepaths

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/viper"
)

func TestFromReaderResolvesRuntimeStatePathsOnce(t *testing.T) {
	stateDir := t.TempDir()
	cacheDir := t.TempDir()
	auditPath := filepath.Join(t.TempDir(), "audit", "events.jsonl")
	logDir := filepath.Join(t.TempDir(), "logs")
	reader := viper.New()
	reader.Set("file_state_dir", stateDir)
	reader.Set("file_cache_dir", cacheDir)
	reader.Set("journal.dir_name", "domain-journal")
	reader.Set("memory.dir_name", "agent-memory")
	reader.Set("contacts.dir_name", "address-book")
	reader.Set("tasks.dir_name", "task-state")
	reader.Set("guard.dir_name", "policy")
	reader.Set("guard.audit.jsonl_path", auditPath)
	reader.Set("logging.file.dir", logDir)

	got := FromReader(reader)
	want := Paths{
		StateDir:                 stateDir,
		CacheDir:                 cacheDir,
		JournalDir:               filepath.Join(stateDir, "domain-journal"),
		MemoryDir:                filepath.Join(stateDir, "agent-memory"),
		ContactsDir:              filepath.Join(stateDir, "address-book"),
		TasksDir:                 filepath.Join(stateDir, "task-state"),
		WorkspaceAttachmentsPath: filepath.Join(stateDir, "workspace_attachments.json"),
		CheckpointRoot:           stateDir,
		PersonaDir:               filepath.Join(stateDir, "persona"),
		HeartbeatPath:            filepath.Join(stateDir, "HEARTBEAT.md"),
		CronPath:                 filepath.Join(stateDir, "cron.yaml"),
		AuditPath:                auditPath,
		LogDir:                   logDir,
		LLMUsageJournalDir:       filepath.Join(stateDir, "stats", "llm_usage"),
		LLMUsageProjectionPath:   filepath.Join(stateDir, "stats", "llm_usage_projection.json"),
		TopicContextPath:         filepath.Join(stateDir, "topic_context.json"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FromReader() = %#v, want %#v", got, want)
	}
	if got.TaskTargetDir("telegram") != filepath.Join(stateDir, "task-state", "telegram") {
		t.Fatalf("TaskTargetDir(telegram) = %q", got.TaskTargetDir("telegram"))
	}
}
