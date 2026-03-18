package consolecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

const (
	setupModeRequired       = "setup_required"
	setupModeReady          = "ready"
	setupLLMValidateTimeout = 20 * time.Second
)

type setupStatus struct {
	Mode          string
	MissingFields []string
}

type llmSetupValidator func(ctx context.Context, route llmutil.ResolvedRoute) error

type setupApplyRequest struct {
	LLM     *setupApplyLLMInput     `json:"llm"`
	Console *setupApplyConsoleInput `json:"console"`
}

type setupApplyLLMInput struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Endpoint  string `json:"endpoint"`
	APIKey    string `json:"api_key"`
	APIKeyRef string `json:"api_key_ref"`
}

type setupApplyConsoleInput struct {
	Password     string                  `json:"password"`
	PasswordHash string                  `json:"password_hash"`
	Endpoints    []setupApplyEndpointRaw `json:"endpoints"`
}

type setupApplyEndpointRaw struct {
	Name            string `json:"name"`
	URL             string `json:"url"`
	AuthToken       string `json:"auth_token"`
	AuthTokenEnvRef string `json:"auth_token_env_ref"`
}

type setupValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func defaultLLMSetupValidator(ctx context.Context, route llmutil.ResolvedRoute) error {
	client, err := llmutil.ClientFromConfigWithValues(route.ClientConfig, route.Values)
	if err != nil {
		return err
	}
	_, err = client.Chat(ctx, llm.Request{
		Model: route.ClientConfig.Model,
		Messages: []llm.Message{
			{Role: "user", Content: "ping"},
		},
		Parameters: map[string]any{
			"max_tokens": 8,
		},
	})
	return err
}

func evaluateSetupStatus(cfg serveConfig, passwordErr error) setupStatus {
	if !cfg.setupMode {
		return setupStatus{Mode: setupModeReady}
	}

	missingSet := map[string]struct{}{}
	if !cfg.authDisabled() {
		if _, err := newPasswordVerifier(cfg.password, cfg.passwordHash); err != nil || passwordErr != nil {
			missingSet["console.password_hash"] = struct{}{}
		}
	}
	if cfg.setupRequireLLM {
		for _, field := range missingLLMFields(llmutil.RuntimeValuesFromViper()) {
			missingSet[field] = struct{}{}
		}
	}

	missing := sortedSetKeys(missingSet)
	mode := setupModeReady
	if len(missing) > 0 {
		mode = setupModeRequired
	}
	return setupStatus{
		Mode:          mode,
		MissingFields: missing,
	}
}

func missingLLMFields(values llmutil.RuntimeValues) []string {
	missingSet := map[string]struct{}{}
	route, routeErr := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)

	provider := strings.TrimSpace(values.Provider)
	model := strings.TrimSpace(values.Model)
	apiKey := strings.TrimSpace(values.APIKey)
	cloudflareAccountID := strings.TrimSpace(values.CloudflareAccountID)
	cloudflareToken := firstNonEmptyString(values.CloudflareAPIToken, values.APIKey)
	bedrockAWSKey := strings.TrimSpace(values.BedrockAWSKey)
	bedrockAWSSecret := strings.TrimSpace(values.BedrockAWSSecret)
	bedrockRegion := strings.TrimSpace(values.BedrockAWSRegion)
	bedrockModelARN := strings.TrimSpace(values.BedrockModelARN)

	if routeErr == nil {
		provider = strings.TrimSpace(route.ClientConfig.Provider)
		model = strings.TrimSpace(route.ClientConfig.Model)
		apiKey = strings.TrimSpace(route.ClientConfig.APIKey)
		cloudflareToken = firstNonEmptyString(values.CloudflareAPIToken, apiKey)
	}

	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		missingSet["llm.provider"] = struct{}{}
		provider = "openai"
	}
	if strings.TrimSpace(model) == "" {
		missingSet["llm.model"] = struct{}{}
	}

	switch provider {
	case "cloudflare":
		if cloudflareAccountID == "" {
			missingSet["llm.cloudflare.account_id"] = struct{}{}
		}
		if strings.TrimSpace(cloudflareToken) == "" {
			missingSet["llm.cloudflare.api_token"] = struct{}{}
		}
	case "bedrock":
		if bedrockAWSKey == "" {
			missingSet["llm.bedrock.aws_key"] = struct{}{}
		}
		if bedrockAWSSecret == "" {
			missingSet["llm.bedrock.aws_secret"] = struct{}{}
		}
		if bedrockRegion == "" {
			missingSet["llm.bedrock.region"] = struct{}{}
		}
		if bedrockModelARN == "" {
			missingSet["llm.bedrock.model_arn"] = struct{}{}
		}
	default:
		if strings.TrimSpace(apiKey) == "" {
			missingSet["llm.api_key"] = struct{}{}
		}
	}

	return sortedSetKeys(missingSet)
}

