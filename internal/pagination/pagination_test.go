package pagination

import (
	"errors"
	"testing"
	"time"
)

type testCursor struct {
	Name string `json:"name"`
}

func TestOpaqueCursorRoundTripAndKind(t *testing.T) {
	t.Parallel()

	raw, err := EncodeCursor("test", testCursor{Name: "下一页"})
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if raw == "" || raw == "下一页" {
		t.Fatalf("EncodeCursor() = %q, want an opaque cursor", raw)
	}
	var decoded testCursor
	if err := DecodeCursor(raw, "test", &decoded); err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if decoded.Name != "下一页" {
		t.Fatalf("decoded = %+v", decoded)
	}
	if err := DecodeCursor(raw, "other", &decoded); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("DecodeCursor(wrong kind) error = %v, want ErrInvalidCursor", err)
	}
}

func TestKeysetCursorRoundTripAndOrder(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 8, 9, 12, 34, 56, 789, time.FixedZone("JST", 9*60*60))
	raw := EncodeKeysetCursor(at, "topic:日本語")
	cursor, ok := ParseKeysetCursor(raw)
	if !ok {
		t.Fatalf("ParseKeysetCursor(%q) rejected a valid cursor", raw)
	}
	if !cursor.Time.Equal(at) || cursor.ID != "topic:日本語" {
		t.Fatalf("cursor = %+v, want time %s and original ID", cursor, at)
	}

	orderCursor := KeysetCursor{Time: at.UTC(), ID: "topic:mmm"}
	if !FollowsKeysetCursor(at.Add(-time.Second), "newer-id", orderCursor) {
		t.Fatal("an older timestamp should follow the cursor")
	}
	if !FollowsKeysetCursor(at, "topic:aaa", orderCursor) {
		t.Fatal("a smaller ID at the same timestamp should follow the cursor")
	}
	if FollowsKeysetCursor(at.Add(time.Second), "older-id", orderCursor) {
		t.Fatal("a newer timestamp must not follow the cursor")
	}
	if FollowsKeysetCursor(at, "topic:zzz", orderCursor) {
		t.Fatal("a larger ID at the same timestamp must not follow the cursor")
	}
}

func TestParseKeysetCursorRejectsMalformedValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"missing-separator", "nan:dG9waWM", "1:", "1:not base64"} {
		if _, ok := ParseKeysetCursor(raw); ok {
			t.Fatalf("ParseKeysetCursor(%q) accepted malformed input", raw)
		}
	}
	if _, ok := ParseKeysetCursor(""); !ok {
		t.Fatal("an empty cursor should represent the first page")
	}
}

func TestPageFromLookahead(t *testing.T) {
	t.Parallel()

	page := PageFromLookahead([]string{"a", "b", "c"}, 2, func(item string) string {
		return "after-" + item
	})
	if len(page.Items) != 2 || page.Items[0] != "a" || page.Items[1] != "b" {
		t.Fatalf("page.Items = %#v, want [a b]", page.Items)
	}
	if page.Limit != 2 || !page.HasNext || page.NextCursor != "after-b" {
		t.Fatalf("page metadata = %+v", page)
	}

	last := PageFromLookahead([]string{"c"}, 2, func(item string) string {
		return "after-" + item
	})
	if len(last.Items) != 1 || last.HasNext || last.NextCursor != "" {
		t.Fatalf("last page = %+v", last)
	}
}

func TestPageWithCursor(t *testing.T) {
	t.Parallel()

	page := PageWithCursor([]string{"a", "b"}, 2, "opaque")
	if !page.HasNext || page.NextCursor != "opaque" {
		t.Fatalf("page = %+v", page)
	}
	last := PageWithCursor([]string{"c"}, 2, "")
	if last.HasNext || last.NextCursor != "" {
		t.Fatalf("last = %+v", last)
	}
}
