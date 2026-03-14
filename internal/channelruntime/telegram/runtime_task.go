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
	"strconv"
	"strings"
	"time"

	"github.com/nickalie/go-webpbin"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
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
	PlanCreateClient        llm.Client
	PlanCreateModel         string
	MemoryOrchestrator      *memoryruntime.Orchestrator
	MemoryProjectionWorker  *memoryruntime.ProjectionWorker
}

const (
	telegramLLMMaxImages     = 3
	telegramLLMMaxImageBytes = int64(5 * 1024 * 1024)
	telegramDraftStreamEvery = 1500 * time.Millisecond
)

var encodeImageToWebP = defaultEncodeImageToWebP

func runTelegramTask(ctx context.Context, d Dependencies, logger *slog.Logger, logOpts agent.LogOptions, client llm.Client, baseReg *tools.Registry, api *telegramAPI, fileCacheDir string, filesMaxBytes int64, sharedGuard *guard.Guard, cfg agent.Config, allowedIDs map[int64]bool, job telegramJob, botUsername string, model string, history []chathistory.ChatHistoryItem, historyCap int, stickySkills []string, requestTimeout time.Duration, runtimeOpts runtimeTaskOptions, sendTelegramText func(context.Context, int64, string, string) error) (*agent.Final, *agent.Context, []string, *telegramtools.Reaction, error) {
	ctx = llmstats.WithRunID(ctx, job.TaskID)
	if sendTelegramText == nil {
		return nil, nil, nil, nil, fmt.Errorf("send telegram text callback is required")
	}
	task := job.Text
	historyMsg, currentMsg, err := buildTelegramPromptMessages(history, job, model, runtimeOpts.ImageRecognitionEnabled, logger)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var llmHistory []llm.Message
	if historyMsg != nil {
		llmHistory = append(llmHistory, *historyMsg)
	}
	if baseReg == nil {
		return nil, nil, nil, nil, fmt.Errorf("base registry is nil")
	}

	// Per-run registry.
	reg := buildTelegramRegistry(baseReg, job.ChatType)
	toolsutil.RegisterRuntimeTools(reg, d.RuntimeToolsConfig, toolsutil.RuntimeToolLLMOptions{
		DefaultClient:    client,
		DefaultModel:     model,
		PlanCreateClient: runtimeOpts.PlanCreateClient,
		PlanCreateModel:  runtimeOpts.PlanCreateModel,
	})
	toolsutil.SetTodoUpdateToolAddContext(reg, todo.AddResolveContext{
		Channel:          "telegram",
		ChatType:         job.ChatType,
		ChatID:           job.ChatID,
		SpeakerUserID:    job.FromUserID,
		SpeakerUsername:  job.FromUsername,
		MentionUsernames: append([]string(nil), job.MentionUsers...),
		UserInputRaw:     job.Text,
	})
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

	promptSpec, loadedSkills, err := depsutil.PromptSpecFromCommon(d, ctx, logger, logOpts, task, client, model, stickySkills)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	promptprofile.ApplyPersonaIdentity(&promptSpec, logger)
	promptprofile.AppendLocalToolNotesBlock(&promptSpec, logger)
	promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, reg)
	promptprofile.AppendTodoWorkflowBlock(&promptSpec, reg)
	promptprofile.AppendTelegramRuntimeBlocks(&promptSpec, isGroupChat(job.ChatType), job.MentionUsers, strings.Join(telegramtools.StandardReactionEmojis(), ","))

	memSubjectID := telegramMemorySubjectID(job)
	if runtimeOpts.MemoryEnabled && runtimeOpts.MemoryOrchestrator != nil && memSubjectID != "" && runtimeOpts.MemoryInjectionEnabled {
		snap, memErr := runtimeOpts.MemoryOrchestrator.PrepareInjectionWithAdapter(telegramMemoryInjectionAdapter{job: job}, runtimeOpts.MemoryInjectionMaxItems)
		if memErr != nil {
			if logger != nil {
				logger.Warn("memory_injection_error", "source", "telegram", "subject_id", memSubjectID, "chat_id", job.ChatID, "error", memErr.Error())
			}
		} else if strings.TrimSpace(snap) != "" {
			promptprofile.AppendMemorySummariesBlock(&promptSpec, snap)
			if logger != nil {
				logger.Info("memory_injection_applied", "source", "telegram", "subject_id", memSubjectID, "chat_id", job.ChatID, "snapshot_len", len(snap))
			}
		} else if logger != nil {
			logger.Debug("memory_injection_skipped", "source", "telegram", "reason", "empty_snapshot", "subject_id", memSubjectID, "chat_id", job.ChatID)
		}
	}

	planUpdateHook := func(runCtx *agent.Context, update agent.PlanStepUpdate) {
		if runCtx == nil || runCtx.Plan == nil {
			return
		}
		msg, err := generateTelegramPlanProgressMessage(ctx, client, model, task, runCtx.Plan, update, requestTimeout)
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
	streamPublisher := newTelegramDraftStreamPublisher(logger, api, job.ChatID, job.MessageID, job.ChatType, model)
	streamHandler := streamPublisher.StreamHandler()

	engineOpts := []agent.Option{
		agent.WithLogger(logger),
		agent.WithLogOptions(logOpts),
		agent.WithGuard(sharedGuard),
		agent.WithPlanStepUpdate(planUpdateHook),
	}
	engine := agent.New(
		client,
		reg,
		cfg,
		promptSpec,
		engineOpts...,
	)
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
	final, agentCtx, err := engine.Run(ctx, task, agent.RunOptions{
		Model:          model,
		Scene:          "telegram.loop",
		History:        llmHistory,
		Meta:           meta,
		CurrentMessage: currentMsg,
		OnStream:       streamHandler,
	})
	if err != nil {
		return final, agentCtx, loadedSkills, nil, err
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

	publishText := shouldPublishTelegramText(final)
	if shouldWriteMemory(publishText, runtimeOpts.MemoryEnabled, runtimeOpts.MemoryOrchestrator, memSubjectID) {
		if err := recordMemoryFromJob(logger, runtimeOpts.MemoryOrchestrator, job, history, historyCap, final); err != nil {
			logger.Warn("memory_update_error", "error", err.Error())
		} else if runtimeOpts.MemoryProjectionWorker != nil {
			runtimeOpts.MemoryProjectionWorker.NotifyRecordAppended()
		}
	}
	return final, agentCtx, loadedSkills, reaction, nil
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

type telegramDraftStreamPublisher struct {
	logger          *slog.Logger
	api             *telegramAPI
	chatID          int64
	draftID         int64
	enabled         bool
	extractor       telegramOutputStreamExtractor
	lastSentText    string
	lastPublishedAt time.Time
	draftDisabled   bool
}

func newTelegramDraftStreamPublisher(logger *slog.Logger, api *telegramAPI, chatID int64, messageID int64, chatType string, model string) *telegramDraftStreamPublisher {
	draftID := int64(0)
	if messageID > 0 {
		draftID = messageID
	}
	enabled := api != nil &&
		draftID > 0 &&
		chatID > 0 &&
		strings.EqualFold(strings.TrimSpace(chatType), "private") &&
		modelSupportsTelegramDraftStream(model)
	return &telegramDraftStreamPublisher{
		logger:  logger,
		api:     api,
		chatID:  chatID,
		draftID: draftID,
		enabled: enabled,
	}
}

func (p *telegramDraftStreamPublisher) StreamHandler() llm.StreamHandler {
	if p == nil || !p.enabled {
		return nil
	}
	return p.OnStream
}

func modelSupportsTelegramDraftStream(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	return modelHasPrefixOrNamespace(model, "gpt-")
}

func modelHasPrefixOrNamespace(model string, token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return false
	}
	return strings.HasPrefix(model, token) || strings.Contains(model, "/"+token)
}

func (p *telegramDraftStreamPublisher) OnStream(ev llm.StreamEvent) error {
	if p == nil || !p.enabled || p.api == nil || p.draftID <= 0 || p.draftDisabled {
		return nil
	}
	changed := false
	if delta := ev.Delta; delta != "" {
		changed = p.extractor.Append(delta)
	}
	if !changed && !ev.Done {
		return nil
	}
	p.publish(ev.Done)
	if ev.Done {
		p.extractor.Reset()
	}
	return nil
}

func (p *telegramDraftStreamPublisher) publish(force bool) {
	if p == nil || !p.enabled || p.api == nil || p.draftID <= 0 || p.draftDisabled {
		return
	}
	text := p.extractor.Output()
	if strings.TrimSpace(text) == "" {
		return
	}
	if text == p.lastSentText {
		return
	}
	if !force && !p.lastPublishedAt.IsZero() && time.Since(p.lastPublishedAt) < telegramDraftStreamEvery {
		return
	}
	if err := p.api.sendMessageDraftHTML(context.Background(), p.chatID, p.draftID, text, true); err != nil {
		p.draftDisabled = true
		if p.logger != nil {
			p.logger.Warn("telegram_stream_publish_error",
				"chat_id", p.chatID,
				"draft_id", p.draftID,
				"error", err.Error(),
			)
		}
		return
	}
	p.lastSentText = text
	p.lastPublishedAt = time.Now()
}

type telegramOutputStreamExtractor struct {
	raw    string
	output string
}

func (e *telegramOutputStreamExtractor) Append(delta string) bool {
	if delta == "" {
		return false
	}
	e.raw += delta
	out, _ := extractTelegramFinalOutputFromJSONStream(e.raw)
	if out == e.output {
		return false
	}
	e.output = out
	return true
}

func (e *telegramOutputStreamExtractor) Output() string {
	if e == nil {
		return ""
	}
	return e.output
}

func (e *telegramOutputStreamExtractor) Reset() {
	if e == nil {
		return
	}
	e.raw = ""
	e.output = ""
}

func extractTelegramFinalOutputFromJSONStream(raw string) (string, bool) {
	const key = `"output":"`
	keyStart := strings.Index(raw, key)
	if keyStart < 0 {
		return "", false
	}
	return decodeJSONStringPrefix(raw[keyStart+len(`"output":`):])
}

func decodeJSONStringPrefix(raw string) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var b strings.Builder
	for i := 1; i < len(raw); i++ {
		ch := raw[i]
		if ch == '"' {
			return b.String(), true
		}
		if ch == '\\' {
			if i+1 >= len(raw) {
				return b.String(), false
			}
			esc := raw[i+1]
			switch esc {
			case '"', '\\', '/':
				b.WriteByte(esc)
				i++
			case 'b':
				b.WriteByte('\b')
				i++
			case 'f':
				b.WriteByte('\f')
				i++
			case 'n':
				b.WriteByte('\n')
				i++
			case 'r':
				b.WriteByte('\r')
				i++
			case 't':
				b.WriteByte('\t')
				i++
			case 'u':
				if i+6 > len(raw) {
					return b.String(), false
				}
				u, err := strconv.ParseUint(raw[i+2:i+6], 16, 64)
				if err != nil {
					b.WriteString(raw[i : i+6])
				} else {
					b.WriteRune(rune(u))
				}
				i += 5
			default:
				b.WriteByte(esc)
				i++
			}
			continue
		}
		b.WriteByte(ch)
	}
	return b.String(), false
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
