package chatcmd

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestNewChatModel(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)

	if m.textarea.Placeholder != "Ask anything..." {
		t.Errorf("placeholder = %q, want %q", m.textarea.Placeholder, "Ask anything...")
	}
	if m.textarea.ShowLineNumbers {
		t.Error("ShowLineNumbers should be false")
	}
	if m.thinking {
		t.Error("thinking should be false initially")
	}
	if len(m.inputHistory) != 0 {
		t.Errorf("history should be empty, got %d", len(m.inputHistory))
	}
}

func TestChatModelWindowSize(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)

	// simulate a window resize
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	cm := m2.(*chatModel)

	if cm.width != 100 {
		t.Errorf("width = %d, want 100", cm.width)
	}

	promptWidth := lipgloss.Width(cm.prompt)
	expectedTW := 100 - promptWidth - 1
	if cm.textarea.Width() != expectedTW {
		t.Errorf("textarea width = %d, want %d", cm.textarea.Width(), expectedTW)
	}
}

func TestChatModelSubmit(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)

	// type something and press enter
	m.textarea.SetValue("hello world")
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cm := m2.(*chatModel)

	// should produce a tea.Println command
	if cmd == nil {
		t.Fatal("expected a command after Enter, got nil")
	}

	// textarea should be reset
	if cm.textarea.Value() != "" {
		t.Errorf("textarea value = %q, want empty after submit", cm.textarea.Value())
	}

	// input should be in history
	if len(cm.inputHistory) != 1 || cm.inputHistory[0] != "hello world" {
		t.Errorf("history = %v, want [hello world]", cm.inputHistory)
	}
}

func TestChatModelHistoryNavigation(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)
	m.inputHistory = []string{"first", "second", "third"}
	m.historyIdx = 3

	// press up twice
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = m2.(*chatModel)
	m3, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = m3.(*chatModel)
	if m.textarea.Value() != "second" {
		t.Errorf("after 2x up, value = %q, want second", m.textarea.Value())
	}

	// press down once
	m4, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = m4.(*chatModel)
	if m.textarea.Value() != "third" {
		t.Errorf("after down, value = %q, want third", m.textarea.Value())
	}

	// press down past the end
	m5, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = m5.(*chatModel)
	if m.textarea.Value() != "" {
		t.Errorf("after down past end, value = %q, want empty", m.textarea.Value())
	}
}

func TestChatModelAutocomplete(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)
	m.textarea.SetValue("/ex")

	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.textarea.Value() != "/exit" {
		t.Errorf("after tab, value = %q, want /exit", m.textarea.Value())
	}
}

func TestChatModelThinkingState(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)

	// turn on thinking with custom message
	m2, cmd := m.Update(thinkingMsg{on: true, message: "running tools..."})
	cm := m2.(*chatModel)
	if !cm.thinking {
		t.Error("thinking should be true")
	}
	if cm.thinkingMessage != "running tools..." {
		t.Errorf("thinkingMessage = %q, want running tools...", cm.thinkingMessage)
	}
	if cmd == nil {
		t.Error("expected spinner.Tick command")
	}

	// turn off thinking
	m3, _ := cm.Update(thinkingMsg{on: false})
	cm = m3.(*chatModel)
	if cm.thinking {
		t.Error("thinking should be false")
	}
	if cm.thinkingMessage != "" {
		t.Errorf("thinkingMessage should be cleared, got %q", cm.thinkingMessage)
	}
}

func TestChatModelView(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)

	view := m.View()
	// View should contain the prompt
	if !strings.Contains(view, "testuser") {
		t.Errorf("View should contain prompt, got:\n%s", view)
	}
	// View should not contain spinner when not thinking
	if strings.Contains(view, "assistant is thinking") {
		t.Error("View should not contain thinking message when not thinking")
	}

	// when thinking
	m.thinking = true
	view = m.View()
	if !strings.Contains(view, "assistant is thinking") {
		t.Errorf("View should contain thinking message, got:\n%s", view)
	}
}

