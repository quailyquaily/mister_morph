package skillsutil

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
)

func TestPromptSpecWithSkills_LoadAllWildcard(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha")
	writeSkill(t, root, "beta")

	spec, loaded, err := PromptSpecWithSkills(
		context.Background(),
		nil,
		agent.DefaultLogOptions(),
		"task",
		nil,
		"gpt-5.2",
		SkillsConfig{
			Roots:     []string{root},
			Enabled:   true,
			Requested: []string{"*"},
		},
	)
	if err != nil {
		t.Fatalf("PromptSpecWithSkills: %v", err)
	}
	if len(spec.Skills) != 2 {
		t.Fatalf("expected 2 loaded skills, got %d", len(spec.Skills))
	}
	sort.Strings(loaded)
	if len(loaded) != 2 || loaded[0] != "alpha" || loaded[1] != "beta" {
		t.Fatalf("unexpected loaded skills: %#v", loaded)
	}
}

func TestPromptSpecWithSkills_LoadAllWhenRequestedEmpty(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha")
	writeSkill(t, root, "beta")

	spec, loaded, err := PromptSpecWithSkills(
		context.Background(),
		nil,
		agent.DefaultLogOptions(),
		"task",
		nil,
		"gpt-5.2",
		SkillsConfig{
			Roots:   []string{root},
			Enabled: true,
		},
	)
	if err != nil {
		t.Fatalf("PromptSpecWithSkills: %v", err)
	}
	if len(spec.Skills) != 2 {
		t.Fatalf("expected 2 loaded skills, got %d", len(spec.Skills))
	}
	sort.Strings(loaded)
	if len(loaded) != 2 || loaded[0] != "alpha" || loaded[1] != "beta" {
		t.Fatalf("unexpected loaded skills: %#v", loaded)
	}
}

func TestPromptSpecWithSkills_LoadAllWildcardIgnoresUnknownRequests(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha")
	writeSkill(t, root, "beta")

	_, loaded, err := PromptSpecWithSkills(
		context.Background(),
		nil,
		agent.DefaultLogOptions(),
		"task",
		nil,
		"gpt-5.2",
		SkillsConfig{
			Roots:     []string{root},
			Enabled:   true,
			Requested: []string{"*", "missing-skill"},
		},
	)
	if err != nil {
		t.Fatalf("PromptSpecWithSkills with wildcard should not fail on unknown skill: %v", err)
	}
	sort.Strings(loaded)
	if len(loaded) != 2 || loaded[0] != "alpha" || loaded[1] != "beta" {
		t.Fatalf("unexpected loaded skills: %#v", loaded)
	}
}

func TestPromptSpecWithSkills_LoadsTaskReferencedSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha")
	writeSkill(t, root, "beta")

	spec, loaded, err := PromptSpecWithSkills(
		context.Background(),
		nil,
		agent.DefaultLogOptions(),
		"use $beta for this task",
		nil,
		"gpt-5.2",
		SkillsConfig{
			Roots:   []string{root},
			Enabled: true,
		},
	)
	if err != nil {
		t.Fatalf("PromptSpecWithSkills: %v", err)
	}
	if len(spec.Skills) != 1 {
		t.Fatalf("expected 1 loaded skill, got %d", len(spec.Skills))
	}
	if len(loaded) != 1 || loaded[0] != "beta" {
		t.Fatalf("unexpected loaded skills: %#v", loaded)
	}
}

func TestPromptSpecWithSkills_DisabledIgnoresTaskReferencedSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha")

	spec, loaded, err := PromptSpecWithSkills(
		context.Background(),
		nil,
		agent.DefaultLogOptions(),
		"use $alpha for this task",
		nil,
		"gpt-5.2",
		SkillsConfig{
			Roots:   []string{root},
			Enabled: false,
		},
	)
	if err != nil {
		t.Fatalf("PromptSpecWithSkills: %v", err)
	}
	if len(spec.Skills) != 0 {
		t.Fatalf("expected 0 loaded skills, got %d", len(spec.Skills))
	}
	if len(loaded) != 0 {
		t.Fatalf("unexpected loaded skills: %#v", loaded)
	}
}

func TestResolveTaskSkillRefs(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha")

	disabled := ResolveTaskSkillRefs("use $alpha", SkillsConfig{Roots: []string{root}, Enabled: false})
	if len(disabled) != 0 {
		t.Fatalf("disabled refs = %#v, want none", disabled)
	}

	enabled := ResolveTaskSkillRefs("use $alpha and $missing", SkillsConfig{Roots: []string{root}, Enabled: true})
	if len(enabled) != 1 || !enabled["alpha"] {
		t.Fatalf("enabled refs = %#v, want alpha", enabled)
	}
}

func TestPromptSpecWithSkills_MergesRequestedAndTaskReferencedSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha")
	writeSkill(t, root, "beta")

	spec, loaded, err := PromptSpecWithSkills(
		context.Background(),
		nil,
		agent.DefaultLogOptions(),
		"also enable $beta",
		nil,
		"gpt-5.2",
		SkillsConfig{
			Roots:     []string{root},
			Enabled:   true,
			Requested: []string{"alpha"},
		},
	)
	if err != nil {
		t.Fatalf("PromptSpecWithSkills: %v", err)
	}
	if len(spec.Skills) != 2 {
		t.Fatalf("expected 2 loaded skills, got %d", len(spec.Skills))
	}
	sort.Strings(loaded)
	if len(loaded) != 2 || loaded[0] != "alpha" || loaded[1] != "beta" {
		t.Fatalf("unexpected loaded skills: %#v", loaded)
	}
}

