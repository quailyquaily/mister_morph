// Package chatcommands provides a unified slash-command dispatcher for chat,
// console, and channel runtimes.
package chatcommands

import (
	"context"
	"strings"
	"sync"
)

// Result is the return value of a command handler.
type Result struct {
	Reply string
	Quit  bool
}

// Handler is the signature for a command handler.
// The args string contains everything after the command word (already trimmed).
// The returned *Result carries reply text and optional quit flag; an error signals a handler failure.
type Handler func(ctx context.Context, args string) (*Result, error)

// Registry maps command names (e.g. "/help") to their handlers.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[string]Handler),
	}
}

// Register binds a command name to a handler. The name is normalised with
// NormalizeCommand before storage, so callers may pass "/help" or "/help@Bot".
// Registering the same name twice overwrites the previous handler.
func (r *Registry) Register(name string, h Handler) {
	name = NormalizeCommand(name)
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[name] = h
}

// Lookup returns the handler for a normalised command name, or nil.
func (r *Registry) Lookup(name string) Handler {
	name = NormalizeCommand(name)
	if name == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.handlers[name]
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		out = append(out, name)
	}
	// Simple bubble sort for deterministic output without importing sort.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[i] > out[j] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
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
