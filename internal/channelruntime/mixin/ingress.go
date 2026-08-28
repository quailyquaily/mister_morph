package mixin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	mixinbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/mixin"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imagehistory"
	"github.com/quailyquaily/mistermorph/internal/filecache"
	"github.com/quailyquaily/mistermorph/internal/imagemime"
	"github.com/quailyquaily/mistermorph/internal/mixinapi"
)

const (
	mixinImageMaxBytes = int64(5 << 20)
	mixinFileMaxBytes  = int64(20 << 20)
)

var errMixinAttachmentDownload = errors.New("mixin attachment download failed")

type mixinAPI interface {
	Me(context.Context) (mixinapi.User, error)
	ReadUser(context.Context, string) (mixinapi.User, error)
	ReadConversation(context.Context, string) (mixinapi.Conversation, error)
	SendMessages(context.Context, []mixinapi.MessageRequest) error
	ReadAttachment(context.Context, string) (mixinapi.Attachment, error)
	DownloadAttachment(context.Context, mixinapi.Attachment, int64) ([]byte, string, error)
	CreateAttachment(context.Context) (mixinapi.Attachment, error)
	UploadAttachment(context.Context, mixinapi.Attachment, string, int64, io.Reader) error
	CreateContactConversation(context.Context, string) (mixinapi.Conversation, error)
}

type mixinBlaze interface {
	Run(context.Context, mixinapi.MessageHandler) error
}

type mixinIngress struct {
	api       mixinAPI
	bot       mixinapi.User
	logger    *slog.Logger
	cacheDir  string
	authorize func(context.Context, mixinbus.InboundMessage) (bool, error)

	mu                        sync.Mutex
	users                     map[string]mixinapi.User
	conversations             map[string]mixinapi.Conversation
	onConversationInvalidated func(string)
}

func newMixinIngress(api mixinAPI, bot mixinapi.User, cacheDir string, logger *slog.Logger) *mixinIngress {
	return &mixinIngress{
		api: api, bot: bot, cacheDir: strings.TrimSpace(cacheDir),
		logger: logger,
		users:  make(map[string]mixinapi.User), conversations: make(map[string]mixinapi.Conversation),
	}
}

