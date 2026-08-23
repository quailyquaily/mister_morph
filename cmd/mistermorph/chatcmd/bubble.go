package chatcmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
)

// Messages sent from the agent goroutine back into the TUI.
type (
	thinkingMsg struct {
		on      bool
		message string
	}
	agentResultMsg struct {
		output       string
		err          error
		keepThinking bool
	}
	sessionStatusMsg      struct{ status chatSessionStatus }
	approvalMsg           struct{ record guard.ApprovalRecord }
	approvalClearedMsg    struct{}
	activityTickMsg       time.Time
	quitConfirmExpiredMsg struct{}
	quitMsg               struct{}
	tuiOutputMsg          struct{ output string }
)

type chatSessionStatus struct {
	model        string
	workspace    string
	contextRatio float64
	contextKnown bool
}

// chatModel owns only the fixed bottom surface. Conversation history is
// printed with tea.Println so it remains in the terminal's native scrollback.
type chatModel struct {
	textarea textarea.Model
	sess     *chatSession

	inputHistory []string
	historyIdx   int
	submitted    chan string

	thinking        bool
	thinkingMessage string
	runStartedAt    time.Time
	activityNow     time.Time
	status          chatSessionStatus

	width  int
	height int

	quitConfirmation  bool
	approval          *guard.ApprovalRecord
	approvalResolving bool
	approvalScroll    int

	commandRegistry *chatcommands.Registry
	pickerIndex     int
	pickerClosed    bool

	// pastedTexts stores the original text behind each paste placeholder,
	// keyed by the exact placeholder string.
	pastedTexts map[string]string
}

const (
	maxInputHeight      = 5
	shortInputHeight    = 3
	shortTerminalHeight = 12
	quitConfirmWindow   = 2 * time.Second
	activityRefresh     = time.Second
	inputMarkerWidth    = 2
	maxPickerItems      = 6
	shortPickerItems    = 3
)

// pastePlaceholderLineThreshold is the minimum line count for a bracketed
// paste to be folded into a compact placeholder.
const pastePlaceholderLineThreshold = 2

var (
	chatAccentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "25", Dark: "75"}).
			Bold(true)
	chatMutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "242", Dark: "245"})
	chatWarningStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "214"}).
				Bold(true)
	chatSuccessStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "77"})
	chatErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"})
)

func (m *chatModel) updateTextareaHeight() {
	lines := strings.Count(m.textarea.Value(), "\n") + 1
	limit := maxInputHeight
	if m.height > 0 && m.height < shortTerminalHeight {
		limit = shortInputHeight
	}
	if lines > limit {
		lines = limit
	}
	m.textarea.SetHeight(max(1, lines))
}

func newChatModel(sess *chatSession) *chatModel {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.SetPromptFunc(inputMarkerWidth, func(line int) string {
		if line == 0 {
			return "› "
		}
		return "  "
	})
	ta.FocusedStyle.Prompt = chatAccentStyle
	ta.BlurredStyle.Prompt = chatAccentStyle
	ta.Focus()
	ta.SetHeight(1)
	ta.SetWidth(79)

	// Enter submits. Alt+Enter and Ctrl+J are handled explicitly below.
	ta.KeyMap.InsertNewline.SetEnabled(false)

	// Keep the composer on the terminal's own background.
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()

	return &chatModel{
		textarea:    ta,
		sess:        sess,
		submitted:   make(chan string, 1),
		status:      chatSessionStatusFromSession(sess),
		width:       80,
		height:      24,
		pastedTexts: make(map[string]string),
	}
}

