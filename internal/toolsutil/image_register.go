package toolsutil

import (
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/quailyquaily/mistermorph/internal/imagesession"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
)

const imageToolRetentionTurns = 16

type ImageToolsRegisterConfig struct {
	GenerateEnabled bool
	EditEnabled     bool
	FileCacheDir    string
	FileStateDir    string
	Configured      bool
	Provider        string
	Model           string
	Options         llm.ImageProviderOptions
	SessionStore    *imagesession.Store
	SessionScope    imagesession.Scope
}

type ImageToolLLMConfig struct {
	Provider            string
	APIKey              string
	Model               string
	ImageProvider       string
	ImageAPIKey         string
	ImageModel          string
	CloudflareAccountID string
	CloudflareAPIToken  string
}

func LoadImageToolsRegisterConfigFromReader(r runtimeRegisterConfigReader) ImageToolsRegisterConfig {
	if r == nil {
		return ImageToolsRegisterConfig{}
	}
	cfg := ImageToolsRegisterConfig{
		GenerateEnabled: r.GetBool("tools.image_generate.enabled"),
		EditEnabled:     r.GetBool("tools.image_edit.enabled"),
		FileCacheDir:    strings.TrimSpace(r.GetString("file_cache_dir")),
		FileStateDir:    strings.TrimSpace(r.GetString("file_state_dir")),
		Options: llm.ImageProviderOptions{
			OpenAI:     loadImageOptionsMap(r, "llm.image.options.openai"),
			Gemini:     loadImageOptionsMap(r, "llm.image.options.gemini"),
			Cloudflare: loadImageOptionsMap(r, "llm.image.options.cloudflare"),
		},
	}
	return ApplyImageToolLLMConfig(cfg, ImageToolLLMConfig{
		Provider:            r.GetString("llm.provider"),
		APIKey:              r.GetString("llm.api_key"),
		Model:               r.GetString("llm.model"),
		ImageProvider:       r.GetString("llm.image.provider"),
		ImageAPIKey:         r.GetString("llm.image.api_key"),
		ImageModel:          r.GetString("llm.image.model"),
		CloudflareAccountID: r.GetString("llm.cloudflare.account_id"),
		CloudflareAPIToken:  r.GetString("llm.cloudflare.api_token"),
	})
}

type ImageToolRetentionMode string

const (
	ImageToolRetentionNone      ImageToolRetentionMode = ""
	ImageToolRetentionSticky    ImageToolRetentionMode = "sticky"
	ImageToolRetentionCountdown ImageToolRetentionMode = "countdown"
)

type ImageToolRetention struct {
	Enabled     bool
	TurnsLeft   int
	TriggeredAt time.Time
}

type ImageToolRetentionStore struct {
	mu    sync.Mutex
	items map[string]*ImageToolRetention
}

func NewImageToolRetentionStore() *ImageToolRetentionStore {
	return &ImageToolRetentionStore{items: map[string]*ImageToolRetention{}}
}

func (s *ImageToolRetentionStore) Resolve(scope string, mode ImageToolRetentionMode, triggered bool) bool {
	scope = strings.TrimSpace(scope)
	if s == nil || scope == "" || mode == ImageToolRetentionNone {
		return triggered
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = map[string]*ImageToolRetention{}
	}
	item := s.items[scope]
	if item == nil {
		if !triggered {
			return false
		}
		item = &ImageToolRetention{}
		s.items[scope] = item
	}
	retained := item.Resolve(mode, triggered)
	if mode == ImageToolRetentionCountdown && !item.Enabled {
		delete(s.items, scope)
	}
	return retained
}

func (r *ImageToolRetention) Resolve(mode ImageToolRetentionMode, triggered bool) bool {
	if r == nil {
		return triggered
	}
	switch mode {
	case ImageToolRetentionSticky:
		if triggered {
			r.Enabled = true
			r.TriggeredAt = time.Now().UTC()
		}
		return r.Enabled || triggered
	case ImageToolRetentionCountdown:
		if triggered {
			r.Enabled = true
			r.TurnsLeft = imageToolRetentionTurns
			r.TriggeredAt = time.Now().UTC()
			return true
		}
		if r.Enabled && r.TurnsLeft > 0 {
			r.TurnsLeft--
			if r.TurnsLeft <= 0 {
				r.Enabled = false
			}
			return true
		}
		return false
	default:
		return triggered
	}
}

func RegisterImageTools(reg *tools.Registry, cfg ImageToolsRegisterConfig, client llm.ImageClient, triggered bool) {
	if reg == nil {
		return
	}
	if !triggered {
		return
	}
	if !cfg.GenerateEnabled && !cfg.EditEnabled {
		return
	}
	if !cfg.Configured {
		return
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return
	}
	if client == nil {
		return
	}
	if strings.TrimSpace(cfg.FileCacheDir) == "" {
		return
	}
	toolCfg := builtin.ImageToolConfig{
		Enabled:  true,
		Client:   client,
		Provider: strings.TrimSpace(cfg.Provider),
		Model:    strings.TrimSpace(cfg.Model),
		Options:  cloneImageOptions(cfg.Options),
		Roots:    pathroots.New("", strings.TrimSpace(cfg.FileCacheDir), strings.TrimSpace(cfg.FileStateDir)),
		Session:  cfg.SessionStore,
		Scope:    cfg.SessionScope,
	}
	if cfg.GenerateEnabled {
		reg.Register(builtin.NewImageGenerateTool(toolCfg))
	}
	if cfg.EditEnabled {
		reg.Register(builtin.NewImageEditTool(toolCfg))
	}
}

