package telegram

import (
	"context"
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
	"gopkg.in/yaml.v3"
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
	Message   string   `json:"message"`
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
	if strings.TrimSpace(frontMatterStatus(string(body))) == "" {
		body = []byte(setFrontMatterStatus(string(body), "draft"))
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}
	return nil
}

func buildInitQuestions(ctx context.Context, client llm.Client, model string, draft initProfileDraft, userText string) ([]string, string, error) {
	defaultQuestions := defaultInitQuestions(userText)
	defaultMessage := fallbackInitQuestionMessage(defaultQuestions, userText)
	if client == nil || strings.TrimSpace(model) == "" {
		return defaultQuestions, defaultMessage, nil
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
	systemPrompt, userPrompt, err := renderInitQuestionsPrompts(payload)
	if err != nil {
		return defaultQuestions, defaultMessage, err
	}

	res, err := client.Chat(ctx, llm.Request{
		Model:     strings.TrimSpace(model),
		Scene:     "telegram.init_questions",
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Parameters: map[string]any{
			"temperature": 0.4,
			"max_tokens":  500,
		},
	})
	if err != nil {
		return defaultQuestions, defaultMessage, err
	}

	var out initQuestionsOutput
	if err := jsonutil.DecodeWithFallback(strings.TrimSpace(res.Text), &out); err != nil {
		return defaultQuestions, defaultMessage, err
	}
	questions := normalizeInitQuestions(out.Questions)
	if len(questions) == 0 {
		questions = defaultQuestions
	}
	message := strings.TrimSpace(out.Message)
	if message == "" {
		message = fallbackInitQuestionMessage(questions, userText)
	}
	return questions, message, nil
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
	soulFinal = polishInitSoulMarkdown(ctx, client, model, soulFinal)

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

func humanizeSoulProfile(ctx context.Context, client llm.Client, model string) (bool, error) {
	stateDir := statepaths.FileStateDir()
	if err := ensureInitProfileFiles(stateDir); err != nil {
		return false, err
	}
	soulPath := filepath.Join(stateDir, "SOUL.md")
	soulRawBytes, err := os.ReadFile(soulPath)
	if err != nil {
		return false, fmt.Errorf("read SOUL.md: %w", err)
	}
	original := strings.ReplaceAll(string(soulRawBytes), "\r\n", "\n")
	polished := polishInitSoulMarkdown(ctx, client, model, original)
	if polished == original {
		return false, nil
	}
	if err := writeFilePreservePerm(soulPath, []byte(polished)); err != nil {
		return false, fmt.Errorf("write SOUL.md: %w", err)
	}
	return true, nil
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
	systemPrompt, userPrompt, err := renderInitPostGreetingPrompts(payload)
	if err != nil {
		return fallbackPostInitGreeting(userAnswer, fallback), err
	}

	res, err := client.Chat(ctx, llm.Request{
		Model:     strings.TrimSpace(model),
		Scene:     "telegram.init_post_greeting",
		ForceJSON: false,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
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
	systemPrompt, userPrompt, err := renderInitFillPrompts(payload)
	if err != nil {
		return fallback, nil
	}

	res, err := client.Chat(ctx, llm.Request{
		Model:     strings.TrimSpace(model),
		Scene:     "telegram.init_fill",
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
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
	name = strings.TrimPrefix(name, "@")
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
	if out, ok := replaceIdentityYAMLBlock(raw, fill); ok {
		return out
	}
	out := raw
	out = replaceIdentityField(out, "Name", fill.Identity.Name)
	out = replaceIdentityField(out, "Creature", fill.Identity.Creature)
	out = replaceIdentityField(out, "Vibe", fill.Identity.Vibe)
	out = replaceIdentityField(out, "Emoji", fill.Identity.Emoji)
	return out
}

func replaceIdentityYAMLBlock(raw string, fill initFillOutput) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	start := -1
	end := -1
	for i, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if start < 0 {
			lowerFence := strings.ToLower(line)
			if strings.HasPrefix(lowerFence, "```yaml") || strings.HasPrefix(lowerFence, "```yml") {
				start = i
			}
			continue
		}
		if strings.HasPrefix(line, "```") {
			end = i
			break
		}
	}
	if start < 0 || end <= start {
		return "", false
	}

	block, err := updateIdentityYAMLBlock(strings.Join(lines[start+1:end], "\n"), fill)
	if err != nil {
		return "", false
	}

	replacement := append([]string{}, lines[:start+1]...)
	replacement = append(replacement, strings.Split(strings.TrimRight(block, "\n"), "\n")...)
	replacement = append(replacement, lines[end:]...)
	return strings.Join(replacement, "\n"), true
}

func updateIdentityYAMLBlock(raw string, fill initFillOutput) (string, error) {
	var doc yaml.Node
	if strings.TrimSpace(raw) == "" {
		doc = yaml.Node{
			Kind: yaml.DocumentNode,
			Content: []*yaml.Node{{
				Kind: yaml.MappingNode,
				Tag:  "!!map",
			}},
		}
	} else if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return "", err
	}

	root, err := identityYAMLMapping(&doc)
	if err != nil {
		return "", err
	}
	setIdentityYAMLScalar(root, "name", fill.Identity.Name)
	setIdentityYAMLScalar(root, "creature", fill.Identity.Creature)
	setIdentityYAMLScalar(root, "vibe", fill.Identity.Vibe)
	setIdentityYAMLScalar(root, "emoji", fill.Identity.Emoji)

	var out strings.Builder
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		_ = enc.Close()
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func identityYAMLMapping(doc *yaml.Node) (*yaml.Node, error) {
	if doc == nil {
		return nil, fmt.Errorf("identity yaml doc is nil")
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		}
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("identity yaml root must be a mapping")
	}
	return doc, nil
}

func setIdentityYAMLScalar(node *yaml.Node, key string, value string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	trimmed := strings.TrimSpace(value)
	for i := 0; i+1 < len(node.Content); i += 2 {
		if strings.TrimSpace(node.Content[i].Value) != key {
			continue
		}
		node.Content[i+1].Kind = yaml.ScalarNode
		node.Content[i+1].Tag = "!!str"
		node.Content[i+1].Value = trimmed
		return
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: trimmed},
	)
}

func applySoulSections(raw string, fill initFillOutput) string {
	out := raw
	out = replaceMarkdownSection(out, "Core Truths", formatBulletList(fill.Soul.CoreTruths))
	out = replaceMarkdownSection(out, "Boundaries", formatBulletList(fill.Soul.Boundaries))
	out = replaceMarkdownSection(out, "Vibe", strings.TrimSpace(fill.Soul.Vibe))
	return out
}

func polishInitSoulMarkdown(ctx context.Context, client llm.Client, model string, soulMarkdown string) string {
	original := strings.ReplaceAll(soulMarkdown, "\r\n", "\n")
	if client == nil || strings.TrimSpace(model) == "" {
		return original
	}

	payload := map[string]any{
		"soul_markdown": original,
	}
	systemPrompt, userPrompt, err := renderInitSoulPolishPrompts(payload)
	if err != nil {
		return original
	}

	res, err := client.Chat(ctx, llm.Request{
		Model:     strings.TrimSpace(model),
		Scene:     "telegram.init_soul_polish",
		ForceJSON: false,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Parameters: map[string]any{
			"temperature": 1,
			"max_tokens":  1200,
		},
	})
	if err != nil {
		return original
	}

	polished := sanitizeMarkdownRewrite(res.Text)
	if polished == "" || !looksLikeSoulMarkdown(polished) {
		return original
	}
	return setFrontMatterStatus(polished, "done")
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

func sanitizeMarkdownRewrite(raw string) string {
	text := strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) >= 3 {
			start := strings.TrimSpace(lines[0])
			if strings.HasPrefix(start, "```") {
				for i := len(lines) - 1; i >= 1; i-- {
					if strings.TrimSpace(lines[i]) == "```" {
						text = strings.TrimSpace(strings.Join(lines[1:i], "\n"))
						break
					}
				}
			}
		}
	}
	return strings.TrimSpace(text)
}

func looksLikeSoulMarkdown(raw string) bool {
	lower := strings.ToLower(strings.ReplaceAll(raw, "\r\n", "\n"))
	return strings.Contains(lower, "## core truths") &&
		strings.Contains(lower, "## boundaries") &&
		strings.Contains(lower, "## vibe")
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
