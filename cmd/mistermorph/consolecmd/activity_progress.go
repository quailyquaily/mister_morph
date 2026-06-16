package consolecmd

import (
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
)

const (
	consoleActivityHistoryLimit   = 24
	consoleActivityOutputMaxChars = 6000
)

type consoleActivityProgress struct {
	Current *consoleActivityEntry  `json:"current,omitempty"`
	History []consoleActivityEntry `json:"history,omitempty"`
}

type consoleActivityEntry struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Name       string         `json:"name,omitempty"`
	Status     string         `json:"status,omitempty"`
	At         string         `json:"at,omitempty"`
	Stream     string         `json:"stream,omitempty"`
	Output     string         `json:"output,omitempty"`
	Args       map[string]any `json:"args,omitempty"`
	Summary    string         `json:"summary,omitempty"`
	Error      string         `json:"error,omitempty"`
	TaskID     string         `json:"task_id,omitempty"`
	Mode       string         `json:"mode,omitempty"`
	Profile    string         `json:"profile,omitempty"`
	OutputKind string         `json:"output_kind,omitempty"`
}

func cloneConsoleActivityProgress(progress *consoleActivityProgress) *consoleActivityProgress {
	if progress == nil {
		return nil
	}
	out := &consoleActivityProgress{
		Current: cloneConsoleActivityEntry(progress.Current),
		History: make([]consoleActivityEntry, 0, len(progress.History)),
	}
	for _, entry := range progress.History {
		cloned := cloneConsoleActivityEntry(&entry)
		if cloned == nil {
			continue
		}
		out.History = append(out.History, *cloned)
	}
	if out.Current == nil && len(out.History) > 0 {
		last := out.History[len(out.History)-1]
		out.Current = cloneConsoleActivityEntry(&last)
	}
	if out.Current == nil && len(out.History) == 0 {
		return nil
	}
	return out
}

func cloneConsoleActivityEntry(entry *consoleActivityEntry) *consoleActivityEntry {
	if entry == nil {
		return nil
	}
	out := *entry
	if len(entry.Args) > 0 {
		out.Args = cloneConsoleArgs(entry.Args)
	}
	return &out
}

func cloneConsoleArgs(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]any, len(args))
	for key, value := range args {
		out[key] = value
	}
	return out
}

func updateConsoleActivityProgress(progress *consoleActivityProgress, event agent.Event) (*consoleActivityProgress, bool) {
	if strings.TrimSpace(event.Kind) == agent.EventKindToolOutput {
		return updateConsoleActivityOutput(progress, event)
	}

	entry := buildConsoleActivityEntry(event)
	if entry == nil {
		return cloneConsoleActivityProgress(progress), false
	}
	if progress == nil {
		progress = &consoleActivityProgress{}
	}

	index := -1
	for i := range progress.History {
		if progress.History[i].ID == entry.ID {
			index = i
			break
		}
	}
	if index >= 0 {
		progress.History[index] = mergeConsoleActivityEntry(progress.History[index], *entry)
		progress.Current = cloneConsoleActivityEntry(&progress.History[index])
		return cloneConsoleActivityProgress(progress), true
	}

	progress.History = append(progress.History, *entry)
	if len(progress.History) > consoleActivityHistoryLimit {
		progress.History = append([]consoleActivityEntry(nil), progress.History[len(progress.History)-consoleActivityHistoryLimit:]...)
	}
	last := progress.History[len(progress.History)-1]
	progress.Current = cloneConsoleActivityEntry(&last)
	return cloneConsoleActivityProgress(progress), true
}

func updateConsoleActivityOutput(progress *consoleActivityProgress, event agent.Event) (*consoleActivityProgress, bool) {
	if progress == nil || event.Text == "" || strings.TrimSpace(event.ToolName) != "coder" {
		return cloneConsoleActivityProgress(progress), false
	}
	index := consoleActivityOutputIndex(progress, event)
	if index < 0 {
		return cloneConsoleActivityProgress(progress), false
	}

	entry := progress.History[index]
	if stream := strings.TrimSpace(event.Stream); stream != "" {
		entry.Stream = stream
	}
	if status := strings.TrimSpace(event.Status); status != "" {
		entry.Status = status
	}
	entry.At = time.Now().Format(time.RFC3339Nano)
	entry.Output = appendConsoleTail(entry.Output, event.Text, consoleActivityOutputMaxChars)
	progress.History[index] = entry
	progress.Current = cloneConsoleActivityEntry(&entry)
	return cloneConsoleActivityProgress(progress), true
}

