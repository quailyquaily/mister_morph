package guard

import "context"

type ctxKeyNetworkPolicy struct{}

type NetworkPolicy struct {
	AllowedURLPrefixes []string
	DenyPrivateIPs     bool
	FollowRedirects    bool
	AllowProxy         bool
}

func WithNetworkPolicy(ctx context.Context, p NetworkPolicy) context.Context {
	return context.WithValue(ctx, ctxKeyNetworkPolicy{}, p)
}

func NetworkPolicyFromContext(ctx context.Context) (NetworkPolicy, bool) {
	if ctx == nil {
		return NetworkPolicy{}, false
	}
	v := ctx.Value(ctxKeyNetworkPolicy{})
	p, ok := v.(NetworkPolicy)
	return p, ok
}
