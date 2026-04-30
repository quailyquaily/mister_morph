package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type slackSocketEnvelope struct {
	EnvelopeID string          `json:"envelope_id,omitempty"`
	Type       string          `json:"type,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type slackEventAuthorization struct {
	TeamID string `json:"team_id,omitempty"`
	UserID string `json:"user_id,omitempty"`
	IsBot  bool   `json:"is_bot,omitempty"`
}

type slackEventsAPIPayload struct {
	TeamID         string                    `json:"team_id,omitempty"`
	EventID        string                    `json:"event_id,omitempty"`
	EventTime      int64                     `json:"event_time,omitempty"`
	Event          json.RawMessage           `json:"event,omitempty"`
	Authorizations []slackEventAuthorization `json:"authorizations,omitempty"`
}

type slackEvent struct {
	Type        string           `json:"type,omitempty"`
	Subtype     string           `json:"subtype,omitempty"`
	User        string           `json:"user,omitempty"`
	Text        string           `json:"text,omitempty"`
	Channel     string           `json:"channel,omitempty"`
	ChannelType string           `json:"channel_type,omitempty"`
	TS          string           `json:"ts,omitempty"`
	ThreadTS    string           `json:"thread_ts,omitempty"`
	BotID       string           `json:"bot_id,omitempty"`
	Team        string           `json:"team,omitempty"`
	EventTS     string           `json:"event_ts,omitempty"`
	Files       []slackEventFile `json:"files,omitempty"`
}

type slackEventFile struct {
	ID                 string `json:"id,omitempty"`
	Name               string `json:"name,omitempty"`
	Title              string `json:"title,omitempty"`
	Mode               string `json:"mode,omitempty"`
	FileAccess         string `json:"file_access,omitempty"`
	Mimetype           string `json:"mimetype,omitempty"`
	Filetype           string `json:"filetype,omitempty"`
	URLPrivate         string `json:"url_private,omitempty"`
	URLPrivateDownload string `json:"url_private_download,omitempty"`
	Size               int64  `json:"size,omitempty"`
}

type slackInboundEvent struct {
	EventType       string
	EventSubtype    string
	TeamID          string
	ChannelID       string
	ChatType        string
	MessageTS       string
	ThreadTS        string
	UserID          string
	Username        string
	DisplayName     string
	Text            string
	EventID         string
	SentAt          time.Time
	MentionUsers    []string
	ImageFiles      []slackEventFile
	ImagePaths      []string
	IsAppMention    bool
	IsThreadMessage bool
}

var slackMentionPattern = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|[^>]+)?>`)

func consumeSlackSocket(ctx context.Context, conn *websocket.Conn, onEnvelope func(envelope slackSocketEnvelope) error) error {
	if conn == nil {
		return fmt.Errorf("slack websocket connection is nil")
	}
	for {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var envelope slackSocketEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			continue
		}
		if strings.TrimSpace(envelope.EnvelopeID) != "" {
			if err := conn.WriteJSON(map[string]string{"envelope_id": envelope.EnvelopeID}); err != nil {
				return err
			}
		}
		if onEnvelope == nil {
			continue
		}
		if err := onEnvelope(envelope); err != nil {
			return err
		}
	}
}