func (i *mixinIngress) Normalize(ctx context.Context, message mixinapi.MessageView) (mixinbus.InboundMessage, bool, error) {
	conversationID := strings.TrimSpace(message.ConversationID)
	fromUserID := strings.TrimSpace(message.UserID)
	if conversationID == "" || fromUserID == "" || strings.TrimSpace(message.MessageID) == "" {
		return mixinbus.InboundMessage{}, false, nil
	}
	if strings.EqualFold(fromUserID, strings.TrimSpace(i.bot.UserID)) {
		return mixinbus.InboundMessage{}, false, nil
	}
	if strings.EqualFold(strings.TrimSpace(message.Category), mixinapi.MessageCategorySystem) {
		i.mu.Lock()
		delete(i.conversations, conversationID)
		i.mu.Unlock()
		if i.onConversationInvalidated != nil {
			i.onConversationInvalidated(conversationID)
		}
		return mixinbus.InboundMessage{}, false, nil
	}
	text, supported, err := decodeMixinText(message.Category, message.DataBase64)
	if err != nil {
		return mixinbus.InboundMessage{}, false, err
	}
	var attachmentPayload *mixinAttachmentPayload
	if !supported {
		payload, attachmentSupported, attachmentErr := decodeMixinAttachment(message.Category, message.DataBase64)
		if attachmentErr != nil || !attachmentSupported {
			return mixinbus.InboundMessage{}, false, attachmentErr
		}
		attachmentPayload = &payload
	}
	user := i.user(ctx, fromUserID)
	conversation := i.conversation(ctx, conversationID, fromUserID)
	chatType := strings.ToUpper(strings.TrimSpace(conversation.Category))
	if chatType != mixinapi.ConversationCategoryContact && chatType != mixinapi.ConversationCategoryGroup {
		chatType = mixinapi.ConversationCategoryGroup
	}
	conversationName := strings.TrimSpace(conversation.Name)
	if conversationName == "" && chatType == mixinapi.ConversationCategoryContact {
		conversationName = strings.TrimSpace(user.FullName)
	}
	var mentions []string
	if mixinBotMentioned(text, i.bot.IdentityNumber) {
		mentions = []string{strings.TrimSpace(i.bot.UserID)}
		text = stripMixinBotMention(text, i.bot.IdentityNumber)
	}
	inbound := mixinbus.InboundMessage{
		ConversationID:   conversationID,
		MessageID:        strings.TrimSpace(message.MessageID),
		SentAt:           message.CreatedAt.UTC(),
		ChatType:         chatType,
		FromUserID:       fromUserID,
		IdentityNumber:   strings.TrimSpace(user.IdentityNumber),
		DisplayName:      strings.TrimSpace(user.FullName),
		FromIsAgent:      strings.TrimSpace(user.AppID) != "",
		Text:             text,
		QuoteMessageID:   strings.TrimSpace(message.QuoteMessageID),
		MentionUserIDs:   mentions,
		ConversationName: conversationName,
	}
	if i.authorize != nil {
		authorized, authorizeErr := i.authorize(ctx, inbound)
		if authorizeErr != nil || !authorized {
			return mixinbus.InboundMessage{}, false, authorizeErr
		}
	}
	if attachmentPayload != nil {
		path, alias, mimeType, downloadErr := i.downloadAttachment(ctx, message, *attachmentPayload)
		if downloadErr != nil {
			return mixinbus.InboundMessage{}, false, downloadErr
		}
		switch strings.ToUpper(strings.TrimSpace(message.Category)) {
		case mixinapi.MessageCategoryPlainImage:
			inbound.Text = "User sent an image."
			inbound.ImageAttachments = []busruntime.ImageAttachment{{
				Path: path, SourceMessageID: strings.TrimSpace(message.MessageID),
				SourceAttachmentID: attachmentPayload.AttachmentID, MIMEType: mimeType,
			}}
		case mixinapi.MessageCategoryPlainAudio:
			inbound.Text = fmt.Sprintf("User sent an audio file: %s\nLocal path: %s", attachmentDisplayName(*attachmentPayload, "audio"), alias)
		default:
			inbound.Text = fmt.Sprintf("User sent a file: %s\nLocal path: %s", attachmentDisplayName(*attachmentPayload, "file"), alias)
		}
	}
	if inbound.Text == "" {
		return mixinbus.InboundMessage{}, false, nil
	}
	return inbound, true, nil
}

