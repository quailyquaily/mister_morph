package telegram

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickalie/go-webpbin"
	"github.com/quailyquaily/mistermorph/agent"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/todo"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	telegramtools "github.com/quailyquaily/mistermorph/tools/telegram"
)

type runtimeTaskOptions struct {
	MemoryEnabled           bool
	MemoryInjectionEnabled  bool
	MemoryInjectionMaxItems int
	ImageRecognitionEnabled bool
	MemoryOrchestrator      *memoryruntime.Orchestrator
	MemoryProjectionWorker  *memoryruntime.ProjectionWorker
}

const (
	telegramLLMMaxImages     = 3
	telegramLLMMaxImageBytes = int64(5 * 1024 * 1024)
)

var encodeImageToWebP = defaultEncodeImageToWebP

func runTelegramTask(ctx context.Context, rt *taskruntime.Runtime, api *telegramAPI, fileCacheDir string, filesMaxBytes int64, allowedIDs map[int64]bool, job telegramJob, botUsername string, history []chathistory.ChatHistoryItem, historyCap int, stickySkills []string, requestTimeout time.Duration, runtimeOpts runtimeTaskOptions, sendTelegramText func(context.Context, int64, string, string) error) (*agent.Final, *agent.Context, []string, *telegramtools.Reaction, error) {
	if rt == nil {
		return nil, nil, nil, nil, fmt.Errorf("telegram task runtime is nil")
	}
	ctx = llmstats.WithRunID(ctx, job.TaskID)
	if sendTelegramText == nil {
		return nil, nil, nil, nil, fmt.Errorf("send telegram text callback is required")
	}
	logger := rt.Logger
	task := job.Text
	historyMsg, currentMsg, err := buildTelegramPromptMessages(history, job, rt.MainModel, runtimeOpts.ImageRecognitionEnabled, logger)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var llmHistory []llm.Message
	if historyMsg != nil {
		llmHistory = append(llmHistory, *historyMsg)
	}

	// Per-run registry.
	reg := buildTelegramRegistry(rt.BaseRegistry, job.ChatType)
	toolAPI := newTelegramToolAPI(api)
	if api != nil {
		reg.Register(telegramtools.NewSendVoiceTool(toolAPI, job.ChatID, fileCacheDir, filesMaxBytes, nil))
		reg.Register(telegramtools.NewSendPhotoTool(toolAPI, job.ChatID, fileCacheDir, filesMaxBytes))
		reg.Register(telegramtools.NewSendFileTool(toolAPI, job.ChatID, fileCacheDir, filesMaxBytes))
	}
	var reactTool *telegramtools.ReactTool
	if api != nil && job.MessageID != 0 {
		reactTool = telegramtools.NewReactTool(toolAPI, job.ChatID, job.MessageID, allowedIDs)
		reg.Register(reactTool)
	}

	memSubjectID := telegramMemorySubjectID(job)
	memoryHooks := taskruntime.MemoryHooks{
		Source:    "telegram",
		SubjectID: memSubjectID,
		LogFields: map[string]any{"chat_id": job.ChatID},
	}
	if runtimeOpts.MemoryEnabled && runtimeOpts.MemoryOrchestrator != nil && memSubjectID != "" {
		memoryHooks.InjectionEnabled = runtimeOpts.MemoryInjectionEnabled
		memoryHooks.InjectionMaxItems = runtimeOpts.MemoryInjectionMaxItems
		memoryHooks.PrepareInjection = func(maxItems int) (string, error) {
			return runtimeOpts.MemoryOrchestrator.PrepareInjectionWithAdapter(telegramMemoryInjectionAdapter{job: job}, maxItems)
		}
		memoryHooks.ShouldRecord = func(final *agent.Final) bool {
			return shouldWriteMemory(shouldPublishTelegramText(final), runtimeOpts.MemoryEnabled, runtimeOpts.MemoryOrchestrator, memSubjectID)
		}
		memoryHooks.Record = func(final *agent.Final, _ string) error {
			return recordMemoryFromJob(logger, runtimeOpts.MemoryOrchestrator, job, history, historyCap, final)
		}
		memoryHooks.NotifyRecorded = func() {
			if runtimeOpts.MemoryProjectionWorker != nil {
				runtimeOpts.MemoryProjectionWorker.NotifyRecordAppended()
			}
		}
	}

	planUpdateHook := func(runCtx *agent.Context, update agent.PlanStepUpdate) {
		if runCtx == nil || runCtx.Plan == nil {
			return
		}
		msg, err := generateTelegramPlanProgressMessage(ctx, rt.MainClient, rt.MainModel, task, runCtx.Plan, update, requestTimeout)
		if err != nil {
			logger.Warn("telegram_plan_progress_error", "error", err.Error())
			return
		}
		if strings.TrimSpace(msg) == "" {
			return
		}
		correlationID := fmt.Sprintf("telegram:plan:%d:%d", job.ChatID, job.MessageID)
		if err := sendTelegramText(context.Background(), job.ChatID, msg, correlationID); err != nil {
			logger.Warn("telegram_bus_publish_error", "channel", busruntime.ChannelTelegram, "chat_id", job.ChatID, "message_id", job.MessageID, "bus_error_code", busErrorCodeString(err), "error", err.Error())
		}
	}
	meta := job.Meta
	if meta == nil {
		meta = map[string]any{
			"trigger":               "telegram",
			"telegram_chat_id":      job.ChatID,
			"telegram_message_id":   job.MessageID,
			"telegram_chat_type":    job.ChatType,
			"telegram_from_user_id": job.FromUserID,
		}
	}
	botUsername = strings.TrimPrefix(strings.TrimSpace(botUsername), "@")
	if botUsername != "" {
		meta["telegram_bot_username"] = botUsername
	}
	result, err := rt.Run(ctx, taskruntime.RunRequest{
		Task:           task,
		Model:          rt.MainModel,
		Scene:          "telegram.loop",
		History:        llmHistory,
		Meta:           meta,
		CurrentMessage: currentMsg,
		StickySkills:   stickySkills,
		Registry:       reg,
		PromptAugment: func(spec *agent.PromptSpec, reg *tools.Registry) {
			toolsutil.SetTodoUpdateToolAddContext(reg, todo.AddResolveContext{
				Channel:          "telegram",
				ChatType:         job.ChatType,
				ChatID:           job.ChatID,
				SpeakerUserID:    job.FromUserID,
				SpeakerUsername:  job.FromUsername,
				MentionUsernames: append([]string(nil), job.MentionUsers...),
				UserInputRaw:     job.Text,
			})
			promptprofile.AppendTelegramRuntimeBlocks(spec, isGroupChat(job.ChatType), job.MentionUsers, strings.Join(telegramtools.StandardReactionEmojis(), ","))
		},
		PlanStepUpdate: planUpdateHook,
		Memory:         memoryHooks,
	})
	if err != nil {
		return result.Final, result.Context, result.LoadedSkills, nil, err
	}

	var reaction *telegramtools.Reaction
	if reactTool != nil {
		reaction = reactTool.LastReaction()
		if reaction != nil && logger != nil {
			logger.Info("message_reaction_applied",
				"chat_id", reaction.ChatID,
				"message_id", reaction.MessageID,
				"emoji", reaction.Emoji,
				"source", reaction.Source,
			)
		}
	}
	return result.Final, result.Context, result.LoadedSkills, reaction, nil
}

