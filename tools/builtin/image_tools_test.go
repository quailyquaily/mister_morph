package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/imagesession"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/llm"
)

type fakeImageClient struct {
	generateReq llm.ImageRequest
	editReq     llm.ImageEditRequest
	result      llm.ImageResult
}

func (c *fakeImageClient) GenerateImage(_ context.Context, req llm.ImageRequest) (llm.ImageResult, error) {
	c.generateReq = req
	return c.result, nil
}

func (c *fakeImageClient) EditImage(_ context.Context, req llm.ImageEditRequest) (llm.ImageResult, error) {
	c.editReq = req
	return c.result, nil
}

func TestImageGenerateToolWritesDefaultOutput(t *testing.T) {
	cacheDir := t.TempDir()
	client := &fakeImageClient{
		result: llm.ImageResult{Image: llm.ImageAsset{Data: []byte("png"), MIMEType: "image/png"}},
	}
	tool := NewImageGenerateTool(ImageToolConfig{
		Enabled: true,
		Client:  client,
		Model:   "gpt-image-2",
		Roots:   pathroots.New("", cacheDir, ""),
	})

	out, err := tool.Execute(context.Background(), map[string]any{"prompt": "生成图片"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var parsed struct {
		Image struct {
			Path string `json:"path"`
		} `json:"image"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("tool output is not JSON: %v", err)
	}
	if !strings.HasPrefix(parsed.Image.Path, "file_cache_dir/images/") || !strings.HasSuffix(parsed.Image.Path, ".png") {
		t.Fatalf("output path = %q", parsed.Image.Path)
	}
	rel := strings.TrimPrefix(parsed.Image.Path, "file_cache_dir/")
	data, err := os.ReadFile(filepath.Join(cacheDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read output image: %v", err)
	}
	if string(data) != "png" {
		t.Fatalf("output image = %q, want png", string(data))
	}
	if client.generateReq.Model != "gpt-image-2" {
		t.Fatalf("model = %q, want gpt-image-2", client.generateReq.Model)
	}
}

func TestImageGenerateToolNormalizesConflictingOutputExtension(t *testing.T) {
	cacheDir := t.TempDir()
	client := &fakeImageClient{
		result: llm.ImageResult{Image: llm.ImageAsset{Data: []byte("png"), MIMEType: "image/png"}},
	}
	tool := NewImageGenerateTool(ImageToolConfig{
		Enabled: true,
		Client:  client,
		Model:   "gpt-image-2",
		Roots:   pathroots.New("", cacheDir, ""),
	})

	out, err := tool.Execute(context.Background(), map[string]any{
		"prompt":      "生成图片",
		"output_path": "file_cache_dir/out.webp",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var parsed struct {
		Image struct {
			Path string `json:"path"`
		} `json:"image"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("tool output is not JSON: %v", err)
	}
	if parsed.Image.Path != "file_cache_dir/out.png" {
		t.Fatalf("output path = %q, want file_cache_dir/out.png", parsed.Image.Path)
	}
	data, err := os.ReadFile(filepath.Join(cacheDir, "out.png"))
	if err != nil {
		t.Fatalf("read output image: %v", err)
	}
	if string(data) != "png" {
		t.Fatalf("output image = %q, want png", string(data))
	}
}

func TestImageGenerateToolRejectsWebPOutputMIME(t *testing.T) {
	cacheDir := t.TempDir()
	client := &fakeImageClient{
		result: llm.ImageResult{Image: llm.ImageAsset{Data: []byte("webp"), MIMEType: "image/webp"}},
	}
	tool := NewImageGenerateTool(ImageToolConfig{
		Enabled: true,
		Client:  client,
		Model:   "gpt-image-2",
		Roots:   pathroots.New("", cacheDir, ""),
	})

	_, err := tool.Execute(context.Background(), map[string]any{"prompt": "生成图片"})
	if err == nil || !strings.Contains(err.Error(), "MIME type is not supported") {
		t.Fatalf("expected unsupported MIME error, got %v", err)
	}
}

func TestImageGenerateToolValidationErrors(t *testing.T) {
	cacheDir := t.TempDir()
	tool := NewImageGenerateTool(ImageToolConfig{
		Enabled: true,
		Client:  &fakeImageClient{},
		Roots:   pathroots.New("", cacheDir, ""),
	})

	if _, err := tool.Execute(context.Background(), map[string]any{"prompt": "生成图片"}); err == nil || !strings.Contains(err.Error(), "llm.image.model") {
		t.Fatalf("expected missing model error, got %v", err)
	}

	tool.cfg.Model = "gpt-image-2"
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("expected missing prompt error, got %v", err)
	}
	if !strings.Contains(tool.ParameterSchema(), "output_path") {
		t.Fatalf("schema should include output_path")
	}
}

func TestImageEditToolRejectsWebPInputImage(t *testing.T) {
	cacheDir := t.TempDir()
	inputPath := filepath.Join(cacheDir, "input.webp")
	if err := os.WriteFile(inputPath, []byte("input"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &fakeImageClient{
		result: llm.ImageResult{Image: llm.ImageAsset{Data: []byte("edited"), MIMEType: "image/png"}},
	}
	tool := NewImageEditTool(ImageToolConfig{
		Enabled: true,
		Client:  client,
		Model:   "gpt-image-2",
		Roots:   pathroots.New("", cacheDir, ""),
	})

	_, err := tool.Execute(context.Background(), map[string]any{
		"prompt":     "改图",
		"input_path": "file_cache_dir/input.webp",
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported input error, got %v", err)
	}
}

func TestImageEditToolReadsInputImage(t *testing.T) {
	cacheDir := t.TempDir()
	inputPath := filepath.Join(cacheDir, "input.png")
	if err := os.WriteFile(inputPath, []byte("input"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &fakeImageClient{
		result: llm.ImageResult{Image: llm.ImageAsset{Data: []byte("edited"), MIMEType: "image/png"}},
	}
	tool := NewImageEditTool(ImageToolConfig{
		Enabled: true,
		Client:  client,
		Model:   "gpt-image-2",
		Roots:   pathroots.New("", cacheDir, ""),
	})

	_, err := tool.Execute(context.Background(), map[string]any{
		"prompt":     "改图",
		"input_path": "file_cache_dir/input.png",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if client.editReq.Image.Filename != "input.png" {
		t.Fatalf("input filename = %q, want input.png", client.editReq.Image.Filename)
	}
	if client.editReq.Image.MIMEType != "image/png" {
		t.Fatalf("input MIME = %q, want image/png", client.editReq.Image.MIMEType)
	}
	if string(client.editReq.Image.Data) != "input" {
		t.Fatalf("input data = %q, want input", string(client.editReq.Image.Data))
	}
}

func TestImageEditToolCanUseActiveSessionImage(t *testing.T) {
	cacheDir := t.TempDir()
	stateDir := t.TempDir()
	roots := pathroots.New("", cacheDir, stateDir)
	store := imagesession.NewStore(stateDir)
	scope := imagesession.NewScope("chat:test")
	client := &fakeImageClient{
		result: llm.ImageResult{Image: llm.ImageAsset{Data: []byte("generated"), MIMEType: "image/png"}},
	}
	cfg := ImageToolConfig{
		Enabled: true,
		Client:  client,
		Model:   "gpt-image-2",
		Roots:   roots,
		Session: store,
		Scope:   scope,
	}
	generateTool := NewImageGenerateTool(cfg)
	generateOut, err := generateTool.Execute(context.Background(), map[string]any{"prompt": "生成图片"})
	if err != nil {
		t.Fatalf("generate Execute() error = %v", err)
	}
	var generated imageToolResult
	if err := json.Unmarshal([]byte(generateOut), &generated); err != nil {
		t.Fatalf("generate output is not JSON: %v", err)
	}
	if strings.TrimSpace(generated.ActiveImageID) == "" {
		t.Fatalf("generate active image id is empty")
	}

	client.result = llm.ImageResult{Image: llm.ImageAsset{Data: []byte("edited"), MIMEType: "image/png"}}
	editTool := NewImageEditTool(cfg)
	editOut, err := editTool.Execute(context.Background(), map[string]any{
		"prompt":           "再亮一点",
		"use_active_image": true,
	})
	if err != nil {
		t.Fatalf("edit Execute() error = %v", err)
	}
	if string(client.editReq.Image.Data) != "generated" {
		t.Fatalf("active image input = %q, want generated", string(client.editReq.Image.Data))
	}
	var edited imageToolResult
	if err := json.Unmarshal([]byte(editOut), &edited); err != nil {
		t.Fatalf("edit output is not JSON: %v", err)
	}
	if strings.TrimSpace(edited.ActiveImageID) == "" || edited.ActiveImageID == generated.ActiveImageID {
		t.Fatalf("edit active image id = %q, generated id = %q", edited.ActiveImageID, generated.ActiveImageID)
	}
	active, err := store.Active(scope, roots)
	if err != nil {
		t.Fatalf("active image: %v", err)
	}
	if active == nil || active.ID != edited.ActiveImageID {
		t.Fatalf("active image = %#v, want id %q", active, edited.ActiveImageID)
	}
	if len(active.ParentIDs) != 1 || active.ParentIDs[0] != generated.ActiveImageID {
		t.Fatalf("parent ids = %#v, want %q", active.ParentIDs, generated.ActiveImageID)
	}
}
