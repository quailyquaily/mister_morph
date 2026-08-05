package agentsettings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

type handlerTestOwner struct {
	view       AgentSettingsView
	lastUpdate AgentSettingsUpdate
	reader     Reader
	updateErr  error
}

func (o *handlerTestOwner) View(context.Context) (AgentSettingsView, error) {
	return o.view, nil
}

func (o *handlerTestOwner) Update(_ context.Context, update AgentSettingsUpdate) (AgentSettingsView, error) {
	o.lastUpdate = update
	if o.updateErr != nil {
		return AgentSettingsView{}, o.updateErr
	}
	if update.LLM.Model != nil {
		o.view.LLM.Model = *update.LLM.Model
	}
	return o.view, nil
}

func TestHandlerSettingsUsesOwnerHTTPStatus(t *testing.T) {
	owner := &handlerTestOwner{
		view:      AgentSettingsView{ReadOnly: false},
		updateErr: &StatusError{Status: http.StatusConflict, Message: "llm.api_key is managed by environment"},
	}
	handler := NewHandler(HandlerOptions{Owner: owner})
	recorder := httptest.NewRecorder()
	handler.Settings(recorder, httptest.NewRequest(http.MethodPut, "/settings/agent", strings.NewReader(`{"llm":{"api_key":"replacement"}}`)))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("PUT status = %d, want %d (%s)", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
}

func (o *handlerTestOwner) CurrentReader() Reader {
	return o.reader
}

func TestHandlerSettingsUsesWritableOwnerForGetAndPut(t *testing.T) {
	owner := &handlerTestOwner{view: AgentSettingsView{
		LLM:      LLMSettingsPayload{LLMConfigFieldsPayload: LLMConfigFieldsPayload{Provider: "openai", Model: "before"}},
		ReadOnly: false,
	}}
	handler := NewHandler(HandlerOptions{Owner: owner})

	getRecorder := httptest.NewRecorder()
	handler.Settings(getRecorder, httptest.NewRequest(http.MethodGet, "/settings/agent", nil))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d (%s)", getRecorder.Code, http.StatusOK, getRecorder.Body.String())
	}
	var getPayload AgentSettingsView
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if getPayload.ReadOnly {
		t.Fatal("GET read_only = true, want false")
	}
	if getPayload.LLM.Model != "before" {
		t.Fatalf("GET model = %q, want before", getPayload.LLM.Model)
	}

	putRecorder := httptest.NewRecorder()
	putRequest := httptest.NewRequest(http.MethodPut, "/settings/agent", strings.NewReader(`{"llm":{"model":"after"}}`))
	handler.Settings(putRecorder, putRequest)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d (%s)", putRecorder.Code, http.StatusOK, putRecorder.Body.String())
	}
	if owner.lastUpdate.LLM.Model == nil || *owner.lastUpdate.LLM.Model != "after" {
		t.Fatalf("owner update model = %#v, want after", owner.lastUpdate.LLM.Model)
	}
	var putPayload AgentSettingsView
	if err := json.Unmarshal(putRecorder.Body.Bytes(), &putPayload); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if putPayload.LLM.Model != "after" {
		t.Fatalf("PUT model = %q, want after", putPayload.LLM.Model)
	}
}

func TestHandlerSettingsReportsOwnerReadOnlyReason(t *testing.T) {
	owner := &handlerTestOwner{view: AgentSettingsView{
		ReadOnly:       true,
		ReadOnlyReason: "settings writer is unavailable",
	}}
	handler := NewHandler(HandlerOptions{Owner: owner})

	getRecorder := httptest.NewRecorder()
	handler.Settings(getRecorder, httptest.NewRequest(http.MethodGet, "/settings/agent", nil))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d (%s)", getRecorder.Code, http.StatusOK, getRecorder.Body.String())
	}
	var getPayload AgentSettingsView
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if !getPayload.ReadOnly || getPayload.ReadOnlyReason != "settings writer is unavailable" {
		t.Fatalf("GET read-only payload = %#v", getPayload)
	}

	putRecorder := httptest.NewRecorder()
	handler.Settings(putRecorder, httptest.NewRequest(http.MethodPut, "/settings/agent", strings.NewReader(`{"llm":{"model":"after"}}`)))
	if putRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT status = %d, want %d (%s)", putRecorder.Code, http.StatusMethodNotAllowed, putRecorder.Body.String())
	}
	if !strings.Contains(putRecorder.Body.String(), "settings writer is unavailable") {
		t.Fatalf("PUT error = %q, want read-only reason", putRecorder.Body.String())
	}
	var putPayload map[string]any
	if err := json.Unmarshal(putRecorder.Body.Bytes(), &putPayload); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if putPayload["read_only"] != true || putPayload["read_only_reason"] != "settings writer is unavailable" {
		t.Fatalf("PUT read-only payload = %#v", putPayload)
	}
}