func (m *chatModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textarea.SetWidth(m.contentWidth())
		m.updateTextareaHeight()
		m.clampApprovalScroll()
		return m, nil

	case tea.KeyMsg:
		if m.approval != nil {
			decision := ""
			switch {
			case msg.Type == tea.KeyUp:
				m.approvalScroll = max(0, m.approvalScroll-1)
			case msg.Type == tea.KeyDown:
				m.approvalScroll++
				m.clampApprovalScroll()
			case msg.Type == tea.KeyCtrlC:
				decision = "n"
			case msg.Type == tea.KeyRunes && strings.EqualFold(string(msg.Runes), "y"):
				decision = "y"
			case msg.Type == tea.KeyRunes && strings.EqualFold(string(msg.Runes), "n"):
				decision = "n"
			}
			if decision != "" && !m.approvalResolving {
				m.approvalResolving = true
				select {
				case m.submitted <- decision:
				default:
					m.approvalResolving = false
				}
			}
			return m, nil
		}

		matches := m.pickerCommands()
		if len(matches) > 0 {
			switch msg.Type {
			case tea.KeyEsc:
				m.pickerClosed = true
				return m, nil
			case tea.KeyUp:
				m.pickerIndex = (m.pickerIndex - 1 + len(matches)) % len(matches)
				return m, nil
			case tea.KeyDown:
				m.pickerIndex = (m.pickerIndex + 1) % len(matches)
				return m, nil
			case tea.KeyTab:
				m.textarea.SetValue(matches[m.pickerIndex].Name)
				m.textarea.CursorEnd()
				m.pickerIndex = 0
				return m, nil
			case tea.KeyEnter:
				m.textarea.SetValue(matches[m.pickerIndex].Name)
				return m, m.submitInput(m.textarea.Value())
			}
		}

		if msg.Type != tea.KeyCtrlC {
			m.quitConfirmation = false
		}

		// Fold bracketed multi-line pastes. Some tmux and SSH combinations do
		// not preserve the paste flag, so retain the existing newline fallback.
		isPasteEvent := msg.Paste && msg.Type == tea.KeyRunes
		if !isPasteEvent && msg.Type == tea.KeyRunes {
			text := string(msg.Runes)
			isPasteEvent = (strings.Contains(text, "\n") || strings.Contains(text, "\r")) && len(text) > 10
		}
		if isPasteEvent {
			text := string(msg.Runes)
			lines := countPasteLines(text)
			if lines >= pastePlaceholderLineThreshold {
				placeholder := fmt.Sprintf("[Pasted text #%d +%d lines]", len(m.pastedTexts)+1, lines)
				m.pastedTexts[placeholder] = text
				m.textarea.InsertString(placeholder)
				m.updateTextareaHeight()
				return m, nil
			}
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			if m.sess != nil && m.sess.cancelForegroundCommand() {
				return m, nil
			}
			if m.thinking {
				go func() { m.submitted <- "/stop" }()
				return m, nil
			}
			if m.textarea.Value() != "" {
				m.textarea.Reset()
				m.pastedTexts = make(map[string]string)
				m.updateTextareaHeight()
				return m, nil
			}
			if m.quitConfirmation {
				return m, tea.Quit
			}
			m.quitConfirmation = true
			return m, tea.Tick(quitConfirmWindow, func(time.Time) tea.Msg {
				return quitConfirmExpiredMsg{}
			})

		case tea.KeyCtrlD:
			if m.textarea.Value() == "" {
				return m, tea.Quit
			}

		case tea.KeyCtrlJ:
			m.textarea.InsertString("\n")
			m.updateTextareaHeight()
			return m, nil

		case tea.KeyEnter:
			if msg.Alt {
				m.textarea.InsertString("\n")
				m.updateTextareaHeight()
				return m, nil
			}
			return m, m.submitInput(m.textarea.Value())

		case tea.KeyUp:
			if m.atTextareaTop() {
				if m.historyIdx > 0 {
					m.historyIdx--
					m.textarea.SetValue(m.inputHistory[m.historyIdx])
					m.textarea.CursorEnd()
					m.updateTextareaHeight()
				}
				return m, nil
			}

		case tea.KeyDown:
			if m.atTextareaBottom() {
				if m.historyIdx < len(m.inputHistory)-1 {
					m.historyIdx++
					m.textarea.SetValue(m.inputHistory[m.historyIdx])
					m.textarea.CursorEnd()
					m.updateTextareaHeight()
				} else if m.historyIdx == len(m.inputHistory)-1 {
					m.historyIdx = len(m.inputHistory)
					m.textarea.Reset()
					m.updateTextareaHeight()
				}
				return m, nil
			}

		}

	case thinkingMsg:
		wasThinking := m.thinking
		m.thinking = msg.on
		if msg.on {
			if !wasThinking {
				m.runStartedAt = time.Now()
				m.activityNow = m.runStartedAt
			}
			m.thinkingMessage = normalizeActivityText(msg.message)
			m.textarea.Focus()
			if !wasThinking {
				return m, activityTick()
			}
			return m, nil
		}
		m.thinkingMessage = ""
		m.runStartedAt = time.Time{}
		m.activityNow = time.Time{}
		m.textarea.Focus()
		return m, nil

	case activityTickMsg:
		if !m.thinking {
			return m, nil
		}
		m.activityNow = time.Time(msg)
		return m, activityTick()

	case sessionStatusMsg:
		m.status = msg.status
		return m, nil

	case approvalMsg:
		record := msg.record
		m.approval = &record
		m.approvalResolving = false
		m.approvalScroll = 0
		m.thinking = false
		m.thinkingMessage = ""
		m.runStartedAt = time.Time{}
		m.activityNow = time.Time{}
		return m, nil

	case approvalClearedMsg:
		m.approval = nil
		m.approvalResolving = false
		m.approvalScroll = 0
		m.textarea.Focus()
		return m, nil

	case quitConfirmExpiredMsg:
		m.quitConfirmation = false
		return m, nil

	case tuiOutputMsg:
		return m, tea.Println(msg.output)

	case agentResultMsg:
		if msg.err != nil && m.approval != nil {
			m.approvalResolving = false
		}
		if !msg.keepThinking {
			m.thinking = false
			m.thinkingMessage = ""
			m.runStartedAt = time.Time{}
			m.activityNow = time.Time{}
		}
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				return m, tea.Println("■ Stopped by user")
			}
			return m, tea.Println(chatErrorStyle.Render("×") + " " + msg.err.Error())
		}
		return m, tea.Println(msg.output)

	case quitMsg:
		return m, tea.Sequence(
			tea.Println("Bye! 👋"),
			tea.Quit,
		)
	}

	before := m.textarea.Value()
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	if m.textarea.Value() != before {
		m.pickerClosed = false
		m.pickerIndex = 0
	}
	cmds = append(cmds, cmd)
	m.updateTextareaHeight()
	return m, tea.Batch(cmds...)
}

