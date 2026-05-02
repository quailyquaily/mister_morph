package chatcmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Messages sent from the agent goroutine back into the TUI.
type (
	thinkingMsg struct {
		on      bool
		message string // optional custom message shown next to spinner
	}
	agentResultMsg struct {
		output string
		err    error
	}
	quitMsg      struct{}
	tuiOutputMsg struct{ output string }
)

// chatModel is a bubbletea Model that powers the interactive chat REPL.
// Design: history is printed via tea.Println (outside the View); View only
// renders the input area (fixed height) so bubbletea can repaint cleanly.
type chatModel struct {
	textarea textarea.Model
	spinner  spinner.Model
	sess     *chatSession

	// prompt shown before the input area (e.g. "• ")
	prompt string

	// inputHistory holds previous user inputs for arrow-up/down recall
	inputHistory []string
	historyIdx   int

	// submitted input is sent through this channel to the REPL loop
	submitted chan string

	// whether the agent is currently running
	thinking bool

	// custom message shown next to the spinner
	thinkingMessage string

	// width tracks terminal size for textarea width
	width int

	// pastedTexts stores the original text behind each paste placeholder,
	// keyed by the placeholder counter. Populated when bracketed paste
	// delivers a multi-line block; consumed when the user submits and
	// placeholders are expanded back to their original content.
	pastedTexts  map[int]string
	pasteCounter int
}

const maxInputHeight = 5

// pastePlaceholderLineThreshold is the minimum line count for a bracketed
// paste to be folded into a "[Pasted text #N +M lines]" placeholder. Single
// or double-line pastes are inserted verbatim.
const pastePlaceholderLineThreshold = 2

// pastePlaceholderRe matches placeholders like "[Pasted text #12 +13 lines]".
var pastePlaceholderRe = regexp.MustCompile(`\[Pasted text #(\d+) \+\d+ lines\]`)

// pulsingDotSpinner returns a custom bubbletea spinner whose frames are a
// single dot (•) that cycles white → yellow → green → dark-grey → white.
func pulsingDotSpinner() spinner.Spinner {
	const dot = "•"
	const stepsPerSegment = 4 // frames per colour transition (start inclusive, end exclusive)

	// 4-colour ring: white → yellow → green → dark-grey → white
	colours := [][3]int{
		{255, 255, 255}, // white
		{255, 255, 0},   // yellow
		{51, 255, 87},   // green (#33FF57)
		{40, 40, 40},    // near-black (visible on dark terminals)
	}

	frames := make([]string, 0, len(colours)*stepsPerSegment)
	n := len(colours)
	for i := 0; i < n; i++ {
		from := colours[i]
		to := colours[(i+1)%n]
		for j := 0; j < stepsPerSegment; j++ {
			t := float64(j) / float64(stepsPerSegment)
			r := int(float64(from[0]) + (float64(to[0])-float64(from[0]))*t)
			g := int(float64(from[1]) + (float64(to[1])-float64(from[1]))*t)
			b := int(float64(from[2]) + (float64(to[2])-float64(from[2]))*t)
			frames = append(frames, fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, b, dot))
		}
	}
	return spinner.Spinner{
		Frames: frames,
		FPS:    time.Second / 12,
	}
}

func (m *chatModel) updateTextareaHeight() {
	lines := strings.Count(m.textarea.Value(), "\n") + 1
	if lines < 1 {
		lines = 1
	}
	if lines > maxInputHeight {
		lines = maxInputHeight
	}
	m.textarea.SetHeight(lines)
}

func newChatModel(sess *chatSession) *chatModel {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.Focus()
	ta.SetHeight(1)
	ta.SetWidth(80)

	// Disable default Enter binding so we handle submission ourselves
	ta.KeyMap.InsertNewline.SetEnabled(false)

	// Clear the default cursor-line background so the input area blends with
	// the terminal background instead of showing a colored block.
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()

	s := spinner.New()
	s.Spinner = pulsingDotSpinner()
	// Colors are baked into the spinner frames; no extra style needed.

	return &chatModel{
		textarea:    ta,
		spinner:     s,
		sess:        sess,
		prompt:      buildUserPrompt(sess.compactMode, sess.userName),
		submitted:   make(chan string, 1),
		pastedTexts: make(map[int]string),
	}
}

func (m *chatModel) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		spinner.Tick,
	)
}

