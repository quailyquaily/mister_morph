package telegramcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/quailyquaily/mistermorph/assets"
	"github.com/quailyquaily/mistermorph/internal/jsonutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/llm"
)

var errInitProfilesNotDraft = errors.New("init requires both IDENTITY.md and SOUL.md with status=draft")

type telegramInitSession struct {
	Questions []string
	StartedAt time.Time
}

type initProfileDraft struct {
	IdentityPath   string
	IdentityRaw    string
	IdentityStatus string
	SoulPath       string
	SoulRaw        string
	SoulStatus     string
}

type initQuestionsOutput struct {
	Questions []string `json:"questions"`
}

type initFillOutput struct {
	Identity struct {
		Name     string `json:"name"`
		Creature string `json:"creature"`
		Vibe     string `json:"vibe"`
		Emoji    string `json:"emoji"`
	} `json:"identity"`
	Soul struct {
		CoreTruths []string `json:"core_truths"`
		Boundaries []string `json:"boundaries"`
		Vibe       string   `json:"vibe"`
	} `json:"soul"`
}

type initApplyResult struct {
	Name string
	Vibe string
}

func loadInitProfileDraft() (initProfileDraft, error) {
	stateDir := statepaths.FileStateDir()
	if err := ensureInitProfileFiles(stateDir); err != nil {
		return initProfileDraft{}, err
	}
	identityPath := filepath.Join(stateDir, "IDENTITY.md")
	soulPath := filepath.Join(stateDir, "SOUL.md")

	identityRawBytes, err := os.ReadFile(identityPath)
	if err != nil {
		return initProfileDraft{}, fmt.Errorf("read IDENTITY.md: %w", err)
	}
	soulRawBytes, err := os.ReadFile(soulPath)
	if err != nil {
		return initProfileDraft{}, fmt.Errorf("read SOUL.md: %w", err)
	}

	draft := initProfileDraft{
		IdentityPath:   identityPath,
		IdentityRaw:    strings.ReplaceAll(string(identityRawBytes), "\r\n", "\n"),
		IdentityStatus: strings.ToLower(strings.TrimSpace(frontMatterStatus(string(identityRawBytes)))),
		SoulPath:       soulPath,
		SoulRaw:        strings.ReplaceAll(string(soulRawBytes), "\r\n", "\n"),
		SoulStatus:     strings.ToLower(strings.TrimSpace(frontMatterStatus(string(soulRawBytes)))),
	}
	if draft.IdentityStatus != "draft" || draft.SoulStatus != "draft" {
		return draft, fmt.Errorf("%w (identity=%q soul=%q)", errInitProfilesNotDraft, draft.IdentityStatus, draft.SoulStatus)
	}
	return draft, nil
}

func ensureInitProfileFiles(stateDir string) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("create file_state_dir: %w", err)
	}
	identityPath := filepath.Join(stateDir, "IDENTITY.md")
	soulPath := filepath.Join(stateDir, "SOUL.md")
	if err := ensureFileFromTemplate(identityPath, "config/IDENTITY.md"); err != nil {
		return err
	}
	if err := ensureFileFromTemplate(soulPath, "config/SOUL.md"); err != nil {
		return err
	}
	return nil
}

func ensureFileFromTemplate(path string, templatePath string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", filepath.Base(path), err)
	}
	body, err := assets.ConfigFS.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read embedded %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}
	return nil
}

