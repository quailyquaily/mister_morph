package awareness

import "context"

// Notifier is an optional adapter for delivering awareness messages.
// The payload is intentionally minimal to keep awareness runtime decoupled
// from transport-specific concepts.
type Notifier interface {
	Notify(ctx context.Context, text string) error
}

// NotifyFunc adapts a function into Notifier.
type NotifyFunc func(ctx context.Context, text string) error

func (f NotifyFunc) Notify(ctx context.Context, text string) error {
	if f == nil {
		return nil
	}
	return f(ctx, text)
}
