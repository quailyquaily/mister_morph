package consolecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
	"github.com/spf13/viper"
)

func TestReadAgentSettings(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n  provider: cloudflare\n  model: gpt-5.2\n  reasoning_effort: high\n  api_key: legacy-cf-token\n  cloudflare:\n    account_id: acc-123\n  profiles:\n    cheap:\n      model: gpt-4.1-mini\n    burst:\n      provider: openai\n      api_key: sk-profile\n      model: gpt-4.1\n  routes:\n    main_loop:\n      fallback_profiles:\n        - cheap\n        - burst\n"+
			"multimodal:\n  image:\n    sources: [telegram, line]\n"+
			"tools:\n  bash:\n    enabled: false\n  powershell:\n    enabled: true\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := readAgentSettings(configPath)
	if err != nil {
		t.Fatalf("readAgentSettings() error = %v", err)
	}
	if got.LLM.Provider != "cloudflare" || got.LLM.Model != "gpt-5.2" || got.LLM.ReasoningEffort != "high" {
		t.Fatalf("got.LLM = %+v", got.LLM)
	}
	if got.LLM.CloudflareAccountID != "acc-123" {
		t.Fatalf("got.LLM.CloudflareAccountID = %q, want acc-123", got.LLM.CloudflareAccountID)
	}
	if got.LLM.CloudflareAPIToken != "legacy-cf-token" {
		t.Fatalf("got.LLM.CloudflareAPIToken = %q, want legacy-cf-token", got.LLM.CloudflareAPIToken)
	}
	if len(got.LLM.Profiles) != 2 || got.LLM.Profiles[0].Name != "cheap" || got.LLM.Profiles[1].Name != "burst" {
		t.Fatalf("got.LLM.Profiles = %+v", got.LLM.Profiles)
	}
	if got.LLM.Profiles[0].Model != "gpt-4.1-mini" {
		t.Fatalf("got.LLM.Profiles[0] = %+v", got.LLM.Profiles[0])
	}
	if got.LLM.Profiles[1].Provider != "openai" || got.LLM.Profiles[1].APIKey != "sk-profile" {
		t.Fatalf("got.LLM.Profiles[1] = %+v", got.LLM.Profiles[1])
	}
	if len(got.LLM.FallbackProfiles) != 2 || got.LLM.FallbackProfiles[0] != "cheap" || got.LLM.FallbackProfiles[1] != "burst" {
		t.Fatalf("got.LLM.FallbackProfiles = %+v", got.LLM.FallbackProfiles)
	}
	if len(got.Multimodal.ImageSources) != 2 || got.Multimodal.ImageSources[0] != "telegram" || got.Multimodal.ImageSources[1] != "line" {
		t.Fatalf("got.Multimodal = %+v", got.Multimodal)
	}
	if !got.Tools.WriteFile.Enabled || !got.Tools.ContactsSend.Enabled || !got.Tools.TodoUpdate.Enabled ||
		!got.Tools.PlanCreate.Enabled || !got.Tools.URLFetch.Enabled || !got.Tools.WebSearch.Enabled {
		t.Fatalf("got.Tools defaults not applied: %+v", got.Tools)
	}
	if !got.Tools.Spawn.Enabled {
		t.Fatalf("got.Tools.Spawn.Enabled = false, want true")
	}
	if got.Tools.Bash.Enabled {
		t.Fatalf("got.Tools.Bash.Enabled = true, want false")
	}
	if !got.Tools.PowerShell.Enabled {
		t.Fatalf("got.Tools.PowerShell.Enabled = false, want true")
	}
}

func TestWriteAgentSettingsPreservesOtherConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"console:\n  listen: 127.0.0.1:9080\n"+
			"llm:\n  provider: openai\n  model: gpt-5.2\n"+
			"tools:\n  url_fetch:\n    timeout: 30s\n    enabled: true\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	serialized, err := writeAgentSettings(configPath, agentSettingsPayload{
		LLM: llmSettingsPayload{
			llmConfigFieldsPayload: llmConfigFieldsPayload{
				Provider:            "anthropic",
				Model:               "claude-3-7-sonnet",
				CloudflareAccountID: "acc-next",
				ToolsEmulationMode:  "fallback",
			},
		},
		Multimodal: multimodalSettingsPayload{
			ImageSources: []string{"telegram", "remote_download"},
		},
		Tools: toolsSettingsPayload{
			WriteFile:    toolEnabledPayload{Enabled: true},
			Spawn:        toolEnabledPayload{Enabled: true},
			ContactsSend: toolEnabledPayload{Enabled: true},
			TodoUpdate:   toolEnabledPayload{Enabled: true},
			PlanCreate:   toolEnabledPayload{Enabled: false},
			URLFetch:     toolEnabledPayload{Enabled: true},
			WebSearch:    toolEnabledPayload{Enabled: false},
			Bash:         toolEnabledPayload{Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("writeAgentSettings() error = %v", err)
	}
	out := string(serialized)
	if !strings.Contains(out, "console:") || !strings.Contains(out, "listen: 127.0.0.1:9080") {
		t.Fatalf("serialized config lost console block: %s", out)
	}
	if !strings.Contains(out, "provider: anthropic") || !strings.Contains(out, "tools_emulation_mode: fallback") {
		t.Fatalf("serialized config missing updated llm block: %s", out)
	}
	if strings.Contains(out, "\n  cloudflare:\n") || strings.Contains(out, "account_id: acc-next") {
		t.Fatalf("serialized config should prune cloudflare block for non-cloudflare provider: %s", out)
	}
	if !strings.Contains(out, "- remote_download") {
		t.Fatalf("serialized config missing multimodal sources: %s", out)
	}
	if !strings.Contains(out, "timeout: 30s") {
		t.Fatalf("serialized config lost existing tool config: %s", out)
	}
}

