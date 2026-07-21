package tools

import "context"

type Tool interface {
	Name() string
	Description() string
	ParameterSchema() string
	Execute(ctx context.Context, params map[string]any) (string, error)
}

// ParallelSafe is an optional capability for tools that are read-only and
// safe to execute concurrently with other ParallelSafe tools in one batch.
// Tools without this capability run in provider order.
type ParallelSafe interface {
	ParallelSafe() bool
}