func buildTelegramPromptMessages(history []chathistory.ChatHistoryItem, job telegramJob, model string, imageRecognitionEnabled bool, logger *slog.Logger) (*llm.Message, *llm.Message, error) {
	historyRaw, err := chathistory.RenderHistoryContext(chathistory.ChannelTelegram, history)
	if err != nil {
		return nil, nil, fmt.Errorf("render telegram history context: %w", err)
	}
	var historyMsg *llm.Message
	if strings.TrimSpace(historyRaw) != "" {
		msg, buildErr := buildTelegramHistoryMessage(historyRaw, model, nil, logger)
		if buildErr != nil {
			return nil, nil, buildErr
		}
		historyMsg = &msg
	}

	currentRaw, err := chathistory.RenderCurrentMessage(newTelegramInboundHistoryItem(job))
	if err != nil {
		return nil, nil, fmt.Errorf("render telegram current message: %w", err)
	}
	imagePaths := append([]string(nil), job.ImagePaths...)
	if !imageRecognitionEnabled {
		imagePaths = nil
	}
	currentMsg, err := buildTelegramCurrentMessage(currentRaw, model, imagePaths, logger)
	if err != nil {
		return nil, nil, err
	}
	return historyMsg, &currentMsg, nil
}

func shouldWriteMemory(publishText bool, memoryEnabled bool, orchestrator *memoryruntime.Orchestrator, subjectID string) bool {
	if !publishText || !memoryEnabled || orchestrator == nil {
		return false
	}
	return strings.TrimSpace(subjectID) != ""
}

