package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/guard"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	slackbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/slack"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
	"github.com/quailyquaily/mistermorph/internal/textutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
)

type RunOptions struct {
	BotToken                      string
	AppToken                      string
	AllowedTeamIDs                []string
	AllowedChannelIDs             []string
	GroupTriggerMode              string
	AddressingConfidenceThreshold float64
	AddressingInterjectThreshold  float64
	TaskTimeout                   time.Duration
	MaxConcurrency                int
	FileCacheDir                  string
	Server                        ServerOptions
	BaseURL                       string
	BusMaxInFlight                int
	RequestTimeout                time.Duration
	AgentLimits                   agent.Limits
	EngineToolsConfig             agent.EngineToolsConfig
	MemoryEnabled                 bool
	MemoryShortTermDays           int
	MemoryInjectionEnabled        bool
	MemoryInjectionMaxItems       int
	Hooks                         Hooks
	InspectPrompt                 bool
	InspectRequest                bool
	TaskStore                     daemonruntime.TaskView
}

type ServerOptions struct {
	Listen    string
	AuthToken string
	MaxQueue  int
	Poke      daemonruntime.PokeFunc
	CronRun   daemonruntime.CronRunFunc
}

type slackJob struct {
	TaskID           string
	ConversationKey  string
	TeamID           string
	ChannelID        string
	ChatType         string
	MessageTS        string
	ThreadTS         string
	UserID           string
	Username         string
	DisplayName      string
	Text             string
	ImagePaths       []string
	Images           []chathistory.ChatHistoryImage
	WorkspaceDir     string
	Route            *llmutil.ResolvedRoute
	ResumeApprovalID string
	SentAt           time.Time
	Version          uint64
	MentionUsers     []string
}

const slackStickySkillsCap = 16
const slackUserIdentityCacheTTL = 6 * time.Hour
const slackCommonReactionEmojiNamesCSV = "+1,-1,ok_hand,clap,pray,tada,muscle,handshake,white_check_mark,heavy_check_mark,x,100,eyes,warning,rotating_light,bangbang,exclamation,question,grey_question,grey_exclamation,triangular_flag_on_post,fire,hourglass_flowing_sand,hourglass,repeat,rewind,fast_forward,construction,hammer_and_wrench,wrench,gear,rocket,bug,mag,mag_right,memo,bookmark_tabs,link,paperclip,pushpin,bell,loudspeaker,computer,file_folder,wave,thinking_face,face_with_monocle,neutral_face,slightly_smiling_face,slightly_frowning_face,joy,sob,sweat_smile,grimacing,calendar,clock1,clock3,clock6,clock9,stopwatch,bar_chart,chart_with_upwards_trend,chart_with_downwards_trend,clipboard"

var slackCommonReactionEmojiNameSet = buildSlackCommonReactionEmojiNameSet()

func slackRunControlConversationKeyForJob(job slackJob) string {
	return slackRunControlConversationKey(job.ConversationKey, job.TeamID, job.ChannelID, job.ThreadTS)
}

func slackRunControlConversationKeyForInbound(inbound slackbus.InboundMessage) string {
	fallback, _ := buildSlackConversationKey(inbound.TeamID, inbound.ChannelID)
	return slackRunControlConversationKey(fallback, inbound.TeamID, inbound.ChannelID, inbound.ThreadTS)
}

func slackRunControlConversationKeyForEvent(event slackInboundEvent) string {
	fallback, _ := buildSlackConversationKey(event.TeamID, event.ChannelID)
	return slackRunControlConversationKey(fallback, event.TeamID, event.ChannelID, event.ThreadTS)
}

func slackRunControlConversationKey(fallback, teamID, channelID, threadTS string) string {
	threadTS = strings.TrimSpace(threadTS)
	key, err := buildSlackHistoryScopeKey(teamID, channelID, threadTS)
	if err == nil && strings.TrimSpace(key) != "" {
		return strings.TrimSpace(key)
	}
	return strings.TrimSpace(fallback)
}

type slackUserIdentityCacheEntry struct {
	Username    string
	DisplayName string
	ExpiresAt   time.Time
}

func buildSlackCommonReactionEmojiNameSet() map[string]bool {
	parts := strings.Split(slackCommonReactionEmojiNamesCSV, ",")
	out := make(map[string]bool, len(parts))
	for _, raw := range parts {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		out[name] = true
	}
	return out
}

