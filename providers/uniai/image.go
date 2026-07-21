package uniai

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/lyricat/goutils/structs"
	"github.com/quailyquaily/mistermorph/llm"
	uniaiapi "github.com/quailyquaily/uniai"
)

func (c *Client) GenerateImage(ctx context.Context, req llm.ImageRequest) (llm.ImageResult, error) {
	start := time.Now()
	if c.requestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.requestTimeout)
		defer cancel()
	}
	opts := buildImageOptions(req, c.provider, c.model)
	resp, err := c.client.Image(ctx, opts...)
	if err != nil {
		return llm.ImageResult{}, err
	}
	out, err := toLLMImageResult(resp)
	if err != nil {
		return llm.ImageResult{}, err
	}
	out.Duration = time.Since(start)
	return out, nil
}

func (c *Client) EditImage(ctx context.Context, req llm.ImageEditRequest) (llm.ImageResult, error) {
	start := time.Now()
	if c.requestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.requestTimeout)
		defer cancel()
	}
	opts := buildImageEditOptions(req, c.provider, c.model)
	resp, err := c.client.EditImage(ctx, opts...)
	if err != nil {
		return llm.ImageResult{}, err
	}
	out, err := toLLMImageResult(resp)
	if err != nil {
		return llm.ImageResult{}, err
	}
	out.Duration = time.Since(start)
	return out, nil
}

func buildImageOptions(req llm.ImageRequest, defaultProvider, defaultModel string) []uniaiapi.ImageOption {
	model := firstNonEmpty(req.Model, defaultModel)
	opts := []uniaiapi.ImageOption{
		uniaiapi.Image(model, strings.TrimSpace(req.Prompt)),
		uniaiapi.WithCount(1),
	}
	if provider := firstNonEmpty(req.Provider, defaultProvider); provider != "" {
		opts = append(opts, uniaiapi.WithImageProvider(provider))
	}
	if imageOptions := toUniaiImageOptions(req.Options); hasImageOptions(imageOptions) {
		opts = append(opts, uniaiapi.WithImageOptions(imageOptions))
	}
	return opts
}

func buildImageEditOptions(req llm.ImageEditRequest, defaultProvider, defaultModel string) []uniaiapi.ImageEditOption {
	model := firstNonEmpty(req.Model, defaultModel)
	input := uniaiapi.InputImage{
		Filename: strings.TrimSpace(req.Image.Filename),
		MIMEType: strings.TrimSpace(req.Image.MIMEType),
		Data:     append([]byte(nil), req.Image.Data...),
	}
	opts := []uniaiapi.ImageEditOption{
		uniaiapi.ImageEdit(model, strings.TrimSpace(req.Prompt), input),
		uniaiapi.WithImageEditCount(1),
	}
	if provider := firstNonEmpty(req.Provider, defaultProvider); provider != "" {
		opts = append(opts, uniaiapi.WithImageEditProvider(provider))
	}
	if imageOptions := toUniaiImageOptions(req.Options); hasImageOptions(imageOptions) {
		opts = append(opts, uniaiapi.WithImageEditOptions(imageOptions))
	}
	return opts
}

func toUniaiImageOptions(opts llm.ImageProviderOptions) uniaiapi.ImageOptions {
	return uniaiapi.ImageOptions{
		OpenAI:     cloneJSONMap(opts.OpenAI),
		Gemini:     cloneJSONMap(opts.Gemini),
		Cloudflare: cloneJSONMap(opts.Cloudflare),
	}
}

func hasImageOptions(opts uniaiapi.ImageOptions) bool {
	return len(opts.OpenAI) > 0 || len(opts.Gemini) > 0 || len(opts.Cloudflare) > 0
}

func cloneJSONMap(in map[string]any) structs.JSONMap {
	if len(in) == 0 {
		return nil
	}
	out := make(structs.JSONMap, len(in))
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

func toLLMImageResult(resp *uniaiapi.ImageResult) (llm.ImageResult, error) {
	if resp == nil {
		return llm.ImageResult{}, fmt.Errorf("uniai: empty image response")
	}
	if len(resp.Images) == 0 {
		return llm.ImageResult{}, fmt.Errorf("uniai: image response has no image")
	}
	image := resp.Images[0]
	dataBase64 := strings.TrimSpace(image.DataBase64)
	if dataBase64 == "" {
		return llm.ImageResult{}, fmt.Errorf("uniai: image response returned no inline image data")
	}
	mimeType := firstNonEmpty(image.MIMEType, imageMIMETypeFromDataURL(dataBase64))
	raw, err := base64.StdEncoding.DecodeString(stripImageDataURLPrefix(dataBase64))
	if err != nil {
		return llm.ImageResult{}, fmt.Errorf("decode image response: %w", err)
	}
	return llm.ImageResult{
		Image: llm.ImageAsset{
			Data:          raw,
			MIMEType:      strings.TrimSpace(mimeType),
			RevisedPrompt: strings.TrimSpace(image.RevisedPrompt),
		},
		Usage: toLLMImageUsage(resp.Usage),
	}, nil
}

func imageMIMETypeFromDataURL(data string) string {
	data = strings.TrimSpace(data)
	if !strings.HasPrefix(strings.ToLower(data), "data:") {
		return ""
	}
	comma := strings.Index(data, ",")
	if comma < 0 {
		return ""
	}
	mediaType := strings.TrimSpace(data[len("data:"):comma])
	if semi := strings.Index(mediaType, ";"); semi >= 0 {
		mediaType = strings.TrimSpace(mediaType[:semi])
	}
	return mediaType
}

func stripImageDataURLPrefix(data string) string {
	if idx := strings.Index(data, ","); idx >= 0 && strings.HasPrefix(strings.ToLower(data[:idx]), "data:") {
		return strings.TrimSpace(data[idx+1:])
	}
	return data
}

func toLLMImageUsage(usage uniaiapi.ImageUsage) llm.Usage {
	cacheDetails := map[string]int{}
	if usage.InputTextTokens > 0 {
		cacheDetails["input_text_tokens"] = usage.InputTextTokens
	}
	if usage.InputImageTokens > 0 {
		cacheDetails["input_image_tokens"] = usage.InputImageTokens
	}
	if usage.CachedTextTokens > 0 {
		cacheDetails["cached_text_tokens"] = usage.CachedTextTokens
	}
	if usage.CachedImageTokens > 0 {
		cacheDetails["cached_image_tokens"] = usage.CachedImageTokens
	}
	return llm.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
		Cache: llm.UsageCache{
			CachedInputTokens: usage.CachedTextTokens + usage.CachedImageTokens,
			Details:           cacheDetails,
		},
		Cost: toLLMUsageCost(usage.Cost),
	}
}
