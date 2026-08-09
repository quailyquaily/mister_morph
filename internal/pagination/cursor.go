package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidCursor = errors.New("invalid pagination cursor")

const (
	cursorVersion    = 1
	keysetCursorKind = "keyset"
	maxCursorLength  = 4096
)

type cursorEnvelope struct {
	Version int             `json:"v"`
	Kind    string          `json:"kind"`
	Data    json.RawMessage `json:"data"`
}

func EncodeCursor(kind string, value any) (string, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "", fmt.Errorf("%w: missing kind", ErrInvalidCursor)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode pagination cursor data: %w", err)
	}
	raw, err := json.Marshal(cursorEnvelope{Version: cursorVersion, Kind: kind, Data: data})
	if err != nil {
		return "", fmt.Errorf("encode pagination cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeCursor(raw string, kind string, out any) error {
	raw = strings.TrimSpace(raw)
	kind = strings.TrimSpace(kind)
	if raw == "" || len(raw) > maxCursorLength || kind == "" || out == nil {
		return ErrInvalidCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return fmt.Errorf("%w: invalid encoding", ErrInvalidCursor)
	}
	var envelope cursorEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("%w: invalid envelope", ErrInvalidCursor)
	}
	if envelope.Version != cursorVersion || envelope.Kind != kind || len(envelope.Data) == 0 {
		return ErrInvalidCursor
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("%w: invalid data", ErrInvalidCursor)
	}
	return nil
}

// KeysetCursor identifies one item in descending time-and-ID order.
type KeysetCursor struct {
	Time time.Time
	ID   string
}

type keysetCursorData struct {
	UnixNano int64  `json:"time"`
	ID       string `json:"id"`
}

func EncodeKeysetCursor(at time.Time, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	raw, err := EncodeCursor(keysetCursorKind, keysetCursorData{UnixNano: at.UTC().UnixNano(), ID: id})
	if err != nil {
		return ""
	}
	return raw
}

func ParseKeysetCursor(raw string) (KeysetCursor, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return KeysetCursor{}, true
	}
	var data keysetCursorData
	if err := DecodeCursor(raw, keysetCursorKind, &data); err != nil {
		return KeysetCursor{}, false
	}
	id := strings.TrimSpace(data.ID)
	if id == "" {
		return KeysetCursor{}, false
	}
	return KeysetCursor{Time: time.Unix(0, data.UnixNano).UTC(), ID: id}, true
}

// FollowsKeysetCursor reports whether an item belongs after cursor when items
// are ordered by descending time and then descending ID.
func FollowsKeysetCursor(at time.Time, id string, cursor KeysetCursor) bool {
	itemTime := at.UTC()
	if itemTime.Before(cursor.Time) {
		return true
	}
	if itemTime.After(cursor.Time) {
		return false
	}
	return strings.Compare(strings.TrimSpace(id), cursor.ID) < 0
}