func buildInitQuestions(ctx context.Context, client llm.Client, model string, draft initProfileDraft, userText string) ([]string, error) {
	if client == nil || strings.TrimSpace(model) == "" {
		return defaultInitQuestions(userText), nil
	}
	payload := map[string]any{
		"identity_markdown": draft.IdentityRaw,
		"soul_markdown":     draft.SoulRaw,
		"user_text":         strings.TrimSpace(userText),
		"required_targets": map[string]any{
			"identity": []string{"Name", "Creature", "Vibe", "Emoji"},
			"soul":     []string{"Core Truths", "Boundaries", "Vibe"},
		},
		"question_count": map[string]int{"min": 4, "max": 7},
	}
	rawPayload, _ := json.Marshal(payload)

	systemPrompt := strings.Join([]string{
		"You design onboarding questions for an assistant persona bootstrap.",
		"Return JSON only: {\"questions\":[string,...]}.",
		"Use the same language as user_text for all questions.",
		"Questions must collect enough info to fill identity Name/Creature/Vibe/Emoji and soul Core Truths/Boundaries/Vibe.",
		"If info is likely missing, ask preference-oriented questions that allow reasonable inference.",
		"Do not include explanations, only concise questions.",
	}, " ")

	res, err := client.Chat(ctx, llm.Request{
		Model:     strings.TrimSpace(model),
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(rawPayload)},
		},
		Parameters: map[string]any{
			"temperature": 0.4,
			"max_tokens":  500,
		},
	})
	if err != nil {
		return defaultInitQuestions(userText), err
	}

	var out initQuestionsOutput
	if err := jsonutil.DecodeWithFallback(strings.TrimSpace(res.Text), &out); err != nil {
		return defaultInitQuestions(userText), err
	}
	questions := normalizeInitQuestions(out.Questions)
	if len(questions) == 0 {
		questions = defaultInitQuestions(userText)
	}
	return questions, nil
}

func applyInitFromAnswer(ctx context.Context, client llm.Client, model string, draft initProfileDraft, session telegramInitSession, answer string, username string, displayName string) (initApplyResult, error) {
	fill, err := buildInitFill(ctx, client, model, draft, session, answer, username, displayName)
	if err != nil {
		return initApplyResult{}, err
	}

	identityFinal := applyIdentityFields(draft.IdentityRaw, fill)
	identityFinal = setFrontMatterStatus(identityFinal, "done")

	soulFinal := applySoulSections(draft.SoulRaw, fill)
	soulFinal = setFrontMatterStatus(soulFinal, "done")

	if err := writeFilePreservePerm(draft.IdentityPath, []byte(identityFinal)); err != nil {
		return initApplyResult{}, fmt.Errorf("write IDENTITY.md: %w", err)
	}
	if err := writeFilePreservePerm(draft.SoulPath, []byte(soulFinal)); err != nil {
		return initApplyResult{}, fmt.Errorf("write SOUL.md: %w", err)
	}

	return initApplyResult{
		Name: strings.TrimSpace(fill.Identity.Name),
		Vibe: strings.TrimSpace(fill.Identity.Vibe),
	}, nil
}

func generatePostInitGreeting(ctx context.Context, client llm.Client, model string, draft initProfileDraft, session telegramInitSession, userAnswer string, fallback initApplyResult) (string, error) {
	if client == nil || strings.TrimSpace(model) == "" {
		return fallbackPostInitGreeting(userAnswer, fallback), nil
	}
	identityRaw, err := os.ReadFile(draft.IdentityPath)
	if err != nil {
		return fallbackPostInitGreeting(userAnswer, fallback), fmt.Errorf("read IDENTITY.md: %w", err)
	}
	soulRaw, err := os.ReadFile(draft.SoulPath)
	if err != nil {
		return fallbackPostInitGreeting(userAnswer, fallback), fmt.Errorf("read SOUL.md: %w", err)
	}

	payload := map[string]any{
		"identity_markdown": string(identityRaw),
		"soul_markdown":     string(soulRaw),
		"context": map[string]any{
			"init_questions": session.Questions,
			"user_answer":    strings.TrimSpace(userAnswer),
		},
	}
	rawPayload, _ := json.Marshal(payload)

	systemPrompt := strings.Join([]string{
		"You are replying in Telegram private chat immediately after persona bootstrap.",
		"Use identity_markdown and soul_markdown as authoritative persona.",
		"Reply naturally in the same language as user_answer.",
		"Keep it concise (1-3 short sentences), warm and conversational.",
		"Do NOT mention initialization flow, files, status fields, or internal process.",
	}, " ")

	res, err := client.Chat(ctx, llm.Request{
		Model:     strings.TrimSpace(model),
		ForceJSON: false,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(rawPayload)},
		},
		Parameters: map[string]any{
			"temperature": 0.7,
			"max_tokens":  220,
		},
	})
	if err != nil {
		return fallbackPostInitGreeting(userAnswer, fallback), err
	}
	text := strings.TrimSpace(res.Text)
	if text == "" {
		return fallbackPostInitGreeting(userAnswer, fallback), nil
	}
	return text, nil
}

