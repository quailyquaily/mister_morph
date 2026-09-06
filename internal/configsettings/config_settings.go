package configsettings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/configbootstrap"
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/configrevision"
	"github.com/quailyquaily/mistermorph/internal/secref"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type Kind string

const (
	KindString     Kind = "string"
	KindDuration   Kind = "duration"
	KindBool       Kind = "bool"
	KindInt        Kind = "int"
	KindFloat      Kind = "float"
	KindStringList Kind = "string_list"
	KindStringMap  Kind = "string_map"
	KindJSON       Kind = "json"
)

type ApplyMode string

const (
	ApplyImmediate      ApplyMode = "immediate"
	ApplyNextGeneration ApplyMode = "next_generation"
	ApplyRuntimeRestart ApplyMode = "runtime_restart"
	ApplyProcessRestart ApplyMode = "process_restart"
)

type Source string

const (
	SourceDefault             Source = "default"
	SourceFile                Source = "file"
	SourceEnvironmentOverride Source = "environment_override"
	SourceConfigEnvRef        Source = "config_env_ref"
	SourceConfigAWSRef        Source = "config_aws_ref"
	SourceConfigOSRef         Source = "config_os_ref"
	SourceRuntimeOverride     Source = "runtime_override"
)

type Field struct {
	Path      string
	Kind      Kind
	Sensitive bool
	ApplyMode ApplyMode
	Enum      []string
	Min       *float64
	Max       *float64
	Default   any
}

type FieldState struct {
	Source     Source    `json:"source"`
	Explicit   bool      `json:"explicit"`
	Configured bool      `json:"configured,omitempty"`
	Sensitive  bool      `json:"sensitive,omitempty"`
	Editable   bool      `json:"editable"`
	ApplyMode  ApplyMode `json:"apply_mode"`
	EnvName    string    `json:"env_name,omitempty"`
}

type ViewPayload struct {
	Values         map[string]any        `json:"config_values"`
	FieldStates    map[string]FieldState `json:"field_states"`
	ConfigRevision string                `json:"config_revision"`
}

type Update struct {
	Changes map[string]json.RawMessage `json:"config_changes,omitempty"`
	Reset   []string                   `json:"reset,omitempty"`
}

func ApplyRuntimeOverrides(view *ViewPayload, overrides map[string]any) {
	if view == nil {
		return
	}
	for path, value := range overrides {
		state, ok := view.FieldStates[path]
		if !ok {
			continue
		}
		state.Source = SourceRuntimeOverride
		state.Editable = false
		if state.Sensitive {
			state.Configured = strings.TrimSpace(fmt.Sprint(value)) != ""
			value = ""
		}
		view.FieldStates[path] = state
		view.Values[path] = value
	}
}

func RejectRuntimeOverrideUpdate(update Update, overrides map[string]any) error {
	for path := range update.Changes {
		if _, managed := overrides[path]; managed {
			return fmt.Errorf("%s is managed by a command-line flag", path)
		}
	}
	for _, path := range update.Reset {
		if _, managed := overrides[path]; managed {
			return fmt.Errorf("%s is managed by a command-line flag", path)
		}
	}
	return nil
}

type ApplyResult struct {
	ApplyMode      ApplyMode `json:"apply_mode"`
	ApplyStatus    string    `json:"apply_status"`
	RestartTargets []string  `json:"restart_targets,omitempty"`
}

func ResultForUpdate(update Update, fields []Field, restartTargets []string, additionalModes ...ApplyMode) ApplyResult {
	modes := make(map[string]ApplyMode, len(fields))
	for _, field := range fields {
		modes[field.Path] = field.ApplyMode
	}
	mode := ApplyImmediate
	for path := range update.Changes {
		mode = strongerApplyMode(mode, modes[path])
	}
	for _, path := range update.Reset {
		mode = strongerApplyMode(mode, modes[path])
	}
	for _, additional := range additionalModes {
		mode = strongerApplyMode(mode, additional)
	}
	result := ApplyResult{ApplyMode: mode, ApplyStatus: "pending"}
	if mode == ApplyImmediate {
		result.ApplyStatus = "applied"
	}
	if mode == ApplyRuntimeRestart || mode == ApplyProcessRestart {
		result.RestartTargets = restartTargets
	}
	return result
}

func strongerApplyMode(left, right ApplyMode) ApplyMode {
	rank := func(mode ApplyMode) int {
		switch mode {
		case ApplyProcessRestart:
			return 4
		case ApplyRuntimeRestart:
			return 3
		case ApplyNextGeneration:
			return 2
		case ApplyImmediate:
			return 1
		default:
			return 0
		}
	}
	if rank(right) > rank(left) {
		return right
	}
	return left
}

