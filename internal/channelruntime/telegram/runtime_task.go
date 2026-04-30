package telegram

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/nickalie/go-webpbin"
	"github.com/quailyquaily/mistermorph/agent"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imageinput"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/todo"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
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
	ctx = pathroots.WithWorkspaceDir(ctx, job.WorkspaceDir)
	ctx = builtin.WithContactsSendRuntimeContext(ctx, contactsSendRuntimeContextForTelegram(job))
	if sendTelegramText == nil {
		return nil, nil, nil, nil, fmt.Errorf("send telegram text callback is required")
	}
	logger := rt.Logger
	task := job.Text
	mainRoute, err := rt.ResolveMainRouteForRun()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	mainClient, err := rt.CreateClientForRoute(mainRoute)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer closeTelegramMainClient(mainClient)
	mainModel := strings.TrimSpace(mainRoute.ClientConfig.Model)
	historyMsg, currentMsg, err := buildTelegramPromptMessages(history, job, mainModel, runtimeOpts.ImageRecognitionEnabled, logger)
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
		msg := telegramPlanProgressText(runCtx.Plan, update)
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
		Model:          mainModel,
		Scene:          "telegram.loop",
		History:        llmHistory,
		Meta:           meta,
		CurrentMessage: currentMsg,
		StickySkills:   stickySkills,
		Registry:       reg,
		PromptAugment: func(spec *agent.PromptSpec, reg *tools.Registry) {
			if block := workspace.PromptBlock(job.WorkspaceDir); strings.TrimSpace(block.Content) != "" {
				spec.Blocks = append([]agent.PromptBlock{block}, spec.Blocks...)
			}
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

func closeTelegramMainClient(client llm.Client) {
	closer, ok := client.(io.Closer)
	if !ok {
		return
	}
	_ = closer.Close()
}

func buildTelegramPromptMessages(history []chathistory.ChatHistoryItem, job telegramJob, model string, imageRecognitionEnabled bool, logger *slog.Logger) (*llm.Message, *llm.Message, error) {
	historyRaw, err := chathistory.RenderHistoryContext(chathistory.ChannelTelegram, history)
	if err != nil {
		return nil, nil, fmt.Errorf("render telegram history context: %w", err)
	}
	var historyMsg *llm.Message
	if strings.TrimSpace(historyRaw) != "" {
		msg := llm.Message{Role: "user", Content: historyRaw}
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

func contactsSendRuntimeContextForTelegram(job telegramJob) builtin.ContactsSendRuntimeContext {
	ids := make([]string, 0, 3)
	if username := strings.TrimPrefix(strings.TrimSpace(job.FromUsername), "@"); username != "" {
		ids = append(ids, "tg:@"+username)
	}
	if job.FromUserID > 0 {
		ids = append(ids, fmt.Sprintf("tg:%d", job.FromUserID))
	}
	if job.ChatID != 0 && strings.EqualFold(strings.TrimSpace(job.ChatType), "private") {
		ids = append(ids, fmt.Sprintf("tg:%d", job.ChatID))
	}
	return builtin.ContactsSendRuntimeContext{ForbiddenTargetIDs: ids}
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

func telegramPlanProgressText(plan *agent.Plan, update agent.PlanStepUpdate) string {
	if plan == nil {
		return ""
	}
	stepText := strings.TrimSpace(update.StartedStep)
	if stepText == "" {
		stepText = stepByIndex(plan, update.StartedIndex)
	}
	return stepText
}

func stepByIndex(plan *agent.Plan, index int) string {
	if plan == nil || index < 0 || index >= len(plan.Steps) {
		return ""
	}
	return strings.TrimSpace(plan.Steps[index].Step)
}

func buildTelegramCurrentMessage(content string, model string, imagePaths []string, logger *slog.Logger) (llm.Message, error) {
	var transcode imageinput.TranscodeFunc
	if llm.ModelSupportsWebPTranscode(model) {
		transcode = func(raw []byte, mimeType string) ([]byte, string, error) {
			if shouldTelegramTranscodeToWebP(mimeType) {
				webpRaw, err := encodeImageToWebP(raw)
				if err != nil {
					return nil, "", err
				}
				return webpRaw, "image/webp", nil
			}
			return raw, mimeType, nil
		}
	}
	return imageinput.BuildUserMessage(content, model, imagePaths, imageinput.MessageOptions{
		MaxImages: telegramLLMMaxImages,
		MaxBytes:  telegramLLMMaxImageBytes,
		Logger:    logger,
		LogPrefix: "telegram",
		Transcode: transcode,
	})
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