func fallbackPostInitGreeting(userAnswer string, result initApplyResult) string {
	name := strings.TrimSpace(result.Name)
	if preferredInitLanguage(userAnswer) == "zh" {
		if name != "" {
			return fmt.Sprintf("嗨，我是 %s。很高兴认识你，我们继续聊。", name)
		}
		return "嗨，很高兴认识你。我们继续聊。"
	}
	if name != "" {
		return fmt.Sprintf("Hi, I’m %s. Great to meet you. Let’s keep going.", name)
	}
	return "Hi. Great to meet you. Let’s keep going."
}

func generateInitQuestionMessage(ctx context.Context, client llm.Client, model string, questions []string, userText string) (string, error) {
	normalized := normalizeInitQuestions(questions)
	if len(normalized) == 0 {
		normalized = defaultInitQuestions(userText)
	}
	if client == nil || strings.TrimSpace(model) == "" {
		return fallbackInitQuestionMessage(normalized, userText), nil
	}

	payload := map[string]any{
		"user_text": strings.TrimSpace(userText),
		"questions": normalized,
	}
	rawPayload, _ := json.Marshal(payload)

	systemPrompt := strings.Join([]string{
		"You are sending a Telegram private message that asks persona-setup questions.",
		"Use the same language as user_text.",
		"Write naturally and conversationally, not as a workflow/status message.",
		"Do not mention initialization, files, status fields, or internal process.",
		"Ask the listed questions clearly and invite user to answer in one reply.",
	}, " ")

	res, err := client.Chat(ctx, llm.Request{
		Model:     strings.TrimSpace(model),
		ForceJSON: false,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(rawPayload)},
		},
		Parameters: map[string]any{
			"temperature": 0.7,
			"max_tokens":  280,
		},
	})
	if err != nil {
		return fallbackInitQuestionMessage(normalized, userText), err
	}
	text := strings.TrimSpace(res.Text)
	if text == "" {
		return fallbackInitQuestionMessage(normalized, userText), nil
	}
	return text, nil
}

func fallbackInitQuestionMessage(questions []string, userText string) string {
	var b strings.Builder
	if preferredInitLanguage(userText) == "zh" {
		b.WriteString("我想更了解你希望我成为什么样子。你可以一次性回答下面这些问题：\n\n")
		for i, q := range questions {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(q)))
		}
		return strings.TrimSpace(b.String())
	}
	b.WriteString("I want to understand how you'd like me to be. Could you answer these in one reply?\n\n")
	for i, q := range questions {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(q)))
	}
	return strings.TrimSpace(b.String())
}

func buildInitFill(ctx context.Context, client llm.Client, model string, draft initProfileDraft, session telegramInitSession, answer string, username string, displayName string) (initFillOutput, error) {
	fallback := defaultInitFill(username, displayName)
	if client == nil || strings.TrimSpace(model) == "" {
		return fallback, nil
	}

	payload := map[string]any{
		"identity_markdown": draft.IdentityRaw,
		"soul_markdown":     draft.SoulRaw,
		"questions":         session.Questions,
		"user_answer":       strings.TrimSpace(answer),
		"telegram_context": map[string]any{
			"username":     strings.TrimSpace(username),
			"display_name": strings.TrimSpace(displayName),
		},
		"targets": map[string]any{
			"identity": []string{"name", "creature", "vibe", "emoji"},
			"soul":     []string{"core_truths", "boundaries", "vibe"},
		},
	}
	rawPayload, _ := json.Marshal(payload)

	systemPrompt := strings.Join([]string{
		"You fill assistant persona fields from onboarding answers.",
		"Return JSON only with schema:",
		"{\"identity\":{\"name\":string,\"creature\":string,\"vibe\":string,\"emoji\":string},\"soul\":{\"core_truths\":[string],\"boundaries\":[string],\"vibe\":string}}.",
		"Never leave required fields empty.",
		"If user input is insufficient, infer plausible values from available context and produce complete output.",
		"core_truths and boundaries should each contain 3-6 concise items.",
	}, " ")

	res, err := client.Chat(ctx, llm.Request{
		Model:     strings.TrimSpace(model),
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(rawPayload)},
		},
		Parameters: map[string]any{
			"temperature": 0.5,
			"max_tokens":  900,
		},
	})
	if err != nil {
		return fallback, nil
	}

	var out initFillOutput
	if err := jsonutil.DecodeWithFallback(strings.TrimSpace(res.Text), &out); err != nil {
		return fallback, nil
	}
	return normalizeInitFill(out, fallback), nil
}