func consoleActivityOutputIndex(progress *consoleActivityProgress, event agent.Event) int {
	if progress == nil {
		return -1
	}
	if id := strings.TrimSpace(event.ActivityID); id != "" {
		for i := range progress.History {
			if progress.History[i].ID == id {
				return i
			}
		}
	}

	toolName := strings.TrimSpace(event.ToolName)
	for i := len(progress.History) - 1; i >= 0; i-- {
		entry := progress.History[i]
		if strings.TrimSpace(entry.Kind) != "tool" {
			continue
		}
		if toolName != "" && strings.TrimSpace(entry.Name) != toolName {
			continue
		}
		switch strings.TrimSpace(entry.Status) {
		case "done", "failed", "canceled":
			continue
		default:
			return i
		}
	}
	return -1
}

func buildConsoleActivityEntry(event agent.Event) *consoleActivityEntry {
	id := strings.TrimSpace(event.ActivityID)
	if id == "" {
		id = strings.TrimSpace(event.TaskID)
	}
	if id == "" {
		return nil
	}

	entry := &consoleActivityEntry{
		ID:         id,
		Status:     strings.TrimSpace(event.Status),
		At:         time.Now().Format(time.RFC3339Nano),
		Summary:    strings.TrimSpace(event.Summary),
		Error:      strings.TrimSpace(event.Error),
		TaskID:     strings.TrimSpace(event.TaskID),
		Mode:       strings.TrimSpace(event.Mode),
		Profile:    strings.TrimSpace(event.Profile),
		OutputKind: strings.TrimSpace(event.OutputKind),
		Args:       cloneConsoleArgs(event.Args),
	}

	switch strings.TrimSpace(event.Kind) {
	case agent.EventKindToolStart, agent.EventKindToolDone:
		entry.Kind = "tool"
		entry.Name = strings.TrimSpace(event.ToolName)
	case agent.EventKindSubtaskStart, agent.EventKindSubtaskDone:
		entry.Kind = "subtask"
		entry.Name = strings.TrimSpace(event.TaskID)
	default:
		return nil
	}

	if entry.Kind == "tool" && entry.Name == "" {
		entry.Name = "tool"
	}
	if entry.Kind == "subtask" && entry.Name == "" {
		entry.Name = "subtask"
	}
	return entry
}

func mergeConsoleActivityEntry(base consoleActivityEntry, update consoleActivityEntry) consoleActivityEntry {
	if strings.TrimSpace(update.Kind) != "" {
		base.Kind = strings.TrimSpace(update.Kind)
	}
	if strings.TrimSpace(update.Name) != "" {
		base.Name = strings.TrimSpace(update.Name)
	}
	if strings.TrimSpace(update.Status) != "" {
		base.Status = strings.TrimSpace(update.Status)
	}
	if strings.TrimSpace(update.At) != "" {
		base.At = strings.TrimSpace(update.At)
	}
	if strings.TrimSpace(update.Stream) != "" {
		base.Stream = strings.TrimSpace(update.Stream)
	}
	if update.Output != "" {
		base.Output = update.Output
	}
	if len(update.Args) > 0 {
		base.Args = cloneConsoleArgs(update.Args)
	}
	if strings.TrimSpace(update.Summary) != "" {
		base.Summary = strings.TrimSpace(update.Summary)
	}
	if strings.TrimSpace(update.Error) != "" {
		base.Error = strings.TrimSpace(update.Error)
	}
	if strings.TrimSpace(update.TaskID) != "" {
		base.TaskID = strings.TrimSpace(update.TaskID)
	}
	if strings.TrimSpace(update.Mode) != "" {
		base.Mode = strings.TrimSpace(update.Mode)
	}
	if strings.TrimSpace(update.Profile) != "" {
		base.Profile = strings.TrimSpace(update.Profile)
	}
	if strings.TrimSpace(update.OutputKind) != "" {
		base.OutputKind = strings.TrimSpace(update.OutputKind)
	}
	return base
}
