package agentsettings

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type readOnlyOwner struct {
	reader Reader
	reason string
}

func NewReadOnlyOwner(reader Reader, reason string) Owner {
	return &readOnlyOwner{
		reader: NewReaderSnapshot(reader),
		reason: strings.TrimSpace(reason),
	}
}

func (o *readOnlyOwner) CurrentReader() Reader {
	if o == nil {
		return nil
	}
	return o.reader
}

func (o *readOnlyOwner) View(context.Context) (AgentSettingsView, error) {
	if o == nil || o.reader == nil {
		return AgentSettingsView{}, fmt.Errorf("agent settings reader is unavailable")
	}
	llmSettings, err := settingsFromRuntimeReader(o.reader)
	if err != nil {
		return AgentSettingsView{}, err
	}
	envManaged := CurrentEnvManaged(llmSettings.Provider)
	SanitizeManagedLLMFields(&llmSettings.LLMConfigFieldsPayload, envManaged.LLM, llmSettings.Provider)
	if len(envManaged.LLM) == 0 {
		envManaged.LLM = nil
	}
	skills, err := buildAgentSkillsSettingsPayloadFromReader(o.reader)
	if err != nil {
		return AgentSettingsView{}, err
	}
	configPath := strings.TrimSpace(o.reader.ConfigFileUsed())
	if configPath == "" {
		configPath = strings.TrimSpace(o.reader.GetString("config"))
	}
	configExists := false
	if configPath != "" {
		_, err := os.Stat(configPath)
		configExists = err == nil
	}
	reason := o.reason
	if reason == "" {
		reason = "settings writer is unavailable"
	}
	return AgentSettingsView{
		LLM:            llmSettings,
		EnvManaged:     envManaged,
		Skills:         skills,
		Tools:          toolsSettingsFromReader(o.reader),
		MCP:            mcpSettingsFromReader(o.reader),
		ConfigPath:     configPath,
		ConfigExists:   configExists,
		ConfigValid:    true,
		ConfigSource:   "runtime",
		ReadOnly:       true,
		ReadOnlyReason: reason,
	}, nil
}

func (o *readOnlyOwner) Update(context.Context, AgentSettingsUpdate) (AgentSettingsView, error) {
	reason := "settings writer is unavailable"
	if o != nil && strings.TrimSpace(o.reason) != "" {
		reason = strings.TrimSpace(o.reason)
	}
	return AgentSettingsView{}, &StatusError{Status: http.StatusMethodNotAllowed, Message: reason}
}

func toolsSettingsFromReader(reader Reader) ToolsSettingsPayload {
	if reader == nil {
		return ToolsSettingsPayload{}
	}
	return ToolsSettingsPayload{
		WriteFile:    ToolEnabledPayload{Enabled: reader.GetBool("tools.write_file.enabled")},
		Spawn:        ToolEnabledPayload{Enabled: reader.GetBool("tools.spawn.enabled")},
		Coder:        ToolEnabledPayload{Enabled: reader.GetBool("tools.coder.enabled")},
		ContactsSend: ToolEnabledPayload{Enabled: reader.GetBool("tools.contacts_send.enabled")},
		TodoUpdate:   ToolEnabledPayload{Enabled: reader.GetBool("tools.todo_update.enabled")},
		PlanCreate:   ToolEnabledPayload{Enabled: reader.GetBool("tools.plan_create.enabled")},
		URLFetch:     ToolEnabledPayload{Enabled: reader.GetBool("tools.url_fetch.enabled")},
		WebSearch:    ToolEnabledPayload{Enabled: reader.GetBool("tools.web_search.enabled")},
		Bash:         ToolEnabledPayload{Enabled: reader.GetBool("tools.bash.enabled")},
		PowerShell:   ToolEnabledPayload{Enabled: reader.GetBool("tools.powershell.enabled")},
	}
}
