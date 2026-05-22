package topiccontext

import (
	"context"
	"strings"
)

type Scope struct {
	Runtime         string
	ConversationKey string
	TopicID         string
}

type scopeContextKey struct{}

func WithScope(ctx context.Context, scope Scope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	scope.Runtime = strings.TrimSpace(scope.Runtime)
	scope.ConversationKey = strings.TrimSpace(scope.ConversationKey)
	scope.TopicID = strings.TrimSpace(scope.TopicID)
	if scope.ConversationKey == "" {
		return ctx
	}
	return context.WithValue(ctx, scopeContextKey{}, scope)
}

func ScopeFromContext(ctx context.Context) (Scope, bool) {
	if ctx == nil {
		return Scope{}, false
	}
	scope, ok := ctx.Value(scopeContextKey{}).(Scope)
	if !ok || strings.TrimSpace(scope.ConversationKey) == "" {
		return Scope{}, false
	}
	scope.Runtime = strings.TrimSpace(scope.Runtime)
	scope.ConversationKey = strings.TrimSpace(scope.ConversationKey)
	scope.TopicID = strings.TrimSpace(scope.TopicID)
	return scope, true
}