func normalizeInitQuestions(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
		if len(out) >= 7 {
			break
		}
	}
	return out
}

func defaultInitQuestions(userText string) []string {
	if preferredInitLanguage(userText) == "zh" {
		return []string{
			"你希望我叫什么名字？",
			"你希望我像什么样的存在（AI、机器人、精灵，或别的）？",
			"你希望我的说话风格是什么样（语气、节奏、表达方式）？",
			"你希望我坚持的三条核心原则是什么？",
			"有哪些边界是你希望我特别注意的？",
		}
	}
	return []string{
		"What name should I use for myself?",
		"What kind of being should I be (AI, robot, familiar, ghost, or something else)?",
		"What speaking vibe do you want from me (tone, pace, style)?",
		"What are the top three principles you want me to hold?",
		"What boundaries should I be especially careful about?",
	}
}

func preferredInitLanguage(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "en"
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return "zh"
		}
	}
	return "en"
}

func defaultInitFill(username string, displayName string) initFillOutput {
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = strings.TrimSpace(username)
	}
	if strings.HasPrefix(name, "@") {
		name = strings.TrimPrefix(name, "@")
	}
	if name == "" {
		name = "Morph"
	}

	var out initFillOutput
	out.Identity.Name = name
	out.Identity.Creature = "AI companion"
	out.Identity.Vibe = "direct, practical, and calm"
	out.Identity.Emoji = "🙂"
	out.Soul.CoreTruths = []string{
		"Be useful through concrete actions, not filler words.",
		"Prefer clear decisions and explicit tradeoffs.",
		"Protect private context and handle external actions carefully.",
	}
	out.Soul.Boundaries = []string{
		"Keep private data private.",
		"Ask before acting externally when uncertainty exists.",
		"Do not send low-quality or half-checked responses.",
	}
	out.Soul.Vibe = "Concise by default, thorough when it matters."
	return out
}

func normalizeInitFill(in initFillOutput, fallback initFillOutput) initFillOutput {
	out := in
	out.Identity.Name = fallbackIfEmpty(out.Identity.Name, fallback.Identity.Name)
	out.Identity.Creature = fallbackIfEmpty(out.Identity.Creature, fallback.Identity.Creature)
	out.Identity.Vibe = fallbackIfEmpty(out.Identity.Vibe, fallback.Identity.Vibe)
	out.Identity.Emoji = fallbackIfEmpty(out.Identity.Emoji, fallback.Identity.Emoji)
	out.Soul.Vibe = fallbackIfEmpty(out.Soul.Vibe, fallback.Soul.Vibe)
	out.Soul.CoreTruths = normalizeStringList(out.Soul.CoreTruths, fallback.Soul.CoreTruths, 3, 6)
	out.Soul.Boundaries = normalizeStringList(out.Soul.Boundaries, fallback.Soul.Boundaries, 3, 6)
	return out
}

func applyIdentityFields(raw string, fill initFillOutput) string {
	out := raw
	out = replaceIdentityField(out, "Name", fill.Identity.Name)
	out = replaceIdentityField(out, "Creature", fill.Identity.Creature)
	out = replaceIdentityField(out, "Vibe", fill.Identity.Vibe)
	out = replaceIdentityField(out, "Emoji", fill.Identity.Emoji)
	return out
}

func applySoulSections(raw string, fill initFillOutput) string {
	out := raw
	out = replaceMarkdownSection(out, "Core Truths", formatBulletList(fill.Soul.CoreTruths))
	out = replaceMarkdownSection(out, "Boundaries", formatBulletList(fill.Soul.Boundaries))
	out = replaceMarkdownSection(out, "Vibe", strings.TrimSpace(fill.Soul.Vibe))
	return out
}