func TestHandleAgentSettingsPut(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n  provider: openai\n  model: gpt-5.2\n"+
			"multimodal:\n  image:\n    sources: [telegram]\n"+
			"tools:\n  bash:\n    enabled: true\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	prevMM, hadMM := viper.Get("multimodal"), viper.IsSet("multimodal")
	prevTools, hadTools := viper.Get("tools"), viper.IsSet("tools")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
		if hadMM {
			viper.Set("multimodal", prevMM)
		} else {
			viper.Set("multimodal", nil)
		}
		if hadTools {
			viper.Set("tools", prevTools)
		} else {
			viper.Set("tools", nil)
		}
	})

	body := bytes.NewBufferString(`{
		"llm":{"provider":"anthropic","model":"claude-3-7-sonnet","api_key":"${ANTHROPIC_API_KEY}","cloudflare_account_id":"acc-live","reasoning_effort":"high","tools_emulation_mode":"fallback"},
		"multimodal":{"image_sources":["telegram","remote_download"]},
		"tools":{
			"write_file":{"enabled":true},
			"spawn":{"enabled":true},
			"contacts_send":{"enabled":false},
			"todo_update":{"enabled":true},
			"plan_create":{"enabled":false},
			"url_fetch":{"enabled":true},
			"web_search":{"enabled":false},
			"bash":{"enabled":false},
			"powershell":{"enabled":true}
		}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/agent", body)
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(raw), "provider: anthropic") || !strings.Contains(string(raw), "reasoning_effort: high") {
		t.Fatalf("config missing updated llm settings: %s", string(raw))
	}
	if strings.Contains(string(raw), "\n  cloudflare:\n") || strings.Contains(string(raw), "account_id: acc-live") {
		t.Fatalf("config should prune cloudflare block for non-cloudflare provider: %s", string(raw))
	}
	if !strings.Contains(string(raw), "- remote_download") {
		t.Fatalf("config missing multimodal update: %s", string(raw))
	}
	var payload struct {
		OK         bool                      `json:"ok"`
		LLM        llmSettingsPayload        `json:"llm"`
		Multimodal multimodalSettingsPayload `json:"multimodal"`
		Tools      toolsSettingsPayload      `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.OK {
		t.Fatalf("payload.OK = false, want true")
	}
	if payload.LLM.Provider != "anthropic" || payload.LLM.ReasoningEffort != "high" {
		t.Fatalf("payload.LLM = %+v", payload.LLM)
	}
	if payload.LLM.CloudflareAccountID != "" {
		t.Fatalf("payload.LLM.CloudflareAccountID = %q, want empty", payload.LLM.CloudflareAccountID)
	}
	if len(payload.Multimodal.ImageSources) != 2 {
		t.Fatalf("payload.Multimodal = %+v", payload.Multimodal)
	}
	if payload.Tools.Bash.Enabled {
		t.Fatalf("payload.Tools.Bash.Enabled = true, want false")
	}
	if !payload.Tools.Spawn.Enabled {
		t.Fatalf("payload.Tools.Spawn.Enabled = false, want true")
	}
	if !payload.Tools.PowerShell.Enabled {
		t.Fatalf("payload.Tools.PowerShell.Enabled = false, want true")
	}
}