// View renders only the fixed bottom surface. The leading blank row separates
// it from transcript content printed into native terminal scrollback.
func (m *chatModel) View() string {
	lines := []string{""}
	if m.approval != nil {
		return strings.Join(append(lines, m.renderApproval()...), "\n")
	}
	if m.thinking {
		lines = append(lines, m.renderActivity())
	}
	lines = append(lines, m.textarea.View())
	if picker := m.renderCommandPicker(); len(picker) > 0 {
		lines = append(lines, picker...)
	}
	lines = append(lines, m.renderFooter())
	return strings.Join(lines, "\n")
}

func (m *chatModel) contentWidth() int {
	if m.width <= 1 {
		return 10
	}
	return max(10, m.width-1)
}

func (m *chatModel) renderActivity() string {
	width := m.contentWidth()
	detail := normalizeActivityText(m.thinkingMessage)
	parts := []string{chatAccentStyle.Render("● Running")}
	if detail != "" {
		parts = append(parts, chatMutedStyle.Render(detail))
	}
	if !m.runStartedAt.IsZero() {
		now := m.activityNow
		if now.IsZero() {
			now = m.runStartedAt
		}
		parts = append(parts, chatMutedStyle.Render(formatActivityElapsed(now.Sub(m.runStartedAt))))
	}
	return fitChatLine(strings.Join(parts, chatMutedStyle.Render(" · ")), width)
}

func (m *chatModel) renderFooter() string {
	width := m.contentWidth()
	if len(m.pickerCommands()) > 0 {
		return fitChatLine(chatMutedStyle.Render("  ↑↓ select · Tab complete · Enter run · Esc close"), width)
	}
	if m.quitConfirmation {
		return fitChatLine(chatMutedStyle.Render("  Ctrl+C again to exit"), width)
	}
	if m.thinking {
		text := "  Enter steer · Ctrl+C stop"
		if width < 60 {
			text = "  Ctrl+C stop"
		}
		return fitChatLine(chatMutedStyle.Render(text), width)
	}

	left := make([]string, 0, 3)
	if m.status.model != "" {
		left = append(left, m.status.model)
	}
	if name := workspaceDisplayName(m.status.workspace); name != "" {
		left = append(left, name)
	}
	if m.status.contextKnown {
		left = append(left, fmt.Sprintf("ctx %.0f%%", m.status.contextRatio*100))
	}
	leftText := strings.Join(left, " · ")
	return chatMutedStyle.Render(joinChatFooter(leftText, "/ commands", width))
}

func (m *chatModel) atTextareaTop() bool {
	info := m.textarea.LineInfo()
	return m.textarea.Line() == 0 && info.RowOffset == 0
}

func (m *chatModel) atTextareaBottom() bool {
	info := m.textarea.LineInfo()
	return m.textarea.Line() == m.textarea.LineCount()-1 && info.RowOffset >= info.Height-1
}

