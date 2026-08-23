// Package chatcommands provides a unified slash-command dispatcher for chat,
// console, and channel runtimes.
package chatcommands

import (
	"context"
	"sort"
	"strings"
	"sync"
)

type Action string

const ActionContextCompact Action = "context_compact"

// Result is the return value of a command handler.
type Result struct {
	Reply  string
	Quit   bool
	Action Action
}

// Handler is the signature for a command handler.
// The args string contains everything after the command word (already trimmed).
// The returned *Result carries reply text and optional quit flag; an error signals a handler failure.
type Handler func(ctx context.Context, args string) (*Result, error)

// Command is the user-facing metadata for one registered command.
type Command struct {
	Name        string
	Description string
}

type entry struct {
	description string
	handler     Handler
}

// Registry maps command names (e.g. "/help") to their handlers.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]entry
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string]entry),
	}
}

// Register binds a command name to a handler. The name is normalised with
// NormalizeCommand before storage, so callers may pass "/help" or "/help@Bot".
// Registering the same name twice overwrites the previous entry.
func (r *Registry) Register(name, description string, h Handler) {
	name = NormalizeCommand(name)
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[name] = entry{description: strings.TrimSpace(description), handler: h}
}

// Lookup returns the handler for a normalised command name, or nil.
func (r *Registry) Lookup(name string) Handler {
	name = NormalizeCommand(name)
	if name == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entries[name].handler
}

// Dispatch parses text into a command word and arguments, looks up the
// registered handler, and invokes it. If the text does not start with a
// recognised command, result == nil, handled == false and err == nil.
func (r *Registry) Dispatch(ctx context.Context, text string) (result *Result, handled bool, err error) {
	cmd, args := ParseCommand(text)
	if cmd == "" {
		return nil, false, nil
	}
	h := r.Lookup(cmd)
	if h == nil {
		return nil, false, nil
	}
	result, err = h(ctx, args)
	return result, true, err
}

// Names returns a sorted snapshot of all registered command names.
func (r *Registry) Names() []string {
	commands := r.Commands()
	out := make([]string, len(commands))
	for i := range commands {
		out[i] = commands[i].Name
	}
	return out
}

// Commands returns a sorted snapshot of command names and descriptions.
func (r *Registry) Commands() []Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Command, 0, len(r.entries))
	for name, entry := range r.entries {
		out = append(out, Command{Name: name, Description: entry.description})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ParseCommand splits a raw message into (cmd, args). If text does not start
// with a "/" command, cmd is empty. The command word is NOT normalised here so
// callers can choose whether to apply bot-mention stripping separately.
func ParseCommand(text string) (cmd string, args string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	i := strings.IndexAny(text, " \n\t")
	if i == -1 {
		return text, ""
	}
	return text[:i], strings.TrimSpace(text[i:])
}

// NormalizeCommand strips a trailing "@bot" suffix (used by Telegram and
// sometimes Slack) and lower-cases the word. It returns "" for non-command
// input (i.e. strings that do not start with "/").
func NormalizeCommand(word string) string {
	word = strings.TrimSpace(word)
	if word == "" || !strings.HasPrefix(word, "/") {
		return ""
	}
	if at := strings.IndexByte(word, '@'); at >= 0 {
		word = word[:at]
	}
	return strings.ToLower(word)
}

// ExtractThinkTask recognises "/think <task>" as a task prefix. It returns the
// task text with the prefix removed, leaving normal slash-command dispatch to
// handle all other commands.
func ExtractThinkTask(text string) (task string, ok bool) {
	cmd, args := ParseCommand(text)
	if NormalizeCommand(cmd) != "/think" {
		return strings.TrimSpace(text), false
	}
	return strings.TrimSpace(args), true
}

// IsContextCompactCommand reports whether text is exactly a /ctx compact
// command, allowing the command-name bot suffix used by channel runtimes.
func IsContextCompactCommand(text string) bool {
	cmd, args := ParseCommand(text)
	return NormalizeCommand(cmd) == "/ctx" && strings.EqualFold(strings.TrimSpace(args), "compact")
}