func TestHandleAgentSettingsPutPreservesOmittedLLMFields(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n  provider: openai\n  endpoint: https://api.openai.com\n  model: gpt-5.2\n  api_key: sk-file\n  reasoning_effort: low\n"+
			"multimodal:\n  image:\n    sources: [telegram]\n"+
			"tools:\n  bash:\n    enabled: true\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	prevMM, hadMM := viper.Get("multimodal"), viper.IsSet("multimodal")
	prevTools, hadTools := viper.Get("tools"), viper.IsSet("tools")
	viper.Set("config", configPath)
	viper.Set("llm", map[string]any{
		"provider":         "openai",
		"endpoint":         "https://api.openai.com",
		"model":            "gpt-5.2",
		"api_key":          "sk-file",
		"reasoning_effort": "low",
	})
	viper.Set("multimodal", map[string]any{
		"image": map[string]any{
			"sources": []string{"telegram"},
		},
	})
	viper.Set("tools", map[string]any{
		"bash": map[string]any{"enabled": true},
	})
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
		if hadMM {
			viper.Set("multimodal", prevMM)
		} else {
			viper.Set("multimodal", nil)
		}
		if hadTools {
			viper.Set("tools", prevTools)
		} else {
			viper.Set("tools", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPut, "/api/settings/agent", bytes.NewBufferString(`{
		"llm":{"model":"gpt-5.1"},
		"multimodal":{"image_sources":["telegram"]},
		"tools":{"write_file":{"enabled":true},"spawn":{"enabled":true},"contacts_send":{"enabled":true},"todo_update":{"enabled":true},"plan_create":{"enabled":true},"url_fetch":{"enabled":true},"web_search":{"enabled":true},"bash":{"enabled":true}}
	}`))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	out := string(raw)
	if !strings.Contains(out, "provider: openai") || !strings.Contains(out, "endpoint: https://api.openai.com") {
		t.Fatalf("config should preserve omitted provider/endpoint: %s", out)
	}
	if !strings.Contains(out, "api_key: sk-file") || !strings.Contains(out, "reasoning_effort: low") {
		t.Fatalf("config should preserve omitted llm secrets/settings: %s", out)
	}
	if !strings.Contains(out, "model: gpt-5.1") {
		t.Fatalf("config should update provided llm field: %s", out)
	}
}

func TestHandleAgentSettingsPutPartialMultimodalUpdatePreservesLLMAndTools(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n  provider: openai\n  endpoint: https://api.openai.com\n  model: gpt-5.2\n  api_key: sk-file\n"+
			"multimodal:\n  image:\n    sources: [telegram]\n"+
			"tools:\n  write_file:\n    enabled: true\n  bash:\n    enabled: false\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	prevMM, hadMM := viper.Get("multimodal"), viper.IsSet("multimodal")
	prevTools, hadTools := viper.Get("tools"), viper.IsSet("tools")
	viper.Set("config", configPath)
	viper.Set("llm", map[string]any{
		"provider": "openai",
		"endpoint": "https://api.openai.com",
		"model":    "gpt-5.2",
		"api_key":  "sk-file",
	})
	viper.Set("multimodal", map[string]any{
		"image": map[string]any{
			"sources": []string{"telegram"},
		},
	})
	viper.Set("tools", map[string]any{
		"write_file": map[string]any{"enabled": true},
		"bash":       map[string]any{"enabled": false},
	})
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
		if hadMM {
			viper.Set("multimodal", prevMM)
		} else {
			viper.Set("multimodal", nil)
		}
		if hadTools {
			viper.Set("tools", prevTools)
		} else {
			viper.Set("tools", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPut, "/api/settings/agent", bytes.NewBufferString(`{
		"multimodal":{"image_sources":["slack","line"]}
	}`))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	out := string(raw)
	if !strings.Contains(out, "provider: openai") || !strings.Contains(out, "model: gpt-5.2") {
		t.Fatalf("config should preserve llm block: %s", out)
	}
	if !strings.Contains(out, "- slack") || !strings.Contains(out, "- line") {
		t.Fatalf("config should update multimodal sources: %s", out)
	}
	if !strings.Contains(out, "write_file:\n    enabled: true") || !strings.Contains(out, "bash:\n    enabled: false") {
		t.Fatalf("config should preserve tools block: %s", out)
	}
}

func TestHandleAgentSettingsPutClearsExplicitEmptyLLMField(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n  provider: openai\n  endpoint: https://api.openai.com\n  model: gpt-5.2\n  api_key: sk-file\n"+
			"multimodal:\n  image:\n    sources: [telegram]\n"+
			"tools:\n  bash:\n    enabled: true\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	prevMM, hadMM := viper.Get("multimodal"), viper.IsSet("multimodal")
	prevTools, hadTools := viper.Get("tools"), viper.IsSet("tools")
	viper.Set("config", configPath)
	viper.Set("llm", map[string]any{
		"provider": "openai",
		"endpoint": "https://api.openai.com",
		"model":    "gpt-5.2",
		"api_key":  "sk-file",
	})
	viper.Set("multimodal", map[string]any{
		"image": map[string]any{
			"sources": []string{"telegram"},
		},
	})
	viper.Set("tools", map[string]any{
		"bash": map[string]any{"enabled": true},
	})
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
		if hadMM {
			viper.Set("multimodal", prevMM)
		} else {
			viper.Set("multimodal", nil)
		}
		if hadTools {
			viper.Set("tools", prevTools)
		} else {
			viper.Set("tools", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPut, "/api/settings/agent", bytes.NewBufferString(`{
		"llm":{"endpoint":""},
		"multimodal":{"image_sources":["telegram"]},
		"tools":{"write_file":{"enabled":true},"spawn":{"enabled":true},"contacts_send":{"enabled":true},"todo_update":{"enabled":true},"plan_create":{"enabled":true},"url_fetch":{"enabled":true},"web_search":{"enabled":true},"bash":{"enabled":true}}
	}`))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	out := string(raw)
	if strings.Contains(out, "endpoint: https://api.openai.com") {
		t.Fatalf("config should delete explicitly cleared llm field: %s", out)
	}
	if !strings.Contains(out, "provider: openai") || !strings.Contains(out, "model: gpt-5.2") {
		t.Fatalf("config should preserve other llm fields: %s", out)
	}
}

func TestHandleAgentSettingsPutFallsBackToRuntimeCloudflareCredentials(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	prevMM, hadMM := viper.Get("multimodal"), viper.IsSet("multimodal")
	prevTools, hadTools := viper.Get("tools"), viper.IsSet("tools")
	viper.Set("config", configPath)
	viper.Set("llm", map[string]any{
		"provider": "openai",
		"endpoint": "https://api.openai.com",
		"model":    "gpt-5.2",
	})
	viper.Set("multimodal", map[string]any{
		"image": map[string]any{
			"sources": []string{"telegram"},
		},
	})
	viper.Set("tools", map[string]any{
		"write_file":    map[string]any{"enabled": true},
		"contacts_send": map[string]any{"enabled": true},
		"todo_update":   map[string]any{"enabled": true},
		"plan_create":   map[string]any{"enabled": true},
		"url_fetch":     map[string]any{"enabled": true},
		"web_search":    map[string]any{"enabled": true},
		"bash":          map[string]any{"enabled": true},
	})
	t.Setenv("MISTER_MORPH_LLM_CLOUDFLARE_ACCOUNT_ID", "acc-env")
	t.Setenv("MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN", "cf-env-token")
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
		if hadMM {
			viper.Set("multimodal", prevMM)
		} else {
			viper.Set("multimodal", nil)
		}
		if hadTools {
			viper.Set("tools", prevTools)
		} else {
			viper.Set("tools", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPut, "/api/settings/agent", bytes.NewBufferString(`{
		"llm":{"provider":"cloudflare","endpoint":"https://api.openai.com"},
		"multimodal":{"image_sources":["telegram"]},
		"tools":{"write_file":{"enabled":true},"spawn":{"enabled":true},"contacts_send":{"enabled":true},"todo_update":{"enabled":true},"plan_create":{"enabled":true},"url_fetch":{"enabled":true},"web_search":{"enabled":true},"bash":{"enabled":true}}
	}`))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"env_name":"MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN"`) {
		t.Fatalf("response should expose runtime cloudflare api token env-managed metadata: %s", body)
	}
}

func TestWriteAgentSettingsKeepsCloudflareBlockForCloudflareProvider(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n  provider: openai\n  model: gpt-5.2\n  api_key: sk-old\n  cloudflare:\n    api_token: ${CLOUDFLARE_API_TOKEN}\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	serialized, err := writeAgentSettings(configPath, agentSettingsPayload{
		LLM: llmSettingsPayload{
			llmConfigFieldsPayload: llmConfigFieldsPayload{
				Provider:            "cloudflare",
				Model:               "@cf/meta/llama-3.1-8b-instruct",
				CloudflareAPIToken:  "cf-new-token",
				CloudflareAccountID: "acc-live",
			},
		},
	})
	if err != nil {
		t.Fatalf("writeAgentSettings() error = %v", err)
	}
	out := string(serialized)
	if !strings.Contains(out, "provider: cloudflare") || !strings.Contains(out, "account_id: acc-live") {
		t.Fatalf("serialized config missing cloudflare settings: %s", out)
	}
	if !strings.Contains(out, "api_token: cf-new-token") {
		t.Fatalf("serialized config should write cloudflare api_token: %s", out)
	}
	if strings.Contains(out, "api_key: sk-old") {
		t.Fatalf("serialized config should remove generic api_key for cloudflare provider: %s", out)
	}
}

func TestWriteAgentSettingsPersistsProfilesAndFallbacks(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n  provider: openai\n  model: gpt-5.2\n  routes:\n    heartbeat: cheap\n  profiles:\n    old:\n      model: stale\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	serialized, err := writeAgentSettings(configPath, agentSettingsPayload{
		LLM: llmSettingsPayload{
			llmConfigFieldsPayload: llmConfigFieldsPayload{
				Provider: "openai",
				Model:    "gpt-5.2",
			},
			Profiles: []llmProfileSettingsPayload{
				{
					Name: "cheap",
					llmConfigFieldsPayload: llmConfigFieldsPayload{
						Model: "gpt-4.1-mini",
					},
				},
				{
					Name: "cloudburst",
					llmConfigFieldsPayload: llmConfigFieldsPayload{
						Provider:            "cloudflare",
						Model:               "@cf/meta/llama-3.1-8b-instruct",
						CloudflareAccountID: "acc-456",
						CloudflareAPIToken:  "cf-profile-token",
					},
				},
			},
			FallbackProfiles: []string{"cheap", "cloudburst"},
		},
	})
	if err != nil {
		t.Fatalf("writeAgentSettings() error = %v", err)
	}
	out := string(serialized)
	if !strings.Contains(out, "heartbeat: cheap") {
		t.Fatalf("serialized config should preserve llm.routes: %s", out)
	}
	if !strings.Contains(out, "profiles:") || !strings.Contains(out, "cheap:") || !strings.Contains(out, "cloudburst:") {
		t.Fatalf("serialized config missing profiles block: %s", out)
	}
	if !strings.Contains(out, "main_loop:") || !strings.Contains(out, "fallback_profiles:") || !strings.Contains(out, "- cheap") || !strings.Contains(out, "- cloudburst") {
		t.Fatalf("serialized config missing fallback profile sequence: %s", out)
	}
	if !strings.Contains(out, "account_id: acc-456") || !strings.Contains(out, "api_token: cf-profile-token") {
		t.Fatalf("serialized config missing cloudflare profile credentials: %s", out)
	}
	if strings.Contains(out, "old:") {
		t.Fatalf("serialized config should remove deleted profiles: %s", out)
	}
}

func TestHandleAgentSettingsPutRejectsDuplicateProfiles(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPut, "/api/settings/agent", bytes.NewBufferString(`{
		"llm":{
			"provider":"openai",
			"profiles":[
				{"name":"cheap","model":"gpt-4.1-mini"},
				{"name":"Cheap","model":"gpt-4.1"}
			],
			"fallback_profiles":["cheap"]
		},
		"multimodal":{"image_sources":[]},
		"tools":{"write_file":{"enabled":true},"spawn":{"enabled":true},"contacts_send":{"enabled":true},"todo_update":{"enabled":true},"plan_create":{"enabled":true},"url_fetch":{"enabled":true},"web_search":{"enabled":true},"bash":{"enabled":true}}
	}`))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "duplicate profile") {
		t.Fatalf("response should mention duplicate profiles: %s", rec.Body.String())
	}
}

func TestHandleAgentSettingsPutUpdatesViperProfilesAndPreservesRoutes(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n  provider: openai\n  model: gpt-5.2\n  routes:\n    heartbeat: burst\n  profiles:\n    burst:\n      model: gpt-4.1\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	prevMM, hadMM := viper.Get("multimodal"), viper.IsSet("multimodal")
	prevTools, hadTools := viper.Get("tools"), viper.IsSet("tools")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
		if hadMM {
			viper.Set("multimodal", prevMM)
		} else {
			viper.Set("multimodal", nil)
		}
		if hadTools {
			viper.Set("tools", prevTools)
		} else {
			viper.Set("tools", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPut, "/api/settings/agent", bytes.NewBufferString(`{
		"llm":{
			"provider":"openai",
			"model":"gpt-5.2",
			"profiles":[
				{"name":"cheap","model":"gpt-4.1-mini"}
			],
			"fallback_profiles":["cheap"]
		},
		"multimodal":{"image_sources":["telegram"]},
		"tools":{"write_file":{"enabled":true},"spawn":{"enabled":true},"contacts_send":{"enabled":true},"todo_update":{"enabled":true},"plan_create":{"enabled":true},"url_fetch":{"enabled":true},"web_search":{"enabled":true},"bash":{"enabled":true}}
	}`))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	out := string(raw)
	if !strings.Contains(out, "heartbeat: burst") {
		t.Fatalf("config should preserve llm.routes.heartbeat: %s", out)
	}
	if !strings.Contains(out, "fallback_profiles:") || !strings.Contains(out, "- cheap") {
		t.Fatalf("config should preserve main_loop fallback profiles: %s", out)
	}
	if !strings.Contains(out, "profiles:") || !strings.Contains(out, "cheap:") || !strings.Contains(out, "model: gpt-4.1-mini") {
		t.Fatalf("config should write updated profile config: %s", out)
	}
}

func TestHandleAgentSettingsPutRejectsInvalidConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPut, "/api/settings/agent", bytes.NewBufferString(`{"llm":{"provider":"not_a_provider"}}`))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleAgentSettingsGetFallsBackWhenConfigIsMalformed(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm: [\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	viper.Set("config", configPath)
	viper.Set("llm", map[string]any{
		"provider": "openai",
		"endpoint": "https://api.openai.com",
		"model":    "gpt-5.2",
	})
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings/agent", nil)
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "\"provider\":\"openai\"") {
		t.Fatalf("response should fall back to current viper defaults: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"config_source":"defaults"`) || !strings.Contains(rec.Body.String(), `"config_valid":false`) {
		t.Fatalf("response should expose config source metadata: %s", rec.Body.String())
	}
}

func TestHandleAgentSettingsGetIncludesEnvManagedMetadata(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n  provider: openai\n  endpoint: https://api.openai.com\n  model: gpt-5.2\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv("MISTER_MORPH_LLM_PROVIDER", "openai")
	t.Setenv("MISTER_MORPH_LLM_API_KEY", "sk-test")
	t.Setenv("MISTER_MORPH_LLM_REASONING_EFFORT", "high")

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	viper.Set("config", configPath)
	viper.Set("llm", map[string]any{
		"provider":         "openai",
		"endpoint":         "https://api.openai.com",
		"model":            "gpt-5.2",
		"api_key":          "sk-test",
		"reasoning_effort": "high",
	})
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings/agent", nil)
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, `"env_name":"MISTER_MORPH_LLM_API_KEY"`) {
		t.Fatalf("response missing api key env-managed metadata: %s", got)
	}
	if strings.Contains(rec.Body.String(), `"api_key":"sk-test"`) {
		t.Fatalf("response should not leak env-managed api key into llm payload: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"env_managed":{"llm":`) {
		t.Fatalf("response missing env_managed wrapper: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"env_name":"MISTER_MORPH_LLM_PROVIDER","value":"openai"`) {
		t.Fatalf("response missing provider env-managed metadata: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"env_name":"MISTER_MORPH_LLM_REASONING_EFFORT","value":"high"`) {
		t.Fatalf("response missing reasoning env-managed metadata: %s", rec.Body.String())
	}
}

func TestHandleAgentSettingsGetUsesDefaultsForLLMWhenConfigMissing(t *testing.T) {
	t.Setenv("MISTER_MORPH_LLM_PROVIDER", "cloudflare")
	t.Setenv("MISTER_MORPH_LLM_MODEL", "@cf/meta/llama-3.3-70b-instruct-fp8-fast")
	t.Setenv("MISTER_MORPH_LLM_API_KEY", "sk-env")

	configPath := filepath.Join(t.TempDir(), "missing-config.yaml")
	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings/agent", nil)
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"llm":{"provider":"openai","endpoint":"","model":"","api_key":"","cloudflare_api_token":"","cloudflare_account_id":"","reasoning_effort":"","tools_emulation_mode":"off"}`) {
		t.Fatalf("llm payload should expose defaults only: %s", body)
	}
	if !strings.Contains(body, `"env_managed":{"llm":`) || !strings.Contains(body, `"env_name":"MISTER_MORPH_LLM_PROVIDER","value":"cloudflare"`) {
		t.Fatalf("env_managed payload should expose env overrides separately: %s", body)
	}
}

func TestHandleAgentSettingsGetIncludesCloudflareTokenEnvManagedWithoutCloudflareProvider(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "missing-config.yaml")
	t.Setenv("MISTER_MORPH_LLM_CLOUDFLARE_ACCOUNT_ID", "acc-env")
	t.Setenv("MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN", "cf-env-token")

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings/agent", nil)
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"env_name":"MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN"`) {
		t.Fatalf("response missing cloudflare api token env-managed metadata: %s", body)
	}
	if !strings.Contains(body, `"env_name":"MISTER_MORPH_LLM_CLOUDFLARE_ACCOUNT_ID","value":"acc-env"`) {
		t.Fatalf("response missing cloudflare account id env-managed metadata: %s", body)
	}
}

func TestHandleAgentSettingsGetIncludesProfileEnvManagedPlaceholders(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n  provider: openai\n  model: ${OPENAI_MODEL}\n  api_key: ${OPENAI_API_KEY}\n  profiles:\n    cheap:\n      model: ${CHEAP_MODEL}\n    edge:\n      provider: cloudflare\n      cloudflare:\n        account_id: ${CF_ACCOUNT_ID}\n        api_token: ${CF_API_TOKEN}\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("OPENAI_MODEL", "gpt-5.2")
	t.Setenv("OPENAI_API_KEY", "sk-openai-env")
	t.Setenv("CHEAP_MODEL", "gpt-4.1-mini")
	t.Setenv("CF_ACCOUNT_ID", "acc-edge")
	t.Setenv("CF_API_TOKEN", "cf-secret")

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings/agent", nil)
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		LLM        llmSettingsPayload             `json:"llm"`
		EnvManaged agentSettingsEnvManagedPayload `json:"env_managed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := payload.EnvManaged.LLM["model"]; got.EnvName != "OPENAI_MODEL" || got.Value != "gpt-5.2" {
		t.Fatalf("top-level model env-managed = %+v, want OPENAI_MODEL/gpt-5.2", got)
	}
	if got := payload.EnvManaged.LLM["api_key"]; got.EnvName != "OPENAI_API_KEY" {
		t.Fatalf("top-level api key env-managed = %+v, want OPENAI_API_KEY", got)
	}
	if payload.LLM.APIKey != "" {
		t.Fatalf("payload.LLM.APIKey = %q, want empty", payload.LLM.APIKey)
	}
	if got := payload.EnvManaged.LLMProfiles["cheap"]["model"]; got.EnvName != "CHEAP_MODEL" || got.Value != "gpt-4.1-mini" {
		t.Fatalf("cheap profile model env-managed = %+v, want CHEAP_MODEL/gpt-4.1-mini", got)
	}
	if got := payload.EnvManaged.LLMProfiles["edge"]["cloudflare_api_token"]; got.EnvName != "CF_API_TOKEN" {
		t.Fatalf("edge profile token env-managed = %+v, want CF_API_TOKEN", got)
	}
	if got := payload.EnvManaged.LLMProfiles["edge"]["cloudflare_account_id"]; got.EnvName != "CF_ACCOUNT_ID" || got.Value != "acc-edge" {
		t.Fatalf("edge profile account env-managed = %+v, want CF_ACCOUNT_ID/acc-edge", got)
	}
	for _, profile := range payload.LLM.Profiles {
		if profile.Name == "edge" && profile.CloudflareAPIToken != "" {
			t.Fatalf("edge profile cloudflare token should be blank in payload, got %q", profile.CloudflareAPIToken)
		}
	}
}

func TestHandleAgentSettingsPutExpandsEnvPlaceholdersForRuntimeReload(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("ANTHROPIC_API_KEY", "sk-anthropic-env")
	t.Setenv("CHEAP_API_KEY", "sk-cheap-env")

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	prevMM, hadMM := viper.Get("multimodal"), viper.IsSet("multimodal")
	prevTools, hadTools := viper.Get("tools"), viper.IsSet("tools")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
		if hadMM {
			viper.Set("multimodal", prevMM)
		} else {
			viper.Set("multimodal", nil)
		}
		if hadTools {
			viper.Set("tools", prevTools)
		} else {
			viper.Set("tools", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPut, "/api/settings/agent", bytes.NewBufferString(`{
		"llm":{
			"provider":"anthropic",
			"model":"claude-3-7-sonnet",
			"api_key":"${ANTHROPIC_API_KEY}",
			"profiles":[
				{"name":"cheap","provider":"openai","model":"gpt-4.1-mini","api_key":"${CHEAP_API_KEY}"}
			],
			"fallback_profiles":["cheap"]
		},
		"multimodal":{"image_sources":["telegram"]},
		"tools":{"write_file":{"enabled":true},"spawn":{"enabled":true},"contacts_send":{"enabled":true},"todo_update":{"enabled":true},"plan_create":{"enabled":true},"url_fetch":{"enabled":true},"web_search":{"enabled":true},"bash":{"enabled":true}}
	}`))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	out := string(raw)
	if !strings.Contains(out, "api_key: ${ANTHROPIC_API_KEY}") {
		t.Fatalf("config should keep main api_key placeholder: %s", out)
	}
	if !strings.Contains(out, "api_key: ${CHEAP_API_KEY}") {
		t.Fatalf("config should keep profile api_key placeholder: %s", out)
	}
	var payload struct {
		LLM        llmSettingsPayload             `json:"llm"`
		EnvManaged agentSettingsEnvManagedPayload `json:"env_managed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := payload.EnvManaged.LLMProfiles["cheap"]["api_key"]; got.EnvName != "CHEAP_API_KEY" {
		t.Fatalf("cheap profile api key env-managed = %+v, want CHEAP_API_KEY", got)
	}
	for _, profile := range payload.LLM.Profiles {
		if profile.Name == "cheap" && profile.APIKey != "" {
			t.Fatalf("cheap profile api key should be blank in payload, got %q", profile.APIKey)
		}
	}
}

func TestWriteAgentSettingsRepairsMalformedConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm: [\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	serialized, err := writeAgentSettings(configPath, agentSettingsPayload{
		LLM: llmSettingsPayload{
			llmConfigFieldsPayload: llmConfigFieldsPayload{
				Provider: "openai",
				Model:    "gpt-5.2",
				APIKey:   "sk-test",
			},
		},
	})
	if err != nil {
		t.Fatalf("writeAgentSettings() error = %v", err)
	}
	out := string(serialized)
	if strings.Contains(out, "llm: [") {
		t.Fatalf("serialized config should replace malformed yaml: %s", out)
	}
	if !strings.Contains(out, "provider: openai") || !strings.Contains(out, "model: gpt-5.2") {
		t.Fatalf("serialized config missing repaired llm block: %s", out)
	}
}

func TestHandleAgentSettingsModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("authorization = %q, want Bearer sk-test", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5-mini"},{"id":"gpt-5"},{"id":"gpt-5-mini"}]}`))
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/settings/agent/models", bytes.NewBufferString(
		`{"endpoint":"`+upstream.URL+`","api_key":"sk-test"}`,
	))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettingsModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, `"items":["gpt-5","gpt-5-mini"]`) {
		t.Fatalf("response missing sorted model ids: %s", got)
	}
}

func TestHandleAgentSettingsModelsFallsBackToRuntimeAPIKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-runtime" {
			t.Fatalf("authorization = %q, want Bearer sk-runtime", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5"}]}`))
	}))
	defer upstream.Close()

	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	viper.Set("llm", map[string]any{
		"provider": "openai",
		"endpoint": upstream.URL,
		"api_key":  "sk-runtime",
	})
	t.Cleanup(func() {
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/agent/models", bytes.NewBufferString(`{"endpoint":"","api_key":""}`))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettingsModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandleAgentSettingsTest(t *testing.T) {
	prev := runAgentSettingsConnectionTest
	runAgentSettingsConnectionTest = func(_ context.Context, settings llmSettingsPayload, opts agentSettingsConnectionTestOptions) (agentSettingsTestResult, error) {
		if opts.InspectPrompt || opts.InspectRequest {
			t.Fatalf("unexpected inspect opts: %+v", opts)
		}
		if settings.Provider != "openai" {
			t.Fatalf("provider = %q, want openai", settings.Provider)
		}
		if settings.Model != "gpt-5" {
			t.Fatalf("model = %q, want gpt-5", settings.Model)
		}
		return agentSettingsTestResult{
			Provider: "openai",
			APIBase:  "https://api.openai.com",
			Model:    "gpt-5",
			Benchmarks: []agentSettingsBenchmarkResult{
				{ID: "text_reply", OK: true, DurationMS: 912, Detail: "OK", RawResponse: "OK"},
				{ID: "json_response", OK: true, DurationMS: 1044, Detail: "json ok"},
				{ID: "tool_calling", OK: false, DurationMS: 1350, Error: "model replied without calling the tool"},
			},
		}, nil
	}
	t.Cleanup(func() {
		runAgentSettingsConnectionTest = prev
	})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/agent/test", bytes.NewBufferString(
		`{"llm":{"provider":"openai","model":"gpt-5","api_key":"sk-test"}}`,
	))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettingsTest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, `"benchmarks":[`) || !strings.Contains(got, `"id":"text_reply"`) || !strings.Contains(got, `"id":"json_response"`) || !strings.Contains(got, `"id":"tool_calling"`) {
		t.Fatalf("response missing test result fields: %s", got)
	}
	if got := rec.Body.String(); !strings.Contains(got, `"api_base":"https://api.openai.com"`) {
		t.Fatalf("response missing api_base: %s", got)
	}
	if got := rec.Body.String(); !strings.Contains(got, `"raw_response":"OK"`) {
		t.Fatalf("response missing raw benchmark response: %s", got)
	}
}