// ProtectSecrets stores changed scalar secrets outside config.yaml and replaces
// their submitted values with opaque references. The update is changed only
// after every store write succeeds.
func ProtectSecrets(ctx context.Context, update *Update, fields []Field, store secref.OSStore) ([]string, error) {
	if update == nil || store == nil || len(update.Changes) == 0 {
		return nil, nil
	}
	allowed := make(map[string]Field, len(fields))
	for _, field := range fields {
		allowed[field.Path] = field
	}
	paths := make([]string, 0, len(update.Changes))
	for path := range update.Changes {
		field, ok := allowed[path]
		if ok && field.Sensitive && field.Kind == KindString {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	type replacement struct {
		path string
		ref  json.RawMessage
	}
	created := make([]string, 0, len(paths))
	replacements := make([]replacement, 0, len(paths))
	for _, path := range paths {
		var value string
		if err := json.Unmarshal(update.Changes[path], &value); err != nil {
			return nil, fmt.Errorf("%s: must be a string", path)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := secref.ParseSingleRef(value); ok {
			continue
		}
		id, err := secref.NewOSSecretID()
		if err != nil {
			secref.DeleteOSSecrets(ctx, store, created)
			return nil, err
		}
		if err := store.Put(ctx, id, path, []byte(value)); err != nil {
			secref.DeleteOSSecrets(ctx, store, created)
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		encoded, err := json.Marshal(secref.OSSecretRef(id))
		if err != nil {
			secref.DeleteOSSecrets(ctx, store, append(created, id))
			return nil, err
		}
		created = append(created, id)
		replacements = append(replacements, replacement{path: path, ref: encoded})
	}
	for _, replacement := range replacements {
		update.Changes[replacement.path] = replacement.ref
	}
	return created, nil
}

func ReadFile(path string, fields []Field) (ViewPayload, error) {
	snapshot, err := configrevision.Read(path)
	if err != nil {
		return ViewPayload{}, err
	}
	view, err := View(snapshot.Data, fields)
	if err != nil {
		return ViewPayload{}, err
	}
	view.ConfigRevision = snapshot.Revision
	return view, nil
}

func View(raw []byte, fields []Field) (ViewPayload, error) {
	doc, err := configbootstrap.LoadDocumentBytes(raw)
	if err != nil {
		return ViewPayload{}, err
	}
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return ViewPayload{}, err
	}
	reader := viper.New()
	configdefaults.Apply(reader)
	reader.SetEnvPrefix("MISTER_MORPH")
	reader.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	reader.AutomaticEnv()
	reader.SetConfigType("yaml")
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := reader.ReadConfig(bytes.NewReader(raw)); err != nil {
			return ViewPayload{}, fmt.Errorf("invalid config yaml: %w", err)
		}
	}

	values := make(map[string]any, len(fields))
	states := make(map[string]FieldState, len(fields))
	for _, field := range fields {
		if err := validateField(field); err != nil {
			return ViewPayload{}, err
		}
		node := findPath(root, field.Path)
		explicit := node != nil
		envName := fieldEnvName(field.Path)
		_, envSet := os.LookupEnv(envName)
		state := FieldState{
			Source:    SourceDefault,
			Explicit:  explicit,
			Sensitive: field.Sensitive,
			Editable:  !envSet,
			ApplyMode: field.ApplyMode,
		}
		if envSet {
			state.Source = SourceEnvironmentOverride
			state.EnvName = envName
		} else if explicit {
			state.Source, state.EnvName = sourceForNode(node)
		}
		value, err := fieldValue(reader, node, field, envSet)
		if err != nil {
			return ViewPayload{}, fmt.Errorf("%s: %w", field.Path, err)
		}
		if field.Sensitive {
			state.Configured = configuredValue(value, node, envSet, reader, field)
			value = ""
		}
		values[field.Path] = value
		states[field.Path] = state
	}
	return ViewPayload{Values: values, FieldStates: states}, nil
}

func Apply(raw []byte, update Update, fields []Field) ([]byte, error) {
	allowed := make(map[string]Field, len(fields))
	for _, field := range fields {
		if err := validateField(field); err != nil {
			return nil, err
		}
		allowed[field.Path] = field
	}
	reset := make(map[string]struct{}, len(update.Reset))
	for _, rawPath := range update.Reset {
		path := strings.TrimSpace(rawPath)
		field, ok := allowed[path]
		if !ok {
			return nil, fmt.Errorf("unsupported config field %q", path)
		}
		if _, exists := update.Changes[path]; exists {
			return nil, fmt.Errorf("%s cannot be changed and reset together", path)
		}
		if envName := activeFieldEnvName(field); envName != "" {
			return nil, fmt.Errorf("%s is managed by %s", path, envName)
		}
		reset[path] = struct{}{}
	}

	paths := make([]string, 0, len(update.Changes))
	decoded := make(map[string]any, len(update.Changes))
	for path, rawValue := range update.Changes {
		field, ok := allowed[path]
		if !ok {
			return nil, fmt.Errorf("unsupported config field %q", path)
		}
		if envName := activeFieldEnvName(field); envName != "" {
			return nil, fmt.Errorf("%s is managed by %s", path, envName)
		}
		value, err := decodeValue(rawValue, field)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		paths = append(paths, path)
		decoded[path] = value
	}
	sort.Strings(paths)

	doc, err := configbootstrap.LoadDocumentBytes(raw)
	if err != nil {
		return nil, err
	}
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return nil, err
	}
	for path := range reset {
		deletePath(root, strings.Split(path, "."))
	}
	for _, path := range paths {
		if err := setPath(root, strings.Split(path, "."), decoded[path]); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return configbootstrap.MarshalDocument(doc)
}

func validateField(field Field) error {
	if strings.TrimSpace(field.Path) == "" || strings.Contains(field.Path, "..") {
		return fmt.Errorf("invalid config field path %q", field.Path)
	}
	switch field.Kind {
	case KindString, KindDuration, KindBool, KindInt, KindFloat, KindStringList, KindStringMap, KindJSON:
		return nil
	default:
		return fmt.Errorf("%s has unsupported field kind %q", field.Path, field.Kind)
	}
}

func fieldEnvName(path string) string {
	name := strings.NewReplacer("-", "_", ".", "_").Replace(path)
	return "MISTER_MORPH_" + strings.ToUpper(name)
}

func activeFieldEnvName(field Field) string {
	name := fieldEnvName(field.Path)
	if _, ok := os.LookupEnv(name); ok {
		return name
	}
	return ""
}

func sourceForNode(node *yaml.Node) (Source, string) {
	if node == nil || node.Kind != yaml.ScalarNode {
		return SourceFile, ""
	}
	ref, ok := secref.ParseSingleRef(strings.TrimSpace(node.Value))
	if !ok {
		return SourceFile, ""
	}
	switch ref.Kind {
	case secref.RefKindEnv:
		return SourceConfigEnvRef, ref.EnvName
	case secref.RefKindAWSSecretsManager:
		return SourceConfigAWSRef, ""
	case secref.RefKindOS:
		return SourceConfigOSRef, ""
	default:
		return SourceFile, ""
	}
}

func fieldValue(reader *viper.Viper, node *yaml.Node, field Field, envSet bool) (any, error) {
	if node != nil && !envSet {
		return decodeYAMLNode(node, field)
	}
	if field.Default != nil && !reader.IsSet(field.Path) {
		return field.Default, nil
	}
	switch field.Kind {
	case KindString, KindDuration:
		return reader.GetString(field.Path), nil
	case KindBool:
		return reader.GetBool(field.Path), nil
	case KindInt:
		return reader.GetInt(field.Path), nil
	case KindFloat:
		return reader.GetFloat64(field.Path), nil
	case KindStringList:
		return reader.GetStringSlice(field.Path), nil
	case KindStringMap:
		return reader.GetStringMapString(field.Path), nil
	case KindJSON:
		return reader.Get(field.Path), nil
	default:
		return nil, fmt.Errorf("unsupported field kind %q", field.Kind)
	}
}

func decodeYAMLNode(node *yaml.Node, field Field) (any, error) {
	var target any
	switch field.Kind {
	case KindString, KindDuration:
		target = new(string)
	case KindBool:
		target = new(bool)
	case KindInt:
		target = new(int)
	case KindFloat:
		target = new(float64)
	case KindStringList:
		target = new([]string)
	case KindStringMap:
		target = new(map[string]string)
	case KindJSON:
		target = new(any)
	}
	if err := node.Decode(target); err != nil {
		return nil, err
	}
	switch value := target.(type) {
	case *string:
		return *value, nil
	case *bool:
		return *value, nil
	case *int:
		return *value, nil
	case *float64:
		return *value, nil
	case *[]string:
		return *value, nil
	case *map[string]string:
		return *value, nil
	case *any:
		return *value, nil
	default:
		return nil, fmt.Errorf("unsupported field kind %q", field.Kind)
	}
}

func configuredValue(value any, node *yaml.Node, envSet bool, reader *viper.Viper, field Field) bool {
	if envSet {
		return strings.TrimSpace(reader.GetString(field.Path)) != ""
	}
	if node != nil && node.Kind == yaml.ScalarNode {
		return strings.TrimSpace(node.Value) != ""
	}
	return strings.TrimSpace(fmt.Sprint(value)) != ""
}

func decodeValue(raw json.RawMessage, field Field) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid json value")
	}
	switch field.Kind {
	case KindString, KindDuration:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("must be a string")
		}
		if field.Kind == KindDuration {
			if _, err := time.ParseDuration(text); err != nil {
				return nil, fmt.Errorf("must be a Go duration")
			}
		}
		if len(field.Enum) > 0 && !containsString(field.Enum, text) {
			return nil, fmt.Errorf("must be one of %s", strings.Join(field.Enum, ", "))
		}
		return text, nil
	case KindBool:
		result, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("must be a boolean")
		}
		return result, nil
	case KindInt:
		number, ok := value.(json.Number)
		if !ok {
			return nil, fmt.Errorf("must be an integer")
		}
		result, err := strconv.ParseInt(number.String(), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("must be an integer")
		}
		if err := validateRange(float64(result), field); err != nil {
			return nil, err
		}
		return result, nil
	case KindFloat:
		number, ok := value.(json.Number)
		if !ok {
			return nil, fmt.Errorf("must be a number")
		}
		result, err := strconv.ParseFloat(number.String(), 64)
		if err != nil || math.IsNaN(result) || math.IsInf(result, 0) {
			return nil, fmt.Errorf("must be a finite number")
		}
		if err := validateRange(result, field); err != nil {
			return nil, err
		}
		return result, nil
	case KindStringList:
		items, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("must be a string array")
		}
		result := make([]string, len(items))
		for i, item := range items {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("item %d must be a string", i+1)
			}
			result[i] = text
		}
		return result, nil
	case KindStringMap:
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("must be a string object")
		}
		result := make(map[string]string, len(object))
		for key, item := range object {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be a string", key)
			}
			result[key] = text
		}
		return result, nil
	case KindJSON:
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported field kind %q", field.Kind)
	}
}

