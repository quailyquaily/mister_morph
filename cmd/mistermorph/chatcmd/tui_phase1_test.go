package chatcmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/llm"
)

func newPhase1TestSession(t *testing.T) *chatSession {
	t.Helper()
	store := topiccontext.NewStore(filepath.Join(t.TempDir(), "topic_context.json"))
	sess := &chatSession{
		projectID:         "cli_tui",
		mainCfg:           llmconfigForTUITest(),
		workspaceDir:      filepath.Join(t.TempDir(), "mistermorph"),
		fileStateDir:      filepath.Join(t.TempDir(), "state"),
		fileCacheDir:      filepath.Join(t.TempDir(), "cache"),
		version:           "v1.2.3",
		topicContextStore: store,
		sessionStore:      llmselect.NewStore(),
	}
	if err := store.UpdateFromSample(topiccontext.Scope{
		Runtime:         "chat",
		ConversationKey: sess.conversationKey(),
		TopicID:         sess.projectID,
	}, topiccontext.UsageSample{
		Model:               sess.mainCfg.Model,
		ContextWindowTokens: 1000,
		InputTokens:         180,
		UpdatedAt:           time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("UpdateFromSample() error = %v", err)
	}
	return sess
}

func llmconfigForTUITest() llmconfig.ClientConfig {
	return llmconfig.ClientConfig{Model: "gpt-5.2"}
}

func TestChatModelIdleLayoutShowsSessionContext(t *testing.T) {
	m := newChatModel(newPhase1TestSession(t))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	view := m.View()
	for _, want := range []string{"❯ ", "gpt-5.2", "mistermorph", "ctx 18%", "Ctrl+J newline", "/ commands"} {
		if !strings.Contains(view, want) {
			t.Fatalf("idle View() missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "testuser>") {
		t.Fatalf("idle View() still contains the old username prompt:\n%s", view)
	}
}

func TestChatModelRunningLayoutExplainsInputSemantics(t *testing.T) {
	m := newChatModel(newPhase1TestSession(t))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(thinkingMsg{on: true, message: "bash · go test ./..."})

	view := m.View()
	for _, want := range []string{"Running", "bash · go test ./...", "Enter steer", "Ctrl+C stop"} {
		if !strings.Contains(view, want) {
			t.Fatalf("running View() missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "assistant is thinking") {
		t.Fatalf("running View() contains the old generic spinner copy:\n%s", view)
	}
}

func TestChatModelBottomRowsFitNarrowTerminal(t *testing.T) {
	m := newChatModel(newPhase1TestSession(t))
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m.Update(thinkingMsg{on: true, message: "bash · a-command-with-a-very-long-target-name"})

	for _, line := range strings.Split(m.View(), "\n") {
		if width := lipgloss.Width(line); width > 39 {
			t.Fatalf("line width = %d, want <= 39: %q", width, line)
		}
	}

	m.textarea.SetValue("1\n2\n3\n4\n5")
	m.updateTextareaHeight()
	if got := m.textarea.Height(); got != 3 {
		t.Fatalf("small-terminal textarea height = %d, want 3", got)
	}
}

func TestChatModelMultilineArrowsMoveCursorBeforeHistory(t *testing.T) {
	m := newChatModel(newPhase1TestSession(t))
	m.inputHistory = []string{"older"}
	m.historyIdx = len(m.inputHistory)
	m.textarea.SetValue("first\nsecond")

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.textarea.Value(); got != "first\nsecond" {
		t.Fatalf("Up inside multiline changed value to history entry %q", got)
	}
	if got := m.textarea.Line(); got != 0 {
		t.Fatalf("textarea line after Up = %d, want 0", got)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.textarea.Value(); got != "older" {
		t.Fatalf("Up at top boundary value = %q, want older history", got)
	}
}

func TestChatModelCtrlJInsertsNewline(t *testing.T) {
	m := newChatModel(newPhase1TestSession(t))
	m.textarea.SetValue("first")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if got := m.textarea.Value(); got != "first\n" {
		t.Fatalf("Ctrl+J value = %q, want newline", got)
	}
}

func TestChatModelAltEnterInsertsNewline(t *testing.T) {
	m := newChatModel(newPhase1TestSession(t))
	m.textarea.SetValue("first")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if got := m.textarea.Value(); got != "first\n" {
		t.Fatalf("Alt+Enter value = %q, want newline", got)
	}
}

func TestChatModelCtrlCClearsDraftBeforeExit(t *testing.T) {
	m := newChatModel(newPhase1TestSession(t))
	m.textarea.SetValue("draft")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		t.Fatalf("Ctrl+C with draft returned quit command")
	}
	if got := m.textarea.Value(); got != "" {
		t.Fatalf("Ctrl+C with draft left value %q", got)
	}
}

func TestChatModelEmptyCtrlCRequiresConfirmation(t *testing.T) {
	m := newChatModel(newPhase1TestSession(t))

	_, first := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if first == nil {
		t.Fatal("first empty Ctrl+C should schedule confirmation expiry")
	}
	if view := m.View(); !strings.Contains(view, "Ctrl+C again to exit") {
		t.Fatalf("first empty Ctrl+C did not show confirmation:\n%s", view)
	}

	_, second := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if second == nil {
		t.Fatal("second empty Ctrl+C should quit")
	}
	if _, ok := second().(tea.QuitMsg); !ok {
		t.Fatalf("second empty Ctrl+C command did not return tea.QuitMsg")
	}
}

func TestChatModelEmptyCtrlDExits(t *testing.T) {
	m := newChatModel(newPhase1TestSession(t))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd == nil {
		t.Fatal("empty Ctrl+D should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("empty Ctrl+D command did not return tea.QuitMsg")
	}
}

func TestPrintChatSessionHeaderKeepsBannerAndSessionMetadata(t *testing.T) {
	var out bytes.Buffer
	printChatSessionHeader(&out, false, "openai", "gpt-5.2", "/work/mistermorph", "v1.2.3")

	got := out.String()
	if !strings.Contains(got, "▄▄") {
		t.Fatalf("header is missing the Morph banner:\n%s", got)
	}
	if strings.Contains(got, "workspace_dir=") {
		t.Fatalf("header contains the old key/value block:\n%s", got)
	}
	for _, want := range []string{"openai", "gpt-5.2", "mistermorph", "version v1.2.3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("header missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "MisterMorph") {
		t.Fatalf("header metadata contains the product name: %q", got)
	}
	var nonEmpty []string
	for _, line := range strings.Split(got, "\n") {
		if strings.TrimSpace(line) != "" {
			nonEmpty = append(nonEmpty, line)
		}
	}
	if len(nonEmpty) != 4 {
		t.Fatalf("header has %d non-empty lines, want 3 logo lines and 1 metadata line:\n%s", len(nonEmpty), got)
	}
	metadata := nonEmpty[3]
	for _, want := range []string{"openai / gpt-5.2", "mistermorph", "version v1.2.3"} {
		if !strings.Contains(metadata, want) {
			t.Fatalf("metadata line missing %q: %q", want, metadata)
		}
	}
}

func TestChatStatusCommandShowsFullSessionDetails(t *testing.T) {
	sess := newPhase1TestSession(t)
	history := make([]llm.Message, 0)
	boundaries := make([]string, 0)
	reg := newChatRuntimeCommandRegistry(sess)
	registerChatCommands(reg, sess, &history, &boundaries)

	result, handled, err := reg.Dispatch(context.Background(), "/status")
	if err != nil {
		t.Fatalf("/status error = %v", err)
	}
	if !handled || result == nil {
		t.Fatalf("/status handled/result = %v/%#v", handled, result)
	}
	for _, want := range []string{
		"Model: gpt-5.2",
		"Workspace: " + sess.workspaceDir,
		"File state: " + sess.fileStateDir,
		"File cache: " + sess.fileCacheDir,
		"Context: 18.0%",
		"Version: v1.2.3",
	} {
		if !strings.Contains(result.Reply, want) {
			t.Fatalf("/status reply missing %q:\n%s", want, result.Reply)
		}
	}
}