func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		tw := msg.Width - lipgloss.Width(m.prompt) - 1
		if tw < 10 {
			tw = 10
		}
		m.textarea.SetWidth(tw)
		return m, nil

	case tea.KeyMsg:
		if m.thinking {
			// While agent is running, only Ctrl+C is handled (to interrupt)
			if msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
			return m, nil
		}

		// Bracketed paste: fold multi-line pastes into a "[Pasted text #N +M lines]"
		// placeholder so the input box stays compact. The original text is
		// stashed in m.pastedTexts and re-expanded on submit.
		//
		// Fallback: if the terminal does not send bracketed-paste events (e.g.
		// some tmux/ssh setups), we also treat any single key event that contains
		// a newline and is longer than a typical typed line as a paste.
		isPasteEvent := msg.Paste && msg.Type == tea.KeyRunes
		if !isPasteEvent && msg.Type == tea.KeyRunes {
			text := string(msg.Runes)
			isPasteEvent = (strings.Contains(text, "\n") || strings.Contains(text, "\r")) && len(text) > 10
		}
		if isPasteEvent {
			text := string(msg.Runes)
			lines := countPasteLines(text)
			if lines >= pastePlaceholderLineThreshold {
				m.pasteCounter++
				id := m.pasteCounter
				m.pastedTexts[id] = text
				placeholder := fmt.Sprintf("[Pasted text #%d +%d lines]", id, lines)
				m.textarea.InsertString(placeholder)
				m.updateTextareaHeight()
				return m, nil
			}
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyEnter:
			raw := strings.TrimSpace(m.textarea.Value())
			if raw != "" {
				expanded := m.expandPastePlaceholders(raw)
				m.submitted <- expanded
				m.saveHistoryLine(raw)
				m.inputHistory = append(m.inputHistory, raw)
				m.historyIdx = len(m.inputHistory)
				m.textarea.Reset()
				// Persist the expanded text to the scrollback so the user can
				// confirm what was actually pasted. History (file + arrow-up)
				// keeps the compact placeholder form.
				return m, tea.Println(fmt.Sprintf("%s%s", m.prompt, expanded))
			}
			m.textarea.Reset()
			return m, nil

		case tea.KeyUp:
			if m.historyIdx > 0 {
				m.historyIdx--
				m.textarea.SetValue(m.inputHistory[m.historyIdx])
				m.textarea.SetCursor(len(m.textarea.Value()))
			}
			return m, nil

		case tea.KeyDown:
			if m.historyIdx < len(m.inputHistory)-1 {
				m.historyIdx++
				m.textarea.SetValue(m.inputHistory[m.historyIdx])
				m.textarea.SetCursor(len(m.textarea.Value()))
			} else if m.historyIdx == len(m.inputHistory)-1 {
				m.historyIdx = len(m.inputHistory)
				m.textarea.Reset()
			}
			return m, nil

		case tea.KeyTab:
			m.doAutocomplete()
			return m, nil
		}

	case thinkingMsg:
		m.thinking = msg.on
		if msg.on {
			m.thinkingMessage = msg.message
			m.textarea.Blur()
			return m, spinner.Tick
		}
		m.thinkingMessage = ""
		m.textarea.Focus()
		return m, nil

	case tuiOutputMsg:
		return m, tea.Println(msg.output)

	case agentResultMsg:
		m.thinking = false
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				return m, tea.Println("\033[33m⚡ Interrupted.\033[0m")
			}
			return m, tea.Println(fmt.Sprintf("error: %s", msg.err.Error()))
		}
		return m, tea.Println(msg.output)

	case quitMsg:
		return m, tea.Sequence(
			tea.Println("Bye! 👋"),
			tea.Quit,
		)
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)
	m.updateTextareaHeight()

	if m.thinking {
		var spinCmd tea.Cmd
		m.spinner, spinCmd = m.spinner.Update(msg)
		cmds = append(cmds, spinCmd)
	}

	return m, tea.Batch(cmds...)
}

// View renders only the input area (fixed height). All conversation history
// is printed via tea.Println outside of View, so bubbletea never has to
// repaint scrolling content.
func (m *chatModel) View() string {
	var b strings.Builder

	if m.thinking {
		b.WriteString(m.spinner.View())
		if m.thinkingMessage != "" {
			b.WriteString(" ")
			b.WriteString(m.thinkingMessage)
		} else {
			b.WriteString(" assistant is thinking...")
		}
		b.WriteString("\n")
	}

	b.WriteString(m.prompt)
	b.WriteString(m.textarea.View())

	return b.String()
}

func (m *chatModel) doAutocomplete() {
	input := m.textarea.Value()
	if input == "" {
		return
	}

	commands := []string{
		"/exit", "/quit", "/reset", "/memory", "/remember ",
		"/skill", "/init", "/update", "/model",
		"/workspace", "/workspace attach ", "/workspace detach",
		"/help",
	}

	for _, cmd := range commands {
		if strings.HasPrefix(cmd, input) && cmd != input {
			m.textarea.SetValue(cmd)
			m.textarea.SetCursor(len(cmd))
			return
		}
	}
}

// countPasteLines returns the line count for a pasted block. An empty string
// is zero lines; a string with no trailing newline counts the final partial
// line. Handles \n, \r\n, and bare \r.
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

// expandPastePlaceholders rewrites every "[Pasted text #N +M lines]" token in
// s back to the original text stored in m.pastedTexts. Unknown ids are left
// as-is so the agent at least sees the literal placeholder.
func (m *chatModel) expandPastePlaceholders(s string) string {
	if len(m.pastedTexts) == 0 || !strings.Contains(s, "[Pasted text #") {
		return s
	}
	return pastePlaceholderRe.ReplaceAllStringFunc(s, func(match string) string {
		sub := pastePlaceholderRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		id, err := strconv.Atoi(sub[1])
		if err != nil {
			return match
		}
		if text, ok := m.pastedTexts[id]; ok {
			return text
		}
		return match
	})
}

// loadHistory reads previous inputs from the history file.
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

// saveHistoryLine appends a single input line to the history file.
func (m *chatModel) saveHistoryLine(input string) {
	path := filepath.Join(os.Getenv("HOME"), ".mistermorph_chat_history")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintln(f, input)
}
