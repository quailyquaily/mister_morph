package secrets

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type Resolver interface {
	Resolve(ctx context.Context, secretRef string) (string, error)
}

// EnvResolver resolves secrets from environment variables.
//
// The MVP behavior is fail-closed:
// - missing/unset env var => error
// - empty value => error
type EnvResolver struct {
	Aliases map[string]string
}

func (r *EnvResolver) Resolve(ctx context.Context, secretRef string) (string, error) {
	_ = ctx

	ref := strings.TrimSpace(secretRef)
	if ref == "" {
		return "", fmt.Errorf("empty secret_ref")
	}

	envName := ref
	if r != nil && r.Aliases != nil {
		if v, ok := r.Aliases[ref]; ok && strings.TrimSpace(v) != "" {
			envName = strings.TrimSpace(v)
		}
	}

	val, ok := os.LookupEnv(envName)
	if !ok {
		return "", fmt.Errorf("secret not found (env var %q is not set)", envName)
	}
	if strings.TrimSpace(val) == "" {
		return "", fmt.Errorf("secret is empty (env var %q)", envName)
	}
	return val, nil
}
