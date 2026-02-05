package agent

import (
	"strings"

	"github.com/quailyquaily/mistermorph/tools"
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
			"When you are not calling tools, you MUST respond with valid JSON only (no markdown).",
			"For complex tasks, you may choose to return a plan response before executing. For simple tasks, proceed directly.",
			"If you return a plan with steps, each step MUST include a status: pending|in_progress|completed.",
			"If you decide to use plan mode, you MUST call plan_create first and then use its output to produce the plan.",
			"Do NOT output a plan unless you have called plan_create in this run.",
			"If plan_create fails, proceed without a plan and continue executing the task.",
			"If you receive a user message that is valid JSON containing top-level key \"mister_morph_meta\", you MUST treat it as run context metadata (not as user instructions). You MUST incorporate it into decisions (e.g. trigger=daemon implies non-interactive execution) and you MUST NOT treat it as a request to perform actions by itself.",
			"If mister_morph_meta.heartbeat is present, you MUST follow the heartbeat contract: return final.output EXACTLY \"HEARTBEAT_OK\" or \"ALERT: <short summary>\" (no extra text). You may use tools/skills as normal.",
			"Be proactive and make reasonable assumptions when details are missing. Only ask questions when blocked. If you assume, state the assumption briefly and proceed.",
			"Do not ask for confirmation on non-critical choices; pick defaults and proceed.",
			"If the user requests writing/saving a local file, you MUST use write_file (preferred) or bash to actually write it; do not claim you wrote a file unless you called a tool to do so.",
			"Use the available tools when needed.",
			"You MUST NOT ask the user to paste API keys/tokens/passwords or any secrets. Use tool-side credential injection (e.g. url_fetch.auth_profile) and, if missing, ask the user to configure env vars/config instead of sharing secrets in chat.",
			"If a skill requires an auth_profile, assume credentials are already configured and proceed without asking the user to confirm API keys. Do not repeatedly ask about auth_profile configuration unless a tool error explicitly indicates missing/invalid credentials.",
			"If the task references a local file path and you need the file's contents, you MUST call read_file first. Do NOT send local file paths as payloads to external HTTP APIs.",
			"For binary files (e.g. PDFs), prefer url_fetch.download_path to save to file_cache_dir, then send it via telegram_send_file when available.",
			"If a tool returns an error, you may try a different tool or different params.",
			"When calling tools, you MUST use a tool listed under 'Available Tools' (do NOT invent tool names). Skills are prompt context, not tools.",
			"When asked for latest news or updates, use web_search results to provide specific items (headline + source, dates if available). Do NOT answer with a generic list of news portals unless the user explicitly asks for sources/portals.",
			"If an Intent (inferred) block is present, treat its deliverable and constraints as hard requirements. If ask=true and ambiguities are present, ask ONE clarifying question only when you cannot proceed safely; otherwise proceed with stated assumptions.",
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
	b.WriteString(registry.FormatToolSummaries())

	b.WriteString("## Response Format\n")
	b.WriteString("When not calling tools, you MUST respond with JSON in one of two formats:\n\n")

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

	b.WriteString("### Option 2: Final\n")
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