func parseSlackInboundEvent(envelope slackSocketEnvelope, botUserID string) (slackInboundEvent, bool, error) {
	if strings.TrimSpace(envelope.Type) != "events_api" || len(envelope.Payload) == 0 {
		return slackInboundEvent{}, false, nil
	}
	var payload slackEventsAPIPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return slackInboundEvent{}, false, err
	}
	var event slackEvent
	if err := json.Unmarshal(payload.Event, &event); err != nil {
		return slackInboundEvent{}, false, err
	}
	eventType := strings.TrimSpace(event.Type)
	if eventType != "message" && eventType != "app_mention" {
		return slackInboundEvent{}, false, nil
	}
	subtype := strings.TrimSpace(event.Subtype)
	text := strings.TrimSpace(event.Text)
	imageFiles := slackImageFilesFromEvent(event.Files)
	if !acceptSlackMessageSubtype(subtype, imageFiles) {
		return slackInboundEvent{}, false, nil
	}
	if strings.TrimSpace(event.BotID) != "" {
		return slackInboundEvent{}, false, nil
	}
	userID := strings.TrimSpace(event.User)
	if userID == "" {
		return slackInboundEvent{}, false, nil
	}
	if userID == strings.TrimSpace(botUserID) {
		return slackInboundEvent{}, false, nil
	}
	channelID := strings.TrimSpace(event.Channel)
	if channelID == "" {
		return slackInboundEvent{}, false, nil
	}
	messageTS := strings.TrimSpace(event.TS)
	if messageTS == "" {
		return slackInboundEvent{}, false, nil
	}
	if text == "" && len(imageFiles) == 0 {
		return slackInboundEvent{}, false, nil
	}
	teamID := strings.TrimSpace(payload.TeamID)
	if teamID == "" {
		teamID = strings.TrimSpace(event.Team)
	}
	if teamID == "" && len(payload.Authorizations) > 0 {
		teamID = strings.TrimSpace(payload.Authorizations[0].TeamID)
	}
	if teamID == "" {
		return slackInboundEvent{}, false, fmt.Errorf("missing team_id in slack event")
	}
	chatType := normalizeSlackChatType(event.ChannelType, channelID)
	isAppMention := eventType == "app_mention"

	eventTime := payload.EventTime
	sentAt := time.Now().UTC()
	if eventTime > 0 {
		sentAt = time.Unix(eventTime, 0).UTC()
	}

	return slackInboundEvent{
		EventType:       eventType,
		EventSubtype:    subtype,
		TeamID:          teamID,
		ChannelID:       channelID,
		ChatType:        chatType,
		MessageTS:       messageTS,
		ThreadTS:        strings.TrimSpace(event.ThreadTS),
		UserID:          userID,
		Text:            text,
		EventID:         strings.TrimSpace(payload.EventID),
		SentAt:          sentAt,
		MentionUsers:    collectSlackMentionUsers(text),
		ImageFiles:      imageFiles,
		IsAppMention:    isAppMention,
		IsThreadMessage: strings.TrimSpace(event.ThreadTS) != "",
	}, true, nil
}

func acceptSlackMessageSubtype(subtype string, imageFiles []slackEventFile) bool {
	subtype = strings.TrimSpace(subtype)
	if subtype == "" {
		return true
	}
	return subtype == "file_share" && len(imageFiles) > 0
}

func slackImageFilesFromEvent(files []slackEventFile) []slackEventFile {
	if len(files) == 0 {
		return nil
	}
	out := make([]slackEventFile, 0, len(files))
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		id := strings.TrimSpace(file.ID)
		url := slackFileDownloadURL(file)
		mimeType := slackFileMIMEType(file)
		if !slackFileNeedsInfo(file) && (url == "" || !strings.HasPrefix(mimeType, "image/")) {
			continue
		}
		key := id
		if key == "" {
			key = url
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, file)
	}
	return out
}

func slackFileNeedsInfo(file slackEventFile) bool {
	if strings.TrimSpace(file.ID) == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(file.FileAccess), "check_file_info") ||
		strings.EqualFold(strings.TrimSpace(file.Mode), "file_access")
}

func slackFileDownloadURL(file slackEventFile) string {
	if url := strings.TrimSpace(file.URLPrivateDownload); url != "" {
		return url
	}
	return strings.TrimSpace(file.URLPrivate)
}

func slackFileMIMEType(file slackEventFile) string {
	mimeType := strings.TrimSpace(strings.ToLower(file.Mimetype))
	if idx := strings.Index(mimeType, ";"); idx >= 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}
	if mimeType != "" {
		return mimeType
	}
	switch strings.ToLower(strings.TrimSpace(file.Filetype)) {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	}
	for _, name := range []string{file.Name, file.Title, slackFileDownloadURL(file)} {
		mimeType = slackImageMIMEFromName(name)
		if mimeType != "" {
			return mimeType
		}
	}
	return ""
}

func slackImageMIMEFromName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasSuffix(name, ".jpg"), strings.HasSuffix(name, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".webp"):
		return "image/webp"
	case strings.HasSuffix(name, ".gif"):
		return "image/gif"
	default:
		return ""
	}
}

func isSlackGroupChat(chatType string) bool {
	switch strings.ToLower(strings.TrimSpace(chatType)) {
	case "channel", "private_channel", "mpim":
		return true
	default:
		return false
	}
}

func normalizeSlackChatType(channelType, channelID string) string {
	channelType = strings.ToLower(strings.TrimSpace(channelType))
	switch channelType {
	case "im", "mpim", "channel", "private_channel":
		return channelType
	}
	switch {
	case strings.HasPrefix(channelID, "D"):
		return "im"
	case strings.HasPrefix(channelID, "C"):
		return "channel"
	case strings.HasPrefix(channelID, "G"):
		return "private_channel"
	default:
		return "channel"
	}
}

func collectSlackMentionUsers(text string) []string {
	matches := slackMentionPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		userID := strings.TrimSpace(match[1])
		if userID == "" || seen[userID] {
			continue
		}
		seen[userID] = true
		out = append(out, userID)
	}
	return out
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
