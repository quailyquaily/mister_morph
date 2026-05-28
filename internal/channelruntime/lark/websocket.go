package lark

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	larkbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/lark"
)

type larkWebSocketClient interface {
	Start(context.Context) error
	Close()
}

type larkWebSocketIngressOptions struct {
	AppID            string
	AppSecret        string
	Domain           string
	Inbound          *larkbus.InboundAdapter
	AllowedChats     map[string]bool
	ImageRecognition bool
	Logger           *slog.Logger
	Client           larkWebSocketClient
}

func runLarkWebSocketIngress(ctx context.Context, opts larkWebSocketIngressOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Inbound == nil {
		return fmt.Errorf("lark inbound adapter is not initialized")
	}
	client := opts.Client
	if client == nil {
		client = newLarkSDKWebSocketClient(opts)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Start(ctx)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("lark websocket start: %w", err)
		}
		return nil
	case <-ctx.Done():
		client.Close()
		return nil
	}
}

func newLarkSDKWebSocketClient(opts larkWebSocketIngressOptions) larkWebSocketClient {
	logger := opts.Logger
	eventHandler := newLarkWebSocketEventDispatcher(opts)

	domain := strings.TrimSpace(opts.Domain)
	if domain == "" {
		domain = larkWebSocketDomainFromBaseURL(defaultLarkBaseURL)
	}
	return larkws.NewClient(
		strings.TrimSpace(opts.AppID),
		strings.TrimSpace(opts.AppSecret),
		larkws.WithDomain(domain),
		larkws.WithEventHandler(eventHandler),
		larkws.WithOnReady(func() {
			if logger != nil {
				logger.Info("lark_websocket_ready", "domain", domain)
			}
		}),
		larkws.WithOnError(func(err error) {
			if logger != nil && err != nil {
				logger.Warn("lark_websocket_error", "domain", domain, "error", err.Error())
			}
		}),
		larkws.WithOnReconnecting(func() {
			if logger != nil {
				logger.Warn("lark_websocket_reconnecting", "domain", domain)
			}
		}),
		larkws.WithOnReconnected(func() {
			if logger != nil {
				logger.Info("lark_websocket_reconnected", "domain", domain)
			}
		}),
		larkws.WithOnDisconnected(func() {
			if logger != nil {
				logger.Warn("lark_websocket_disconnected", "domain", domain)
			}
		}),
	)
}

func newLarkWebSocketEventDispatcher(opts larkWebSocketIngressOptions) *dispatcher.EventDispatcher {
	logger := opts.Logger
	return dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(eventCtx context.Context, event *larkim.P2MessageReceiveV1) error {
			inbound, ok, err := inboundMessageFromSDKEvent(event, opts.AllowedChats)
			if err != nil {
				logLarkWebSocketWarn(logger, "lark_websocket_event_invalid",
					"event_id", larkSDKEventID(event),
					"error", err.Error(),
				)
				return nil
			}
			if !ok {
				return nil
			}
			if len(inbound.ImageKeys) > 0 {
				inbound.Text = larkImageFallbackText(inbound.Text, opts.ImageRecognition, len(inbound.ImageKeys))
				if !opts.ImageRecognition {
					inbound.ImageKeys = nil
				}
			}
			accepted, publishErr := opts.Inbound.HandleInboundMessage(eventCtx, inbound)
			if publishErr != nil {
				logLarkWebSocketWarn(logger, "lark_websocket_publish_error",
					"event_id", larkSDKEventID(event),
					"chat_id", strings.TrimSpace(inbound.ChatID),
					"message_id", strings.TrimSpace(inbound.MessageID),
					"error", publishErr.Error(),
				)
			} else if !accepted {
				logLarkWebSocketDebug(logger, "lark_websocket_inbound_deduped",
					"chat_id", strings.TrimSpace(inbound.ChatID),
					"message_id", strings.TrimSpace(inbound.MessageID),
				)
			}
			return nil
		}).
		OnP2MessageReactionCreatedV1(func(context.Context, *larkim.P2MessageReactionCreatedV1) error {
			return nil
		}).
		OnP2MessageReactionDeletedV1(func(context.Context, *larkim.P2MessageReactionDeletedV1) error {
			return nil
		})
}

func larkWebSocketDomainFromBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "https://open.feishu.cn"
	}
	u, err := url.Parse(baseURL)
	if err == nil && u.Host != "" {
		scheme := strings.TrimSpace(u.Scheme)
		if scheme == "" {
			scheme = "https"
		}
		return scheme + "://" + u.Host
	}
	baseURL = strings.TrimRight(baseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/open-apis")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return "https://" + baseURL
	}
	return baseURL
}

func logLarkWebSocketWarn(logger *slog.Logger, msg string, args ...any) {
	if logger == nil {
		return
	}
	logger.Warn(msg, args...)
}

func logLarkWebSocketDebug(logger *slog.Logger, msg string, args ...any) {
	if logger == nil {
		return
	}
	logger.Debug(msg, args...)
}