func (s *server) isSetupRequired() bool {
	if s == nil || !s.cfg.setupMode {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(s.setupStatus.Mode), setupModeRequired)
}

func (s *server) writeSetupRequiredError(w http.ResponseWriter) {
	setupPath := s.cfg.basePath + "/api/setup/status"
	writeJSON(w, http.StatusConflict, map[string]any{
		"ok":         false,
		"code":       setupModeRequired,
		"message":    "initial setup is required",
		"setup_path": setupPath,
	})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	mode := setupModeReady
	if s.isSetupRequired() {
		mode = setupModeRequired
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"mode":           mode,
		"setup_required": s.isSetupRequired(),
	})
}

func (s *server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.setupMode {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"mode":           s.setupStatus.Mode,
		"missing_fields": append([]string(nil), s.setupStatus.MissingFields...),
		"config_path":    s.cfg.configPath,
	})
}

func (s *server) handleSetupApply(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.setupMode {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req setupApplyRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	errs, route, validateLLM := s.validateSetupApplyRequest(req)
	if len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":     false,
			"code":   "validation_failed",
			"errors": errs,
		})
		return
	}

	if validateLLM {
		ctx, cancel := context.WithTimeout(r.Context(), setupLLMValidateTimeout)
		err := s.llmValidator(ctx, route)
		cancel()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"ok":   false,
				"code": "validation_failed",
				"errors": []setupValidationError{
					{
						Field:   "llm.api_key",
						Message: outputfmt.SanitizeErrorText(err.Error()),
					},
				},
			})
			return
		}
	}

	applied, redacted, err := applySetupToConfig(s.cfg.configPath, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist setup config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"restart_required": true,
		"applied":          applied,
		"redacted":         redacted,
	})
}

