package telegramcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	raw := "---\nstatus: draft\n---\n\n# IDENTITY.md\n\n- **Name:**\n  *(pick one)*\n- **Creature:**\n  *(pick one)*\n- **Vibe:**\n  *(pick one)*\n- **Emoji:**\n  *(pick one)*\n"
	fill := defaultInitFill("tester", "Tester")
	fill.Identity.Name = "Nova"
	fill.Identity.Creature = "robot"
	fill.Identity.Vibe = "warm and sharp"
	fill.Identity.Emoji = "✨"

	out := applyIdentityFields(raw, fill)
	if !strings.Contains(out, "- **Name:** Nova") {
		t.Fatalf("name not applied: %s", out)
	}
	if strings.Contains(out, "*(pick one)*") {
		t.Fatalf("placeholder should be removed: %s", out)
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
