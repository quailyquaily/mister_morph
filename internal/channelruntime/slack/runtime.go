package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	slackbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/slack"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/llm"
	slacktools "github.com/quailyquaily/mistermorph/tools/slack"
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
}

type slackJob struct {
	TaskID          string
	ConversationKey string
	TeamID          string
	ChannelID       string
	ChatType        string
	MessageTS       string
	ThreadTS        string
	UserID          string
	Username        string
	DisplayName     string
	Text            string
	SentAt          time.Time
	Version         uint64
	MentionUsers    []string
}

const slackStickySkillsCap = 16
const slackUserIdentityCacheTTL = 6 * time.Hour
const slackCommonReactionEmojiNamesCSV = "+1,-1,ok_hand,clap,pray,tada,muscle,handshake,white_check_mark,heavy_check_mark,x,100,eyes,warning,rotating_light,bangbang,exclamation,question,grey_question,grey_exclamation,triangular_flag_on_post,fire,hourglass_flowing_sand,hourglass,repeat,rewind,fast_forward,construction,hammer_and_wrench,wrench,gear,rocket,bug,mag,mag_right,memo,bookmark_tabs,link,paperclip,pushpin,bell,loudspeaker,computer,file_folder,wave,thinking_face,face_with_monocle,neutral_face,slightly_smiling_face,slightly_frowning_face,joy,sob,sweat_smile,grimacing,calendar,clock1,clock3,clock6,clock9,stopwatch,bar_chart,chart_with_upwards_trend,chart_with_downwards_trend,clipboard"

var slackCommonReactionEmojiNameSet = buildSlackCommonReactionEmojiNameSet()

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
	return runSlackLoop(ctx, d, resolveRuntimeLoopOptionsFromRunOptions(opts))
}

