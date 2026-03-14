package lark

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	larkbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/lark"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	runtimeworker "github.com/quailyquaily/mistermorph/internal/channelruntime/worker"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/tools"
)

type larkConversationWorker struct {
	Jobs    chan larkJob
	Version uint64
}

func runLarkLoop(ctx context.Context, d Dependencies, opts runtimeLoopOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(opts.AppID) == "" {
		return fmt.Errorf("missing lark.app_id")
	}
	if strings.TrimSpace(opts.AppSecret) == "" {
		return fmt.Errorf("missing lark.app_secret")
	}

	logger, err := depsutil.LoggerFromCommon(d)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)

	if strings.TrimSpace(opts.VerificationToken) == "" {
		logger.Warn("lark_verification_token_empty", "hint", "set lark.verification_token to validate webhook requests")
	}

	daemonStore := daemonruntime.NewMemoryStore(opts.ServerMaxQueue)
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: opts.BusMaxInFlight,
		Logger:      logger,
		Component:   "lark",
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
	larkInboundAdapter, err := larkbus.NewInboundAdapter(larkbus.InboundAdapterOptions{
		Bus:   inprocBus,
		Store: contactsStore,
	})
	if err != nil {
		return err
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	tokenClient := NewTenantTokenClient(httpClient, strings.TrimSpace(opts.BaseURL), opts.AppID, opts.AppSecret)
	api := newLarkAPI(httpClient, strings.TrimSpace(opts.BaseURL), tokenClient)
	larkDeliveryAdapter, err := larkbus.NewDeliveryAdapter(larkbus.DeliveryAdapterOptions{
		SendText: func(ctx context.Context, target any, text string, opts larkbus.SendTextOptions) error {
			deliverTarget, ok := target.(larkbus.DeliveryTarget)
			if !ok {
				return fmt.Errorf("lark target is invalid")
			}
			return sendLarkText(ctx, api, logger, deliverTarget.ChatID, text, opts.ReplyToMessageID)
		},
	})
	if err != nil {
		return err
	}

	requestTimeout := opts.RequestTimeout
	mainRoute, err := depsutil.ResolveLLMRouteFromCommon(d, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return err
	}
	client, err := depsutil.CreateClient(d.CreateLLMClient, mainRoute)
	if err != nil {
		return err
	}
	addressingRoute, err := depsutil.ResolveLLMRouteFromCommon(d, llmutil.RoutePurposeAddressing)
	if err != nil {
		return err
	}
	addressingClient := client
	if !addressingRoute.SameProfile(mainRoute) {
		addressingClient, err = depsutil.CreateClient(d.CreateLLMClient, addressingRoute)
		if err != nil {
			return err
		}
	}
	planRoute, err := depsutil.ResolveLLMRouteFromCommon(d, llmutil.RoutePurposePlanCreate)
	if err != nil {
		return err
	}
	planClient := client
	if planRoute.SameProfile(mainRoute) {
		planClient = client
	} else if planRoute.SameProfile(addressingRoute) {
		planClient = addressingClient
	} else {
		planClient, err = depsutil.CreateClient(d.CreateLLMClient, planRoute)
		if err != nil {
			return err
		}
	}
	model := strings.TrimSpace(mainRoute.ClientConfig.Model)
	addressingModel := strings.TrimSpace(addressingRoute.ClientConfig.Model)
	planModel := strings.TrimSpace(planRoute.ClientConfig.Model)
	var requestInspector *llminspect.RequestInspector
	if opts.InspectRequest {
		requestInspector, err = llminspect.NewRequestInspector(llminspect.Options{
			Mode:            "lark",
			Task:            "lark",
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
			Mode:            "lark",
			Task:            "lark",
			TimestampFormat: "20060102_150405",
		})
		if err != nil {
			return err
		}
		defer func() { _ = promptInspector.Close() }()
	}
	client = llminspect.WrapClient(client, llminspect.ClientOptions{
		PromptInspector:  promptInspector,
		RequestInspector: requestInspector,
		APIBase:          mainRoute.ClientConfig.Endpoint,
		Model:            model,
	})
	addressingClient = llminspect.WrapClient(addressingClient, llminspect.ClientOptions{
		PromptInspector:  promptInspector,
		RequestInspector: requestInspector,
		APIBase:          addressingRoute.ClientConfig.Endpoint,
		Model:            addressingModel,
	})
	planClient = llminspect.WrapClient(planClient, llminspect.ClientOptions{
		PromptInspector:  promptInspector,
		RequestInspector: requestInspector,
		APIBase:          planRoute.ClientConfig.Endpoint,
		Model:            planModel,
	})
	reg := depsutil.RegistryFromCommon(d)
	if reg == nil {
		reg = tools.NewRegistry()
	}
	logOpts := depsutil.LogOptionsFromCommon(d)
	cfg := opts.AgentLimits.ToConfig()
	sharedGuard := depsutil.GuardFromCommon(d, logger)
	groupTriggerMode := strings.ToLower(strings.TrimSpace(opts.GroupTriggerMode))
	taskRuntimeOpts := runtimeTaskOptions{
		PlanCreateClient: planClient,
		PlanCreateModel:  planModel,
	}
	addressingLLMTimeout := addressingRoute.ClientConfig.RequestTimeout
	if addressingLLMTimeout <= 0 {
		addressingLLMTimeout = requestTimeout
	}
	addressingConfidenceThreshold := opts.AddressingConfidenceThreshold
	addressingInterjectThreshold := opts.AddressingInterjectThreshold

	taskTimeout := opts.TaskTimeout
	maxConcurrency := opts.MaxConcurrency
	sem := make(chan struct{}, maxConcurrency)
	workersCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	allowedChats := toAllowlist(opts.AllowedChatIDs)

	serverListen := strings.TrimSpace(opts.ServerListen)
	if serverListen != "" {
		if strings.TrimSpace(opts.ServerAuthToken) == "" {
			logger.Warn("lark_daemon_server_auth_empty", "hint", "set server.auth_token so console can read /tasks")
		}
		_, err := daemonruntime.StartServer(ctx, logger, daemonruntime.ServerOptions{
			Listen: serverListen,
			Routes: daemonruntime.RoutesOptions{
				Mode:       "lark",
				AuthToken:  strings.TrimSpace(opts.ServerAuthToken),
				TaskReader: daemonStore,
				Overview: func(ctx context.Context) (map[string]any, error) {
					return map[string]any{
						"llm": map[string]any{
							"provider": strings.TrimSpace(mainRoute.ClientConfig.Provider),
							"model":    model,
						},
						"channel": map[string]any{
							"configured":       true,
							"telegram_running": false,
							"slack_running":    false,
							"line_running":     false,
							"lark_running":     true,
							"running":          "lark",
						},
					}, nil
				},
				HealthEnabled: true,
			},
		})
		if err != nil {
			logger.Warn("lark_daemon_server_start_error", "addr", serverListen, "error", err.Error())
		}
	}

	var (
		mu                 sync.Mutex
		history            = make(map[string][]chathistory.ChatHistoryItem)
		stickySkillsByConv = make(map[string][]string)
		workers            = make(map[string]*larkConversationWorker)
		enqueueLarkInbound func(context.Context, busruntime.BusMessage) error
	)
	getOrStartWorkerLocked := func(conversationKey string) *larkConversationWorker {
		if w, ok := workers[conversationKey]; ok && w != nil {
			return w
		}
		w := &larkConversationWorker{Jobs: make(chan larkJob, 16)}
		workers[conversationKey] = w
		runtimeworker.Start(runtimeworker.StartOptions[larkJob]{
			Ctx:  workersCtx,
			Sem:  sem,
			Jobs: w.Jobs,
			Handle: func(workerCtx context.Context, job larkJob) {
				mu.Lock()
				h := append([]chathistory.ChatHistoryItem(nil), history[conversationKey]...)
				curVersion := w.Version
				sticky := append([]string(nil), stickySkillsByConv[conversationKey]...)
				mu.Unlock()
				if job.Version != curVersion {
					h = nil
				}
				if daemonStore != nil && strings.TrimSpace(job.TaskID) != "" {
					startedAt := time.Now().UTC()
					daemonStore.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
						info.Status = daemonruntime.TaskRunning
						info.StartedAt = &startedAt
					})
				}
				runCtx, cancel := context.WithTimeout(workerCtx, taskTimeout)
				final, _, loadedSkills, runErr := runLarkTask(
					runCtx,
					d,
					logger,
					logOpts,
					client,
					reg,
					sharedGuard,
					cfg,
					model,
					job,
					h,
					sticky,
					taskRuntimeOpts,
				)
				cancel()
				if runErr != nil {
					if workerCtx.Err() != nil {
						return
					}
					displayErr := depsutil.FormatRuntimeError(runErr)
					if daemonStore != nil && strings.TrimSpace(job.TaskID) != "" {
						finishedAt := time.Now().UTC()
						daemonStore.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
							info.Status = daemonruntime.TaskFailed
							info.Error = displayErr
							info.FinishedAt = &finishedAt
						})
					}
					logger.Warn("lark_task_error",
						"chat_id", job.ChatID,
						"message_id", job.MessageID,
						"error", displayErr,
					)
					errorText := "error: " + displayErr
					errorCorrelationID := fmt.Sprintf("lark:error:%s:%s", job.ChatID, job.MessageID)
					_, err := publishLarkBusOutbound(workerCtx, inprocBus, job.ChatID, errorText, job.MessageID, errorCorrelationID)
					if err != nil {
						logger.Warn("lark_bus_publish_error",
							"channel", busruntime.ChannelLark,
							"chat_id", job.ChatID,
							"bus_error_code", string(busruntime.ErrorCodeOf(err)),
							"error", err.Error(),
						)
					}
					return
				}
				outText := ""
				if shouldPublishLarkText(final) {
					outText = strings.TrimSpace(depsutil.FormatFinalOutput(final))
				}
				if daemonStore != nil && strings.TrimSpace(job.TaskID) != "" {
					finishedAt := time.Now().UTC()
					daemonStore.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
						info.Status = daemonruntime.TaskDone
						info.Error = ""
						info.FinishedAt = &finishedAt
						info.Result = map[string]any{"output": daemonruntime.TruncateUTF8(outText, 4000)}
					})
				}
				if outText != "" {
					if workerCtx.Err() != nil {
						return
					}
					outCorrelationID := fmt.Sprintf("lark:message:%s:%s", job.ChatID, job.MessageID)
					_, err := publishLarkBusOutbound(workerCtx, inprocBus, job.ChatID, outText, job.MessageID, outCorrelationID)
					if err != nil {
						logger.Warn("lark_bus_publish_error",
							"channel", busruntime.ChannelLark,
							"chat_id", job.ChatID,
							"bus_error_code", string(busruntime.ErrorCodeOf(err)),
							"error", err.Error(),
						)
					}
				}
				mu.Lock()
				if w.Version != curVersion {
					history[conversationKey] = nil
					stickySkillsByConv[conversationKey] = nil
				}
				if w.Version == curVersion && len(loadedSkills) > 0 {
					stickySkillsByConv[conversationKey] = capUniqueStrings(loadedSkills, larkStickySkillsCap)
				}
				cur := history[conversationKey]
				cur = append(cur, newLarkInboundHistoryItem(job))
				if outText != "" {
					cur = append(cur, newLarkOutboundAgentHistoryItem(job, outText, time.Now().UTC()))
				}
				history[conversationKey] = trimChatHistoryItems(cur, larkHistoryCapForMode(groupTriggerMode))
				mu.Unlock()
			},
		})
		return w
	}

	enqueueLarkInbound = func(ctx context.Context, msg busruntime.BusMessage) error {
		if ctx == nil {
			ctx = workersCtx
		}
		inbound, err := larkbus.InboundMessageFromBusMessage(msg)
		if err != nil {
			return err
		}
		text := strings.TrimSpace(inbound.Text)
		if text == "" {
			return fmt.Errorf("lark inbound text is required")
		}
		if strings.EqualFold(strings.TrimSpace(inbound.ChatType), "group") {
			mu.Lock()
			historySnapshot := append([]chathistory.ChatHistoryItem(nil), history[msg.ConversationKey]...)
			mu.Unlock()
			decisionCtx := llmstats.WithMetadata(context.Background(), larkTaskID(inbound.ChatID, inbound.MessageID), inbound.EventID)
			dec, accepted, decErr := decideLarkGroupTrigger(
				decisionCtx,
				addressingClient,
				addressingModel,
				inbound,
				groupTriggerMode,
				addressingLLMTimeout,
				addressingConfidenceThreshold,
				addressingInterjectThreshold,
				historySnapshot,
			)
			if decErr != nil {
				logger.Warn("lark_addressing_llm_error", "chat_id", inbound.ChatID, "error", decErr.Error())
				return nil
			}
			if !accepted {
				logger.Info("lark_group_ignored",
					"chat_id", inbound.ChatID,
					"text_len", len(text),
					"llm_attempted", dec.AddressingLLMAttempted,
					"llm_ok", dec.AddressingLLMOK,
					"llm_addressed", dec.Addressing.Addressed,
					"confidence", dec.Addressing.Confidence,
					"wanna_interject", dec.Addressing.WannaInterject,
					"interject", dec.Addressing.Interject,
					"impulse", dec.Addressing.Impulse,
					"is_lightweight", dec.Addressing.IsLightweight,
					"reason", dec.Reason,
				)
				if strings.EqualFold(groupTriggerMode, "talkative") {
					mu.Lock()
					cur := history[msg.ConversationKey]
					cur = append(cur, newLarkInboundHistoryItemFromInbound(inbound))
					history[msg.ConversationKey] = trimChatHistoryItems(cur, larkHistoryCapForMode(groupTriggerMode))
					mu.Unlock()
				}
				return nil
			}
			logger.Info("lark_group_trigger",
				"chat_id", inbound.ChatID,
				"reason", dec.Reason,
				"llm_addressed", dec.Addressing.Addressed,
				"confidence", dec.Addressing.Confidence,
				"wanna_interject", dec.Addressing.WannaInterject,
				"interject", dec.Addressing.Interject,
				"impulse", dec.Addressing.Impulse,
				"is_lightweight", dec.Addressing.IsLightweight,
			)
		}

		mu.Lock()
		w := getOrStartWorkerLocked(msg.ConversationKey)
		version := w.Version
		mu.Unlock()

		job := larkJob{
			TaskID:          larkTaskID(inbound.ChatID, inbound.MessageID),
			ConversationKey: msg.ConversationKey,
			ChatID:          inbound.ChatID,
			ChatType:        inbound.ChatType,
			MessageID:       inbound.MessageID,
			FromUserID:      inbound.FromUserID,
			DisplayName:     inbound.DisplayName,
			Text:            text,
			SentAt:          inbound.SentAt,
			Version:         version,
			MentionUsers:    append([]string(nil), inbound.MentionUsers...),
			EventID:         inbound.EventID,
		}
		if err := runtimeworker.Enqueue(ctx, workersCtx, w.Jobs, job); err != nil {
			return err
		}
		if daemonStore != nil {
			createdAt := inbound.SentAt.UTC()
			if createdAt.IsZero() {
				createdAt = time.Now().UTC()
			}
			daemonStore.Upsert(daemonruntime.TaskInfo{
				ID:        job.TaskID,
				Status:    daemonruntime.TaskQueued,
				Task:      daemonruntime.TruncateUTF8(text, 2000),
				Model:     model,
				Timeout:   taskTimeout.String(),
				CreatedAt: createdAt,
				Result: map[string]any{
					"source":            "lark",
					"lark_chat_id":      inbound.ChatID,
					"lark_message_id":   inbound.MessageID,
					"lark_chat_type":    inbound.ChatType,
					"lark_from_open_id": inbound.FromUserID,
				},
			})
		}
		logger.Info("lark_task_enqueued",
			"channel", msg.Channel,
			"topic", msg.Topic,
			"chat_id", inbound.ChatID,
			"chat_type", inbound.ChatType,
			"idempotency_key", msg.IdempotencyKey,
			"conversation_key", msg.ConversationKey,
			"text_len", len(text),
		)
		return nil
	}

	busHandler := func(ctx context.Context, msg busruntime.BusMessage) error {
		switch msg.Direction {
		case busruntime.DirectionInbound:
			if msg.Channel != busruntime.ChannelLark {
				return fmt.Errorf("unsupported inbound channel: %s", msg.Channel)
			}
			if err := contactsSvc.ObserveInboundBusMessage(context.Background(), msg, time.Now().UTC()); err != nil {
				logger.Warn("contacts_observe_bus_error", "channel", msg.Channel, "idempotency_key", msg.IdempotencyKey, "error", err.Error())
			}
			if enqueueLarkInbound == nil {
				return fmt.Errorf("lark inbound handler is not initialized")
			}
			return enqueueLarkInbound(ctx, msg)
		case busruntime.DirectionOutbound:
			if msg.Channel != busruntime.ChannelLark {
				return fmt.Errorf("unsupported outbound channel: %s", msg.Channel)
			}
			if larkDeliveryAdapter == nil {
				return fmt.Errorf("lark delivery adapter is not initialized")
			}
			_, _, err := larkDeliveryAdapter.Deliver(ctx, msg)
			return err
		default:
			return fmt.Errorf("unsupported direction: %s", msg.Direction)
		}
	}
	for _, topic := range busruntime.AllTopics() {
		if err := inprocBus.Subscribe(topic, busHandler); err != nil {
			return err
		}
	}

	webhookPath := normalizeWebhookPath(opts.WebhookPath)
	webhookMux := http.NewServeMux()
	webhookMux.Handle(webhookPath, newLarkWebhookHandler(larkWebhookHandlerOptions{
		VerificationToken: strings.TrimSpace(opts.VerificationToken),
		EncryptKey:        strings.TrimSpace(opts.EncryptKey),
		Inbound:           larkInboundAdapter,
		AllowedChats:      allowedChats,
		Logger:            logger,
	}))
	webhookServer := &http.Server{
		Addr:              strings.TrimSpace(opts.WebhookListen),
		Handler:           webhookMux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	webhookErrCh := make(chan error, 1)
	go func() {
		err := webhookServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			webhookErrCh <- err
			return
		}
		webhookErrCh <- nil
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = webhookServer.Shutdown(shutdownCtx)
	}()

	logger.Info("lark_start",
		"base_url", strings.TrimSpace(opts.BaseURL),
		"webhook_listen", strings.TrimSpace(opts.WebhookListen),
		"webhook_path", webhookPath,
		"allowed_chat_ids", len(allowedChats),
		"task_timeout", taskTimeout.String(),
		"max_concurrency", maxConcurrency,
		"group_trigger_mode", strings.TrimSpace(opts.GroupTriggerMode),
		"addressing_confidence_threshold", opts.AddressingConfidenceThreshold,
		"addressing_interject_threshold", opts.AddressingInterjectThreshold,
		"encrypt_enabled", strings.TrimSpace(opts.EncryptKey) != "",
	)

	select {
	case err := <-webhookErrCh:
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		logger.Info("lark_stop", "reason", "context_canceled")
		return nil
	}
}

func toAllowlist(items []string) map[string]bool {
	out := make(map[string]bool)
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		out[item] = true
	}
	return out
}

func larkTaskID(chatID, messageID string) string {
	return daemonruntime.BuildTaskID("lk", chatID, messageID)
}

func sendLarkText(ctx context.Context, api *larkAPI, logger *slog.Logger, chatID, text, replyToMessageID string) error {
	if api == nil {
		return fmt.Errorf("lark api is not initialized")
	}
	chatID = strings.TrimSpace(chatID)
	text = strings.TrimSpace(text)
	replyToMessageID = strings.TrimSpace(replyToMessageID)
	if replyToMessageID != "" {
		if err := api.replyText(ctx, replyToMessageID, text); err == nil {
			return nil
		} else if logger != nil {
			logger.Warn("lark_reply_fallback_to_send", "chat_id", chatID, "reply_to_message_id", replyToMessageID, "error", err.Error())
		}
	}
	return api.sendText(ctx, "chat_id", chatID, text)
}