func intersectSlackCommonReactionEmojiNames(available []string) []string {
	if len(available) == 0 || len(slackCommonReactionEmojiNameSet) == 0 {
		return nil
	}
	out := make([]string, 0, len(available))
	for _, raw := range available {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if !slackCommonReactionEmojiNameSet[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}

func Run(ctx context.Context, d Dependencies, opts RunOptions) error {
	if err := d.CommonDependencies.Validate(); err != nil {
		return err
	}
	return runSlackLoop(ctx, d, normalizeRunOptions(opts))
}

func runSlackLoop(ctx context.Context, d Dependencies, opts RunOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}

	botToken := strings.TrimSpace(opts.BotToken)
	if botToken == "" {
		return fmt.Errorf("missing slack.bot_token")
	}
	appToken := strings.TrimSpace(opts.AppToken)
	if appToken == "" {
		return fmt.Errorf("missing slack.app_token")
	}

	allowedTeams := toAllowlist(opts.AllowedTeamIDs)
	allowedChannels := toAllowlist(opts.AllowedChannelIDs)

	logger, err := d.Logger()
	if err != nil {
		return err
	}
	daemonStore := opts.TaskStore
	if daemonStore == nil {
		daemonStore, err = daemonruntime.NewTaskViewForTarget("slack", opts.Server.MaxQueue, daemonruntime.TaskViewConfig{
			PersistenceTargets: d.TaskPersistenceTargets,
			TasksDir:           d.RuntimePaths.TasksDir,
			JournalDir:         d.RuntimePaths.JournalDir,
			RotateMaxBytes:     d.TaskRotateMaxBytes,
		})
		if err != nil {
			return err
		}
	}

	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: opts.BusMaxInFlight,
		Logger:      logger,
		Component:   "slack",
	})
	if err != nil {
		return err
	}
	busOwnedByState := false
	defer func() {
		if !busOwnedByState {
			_ = inprocBus.Close()
		}
	}()

	contactsStore := contacts.NewFileStore(d.RuntimePaths.ContactsDir)
	if err := contactsStore.Ensure(context.Background()); err != nil {
		return err
	}
	workspaceStore := workspace.NewStore(d.RuntimePaths.WorkspaceAttachmentsPath)
	contactsSvc := contacts.NewService(contactsStore)
	slackInboundAdapter, err := slackbus.NewInboundAdapter(slackbus.InboundAdapterOptions{
		Bus:   inprocBus,
		Store: contactsStore,
	})
	if err != nil {
		return err
	}

	baseURL := strings.TrimSpace(opts.BaseURL)
	httpClient := &http.Client{Timeout: 30 * time.Second}
	api := newSlackAPI(httpClient, baseURL, botToken, appToken)
	auth, err := api.authTest(ctx)
	if err != nil {
		return fmt.Errorf("slack auth.test: %w", err)
	}
	botUserID := strings.TrimSpace(auth.UserID)
	if botUserID == "" {
		return fmt.Errorf("slack auth.test returned empty user_id")
	}
	if len(allowedTeams) == 0 && strings.TrimSpace(auth.TeamID) != "" {
		allowedTeams[strings.TrimSpace(auth.TeamID)] = true
	}
	emojiLookupCtx, cancelEmojiLookup := context.WithTimeout(ctx, 8*time.Second)
	availableEmojiNames, emojiErr := api.listEmojiNames(emojiLookupCtx)
	cancelEmojiLookup()
	if emojiErr != nil {
		logger.Warn("slack_emoji_catalog_load_failed",
			"error", emojiErr.Error(),
			"hint", "add bot scope emoji:read and reinstall app if message_react should be enabled",
		)
	} else {
		rawCount := len(availableEmojiNames)
		availableEmojiNames = intersectSlackCommonReactionEmojiNames(availableEmojiNames)
		logger.Info("slack_emoji_catalog_loaded",
			"emoji_count", len(availableEmojiNames),
			"emoji_count_raw", rawCount,
		)
	}
	slackDeliveryAdapter, err := slackbus.NewDeliveryAdapter(slackbus.DeliveryAdapterOptions{
		SendText: func(ctx context.Context, target any, text string, opts slackbus.SendTextOptions) error {
			deliverTarget, ok := target.(slackbus.DeliveryTarget)
			if !ok {
				return fmt.Errorf("slack target is invalid")
			}
			return api.postMessage(ctx, deliverTarget.ChannelID, text, opts.ThreadTS)
		},
	})
	if err != nil {
		return err
	}

	sharedRuntime, err := runtimecore.BootstrapChannelRuntime(ctx, d.CommonDependencies, runtimecore.ChannelBootstrapOptions{
		Mode:                "slack",
		InspectRequest:      opts.InspectRequest,
		InspectPrompt:       opts.InspectPrompt,
		AgentConfig:         opts.AgentLimits.ToConfig(),
		EngineToolsConfig:   &opts.EngineToolsConfig,
		MemoryEnabled:       opts.MemoryEnabled,
		MemoryShortTermDays: opts.MemoryShortTermDays,
		Logger:              logger,
	})
	if err != nil {
		return err
	}
	runtimeOwnedByState := false
	defer func() {
		if !runtimeOwnedByState {
			sharedRuntime.Cleanup()
		}
	}()

	runtimeState, stateErr := newSlackRuntimeState(slackRuntimeStateConfig{
		ctx:                 ctx,
		logger:              logger,
		dependencies:        d,
		options:             opts,
		taskStore:           daemonStore,
		api:                 api,
		botUserID:           botUserID,
		allowedTeams:        allowedTeams,
		allowedChannels:     allowedChannels,
		availableEmojiNames: availableEmojiNames,
		inprocBus:           inprocBus,
		contactsService:     contactsSvc,
		workspaceStore:      workspaceStore,
		inboundAdapter:      slackInboundAdapter,
		deliveryAdapter:     slackDeliveryAdapter,
		runtimeBundle:       sharedRuntime,
	})
	busOwnedByState = true
	runtimeOwnedByState = true
	if stateErr != nil {
		return stateErr
	}
	defer runtimeState.close()

	logger.Info("slack_start",
		"bot_user_id", runtimeState.botUserID,
		"allowed_team_ids", len(runtimeState.allowedTeams),
		"allowed_channel_ids", len(runtimeState.allowedChannels),
		"emoji_catalog_size", len(runtimeState.availableEmojiNames),
		"task_timeout", runtimeState.taskTimeout.String(),
		"max_concurrency", opts.MaxConcurrency,
		"group_trigger_mode", runtimeState.groupTriggerMode,
		"addressing_confidence_threshold", runtimeState.addressingConfidenceThreshold,
		"addressing_interject_threshold", runtimeState.addressingInterjectThreshold,
	)
	serverListen := strings.TrimSpace(opts.Server.Listen)
	if serverListen != "" {
		if strings.TrimSpace(opts.Server.AuthToken) == "" {
			logger.Warn("slack_daemon_server_auth_empty", "hint", "set server.auth_token so console can read /tasks")
		}
		if err := runtimeState.serveDaemon(); err != nil {
			logger.Warn("slack_daemon_server_start_error", "addr", serverListen, "error", err.Error())
		}
	}
	return runtimeState.runSocketLoop()
}