func TestHandleAgentSettingsTestFallsBackToRuntimeConfig(t *testing.T) {
	prev := runAgentSettingsConnectionTest
	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	viper.Set("llm", map[string]any{
		"provider": "openai",
		"endpoint": "https://api.openai.com",
		"model":    "gpt-5-mini",
		"api_key":  "sk-runtime",
	})
	runAgentSettingsConnectionTest = func(_ context.Context, settings llmSettingsPayload, opts agentSettingsConnectionTestOptions) (agentSettingsTestResult, error) {
		if opts.InspectPrompt || opts.InspectRequest {
			t.Fatalf("unexpected inspect opts: %+v", opts)
		}
		if settings.Provider != "openai" {
			t.Fatalf("provider = %q, want openai", settings.Provider)
		}
		if settings.Endpoint != "https://api.openai.com" {
			t.Fatalf("endpoint = %q, want https://api.openai.com", settings.Endpoint)
		}
		if settings.APIKey != "sk-runtime" {
			t.Fatalf("api_key = %q, want sk-runtime", settings.APIKey)
		}
		if settings.Model != "gpt-5" {
			t.Fatalf("model = %q, want gpt-5", settings.Model)
		}
		return agentSettingsTestResult{
			Provider: "openai",
			Model:    "gpt-5",
			Benchmarks: []agentSettingsBenchmarkResult{
				{ID: "text_reply", OK: true, DurationMS: 1, Detail: "OK"},
			},
		}, nil
	}
	t.Cleanup(func() {
		runAgentSettingsConnectionTest = prev
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/agent/test", bytes.NewBufferString(
		`{"llm":{"model":"gpt-5"}}`,
	))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettingsTest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandleAgentSettingsTestFallsBackToRuntimeCloudflareToken(t *testing.T) {
	prev := runAgentSettingsConnectionTest
	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	viper.Set("llm", map[string]any{
		"provider": "cloudflare",
		"model":    "@cf/meta/llama-3.3-70b-instruct-fp8-fast",
		"cloudflare": map[string]any{
			"account_id": "acc-runtime",
			"api_token":  "cf-runtime-token",
		},
	})
	runAgentSettingsConnectionTest = func(_ context.Context, settings llmSettingsPayload, opts agentSettingsConnectionTestOptions) (agentSettingsTestResult, error) {
		if opts.InspectPrompt || opts.InspectRequest {
			t.Fatalf("unexpected inspect opts: %+v", opts)
		}
		if settings.Provider != "cloudflare" {
			t.Fatalf("provider = %q, want cloudflare", settings.Provider)
		}
		if settings.CloudflareAccountID != "acc-runtime" {
			t.Fatalf("cloudflare_account_id = %q, want acc-runtime", settings.CloudflareAccountID)
		}
		if settings.CloudflareAPIToken != "cf-runtime-token" {
			t.Fatalf("cloudflare_api_token = %q, want cf-runtime-token", settings.CloudflareAPIToken)
		}
		if settings.Model != "@cf/meta/llama-3.3-70b-instruct-fp8-fast" {
			t.Fatalf("model = %q, want cloudflare runtime model", settings.Model)
		}
		return agentSettingsTestResult{
			Provider: "cloudflare",
			Model:    settings.Model,
			Benchmarks: []agentSettingsBenchmarkResult{
				{ID: "text_reply", OK: true, DurationMS: 1, Detail: "OK"},
			},
		}, nil
	}
	t.Cleanup(func() {
		runAgentSettingsConnectionTest = prev
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/agent/test", bytes.NewBufferString(
		`{"llm":{}}`,
	))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettingsTest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandleAgentSettingsTestPrefersEnvManagedRuntimeConfig(t *testing.T) {
	prev := runAgentSettingsConnectionTest
	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	viper.Set("llm", map[string]any{
		"provider": "openai",
		"endpoint": "https://api.openai.com",
		"model":    "gpt-5.2",
		"api_key":  "sk-viper",
	})
	t.Setenv("MISTER_MORPH_LLM_PROVIDER", "cloudflare")
	t.Setenv("MISTER_MORPH_LLM_MODEL", "@cf/meta/llama-3.3-70b-instruct-fp8-fast")
	t.Setenv("MISTER_MORPH_LLM_CLOUDFLARE_ACCOUNT_ID", "acc-env")
	t.Setenv("MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN", "cf-env-token")
	runAgentSettingsConnectionTest = func(_ context.Context, settings llmSettingsPayload, opts agentSettingsConnectionTestOptions) (agentSettingsTestResult, error) {
		if opts.InspectPrompt || opts.InspectRequest {
			t.Fatalf("unexpected inspect opts: %+v", opts)
		}
		if settings.Provider != "cloudflare" {
			t.Fatalf("provider = %q, want cloudflare", settings.Provider)
		}
		if settings.Model != "@cf/meta/llama-3.3-70b-instruct-fp8-fast" {
			t.Fatalf("model = %q, want cloudflare env model", settings.Model)
		}
		if settings.CloudflareAccountID != "acc-env" {
			t.Fatalf("cloudflare_account_id = %q, want acc-env", settings.CloudflareAccountID)
		}
		if settings.CloudflareAPIToken != "cf-env-token" {
			t.Fatalf("cloudflare_api_token = %q, want cf-env-token", settings.CloudflareAPIToken)
		}
		return agentSettingsTestResult{
			Provider: "cloudflare",
			Model:    settings.Model,
			Benchmarks: []agentSettingsBenchmarkResult{
				{ID: "text_reply", OK: true, DurationMS: 1, Detail: "OK"},
			},
		}, nil
	}
	t.Cleanup(func() {
		runAgentSettingsConnectionTest = prev
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/agent/test", bytes.NewBufferString(
		`{"llm":{}}`,
	))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettingsTest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandleAgentSettingsTestTreatsEmptyTargetProfileAsDefaultFallback(t *testing.T) {
	prev := runAgentSettingsConnectionTest
	prevLLM, hadLLM := viper.Get("llm"), viper.IsSet("llm")
	viper.Set("llm", map[string]any{
		"provider": "cloudflare",
		"model":    "@cf/moonshotai/kimi-k2.5",
		"cloudflare": map[string]any{
			"account_id": "acc-runtime",
			"api_token":  "cf-runtime-token",
		},
	})
	runAgentSettingsConnectionTest = func(_ context.Context, settings llmSettingsPayload, opts agentSettingsConnectionTestOptions) (agentSettingsTestResult, error) {
		if opts.InspectPrompt || opts.InspectRequest {
			t.Fatalf("unexpected inspect opts: %+v", opts)
		}
		if settings.Provider != "cloudflare" {
			t.Fatalf("provider = %q, want cloudflare", settings.Provider)
		}
		if settings.Endpoint != "" {
			t.Fatalf("endpoint = %q, want empty cloudflare endpoint", settings.Endpoint)
		}
		if settings.CloudflareAccountID != "acc-runtime" {
			t.Fatalf("cloudflare_account_id = %q, want runtime fallback", settings.CloudflareAccountID)
		}
		if settings.CloudflareAPIToken != "cf-runtime-token" {
			t.Fatalf("cloudflare_api_token = %q, want runtime fallback", settings.CloudflareAPIToken)
		}
		if settings.Model != "@cf/moonshotai/kimi-k2.5" {
			t.Fatalf("model = %q, want runtime fallback model", settings.Model)
		}
		return agentSettingsTestResult{
			Provider: "cloudflare",
			Model:    settings.Model,
			Benchmarks: []agentSettingsBenchmarkResult{
				{ID: "text_reply", OK: true, DurationMS: 1, Detail: "OK"},
			},
		}, nil
	}
	t.Cleanup(func() {
		runAgentSettingsConnectionTest = prev
		if hadLLM {
			viper.Set("llm", prevLLM)
		} else {
			viper.Set("llm", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/agent/test", bytes.NewBufferString(
		`{"llm":{"provider":"cloudflare","endpoint":"","model":"","cloudflare_api_token":"","cloudflare_account_id":"","profiles":[{"name":"backup","provider":"openai","endpoint":"https://susanoo-api.quaily.com/v1/","model":"carrot/gpt-5.4","api_key":"${MISTER_MORPH_LLM_PROFILE_BACKUP_API_KEY}","cloudflare_api_token":"","cloudflare_account_id":"","reasoning_effort":"","tools_emulation_mode":""}],"fallback_profiles":["backup"]},"target_profile":""}`,
	))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettingsTest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandleAgentSettingsTestResolvesTargetProfileFromSnapshot(t *testing.T) {
	prev := runAgentSettingsConnectionTest
	t.Setenv("BASE_API_KEY", "sk-base")
	runAgentSettingsConnectionTest = func(_ context.Context, settings llmSettingsPayload, opts agentSettingsConnectionTestOptions) (agentSettingsTestResult, error) {
		if opts.InspectPrompt || opts.InspectRequest {
			t.Fatalf("unexpected inspect opts: %+v", opts)
		}
		if settings.Provider != "openai" {
			t.Fatalf("provider = %q, want openai", settings.Provider)
		}
		if settings.Endpoint != "https://api.example.com" {
			t.Fatalf("endpoint = %q, want https://api.example.com", settings.Endpoint)
		}
		if settings.APIKey != "sk-base" {
			t.Fatalf("api_key = %q, want resolved base env value", settings.APIKey)
		}
		if settings.Model != "gpt-5-nano" {
			t.Fatalf("model = %q, want gpt-5-nano", settings.Model)
		}
		if len(settings.Profiles) != 0 || len(settings.FallbackProfiles) != 0 {
			t.Fatalf("resolved test settings should not retain profile metadata: %+v", settings)
		}
		return agentSettingsTestResult{
			Provider: "openai",
			Model:    settings.Model,
			Benchmarks: []agentSettingsBenchmarkResult{
				{ID: "text_reply", OK: true, DurationMS: 1, Detail: "OK"},
			},
		}, nil
	}
	t.Cleanup(func() {
		runAgentSettingsConnectionTest = prev
	})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/agent/test", bytes.NewBufferString(
		`{"llm":{"provider":"openai","endpoint":"https://api.example.com","model":"gpt-5.2","api_key":"${BASE_API_KEY}","profiles":[{"name":"cheap","provider":"","endpoint":"","model":"gpt-5-nano","api_key":"","cloudflare_api_token":"","cloudflare_account_id":"","reasoning_effort":"","tools_emulation_mode":""}]},"target_profile":"cheap"}`,
	))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettingsTest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandleAgentSettingsTestIgnoresUnrelatedInvalidProfiles(t *testing.T) {
	prev := runAgentSettingsConnectionTest
	t.Setenv("BASE_API_KEY", "sk-base")
	runAgentSettingsConnectionTest = func(_ context.Context, settings llmSettingsPayload, opts agentSettingsConnectionTestOptions) (agentSettingsTestResult, error) {
		if opts.InspectPrompt || opts.InspectRequest {
			t.Fatalf("unexpected inspect opts: %+v", opts)
		}
		if settings.Model != "gpt-5-nano" {
			t.Fatalf("model = %q, want target profile model", settings.Model)
		}
		return agentSettingsTestResult{
			Provider: "openai",
			Model:    settings.Model,
			Benchmarks: []agentSettingsBenchmarkResult{
				{ID: "text_reply", OK: true, DurationMS: 1, Detail: "OK"},
			},
		}, nil
	}
	t.Cleanup(func() {
		runAgentSettingsConnectionTest = prev
	})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/agent/test", bytes.NewBufferString(
		`{"llm":{"provider":"openai","endpoint":"https://api.example.com","model":"gpt-5.2","api_key":"${BASE_API_KEY}","profiles":[{"name":"cheap","provider":"","endpoint":"","model":"gpt-5-nano","api_key":"","cloudflare_api_token":"","cloudflare_account_id":"","reasoning_effort":"","tools_emulation_mode":""},{"name":"broken","provider":"","endpoint":"","model":"","api_key":"${MISSING_PROFILE_KEY}","cloudflare_api_token":"","cloudflare_account_id":"","reasoning_effort":"","tools_emulation_mode":""}],"fallback_profiles":[]},"target_profile":"cheap"}`,
	))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettingsTest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandleAgentSettingsTestRejectsMissingTargetProfile(t *testing.T) {
	prev := runAgentSettingsConnectionTest
	runAgentSettingsConnectionTest = func(_ context.Context, _ llmSettingsPayload, _ agentSettingsConnectionTestOptions) (agentSettingsTestResult, error) {
		t.Fatalf("runAgentSettingsConnectionTest should not be called when target profile is missing")
		return agentSettingsTestResult{}, nil
	}
	t.Cleanup(func() {
		runAgentSettingsConnectionTest = prev
	})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/agent/test", bytes.NewBufferString(
		`{"llm":{"provider":"openai","model":"gpt-5","profiles":[]},"target_profile":"cheap"}`,
	))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettingsTest(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `missing profile \"cheap\"`) {
		t.Fatalf("missing profile error not reported: %s", rec.Body.String())
	}
}

func TestHandleAgentSettingsTestRejectsMissingEnvInTargetProfile(t *testing.T) {
	prev := runAgentSettingsConnectionTest
	runAgentSettingsConnectionTest = func(_ context.Context, _ llmSettingsPayload, _ agentSettingsConnectionTestOptions) (agentSettingsTestResult, error) {
		t.Fatalf("runAgentSettingsConnectionTest should not be called when target profile env is missing")
		return agentSettingsTestResult{}, nil
	}
	t.Cleanup(func() {
		runAgentSettingsConnectionTest = prev
	})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/agent/test", bytes.NewBufferString(
		`{"llm":{"provider":"openai","endpoint":"https://api.example.com","model":"gpt-5.2","api_key":"sk-base","profiles":[{"name":"cheap","provider":"","endpoint":"","model":"gpt-5-nano","api_key":"${MISSING_PROFILE_KEY}","cloudflare_api_token":"","cloudflare_account_id":"","reasoning_effort":"","tools_emulation_mode":""}],"fallback_profiles":[]},"target_profile":"cheap"}`,
	))
	rec := httptest.NewRecorder()

	(&server{}).handleAgentSettingsTest(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `missing env \"MISSING_PROFILE_KEY\"`) {
		t.Fatalf("missing env error not reported: %s", rec.Body.String())
	}
}

