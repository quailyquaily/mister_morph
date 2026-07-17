package chatcommands

import (
	"context"
	"fmt"
	"strings"
)

// HelpHandler returns a Handler that replies with a list of registered commands.
// The optional header is printed before the command list.
func HelpHandler(r *Registry, header string) Handler {
	return func(ctx context.Context, args string) (*Result, error) {
		names := r.Names()
		var b strings.Builder
		if header != "" {
			b.WriteString(header)
			b.WriteString("\n")
		}
		if len(names) == 0 {
			b.WriteString("No commands available.")
			return &Result{Reply: b.String()}, nil
		}
		for _, name := range names {
			if b.Len() > 0 && b.String()[b.Len()-1] != '\n' {
				b.WriteString("\n")
			}
			b.WriteString("  ")
			b.WriteString(name)
		}
		return &Result{Reply: b.String()}, nil
	}
}

// ModelCommandFunc executes a /models command string and reports whether it was handled.
type ModelCommandFunc = func(text string) (output string, handled bool, err error)

// ModelCommandHandler adapts a /models command executor to the Registry Handler
// signature, whose input is only the argument tail after "/models".
func ModelCommandHandler(fn ModelCommandFunc) Handler {
	return func(ctx context.Context, args string) (*Result, error) {
		if fn == nil {
			return nil, fmt.Errorf("missing llm profile command handler")
		}
		output, handled, err := fn(modelCommandText(args))
		if !handled {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &Result{Reply: output}, nil
	}
}

func modelCommandText(args string) string {
	text := "/models"
	if args = strings.TrimSpace(args); args != "" {
		text += " " + args
	}
	return text
}

// SkillCommandFunc returns a snapshot of the current skill loading state.
type SkillCommandFunc = func() (output string, err error)

func SkillCommandHandler(fn SkillCommandFunc) Handler {
	return func(ctx context.Context, args string) (*Result, error) {
		if fn == nil {
			return nil, fmt.Errorf("missing skill command handler")
		}
		output, err := fn()
		if err != nil {
			return nil, err
		}
		return &Result{Reply: output}, nil
	}
}

// ContextCommandFunc returns the current topic context usage.
type ContextCommandFunc = func() (output string, err error)

func ContextCommandHandler(fn ContextCommandFunc) Handler {
	return func(ctx context.Context, args string) (*Result, error) {
		switch strings.ToLower(strings.TrimSpace(args)) {
		case "compact":
			return &Result{Action: ActionContextCompact}, nil
		case "":
		default:
			return &Result{Reply: "Usage: /ctx [compact]"}, nil
		}
		if fn == nil {
			return nil, fmt.Errorf("missing context command handler")
		}
		output, err := fn()
		if err != nil {
			return nil, err
		}
		return &Result{Reply: output}, nil
	}
}

// WorkspaceCommandFunc executes a /workspace command against runtime-local state.
type WorkspaceCommandFunc = func(args string) (output string, err error)

func WorkspaceCommandHandler(fn WorkspaceCommandFunc) Handler {
	return func(ctx context.Context, args string) (*Result, error) {
		if fn == nil {
			return nil, fmt.Errorf("missing workspace command handler")
		}
		output, err := fn(args)
		if err != nil {
			return nil, err
		}
		return &Result{Reply: output}, nil
	}
}
