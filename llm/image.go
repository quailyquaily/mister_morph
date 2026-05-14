package llm

import (
	"context"
	"time"
)

type ImageProviderOptions struct {
	OpenAI     map[string]any
	Gemini     map[string]any
	Cloudflare map[string]any
}

type ImageRequest struct {
	Provider string
	Model    string
	Prompt   string
	Options  ImageProviderOptions
}

type ImageEditRequest struct {
	Provider string
	Model    string
	Prompt   string
	Image    ImageInput
	Options  ImageProviderOptions
}

type ImageInput struct {
	Filename string
	MIMEType string
	Data     []byte
}

type ImageAsset struct {
	Data          []byte
	MIMEType      string
	RevisedPrompt string
}

type ImageResult struct {
	Image    ImageAsset
	Usage    Usage
	Duration time.Duration
}

type ImageClient interface {
	GenerateImage(ctx context.Context, req ImageRequest) (ImageResult, error)
	EditImage(ctx context.Context, req ImageEditRequest) (ImageResult, error)
}
