package chatcommands

import (
	"context"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
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

// ModelCommandFunc executes a /model command string and reports whether it was handled.
type ModelCommandFunc = func(text string) (output string, handled bool, err error)

// ModelCommandHandler adapts a /model command executor to the Registry Handler
// signature, whose input is only the argument tail after "/model".
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
	text := "/model"
	if args = strings.TrimSpace(args); args != "" {
		text += " " + args
	}
	return text
}

// ModelHandler wraps the llmselect package so that /model commands can be
// handled uniformly across chat front-ends.
//
// The raw text passed to the handler should be the full user message (e.g.
// "/model set foo" or "/model@Bot set foo"). The handler normalises the
// command word internally.
type ModelHandler struct {
	Values llmutil.RuntimeValues
	Store  *llmselect.Store
}

// NewModelHandler creates a ModelHandler backed by the given runtime values and
// selection store. If store is nil a fresh one is allocated.
func NewModelHandler(values llmutil.RuntimeValues, store *llmselect.Store) *ModelHandler {
	if store == nil {
		store = llmselect.NewStore()
	}
	return &ModelHandler{Values: values, Store: store}
}

// Handle implements the Handler signature for /model commands.
func (m *ModelHandler) Handle(ctx context.Context, text string) (*Result, error) {
	output, handled, err := llmselect.ExecuteCommandText(m.Values, m.Store, text)
	if !handled {
		return nil, nil
	}
	return &Result{Reply: output}, err
}

// AsHandler returns the model handler as a standard Handler closure so it can
// be registered in a Registry.
func (m *ModelHandler) AsHandler() Handler {
	return func(ctx context.Context, args string) (*Result, error) {
		return m.Handle(ctx, modelCommandText(args))
	}
}
