package agentpair

import (
	"fmt"
	"strconv"
	"strings"
)

// Admins contains platform identities and Contact references allowed to start Agent pairing.
type Admins struct {
	ids map[string]struct{}
}

func ParseAdmins(values []string) (Admins, error) {
	admins := Admins{ids: make(map[string]struct{})}
	for _, raw := range values {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		id, err := normalizeReference(raw)
		if err != nil {
			return Admins{}, fmt.Errorf("invalid admin %q: %w", strings.TrimSpace(raw), err)
		}
		admins.ids[referenceKey(id)] = struct{}{}
	}
	return admins, nil
}

func (a Admins) Contains(raw string) bool {
	id, err := normalizeReference(raw)
	if err != nil {
		return false
	}
	_, ok := a.ids[referenceKey(id)]
	return ok
}

func normalizeStableIdentity(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	lower := strings.ToLower(value)
	switch {
	case strings.HasPrefix(lower, "tg:"):
		suffix := strings.TrimSpace(value[len("tg:"):])
		id, err := strconv.ParseInt(suffix, 10, 64)
		if err != nil || id <= 0 || suffix != strconv.FormatInt(id, 10) {
			return "", fmt.Errorf("Telegram identity must be tg:<positive_user_id>")
		}
		return "tg:" + suffix, nil
	case strings.HasPrefix(lower, "slack:"):
		parts := strings.Split(value[len("slack:"):], ":")
		if len(parts) != 2 {
			return "", fmt.Errorf("Slack identity must be slack:<team_id>:<user_id>")
		}
		teamID := strings.ToUpper(strings.TrimSpace(parts[0]))
		userID := strings.ToUpper(strings.TrimSpace(parts[1]))
		if !isSlackStableID(teamID, 'T') || !(isSlackStableID(userID, 'U') || isSlackStableID(userID, 'W')) {
			return "", fmt.Errorf("Slack identity must use stable team and user IDs")
		}
		return "slack:" + teamID + ":" + userID, nil
	case strings.HasPrefix(lower, "line_user:"):
		userID := strings.TrimSpace(value[len("line_user:"):])
		if !isOpaqueStableID(userID) {
			return "", fmt.Errorf("LINE identity must be line_user:<user_id>")
		}
		return "line_user:" + userID, nil
	case strings.HasPrefix(lower, "lark_user:"):
		openID := strings.TrimSpace(value[len("lark_user:"):])
		if !isOpaqueStableID(openID) {
			return "", fmt.Errorf("Lark identity must be lark_user:<open_id>")
		}
		return "lark_user:" + openID, nil
	default:
		return "", fmt.Errorf("unsupported platform identity")
	}
}

func isSlackStableID(value string, prefix byte) bool {
	if len(value) < 2 || value[0] != prefix {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if c < 'A' || c > 'Z' {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

func isOpaqueStableID(value string) bool {
	return value != "" && !strings.ContainsAny(value, " \t\r\n:()")
}
