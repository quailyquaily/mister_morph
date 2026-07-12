package configutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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

// ReadExpandedConfig reads a config file, expands ${ENV_VAR} and secret
// references in scalar values, then feeds the result into the provided viper
// instance.
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
	var patches []yamlScalarPatch
	lineStarts := yamlLineStarts(raw)
	if err := expandYAMLScalarNodeRefs(ctx, &node, resolver, &out, raw, lineStarts, false, &patches); err != nil {
		return out, "", err
	}
	expanded, err := applyYAMLScalarPatches(raw, patches)
	if err != nil {
		return out, "", err
	}
	out.Value = expanded
	return out, out.Value, nil
}

type yamlScalarPatch struct {
	start       int
	end         int
	replacement string
}

func expandYAMLScalarNodeRefs(ctx context.Context, node *yaml.Node, resolver *secref.Resolver, out *secref.Result, raw string, lineStarts []int, inFlow bool, patches *[]yamlScalarPatch) error {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for i := range node.Content {
			if err := expandYAMLScalarNodeRefs(ctx, node.Content[i], resolver, out, raw, lineStarts, inFlow, patches); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		childInFlow := inFlow || node.Style&yaml.FlowStyle != 0
		for i := 1; i < len(node.Content); i += 2 {
			if err := expandYAMLScalarNodeRefs(ctx, node.Content[i], resolver, out, raw, lineStarts, childInFlow, patches); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		childInFlow := inFlow || node.Style&yaml.FlowStyle != 0
		for i := range node.Content {
			if err := expandYAMLScalarNodeRefs(ctx, node.Content[i], resolver, out, raw, lineStarts, childInFlow, patches); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		result, err := resolver.ResolveString(ctx, node.Value, secref.Options{EnvMissing: secref.EnvMissingWarn})
		if err != nil {
			return err
		}
		if result.Value != node.Value {
			patch, err := yamlScalarPatchForNode(raw, lineStarts, node, inFlow, result.Value)
			if err != nil {
				return err
			}
			*patches = append(*patches, patch)
		}
		out.MissingEnv = append(out.MissingEnv, result.MissingEnv...)
		out.Warnings = append(out.Warnings, result.Warnings...)
	}
	return nil
}

func yamlScalarPatchForNode(raw string, lineStarts []int, node *yaml.Node, inFlow bool, value string) (yamlScalarPatch, error) {
	start, err := yamlNodeOffset(raw, lineStarts, node)
	if err != nil {
		return yamlScalarPatch{}, err
	}
	end, blockScalar, err := scanYAMLScalarEnd(raw, lineStarts, start, node, inFlow)
	if err != nil {
		return yamlScalarPatch{}, err
	}
	replacement := quoteYAMLString(value)
	if blockScalar && end > start && raw[end-1] == '\n' {
		replacement += "\n"
	}
	return yamlScalarPatch{
		start:       start,
		end:         end,
		replacement: replacement,
	}, nil
}

func yamlLineStarts(raw string) []int {
	lineStarts := []int{0}
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\n' && i+1 < len(raw) {
			lineStarts = append(lineStarts, i+1)
		}
	}
	return lineStarts
}

func yamlNodeOffset(raw string, lineStarts []int, node *yaml.Node) (int, error) {
	if node.Line <= 0 || node.Line > len(lineStarts) {
		return 0, fmt.Errorf("yaml scalar has invalid line %d", node.Line)
	}
	if node.Column <= 0 {
		return 0, fmt.Errorf("yaml scalar has invalid column %d", node.Column)
	}
	offset := lineStarts[node.Line-1] + node.Column - 1
	if offset < 0 || offset >= len(raw) {
		return 0, fmt.Errorf("yaml scalar position is outside input: line=%d column=%d", node.Line, node.Column)
	}
	return offset, nil
}

func scanYAMLScalarEnd(raw string, lineStarts []int, start int, node *yaml.Node, inFlow bool) (int, bool, error) {
	switch raw[start] {
	case '"':
		end, err := scanDoubleQuotedScalarEnd(raw, start)
		return end, false, err
	case '\'':
		end, err := scanSingleQuotedScalarEnd(raw, start)
		return end, false, err
	case '|', '>':
		return scanBlockScalarEnd(raw, start, yamlLineIndent(raw, lineStarts, node.Line)), true, nil
	default:
		return scanPlainScalarEnd(raw, start, inFlow), false, nil
	}
}