func (s *server) validateSetupApplyRequest(req setupApplyRequest) ([]setupValidationError, llmutil.ResolvedRoute, bool) {
	errs := make([]setupValidationError, 0, 8)

	password := strings.TrimSpace(viper.GetString("console.password"))
	passwordHash := strings.TrimSpace(viper.GetString("console.password_hash"))
	if req.Console != nil {
		if v := strings.TrimSpace(req.Console.Password); v != "" {
			password = v
		}
		if v := strings.TrimSpace(req.Console.PasswordHash); v != "" {
			passwordHash = v
		}
	}
	if _, err := newPasswordVerifier(password, passwordHash); err != nil {
		errs = append(errs, setupValidationError{
			Field:   "console.password_hash",
			Message: err.Error(),
		})
	}

	endpointRaw := setupEndpointInputsToRuntimeRaw(nil)
	if req.Console != nil && len(req.Console.Endpoints) > 0 {
		endpointRaw = setupEndpointInputsToRuntimeRaw(req.Console.Endpoints)
	} else {
		var existing []runtimeEndpointConfigRaw
		_ = viper.UnmarshalKey("console.endpoints", &existing)
		endpointRaw = existing
	}
	if _, err := resolveRuntimeEndpoints(endpointRaw); err != nil {
		errs = append(errs, setupValidationError{
			Field:   "console.endpoints",
			Message: err.Error(),
		})
	}

	validateLLM := false
	if s.cfg.setupRequireLLM {
		validateLLM = setupStatusHasLLMGap(s.setupStatus) || (req.LLM != nil)
	}
	if !validateLLM {
		return errs, llmutil.ResolvedRoute{}, false
	}

	values := llmutil.RuntimeValuesFromViper()
	if req.LLM != nil {
		if v := strings.TrimSpace(req.LLM.Provider); v != "" {
			values.Provider = v
		}
		if v := strings.TrimSpace(req.LLM.Model); v != "" {
			values.Model = v
		}
		if v := strings.TrimSpace(req.LLM.Endpoint); v != "" {
			values.Endpoint = v
		}
		if v := strings.TrimSpace(req.LLM.APIKey); v != "" {
			values.APIKey = v
		}
		if v := strings.TrimSpace(req.LLM.APIKeyRef); v != "" {
			values.APIKey = strings.TrimSpace(os.Getenv(v))
		}
	}

	for _, field := range missingLLMFields(values) {
		errs = append(errs, setupValidationError{
			Field:   field,
			Message: "missing required value",
		})
	}
	if len(errs) > 0 {
		return compactSetupValidationErrors(errs), llmutil.ResolvedRoute{}, true
	}

	route, err := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)
	if err != nil {
		errs = append(errs, setupValidationError{
			Field:   "llm",
			Message: outputfmt.SanitizeErrorText(err.Error()),
		})
		return compactSetupValidationErrors(errs), llmutil.ResolvedRoute{}, true
	}
	if strings.TrimSpace(route.ClientConfig.Model) == "" {
		errs = append(errs, setupValidationError{
			Field:   "llm.model",
			Message: "missing required value",
		})
	}
	if strings.TrimSpace(route.ClientConfig.APIKey) == "" && strings.TrimSpace(route.ClientConfig.Provider) != "bedrock" && strings.TrimSpace(route.ClientConfig.Provider) != "cloudflare" {
		errs = append(errs, setupValidationError{
			Field:   "llm.api_key",
			Message: "missing required value",
		})
	}
	if len(errs) > 0 {
		return compactSetupValidationErrors(errs), llmutil.ResolvedRoute{}, true
	}
	return nil, route, true
}

func applySetupToConfig(configPath string, req setupApplyRequest) ([]string, []string, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil, nil, fmt.Errorf("invalid config path")
	}

	raw, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("read config: %w", err)
	}

	doc := map[string]any{}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, nil, fmt.Errorf("decode config: %w", err)
		}
		if doc == nil {
			doc = map[string]any{}
		}
	}

	appliedSet := map[string]struct{}{}
	redactedSet := map[string]struct{}{}

	if req.LLM != nil {
		if v := strings.TrimSpace(req.LLM.Provider); v != "" {
			setYAMLPath(doc, []string{"llm", "provider"}, v)
			appliedSet["llm.provider"] = struct{}{}
		}
		if v := strings.TrimSpace(req.LLM.Model); v != "" {
			setYAMLPath(doc, []string{"llm", "model"}, v)
			appliedSet["llm.model"] = struct{}{}
		}
		if v := strings.TrimSpace(req.LLM.Endpoint); v != "" {
			setYAMLPath(doc, []string{"llm", "endpoint"}, v)
			appliedSet["llm.endpoint"] = struct{}{}
		}
		if v := strings.TrimSpace(req.LLM.APIKey); v != "" {
			setYAMLPath(doc, []string{"llm", "api_key"}, v)
			setYAMLPath(doc, []string{"llm", "api_key_ref"}, "")
			appliedSet["llm.api_key"] = struct{}{}
			redactedSet["llm.api_key"] = struct{}{}
		}
		if v := strings.TrimSpace(req.LLM.APIKeyRef); v != "" {
			setYAMLPath(doc, []string{"llm", "api_key"}, fmt.Sprintf("${%s}", v))
			setYAMLPath(doc, []string{"llm", "api_key_ref"}, "")
			appliedSet["llm.api_key"] = struct{}{}
		}
	}

	if req.Console != nil {
		if v := strings.TrimSpace(req.Console.Password); v != "" {
			hashRaw, err := bcrypt.GenerateFromPassword([]byte(v), bcrypt.DefaultCost)
			if err != nil {
				return nil, nil, fmt.Errorf("hash console password: %w", err)
			}
			setYAMLPath(doc, []string{"console", "password"}, "")
			setYAMLPath(doc, []string{"console", "password_hash"}, string(hashRaw))
			appliedSet["console.password_hash"] = struct{}{}
			redactedSet["console.password"] = struct{}{}
		}
		if v := strings.TrimSpace(req.Console.PasswordHash); v != "" {
			setYAMLPath(doc, []string{"console", "password"}, "")
			setYAMLPath(doc, []string{"console", "password_hash"}, v)
			appliedSet["console.password_hash"] = struct{}{}
			redactedSet["console.password_hash"] = struct{}{}
		}
		if len(req.Console.Endpoints) > 0 {
			setYAMLPath(doc, []string{"console", "endpoints"}, setupEndpointInputsToYAML(req.Console.Endpoints))
			appliedSet["console.endpoints"] = struct{}{}
			redactedSet["console.endpoints[].auth_token"] = struct{}{}
		}
	}

	encoded, err := yaml.Marshal(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("encode config: %w", err)
	}
	content := string(encoded)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := fsstore.WriteTextAtomic(configPath, content, fsstore.FileOptions{}); err != nil {
		return nil, nil, fmt.Errorf("write config: %w", err)
	}

	return sortedSetKeys(appliedSet), sortedSetKeys(redactedSet), nil
}

