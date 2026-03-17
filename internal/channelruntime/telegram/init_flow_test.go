package telegram

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
	"github.com/spf13/viper"
)

func TestLoadInitProfileDraftCreatesMissingFiles(t *testing.T) {
	workspaceDir := t.TempDir()
	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", workspaceDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", prevStateDir)
	})

	draft, err := loadInitProfileDraft()
	if err != nil {
		t.Fatalf("loadInitProfileDraft() error = %v", err)
	}
	if draft.IdentityStatus != "draft" {
		t.Fatalf("identity status mismatch: got %q", draft.IdentityStatus)
	}
	if draft.SoulStatus != "draft" {
		t.Fatalf("soul status mismatch: got %q", draft.SoulStatus)
	}
	if _, err := os.Stat(filepath.Join(workspaceDir, "IDENTITY.md")); err != nil {
		t.Fatalf("IDENTITY.md should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspaceDir, "SOUL.md")); err != nil {
		t.Fatalf("SOUL.md should exist: %v", err)
	}
}

func TestDefaultInitQuestionsEnglish(t *testing.T) {
	questions := defaultInitQuestions("hello there")
	if len(questions) == 0 {
		t.Fatalf("expected default questions")
	}
	for _, q := range questions {
		for _, r := range q {
			if r >= 0x4E00 && r <= 0x9FFF {
				t.Fatalf("default question should be English, got: %q", q)
			}
		}
	}
}

func TestDefaultInitQuestionsChinese(t *testing.T) {
	questions := defaultInitQuestions("你好，先聊一下")
	if len(questions) == 0 {
		t.Fatalf("expected default questions")
	}
	hasCJK := false
	for _, q := range questions {
		for _, r := range q {
			if r >= 0x4E00 && r <= 0x9FFF {
				hasCJK = true
				break
			}
		}
		if hasCJK {
			break
		}
	}
	if !hasCJK {
		t.Fatalf("expected Chinese questions for Chinese input")
	}
}

func TestSetFrontMatterStatus(t *testing.T) {
	input := "---\nstatus: draft\n---\n\n# X\n"
	got := setFrontMatterStatus(input, "done")
	if !strings.Contains(got, "status: done") {
		t.Fatalf("expected done status, got: %s", got)
	}
}

func TestSetFrontMatterStatusWithoutFrontMatter(t *testing.T) {
	input := "# X\n"
	got := setFrontMatterStatus(input, "done")
	if !strings.HasPrefix(got, "---\nstatus: done\n---\n\n") {
		t.Fatalf("expected front matter prepend, got: %s", got)
	}
}

func TestApplyIdentityFields(t *testing.T) {
	raw := "---\nstatus: draft\n---\n\n# IDENTITY.md\n\n```yaml\nname: \"\"\nname_alts: [\"John\", \"Mr. Wick\"]\ncreature: \"\"\nvibe: \"\"\nemoji: \"\"\n```\n\n*This isn't just metadata.*\n"
	fill := defaultInitFill("tester", "Tester")
	fill.Identity.Name = "Nova"
	fill.Identity.Creature = "robot"
	fill.Identity.Vibe = "warm and sharp"
	fill.Identity.Emoji = "✨"

	out := applyIdentityFields(raw, fill)
	if !strings.Contains(out, "name: \"Nova\"") {
		t.Fatalf("yaml block not applied: %s", out)
	}
	if !strings.Contains(out, "creature: \"robot\"") || !strings.Contains(out, "vibe: \"warm and sharp\"") || !strings.Contains(out, "emoji: \"✨\"") {
		t.Fatalf("identity fields not fully applied: %s", out)
	}
	if !strings.Contains(out, "name_alts: [\"John\", \"Mr. Wick\"]") {
		t.Fatalf("extra yaml keys should be preserved: %s", out)
	}
	if !strings.Contains(out, "*This isn't just metadata.*") {
		t.Fatalf("markdown outside yaml block should be preserved: %s", out)
	}
}

func TestApplyIdentityFieldsLegacyFallback(t *testing.T) {
	raw := "---\nstatus: draft\n---\n\n# IDENTITY.md\n\n- **Name:**\n  *(pick one)*\n- **Creature:**\n  *(pick one)*\n- **Vibe:**\n  *(pick one)*\n- **Emoji:**\n  *(pick one)*\n"
	fill := defaultInitFill("tester", "Tester")
	fill.Identity.Name = "Nova"
	fill.Identity.Creature = "robot"
	fill.Identity.Vibe = "warm and sharp"
	fill.Identity.Emoji = "✨"

	out := applyIdentityFields(raw, fill)
	if !strings.Contains(out, "- **Name:** Nova") {
		t.Fatalf("legacy name not applied: %s", out)
	}
	if strings.Contains(out, "*(pick one)*") {
		t.Fatalf("legacy placeholder should be removed: %s", out)
	}
}

func TestApplySoulSections(t *testing.T) {
	raw := "---\nstatus: draft\n---\n\n# SOUL.md\n\n## Core Truths\nold\n\n## Boundaries\nold\n\n## Vibe\nold\n"
	fill := defaultInitFill("tester", "Tester")
	fill.Soul.CoreTruths = []string{"A", "B", "C"}
	fill.Soul.Boundaries = []string{"X", "Y", "Z"}
	fill.Soul.Vibe = "concise"

	out := applySoulSections(raw, fill)
	if !strings.Contains(out, "## Core Truths") || !strings.Contains(out, "- A") {
		t.Fatalf("core truths not applied: %s", out)
	}
	if !strings.Contains(out, "## Boundaries") || !strings.Contains(out, "- X") {
		t.Fatalf("boundaries not applied: %s", out)
	}
	if !strings.Contains(out, "## Vibe\n\nconcise") {
		t.Fatalf("vibe not applied: %s", out)
	}
}