func activityTick() tea.Cmd {
	return tea.Tick(activityRefresh, func(now time.Time) tea.Msg {
		return activityTickMsg(now)
	})
}

func formatActivityElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	totalSeconds := int(elapsed.Round(time.Second) / time.Second)
	return fmt.Sprintf("%02d:%02d", totalSeconds/60, totalSeconds%60)
}

func normalizeActivityText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || text == "assistant is thinking..." {
		return "waiting for model"
	}
	return strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(text)
}

func joinChatFooter(left, right string, width int) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return fitChatLine("  "+right, width)
	}
	left = "  " + left
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap >= 3 {
		return left + strings.Repeat(" ", gap) + right
	}
	if width < 40 {
		return fitChatLine("  "+right, width)
	}
	availableLeft := width - ansi.StringWidth(right) - 3
	if availableLeft <= inputMarkerWidth {
		return fitChatLine("  "+right, width)
	}
	left = ansi.Truncate(left, availableLeft, "…")
	return left + "   " + right
}

func fitChatLine(line string, width int) string {
	if width <= 0 || ansi.StringWidth(line) <= width {
		return line
	}
	return ansi.Truncate(line, width, "…")
}

func workspaceDisplayName(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	name := filepath.Base(filepath.Clean(path))
	if name == "." || name == string(filepath.Separator) {
		return ""
	}
	return name
}

func chatSessionStatusFromSession(sess *chatSession) chatSessionStatus {
	if sess == nil {
		return chatSessionStatus{}
	}
	status := chatSessionStatus{
		model:     strings.TrimSpace(sess.mainCfg.Model),
		workspace: strings.TrimSpace(sess.workspaceDir),
	}
	if sess.topicContextStore == nil {
		return status
	}
	item, found, err := sess.topicContextStore.Get(sess.conversationKey())
	if err == nil && found && item.ContextWindowTokens > 0 {
		status.contextRatio = item.UsageRatio
		status.contextKnown = true
	}
	return status
}