func TestAgentSettingsBenchmarksIncludeRawResponseOnChatError(t *testing.T) {
	tests := []struct {
		name string
		run  func(context.Context, llm.Client, string) agentSettingsBenchmarkResult
	}{
		{name: "text", run: runAgentSettingsTextBenchmark},
		{name: "json", run: runAgentSettingsJSONBenchmark},
		{name: "tool", run: runAgentSettingsToolCallingBenchmark},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.run(context.Background(), benchmarkClientStub{
				err: errors.New(`openai API request failed with status 400: {"error":{"message":"bad model"}}`),
			}, "gpt-5")
			if got.OK {
				t.Fatalf("got.OK = true, want false")
			}
			if got.RawResponse != `{"error":{"message":"bad model"}}` {
				t.Fatalf("got.RawResponse = %q, want upstream response body", got.RawResponse)
			}
		})
	}
}

func TestBenchmarkRawResponseFromErrorFallsBackToErrorText(t *testing.T) {
	err := errors.New("dial tcp 127.0.0.1:443: connect: connection refused")
	if got := benchmarkRawResponseFromError(err); got != err.Error() {
		t.Fatalf("benchmarkRawResponseFromError() = %q, want %q", got, err.Error())
	}
}

func TestHandleAgentSettingsTestPassesInspectFlags(t *testing.T) {
	prev := runAgentSettingsConnectionTest
	runAgentSettingsConnectionTest = func(_ context.Context, _ llmSettingsPayload, opts agentSettingsConnectionTestOptions) (agentSettingsTestResult, error) {
		if !opts.InspectPrompt || !opts.InspectRequest {
			t.Fatalf("inspect opts = %+v, want both enabled", opts)
		}
		return agentSettingsTestResult{
			Provider: "openai",
			Model:    "gpt-5",
			Benchmarks: []agentSettingsBenchmarkResult{
				{ID: "text_reply", OK: true, DurationMS: 1, Detail: "OK"},
			},
		}, nil
	}
	t.Cleanup(func() {
		runAgentSettingsConnectionTest = prev
	})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/agent/test", bytes.NewBufferString(`{"llm":{"provider":"openai","model":"gpt-5"}}`))
	rec := httptest.NewRecorder()

	(&server{cfg: serveConfig{inspectPrompt: true, inspectRequest: true}}).handleAgentSettingsTest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestNormalizeAgentSettingsProvider(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "openai"},
		{name: "openai compatible", in: "openai_compatible", want: "openai"},
		{name: "cloudflare", in: "cloudflare", want: "cloudflare"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeAgentSettingsProvider(tc.in); got != tc.want {
				t.Fatalf("normalizeAgentSettingsProvider(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCurrentAgentSettingsLLMEnvManagedPrefersProviderSpecificEnv(t *testing.T) {
	t.Setenv("MISTER_MORPH_LLM_API_KEY", "sk-generic")
	t.Setenv("MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN", "cf-token")
	t.Setenv("MISTER_MORPH_LLM_PROVIDER", "cloudflare")
	t.Setenv("MISTER_MORPH_LLM_MODEL", "gpt-5.2")
	t.Setenv("MISTER_MORPH_LLM_AZURE_DEPLOYMENT", "azure-deploy")

	cloudflareFields := currentAgentSettingsLLMEnvManaged("cloudflare")
	if got := cloudflareFields["cloudflare_api_token"].EnvName; got != "MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN" {
		t.Fatalf("cloudflare api token env = %q, want MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN", got)
	}

	azureFields := currentAgentSettingsLLMEnvManaged("azure")
	if got := azureFields["model"].EnvName; got != "MISTER_MORPH_LLM_AZURE_DEPLOYMENT" {
		t.Fatalf("azure model env = %q, want MISTER_MORPH_LLM_AZURE_DEPLOYMENT", got)
	}
	if got := cloudflareFields["provider"].Value; got != "cloudflare" {
		t.Fatalf("provider value = %q, want cloudflare", got)
	}
	if got := cloudflareFields["cloudflare_api_token"].Value; got != "" {
		t.Fatalf("api token value = %q, want empty for sensitive env-managed field", got)
	}

	emptyProviderFields := currentAgentSettingsLLMEnvManaged("")
	if got := emptyProviderFields["cloudflare_api_token"].EnvName; got != "MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN" {
		t.Fatalf("empty provider cloudflare api token env = %q, want MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN", got)
	}
	if got := emptyProviderFields["api_key"].EnvName; got != "MISTER_MORPH_LLM_API_KEY" {
		t.Fatalf("empty provider api key env = %q, want MISTER_MORPH_LLM_API_KEY", got)
	}
}

type benchmarkClientStub struct {
	result llm.Result
	err    error
}

func (c benchmarkClientStub) Chat(context.Context, llm.Request) (llm.Result, error) {
	return c.result, c.err
}
