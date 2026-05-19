package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Reaction struct {
	MessageID string
	EmojiType string
	Source    string
}

type ReactTool struct {
	api              API
	defaultMessageID string
	allowedTypes     map[string]bool
	lastReaction     *Reaction
}

var larkReactionEmojiTypes = []string{
	"THUMBSUP", "SMILE", "OK", "HEART", "LOVE", "THANKS", "YEAH", "AWESOME", "PARTY", "CLAP", "APPLAUSE",
	"CRY", "ANGRY", "SHY", "BLUSH", "SPEECHLESS", "TERROR", "WOW", "FACEPALM", "SWEAT", "PROUD", "OBSESSED",
	"WAVE", "HUG", "KISS", "WINK", "TONGUE", "MUSCLE", "SALUTE", "FIRE", "BEER", "CAKE", "GIFT", "ROSE",
	"FIREWORKS", "WITTY", "JIAYI",
}

var larkReactionAliases = map[string]string{
	"👍":  "THUMBSUP",
	"😊":  "SMILE",
	"🙂":  "SMILE",
	"👌":  "OK",
	"❤":  "HEART",
	"❤️": "HEART",
	"💗":  "LOVE",
	"🙏":  "THANKS",
	"🎉":  "PARTY",
	"👏":  "CLAP",
	"😢":  "CRY",
	"😭":  "CRY",
	"😡":  "ANGRY",
	"😳":  "BLUSH",
	"😱":  "TERROR",
	"😮":  "WOW",
	"🤦":  "FACEPALM",
	"😅":  "SWEAT",
	"👋":  "WAVE",
	"🤗":  "HUG",
	"😘":  "KISS",
	"😉":  "WINK",
	"😛":  "TONGUE",
	"💪":  "MUSCLE",
	"🫡":  "SALUTE",
	"🔥":  "FIRE",
	"🍺":  "BEER",
	"🎂":  "CAKE",
	"🎁":  "GIFT",
	"🌹":  "ROSE",
	"🎆":  "FIREWORKS",
}

func StandardReactionEmojiTypes() []string {
	out := make([]string, len(larkReactionEmojiTypes))
	copy(out, larkReactionEmojiTypes)
	return out
}

func NewReactTool(api API, defaultMessageID string) *ReactTool {
	allowed := make(map[string]bool, len(larkReactionEmojiTypes))
	for _, emojiType := range larkReactionEmojiTypes {
		allowed[strings.ToUpper(strings.TrimSpace(emojiType))] = true
	}
	return &ReactTool{
		api:              api,
		defaultMessageID: strings.TrimSpace(defaultMessageID),
		allowedTypes:     allowed,
	}
}

func (t *ReactTool) Name() string { return "message_react" }

func (t *ReactTool) Description() string {
	return "Adds a Lark emoji reaction to the triggering message. Use when a lightweight acknowledgement is enough; do not also send a text reply when reaction alone is enough."
}

func (t *ReactTool) ParameterSchema() string {
	typesDescription := "Lark emoji_type. Supported values: " + strings.Join(larkReactionEmojiTypes, ",") + "."
	s := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"message_id": map[string]any{
				"type":        "string",
				"description": "Target Lark open_message_id. Optional in active chat context; defaults to the triggering message.",
			},
			"emoji": map[string]any{
				"type":        "string",
				"description": "Unicode emoji alias or Lark emoji_type, for example 👍 or THUMBSUP.",
			},
			"emoji_type": map[string]any{
				"type":        "string",
				"description": typesDescription,
			},
		},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *ReactTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || t.api == nil {
		return "", fmt.Errorf("message_react is disabled")
	}
	messageID := t.defaultMessageID
	if v, ok := params["message_id"].(string); ok && strings.TrimSpace(v) != "" {
		messageID = strings.TrimSpace(v)
	}
	if strings.TrimSpace(messageID) == "" {
		return "", fmt.Errorf("missing required param: message_id")
	}

	rawEmojiType, _ := params["emoji_type"].(string)
	rawEmoji, _ := params["emoji"].(string)
	emojiType, err := t.normalizeEmojiType(rawEmojiType, rawEmoji)
	if err != nil {
		return "", err
	}
	if err := t.api.SetEmojiReaction(ctx, messageID, emojiType); err != nil {
		return "", err
	}
	t.lastReaction = &Reaction{
		MessageID: messageID,
		EmojiType: emojiType,
		Source:    "tool",
	}
	return fmt.Sprintf("reacted with %s", emojiType), nil
}

func (t *ReactTool) normalizeEmojiType(rawEmojiType string, rawEmoji string) (string, error) {
	candidates := []string{rawEmojiType, rawEmoji}
	for _, raw := range candidates {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if alias := larkReactionAliases[item]; alias != "" {
			return alias, nil
		}
		item = strings.ToUpper(item)
		if t.allowedTypes[item] {
			return item, nil
		}
	}
	return "", fmt.Errorf("missing or unsupported Lark reaction emoji; use a supported emoji_type such as THUMBSUP, SMILE, OK, HEART, FIRE, PARTY")
}

func (t *ReactTool) LastReaction() *Reaction {
	if t == nil {
		return nil
	}
	return t.lastReaction
}