func (i *mixinIngress) downloadAttachment(ctx context.Context, message mixinapi.MessageView, payload mixinAttachmentPayload) (string, string, string, error) {
	attachmentID, err := uuid.Parse(payload.AttachmentID)
	if err != nil || attachmentID == uuid.Nil {
		return "", "", "", fmt.Errorf("mixin attachment_id is invalid")
	}
	maxBytes := mixinFileMaxBytes
	if strings.EqualFold(strings.TrimSpace(message.Category), mixinapi.MessageCategoryPlainImage) {
		maxBytes = mixinImageMaxBytes
	}
	if payload.Size < 0 || payload.Size > maxBytes {
		return "", "", "", fmt.Errorf("mixin attachment size %d exceeds %d", payload.Size, maxBytes)
	}
	if i.api == nil {
		return "", "", "", fmt.Errorf("%w: api unavailable", errMixinAttachmentDownload)
	}
	cacheDir, err := imagehistory.DownloadDir(i.cacheDir, "", string(busruntime.ChannelMixin))
	if err != nil {
		return "", "", "", fmt.Errorf("%w: %v", errMixinAttachmentDownload, err)
	}
	name := attachmentDisplayName(payload, strings.ToLower(strings.TrimPrefix(message.Category, "PLAIN_")))
	filename := "mixin_" + attachmentID.String() + "_" + filecache.SanitizeFilename(name)
	path := filepath.Join(cacheDir, filename)
	alias := filepath.ToSlash(filepath.Join("file_cache_dir", string(busruntime.ChannelMixin), filename))
	if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() && info.Size() <= maxBytes {
		return path, alias, payload.MimeType, nil
	}
	attachment, err := i.api.ReadAttachment(ctx, attachmentID.String())
	if err != nil {
		return "", "", "", classifyMixinAttachmentError(err)
	}
	raw, contentType, err := i.api.DownloadAttachment(ctx, attachment, maxBytes)
	if err != nil {
		return "", "", "", classifyMixinAttachmentError(err)
	}
	tmp, err := os.CreateTemp(cacheDir, filename+".tmp-*")
	if err != nil {
		return "", "", "", fmt.Errorf("%w: %v", errMixinAttachmentDownload, err)
	}
	tmpPath := tmp.Name()
	if _, err = tmp.Write(raw); err == nil {
		err = tmp.Chmod(0o600)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpPath, path)
	}
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", "", "", fmt.Errorf("%w: %v", errMixinAttachmentDownload, err)
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = payload.MimeType
	}
	return path, alias, strings.TrimSpace(contentType), nil
}

func classifyMixinAttachmentError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, mixinapi.ErrRequestTooLarge) {
		return err
	}
	var apiErr *mixinapi.APIError
	if errors.As(err, &apiErr) && (apiErr.HTTPStatus == http.StatusNotFound || apiErr.HTTPStatus == http.StatusGone || apiErr.Status == http.StatusNotFound || apiErr.Status == http.StatusGone) {
		return err
	}
	return fmt.Errorf("%w: %w", errMixinAttachmentDownload, err)
}

func attachmentDisplayName(payload mixinAttachmentPayload, fallback string) string {
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		name = strings.TrimSpace(fallback)
	}
	if name == "" {
		name = "file"
	}
	name = filecache.SanitizeFilename(name)
	if filepath.Ext(name) == "" {
		name += imagemime.Extension(payload.MimeType)
	}
	return name
}

func (i *mixinIngress) user(ctx context.Context, userID string) mixinapi.User {
	i.mu.Lock()
	user, found := i.users[userID]
	i.mu.Unlock()
	if found {
		return user
	}
	user = mixinapi.User{UserID: userID}
	if i.api != nil {
		if fetched, err := i.api.ReadUser(ctx, userID); err == nil {
			user = fetched
			i.mu.Lock()
			i.users[userID] = user
			i.mu.Unlock()
		} else if i.logger != nil {
			i.logger.Warn("mixin_profile_fetch_failed", "user_id", userID, "error", err.Error())
		}
	}
	return user
}

func (i *mixinIngress) conversation(ctx context.Context, conversationID, fromUserID string) mixinapi.Conversation {
	i.mu.Lock()
	conversation, found := i.conversations[conversationID]
	i.mu.Unlock()
	if found {
		return conversation
	}
	conversation = mixinapi.Conversation{ConversationID: conversationID, Category: mixinapi.ConversationCategoryGroup}
	if expected, err := mixinapi.UniqueConversationID(i.bot.UserID, fromUserID); err == nil && expected == conversationID {
		conversation.Category = mixinapi.ConversationCategoryContact
	}
	if i.api != nil {
		if fetched, err := i.api.ReadConversation(ctx, conversationID); err == nil {
			conversation = fetched
		} else if i.logger != nil {
			i.logger.Warn("mixin_profile_fetch_failed", "conversation_id", conversationID, "error", err.Error())
		}
	}
	i.mu.Lock()
	i.conversations[conversationID] = conversation
	i.mu.Unlock()
	return conversation
}