func TestChatModelDynamicHeight(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)
	m.textarea.SetWidth(40)

	// empty -> height 1
	m.updateTextareaHeight()
	if m.textarea.Height() != 1 {
		t.Errorf("empty height = %d, want 1", m.textarea.Height())
	}

	// 3 lines -> height 3
	m.textarea.SetValue("line1\nline2\nline3")
	m.updateTextareaHeight()
	if m.textarea.Height() != 3 {
		t.Errorf("3-line height = %d, want 3", m.textarea.Height())
	}

	// 10 lines -> capped at maxInputHeight
	m.textarea.SetValue("1\n2\n3\n4\n5\n6\n7\n8\n9\n10")
	m.updateTextareaHeight()
	if m.textarea.Height() != maxInputHeight {
		t.Errorf("10-line height = %d, want %d", m.textarea.Height(), maxInputHeight)
	}
}

func TestChatModelQuit(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)

	_, cmd := m.Update(quitMsg{})
	if cmd == nil {
		t.Fatal("expected quit command sequence")
	}
}

func TestChatModelAgentResult(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)
	m.thinking = true

	m2, cmd := m.Update(agentResultMsg{output: "hello from agent"})
	cm := m2.(*chatModel)
	if cm.thinking {
		t.Error("thinking should be false after result")
	}
	if cmd == nil {
		t.Error("expected tea.Println command for output")
	}

	// error case
	m3, cmd2 := cm.Update(agentResultMsg{err: errTest})
	_ = m3.(*chatModel)
	if cmd2 == nil {
		t.Error("expected tea.Println command for error")
	}
}

func TestCountPasteLines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"a\n", 1},
		{"a\nb", 2},
		{"a\nb\n", 2},
		{"a\nb\nc", 3},
		{"\n\n\n", 3},
	}
	for _, c := range cases {
		if got := countPasteLines(c.in); got != c.want {
			t.Errorf("countPasteLines(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestChatModelPasteFoldsLargeBlock(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)

	pasted := "line1\nline2\nline3\nline4"
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true}
	m2, cmd := m.Update(msg)
	cm := m2.(*chatModel)

	if cmd != nil {
		t.Errorf("expected no command after paste fold, got %v", cmd)
	}
	if cm.pasteCounter != 1 {
		t.Errorf("pasteCounter = %d, want 1", cm.pasteCounter)
	}
	if got := cm.pastedTexts[1]; got != pasted {
		t.Errorf("pastedTexts[1] = %q, want %q", got, pasted)
	}

	want := "[Pasted text #1 +4 lines]"
	if got := cm.textarea.Value(); got != want {
		t.Errorf("textarea value = %q, want %q", got, want)
	}
}

func TestChatModelPasteShortInline(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)

	// 2 lines is below the threshold — should be inserted verbatim.
	pasted := "one\ntwo"
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true}
	m2, _ := m.Update(msg)
	cm := m2.(*chatModel)

	if cm.pasteCounter != 0 {
		t.Errorf("short paste should not bump counter, got %d", cm.pasteCounter)
	}
	if cm.textarea.Value() != pasted {
		t.Errorf("textarea value = %q, want %q", cm.textarea.Value(), pasted)
	}
}

func TestChatModelPasteSubmitExpands(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)

	pasted := "alpha\nbeta\ngamma\ndelta"
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})

	// Prepend a label so we can verify mixed text + placeholder submission.
	m.textarea.SetValue("look: " + m.textarea.Value())

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case sent := <-m.submitted:
		want := "look: alpha\nbeta\ngamma\ndelta"
		if sent != want {
			t.Errorf("submitted = %q, want %q", sent, want)
		}
	default:
		t.Fatal("expected a value on submitted channel")
	}

	// History stores the placeholder version, not the expanded text.
	if len(m.inputHistory) != 1 {
		t.Fatalf("inputHistory len = %d, want 1", len(m.inputHistory))
	}
	if !strings.Contains(m.inputHistory[0], "[Pasted text #1 +4 lines]") {
		t.Errorf("history[0] = %q, want placeholder", m.inputHistory[0])
	}
}

func TestExpandPastePlaceholdersUnknownID(t *testing.T) {
	sess := &chatSession{compactMode: false, userName: "testuser"}
	m := newChatModel(sess)
	m.pastedTexts[2] = "the real text"

	// id 99 is unknown — left as literal; id 2 is expanded.
	in := "see [Pasted text #99 +5 lines] and [Pasted text #2 +3 lines]"
	out := m.expandPastePlaceholders(in)
	want := "see [Pasted text #99 +5 lines] and the real text"
	if out != want {
		t.Errorf("expand = %q, want %q", out, want)
	}
}

var errTest = tea.ErrProgramKilled

