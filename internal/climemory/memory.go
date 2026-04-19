// Package climemory provides project-level memory for CLI mode.
// It reads/writes a .morph/memory.md file in the current working directory,
// allowing the AI to remember context across CLI sessions.
package climemory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/markdown"
	"gopkg.in/yaml.v3"
)

const (
	memoryDirName   = ".morph"
	memoryFileName  = "memory.md"
	maxBodyChars    = 3000 // Max characters of memory body to inject into prompt
	maxSummaryItems = 20   // Max bullet items in memory
)

// Memory holds the parsed content of a .morph/memory.md file.
type Memory struct {
	Frontmatter MemoryFrontmatter `yaml:"-"`
	Body        string            `yaml:"-"`
	AbsPath     string            `yaml:"-"`
}

// MemoryFrontmatter is the YAML frontmatter for CLI memory files.
type MemoryFrontmatter struct {
	CreatedAt string   `yaml:"created_at"`
	UpdatedAt string   `yaml:"updated_at"`
	Version   int      `yaml:"version"`
	Tags      []string `yaml:"tags,omitempty"`
}

// MemoryDir returns the .morph directory path for the given working directory.
func MemoryDir(workDir string) string {
	return filepath.Join(workDir, memoryDirName)
}

// MemoryFilePath returns the full path to the memory file.
func MemoryFilePath(workDir string) string {
	return filepath.Join(MemoryDir(workDir), memoryFileName)
}

// Load reads the memory file from the working directory if it exists.
// Returns nil, nil if the file doesn't exist.
func Load(workDir string) (*Memory, error) {
	absPath := MemoryFilePath(workDir)
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read memory file: %w", err)
	}

	contents := string(data)
	var fm MemoryFrontmatter
	var body string
	var ok bool

	raw, bodyOnly, hasFM := markdown.SplitFrontmatter(contents)
	if hasFM {
		if err := yaml.Unmarshal([]byte(raw), &fm); err == nil {
			ok = true
		}
		body = bodyOnly
	} else {
		body = contents
	}

	if !ok {
		// No valid frontmatter, treat entire file as body
		body = contents
		fm = MemoryFrontmatter{}
	}

	return &Memory{
		Frontmatter: fm,
		Body:        strings.TrimSpace(body),
		AbsPath:     absPath,
	}, nil
}

// Save writes the memory to disk, creating the .morph directory if needed.
func (m *Memory) Save() error {
	if m == nil || m.AbsPath == "" {
		return nil
	}

	dir := filepath.Dir(m.AbsPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if m.Frontmatter.CreatedAt == "" {
		m.Frontmatter.CreatedAt = now
	}
	m.Frontmatter.UpdatedAt = now
	if m.Frontmatter.Version <= 0 {
		m.Frontmatter.Version = 1
	}

	fmData, err := yaml.Marshal(m.Frontmatter)
	if err != nil {
		return fmt.Errorf("marshal frontmatter: %w", err)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(string(fmData))
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(m.Body))
	b.WriteString("\n")

	if err := os.WriteFile(m.AbsPath, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write memory file: %w", err)
	}
	return nil
}

// ToPromptBlock formats the memory as a prompt block for injection.
// It truncates if the body exceeds maxBodyChars.
func (m *Memory) ToPromptBlock() string {
	if m == nil || m.Body == "" {
		return ""
	}

	body := m.Body
	if len(body) > maxBodyChars {
		body = body[:maxBodyChars]
		// Try to cut at a newline
		if idx := strings.LastIndex(body, "\n"); idx > maxBodyChars*3/4 {
			body = body[:idx]
		}
		body = strings.TrimSpace(body) + "\n\n... (memory truncated)"
	}

	var b strings.Builder
	b.WriteString("## Project Memory\n\n")
	b.WriteString("The following is remembered context about this project/directory. " +
		"Use it to understand the user's preferences and past interactions.\n\n")
	b.WriteString(body)
	return b.String()
}

// AppendEntry adds a new memory entry to the body.
func (m *Memory) AppendEntry(entry string) {
	if m == nil {
		return
	}
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return
	}
	timestamp := time.Now().UTC().Format("2006-01-02")
	line := fmt.Sprintf("- [%s] %s", timestamp, entry)
	if m.Body != "" {
		m.Body += "\n"
	}
	m.Body += line
}

// TruncateIfNeeded removes oldest entries if there are more than maxSummaryItems.
func (m *Memory) TruncateIfNeeded() {
	if m == nil || m.Body == "" {
		return
	}
	lines := strings.Split(m.Body, "\n")
	var items []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			items = append(items, line)
		}
	}
	if len(items) <= maxSummaryItems {
		return
	}
	// Keep the most recent entries
	keep := items[len(items)-maxSummaryItems:]
	m.Body = strings.Join(keep, "\n")
}

// Exists checks if a memory file exists for the given working directory.
func Exists(workDir string) bool {
	_, err := os.Stat(MemoryFilePath(workDir))
	return err == nil
}
