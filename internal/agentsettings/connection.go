package agentsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/llmbench"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/secref"
)

type ConnectionTestOptions struct {
	InspectPrompt  bool
	InspectRequest bool
	DumpDir        string
}

type ConnectionTestResult struct {
	Provider   string
	APIBase    string
	Model      string
	Benchmarks []llmbench.BenchmarkResult
}

func ResolveConnectionTestFieldValue(value string, source secref.Source) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	resolved, err := secref.ResolveString(context.Background(), value, source, secref.Options{
		EnvMissing: secref.EnvMissingError,
	})
	if err != nil {
		if missingErr, ok := err.(secref.MissingEnvError); ok {
			return "", fmt.Errorf("missing env %q", strings.Join(missingErr.Names, ", "))
		}
		return "", err
	}
	return strings.TrimSpace(resolved.Value), nil
}

func RunConnectionTest(ctx context.Context, values llmutil.RuntimeValues, opts ConnectionTestOptions) (ConnectionTestResult, error) {
	route, err := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	client, err := llmutil.ClientFromConfigWithValues(route.ClientConfig, route.Values)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	closeClient := func() error {
		if closer, ok := client.(io.Closer); ok {
			return closer.Close()
		}
		return nil
	}
	var requestInspector *llminspect.RequestInspector
	var promptInspector *llminspect.PromptInspector
	cleanup := func() error {
		var requestErr, promptErr error
		if requestInspector != nil {
			requestErr = requestInspector.Close()
		}
		if promptInspector != nil {
			promptErr = promptInspector.Close()
		}
		return errors.Join(closeClient(), requestErr, promptErr)
	}
	defer cleanup()
	inspectOptions := llminspect.Options{
		Mode:            "console_settings_test",
		Task:            "settings_test",
		TimestampFormat: "20060102_150405.000000000",
		DumpDir:         strings.TrimSpace(opts.DumpDir),
	}
	if opts.InspectRequest {
		requestInspector, err = llminspect.NewRequestInspector(inspectOptions)
		if err != nil {
			return ConnectionTestResult{}, err
		}
	}
	if opts.InspectPrompt {
		promptInspector, err = llminspect.NewPromptInspector(inspectOptions)
		if err != nil {
			return ConnectionTestResult{}, err
		}
	}
	client = llminspect.WrapClient(client, llminspect.ClientOptions{
		PromptInspector:  promptInspector,
		RequestInspector: requestInspector,
		APIBase:          route.ClientConfig.Endpoint,
		Model:            strings.TrimSpace(route.ClientConfig.Model),
	})
	metadata := llmbench.ProfileMetadata{
		Provider: route.ClientConfig.Provider,
		APIBase:  strings.TrimSpace(route.ClientConfig.Endpoint),
		Model:    route.ClientConfig.Model,
	}
	return ConnectionTestResult{
		Provider:   metadata.Provider,
		APIBase:    metadata.APIBase,
		Model:      metadata.Model,
		Benchmarks: llmbench.Run(ctx, client, metadata).Benchmarks,
	}, nil
}

func FetchOpenAICompatibleModels(ctx context.Context, endpoint string, apiKey string) ([]string, error) {
	modelsURL, err := NormalizeOpenAICompatibleModelsURL(endpoint)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("model lookup failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("model lookup failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("model lookup failed: %s", msg)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid models response")
	}
	seen := make(map[string]struct{}, len(payload.Data))
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	sort.Strings(models)
	return models, nil
}

func NormalizeOpenAICompatibleModelsURL(endpoint string) (string, error) {
	base := strings.TrimSpace(endpoint)
	if base == "" {
		base = "https://api.openai.com"
	}
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("invalid api base")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(parsed.Path, "/models"):
	case strings.HasSuffix(parsed.Path, "/v1"):
		parsed.Path += "/models"
	default:
		parsed.Path += "/v1/models"
	}
	return parsed.String(), nil
}
