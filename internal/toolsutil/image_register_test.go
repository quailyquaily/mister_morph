package toolsutil

import (
	"context"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

type fakeRegisterImageClient struct{}

func (fakeRegisterImageClient) GenerateImage(context.Context, llm.ImageRequest) (llm.ImageResult, error) {
	return llm.ImageResult{}, nil
}

func (fakeRegisterImageClient) EditImage(context.Context, llm.ImageEditRequest) (llm.ImageResult, error) {
	return llm.ImageResult{}, nil
}

type recordingRegisterImageClient struct {
	generateReq llm.ImageRequest
}

func (c *recordingRegisterImageClient) GenerateImage(_ context.Context, req llm.ImageRequest) (llm.ImageResult, error) {
	c.generateReq = req
	return llm.ImageResult{Image: llm.ImageAsset{Data: []byte("png"), MIMEType: "image/png"}}, nil
}

func (c *recordingRegisterImageClient) EditImage(context.Context, llm.ImageEditRequest) (llm.ImageResult, error) {
	return llm.ImageResult{}, nil
}

func TestImageToolIntentMatchesLanguages(t *testing.T) {
	tests := []struct {
		name   string
		task   string
		active bool
		want   bool
	}{
		{name: "chinese generate", task: "请帮我生成图片", want: true},
		{name: "japanese edit", task: "この画像を明るくして", want: true},
		{name: "english generate phrase", task: "Create an illustration of a quiet desk", want: true},
		{name: "english generate with article", task: "generate an image of a cat", want: true},
		{name: "english generate picture", task: "generate a picture of a cat", want: true},
		{name: "english no bare draw", task: "draw conclusions from this log", want: false},
		{name: "follow up requires active state", task: "brighter", active: false, want: false},
		{name: "follow up with active state", task: "brighter", active: true, want: true},
		{name: "follow up with current image path note", task: "brighter\n\nLocal image files available to image_edit:\n- attached image 1: file_cache_dir/input.png", active: false, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ImageToolIntentMatches(tc.task, tc.active); got != tc.want {
				t.Fatalf("ImageToolIntentMatches() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestImageToolRetentionSticky(t *testing.T) {
	var r ImageToolRetention
	if got := r.ResolveWithActive(ImageToolRetentionSticky, "请生成图片", false); !got {
		t.Fatalf("first image task should retain tools")
	}
	if got := r.ResolveWithActive(ImageToolRetentionSticky, "继续", false); !got {
		t.Fatalf("sticky retention should keep tools enabled")
	}
}

func TestImageToolRetentionCountdownRefreshes(t *testing.T) {
	var r ImageToolRetention
	if got := r.ResolveWithActive(ImageToolRetentionCountdown, "生成图片", false); !got {
		t.Fatalf("first image task should retain tools")
	}
	for i := 0; i < imageToolRetentionTurns; i++ {
		if got := r.ResolveWithActive(ImageToolRetentionCountdown, "普通消息", false); !got {
			t.Fatalf("turn %d should still retain tools", i+1)
		}
	}
	if got := r.ResolveWithActive(ImageToolRetentionCountdown, "普通消息", false); got {
		t.Fatalf("retention should expire after %d turns", imageToolRetentionTurns)
	}

	if got := r.ResolveWithActive(ImageToolRetentionCountdown, "生成图片", false); !got {
		t.Fatalf("image task should refresh retention")
	}
	if r.TurnsLeft != imageToolRetentionTurns {
		t.Fatalf("turns left = %d, want %d", r.TurnsLeft, imageToolRetentionTurns)
	}
}

func TestImageToolRetentionStoreDoesNotCreateItemsForOrdinaryScopes(t *testing.T) {
	store := NewImageToolRetentionStore()
	if got := store.ResolveWithActive("tg:ordinary", ImageToolRetentionCountdown, "普通消息", false); got {
		t.Fatalf("ordinary message should not retain image tools")
	}
	if len(store.items) != 0 {
		t.Fatalf("retention items = %d, want 0", len(store.items))
	}

	if got := store.ResolveWithActive("tg:ordinary", ImageToolRetentionCountdown, "生成图片", false); !got {
		t.Fatalf("image task should retain image tools")
	}
	for i := 0; i < imageToolRetentionTurns; i++ {
		_ = store.ResolveWithActive("tg:ordinary", ImageToolRetentionCountdown, "普通消息", false)
	}
	if len(store.items) != 0 {
		t.Fatalf("expired retention item should be deleted")
	}
}

func TestRegisterImageToolsRequiresIntentOrRetention(t *testing.T) {
	cfg := ImageToolsRegisterConfig{
		GenerateEnabled: true,
		EditEnabled:     true,
		FileCacheDir:    t.TempDir(),
		Configured:      true,
		Provider:        "openai",
		Model:           "gpt-image-2",
	}
	client := fakeRegisterImageClient{}

	reg := tools.NewRegistry()
	RegisterImageTools(reg, cfg, client, "summarize this", false)
	if _, ok := reg.Get("image_generate"); ok {
		t.Fatalf("image_generate registered without intent")
	}

	disabledCfg := cfg
	disabledCfg.Configured = false
	RegisterImageTools(reg, disabledCfg, client, "生成图片", true)
	if _, ok := reg.Get("image_generate"); ok {
		t.Fatalf("image_generate registered without usable image config")
	}

	RegisterImageTools(reg, cfg, nil, "生成图片", true)
	if _, ok := reg.Get("image_generate"); ok {
		t.Fatalf("image_generate registered without image client")
	}

	RegisterImageTools(reg, cfg, client, "summarize this", true)
	if _, ok := reg.Get("image_generate"); !ok {
		t.Fatalf("image_generate not registered under retention")
	}
	if _, ok := reg.Get("image_edit"); !ok {
		t.Fatalf("image_edit not registered under retention")
	}
}

func TestRegisterRuntimeToolsImageModelInheritsDefaultModel(t *testing.T) {
	cacheDir := t.TempDir()
	client := &recordingRegisterImageClient{}
	reg := tools.NewRegistry()
	RegisterRuntimeTools(reg, RuntimeToolsRegisterConfig{
		Image: ImageToolsRegisterConfig{
			GenerateEnabled: true,
			FileCacheDir:    cacheDir,
			Configured:      true,
			Provider:        "openai",
		},
	}, RuntimeToolLLMOptions{
		DefaultModel:  "gpt-5.5",
		ImageClient:   client,
		Task:          "生成图片",
		ImageRetained: true,
	})
	tool, ok := reg.Get("image_generate")
	if !ok {
		t.Fatalf("image_generate not registered")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"prompt": "生成图片"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if client.generateReq.Model != "gpt-5.5" {
		t.Fatalf("image model = %q, want gpt-5.5", client.generateReq.Model)
	}
}

func TestLoadImageToolsRegisterConfigFromReader(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(`
file_cache_dir: /cache
file_state_dir: /state
llm:
  provider: openai
  api_key: image-key
  image:
    model: gpt-image-2
    options:
      openai:
        quality: high
tools:
  image_generate:
    enabled: true
  image_edit:
    enabled: true
`)); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	cfg := LoadImageToolsRegisterConfigFromReader(v)
	if !cfg.GenerateEnabled || !cfg.EditEnabled {
		t.Fatalf("image tools not enabled: %#v", cfg)
	}
	if cfg.FileCacheDir != "/cache" || cfg.FileStateDir != "/state" {
		t.Fatalf("paths = %q/%q", cfg.FileCacheDir, cfg.FileStateDir)
	}
	if cfg.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", cfg.Provider)
	}
	if cfg.Model != "gpt-image-2" {
		t.Fatalf("model = %q, want gpt-image-2", cfg.Model)
	}
	if !cfg.Configured {
		t.Fatalf("Configured = false, want true")
	}
	if got := cfg.Options.OpenAI["quality"]; got != "high" {
		t.Fatalf("openai quality = %#v, want high", got)
	}
}

func TestLoadImageToolsRegisterConfigInheritsLLMModel(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(`
file_cache_dir: /cache
llm:
  provider: openai
  api_key: openai-key
  model: gpt-5.5
tools:
  image_generate:
    enabled: true
`)); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	cfg := LoadImageToolsRegisterConfigFromReader(v)
	if cfg.Model != "gpt-5.5" {
		t.Fatalf("model = %q, want gpt-5.5", cfg.Model)
	}
	if !cfg.Configured {
		t.Fatalf("Configured = false, want true")
	}
}

func TestLoadImageToolsRegisterConfigAllowsRuntimeModelFallback(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(`
file_cache_dir: /cache
llm:
  provider: openai
  api_key: openai-key
tools:
  image_generate:
    enabled: true
`)); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	cfg := LoadImageToolsRegisterConfigFromReader(v)
	if !cfg.Configured {
		t.Fatalf("Configured = false, want true")
	}
	if cfg.Model != "" {
		t.Fatalf("model = %q, want empty before runtime fallback", cfg.Model)
	}
}

func TestApplyImageToolLLMConfigUsesEffectiveOverrides(t *testing.T) {
	cfg := ApplyImageToolLLMConfig(ImageToolsRegisterConfig{
		GenerateEnabled: true,
		FileCacheDir:    t.TempDir(),
	}, ImageToolLLMConfig{
		Provider: "openai",
		APIKey:   "image-key",
		Model:    "gpt-image-2",
	})
	if !cfg.Configured {
		t.Fatalf("Configured = false, want true")
	}
	if cfg.Provider != "openai" || cfg.Model != "gpt-image-2" {
		t.Fatalf("image config = %#v", cfg)
	}
}

func TestLoadImageToolsRegisterConfigDoesNotInheritCodexAuth(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(`
file_cache_dir: /cache
llm:
  provider: openai_codex
  api_key: ignored-for-images
  model: gpt-5.5
  image:
    endpoint: https://api.openai.com/v1
tools:
  image_generate:
    enabled: true
`)); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	cfg := LoadImageToolsRegisterConfigFromReader(v)
	if cfg.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", cfg.Provider)
	}
	if cfg.Configured {
		t.Fatalf("Configured = true, want false")
	}
}

