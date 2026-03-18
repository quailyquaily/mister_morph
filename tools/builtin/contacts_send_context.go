package builtin

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	refid "github.com/quailyquaily/mistermorph/internal/entryutil/refid"
)

type ContactsSendRuntimeContext struct {
	ForbiddenTargetIDs []string
}

type contactsSendRuntimeContextKey struct{}

func WithContactsSendRuntimeContext(ctx context.Context, runtime ContactsSendRuntimeContext) context.Context {
	runtime = normalizeContactsSendRuntimeContext(runtime)
	if len(runtime.ForbiddenTargetIDs) == 0 {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contactsSendRuntimeContextKey{}, runtime)
}

func ContactsSendRuntimeContextFromContext(ctx context.Context) (ContactsSendRuntimeContext, bool) {
	if ctx == nil {
		return ContactsSendRuntimeContext{}, false
	}
	runtime, ok := ctx.Value(contactsSendRuntimeContextKey{}).(ContactsSendRuntimeContext)
	if !ok {
		return ContactsSendRuntimeContext{}, false
	}
	runtime = normalizeContactsSendRuntimeContext(runtime)
	if len(runtime.ForbiddenTargetIDs) == 0 {
		return ContactsSendRuntimeContext{}, false
	}
	return runtime, true
}

func normalizeContactsSendRuntimeContext(runtime ContactsSendRuntimeContext) ContactsSendRuntimeContext {
	if len(runtime.ForbiddenTargetIDs) == 0 {
		return ContactsSendRuntimeContext{}
	}
	seen := make(map[string]bool, len(runtime.ForbiddenTargetIDs))
	out := make([]string, 0, len(runtime.ForbiddenTargetIDs))
	for _, raw := range runtime.ForbiddenTargetIDs {
		id := normalizeContactsSendTargetID(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return ContactsSendRuntimeContext{ForbiddenTargetIDs: out}
}

func contactsSendBlockedTarget(contactID string, chatID string, runtime ContactsSendRuntimeContext) (field string, target string, ok bool) {
	if len(runtime.ForbiddenTargetIDs) == 0 {
		return "", "", false
	}
	blocked := make(map[string]bool, len(runtime.ForbiddenTargetIDs))
	for _, id := range runtime.ForbiddenTargetIDs {
		blocked[id] = true
	}
	if id := normalizeContactsSendTargetID(contactID); id != "" && blocked[id] {
		return "contact_id", id, true
	}
	if id := normalizeContactsSendTargetID(chatID); id != "" && blocked[id] {
		return "chat_id", id, true
	}
	return "", "", false
}

func normalizeContactsSendTargetID(raw string) string {
	protocol, id, ok := refid.Parse(raw)
	if !ok {
		return ""
	}
	switch protocol {
	case "tg":
		id = strings.TrimSpace(id)
		if strings.HasPrefix(id, "@") {
			username := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(id, "@")))
			if username == "" {
				return ""
			}
			return "tg:@" + username
		}
		chatID, err := strconv.ParseInt(id, 10, 64)
		if err != nil || chatID == 0 {
			return ""
		}
		return fmt.Sprintf("tg:%d", chatID)
	case "slack":
		parts := strings.Split(id, ":")
		if len(parts) != 2 {
			return ""
		}
		teamID := strings.ToUpper(strings.TrimSpace(parts[0]))
		targetID := strings.ToUpper(strings.TrimSpace(parts[1]))
		if teamID == "" || targetID == "" {
			return ""
		}
		return "slack:" + teamID + ":" + targetID
	case "line":
		chatID := refid.NormalizeLineID(id)
		if chatID == "" {
			return ""
		}
		return "line:" + chatID
	case "line_user":
		userID := refid.NormalizeLineID(id)
		if userID == "" {
			return ""
		}
		return "line_user:" + userID
	case "lark":
		chatID := refid.NormalizeLarkID(id)
		if chatID == "" {
			return ""
		}
		return "lark:" + chatID
	case "lark_user":
		openID := refid.NormalizeLarkID(id)
		if openID == "" {
			return ""
		}
		return "lark_user:" + openID
	default:
		normalized, ok := refid.Normalize(raw)
		if !ok {
			return ""
		}
		return normalized
	}
}