func buildTelegramRegistry(baseReg *tools.Registry, chatType string) *tools.Registry {
	reg := tools.NewRegistry()
	if baseReg == nil {
		return reg
	}
	groupChat := isGroupChat(chatType)
	for _, t := range baseReg.All() {
		name := strings.TrimSpace(t.Name())
		if groupChat && strings.EqualFold(name, "contacts_send") {
			continue
		}
		reg.Register(t)
	}
	return reg
}

func generateTelegramPlanProgressMessage(ctx context.Context, client llm.Client, model string, task string, plan *agent.Plan, update agent.PlanStepUpdate, requestTimeout time.Duration) (string, error) {
	_ = ctx
	_ = client
	_ = model
	_ = task
	_ = requestTimeout

	if plan == nil {
		return "", nil
	}
	if update.CompletedIndex < 0 && update.StartedIndex < 0 {
		return "", nil
	}
	stepText := firstNonEmpty(
		strings.TrimSpace(update.CompletedStep),
		stepByIndex(plan, update.CompletedIndex),
		strings.TrimSpace(update.StartedStep),
		stepByIndex(plan, update.StartedIndex),
	)
	if stepText == "" {
		return "", nil
	}
	return stepText, nil
}

func stepByIndex(plan *agent.Plan, index int) string {
	if plan == nil || index < 0 || index >= len(plan.Steps) {
		return ""
	}
	return strings.TrimSpace(plan.Steps[index].Step)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func buildTelegramHistoryMessage(content string, model string, imagePaths []string, logger *slog.Logger) (llm.Message, error) {
	return buildTelegramCurrentMessage(content, model, imagePaths, logger)
}

func buildTelegramCurrentMessage(content string, model string, imagePaths []string, logger *slog.Logger) (llm.Message, error) {
	msg := llm.Message{Role: "user", Content: content}
	if !llm.ModelSupportsImageParts(model) {
		return msg, nil
	}
	if len(imagePaths) == 0 {
		return msg, nil
	}
	parts := make([]llm.Part, 0, 1+min(len(imagePaths), telegramLLMMaxImages))
	if strings.TrimSpace(content) != "" {
		parts = append(parts, llm.Part{Type: llm.PartTypeText, Text: content})
	}

	enableWebPTranscode := llm.ModelSupportsWebPTranscode(model)
	seen := make(map[string]bool, len(imagePaths))
	imageCount := 0
	for _, rawPath := range imagePaths {
		if imageCount >= telegramLLMMaxImages {
			break
		}
		path := strings.TrimSpace(rawPath)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true

		info, err := os.Stat(path)
		if err != nil {
			if logger != nil {
				logger.Warn("telegram_image_part_skip", "path", path, "error", err.Error())
			}
			continue
		}
		if info.Size() <= 0 {
			continue
		}
		if info.Size() > telegramLLMMaxImageBytes {
			return llm.Message{}, fmt.Errorf("图片太大: %s (%d bytes > %d bytes)", filepath.Base(path), info.Size(), telegramLLMMaxImageBytes)
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			if logger != nil {
				logger.Warn("telegram_image_part_read_error", "path", path, "error", err.Error())
			}
			continue
		}
		mimeType := telegramImageMIMEType(path)
		if !isTelegramSupportedUploadImageMIME(mimeType) {
			if logger != nil {
				logger.Warn("telegram_image_part_skip_unsupported_format", "path", path, "mime_type", mimeType)
			}
			continue
		}
		if enableWebPTranscode && shouldTelegramTranscodeToWebP(mimeType) {
			webpRaw, webpErr := encodeImageToWebP(raw)
			if webpErr != nil {
				return llm.Message{}, fmt.Errorf("图片转换失败: %s: %w", filepath.Base(path), webpErr)
			}
			raw = webpRaw
			mimeType = "image/webp"
		}

		parts = append(parts, llm.Part{
			Type:       llm.PartTypeImageBase64,
			MIMEType:   mimeType,
			DataBase64: base64.StdEncoding.EncodeToString(raw),
		})
		imageCount++
	}
	if imageCount == 0 {
		return msg, nil
	}
	msg.Parts = parts
	return msg, nil
}

func telegramImageMIMEType(path string) string {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(path)))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".heic":
		return "image/heic"
	case ".heif":
		return "image/heif"
	}
	return "image/png"
}

func isTelegramSupportedUploadImageMIME(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	switch mimeType {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func shouldTelegramTranscodeToWebP(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	switch mimeType {
	case "image/jpeg", "image/png":
		return true
	default:
		return false
	}
}

func defaultEncodeImageToWebP(raw []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := webpbin.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