func TestPolishInitSoulMarkdownBypassesWithoutClient(t *testing.T) {
	input := "---\nstatus: done\n---\n\n# SOUL.md\n\n## Core Truths\n- A\n\n## Boundaries\n- B\n\n## Vibe\n\nC\n"
	out := polishInitSoulMarkdown(context.Background(), nil, "model", input)
	if out != input {
		t.Fatalf("expected unchanged markdown without client")
	}
}

func TestPolishInitSoulMarkdownFallbackOnInvalidOutput(t *testing.T) {
	input := "---\nstatus: done\n---\n\n# SOUL.md\n\n## Core Truths\n- A\n\n## Boundaries\n- B\n\n## Vibe\n\nC\n"
	client := &stubInitLLMClient{Text: "hello"}
	out := polishInitSoulMarkdown(context.Background(), client, "model", input)
	if out != input {
		t.Fatalf("expected fallback to original markdown, got: %s", out)
	}
}

func TestPolishInitSoulMarkdownAppliesValidRewrite(t *testing.T) {
	input := "---\nstatus: done\n---\n\n# SOUL.md\n\n## Core Truths\n- A\n\n## Boundaries\n- B\n\n## Vibe\n\nC\n"
	client := &stubInitLLMClient{
		Text: "```markdown\n# SOUL.md\n\n## Core Truths\n- Have a take.\n\n## Boundaries\n- Keep secrets secret.\n\n## Vibe\n\nFast and sharp.\n\nBe the assistant you'd actually want to talk to at 2am. Not a corporate drone. Not a sycophant. Just... good.\n```",
	}
	out := polishInitSoulMarkdown(context.Background(), client, "model", input)
	if !strings.Contains(out, "status: done") {
		t.Fatalf("expected done status in polished markdown: %s", out)
	}
	if !strings.Contains(out, "## Core Truths") || !strings.Contains(out, "## Boundaries") || !strings.Contains(out, "## Vibe") {
		t.Fatalf("expected required sections in polished markdown: %s", out)
	}
	if !strings.Contains(out, "Be the assistant you'd actually want to talk to at 2am.") {
		t.Fatalf("expected mandatory vibe line in polished markdown: %s", out)
	}
	if strings.Contains(out, "```") {
		t.Fatalf("expected markdown fences stripped: %s", out)
	}
}

func TestHumanizeSoulProfileUpdatesFileOnValidRewrite(t *testing.T) {
	workspaceDir := t.TempDir()
	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", workspaceDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", prevStateDir)
	})

	input := "---\nstatus: done\n---\n\n# SOUL.md\n\n## Core Truths\n- A\n\n## Boundaries\n- B\n\n## Vibe\n\nC\n"
	soulPath := filepath.Join(workspaceDir, "SOUL.md")
	if err := os.WriteFile(soulPath, []byte(input), 0o644); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}

	client := &stubInitLLMClient{
		Text: "# SOUL.md\n\n## Core Truths\n- Have a take.\n\n## Boundaries\n- Keep secrets secret.\n\n## Vibe\n\nFast and sharp.\n\nBe the assistant you'd actually want to talk to at 2am. Not a corporate drone. Not a sycophant. Just... good.",
	}
	updated, err := humanizeSoulProfile(context.Background(), client, "model")
	if err != nil {
		t.Fatalf("humanizeSoulProfile() error = %v", err)
	}
	if !updated {
		t.Fatalf("expected SOUL.md to be updated")
	}

	outBytes, err := os.ReadFile(soulPath)
	if err != nil {
		t.Fatalf("read SOUL.md: %v", err)
	}
	out := string(outBytes)
	if !strings.Contains(out, "status: done") {
		t.Fatalf("expected done status after humanize: %s", out)
	}
	if !strings.Contains(out, "Have a take.") {
		t.Fatalf("expected rewritten content: %s", out)
	}
}

func TestHumanizeSoulProfileKeepsOriginalWhenRewriteInvalid(t *testing.T) {
	workspaceDir := t.TempDir()
	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", workspaceDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", prevStateDir)
	})

	input := "---\nstatus: done\n---\n\n# SOUL.md\n\n## Core Truths\n- A\n\n## Boundaries\n- B\n\n## Vibe\n\nC\n"
	soulPath := filepath.Join(workspaceDir, "SOUL.md")
	if err := os.WriteFile(soulPath, []byte(input), 0o644); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}

	client := &stubInitLLMClient{Text: "hello"}
	updated, err := humanizeSoulProfile(context.Background(), client, "model")
	if err != nil {
		t.Fatalf("humanizeSoulProfile() error = %v", err)
	}
	if updated {
		t.Fatalf("expected SOUL.md to stay unchanged")
	}

	outBytes, err := os.ReadFile(soulPath)
	if err != nil {
		t.Fatalf("read SOUL.md: %v", err)
	}
	if string(outBytes) != input {
		t.Fatalf("expected original SOUL.md preserved, got: %s", string(outBytes))
	}
}

type stubInitLLMClient struct {
	Text string
	Err  error
}

func (s *stubInitLLMClient) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	if s.Err != nil {
		return llm.Result{}, s.Err
	}
	return llm.Result{Text: s.Text}, nil
}