func TestPromptSpecWithSkills_UnknownTaskReferencesDoNotDisableLoadAll(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha")
	writeSkill(t, root, "beta")

	spec, loaded, err := PromptSpecWithSkills(
		context.Background(),
		nil,
		agent.DefaultLogOptions(),
		"budget is $100 and env $OPENAI_API_KEY",
		nil,
		"gpt-5.2",
		SkillsConfig{
			Roots:   []string{root},
			Enabled: true,
		},
	)
	if err != nil {
		t.Fatalf("PromptSpecWithSkills: %v", err)
	}
	if len(spec.Skills) != 2 {
		t.Fatalf("expected 2 loaded skills, got %d", len(spec.Skills))
	}
	sort.Strings(loaded)
	if len(loaded) != 2 || loaded[0] != "alpha" || loaded[1] != "beta" {
		t.Fatalf("unexpected loaded skills: %#v", loaded)
	}
}

func TestPromptSpecWithSkills_IgnoresUnknownRequests(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha")
	writeSkill(t, root, "beta")

	spec, loaded, err := PromptSpecWithSkills(
		context.Background(),
		nil,
		agent.DefaultLogOptions(),
		"task",
		nil,
		"gpt-5.2",
		SkillsConfig{
			Roots:     []string{root},
			Enabled:   true,
			Requested: []string{"alpha", "missing-skill"},
		},
	)
	if err != nil {
		t.Fatalf("PromptSpecWithSkills should ignore unknown skills: %v", err)
	}
	if len(spec.Skills) != 1 {
		t.Fatalf("expected 1 loaded skill, got %d", len(spec.Skills))
	}
	if len(loaded) != 1 || loaded[0] != "alpha" {
		t.Fatalf("unexpected loaded skills: %#v", loaded)
	}
}

func TestPromptSpecWithSkills_AllUnknownRequestsLoadNone(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha")
	writeSkill(t, root, "beta")

	spec, loaded, err := PromptSpecWithSkills(
		context.Background(),
		nil,
		agent.DefaultLogOptions(),
		"task",
		nil,
		"gpt-5.2",
		SkillsConfig{
			Roots:     []string{root},
			Enabled:   true,
			Requested: []string{"missing-one", "missing-two"},
		},
	)
	if err != nil {
		t.Fatalf("PromptSpecWithSkills should ignore unknown skills: %v", err)
	}
	if len(spec.Skills) != 0 {
		t.Fatalf("expected 0 loaded skills, got %d", len(spec.Skills))
	}
	if len(loaded) != 0 {
		t.Fatalf("unexpected loaded skills: %#v", loaded)
	}
}

func TestPromptSpecWithSkills_InjectsSkillMetadataOnly(t *testing.T) {
	root := t.TempDir()
	writeSkillWithFrontmatter(t, root, "jsonbill", `---
name: jsonbill
description: Generate invoice PDF.
auth_profiles: ["jsonbill"]
requirements:
  - http_client
  - optional: file_send (chat)
---

# JSONBill

very long instructions that should not be injected
`)

	spec, loaded, err := PromptSpecWithSkills(
		context.Background(),
		nil,
		agent.DefaultLogOptions(),
		"task",
		nil,
		"gpt-5.2",
		SkillsConfig{
			Roots:     []string{root},
			DirName:   "skills",
			Enabled:   true,
			Requested: []string{"jsonbill"},
		},
	)
	if err != nil {
		t.Fatalf("PromptSpecWithSkills: %v", err)
	}
	if len(loaded) != 1 || loaded[0] != "jsonbill" {
		t.Fatalf("unexpected loaded skills: %#v", loaded)
	}
	if len(spec.Skills) < 1 {
		t.Fatalf("expected at least 1 skill, got %d", len(spec.Skills))
	}
	if len(spec.Skills) != 1 {
		t.Fatalf("expected only 1 skill metadata, got %d", len(spec.Skills))
	}
	sk := spec.Skills[0]
	if sk.Name != "jsonbill" {
		t.Fatalf("unexpected skill name: %q", sk.Name)
	}
	if sk.Description != "Generate invoice PDF." {
		t.Fatalf("unexpected skill description: %q", sk.Description)
	}
	if sk.FilePath != "file_state_dir/skills/jsonbill/SKILL.md" {
		t.Fatalf("unexpected skill file path: %q", sk.FilePath)
	}
	if len(sk.AuthProfiles) != 1 || sk.AuthProfiles[0] != "jsonbill" {
		t.Fatalf("unexpected skill auth profiles: %#v", sk.AuthProfiles)
	}
}

func TestPromptSpecWithSkills_InjectsSkillFilePathWithConfiguredSkillsDir(t *testing.T) {
	root := t.TempDir()
	writeSkillWithFrontmatter(t, root, "alpha", `---
name: alpha
description: d
---
`)

	spec, _, err := PromptSpecWithSkills(
		context.Background(),
		nil,
		agent.DefaultLogOptions(),
		"task",
		nil,
		"gpt-5.2",
		SkillsConfig{
			Roots:     []string{root},
			DirName:   "my_skills",
			Enabled:   true,
			Requested: []string{"alpha"},
		},
	)
	if err != nil {
		t.Fatalf("PromptSpecWithSkills: %v", err)
	}
	if len(spec.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(spec.Skills))
	}
	if spec.Skills[0].FilePath != "file_state_dir/my_skills/alpha/SKILL.md" {
		t.Fatalf("unexpected skill file path: %q", spec.Skills[0].FilePath)
	}
}

func writeSkill(t *testing.T, root, id string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+id+"\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func writeSkillWithFrontmatter(t *testing.T, root, id, content string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}
