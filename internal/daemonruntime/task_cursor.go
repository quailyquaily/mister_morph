package daemonruntime

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

type taskListCursor struct {
	CreatedAt time.Time
	ID        string
}

func parseTaskListCursor(raw string) (taskListCursor, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return taskListCursor{}, true
	}
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return taskListCursor{}, false
	}
	unixNano, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return taskListCursor{}, false
	}
	idRaw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(parts[1]))
	if err != nil {
		return taskListCursor{}, false
	}
	id := strings.TrimSpace(string(idRaw))
	if id == "" {
		return taskListCursor{}, false
	}
	return taskListCursor{
		CreatedAt: time.Unix(0, unixNano).UTC(),
		ID:        id,
	}, true
}

// TaskListCursorAfter returns the cursor for the page after info in task list order.
func TaskListCursorAfter(info TaskInfo) string {
	id := strings.TrimSpace(info.ID)
	if id == "" {
		return ""
	}
	return strconv.FormatInt(info.CreatedAt.UTC().UnixNano(), 10) + ":" + base64.RawURLEncoding.EncodeToString([]byte(id))
}

func filterTasksByCursor(items []TaskInfo, raw string) []TaskInfo {
	cursor, ok := parseTaskListCursor(raw)
	if !ok || strings.TrimSpace(raw) == "" {
		return items
	}
	out := items[:0]
	for _, item := range items {
		if taskAfterCursor(item, cursor) {
			out = append(out, item)
		}
	}
	return out
}

func taskAfterCursor(item TaskInfo, cursor taskListCursor) bool {
	itemTime := item.CreatedAt.UTC()
	if itemTime.Before(cursor.CreatedAt) {
		return true
	}
	if itemTime.After(cursor.CreatedAt) {
		return false
	}
	return strings.Compare(strings.TrimSpace(item.ID), cursor.ID) < 0
}
