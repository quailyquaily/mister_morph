package cron

import (
	"context"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/secref"
	"github.com/quailyquaily/mistermorph/internal/shellenv"
	"gopkg.in/yaml.v3"
)

// BashEnvRef injects one environment variable into bash for a cron awareness run.
type BashEnvRef struct {
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value" json:"value"`
}

type BashEnvResolveOptions struct {
	Source secref.Source
	Warnf  func(format string, args ...any)
}

func (b *BashEnvRef) UnmarshalYAML(node *yaml.Node) error {
	if node == nil {
		return fmt.Errorf("bash_env entry is nil")
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("bash_env entry must be a mapping")
	}
	ref := BashEnvRef{}
	shorthand := false
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := strings.TrimSpace(node.Content[i].Value)
		valNode := node.Content[i+1]
		switch key {
		case "name":
			if shorthand {
				return fmt.Errorf("bash_env entry cannot mix shorthand and name/value fields")
			}
			ref.Name = yamlScalarString(valNode)
		case "value":
			if shorthand {
				return fmt.Errorf("bash_env entry cannot mix shorthand and name/value fields")
			}
			ref.Value = yamlScalarString(valNode)
		default:
			if ref.Name != "" || ref.Value != "" {
				return fmt.Errorf("bash_env entry cannot mix shorthand and name/value fields")
			}
			if key == "" {
				continue
			}
			ref.Name = key
			ref.Value = yamlScalarString(valNode)
			shorthand = true
		}
	}
	*b = ref
	return nil
}

func yamlScalarString(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yaml.ScalarNode {
		return node.Value
	}
	return ""
}

func validateBashEnvRefs(refs []BashEnvRef) error {
	seen := map[string]bool{}
	for i, ref := range refs {
		name := shellenv.NormalizeName(strings.TrimSpace(ref.Name))
		if name == "" {
			return fmt.Errorf("bash_env[%d] name is required", i)
		}
		if seen[name] {
			return fmt.Errorf("duplicate bash_env name: %s", name)
		}
		seen[name] = true
	}
	return nil
}

// ResolveBashEnvRefsWithOptions expands supported secret references in values at run time.
func ResolveBashEnvRefsWithOptions(refs []BashEnvRef, opts BashEnvResolveOptions) ([]shellenv.InjectedEnvVar, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	source := opts.Source
	if source == nil {
		source = configutil.DefaultSecretRefSource()
	}
	resolver := secref.NewResolver(source)
	out := make([]shellenv.InjectedEnvVar, 0, len(refs))
	for i, ref := range refs {
		name := shellenv.NormalizeName(strings.TrimSpace(ref.Name))
		if name == "" {
			return nil, fmt.Errorf("bash_env[%d] name is required", i)
		}
		resolved, err := resolver.ResolveString(context.Background(), ref.Value, secref.Options{
			EnvMissing: secref.EnvMissingError,
		})
		if err != nil {
			if missingErr, ok := err.(secref.MissingEnvError); ok {
				return nil, fmt.Errorf("bash_env[%d] unset environment variable(s): %s", i, strings.Join(missingErr.Names, ", "))
			}
			return nil, fmt.Errorf("bash_env[%d] resolve refs: %w", i, err)
		}
		if opts.Warnf != nil {
			for _, warning := range resolved.Warnings {
				opts.Warnf("bash_env[%d]: %s; replaced with empty string", i, warning.String())
			}
		}
		out = append(out, shellenv.InjectedEnvVar{Name: name, Value: resolved.Value})
	}
	return out, nil
}