func scanDoubleQuotedScalarEnd(raw string, start int) (int, error) {
	escaped := false
	for i := start + 1; i < len(raw); i++ {
		if escaped {
			escaped = false
			continue
		}
		switch raw[i] {
		case '\\':
			escaped = true
		case '"':
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("unterminated YAML double-quoted scalar at offset %d", start)
}

func scanSingleQuotedScalarEnd(raw string, start int) (int, error) {
	for i := start + 1; i < len(raw); i++ {
		if raw[i] != '\'' {
			continue
		}
		if i+1 < len(raw) && raw[i+1] == '\'' {
			i++
			continue
		}
		return i + 1, nil
	}
	return 0, fmt.Errorf("unterminated YAML single-quoted scalar at offset %d", start)
}

func scanBlockScalarEnd(raw string, start int, scalarIndent int) int {
	lineEnd := strings.IndexByte(raw[start:], '\n')
	if lineEnd < 0 {
		return len(raw)
	}
	pos := start + lineEnd + 1
	for pos < len(raw) {
		nextLineEnd := pos
		for nextLineEnd < len(raw) && raw[nextLineEnd] != '\n' {
			nextLineEnd++
		}
		line := raw[pos:nextLineEnd]
		if strings.TrimSpace(line) != "" && countLeadingSpaces(line) <= scalarIndent {
			return pos
		}
		if nextLineEnd == len(raw) {
			return len(raw)
		}
		pos = nextLineEnd + 1
	}
	return len(raw)
}

func scanPlainScalarEnd(raw string, start int, inFlow bool) int {
	for i := start; i < len(raw); i++ {
		switch raw[i] {
		case '\n', '\r':
			return trimYAMLScalarTrailingSpace(raw, start, i)
		case '#':
			if i == start || raw[i-1] == ' ' || raw[i-1] == '\t' {
				return trimYAMLScalarTrailingSpace(raw, start, i)
			}
		case ',', ']', '}':
			if inFlow {
				return trimYAMLScalarTrailingSpace(raw, start, i)
			}
		}
	}
	return trimYAMLScalarTrailingSpace(raw, start, len(raw))
}

func trimYAMLScalarTrailingSpace(raw string, start, end int) int {
	for end > start && (raw[end-1] == ' ' || raw[end-1] == '\t') {
		end--
	}
	return end
}

func countLeadingSpaces(s string) int {
	count := 0
	for count < len(s) && s[count] == ' ' {
		count++
	}
	return count
}

func yamlLineIndent(raw string, lineStarts []int, line int) int {
	if line <= 0 || line > len(lineStarts) {
		return 0
	}
	lineStart := lineStarts[line-1]
	lineEnd := lineStart
	for lineEnd < len(raw) && raw[lineEnd] != '\n' && raw[lineEnd] != '\r' {
		lineEnd++
	}
	return countLeadingSpaces(raw[lineStart:lineEnd])
}

func quoteYAMLString(value string) string {
	return strconv.Quote(value)
}

func applyYAMLScalarPatches(raw string, patches []yamlScalarPatch) (string, error) {
	if len(patches) == 0 {
		return raw, nil
	}
	sort.Slice(patches, func(i, j int) bool {
		return patches[i].start < patches[j].start
	})
	for i, patch := range patches {
		if patch.start < 0 || patch.end < patch.start || patch.end > len(raw) {
			return "", fmt.Errorf("invalid YAML scalar patch range: start=%d end=%d", patch.start, patch.end)
		}
		if i > 0 && patch.start < patches[i-1].end {
			return "", fmt.Errorf("overlapping YAML scalar patch ranges")
		}
	}
	expanded := raw
	for i := len(patches) - 1; i >= 0; i-- {
		patch := patches[i]
		expanded = expanded[:patch.start] + patch.replacement + expanded[patch.end:]
	}
	return expanded, nil
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
