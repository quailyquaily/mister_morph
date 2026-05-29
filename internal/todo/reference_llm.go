package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/jsonutil"
	"github.com/quailyquaily/mistermorph/internal/runtimeclock"
	"github.com/quailyquaily/mistermorph/llm"
)

type AddResolveContext struct {
	Channel          string   `json:"channel,omitempty"`
	ChatType         string   `json:"chat_type,omitempty"`
	ChatID           int64    `json:"chat_id,omitempty"`
	SpeakerUserID    int64    `json:"speaker_user_id,omitempty"`
	SpeakerUsername  string   `json:"speaker_username,omitempty"`
	MentionUsernames []string `json:"mention_usernames,omitempty"`
	UserInputRaw     string   `json:"user_input_raw,omitempty"`
}

type MissingReference struct {
	Mention    string `json:"mention"`
	Suggestion string `json:"suggestion,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type MissingReferenceIDError struct {
	Items []MissingReference
}

func (e *MissingReferenceIDError) Error() string {
	if e == nil || len(e.Items) == 0 {
		return "missing_reference_id"
	}
	first := e.Items[0]
	mention := strings.TrimSpace(first.Mention)
	suggestion := strings.TrimSpace(first.Suggestion)
	switch {
	case mention != "" && suggestion != "":
		return fmt.Sprintf("missing_reference_id: mention=%q suggestion=%q", mention, suggestion)
	case mention != "":
		return fmt.Sprintf("missing_reference_id: mention=%q", mention)
	default:
		return "missing_reference_id"
	}
}

type LLMReferenceResolver struct {
	Client llm.Client
	Model  string
}

func NewLLMReferenceResolver(client llm.Client, model string) *LLMReferenceResolver {
	return &LLMReferenceResolver{
		Client: client,
		Model:  strings.TrimSpace(model),
	}
}

func (r *LLMReferenceResolver) ResolveAddContent(ctx context.Context, content string, people []string, snapshot ContactSnapshot, runtime AddResolveContext) (string, []string, error) {
	if err := r.validateReady(); err != nil {
		return "", nil, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", nil, fmt.Errorf("content is required")
	}
	runtime = normalizeAddResolveContext(runtime)
	people = normalizePeople(people)
	if runtime.UserInputRaw == "" {
		runtime.UserInputRaw = content
	}

	slog.Default().Debug("todo_reference_resolve_start",
		"content_len", len(content),
		"people_count", len(people),
		"channel", runtime.Channel,
		"chat_type", runtime.ChatType,
		"chat_id", runtime.ChatID,
		"speaker_user_id", runtime.SpeakerUserID,
		"speaker_username", runtime.SpeakerUsername,
		"mention_usernames_count", len(runtime.MentionUsernames),
		"user_input_raw_len", len(runtime.UserInputRaw),
		"contact_count", len(snapshot.Contacts),
		"reachable_id_count", len(snapshot.ReachableIDs),
	)

	runtimeMeta := runtimeclock.WithRuntimeClockMeta(map[string]any{
		"channel":           runtime.Channel,
		"chat_type":         runtime.ChatType,
		"chat_id":           runtime.ChatID,
		"speaker_user_id":   runtime.SpeakerUserID,
		"speaker_username":  runtime.SpeakerUsername,
		"mention_usernames": runtime.MentionUsernames,
	}, time.Now())

	payload := map[string]any{
		"input": map[string]any{
			"content": content,
			"people":  people,
		},
		"input_raw":     runtime.UserInputRaw,
		"runtime":       runtimeMeta,
		"contacts":      snapshot.Contacts,
		"reachable_ids": snapshot.ReachableIDs,
		"output_rules":  []string{"strict_json_only"},
	}
	input, _ := json.Marshal(payload)

	systemPrompt := strings.Join([]string{
		"You rewrite `input.content` by attaching explicit reference IDs to `input.people` mentions.",
		"Return strict JSON only.",
		`Output schema: { "status":"ok","rewritten_content":"..." }`,
		"Use `content` as the target text to rewrite.",
		"Use `contacts` as the primary list of people to resolve.",
		"Use `input_raw` and `runtime` context for disambiguation.",
		"If `input_raw` mentions a time (explicit or relative), resolve it with current time context in `runtime` (now_local/now_utc) and rewrite it as exact `YYYY-MM-DD hh:mm`.",
		"Must consider first-person references with speaker context: ",
		"- if the speaker mention themselves (like 'I', 'me', '$SPEAKER'), resolve to their own contact if available; similarly, resolve mentions of the speaker's direct interlocutors to those contacts if available;",
		"- if runtime.speaker_username is already a valid `protocol:id`, use it as the speaker reference id;",
		"Attach IDs as `[Name](protocol:id)`, where id may be from reachable_ids, example input:",
		"Notice $SPEAKER to tell Alice invites Bob to the meeting of Lucy.",
		"and rewritten content (assume the $SPEAKER is 'Lyric'): ",
		"Notice [Lyric](tg:98765) to tell [Alice](tg:12345) invites [Bob](slack:T123:D456) to the meeting of [Lucy](tg:@lucy).",
		"If any person in `people` cannot be uniquely resolved to one reachable id, keep it in the rewritten content in the same form",
		"Preserve original language and intent. Keep the sentence natural.",
	}, " ")

	res, err := r.Client.Chat(ctx, llm.Request{
		Model:     r.Model,
		Scene:     "todo.reference_resolve",
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(input)},
		},
		Parameters: map[string]any{
			"temperature": 0,
			"max_tokens":  1200,
		},
	})
	if err != nil {
		slog.Default().Debug("todo_reference_resolve_failed", "error", err.Error())
		return "", nil, err
	}

	var out struct {
		Status    string             `json:"status"`
		Rewritten string             `json:"rewritten_content"`
		Warnings  []string           `json:"warnings,omitempty"`
		Missing   []MissingReference `json:"missing,omitempty"`
	}
	if err := jsonutil.DecodeWithFallback(res.Text, &out); err != nil {
		slog.Default().Debug("todo_reference_resolve_failed", "error", err.Error(), "response_text", res.Text)
		return "", nil, fmt.Errorf("invalid reference_resolve response: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(out.Status)) {
	case "ok":
		rewritten := strings.TrimSpace(out.Rewritten)
		if rewritten == "" {
			err := fmt.Errorf("reference_resolve returned empty rewritten_content")
			slog.Default().Debug("todo_reference_resolve_failed", "error", err.Error())
			return "", nil, err
		}
		warnings := normalizeWarnings(out.Warnings)
		slog.Default().Debug("todo_reference_resolve_ok",
			"rewritten", rewritten,
			"warnings_count", len(warnings),
		)
		return rewritten, warnings, nil
	case "missing_reference_id":
		items := normalizeMissingReferences(out.Missing)
		slog.Default().Debug("todo_reference_missing_ids", "count", len(items))
		if len(items) == 0 {
			return "", nil, &MissingReferenceIDError{}
		}
		return "", nil, &MissingReferenceIDError{Items: items}
	default:
		err := fmt.Errorf("invalid reference_resolve status: %s", strings.TrimSpace(out.Status))
		slog.Default().Debug("todo_reference_resolve_failed", "error", err.Error())
		return "", nil, err
	}
}

func (r *LLMReferenceResolver) validateReady() error {
	if r == nil || r.Client == nil {
		return fmt.Errorf("todo reference resolver missing llm client")
	}
	if strings.TrimSpace(r.Model) == "" {
		return fmt.Errorf("todo reference resolver missing llm model")
	}
	return nil
}

func normalizeAddResolveContext(in AddResolveContext) AddResolveContext {
	out := AddResolveContext{
		Channel:       strings.ToLower(strings.TrimSpace(in.Channel)),
		ChatType:      strings.ToLower(strings.TrimSpace(in.ChatType)),
		ChatID:        in.ChatID,
		SpeakerUserID: in.SpeakerUserID,
		UserInputRaw:  strings.TrimSpace(in.UserInputRaw),
		SpeakerUsername: func() string {
			v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(in.SpeakerUsername), "@"))
			return v
		}(),
	}
	if len(in.MentionUsernames) > 0 {
		out.MentionUsernames = normalizePeople(in.MentionUsernames)
	}
	return out
}

func normalizePeople(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	out := make([]string, 0, len(input))
	seen := make(map[string]bool, len(input))
	for _, raw := range input {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
	}
	return out
}

func normalizeMissingReferences(items []MissingReference) []MissingReference {
	if len(items) == 0 {
		return nil
	}
	out := make([]MissingReference, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		n := MissingReference{
			Mention:    strings.TrimSpace(item.Mention),
			Suggestion: strings.TrimSpace(item.Suggestion),
			Reason:     strings.TrimSpace(item.Reason),
		}
		if n.Mention == "" && n.Suggestion == "" {
			continue
		}
		key := n.Mention + "|" + n.Suggestion
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, n)
	}
	return out
}

func normalizeWarnings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, raw := range items {
		v := strings.TrimSpace(raw)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