func TestLoadImageToolsRegisterConfigExplicitImageKeyWithCodexChatProvider(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(`
file_cache_dir: /cache
llm:
  provider: openai_codex
  model: gpt-5.5
  image:
    api_key: image-key
tools:
  image_generate:
    enabled: true
`)); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	cfg := LoadImageToolsRegisterConfigFromReader(v)
	if cfg.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", cfg.Provider)
	}
	if !cfg.Configured {
		t.Fatalf("Configured = false, want true")
	}
}

func TestLoadImageToolsRegisterConfigDoesNotInheritCodexKeyThroughExplicitOpenAIProvider(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(`
file_cache_dir: /cache
llm:
  provider: openai_codex
  api_key: codex-key
  model: gpt-5.5
  image:
    provider: openai
tools:
  image_generate:
    enabled: true
`)); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	cfg := LoadImageToolsRegisterConfigFromReader(v)
	if cfg.Configured {
		t.Fatalf("Configured = true, want false")
	}
}

func TestLoadImageToolsRegisterConfigRejectsMismatchedInheritedAPIKey(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(`
file_cache_dir: /cache
llm:
  provider: openai
  api_key: openai-key
  model: gpt-5.5
  image:
    provider: gemini
tools:
  image_generate:
    enabled: true
`)); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	cfg := LoadImageToolsRegisterConfigFromReader(v)
	if cfg.Provider != "gemini" {
		t.Fatalf("provider = %q, want gemini", cfg.Provider)
	}
	if cfg.Configured {
		t.Fatalf("Configured = true, want false")
	}
}