func validateRange(value float64, field Field) error {
	if field.Min != nil && value < *field.Min {
		return fmt.Errorf("must be at least %v", *field.Min)
	}
	if field.Max != nil && value > *field.Max {
		return fmt.Errorf("must be at most %v", *field.Max)
	}
	return nil
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if value == item {
			return true
		}
	}
	return false
}

func findPath(root *yaml.Node, path string) *yaml.Node {
	node := root
	for _, part := range strings.Split(path, ".") {
		node = configbootstrap.FindMappingValue(node, part)
		if node == nil {
			return nil
		}
	}
	return node
}

func setPath(root *yaml.Node, parts []string, value any) error {
	if len(parts) == 0 {
		return fmt.Errorf("empty path")
	}
	parent := root
	for _, part := range parts[:len(parts)-1] {
		parent = configbootstrap.EnsureMappingValue(parent, part)
		if parent == nil {
			return fmt.Errorf("parent %q is not a mapping", part)
		}
	}
	var replacement yaml.Node
	if err := replacement.Encode(value); err != nil {
		return err
	}
	key := parts[len(parts)-1]
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if !strings.EqualFold(strings.TrimSpace(parent.Content[i].Value), key) {
			continue
		}
		current := parent.Content[i+1]
		replacement.HeadComment = current.HeadComment
		replacement.LineComment = current.LineComment
		replacement.FootComment = current.FootComment
		parent.Content[i+1] = &replacement
		return nil
	}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&replacement,
	)
	return nil
}

func deletePath(root *yaml.Node, parts []string) bool {
	if root == nil || root.Kind != yaml.MappingNode || len(parts) == 0 {
		return false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if !strings.EqualFold(strings.TrimSpace(root.Content[i].Value), parts[0]) {
			continue
		}
		if len(parts) == 1 {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			return true
		}
		child := root.Content[i+1]
		if !deletePath(child, parts[1:]) {
			return false
		}
		if child.Kind == yaml.MappingNode && len(child.Content) == 0 {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
		}
		return true
	}
	return false
}
