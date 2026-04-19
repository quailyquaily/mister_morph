package chatcommands

import (
	"context"
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

// EchoHandler returns a Handler that echoes back its arguments.
func EchoHandler() Handler {
	return func(ctx context.Context, args string) (*Result, error) {
		if args == "" {
			return &Result{Reply: "usage: /echo <msg>"}, nil
		}
		return &Result{Reply: args}, nil
	}
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
	return func(ctx context.Context, text string) (*Result, error) {
		return m.Handle(ctx, text)
	}
}