func TestHandlerModelsUsesOwnerCurrentReader(t *testing.T) {
	reader := viper.New()
	reader.Set("llm.provider", "openai")
	reader.Set("llm.endpoint", "https://runtime.example/v1")
	reader.Set("llm.api_key", "runtime-key")
	owner := &handlerTestOwner{reader: NewReaderSnapshot(reader)}
	handler := NewHandler(HandlerOptions{
		Owner: owner,
		FetchModels: func(_ context.Context, endpoint, apiKey string) ([]string, error) {
			if endpoint != "https://runtime.example/v1" {
				t.Fatalf("endpoint = %q, want runtime endpoint", endpoint)
			}
			if apiKey != "runtime-key" {
				t.Fatalf("api key = %q, want runtime key", apiKey)
			}
			return []string{"gpt-5", "gpt-5-mini"}, nil
		},
	})

	recorder := httptest.NewRecorder()
	handler.Models(recorder, httptest.NewRequest(http.MethodPost, "/settings/agent/models", strings.NewReader(`{"endpoint":"","api_key":""}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want %d (%s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := recorder.Body.String(); !strings.Contains(got, `"items":["gpt-5","gpt-5-mini"]`) {
		t.Fatalf("response = %s, want model items", got)
	}
}

func TestHandlerConnectionTestUsesSharedProfileResolution(t *testing.T) {
	reader := viper.New()
	reader.Set("llm.provider", "openai")
	reader.Set("llm.endpoint", "https://runtime.example/v1")
	reader.Set("llm.api_key", "runtime-key")
	reader.Set("llm.model", "gpt-runtime")
	owner := &handlerTestOwner{reader: NewReaderSnapshot(reader)}
	handler := NewHandler(HandlerOptions{
		Owner: owner,
		ConnectionTest: func(_ context.Context, settings LLMSettingsPayload, gotReader Reader, opts ConnectionTestOptions) (ConnectionTestResult, error) {
			if gotReader != owner.reader {
				t.Fatal("connection test did not receive owner current reader")
			}
			if !opts.InspectPrompt || !opts.InspectRequest {
				t.Fatalf("inspect options = %+v, want both enabled", opts)
			}
			if settings.Provider != "openai_custom" || settings.Model != "gpt-candidate" || settings.APIKey != "runtime-key" {
				t.Fatalf("resolved settings = %+v", settings)
			}
			return ConnectionTestResult{Provider: settings.Provider, APIBase: settings.Endpoint, Model: settings.Model}, nil
		},
		ConnectionTestOptions: ConnectionTestOptions{InspectPrompt: true, InspectRequest: true},
	})

	recorder := httptest.NewRecorder()
	handler.Test(recorder, httptest.NewRequest(http.MethodPost, "/settings/agent/test", strings.NewReader(`{"llm":{"model":"gpt-candidate"}}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want %d (%s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := recorder.Body.String(); !strings.Contains(got, `"model":"gpt-candidate"`) {
		t.Fatalf("response = %s, want resolved model", got)
	}
}

func TestReadOnlyOwnerBuildsViewFromRuntimeReader(t *testing.T) {
	reader := viper.New()
	reader.Set("llm.provider", "openai")
	reader.Set("llm.model", "gpt-runtime")
	reader.Set("tools.bash.enabled", true)
	owner := NewReadOnlyOwner(NewReaderSnapshot(reader), "settings writer is unavailable")

	view, err := owner.View(context.Background())
	if err != nil {
		t.Fatalf("View() error = %v", err)
	}
	if !view.ReadOnly || view.ReadOnlyReason != "settings writer is unavailable" {
		t.Fatalf("read-only fields = %+v", view)
	}
	if view.LLM.Model != "gpt-runtime" || !view.Tools.Bash.Enabled {
		t.Fatalf("runtime view = %+v", view)
	}
}