func slackOutboundEventFromBusMessage(msg busruntime.BusMessage) (OutboundEvent, error) {
	teamID := strings.TrimSpace(msg.Extensions.TeamID)
	channelID := strings.TrimSpace(msg.Extensions.ChannelID)
	if teamID == "" || channelID == "" {
		parsedTeamID, parsedChannelID, err := slackConversationPartsFromKey(msg.ConversationKey)
		if err != nil {
			return OutboundEvent{}, err
		}
		if teamID == "" {
			teamID = parsedTeamID
		}
		if channelID == "" {
			channelID = parsedChannelID
		}
	}
	env, err := msg.Envelope()
	if err != nil {
		return OutboundEvent{}, err
	}
	threadTS := strings.TrimSpace(msg.Extensions.ThreadTS)
	if threadTS == "" {
		threadTS = strings.TrimSpace(msg.Extensions.ReplyTo)
	}
	if threadTS == "" {
		threadTS = strings.TrimSpace(env.ReplyTo)
	}
	return OutboundEvent{
		ConversationKey: strings.TrimSpace(msg.ConversationKey),
		TeamID:          teamID,
		ChannelID:       channelID,
		ThreadTS:        threadTS,
		Text:            strings.TrimSpace(env.Text),
		CorrelationID:   strings.TrimSpace(msg.CorrelationID),
		Kind:            slackOutboundKind(msg.CorrelationID),
	}, nil
}

func slackConversationPartsFromKey(conversationKey string) (string, string, error) {
	const prefix = "slack:"
	if !strings.HasPrefix(conversationKey, prefix) {
		return "", "", fmt.Errorf("slack conversation key is invalid")
	}
	raw := strings.TrimSpace(strings.TrimPrefix(conversationKey, prefix))
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("slack conversation key is invalid")
	}
	teamID := strings.TrimSpace(parts[0])
	channelID := strings.TrimSpace(parts[1])
	if teamID == "" || channelID == "" {
		return "", "", fmt.Errorf("slack conversation key is invalid")
	}
	return teamID, channelID, nil
}

func slackOutboundKind(correlationID string) string {
	id := strings.ToLower(strings.TrimSpace(correlationID))
	if strings.Contains(id, ":plan:") {
		return "plan_progress"
	}
	if strings.Contains(id, ":error:") {
		return "error"
	}
	return "message"
}

