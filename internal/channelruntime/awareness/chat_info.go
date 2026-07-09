package awareness

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/internal/chatinfo"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/spf13/viper"
)

func resolveChatInfoRuntime(opts runtimeLoopOptions) (*chatinfo.Store, chatinfo.Refresher) {
	store := opts.ChatInfoStore
	if store == nil && strings.TrimSpace(opts.ChatInfoContactsDir) != "" {
		store = chatinfo.NewStore(opts.ChatInfoContactsDir)
	}
	refresher := opts.ChatInfoRefresher
	if refresher == nil {
		refresher = chatinfo.NewFetcher(chatInfoFetcherOptionsFromViper())
	}
	return store, refresher
}

func chatInfoFetcherOptionsFromViper() chatinfo.FetcherOptions {
	return chatinfo.FetcherOptions{
		TelegramBotToken: strings.TrimSpace(viper.GetString("telegram.bot_token")),
		SlackBotToken:    strings.TrimSpace(viper.GetString("slack.bot_token")),
		SlackBaseURL:     strings.TrimSpace(viper.GetString("slack.base_url")),
		LineChannelToken: strings.TrimSpace(viper.GetString("line.channel_access_token")),
		LineBaseURL:      strings.TrimSpace(viper.GetString("line.base_url")),
		LarkAppID:        strings.TrimSpace(viper.GetString("lark.app_id")),
		LarkAppSecret:    strings.TrimSpace(viper.GetString("lark.app_secret")),
		LarkBaseURL:      strings.TrimSpace(viper.GetString("lark.base_url")),
	}
}

func refreshChatInfoOnStart(ctx context.Context, store *chatinfo.Store, refresher chatinfo.Refresher, contactsDir string, logger *slog.Logger) {
	if store == nil || refresher == nil {
		return
	}
	now := time.Now().UTC()
	if err := store.RefreshExpired(ctx, now, refresher); err != nil && logger != nil {
		logger.Warn("chat_profile_refresh_expired_failed", "error", err.Error())
	}
	if !chatInfoStoreEmpty(ctx, store) {
		return
	}
	candidates, err := chatinfo.ActiveContactCandidateIDs(ctx, contactsDir)
	if err != nil {
		if logger != nil {
			logger.Warn("chat_profile_startup_candidates_failed", "error", err.Error())
		}
		return
	}
	for _, chatID := range candidates {
		item, ok, err := store.Get(ctx, now, chatID, refresher)
		if err != nil {
			if logger != nil {
				logger.Warn("chat_profile_startup_fetch_failed", "chat_id", chatID, "error", err.Error())
			}
			continue
		}
		if ok && logger != nil {
			logger.Info(
				"chat_profile_startup_fetch_ok",
				"chat_id", strings.TrimSpace(item.ChatID),
				"platform", strings.TrimSpace(item.Platform),
				"type", strings.TrimSpace(item.Type),
				"has_name", strings.TrimSpace(item.Name) != "",
				"expires_at", item.ExpiresAt,
			)
		}
	}
}

func chatInfoStoreEmpty(ctx context.Context, store *chatinfo.Store) bool {
	items, exists, err := store.Read(ctx)
	return err == nil && (!exists || len(items) == 0)
}

func buildCronNotifyTargetForTask(ctx context.Context, task cronstore.Task, now time.Time, store *chatinfo.Store, refresher chatinfo.Refresher, logger *slog.Logger) map[string]any {
	chatID := strings.TrimSpace(task.ChatID)
	if chatID == "" {
		return nil
	}
	var info *chatinfo.Info
	if store != nil && refresher != nil {
		item, ok, err := store.Get(ctx, now, chatID, refresher)
		if err != nil {
			if logger != nil {
				logger.Warn("chat_profile_lazy_fetch_failed", "chat_id", chatID, "error", err.Error())
			}
		} else if ok {
			info = &item
		}
	}
	return awarenessutil.BuildCronNotifyTarget(strings.TrimSpace(task.Content), chatID, info)
}
