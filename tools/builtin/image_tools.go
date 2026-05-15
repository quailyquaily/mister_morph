package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imageinput"
	"github.com/quailyquaily/mistermorph/internal/imagesession"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/llm"
)

const (
	imageToolMaxPromptBytes = 8 * 1024
	imageToolMaxInputBytes  = int64(20 * 1024 * 1024)
	imageToolMaxOutputBytes = int64(50 * 1024 * 1024)
)

type ImageToolConfig struct {
	Enabled  bool
	Client   llm.ImageClient
	Provider string
	Model    string
	Options  llm.ImageProviderOptions
	Roots    pathroots.PathRoots
	Session  *imagesession.Store
	Scope    imagesession.Scope
}

type ImageGenerateTool struct {
	cfg ImageToolConfig
}

type ImageEditTool struct {
	cfg ImageToolConfig
}

func NewImageGenerateTool(cfg ImageToolConfig) *ImageGenerateTool {
	cfg.Roots = pathroots.New(cfg.Roots.WorkspaceDir, cfg.Roots.FileCacheDir, cfg.Roots.FileStateDir)
	return &ImageGenerateTool{cfg: cfg}
}

func NewImageEditTool(cfg ImageToolConfig) *ImageEditTool {
	cfg.Roots = pathroots.New(cfg.Roots.WorkspaceDir, cfg.Roots.FileCacheDir, cfg.Roots.FileStateDir)
	return &ImageEditTool{cfg: cfg}
}

func (t *ImageGenerateTool) Name() string { return "image_generate" }

func (t *ImageGenerateTool) Description() string {
	return "Generates one image from a prompt and saves it to a local file under file_cache_dir or workspace_dir."
}

func (t *ImageGenerateTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Image generation prompt.",
			},
			"output_path": map[string]any{
				"type":        "string",
				"description": "Optional output path. Supports workspace_dir/... and file_cache_dir/... aliases. Relative paths resolve under file_cache_dir/images/. The final file extension is normalized to the returned PNG/JPEG MIME type.",
			},
		},
		"required": []string{"prompt"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *ImageGenerateTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || !t.cfg.Enabled {
		return "", fmt.Errorf("image_generate tool is disabled")
	}
	if strings.TrimSpace(t.cfg.Model) == "" {
		return "", fmt.Errorf("llm.image.model and llm.model are not configured")
	}
	if t.cfg.Client == nil {
		return "", fmt.Errorf("image client is not configured")
	}
	prompt, err := imagePromptParam(params)
	if err != nil {
		return "", err
	}
	resp, err := t.cfg.Client.GenerateImage(ctx, llm.ImageRequest{
		Provider: strings.TrimSpace(t.cfg.Provider),
		Model:    strings.TrimSpace(t.cfg.Model),
		Prompt:   prompt,
		Options:  cloneImageProviderOptions(t.cfg.Options),
	})
	if err != nil {
		return "", err
	}
	return t.writeResult(ctx, params, resp)
}

func (t *ImageGenerateTool) writeResult(ctx context.Context, params map[string]any, resp llm.ImageResult) (string, error) {
	roots := resolveLocalPathRoots(ctx, t.cfg.Roots)
	outputPath, displayPath, err := resolveImageOutputPath(roots, stringParam(params, "output_path"), resp.Image.MIMEType)
	if err != nil {
		return "", err
	}
	if err := writeImageOutput(outputPath, resp.Image.Data); err != nil {
		return "", err
	}
	activeID, err := recordImageSessionOutput(t.cfg.Session, t.cfg.Scope, roots, displayPath, resp.Image.MIMEType, len(resp.Image.Data), "image_generate", "")
	if err != nil {
		return "", err
	}
	return marshalImageToolResult(imageToolResult{
		Image: imageToolResultImage{
			Path:          displayPath,
			MIMEType:      resp.Image.MIMEType,
			Bytes:         len(resp.Image.Data),
			RevisedPrompt: resp.Image.RevisedPrompt,
		},
		Model:         strings.TrimSpace(t.cfg.Model),
		Provider:      strings.TrimSpace(t.cfg.Provider),
		Usage:         resp.Usage,
		ActiveImageID: activeID,
	}), nil
}