func formatSubmittedInput(input string) string {
	lines := strings.Split(input, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = chatAccentStyle.Render("› ") + lines[i]
		} else {
			lines[i] = "  " + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

func (m *chatModel) submitInput(value string) tea.Cmd {
	raw := strings.TrimSpace(value)
	if raw == "" {
		m.textarea.Reset()
		return nil
	}
	expanded := m.expandPastePlaceholders(raw)
	go func() { m.submitted <- expanded }()
	m.saveHistoryLine(raw)
	m.inputHistory = append(m.inputHistory, raw)
	m.historyIdx = len(m.inputHistory)
	m.textarea.Reset()
	m.pastedTexts = make(map[string]string)
	m.pickerClosed = false
	m.pickerIndex = 0
	m.updateTextareaHeight()
	return tea.Println(formatSubmittedInput(expanded))
}

func (m *chatModel) pickerCommands() []chatcommands.Command {
	if m.commandRegistry == nil || m.pickerClosed {
		return nil
	}
	input := m.textarea.Value()
	if !strings.HasPrefix(input, "/") || strings.ContainsAny(input, " \t\r\n") {
		return nil
	}
	input = strings.ToLower(input)
	commands := m.commandRegistry.Commands()
	matches := make([]chatcommands.Command, 0, len(commands))
	for _, command := range commands {
		if strings.HasPrefix(command.Name, input) {
			matches = append(matches, command)
		}
	}
	if len(matches) == 0 {
		m.pickerIndex = 0
	} else if m.pickerIndex >= len(matches) {
		m.pickerIndex = len(matches) - 1
	}
	return matches
}

func (m *chatModel) renderCommandPicker() []string {
	matches := m.pickerCommands()
	if len(matches) == 0 {
		return nil
	}
	limit := maxPickerItems
	if m.width < 60 || (m.height > 0 && m.height <= shortTerminalHeight) {
		limit = shortPickerItems
	}
	if m.height > 0 {
		textareaRows := strings.Count(m.textarea.View(), "\n") + 1
		reservedRows := 2 + textareaRows // separator and footer
		if m.thinking {
			reservedRows++
		}
		rowsPerItem := 1
		if m.width < 60 {
			rowsPerItem = 2
		}
		limit = min(limit, max(0, m.height-reservedRows)/rowsPerItem)
	}
	if limit == 0 {
		return nil
	}
	start := 0
	if m.pickerIndex >= limit {
		start = m.pickerIndex - limit + 1
	}
	end := min(len(matches), start+limit)
	visible := matches[start:end]
	nameWidth := 0
	for _, command := range visible {
		nameWidth = max(nameWidth, ansi.StringWidth(command.Name))
	}

	width := m.contentWidth()
	lines := make([]string, 0, len(visible)*2)
	for i, command := range visible {
		selected := start+i == m.pickerIndex
		marker := "  "
		name := command.Name
		if selected {
			marker = chatAccentStyle.Render("› ")
			name = chatAccentStyle.Render(name)
		}
		if width >= 60 || command.Description == "" {
			gap := strings.Repeat(" ", nameWidth-ansi.StringWidth(command.Name)+2)
			lines = append(lines, fitChatLine(marker+name+gap+chatMutedStyle.Render(command.Description), width))
			continue
		}
		lines = append(lines, fitChatLine(marker+name, width))
		lines = append(lines, fitChatLine("  "+chatMutedStyle.Render(command.Description), width))
	}
	return lines
}

func (m *chatModel) renderApproval() []string {
	data := chatApprovalData(*m.approval)
	tool := data.tool
	if tool == "" {
		tool = "action"
	}
	width := m.contentWidth()
	title := fitChatLine(chatWarningStyle.Render("! Approval")+" · "+tool, width)
	surfaceHeight := max(1, m.height-1)
	if surfaceHeight == 1 {
		return []string{title}
	}

	body := renderApprovalBody(data, width)
	bodyHeight := max(0, surfaceHeight-2)
	start := min(max(0, len(body)-bodyHeight), max(0, m.approvalScroll))
	end := min(len(body), start+bodyHeight)
	visible := append([]string(nil), body[start:end]...)
	if len(body) <= bodyHeight && len(visible) < bodyHeight {
		visible = append(visible, "")
	}

	footer := "  y approve · n deny"
	if len(body) > bodyHeight && bodyHeight > 0 {
		footer = fmt.Sprintf("  y approve · n deny · ↑↓ %d–%d/%d", start+1, end, len(body))
	}
	lines := make([]string, 0, 2+len(visible))
	lines = append(lines, title)
	lines = append(lines, visible...)
	lines = append(lines, fitChatLine(chatMutedStyle.Render(footer), width))
	return lines
}

func renderApprovalBody(data chatApprovalViewData, width int) []string {
	lines := make([]string, 0, len(data.reasons)+len(data.params)*2)
	for _, reason := range data.reasons {
		lines = append(lines, wrapIndentedChatText(reason, width)...)
	}
	for _, param := range data.params {
		lines = append(lines, "  "+chatMutedStyle.Render(param.name))
		value := param.value
		if param.name == "cmd" {
			prefix := "$ "
			if strings.EqualFold(data.tool, "powershell") {
				prefix = "PS> "
			}
			value = prefix + value
		}
		wrapped := ansi.Hardwrap(value, max(1, width-4), false)
		for _, line := range strings.Split(wrapped, "\n") {
			lines = append(lines, "    "+line)
		}
	}
	if len(data.params) == 0 && data.action != "" {
		lines = append(lines, wrapIndentedChatText(data.action, width)...)
	}
	return lines
}

func (m *chatModel) clampApprovalScroll() {
	if m.approval == nil {
		m.approvalScroll = 0
		return
	}
	bodyHeight := max(0, max(1, m.height-1)-2)
	body := renderApprovalBody(chatApprovalData(*m.approval), m.contentWidth())
	m.approvalScroll = min(max(0, m.approvalScroll), max(0, len(body)-bodyHeight))
}

func wrapIndentedChatText(text string, width int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	wrapped := ansi.Hardwrap(text, max(1, width-inputMarkerWidth), false)
	parts := strings.Split(wrapped, "\n")
	for i := range parts {
		parts[i] = "  " + parts[i]
	}
	return parts
}

func countPasteLines(s string) int {
	if s == "" {
		return 0
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

func (m *chatModel) expandPastePlaceholders(s string) string {
	for placeholder, original := range m.pastedTexts {
		s = strings.ReplaceAll(s, placeholder, original)
	}
	return s
}

func (m *chatModel) loadHistory() error {
	path := filepath.Join(os.Getenv("HOME"), ".mistermorph_chat_history")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			m.inputHistory = append(m.inputHistory, line)
		}
	}
	m.historyIdx = len(m.inputHistory)
	return nil
}

func (m *chatModel) saveHistoryLine(input string) {
	path := filepath.Join(os.Getenv("HOME"), ".mistermorph_chat_history")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintln(f, input)
}
