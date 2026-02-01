package secrets

import (
	"context"
	"strings"
)

type SkillAuthProfilePolicy struct {
	Allowed map[string]bool
	Enforce bool
}

type ctxKeySkillAuthProfilePolicy struct{}

func WithSkillAuthProfilePolicy(ctx context.Context, allowed []string, enforce bool) context.Context {
	p := SkillAuthProfilePolicy{
		Allowed: make(map[string]bool),
		Enforce: enforce,
	}
	for _, id := range allowed {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		p.Allowed[id] = true
	}
	return context.WithValue(ctx, ctxKeySkillAuthProfilePolicy{}, p)
}

func SkillAuthProfilePolicyFromContext(ctx context.Context) (SkillAuthProfilePolicy, bool) {
	if ctx == nil {
		return SkillAuthProfilePolicy{}, false
	}
	v := ctx.Value(ctxKeySkillAuthProfilePolicy{})
	if v == nil {
		return SkillAuthProfilePolicy{}, false
	}
	p, ok := v.(SkillAuthProfilePolicy)
	return p, ok
}
