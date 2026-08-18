package secref

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var (
	envNameRe     = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	placeholderRe = regexp.MustCompile(`\$\{([^}]*)\}`)

	ErrAWSSecretNotFound = errors.New("aws secret not found")
)

type EnvMissingPolicy int

const (
	EnvMissingWarn EnvMissingPolicy = iota
	EnvMissingError
)

type Options struct {
	EnvMissing EnvMissingPolicy
}

type Source interface {
	LookupEnv(name string) (string, bool)
	GetAWSSecretString(ctx context.Context, secretID string) (string, error)
}

type RefKind string

const (
	RefKindEnv               RefKind = "env"
	RefKindAWSSecretsManager RefKind = "aws_secrets_manager"
)

type Ref struct {
	Kind     RefKind
	EnvName  string
	SecretID string
	Field    string
}

type Result struct {
	Value      string
	MissingEnv []string
	Warnings   []Warning
}

type Warning struct {
	Source   string
	SecretID string
	Field    string
	Reason   string
}

func (w Warning) String() string {
	parts := []string{"secret ref warning"}
	if source := strings.TrimSpace(w.Source); source != "" {
		parts = append(parts, "source="+source)
	}
	if secretID := strings.TrimSpace(w.SecretID); secretID != "" {
		parts = append(parts, "secret_id="+secretID)
	}
	if field := strings.TrimSpace(w.Field); field != "" {
		parts = append(parts, "field="+field)
	}
	if reason := strings.TrimSpace(w.Reason); reason != "" {
		parts = append(parts, "reason="+reason)
	}
	return strings.Join(parts, " ")
}

type MissingEnvError struct {
	Names []string
}

func (e MissingEnvError) Error() string {
	names := append([]string(nil), e.Names...)
	sort.Strings(names)
	return fmt.Sprintf("missing environment variable(s): %s", strings.Join(names, ", "))
}

type Resolver struct {
	source   Source
	awsCache map[string]awsCacheEntry
}

type awsCacheEntry struct {
	value string
	err   error
}

func NewResolver(source Source) *Resolver {
	if source == nil {
		source = NewDefaultSource(AWSSecretsManagerConfig{})
	}
	return &Resolver{
		source:   source,
		awsCache: map[string]awsCacheEntry{},
	}
}

func ResolveString(ctx context.Context, value string, source Source, opts Options) (Result, error) {
	return NewResolver(source).ResolveString(ctx, value, opts)
}

func ParseSingleRef(value string) (Ref, bool) {
	value = strings.TrimSpace(value)
	groups := placeholderRe.FindStringSubmatch(value)
	if len(groups) != 2 || groups[0] != value {
		return Ref{}, false
	}
	body := strings.TrimSpace(groups[1])
	if envNameRe.MatchString(body) {
		return Ref{Kind: RefKindEnv, EnvName: body}, true
	}
	if strings.HasPrefix(body, "aws-sm:") {
		secretID, field, _ := strings.Cut(strings.TrimPrefix(body, "aws-sm:"), "#")
		secretID = strings.TrimSpace(secretID)
		if secretID == "" {
			return Ref{}, false
		}
		return Ref{
			Kind:     RefKindAWSSecretsManager,
			SecretID: secretID,
			Field:    strings.TrimSpace(field),
		}, true
	}
	return Ref{}, false
}

func (r *Resolver) ResolveString(ctx context.Context, value string, opts Options) (Result, error) {
	var result Result
	result.Value = placeholderRe.ReplaceAllStringFunc(value, func(match string) string {
		groups := placeholderRe.FindStringSubmatch(match)
		if len(groups) != 2 {
			return match
		}
		body := strings.TrimSpace(groups[1])
		if envNameRe.MatchString(body) {
			resolved, ok := r.source.LookupEnv(body)
			if !ok {
				result.MissingEnv = append(result.MissingEnv, body)
				return ""
			}
			return resolved
		}
		if strings.HasPrefix(body, "aws-sm:") {
			return r.resolveAWSRef(ctx, strings.TrimPrefix(body, "aws-sm:"), &result)
		}
		return match
	})
	if opts.EnvMissing == EnvMissingError && len(result.MissingEnv) > 0 {
		return result, MissingEnvError{Names: result.MissingEnv}
	}
	return result, nil
}

func (r *Resolver) resolveAWSRef(ctx context.Context, body string, result *Result) string {
	secretID, field, _ := strings.Cut(body, "#")
	secretID = strings.TrimSpace(secretID)
	field = strings.TrimSpace(field)
	if secretID == "" {
		result.Warnings = append(result.Warnings, Warning{
			Source: "aws_secrets_manager",
			Field:  field,
			Reason: "empty secret id",
		})
		return ""
	}
	secret, err := r.getAWSSecretString(ctx, secretID)
	if err != nil {
		result.Warnings = append(result.Warnings, Warning{
			Source:   "aws_secrets_manager",
			SecretID: secretID,
			Field:    field,
			Reason:   "aws secret fetch failed",
		})
		return ""
	}
	if field == "" {
		return secret
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(secret), &payload); err != nil {
		result.Warnings = append(result.Warnings, Warning{
			Source:   "aws_secrets_manager",
			SecretID: secretID,
			Field:    field,
			Reason:   "secret string is not a json object",
		})
		return ""
	}
	value, ok := payload[field]
	if !ok {
		result.Warnings = append(result.Warnings, Warning{
			Source:   "aws_secrets_manager",
			SecretID: secretID,
			Field:    field,
			Reason:   "json field missing",
		})
		return ""
	}
	str, ok := value.(string)
	if !ok {
		result.Warnings = append(result.Warnings, Warning{
			Source:   "aws_secrets_manager",
			SecretID: secretID,
			Field:    field,
			Reason:   "json field is not a string",
		})
		return ""
	}
	return str
}

func (r *Resolver) getAWSSecretString(ctx context.Context, secretID string) (string, error) {
	if entry, ok := r.awsCache[secretID]; ok {
		return entry.value, entry.err
	}
	value, err := r.source.GetAWSSecretString(ctx, secretID)
	r.awsCache[secretID] = awsCacheEntry{value: value, err: err}
	return value, err
}

type EnvSource struct{}

func (EnvSource) LookupEnv(name string) (string, bool) {
	return os.LookupEnv(name)
}

func (EnvSource) GetAWSSecretString(context.Context, string) (string, error) {
	return "", ErrAWSSecretNotFound
}