func (t *ImageEditTool) Name() string { return "image_edit" }

func (t *ImageEditTool) Description() string {
	return "Edits one local image from a prompt and saves one output image under file_cache_dir or workspace_dir."
}

func (t *ImageEditTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Image edit prompt.",
			},
			"input_path": map[string]any{
				"type":        "string",
				"description": "Input image path. Supports workspace_dir/... and file_cache_dir/... aliases.",
			},
			"use_active_image": map[string]any{
				"type":        "boolean",
				"description": "If true and input_path is empty, use the current session active image.",
			},
			"output_path": map[string]any{
				"type":        "string",
				"description": "Optional output path. Supports workspace_dir/... and file_cache_dir/... aliases. Relative paths resolve under file_cache_dir/images/. The final file extension is normalized to the returned PNG/JPEG MIME type.",
			},
		},
		"required": []string{"prompt"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *ImageEditTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || !t.cfg.Enabled {
		return "", fmt.Errorf("image_edit tool is disabled")
	}
	if strings.TrimSpace(t.cfg.Model) == "" {
		return "", fmt.Errorf("llm.image.model and llm.model are not configured")
	}
	if t.cfg.Client == nil {
		return "", fmt.Errorf("image client is not configured")
	}
	prompt, err := imagePromptParam(params)
	if err != nil {
		return "", err
	}
	roots := resolveLocalPathRoots(ctx, t.cfg.Roots)
	inputPath, parentID, err := t.resolveEditInputPath(roots, params)
	if err != nil {
		return "", err
	}
	input, err := readImageInput(roots, inputPath)
	if err != nil {
		return "", err
	}
	resp, err := t.cfg.Client.EditImage(ctx, llm.ImageEditRequest{
		Provider: strings.TrimSpace(t.cfg.Provider),
		Model:    strings.TrimSpace(t.cfg.Model),
		Prompt:   prompt,
		Image:    input,
		Options:  cloneImageProviderOptions(t.cfg.Options),
	})
	if err != nil {
		return "", err
	}
	outputPath, displayPath, err := resolveImageOutputPath(roots, stringParam(params, "output_path"), resp.Image.MIMEType)
	if err != nil {
		return "", err
	}
	if err := writeImageOutput(outputPath, resp.Image.Data); err != nil {
		return "", err
	}
	activeID, err := recordImageSessionOutput(t.cfg.Session, t.cfg.Scope, roots, displayPath, resp.Image.MIMEType, len(resp.Image.Data), "image_edit", parentID)
	if err != nil {
		return "", err
	}
	return marshalImageToolResult(imageToolResult{
		Image: imageToolResultImage{
			Path:          displayPath,
			MIMEType:      resp.Image.MIMEType,
			Bytes:         len(resp.Image.Data),
			RevisedPrompt: resp.Image.RevisedPrompt,
		},
		Model:         strings.TrimSpace(t.cfg.Model),
		Provider:      strings.TrimSpace(t.cfg.Provider),
		Usage:         resp.Usage,
		ActiveImageID: activeID,
	}), nil
}

func (t *ImageEditTool) resolveEditInputPath(roots pathroots.PathRoots, params map[string]any) (string, string, error) {
	inputPath := stringParam(params, "input_path")
	if inputPath != "" {
		return inputPath, "", nil
	}
	if !boolParam(params, "use_active_image") {
		return "", "", fmt.Errorf("missing required param: input_path")
	}
	if t.cfg.Session == nil || t.cfg.Scope.Empty() {
		return "", "", fmt.Errorf("active image is not available for this session")
	}
	active, err := t.cfg.Session.Active(t.cfg.Scope, roots)
	if err != nil {
		if errors.Is(err, imagesession.ErrActiveImageMissing) {
			return "", "", fmt.Errorf("active image file is missing")
		}
		return "", "", err
	}
	if active == nil || strings.TrimSpace(active.Path) == "" {
		return "", "", fmt.Errorf("active image is not available for this session")
	}
	return active.Path, strings.TrimSpace(active.ID), nil
}