func finalizeSlackPlanProgressMessage(ctx context.Context, logger *slog.Logger, hooks Hooks, job slackJob, workingMessage *slackWorkingMessage, agentCtx *agent.Context) bool {
	if workingMessage == nil || agentCtx == nil || agentCtx.Plan == nil {
		return false
	}
	planText := renderSlackPlanProgressText(agentCtx.Plan)
	text, blocks := buildSlackPlanProgressBlocks(agentCtx.Plan, false)
	if strings.TrimSpace(text) == "" || len(blocks) == 0 {
		return false
	}
	updated, err := workingMessage.UpdateBlocks(ctx, text, blocks)
	if err != nil {
		if logger != nil {
			logger.Warn("slack_plan_progress_finalize_error", "channel_id", job.ChannelID, "message_ts", job.MessageTS, "error", err.Error())
		}
		callErrorHook(ctx, logger, hooks, ErrorEvent{
			Stage:           ErrorStagePublishOutbound,
			ConversationKey: job.ConversationKey,
			TeamID:          job.TeamID,
			ChannelID:       job.ChannelID,
			MessageTS:       job.MessageTS,
			Err:             err,
		})
		return false
	}
	if updated {
		callSlackDirectOutboundHook(ctx, logger, hooks, job, planText, fmt.Sprintf("slack:plan:%s:%s", job.ChannelID, job.MessageTS))
	}
	return updated
}

func normalizeThreshold(primary, secondary, def float64) float64 {
	v := primary
	if v <= 0 {
		v = secondary
	}
	if v <= 0 {
		v = def
	}
	if v > 1 {
		return 1
	}
	return v
}

func slackTaskID(teamID, channelID, messageTS string) string {
	return daemonruntime.BuildTaskID("sl", teamID, channelID, messageTS)
}

func slackApprovalDecisionError(err error) error {
	if errors.Is(err, guard.ErrApprovalNotFound) {
		return daemonruntime.BadRequest("approval not found")
	}
	if errors.Is(err, guard.ErrApprovalNotPending) {
		return daemonruntime.BadRequest("approval is not pending")
	}
	return err
}

func markSlackApprovalResumeFailed(store daemonruntime.TaskUpdater, taskID string, msg string) error {
	taskID = strings.TrimSpace(taskID)
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "unknown error"
	}
	displayErr := "approval resume failed: " + msg
	if store != nil && taskID != "" {
		finishedAt := time.Now().UTC()
		if err := store.Update(taskID, func(info *daemonruntime.TaskInfo) {
			info.Status = daemonruntime.TaskFailed
			info.Error = displayErr
			info.FinishedAt = &finishedAt
			runtimecore.ClearTaskPendingApprovalFields(info)
		}); err != nil {
			return fmt.Errorf("%s: persist task state: %w", displayErr, err)
		}
	}
	return fmt.Errorf("%s", displayErr)
}

func markSlackMissingApprovalHandle(store daemonruntime.TaskView, approvalID string, approved bool) (string, bool, error) {
	taskID := runtimecore.TaskIDForPendingApproval(store, approvalID)
	if taskID == "" {
		return "", false, fmt.Errorf("pending approval handle is unavailable")
	}
	if approved {
		return taskID, false, markSlackApprovalResumeFailed(store, taskID, "pending approval handle is unavailable")
	}
	finishedAt := time.Now().UTC()
	if err := store.Update(taskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskCanceled
		info.Error = slackApprovalResultText(false)
		info.FinishedAt = &finishedAt
		runtimecore.ClearTaskPendingApprovalFields(info)
	}); err != nil {
		return taskID, false, err
	}
	return taskID, false, nil
}

func slackManagedTopicInfo(teamID, channelID, threadTS, messageTS string) (string, string) {
	teamID = strings.TrimSpace(teamID)
	channelID = strings.TrimSpace(channelID)
	threadTS = strings.TrimSpace(threadTS)
	messageTS = strings.TrimSpace(messageTS)
	topicID := "slack:" + teamID + ":" + channelID
	title := "Slack · " + channelID
	if threadTS != "" && threadTS != messageTS {
		topicID += ":thread:" + threadTS
		title += " · thread"
	}
	return textutil.TruncateRunes(topicID, 120), textutil.TruncateRunes(title, 72)
}

func recordSlackQueuedTask(store daemonruntime.TaskView, info daemonruntime.TaskInfo, trigger daemonruntime.TaskTrigger, topicTitle string) error {
	if store == nil {
		return nil
	}
	if writer, ok := store.(interface {
		UpsertWithTrigger(daemonruntime.TaskInfo, daemonruntime.TaskTrigger, string) error
	}); ok {
		return writer.UpsertWithTrigger(info, trigger, topicTitle)
	}
	return taskdomain.RecordTaskUpsert(store, info, trigger)
}

func isSlackTaskContextCanceled(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "context canceled") || strings.Contains(msg, "context deadline exceeded")
}
