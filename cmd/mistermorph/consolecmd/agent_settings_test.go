package consolecmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestReadAgentSettings(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n  provider: openai\n  model: gpt-5.2\n  reasoning_effort: high\n"+
			"multimodal:\n  image:\n    sources: [telegram, line]\n"+
			"tools:\n  bash:\n    enabled: false\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := readAgentSettings(configPath)
	if err != nil {
		t.Fatalf("readAgentSettings() error = %v", err)
	}
	if got.LLM.Provider != "openai" || got.LLM.Model != "gpt-5.2" || got.LLM.ReasoningEffort != "high" {
		t.Fatalf("got.LLM = %+v", got.LLM)
	}
	if len(got.Multimodal.ImageSources) != 2 || got.Multimodal.ImageSources[0] != "telegram" || got.Multimodal.ImageSources[1] != "line" {
		t.Fatalf("got.Multimodal = %+v", got.Multimodal)
	}
	if got.Tools.BashEnabled {
		t.Fatalf("got.Tools.BashEnabled = true, want false")
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
			Provider:           "anthropic",
			Model:              "claude-3-7-sonnet",
			ToolsEmulationMode: "fallback",
		},
		Multimodal: multimodalSettingsPayload{
			ImageSources: []string{"telegram", "remote_download"},
		},
		Tools: toolsSettingsPayload{
			WriteFileEnabled:    true,
			ContactsSendEnabled: true,
			TodoUpdateEnabled:   true,
			PlanCreateEnabled:   false,
			URLFetchEnabled:     true,
			WebSearchEnabled:    false,
			BashEnabled:         true,
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
		"llm":{"provider":"anthropic","model":"claude-3-7-sonnet","api_key":"${ANTHROPIC_API_KEY}","reasoning_effort":"high","tools_emulation_mode":"fallback"},
		"multimodal":{"image_sources":["telegram","remote_download"]},
		"tools":{"write_file_enabled":true,"contacts_send_enabled":false,"todo_update_enabled":true,"plan_create_enabled":false,"url_fetch_enabled":true,"web_search_enabled":false,"bash_enabled":false}
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
	if !strings.Contains(string(raw), "- remote_download") {
		t.Fatalf("config missing multimodal update: %s", string(raw))
	}
	if got := viper.GetString("llm.provider"); got != "anthropic" {
		t.Fatalf("viper llm.provider = %q, want anthropic", got)
	}
	if !viper.GetBool("tools.write_file.enabled") || viper.GetBool("tools.bash.enabled") {
		t.Fatalf("viper tools not updated: write_file=%v bash=%v", viper.GetBool("tools.write_file.enabled"), viper.GetBool("tools.bash.enabled"))
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
	if len(payload.Multimodal.ImageSources) != 2 {
		t.Fatalf("payload.Multimodal = %+v", payload.Multimodal)
	}
	if payload.Tools.BashEnabled {
		t.Fatalf("payload.Tools.BashEnabled = true, want false")
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
