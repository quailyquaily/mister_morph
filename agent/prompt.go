package agent

import (
	"strings"

	"github.com/quailyquaily/mister_morph/tools"
)

type PromptSpec struct {
	Identity string
	Rules    []string
	Blocks   []PromptBlock
}

type PromptBlock struct {
	Title   string
	Content string
}

func DefaultPromptSpec() PromptSpec {
	return PromptSpec{
		Identity: "You are MisterMorph, a general-purpose AI agent that can use tools to complete tasks.",
		Rules: []string{
			"You MUST respond with valid JSON only (no markdown).",
			"For complex tasks, start by returning a plan response, then execute it. For simple tasks, proceed directly.",
			"If you return a plan with steps, each step MUST include a status: pending|in_progress|completed.",
			"If the user requests writing/saving a local file, you MUST use write_file (preferred) or bash to actually write it; do not claim you wrote a file unless you called a tool to do so.",
			"Use tool_call when you need external information or actions; otherwise respond with final.",
			"You MUST NOT ask the user to paste API keys/tokens/passwords or any secrets. Use tool-side credential injection (e.g. url_fetch.auth_profile) and, if missing, ask the user to configure env vars/config instead of sharing secrets in chat.",
			"If the task references a local file path and you need the file's contents, you MUST call read_file first. Do NOT send local file paths as payloads to external HTTP APIs.",
			"For binary files (e.g. PDFs), prefer url_fetch.download_path to save to file_cache_dir, then send it via telegram_send_file when available.",
			"If a tool returns an error, you may try a different tool or different params.",
			"In tool_call.tool_name, you MUST use a tool listed under 'Available Tools' (do NOT invent tool names). Skills are prompt context, not tools.",
		},
	}
}

func BuildSystemPrompt(registry *tools.Registry, spec PromptSpec) string {
	var b strings.Builder
	b.WriteString(spec.Identity)
	if len(spec.Blocks) > 0 {
		b.WriteString("\n\n## Skills & Context\n")
		b.WriteString("Skills are not tools. They provide extra context and may include scripts to run via tools like bash.\n\n")
		for _, blk := range spec.Blocks {
			title := strings.TrimSpace(blk.Title)
			if title == "" {
				title = "Context"
			}
			b.WriteString("### ")
			b.WriteString(title)
			b.WriteString("\n")
			b.WriteString(strings.TrimSpace(blk.Content))
			b.WriteString("\n\n")
		}
	}
	b.WriteString("\n\n## Available Tools\n")
	b.WriteString(registry.FormatToolDescriptions())

	b.WriteString("## Response Format\n")
	b.WriteString("You MUST respond with JSON in one of three formats:\n\n")

	b.WriteString("### Option 1: Plan\n")
	b.WriteString("```json\n")
	b.WriteString(`{
  "type": "plan",
  "plan": {
    "thought": "brief reasoning (optional)",
    "summary": "1-2 sentence overview",
    "steps": [
      {"step": "step 1", "status": "in_progress"},
      {"step": "step 2", "status": "pending"},
      {"step": "step 3", "status": "pending"}
    ],
    "risks": ["optional"],
    "questions": ["optional clarifying questions"],
    "completion": "what done looks like (optional)"
  }
}`)
	b.WriteString("\n```\n\n")

	b.WriteString("### Option 2: Tool Call\n")
	b.WriteString("```json\n")
	b.WriteString(`{
  "type": "tool_call",
  "tool_call": {
    "thought": "what you will do next",
    "tool_name": "<tool name>",
    "tool_params": { }
  }
}`)
	b.WriteString("\n```\n\n")

	b.WriteString("### Option 3: Final\n")
	b.WriteString("```json\n")
	b.WriteString(`{
  "type": "final",
  "final": {
    "thought": "brief reasoning",
    "output": "your final answer"
  }
}`)
	b.WriteString("\n```\n\n")

	if len(spec.Rules) > 0 {
		b.WriteString("## Rules\n")
		for i, rule := range spec.Rules {
			b.WriteString(strings.TrimSpace(rule))
			if i < len(spec.Rules)-1 {
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}