func ImageToolIntentMatches(task string, active bool) bool {
	text := normalizeIntentText(task)
	if text == "" {
		return false
	}
	active = active || containsCurrentImageAttachment(text)
	for _, phrase := range imageGenerationPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	for _, phrase := range imageEditPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	if active {
		for _, phrase := range imageFollowupEditPhrases {
			if strings.Contains(text, phrase) {
				return true
			}
		}
	}
	return false
}

func containsCurrentImageAttachment(text string) bool {
	return strings.Contains(text, "local image files available to image_edit") ||
		strings.Contains(text, "attached image")
}

var imageGenerationPhrases = []string{
	"画图",
	"作图",
	"做图",
	"画个图",
	"画张图",
	"生成图片",
	"生成一张图",
	"画一张",
	"图片生成",
	"生成海报",
	"生成插画",
	"画像生成",
	"画像を生成",
	"画像作成",
	"絵を描",
	"描いて",
	"作画",
	"イラストを作",
	"generate image",
	"generate an image",
	"generate a picture",
	"generate a photo",
	"create image",
	"create an image",
	"create a picture",
	"create a photo",
	"make an image",
	"make a picture",
	"make a photo",
	"draw me",
	"draw a",
	"draw an",
	"draw the",
	"draw image",
	"draw picture",
	"draw illustration",
	"create a poster",
	"create an illustration",
}

var imageEditPhrases = []string{
	"修图",
	"改图",
	"编辑图片",
	"修改图片",
	"重绘",
	"换背景",
	"去背景",
	"调亮",
	"调暗",
	"画像編集",
	"編集して",
	"修正して",
	"描き直",
	"背景を変え",
	"明るくして",
	"暗くして",
	"edit image",
	"modify image",
	"change the image",
	"redraw",
	"change background",
	"remove background",
	"make it brighter",
	"make it darker",
}

var imageFollowupEditPhrases = []string{
	"再亮一点",
	"亮一点",
	"暗一点",
	"换背景",
	"去背景",
	"明るくして",
	"暗くして",
	"背景を変え",
	"brighter",
	"darker",
	"change background",
	"remove background",
}

func normalizeIntentText(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		lastSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func cloneImageOptions(in llm.ImageProviderOptions) llm.ImageProviderOptions {
	return llm.ImageProviderOptions{
		OpenAI:     cloneImageAnyMap(in.OpenAI),
		Gemini:     cloneImageAnyMap(in.Gemini),
		Cloudflare: cloneImageAnyMap(in.Cloudflare),
	}
}

func loadImageOptionsMap(r runtimeRegisterConfigReader, key string) map[string]any {
	reader, ok := any(r).(interface {
		GetStringMap(string) map[string]any
	})
	if !ok {
		return nil
	}
	return cloneImageAnyMap(reader.GetStringMap(key))
}

func cloneImageAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstNonEmptyImageRegister(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

func ResolveImageToolLLMConfig(cfg ImageToolLLMConfig) (provider string, model string, configured bool) {
	provider = normalizeImageToolProvider(firstNonEmptyImageRegister(cfg.ImageProvider, cfg.Provider))
	model = firstNonEmptyImageRegister(cfg.ImageModel, cfg.Model)
	if provider == "" {
		return provider, model, false
	}
	imageSpecific := strings.TrimSpace(cfg.ImageProvider) != "" ||
		strings.TrimSpace(cfg.ImageAPIKey) != ""
	switch provider {
	case "openai", "gemini":
		if imageSpecific {
			return provider, model, imageAPIKeyAvailableForProvider(provider, cfg)
		}
		return provider, model, inheritedImageConfigAvailable(cfg)
	case "cloudflare":
		if !imageSpecific {
			return provider, model, false
		}
		return provider, model, cloudflareImageConfigAvailable(cfg)
	default:
		return provider, model, false
	}
}

func ApplyImageToolLLMConfig(cfg ImageToolsRegisterConfig, llmCfg ImageToolLLMConfig) ImageToolsRegisterConfig {
	provider, model, configured := ResolveImageToolLLMConfig(llmCfg)
	cfg.Provider = provider
	cfg.Model = model
	cfg.Configured = configured
	return cfg
}

func normalizeImageToolProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "openai_codex", "openai_custom", "openai_resp":
		return "openai"
	default:
		return provider
	}
}

func inheritedImageConfigAvailable(cfg ImageToolLLMConfig) bool {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider != "openai" && provider != "gemini" {
		return false
	}
	return strings.TrimSpace(cfg.APIKey) != ""
}

func imageAPIKeyAvailableForProvider(provider string, cfg ImageToolLLMConfig) bool {
	if strings.TrimSpace(cfg.ImageAPIKey) != "" {
		return true
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg.Provider), provider)
}

func cloudflareImageConfigAvailable(cfg ImageToolLLMConfig) bool {
	if strings.TrimSpace(cfg.CloudflareAccountID) == "" {
		return false
	}
	if strings.TrimSpace(cfg.ImageAPIKey) != "" || strings.TrimSpace(cfg.CloudflareAPIToken) != "" {
		return true
	}
	return strings.TrimSpace(cfg.APIKey) != "" && strings.EqualFold(strings.TrimSpace(cfg.Provider), "cloudflare")
}