func recordImageSessionOutput(store *imagesession.Store, scope imagesession.Scope, roots pathroots.PathRoots, displayPath string, mimeType string, bytes int, source string, parentID string) (string, error) {
	if store == nil || scope.Empty() {
		return "", nil
	}
	parentIDs := []string(nil)
	if strings.TrimSpace(parentID) != "" {
		parentIDs = []string{strings.TrimSpace(parentID)}
	}
	rec, err := store.Record(scope, roots, imagesession.ImageRecord{
		Path:      filepath.ToSlash(strings.TrimSpace(displayPath)),
		MIMEType:  strings.TrimSpace(mimeType),
		Bytes:     bytes,
		Source:    strings.TrimSpace(source),
		ParentIDs: parentIDs,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(rec.ID), nil
}

type imageToolResult struct {
	Image         imageToolResultImage `json:"image"`
	Model         string               `json:"model,omitempty"`
	Provider      string               `json:"provider,omitempty"`
	ActiveImageID string               `json:"active_image_id,omitempty"`
	Usage         llm.Usage            `json:"usage"`
}

type imageToolResultImage struct {
	Path          string `json:"path"`
	MIMEType      string `json:"mime_type"`
	Bytes         int    `json:"bytes"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

func marshalImageToolResult(result imageToolResult) string {
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b)
}

func imagePromptParam(params map[string]any) (string, error) {
	prompt := stringParam(params, "prompt")
	if prompt == "" {
		return "", fmt.Errorf("missing required param: prompt")
	}
	if len(prompt) > imageToolMaxPromptBytes {
		return "", fmt.Errorf("prompt too large (%d bytes > %d max)", len(prompt), imageToolMaxPromptBytes)
	}
	return prompt, nil
}

func stringParam(params map[string]any, name string) string {
	if params == nil {
		return ""
	}
	raw, _ := params[name].(string)
	return strings.TrimSpace(raw)
}

func boolParam(params map[string]any, name string) bool {
	if params == nil {
		return false
	}
	raw, ok := params[name]
	if !ok {
		return false
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func readImageInput(roots pathroots.PathRoots, rawPath string) (llm.ImageInput, error) {
	if strings.TrimSpace(rawPath) == "" {
		return llm.ImageInput{}, fmt.Errorf("missing required param: input_path")
	}
	path, err := resolveImageInputPath(roots, rawPath)
	if err != nil {
		return llm.ImageInput{}, err
	}
	path, err = resolveImageRealPath(roots, path)
	if err != nil {
		return llm.ImageInput{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return llm.ImageInput{}, err
	}
	if info.IsDir() {
		return llm.ImageInput{}, fmt.Errorf("input path is a directory: %s", path)
	}
	if info.Size() <= 0 {
		return llm.ImageInput{}, fmt.Errorf("input image is empty: %s", path)
	}
	if info.Size() > imageToolMaxInputBytes {
		return llm.ImageInput{}, fmt.Errorf("input image too large (%d bytes > %d max)", info.Size(), imageToolMaxInputBytes)
	}
	mimeType := imageinput.MIMETypeFromPath(path)
	if imageToolExtensionForMIMEType(mimeType) == "" {
		return llm.ImageInput{}, fmt.Errorf("input image format is not supported: %s", filepath.Ext(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return llm.ImageInput{}, err
	}
	return llm.ImageInput{
		Filename: filepath.Base(path),
		MIMEType: mimeType,
		Data:     data,
	}, nil
}

func resolveImageInputPath(roots pathroots.PathRoots, rawPath string) (string, error) {
	roots = pathroots.New(roots.WorkspaceDir, roots.FileCacheDir, roots.FileStateDir)
	rawPath = pathutil.ExpandHomePath(strings.TrimSpace(rawPath))
	if alias, rest := detectPathAlias(rawPath); alias != "" {
		if alias == "file_state_dir" {
			return "", fmt.Errorf("image input path cannot use file_state_dir")
		}
		return resolveAliasedPath(roots, alias, rest, true)
	}
	if filepath.IsAbs(rawPath) {
		return resolveImageAbsPath(roots, rawPath, false)
	}
	alias := "file_cache_dir"
	if strings.TrimSpace(roots.WorkspaceDir) != "" {
		alias = "workspace_dir"
	}
	return resolveAliasedPath(roots, alias, rawPath, true)
}

func resolveImageRealPath(roots pathroots.PathRoots, path string) (string, error) {
	roots = pathroots.New(roots.WorkspaceDir, roots.FileCacheDir, roots.FileStateDir)
	pathAbs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(pathAbs)
	if err != nil {
		return "", err
	}
	realPath, err = filepath.Abs(realPath)
	if err != nil {
		return "", err
	}
	for _, base := range []string{roots.WorkspaceDir, roots.FileCacheDir} {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		baseAbs, err := filepath.Abs(base)
		if err != nil {
			continue
		}
		baseReal, err := filepath.EvalSymlinks(baseAbs)
		if err != nil {
			baseReal = baseAbs
		}
		baseReal = filepath.Clean(baseReal)
		if pathutil.IsWithinDir(baseReal, realPath) || baseReal == filepath.Clean(realPath) {
			return realPath, nil
		}
	}
	return "", fmt.Errorf("refusing image path outside workspace_dir or file_cache_dir: %s", pathAbs)
}

func resolveImageOutputPath(roots pathroots.PathRoots, rawPath string, mimeType string) (string, string, error) {
	roots = pathroots.New(roots.WorkspaceDir, roots.FileCacheDir, roots.FileStateDir)
	ext := imageToolExtensionForMIMEType(mimeType)
	if ext == "" {
		return "", "", fmt.Errorf("output image MIME type is not supported: %s", mimeType)
	}
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		base := strings.TrimSpace(roots.FileCacheDir)
		if base == "" {
			return "", "", fmt.Errorf("file_cache_dir is not configured")
		}
		name := time.Now().UTC().Format("20060102-150405") + "-" + shortImageID() + ext
		path := filepath.Join(base, "images", name)
		return path, filepath.ToSlash(filepath.Join("file_cache_dir", "images", name)), nil
	}
	rawPath = pathutil.ExpandHomePath(rawPath)
	if alias, rest := detectPathAlias(rawPath); alias != "" {
		if alias == "file_state_dir" {
			return "", "", fmt.Errorf("image output path cannot use file_state_dir")
		}
		rest, err := normalizeImageOutputLeaf(rest, ext)
		if err != nil {
			return "", "", err
		}
		path, err := resolveAliasedPath(roots, alias, rest, true)
		if err != nil {
			return "", "", err
		}
		return path, filepath.ToSlash(filepath.Join(alias, rest)), nil
	}
	rawPath, err := normalizeImageOutputLeaf(rawPath, ext)
	if err != nil {
		return "", "", err
	}
	if filepath.IsAbs(rawPath) {
		path, err := resolveImageAbsPath(roots, rawPath, true)
		if err != nil {
			return "", "", err
		}
		return path, displayImagePath(roots, path), nil
	}
	base := strings.TrimSpace(roots.FileCacheDir)
	if base == "" {
		return "", "", fmt.Errorf("file_cache_dir is not configured")
	}
	rel := filepath.Join("images", strings.TrimLeft(rawPath, "/\\"))
	path, err := resolveAliasedPath(roots, "file_cache_dir", rel, true)
	if err != nil {
		return "", "", err
	}
	return path, filepath.ToSlash(filepath.Join("file_cache_dir", rel)), nil
}

func normalizeImageOutputLeaf(path string, expectedExt string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("missing output path")
	}
	currentExt := strings.ToLower(filepath.Ext(path))
	if currentExt == "" {
		return path + expectedExt, nil
	}
	if !imageExtensionMatchesMIME(currentExt, expectedExt) {
		return strings.TrimSuffix(path, currentExt) + expectedExt, nil
	}
	return path, nil
}

func imageExtensionMatchesMIME(currentExt, expectedExt string) bool {
	currentExt = strings.ToLower(strings.TrimSpace(currentExt))
	expectedExt = strings.ToLower(strings.TrimSpace(expectedExt))
	if expectedExt == ".jpg" {
		return currentExt == ".jpg" || currentExt == ".jpeg"
	}
	return currentExt == expectedExt
}

func imageToolExtensionForMIMEType(mimeType string) string {
	switch imageinput.NormalizeMIMEType(mimeType) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	default:
		return ""
	}
}

func resolveImageAbsPath(roots pathroots.PathRoots, rawPath string, forWrite bool) (string, error) {
	pathAbs, err := filepath.Abs(filepath.Clean(rawPath))
	if err != nil {
		return "", err
	}
	for _, base := range []string{roots.WorkspaceDir, roots.FileCacheDir} {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		baseAbs, err := filepath.Abs(base)
		if err != nil {
			continue
		}
		if pathutil.IsWithinDir(baseAbs, pathAbs) || filepath.Clean(baseAbs) == filepath.Clean(pathAbs) {
			if forWrite && filepath.Clean(baseAbs) == filepath.Clean(pathAbs) {
				return "", fmt.Errorf("invalid output path: base directory is not a file path")
			}
			return pathAbs, nil
		}
	}
	return "", fmt.Errorf("refusing image path outside workspace_dir or file_cache_dir: %s", pathAbs)
}

func writeImageOutput(path string, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("output image is empty")
	}
	if int64(len(data)) > imageToolMaxOutputBytes {
		return fmt.Errorf("output image too large (%d bytes > %d max)", len(data), imageToolMaxOutputBytes)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write image to symlink: %s", path)
		}
		if st.IsDir() {
			return fmt.Errorf("image output path is a directory: %s", path)
		}
	}
	return os.WriteFile(path, data, 0o600)
}

func displayImagePath(roots pathroots.PathRoots, path string) string {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	for _, item := range []struct {
		alias string
		base  string
	}{
		{alias: "workspace_dir", base: roots.WorkspaceDir},
		{alias: "file_cache_dir", base: roots.FileCacheDir},
	} {
		base := strings.TrimSpace(item.base)
		if base == "" {
			continue
		}
		baseAbs, err := filepath.Abs(base)
		if err != nil {
			continue
		}
		if !pathutil.IsWithinDir(baseAbs, pathAbs) {
			continue
		}
		rel, err := filepath.Rel(baseAbs, pathAbs)
		if err != nil {
			continue
		}
		return filepath.ToSlash(filepath.Join(item.alias, rel))
	}
	return path
}

func shortImageID() string {
	id := strings.ReplaceAll(uuid.NewString(), "-", "")
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func cloneImageProviderOptions(in llm.ImageProviderOptions) llm.ImageProviderOptions {
	return llm.ImageProviderOptions{
		OpenAI:     cloneAnyMapForImage(in.OpenAI),
		Gemini:     cloneAnyMapForImage(in.Gemini),
		Cloudflare: cloneAnyMapForImage(in.Cloudflare),
	}
}

func cloneAnyMapForImage(in map[string]any) map[string]any {
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
