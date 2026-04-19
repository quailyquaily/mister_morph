package chatcmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/memory"
)

func buildTurnSummary(userInput, assistantOutput string, steps []agent.Step) string {
	userInput = strings.TrimSpace(userInput)
	if userInput == "" {
		return ""
	}

	lower := strings.ToLower(userInput)
	if strings.HasPrefix(lower, "/remember") || strings.HasPrefix(lower, "/memory") {
		return ""
	}

	var toolNames []string
	for _, step := range steps {
		if step.Action != "" {
			toolNames = append(toolNames, step.Action)
		}
	}

	if len(toolNames) == 0 {
		return ""
	}

	summary := userInput
	if len(toolNames) > 0 {
		summary += fmt.Sprintf(" (tools: %s)", strings.Join(toolNames, ", "))
	}

	const maxLen = 200
	if len(summary) > maxLen {
		summary = summary[:maxLen-3] + "..."
	}
	return summary
}

func cliMemorySubjectID(cwd string) string {
	h := sha256.Sum256([]byte(cwd))
	return "cli_" + hex.EncodeToString(h[:])[:16]
}

func initChatMemoryRuntime(cwd string, logger *slog.Logger) (*memory.Manager, *memoryruntime.Orchestrator, *memoryruntime.ProjectionWorker, func(), error) {
	mgr := memory.NewManager(statepaths.MemoryDir(), 7)
	journal := mgr.NewJournal(memory.JournalOptions{})

	projector := memory.NewProjector(mgr, journal, memory.ProjectorOptions{
		DraftResolver: memoryruntime.NewDraftResolver(nil, ""),
	})

	orchestrator, err := memoryruntime.New(mgr, journal, projector, memoryruntime.OrchestratorOptions{})
	if err != nil {
		_ = journal.Close()
		return nil, nil, nil, nil, err
	}

	projectionWorker, err := memoryruntime.NewProjectionWorker(journal, projector, memoryruntime.ProjectionWorkerOptions{
		Logger: logger,
	})
	if err != nil {
		_ = journal.Close()
		return nil, nil, nil, nil, err
	}

	cleanup := func() {
		_ = journal.Close()
	}

	return mgr, orchestrator, projectionWorker, cleanup, nil
}

func autoUpdateMemory(
	writer io.Writer,
	logger *slog.Logger,
	memOrchestrator *memoryruntime.Orchestrator,
	memWorker *memoryruntime.ProjectionWorker,
	subjectID string,
	runID string,
	input, output string,
	steps []agent.Step,
) {
	if len(steps) == 0 || memOrchestrator == nil {
		return
	}
	summary := buildTurnSummary(input, output, steps)
	if summary == "" {
		return
	}
	_, recErr := memOrchestrator.Record(memoryruntime.RecordRequest{
		TaskRunID:   runID,
		SessionID:   subjectID,
		SubjectID:   subjectID,
		Channel:     "cli",
		TaskText:    input,
		FinalOutput: summary,
		SessionContext: memory.SessionContext{
			ConversationID: subjectID,
		},
	})
	if recErr != nil {
		logger.Warn("chat_memory_record_failed", "error", recErr.Error())
	} else {
		if memWorker != nil {
			memWorker.NotifyRecordAppended()
		}
		logger.Debug("chat_memory_recorded", "summary", summary)
	}
}

func handleRemember(
	writer io.Writer,
	input string,
	mgr *memory.Manager,
	subjectID string,
) {
	entry := input[len("/remember "):]
	if entry == "" {
		_, _ = fmt.Fprintln(writer, "Usage: /remember <content>")
		return
	}
	if mgr == nil {
		_, _ = fmt.Fprintln(writer, "Memory system not available.")
		return
	}
	updated, err := mgr.UpdateLongTerm(subjectID, memory.PromoteDraft{
		GoalsProjects: []string{entry},
	})
	if err != nil {
		_, _ = fmt.Fprintf(writer, "error saving long-term memory: %v\n", err)
		return
	}
	if !updated {
		_, _ = fmt.Fprintln(writer, "No long-term memory added.")
		return
	}
	_, _ = fmt.Fprintln(writer, "Remembered.")
}

func handleMemory(
	writer io.Writer,
	memOrchestrator *memoryruntime.Orchestrator,
	subjectID string,
) {
	if memOrchestrator == nil {
		_, _ = fmt.Fprintln(writer, "No project memory yet.")
		return
	}
	memCtx, memErr := memOrchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{
		SubjectID:      subjectID,
		RequestContext: memory.ContextPrivate,
		MaxItems:       50,
	})
	if memErr != nil {
		_, _ = fmt.Fprintf(writer, "Error loading memory: %v\n", memErr)
		return
	}
	if strings.TrimSpace(memCtx) == "" {
		_, _ = fmt.Fprintln(writer, "No project memory yet.")
		return
	}
	_, _ = fmt.Fprintln(writer, "\n--- Project Memory ---")
	_, _ = fmt.Fprintln(writer, memCtx)
	_, _ = fmt.Fprintln(writer, "----------------------")
}

func prepareTurnMemoryContext(memOrchestrator *memoryruntime.Orchestrator, subjectID string) (string, error) {
	if memOrchestrator == nil || strings.TrimSpace(subjectID) == "" {
		return "", nil
	}
	return memOrchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{
		SubjectID:      subjectID,
		RequestContext: memory.ContextPrivate,
		MaxItems:       20,
	})
}
