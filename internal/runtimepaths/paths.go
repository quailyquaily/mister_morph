package runtimepaths

import (
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
)

type Paths struct {
	StateDir                 string
	CacheDir                 string
	JournalDir               string
	ContactsDir              string
	TasksDir                 string
	WorkspaceAttachmentsPath string
	CheckpointRoot           string
	PersonaDir               string
	HeartbeatPath            string
	CronPath                 string
	AuditPath                string
	LogDir                   string
	LLMUsageJournalDir       string
	LLMUsageProjectionPath   string
	TopicContextPath         string
}

type Reader interface {
	GetString(string) string
}

func FromReader(reader Reader) Paths {
	get := func(key string) string {
		if reader == nil {
			return ""
		}
		return strings.TrimSpace(reader.GetString(key))
	}
	stateDir := pathutil.ResolveStateDir(get("file_state_dir"))
	cacheDir := get("file_cache_dir")
	if cacheDir == "" {
		cacheDir = "~/.cache/morph"
	}
	cacheDir = pathutil.ExpandHomePath(cacheDir)
	journalDir := pathutil.ResolveStateChildDir(stateDir, get("journal.dir_name"), "journal")
	contactsDir := pathutil.ResolveStateChildDir(stateDir, get("contacts.dir_name"), "contacts")
	tasksDir := pathutil.ResolveStateChildDir(stateDir, get("tasks.dir_name"), "tasks")
	personaDir := filepath.Join(stateDir, statepaths.PersonaDirName)
	auditPath := pathutil.ExpandHomePath(get("guard.audit.jsonl_path"))
	if auditPath == "" {
		guardDir := pathutil.ResolveStateChildDir(stateDir, get("guard.dir_name"), "guard")
		auditPath = filepath.Join(guardDir, "audit", "guard_audit.jsonl")
	}
	statsDir := filepath.Join(stateDir, "stats")
	return Paths{
		StateDir:                 stateDir,
		CacheDir:                 cacheDir,
		JournalDir:               journalDir,
		ContactsDir:              contactsDir,
		TasksDir:                 tasksDir,
		WorkspaceAttachmentsPath: filepath.Join(stateDir, "workspace_attachments.json"),
		CheckpointRoot:           stateDir,
		PersonaDir:               personaDir,
		HeartbeatPath:            filepath.Join(stateDir, statepaths.HeartbeatChecklistFilename),
		CronPath:                 filepath.Join(stateDir, statepaths.CronFilename),
		AuditPath:                auditPath,
		LogDir:                   logutil.ResolveFileLogDir(stateDir, get("logging.file.dir")),
		LLMUsageJournalDir:       filepath.Join(statsDir, "llm_usage"),
		LLMUsageProjectionPath:   filepath.Join(statsDir, "llm_usage_projection.json"),
		TopicContextPath:         filepath.Join(stateDir, "topic_context.json"),
	}
}

func (p Paths) TaskTargetDir(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		target = "tasks"
	}
	return filepath.Join(p.TasksDir, target)
}