func runSlackLoop(ctx context.Context, d Dependencies, opts runtimeLoopOptions) error {
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

	logger, err := depsutil.LoggerFromCommon(d.CommonDependencies)
	if err != nil {
		return err
	}
	hooks := opts.Hooks
	slog.SetDefault(logger)
	daemonStore := opts.TaskStore
	if daemonStore == nil {
		daemonStore, err = daemonruntime.NewTaskViewForTarget("slack", opts.Server.MaxQueue)
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
	defer inprocBus.Close()

	contactsStore := contacts.NewFileStore(statepaths.ContactsDir())
	if err := contactsStore.Ensure(context.Background()); err != nil {
		return err
	}
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
	availableEmojiList := strings.Join(availableEmojiNames, ",")

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

	requestTimeout := opts.RequestTimeout
	var requestInspector *llminspect.RequestInspector
	if opts.InspectRequest {
		requestInspector, err = llminspect.NewRequestInspector(llminspect.Options{
			Mode:            "slack",
			Task:            "slack",
			TimestampFormat: "20060102_150405",
		})
		if err != nil {
			return err
		}
		defer func() { _ = requestInspector.Close() }()
	}
	var promptInspector *llminspect.PromptInspector
	if opts.InspectPrompt {
		promptInspector, err = llminspect.NewPromptInspector(llminspect.Options{
			Mode:            "slack",
			Task:            "slack",
			TimestampFormat: "20060102_150405",
		})
		if err != nil {
			return err
		}
		defer func() { _ = promptInspector.Close() }()
	}
	decorateRuntimeClient := func(client llm.Client, route llmutil.ResolvedRoute) llm.Client {
		return llminspect.WrapClient(client, llminspect.ClientOptions{
			PromptInspector:  promptInspector,
			RequestInspector: requestInspector,
			APIBase:          route.ClientConfig.Endpoint,
			Model:            strings.TrimSpace(route.ClientConfig.Model),
		})
	}
	execRuntime, err := taskruntime.Bootstrap(d.CommonDependencies, taskruntime.BootstrapOptions{
		AgentConfig:       opts.AgentLimits.ToConfig(),
		EngineToolsConfig: &opts.EngineToolsConfig,
		ClientDecorator:   decorateRuntimeClient,
	})
	if err != nil {
		return err
	}
	mainRoute := execRuntime.BootstrapMainRoute
	model := execRuntime.BootstrapMainModel
	addressingRoute, err := depsutil.ResolveLLMRouteFromCommon(d.CommonDependencies, llmutil.RoutePurposeAddressing)
	if err != nil {
		return err
	}
	addressingModel := strings.TrimSpace(addressingRoute.ClientConfig.Model)
	addressingClient := execRuntime.BootstrapMainClient
	if !addressingRoute.SameProfile(mainRoute) {
		addressingClient, err = depsutil.CreateClient(d.CreateLLMClient, addressingRoute)
		if err != nil {
			return err
		}
		addressingClient = decorateRuntimeClient(addressingClient, addressingRoute)
	}
	memRuntime, err := runtimecore.NewMemoryRuntime(d.CommonDependencies, runtimecore.MemoryRuntimeOptions{
		Enabled:       opts.MemoryEnabled,
		ShortTermDays: opts.MemoryShortTermDays,
		Logger:        logger,
		Decorate:      decorateRuntimeClient,
	})
	if err != nil {
		return err
	}
	if memRuntime.ProjectionWorker != nil {
		memRuntime.ProjectionWorker.Start(ctx)
	}
	defer memRuntime.Cleanup()
	taskRuntimeOpts := runtimeTaskOptions{
		MemoryEnabled:           opts.MemoryEnabled,
		MemoryInjectionEnabled:  opts.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: opts.MemoryInjectionMaxItems,
		MemoryOrchestrator:      memRuntime.Orchestrator,
		MemoryProjectionWorker:  memRuntime.ProjectionWorker,
	}
	taskTimeout := opts.TaskTimeout
	maxConc := opts.MaxConcurrency
	sem := make(chan struct{}, maxConc)

	groupTriggerMode := strings.ToLower(strings.TrimSpace(opts.GroupTriggerMode))
	fileCacheDir := strings.TrimSpace(opts.FileCacheDir)
	slackHistoryCap := slackHistoryCapForMode(groupTriggerMode)
	addressingLLMTimeout := addressingRoute.ClientConfig.RequestTimeout
	if addressingLLMTimeout <= 0 {
		addressingLLMTimeout = requestTimeout
	}
	addressingConfidenceThreshold := opts.AddressingConfidenceThreshold
	addressingInterjectThreshold := opts.AddressingInterjectThreshold

	serverListen := strings.TrimSpace(opts.Server.Listen)
	if serverListen != "" {
		if strings.TrimSpace(opts.Server.AuthToken) == "" {
			logger.Warn("slack_daemon_server_auth_empty", "hint", "set server.auth_token so console can read /tasks")
		}
		_, err := daemonruntime.StartServer(ctx, logger, daemonruntime.ServerOptions{
			Listen: serverListen,
			Routes: daemonruntime.RoutesOptions{
				Mode:          "slack",
				AgentNameFunc: func() string { return personautil.LoadAgentName(statepaths.FileStateDir()) },
				AuthToken:     strings.TrimSpace(opts.Server.AuthToken),
				TaskReader:    daemonStore,
				Overview: func(ctx context.Context) (map[string]any, error) {
					return map[string]any{
						"llm": map[string]any{
							"provider": strings.TrimSpace(mainRoute.ClientConfig.Provider),
							"model":    model,
						},
						"channel": map[string]any{
							"configured":          true,
							"telegram_configured": false,
							"slack_configured":    true,
							"running":             "slack",
							"telegram_running":    false,
							"slack_running":       true,
						},
					}, nil
				},
				Poke:          opts.Server.Poke,
				HealthEnabled: true,
			},
		})
		if err != nil {
			logger.Warn("slack_daemon_server_start_error", "addr", serverListen, "error", err.Error())
		}
	}

	workersCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()

	var (
		mu                  sync.Mutex
		history             = make(map[string][]chathistory.ChatHistoryItem)
		stickySkillsByConv  = make(map[string][]string)
		userIdentityCache   = make(map[string]slackUserIdentityCacheEntry)
		enqueueSlackInbound func(context.Context, busruntime.BusMessage) error
	)

	resolveSlackUserIdentity := func(ctx context.Context, teamID, userID string) (string, string, error) {
		teamID = strings.TrimSpace(teamID)
		userID = strings.TrimSpace(userID)
		if teamID == "" || userID == "" {
			return "", "", fmt.Errorf("slack user identity requires team_id and user_id")
		}
		cacheKey := strings.ToUpper(teamID) + ":" + strings.ToUpper(userID)
		now := time.Now().UTC()

		mu.Lock()
		if cached, ok := userIdentityCache[cacheKey]; ok && cached.ExpiresAt.After(now) {
			mu.Unlock()
			username := strings.TrimSpace(cached.Username)
			displayName := strings.TrimSpace(cached.DisplayName)
			if username != "" && displayName != "" {
				return username, displayName, nil
			}
			return "", "", fmt.Errorf("slack user identity cache entry is incomplete")
		}
		mu.Unlock()

		lookupCtx := ctx
		if lookupCtx == nil {
			lookupCtx = context.Background()
		}
		lookupCtx, cancel := context.WithTimeout(lookupCtx, 3*time.Second)
		defer cancel()

		identity, err := api.userIdentity(lookupCtx, userID)
		if err != nil {
			return "", "", err
		}

		username := strings.TrimSpace(identity.Username)
		displayName := strings.TrimSpace(identity.DisplayName)
		if username == "" || displayName == "" {
			return "", "", fmt.Errorf("slack users.info returned empty username/display_name")
		}
		mu.Lock()
		userIdentityCache[cacheKey] = slackUserIdentityCacheEntry{
			Username:    username,
			DisplayName: displayName,
			ExpiresAt:   now.Add(slackUserIdentityCacheTTL),
		}
		mu.Unlock()
		return username, displayName, nil
	}

	var runner *runtimecore.ConversationRunner[string, slackJob]
	runner = runtimecore.NewConversationRunner[string, slackJob](
		workersCtx,
		sem,
		16,
		func(workerCtx context.Context, conversationKey string, job slackJob) {
			historyScopeKey := slackHistoryScopeKeyForJob(job)
			if historyScopeKey == "" {
				historyScopeKey = conversationKey
			}
			mu.Lock()
			h := append([]chathistory.ChatHistoryItem(nil), history[historyScopeKey]...)
			sticky := append([]string(nil), stickySkillsByConv[historyScopeKey]...)
			mu.Unlock()
			curVersion := runner.CurrentVersion(conversationKey)
			if job.Version != curVersion {
				h = nil
			}
			runtimecore.MarkTaskRunning(daemonStore, job.TaskID)
			runCtx, cancel := context.WithTimeout(workerCtx, taskTimeout)
			final, _, loadedSkills, reaction, runErr := runSlackTask(
				runCtx,
				execRuntime,
				api,
				job,
				h,
				slackHistoryCap,
				sticky,
				allowedChannels,
				availableEmojiNames,
				fileCacheDir,
				taskRuntimeOpts,
				func(ctx context.Context, text, correlationID string) error {
					if ctx == nil {
						ctx = context.Background()
					}
					_, err := publishSlackBusOutbound(
						ctx,
						inprocBus,
						job.TeamID,
						job.ChannelID,
						text,
						job.ThreadTS,
						correlationID,
					)
					return err
				},
			)
			cancel()

			if runErr != nil {
				if workerCtx.Err() != nil {
					return
				}
				displayErr := depsutil.FormatRuntimeError(runErr)
				runtimecore.MarkTaskFailed(daemonStore, job.TaskID, displayErr, isSlackTaskContextCanceled(runErr))
				callErrorHook(workerCtx, logger, hooks, ErrorEvent{
					Stage:           ErrorStageRunTask,
					ConversationKey: job.ConversationKey,
					TeamID:          job.TeamID,
					ChannelID:       job.ChannelID,
					MessageTS:       job.MessageTS,
					Err:             runErr,
				})
				errorText := "error: " + displayErr
				errorCorrelationID := fmt.Sprintf("slack:error:%s:%s", job.ChannelID, job.MessageTS)
				_, err := publishSlackBusOutbound(
					workerCtx,
					inprocBus,
					job.TeamID,
					job.ChannelID,
					errorText,
					job.ThreadTS,
					errorCorrelationID,
				)
				if err != nil {
					logger.Warn("slack_bus_publish_error", "channel", busruntime.ChannelSlack, "channel_id", job.ChannelID, "bus_error_code", busErrorCodeString(err), "error", err.Error())
					callErrorHook(workerCtx, logger, hooks, ErrorEvent{
						Stage:           ErrorStagePublishErrorReply,
						ConversationKey: job.ConversationKey,
						TeamID:          job.TeamID,
						ChannelID:       job.ChannelID,
						MessageTS:       job.MessageTS,
						Err:             err,
					})
				}
				return
			}

			outText := strings.TrimSpace(depsutil.FormatFinalOutput(final))
			runtimecore.MarkTaskDone(daemonStore, job.TaskID, outText)
			if outText != "" {
				if workerCtx.Err() != nil {
					return
				}
				outCorrelationID := fmt.Sprintf("slack:message:%s:%s", job.ChannelID, job.MessageTS)
				_, err := publishSlackBusOutbound(
					workerCtx,
					inprocBus,
					job.TeamID,
					job.ChannelID,
					outText,
					job.ThreadTS,
					outCorrelationID,
				)
				if err != nil {
					logger.Warn("slack_bus_publish_error", "channel", busruntime.ChannelSlack, "channel_id", job.ChannelID, "bus_error_code", busErrorCodeString(err), "error", err.Error())
					callErrorHook(workerCtx, logger, hooks, ErrorEvent{
						Stage:           ErrorStagePublishOutbound,
						ConversationKey: job.ConversationKey,
						TeamID:          job.TeamID,
						ChannelID:       job.ChannelID,
						MessageTS:       job.MessageTS,
						Err:             err,
					})
				}
			}

			mu.Lock()
			latestVersion := runner.CurrentVersion(conversationKey)
			if latestVersion != curVersion {
				history[historyScopeKey] = nil
				stickySkillsByConv[historyScopeKey] = nil
			}
			if latestVersion == curVersion && len(loadedSkills) > 0 {
				stickySkillsByConv[historyScopeKey] = capUniqueStrings(loadedSkills, slackStickySkillsCap)
			}
			cur := history[historyScopeKey]
			cur = append(cur, newSlackInboundHistoryItem(job))
			if reaction != nil {
				note := "[reacted]"
				if emoji := strings.TrimSpace(reaction.Emoji); emoji != "" {
					note = "[reacted: :" + emoji + ":]"
				}
				cur = append(cur, newSlackOutboundReactionHistoryItem(job, note, reaction.Emoji, time.Now().UTC(), botUserID))
			}
			if outText != "" {
				cur = append(cur, newSlackOutboundAgentHistoryItem(job, outText, time.Now().UTC(), botUserID))
			}
			history[historyScopeKey] = trimChatHistoryItems(cur, slackHistoryCapForMode(groupTriggerMode))
			mu.Unlock()
		},
	)

	enqueueSlackInbound = func(ctx context.Context, msg busruntime.BusMessage) error {
		if ctx == nil {
			ctx = workersCtx
		}
		inbound, err := slackbus.InboundMessageFromBusMessage(msg)
		if err != nil {
			return err
		}
		text := strings.TrimSpace(inbound.Text)
		if text == "" {
			return fmt.Errorf("slack inbound text is required")
		}
		jobTaskID := slackTaskID(inbound.TeamID, inbound.ChannelID, inbound.MessageTS)
		if err := runner.Enqueue(ctx, msg.ConversationKey, func(version uint64) slackJob {
			return slackJob{
				TaskID:          jobTaskID,
				ConversationKey: msg.ConversationKey,
				TeamID:          inbound.TeamID,
				ChannelID:       inbound.ChannelID,
				ChatType:        inbound.ChatType,
				MessageTS:       inbound.MessageTS,
				ThreadTS:        inbound.ThreadTS,
				UserID:          inbound.UserID,
				Username:        inbound.Username,
				DisplayName:     inbound.DisplayName,
				Text:            text,
				SentAt:          inbound.SentAt,
				Version:         version,
				MentionUsers:    append([]string(nil), inbound.MentionUsers...),
			}
		}); err != nil {
			return err
		}
		if daemonStore != nil {
			createdAt := inbound.SentAt.UTC()
			if createdAt.IsZero() {
				createdAt = time.Now().UTC()
			}
			topicID, topicTitle := slackManagedTopicInfo(inbound.TeamID, inbound.ChannelID, inbound.ThreadTS, inbound.MessageTS)
			recordSlackQueuedTask(daemonStore, daemonruntime.TaskInfo{
				ID:        jobTaskID,
				Status:    daemonruntime.TaskQueued,
				Task:      daemonruntime.TruncateUTF8(text, 2000),
				Model:     strings.TrimSpace(model),
				Timeout:   taskTimeout.String(),
				CreatedAt: createdAt,
				TopicID:   topicID,
				Result: map[string]any{
					"source":            "slack",
					"slack_team_id":     inbound.TeamID,
					"slack_channel_id":  inbound.ChannelID,
					"slack_message_ts":  inbound.MessageTS,
					"slack_thread_ts":   inbound.ThreadTS,
					"slack_from_userID": inbound.UserID,
				},
			}, daemonruntime.TaskTrigger{
				Source: "webhook",
				Event:  "webhook_inbound",
				Ref:    fmt.Sprintf("slack/%s/%s/%s", inbound.TeamID, inbound.ChannelID, inbound.MessageTS),
			}, topicTitle)
		}
		callInboundHook(ctx, logger, hooks, InboundEvent{
			ConversationKey: msg.ConversationKey,
			TeamID:          inbound.TeamID,
			ChannelID:       inbound.ChannelID,
			ChatType:        inbound.ChatType,
			MessageTS:       inbound.MessageTS,
			ThreadTS:        inbound.ThreadTS,
			UserID:          inbound.UserID,
			Username:        inbound.Username,
			DisplayName:     inbound.DisplayName,
			Text:            text,
			MentionUsers:    append([]string(nil), inbound.MentionUsers...),
		})
		return nil
	}

	busHandler := func(ctx context.Context, msg busruntime.BusMessage) error {
		switch msg.Direction {
		case busruntime.DirectionInbound:
			if msg.Channel != busruntime.ChannelSlack {
				return fmt.Errorf("unsupported inbound channel: %s", msg.Channel)
			}
			if err := contactsSvc.ObserveInboundBusMessage(context.Background(), msg, time.Now().UTC()); err != nil {
				logger.Warn("contacts_observe_bus_error", "channel", msg.Channel, "idempotency_key", msg.IdempotencyKey, "error", err.Error())
			}
			if enqueueSlackInbound == nil {
				return fmt.Errorf("slack inbound handler is not initialized")
			}
			return enqueueSlackInbound(ctx, msg)
		case busruntime.DirectionOutbound:
			if msg.Channel != busruntime.ChannelSlack {
				return fmt.Errorf("unsupported outbound channel: %s", msg.Channel)
			}
			_, _, err := slackDeliveryAdapter.Deliver(ctx, msg)
			if err != nil {
				callErrorHook(ctx, logger, hooks, ErrorEvent{
					Stage:           ErrorStageDeliverOutbound,
					ConversationKey: msg.ConversationKey,
					Err:             err,
				})
				return err
			}
			event, eventErr := slackOutboundEventFromBusMessage(msg)
			if eventErr != nil {
				callErrorHook(ctx, logger, hooks, ErrorEvent{
					Stage:           ErrorStageDeliverOutbound,
					ConversationKey: msg.ConversationKey,
					Err:             eventErr,
				})
			} else {
				callOutboundHook(ctx, logger, hooks, event)
			}
			return nil
		default:
			return fmt.Errorf("unsupported direction: %s", msg.Direction)
		}
	}
	for _, topic := range busruntime.AllTopics() {
		if err := inprocBus.Subscribe(topic, busHandler); err != nil {
			return err
		}
	}

	appendIgnoredInboundHistory := func(event slackInboundEvent) {
		conversationKey, err := buildSlackConversationKey(event.TeamID, event.ChannelID)
		if err != nil {
			return
		}
		historyScopeKey, err := buildSlackHistoryScopeKey(event.TeamID, event.ChannelID, event.ThreadTS)
		if err != nil {
			return
		}
		mu.Lock()
		cur := history[historyScopeKey]
		cur = append(cur, newSlackInboundHistoryItem(slackJob{
			ConversationKey: conversationKey,
			TeamID:          event.TeamID,
			ChannelID:       event.ChannelID,
			ChatType:        event.ChatType,
			MessageTS:       event.MessageTS,
			ThreadTS:        event.ThreadTS,
			UserID:          event.UserID,
			Username:        event.Username,
			DisplayName:     event.DisplayName,
			Text:            event.Text,
			SentAt:          event.SentAt,
			MentionUsers:    append([]string(nil), event.MentionUsers...),
		}))
		history[historyScopeKey] = trimChatHistoryItems(cur, slackHistoryCapForMode(groupTriggerMode))
		mu.Unlock()
	}

	logger.Info("slack_start",
		"bot_user_id", botUserID,
		"allowed_team_ids", len(allowedTeams),
		"allowed_channel_ids", len(allowedChannels),
		"emoji_catalog_size", len(availableEmojiNames),
		"task_timeout", taskTimeout.String(),
		"max_concurrency", maxConc,
		"group_trigger_mode", groupTriggerMode,
		"addressing_confidence_threshold", addressingConfidenceThreshold,
		"addressing_interject_threshold", addressingInterjectThreshold,
	)

	for {
		if ctx.Err() != nil {
			logger.Info("slack_stop", "reason", "context_canceled")
			return nil
		}
		conn, err := api.connectSocket(ctx)
		if err != nil {
			if ctx.Err() != nil {
				logger.Info("slack_stop", "reason", "context_canceled")
				return nil
			}
			logger.Warn("slack_socket_connect_error", "error", err.Error())
			callErrorHook(ctx, logger, hooks, ErrorEvent{
				Stage: ErrorStageSocketConnect,
				Err:   err,
			})
			if err := sleepWithContext(ctx, 2*time.Second); err != nil {
				return nil
			}
			continue
		}
		logger.Info("slack_socket_connected")
		readErr := consumeSlackSocket(ctx, conn, func(envelope slackSocketEnvelope) error {
			event, ok, err := parseSlackInboundEvent(envelope, botUserID)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			logger.Info("slack_inbound_event",
				"event_type", event.EventType,
				"event_id", event.EventID,
				"team_id", event.TeamID,
				"channel_id", event.ChannelID,
				"chat_type", event.ChatType,
				"user_id", event.UserID,
				"message_ts", event.MessageTS,
				"thread_ts", event.ThreadTS,
				"is_app_mention", event.IsAppMention,
				"is_thread_message", event.IsThreadMessage,
			)
			if len(allowedTeams) > 0 && !allowedTeams[event.TeamID] {
				return nil
			}
			if len(allowedChannels) > 0 && !allowedChannels[event.ChannelID] {
				return nil
			}
			conversationKey, err := buildSlackConversationKey(event.TeamID, event.ChannelID)
			if err != nil {
				return err
			}
			historyScopeKey, err := buildSlackHistoryScopeKey(event.TeamID, event.ChannelID, event.ThreadTS)
			if err != nil {
				return err
			}
			username, displayName, identityErr := resolveSlackUserIdentity(context.Background(), event.TeamID, event.UserID)
			if identityErr != nil {
				logger.Warn("slack_user_identity_enrichment_failed",
					"conversation_key", conversationKey,
					"team_id", event.TeamID,
					"channel_id", event.ChannelID,
					"user_id", event.UserID,
					"error", identityErr.Error(),
				)
				callErrorHook(context.Background(), logger, hooks, ErrorEvent{
					Stage:           ErrorStageIdentityEnrich,
					ConversationKey: conversationKey,
					TeamID:          event.TeamID,
					ChannelID:       event.ChannelID,
					MessageTS:       event.MessageTS,
					Err:             identityErr,
				})
				return nil
			}
			event.Username = username
			event.DisplayName = displayName
			handledCommand, cmdErr := maybeHandleSlackProfileCommand(context.Background(), d, inprocBus, event, botUserID)
			if cmdErr != nil {
				logger.Warn("slack_profile_command_error",
					"conversation_key", conversationKey,
					"team_id", event.TeamID,
					"channel_id", event.ChannelID,
					"message_ts", event.MessageTS,
					"error", cmdErr.Error(),
				)
				callErrorHook(context.Background(), logger, hooks, ErrorEvent{
					Stage:           ErrorStagePublishOutbound,
					ConversationKey: conversationKey,
					TeamID:          event.TeamID,
					ChannelID:       event.ChannelID,
					MessageTS:       event.MessageTS,
					Err:             cmdErr,
				})
				return nil
			}
			if handledCommand {
				return nil
			}

			isGroup := isSlackGroupChat(event.ChatType)
			if isGroup {
				mu.Lock()
				historySnapshot := append([]chathistory.ChatHistoryItem(nil), history[historyScopeKey]...)
				mu.Unlock()
				decisionCtx := llmstats.WithRunID(context.Background(), slackTaskID(event.TeamID, event.ChannelID, event.MessageTS))
				var addressingReactionTool *slacktools.ReactTool
				if api != nil &&
					strings.TrimSpace(event.ChannelID) != "" &&
					strings.TrimSpace(event.MessageTS) != "" {
					addressingReactionTool = slacktools.NewReactTool(newSlackToolAPI(api), event.ChannelID, event.MessageTS, allowedChannels, availableEmojiNames)
				}
				dec, accepted, err := decideSlackGroupTrigger(
					decisionCtx,
					addressingClient,
					addressingModel,
					event,
					botUserID,
					availableEmojiList,
					groupTriggerMode,
					addressingLLMTimeout,
					addressingConfidenceThreshold,
					addressingInterjectThreshold,
					historySnapshot,
					addressingReactionTool,
				)
				if addressingReactionTool != nil {
					if reaction := addressingReactionTool.LastReaction(); reaction != nil {
						logger.Info("slack_group_addressing_reaction_applied",
							"channel_id", reaction.ChannelID,
							"message_ts", reaction.MessageTS,
							"emoji", reaction.Emoji,
							"source", reaction.Source,
						)
					}
				}
				if err != nil {
					logger.Warn("slack_addressing_llm_error", "channel_id", event.ChannelID, "error", err.Error())
					callErrorHook(context.Background(), logger, hooks, ErrorEvent{
						Stage:           ErrorStageGroupTrigger,
						ConversationKey: conversationKey,
						TeamID:          event.TeamID,
						ChannelID:       event.ChannelID,
						MessageTS:       event.MessageTS,
						Err:             err,
					})
					return nil
				}
				if !accepted {
					if strings.EqualFold(groupTriggerMode, "talkative") {
						appendIgnoredInboundHistory(event)
					}
					return nil
				}
				event.ThreadTS = quoteReplyThreadTSForGroupTrigger(event, dec)
			}

			accepted, err := slackInboundAdapter.HandleInboundMessage(context.Background(), slackbus.InboundMessage{
				TeamID:       event.TeamID,
				ChannelID:    event.ChannelID,
				ChatType:     event.ChatType,
				MessageTS:    event.MessageTS,
				ThreadTS:     event.ThreadTS,
				UserID:       event.UserID,
				Username:     event.Username,
				DisplayName:  event.DisplayName,
				Text:         event.Text,
				SentAt:       event.SentAt,
				MentionUsers: append([]string(nil), event.MentionUsers...),
				EventID:      event.EventID,
			})
			if err != nil {
				logger.Warn("slack_bus_publish_error", "channel_id", event.ChannelID, "message_ts", event.MessageTS, "bus_error_code", busErrorCodeString(err), "error", err.Error())
				callErrorHook(context.Background(), logger, hooks, ErrorEvent{
					Stage:           ErrorStagePublishInbound,
					ConversationKey: conversationKey,
					TeamID:          event.TeamID,
					ChannelID:       event.ChannelID,
					MessageTS:       event.MessageTS,
					Err:             err,
				})
				return nil
			}
			if !accepted {
				logger.Debug("slack_bus_inbound_deduped", "channel_id", event.ChannelID, "message_ts", event.MessageTS)
			}
			return nil
		})
		_ = conn.Close()
		if readErr != nil && !errors.Is(readErr, context.Canceled) && !errors.Is(readErr, context.DeadlineExceeded) {
			logger.Warn("slack_socket_read_error", "error", readErr.Error())
			callErrorHook(ctx, logger, hooks, ErrorEvent{
				Stage: ErrorStageSocketRead,
				Err:   readErr,
			})
		}
	}
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
	return daemonruntime.TruncateUTF8(topicID, 120), daemonruntime.TruncateUTF8(title, 72)
}

func recordSlackQueuedTask(store daemonruntime.TaskView, info daemonruntime.TaskInfo, trigger daemonruntime.TaskTrigger, topicTitle string) {
	if store == nil {
		return
	}
	if writer, ok := store.(interface {
		UpsertWithTrigger(daemonruntime.TaskInfo, daemonruntime.TaskTrigger, string) error
	}); ok {
		_ = writer.UpsertWithTrigger(info, trigger, topicTitle)
		return
	}
	_ = daemonruntime.RecordTaskUpsert(store, info, trigger)
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