func setupEndpointInputsToRuntimeRaw(inputs []setupApplyEndpointRaw) []runtimeEndpointConfigRaw {
	out := make([]runtimeEndpointConfigRaw, 0, len(inputs))
	for _, item := range inputs {
		token := strings.TrimSpace(item.AuthToken)
		if token == "" {
			if ref := strings.TrimSpace(item.AuthTokenEnvRef); ref != "" {
				token = "${" + ref + "}"
			}
		}
		out = append(out, runtimeEndpointConfigRaw{
			Name:      strings.TrimSpace(item.Name),
			URL:       strings.TrimSpace(item.URL),
			AuthToken: token,
		})
	}
	return out
}

func setupEndpointInputsToYAML(inputs []setupApplyEndpointRaw) []map[string]any {
	out := make([]map[string]any, 0, len(inputs))
	for _, item := range inputs {
		name := strings.TrimSpace(item.Name)
		url := strings.TrimSpace(item.URL)
		token := strings.TrimSpace(item.AuthToken)
		tokenEnvRef := strings.TrimSpace(item.AuthTokenEnvRef)
		if name == "" || url == "" {
			continue
		}
		entry := map[string]any{
			"name": name,
			"url":  url,
		}
		if token != "" {
			entry["auth_token"] = token
		} else if tokenEnvRef != "" {
			entry["auth_token"] = "${" + tokenEnvRef + "}"
		}
		out = append(out, entry)
	}
	return out
}

func setupStatusHasLLMGap(status setupStatus) bool {
	for _, field := range status.MissingFields {
		if strings.HasPrefix(strings.TrimSpace(field), "llm.") {
			return true
		}
	}
	return false
}

func compactSetupValidationErrors(errs []setupValidationError) []setupValidationError {
	if len(errs) <= 1 {
		return errs
	}
	seen := map[string]setupValidationError{}
	for _, item := range errs {
		field := strings.TrimSpace(item.Field)
		if field == "" {
			field = "setup"
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = setupValidationError{
			Field:   field,
			Message: strings.TrimSpace(item.Message),
		}
	}

	fields := make([]string, 0, len(seen))
	for field := range seen {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	out := make([]setupValidationError, 0, len(fields))
	for _, field := range fields {
		out = append(out, seen[field])
	}
	return out
}

func setYAMLPath(root map[string]any, path []string, value any) {
	if len(path) == 0 {
		return
	}
	m := root
	for i := 0; i < len(path)-1; i++ {
		key := strings.TrimSpace(path[i])
		if key == "" {
			return
		}
		next, ok := m[key]
		if !ok {
			child := map[string]any{}
			m[key] = child
			m = child
			continue
		}
		typed, ok := next.(map[string]any)
		if !ok {
			child := map[string]any{}
			m[key] = child
			m = child
			continue
		}
		m = typed
	}
	leaf := strings.TrimSpace(path[len(path)-1])
	if leaf == "" {
		return
	}
	m[leaf] = value
}

func sortedSetKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for key := range set {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