func replaceIdentityField(raw string, label string, value string) string {
	lines := strings.Split(raw, "\n")
	targetPrefix := "- **" + strings.TrimSpace(label) + ":**"
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), targetPrefix) {
			continue
		}
		lines[i] = targetPrefix + " " + strings.TrimSpace(value)
		if i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if strings.HasPrefix(next, "*(") || strings.HasPrefix(next, "_(") {
				lines = append(lines[:i+1], lines[i+2:]...)
			}
		}
		return strings.Join(lines, "\n")
	}
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	lines = append(lines, targetPrefix+" "+strings.TrimSpace(value))
	return strings.Join(lines, "\n")
}

func replaceMarkdownSection(raw string, title string, body string) string {
	lines := strings.Split(raw, "\n")
	header := "## " + strings.TrimSpace(title)
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			start = i
			break
		}
	}

	contentLines := []string{}
	trimmedBody := strings.TrimSpace(body)
	if trimmedBody != "" {
		contentLines = strings.Split(trimmedBody, "\n")
	}

	if start < 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, header, "")
		lines = append(lines, contentLines...)
		lines = append(lines, "")
		return strings.Join(lines, "\n")
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}

	replacement := []string{header, ""}
	replacement = append(replacement, contentLines...)
	replacement = append(replacement, "")
	merged := make([]string, 0, start+len(replacement)+(len(lines)-end))
	merged = append(merged, lines[:start]...)
	merged = append(merged, replacement...)
	merged = append(merged, lines[end:]...)
	return strings.Join(merged, "\n")
}

func formatBulletList(items []string) string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		out = append(out, "- "+v)
	}
	return strings.Join(out, "\n")
}

func frontMatterStatus(raw string) string {
	fmLines, _, ok := splitFrontMatter(raw)
	if !ok {
		return ""
	}
	for _, line := range fmLines {
		key, value, ok := parseKeyValueLine(line)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "status") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func setFrontMatterStatus(raw string, status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		status = "done"
	}
	fmLines, body, hasFrontMatter := splitFrontMatter(raw)
	if !hasFrontMatter {
		body = strings.TrimLeft(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
		return "---\nstatus: " + status + "\n---\n\n" + body
	}

	updated := false
	for i := range fmLines {
		key, _, ok := parseKeyValueLine(fmLines[i])
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "status") {
			fmLines[i] = "status: " + status
			updated = true
			break
		}
	}
	if !updated {
		fmLines = append(fmLines, "status: "+status)
	}
	body = strings.TrimLeft(body, "\n")
	header := "---\n" + strings.Join(fmLines, "\n") + "\n---"
	if body == "" {
		return header + "\n"
	}
	return header + "\n\n" + body
}

func splitFrontMatter(raw string) ([]string, string, bool) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, raw, false
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end <= 0 {
		return nil, raw, false
	}
	fm := append([]string(nil), lines[1:end]...)
	body := strings.Join(lines[end+1:], "\n")
	return fm, body, true
}

func parseKeyValueLine(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	value := strings.TrimSpace(line[idx+1:])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

func writeFilePreservePerm(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return os.WriteFile(path, data, mode)
}

func fallbackIfEmpty(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func normalizeStringList(in []string, fallback []string, minLen int, maxLen int) []string {
	if maxLen <= 0 {
		maxLen = len(in)
	}
	if maxLen <= 0 {
		maxLen = len(fallback)
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
		if len(out) >= maxLen {
			break
		}
	}
	if len(out) >= minLen {
		return out
	}
	for _, item := range fallback {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
		if len(out) >= maxLen {
			break
		}
	}
	return out
}

func initFlowTimeout(requestTimeout time.Duration) time.Duration {
	if requestTimeout > 0 {
		timeout := requestTimeout
		if timeout < 15*time.Second {
			timeout = 15 * time.Second
		}
		if timeout > 90*time.Second {
			timeout = 90 * time.Second
		}
		return timeout
	}
	return 45 * time.Second
}
