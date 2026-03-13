package slack

import "context"

// API is the minimal Slack transport surface needed by Slack tools.
type API interface {
	AddReaction(ctx context.Context, channelID, messageTS, emoji string) error
	SendFile(ctx context.Context, channelID, threadTS, filePath, filename, title, initialComment string) error
}

type Reaction struct {
	ChannelID string
	MessageTS string
	Emoji     string
	Source    string
}
