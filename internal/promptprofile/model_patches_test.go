package promptprofile

import (
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
)

func TestIsGPT5FamilyModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{model: "gpt-5", want: true},
		{model: "gpt-5.4", want: true},
		{model: "gpt-5-mini", want: true},
		{model: "openai/gpt-5.4", want: true},
		{model: "carrot/gpt-5.4", want: true},
		{model: "vendor/models/gpt-5.4", want: true},
		{model: "gpt-5.5", want: false},
		{model: "openai/gpt-5.5", want: false},
		{model: "gpt-50", want: false},
		{model: "openai/gpt-50", want: false},
		{model: "gpt-4.1", want: false},
		{model: "", want: false},
	}
	for _, tc := range tests {
		if got := isGPT5FamilyModel(tc.model); got != tc.want {
			t.Fatalf("isGPT5FamilyModel(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

func TestIsQwen3FamilyModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{model: "qwen3", want: true},
		{model: "qwen3.5", want: true},
		{model: "qwen3.6", want: true},
		{model: "qwen3-235b-a22b", want: true},
		{model: "qwen3:8b", want: true},
		{model: "qwen-3", want: true},
		{model: "qwen-3.6", want: true},
		{model: "qwen/qwen3.5-coder", want: true},
		{model: "Alibaba/Qwen3.6-27B-MTP-Q4_K_M", want: true},
		{model: "models/qwen3.6", want: true},
		{model: "qwen2.5", want: false},
		{model: "qwen30", want: false},
		{model: "qwen-max", want: false},
		{model: "", want: false},
	}
	for _, tc := range tests {
		if got := isQwen3FamilyModel(tc.model); got != tc.want {
			t.Fatalf("isQwen3FamilyModel(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

func TestAppendGPT5PromptPatch_GPT5Family(t *testing.T) {
	restore := stubGPT5PromptPatchSource(t, "[[ GPT-5 Patch ]]\n- Use the GPT-5 policy.")
	defer restore()

	tests := []string{
		"gpt-5",
		"gpt-5.4",
		"openai/gpt-5.4",
		"carrot/gpt-5.4",
		"models/gpt-5.4",
	}
	for _, model := range tests {
		spec := agent.PromptSpec{}
		AppendGPT5PromptPatch(&spec, model, nil)
		if len(spec.Blocks) != 1 {
			t.Fatalf("model %q blocks len = %d, want 1", model, len(spec.Blocks))
		}
		if got := spec.Blocks[0].Content; got != "[[ GPT-5 Patch ]]\n- Use the GPT-5 policy." {
			t.Fatalf("model %q patch content = %q", model, got)
		}
	}
}

func TestAppendQwen3PromptPatch_Qwen3Family(t *testing.T) {
	restore := stubQwen3PromptPatchSource(t, "[[ Qwen 3 Patch ]]\n- Use necessary tools only.")
	defer restore()

	tests := []string{
		"qwen3.5",
		"qwen3.6",
		"qwen/qwen3.5-coder",
		"Alibaba/Qwen3.6-27B-MTP-Q4_K_M",
	}
	for _, model := range tests {
		spec := agent.PromptSpec{}
		appendQwen3PromptPatch(&spec, model)
		if len(spec.Blocks) != 1 {
			t.Fatalf("model %q blocks len = %d, want 1", model, len(spec.Blocks))
		}
		if got := spec.Blocks[0].Content; got != "[[ Qwen 3 Patch ]]\n- Use necessary tools only." {
			t.Fatalf("model %q patch content = %q", model, got)
		}
	}
}

func TestAppendGPT5PromptPatch_SkipsNonMatchingModel(t *testing.T) {
	restore := stubGPT5PromptPatchSource(t, "patch")
	defer restore()

	tests := []string{"gpt-4.1", "gpt-5.5", "openai/gpt-5.5"}
	for _, model := range tests {
		spec := agent.PromptSpec{}
		AppendGPT5PromptPatch(&spec, model, nil)
		if len(spec.Blocks) != 0 {
			t.Fatalf("model %q blocks len = %d, want 0", model, len(spec.Blocks))
		}
	}
}

func TestAppendQwen3PromptPatch_SkipsNonMatchingModel(t *testing.T) {
	restore := stubQwen3PromptPatchSource(t, "patch")
	defer restore()

	spec := agent.PromptSpec{}
	appendQwen3PromptPatch(&spec, "qwen2.5")
	if len(spec.Blocks) != 0 {
		t.Fatalf("blocks len = %d, want 0", len(spec.Blocks))
	}
}

func TestAppendGPT5PromptPatch_SkipsEmptyPatchContent(t *testing.T) {
	restore := stubGPT5PromptPatchSource(t, "\n")
	defer restore()

	spec := agent.PromptSpec{}
	AppendGPT5PromptPatch(&spec, "gpt-5.4", nil)
	if len(spec.Blocks) != 0 {
		t.Fatalf("blocks len = %d, want 0", len(spec.Blocks))
	}
}

func TestAppendQwen3PromptPatch_SkipsEmptyPatchContent(t *testing.T) {
	restore := stubQwen3PromptPatchSource(t, "\n")
	defer restore()

	spec := agent.PromptSpec{}
	appendQwen3PromptPatch(&spec, "qwen3.6")
	if len(spec.Blocks) != 0 {
		t.Fatalf("blocks len = %d, want 0", len(spec.Blocks))
	}
}

func stubGPT5PromptPatchSource(t *testing.T, content string) func() {
	t.Helper()
	old := gpt5PromptPatchSource
	gpt5PromptPatchSource = content
	return func() {
		gpt5PromptPatchSource = old
	}
}

func stubQwen3PromptPatchSource(t *testing.T, content string) func() {
	t.Helper()
	old := qwen3PromptPatchSource
	qwen3PromptPatchSource = content
	return func() {
		qwen3PromptPatchSource = old
	}
}
