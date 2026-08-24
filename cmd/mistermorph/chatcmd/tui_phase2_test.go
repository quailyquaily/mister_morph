package chatcmd

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
)

func phase2ApprovalRecord() guard.ApprovalRecord {
	return guard.ApprovalRecord{
		ID:       "apr_tui",
		ToolName: "bash",
		Reasons:  []string{"writes to a remote repository", "uses network access"},
		ResumeState: []byte(`{
			"v": 1,
			"pending_tool": {
				"tool_call": {
					"tool_name": "bash",
					"tool_params": {
						"cmd": "git push origin feature/chat-tui",
						"timeout": 30
					}
				}
			}
		}`),
	}
}

func TestChatModelApprovalReplacesComposerAndRestoresDraft(t *testing.T) {
	m := newChatModel(newPhase1TestSession(t))
	m.textarea.SetValue("keep this draft")
	m.Update(approvalMsg{record: phase2ApprovalRecord()})

	view := m.View()
	for _, want := range []string{
		"! Approval · bash",
		"$ git push origin feature/chat-tui",
		"writes to a remote repository",
		"uses network access",
		"cmd",
		"timeout",
		"30",
		"y approve",
		"n deny",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("approval View() missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "keep this draft") {
		t.Fatalf("approval View() still renders composer draft:\n%s", view)
	}

	m.Update(approvalClearedMsg{})
	if got := m.textarea.Value(); got != "keep this draft" {
		t.Fatalf("draft after approval = %q, want preserved value", got)
	}
}

func TestChatModelApprovalAcceptsOnlyApprovalKeys(t *testing.T) {
	m := newChatModel(newPhase1TestSession(t))
	m.textarea.SetValue("draft")
	m.Update(approvalMsg{record: phase2ApprovalRecord()})

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if got := m.textarea.Value(); got != "draft" {
		t.Fatalf("ordinary key changed approval draft to %q", got)
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd != nil {
		t.Fatal("approval key should not print a user transcript line")
	}
	select {
	case got := <-m.submitted:
		if got != "y" {
			t.Fatalf("approval submission = %q, want y", got)
		}
	case <-time.After(time.Second):
		t.Fatal("approval key was not submitted")
	}
}

func TestChatModelApprovalSubmitsOnlyFirstDecision(t *testing.T) {
	m := newChatModel(newPhase1TestSession(t))
	m.Update(approvalMsg{record: phase2ApprovalRecord()})

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	select {
	case got := <-m.submitted:
		if got != "y" {
			t.Fatalf("first approval submission = %q, want y", got)
		}
	case <-time.After(time.Second):
		t.Fatal("first approval decision was not submitted")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	select {
	case got := <-m.submitted:
		t.Fatalf("second approval decision was submitted: %q", got)
	case <-time.After(25 * time.Millisecond):
	}

	// A transient resolution error leaves the same approval pending and must
	// allow the user to retry without replacing the approval view.
	m.Update(agentResultMsg{err: errTest})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	select {
	case got := <-m.submitted:
		if got != "n" {
			t.Fatalf("retried approval submission = %q, want n", got)
		}
	case <-time.After(time.Second):
		t.Fatal("pending approval did not accept a retry after an error")
	}
}

func TestChatModelApprovalScrollsWithinTerminalHeight(t *testing.T) {
	record := phase2ApprovalRecord()
	record.ResumeState = []byte(`{
		"v": 1,
		"pending_tool": {
			"tool_call": {
				"tool_name": "bash",
				"tool_params": {
					"cmd": "line 01\nline 02\nline 03\nline 04\nline 05\nline 06\nline 07\nline 08\nline 09\nline 10\nline 11\nline 12"
				}
			}
		}
	}`)
	m := newChatModel(newPhase1TestSession(t))
	m.Update(tea.WindowSizeMsg{Width: 48, Height: 9})
	m.Update(approvalMsg{record: record})

	initial := m.View()
	if lines := strings.Count(initial, "\n") + 1; lines > 9 {
		t.Fatalf("approval View() uses %d rows in a 9-row terminal:\n%s", lines, initial)
	}
	for _, want := range []string{"! Approval · bash", "$ line 01", "y approve", "n deny"} {
		if !strings.Contains(initial, want) {
			t.Fatalf("initial approval View() missing %q:\n%s", want, initial)
		}
	}
	if strings.Contains(initial, "line 12") {
		t.Fatalf("initial approval View() unexpectedly shows the final command line:\n%s", initial)
	}

	for range 32 {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	scrolled := m.View()
	if lines := strings.Count(scrolled, "\n") + 1; lines > 9 {
		t.Fatalf("scrolled approval View() uses %d rows in a 9-row terminal:\n%s", lines, scrolled)
	}
	for _, want := range []string{"! Approval · bash", "line 12", "y approve", "n deny"} {
		if !strings.Contains(scrolled, want) {
			t.Fatalf("scrolled approval View() missing %q:\n%s", want, scrolled)
		}
	}
}

func TestChatModelApprovalEscapesTerminalControlCharacters(t *testing.T) {
	record := phase2ApprovalRecord()
	record.Reasons = []string{"review \x1b]0;untrusted\x07 command"}
	record.ResumeState = []byte(strings.Replace(
		string(record.ResumeState),
		"git push origin feature/chat-tui",
		`\u001b[2Jecho safe`,
		1,
	))
	m := newChatModel(newPhase1TestSession(t))
	m.Update(approvalMsg{record: record})

	view := m.View()
	for _, unsafe := range []string{"\x1b[2J", "\x1b]0;untrusted\x07"} {
		if strings.Contains(view, unsafe) {
			t.Fatalf("approval View() contains executable terminal control %q: %q", unsafe, view)
		}
	}
	for _, visible := range []string{`\x1b[2Jecho safe`, `\x1b]0;untrusted\x07`} {
		if !strings.Contains(view, visible) {
			t.Fatalf("approval View() missing visible control sequence %q: %q", visible, view)
		}
	}

	outcome := formatChatApprovalOutcome("Approved", record)
	if strings.Contains(outcome, "\x1b[2J") || !strings.Contains(outcome, `\x1b[2Jecho safe`) {
		t.Fatalf("approval outcome did not escape terminal control characters: %q", outcome)
	}
}

func TestChatModelCommandPickerUsesRegistryMetadata(t *testing.T) {
	reg := chatcommands.NewRegistry()
	reg.Register("/workspace", "show or change workspace", nil)
	reg.Register("/status", "show session details", nil)
	m := newChatModel(newPhase1TestSession(t))
	m.commandRegistry = reg
	m.textarea.SetValue("/wo")
	m.textarea.CursorEnd()

	view := m.View()
	if !strings.Contains(view, "/workspace") || !strings.Contains(view, "show or change workspace") {
		t.Fatalf("picker does not render registry metadata:\n%s", view)
	}
	if strings.Contains(view, "/status") {
		t.Fatalf("picker renders a non-matching command:\n%s", view)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.textarea.Value(); got != "/workspace" {
		t.Fatalf("Tab completion = %q, want /workspace", got)
	}
}

func TestChatModelCommandPickerCanCloseWithoutLosingDraft(t *testing.T) {
	reg := chatcommands.NewRegistry()
	reg.Register("/workspace", "show or change workspace", nil)
	m := newChatModel(newPhase1TestSession(t))
	m.commandRegistry = reg
	m.textarea.SetValue("/wo")

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := m.textarea.Value(); got != "/wo" {
		t.Fatalf("Esc changed picker draft to %q", got)
	}
	if view := m.View(); strings.Contains(view, "show or change workspace") {
		t.Fatalf("Esc left picker open:\n%s", view)
	}
}

func TestChatModelCommandPickerLimitsNarrowTerminalToThreeItems(t *testing.T) {
	reg := chatcommands.NewRegistry()
	for _, name := range []string{"/alpha", "/beta", "/charlie", "/delta"} {
		reg.Register(name, "description for "+name, nil)
	}
	m := newChatModel(newPhase1TestSession(t))
	m.commandRegistry = reg
	m.textarea.SetValue("/")
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})

	view := m.View()
	for _, want := range []string{"/alpha", "/beta", "/charlie"} {
		if !strings.Contains(view, want) {
			t.Fatalf("narrow picker missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "/delta") {
		t.Fatalf("narrow picker rendered more than three commands:\n%s", view)
	}
}

func TestChatModelCommandPickerFitsRemainingTerminalHeight(t *testing.T) {
	reg := chatcommands.NewRegistry()
	for _, name := range []string{"/alpha", "/beta", "/charlie", "/delta"} {
		reg.Register(name, "description for "+name, nil)
	}
	m := newChatModel(newPhase1TestSession(t))
	m.commandRegistry = reg
	m.textarea.SetValue("/")
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 5})

	view := m.View()
	if lines := strings.Count(view, "\n") + 1; lines > 5 {
		t.Fatalf("command picker View() uses %d rows in a 5-row terminal:\n%s", lines, view)
	}
	for _, want := range []string{"❯ ", "/alpha", "description for /alpha", "select"} {
		if !strings.Contains(view, want) {
			t.Fatalf("short command picker missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "/beta") {
		t.Fatalf("short command picker rendered more rows than available:\n%s", view)
	}
}

func TestChatModelSkillPickerFiltersAndInsertsReference(t *testing.T) {
	sess := newPhase1TestSession(t)
	sess.skillItems = []skillsutil.SkillStatusItem{
		{ID: "imagegen", Name: "Image Generator", Description: "Generate or edit images."},
		{ID: "openai-docs", Name: "OpenAI Docs", Description: "Answer OpenAI product questions."},
	}
	m := newChatModel(sess)
	m.textarea.SetValue("Use $ima")
	m.textarea.CursorEnd()

	view := m.View()
	for _, want := range []string{"❯ ", "$imagegen", "Generate or edit images.", "Enter insert"} {
		if !strings.Contains(view, want) {
			t.Fatalf("skill picker missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "$openai-docs") {
		t.Fatalf("skill picker renders a non-matching skill:\n%s", view)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.textarea.Value(); got != "Use $imagegen " {
		t.Fatalf("skill completion = %q, want %q", got, "Use $imagegen ")
	}
	select {
	case got := <-m.submitted:
		t.Fatalf("skill completion unexpectedly submitted %q", got)
	default:
	}
}

func TestChatModelSkillPickerEscapesTerminalControlCharacters(t *testing.T) {
	sess := newPhase1TestSession(t)
	sess.skillItems = []skillsutil.SkillStatusItem{
		{ID: "safe", Description: "description\x1b[2Jclear\x1b]0;title\x07"},
		{ID: "unsafe\x1b]52;c;clipboard\x07", Description: "ignored"},
		{ID: "line\nbreak", Description: "ignored"},
	}
	m := newChatModel(sess)
	m.textarea.SetValue("$")
	m.textarea.CursorEnd()

	view := m.View()
	for _, unsafe := range []string{"\x1b[2J", "\x1b]0;title\x07", "\x1b]52;c;clipboard\x07"} {
		if strings.Contains(view, unsafe) {
			t.Fatalf("skill picker contains executable terminal control %q: %q", unsafe, view)
		}
	}
	for _, visible := range []string{`\x1b[2Jclear`, `\x1b]0;title\x07`} {
		if !strings.Contains(view, visible) {
			t.Fatalf("skill picker missing visible control sequence %q: %q", visible, view)
		}
	}
	for _, item := range m.picker().items {
		if strings.ContainsAny(item.value, "\r\n\x1b") {
			t.Fatalf("skill picker retained unsafe reference value %q", item.value)
		}
	}
}

func TestChatModelSkillPickerMatchesNameAndUsesID(t *testing.T) {
	sess := newPhase1TestSession(t)
	sess.skillItems = []skillsutil.SkillStatusItem{
		{ID: "imagegen", Name: "Raster Artist", Description: "Generate or edit images."},
	}
	m := newChatModel(sess)
	m.textarea.SetValue("Ask $artist")
	m.textarea.CursorEnd()

	if view := m.View(); !strings.Contains(view, "$imagegen") {
		t.Fatalf("skill picker did not match the display name:\n%s", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.textarea.Value(); got != "Ask $imagegen " {
		t.Fatalf("skill Tab completion = %q, want %q", got, "Ask $imagegen ")
	}
}

func TestChatModelSkillPickerReplacesTokenAtCursor(t *testing.T) {
	sess := newPhase1TestSession(t)
	sess.skillItems = []skillsutil.SkillStatusItem{
		{ID: "imagegen", Description: "Generate or edit images."},
	}
	m := newChatModel(sess)
	m.textarea.SetValue("Use $ima tomorrow")
	m.textarea.SetCursor(len([]rune("Use $ima")))

	if view := m.View(); !strings.Contains(view, "$imagegen") {
		t.Fatalf("skill picker did not follow the cursor:\n%s", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.textarea.Value(); got != "Use $imagegen tomorrow" {
		t.Fatalf("skill completion = %q, want %q", got, "Use $imagegen tomorrow")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if got := m.textarea.Value(); got != "Use $imagegen xtomorrow" {
		t.Fatalf("cursor after skill completion produced %q", got)
	}
}
