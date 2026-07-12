package configutil

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/secref"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// envVarRe matches only the ${NAME} form (not bare $NAME).
// This avoids corrupting values like bcrypt hashes ($2a$10$...) or
// regex patterns that contain literal dollar signs.
var envVarRe = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// expandStrictEnv replaces only ${VAR} references with their environment
// values. Bare $VAR references are left untouched.
// Returns the expanded string and a list of referenced-but-unset variable names.
func expandStrictEnv(s string) (string, []string) {
	var missing []string
	result := envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		name := envVarRe.FindStringSubmatch(match)[1]
		val, ok := os.LookupEnv(name)
		if !ok {
			missing = append(missing, name)
			return ""
		}
		return val
	})
	return result, missing
}

// ExpandStrictEnv replaces only ${VAR} references with their environment
// values. Bare $VAR references are left untouched.
func ExpandStrictEnv(s string) (string, []string) {
	return expandStrictEnv(s)
}

// ReadExpandedConfig reads a config file, expands only ${ENV_VAR}
// references in the raw text, then feeds the result into the provided
// viper instance.
//
// Unset environment variables are replaced with empty strings and
// reported via the optional warn callback. Pass nil to suppress warnings.
func ReadExpandedConfig(v *viper.Viper, path string, warn func(format string, args ...any)) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	awsCfg := awsSecretsManagerConfigFromRawYAML(raw, warn)
	return readExpandedConfigRaw(v, path, raw, secref.NewDefaultSource(awsCfg), warn)
}

func ReadExpandedConfigWithSource(v *viper.Viper, path string, source secref.Source, warn func(format string, args ...any)) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return readExpandedConfigRaw(v, path, raw, source, warn)
}

type secretRefConfigReader interface {
	GetString(string) string
}

func SecretRefSourceFromReader(reader secretRefConfigReader) secref.Source {
	return secref.NewDefaultSource(AWSSecretsManagerConfigFromReader(reader))
}

func DefaultSecretRefSource() secref.Source {
	return SecretRefSourceFromReader(viper.GetViper())
}

func AWSSecretsManagerConfigFromReader(reader secretRefConfigReader) secref.AWSSecretsManagerConfig {
	if reader == nil {
		return secref.AWSSecretsManagerConfig{}
	}
	return secref.AWSSecretsManagerConfig{
		Region:  strings.TrimSpace(reader.GetString("secrets.aws_secrets_manager.region")),
		Profile: strings.TrimSpace(reader.GetString("secrets.aws_secrets_manager.profile")),
	}
}

func readExpandedConfigRaw(v *viper.Viper, path string, raw []byte, source secref.Source, warn func(format string, args ...any)) error {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if ext == "" {
		ext = "yaml"
	}

	resolver := secref.NewResolver(source)
	var result secref.Result
	var expanded string
	var err error
	if isYAMLConfigType(ext) {
		result, expanded, err = expandYAMLScalarRefs(context.Background(), string(raw), resolver)
	} else {
		result, err = resolver.ResolveString(context.Background(), string(raw), secref.Options{
			EnvMissing: secref.EnvMissingWarn,
		})
		expanded = result.Value
	}
	if err != nil {
		return err
	}
	missing := result.MissingEnv
	if len(missing) > 0 && warn != nil {
		warn("config %s: unset environment variable(s) replaced with empty string: %s",
			filepath.Base(path), strings.Join(missing, ", "))
	}
	if len(result.Warnings) > 0 && warn != nil {
		for _, warning := range result.Warnings {
			warn("config %s: %s; replaced with empty string", filepath.Base(path), warning.String())
		}
	}
	v.SetConfigType(ext)
	return v.ReadConfig(strings.NewReader(expanded))
}

func isYAMLConfigType(ext string) bool {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case "yaml", "yml":
		return true
	default:
		return false
	}
}

func expandYAMLScalarRefs(ctx context.Context, raw string, resolver *secref.Resolver) (secref.Result, string, error) {
	if strings.TrimSpace(raw) == "" {
		return secref.Result{Value: ""}, raw, nil
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &node); err != nil {
		return secref.Result{}, "", err
	}
	var out secref.Result
	if err := expandYAMLScalarNodeRefs(ctx, &node, resolver, &out); err != nil {
		return out, "", err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		_ = enc.Close()
		return out, "", err
	}
	if err := enc.Close(); err != nil {
		return out, "", err
	}
	out.Value = buf.String()
	return out, out.Value, nil
}

func expandYAMLScalarNodeRefs(ctx context.Context, node *yaml.Node, resolver *secref.Resolver, out *secref.Result) error {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for i := range node.Content {
			if err := expandYAMLScalarNodeRefs(ctx, node.Content[i], resolver, out); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 1; i < len(node.Content); i += 2 {
			if err := expandYAMLScalarNodeRefs(ctx, node.Content[i], resolver, out); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i := range node.Content {
			if err := expandYAMLScalarNodeRefs(ctx, node.Content[i], resolver, out); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		result, err := resolver.ResolveString(ctx, node.Value, secref.Options{EnvMissing: secref.EnvMissingWarn})
		if err != nil {
			return err
		}
		if result.Value != node.Value {
			node.Value = result.Value
			node.Tag = "!!str"
		}
		out.MissingEnv = append(out.MissingEnv, result.MissingEnv...)
		out.Warnings = append(out.Warnings, result.Warnings...)
	}
	return nil
}

func awsSecretsManagerConfigFromRawYAML(raw []byte, warn func(format string, args ...any)) secref.AWSSecretsManagerConfig {
	var doc struct {
		Secrets struct {
			AWSSecretsManager struct {
				Region  string `yaml:"region"`
				Profile string `yaml:"profile"`
			} `yaml:"aws_secrets_manager"`
		} `yaml:"secrets"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		if warn != nil {
			warn("config: unable to read secrets.aws_secrets_manager bootstrap config: %v", err)
		}
		return secref.AWSSecretsManagerConfig{}
	}
	return secref.AWSSecretsManagerConfig{
		Region:  expandBootstrapEnv(doc.Secrets.AWSSecretsManager.Region, "secrets.aws_secrets_manager.region", warn),
		Profile: expandBootstrapEnv(doc.Secrets.AWSSecretsManager.Profile, "secrets.aws_secrets_manager.profile", warn),
	}
}

func expandBootstrapEnv(value, field string, warn func(format string, args ...any)) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "${aws-sm:") {
		if warn != nil {
			warn("config %s: AWS Secrets Manager refs are not supported in bootstrap config; using empty string", field)
		}
		return ""
	}
	expanded, missing := expandStrictEnv(value)
	if len(missing) > 0 && warn != nil {
		warn("config %s: unset environment variable(s) replaced with empty string: %s", field, strings.Join(missing, ", "))
	}
	return strings.TrimSpace(expanded)
}
