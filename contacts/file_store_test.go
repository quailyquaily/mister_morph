package contacts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreContactsReadWrite(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	active := Contact{
		ContactID:        "slack:T111:U222",
		Kind:             KindHuman,
		Channel:          ChannelSlack,
		ContactNickname:  "Active Slack",
		SlackTeamID:      "T111",
		SlackUserID:      "U222",
		SlackDMChannelID: "D333",
		TopicPreferences: []string{"ops", "alerts"},
	}
	inactive := Contact{
		ContactID:        "tg:1001",
		Kind:             KindHuman,
		Channel:          ChannelTelegram,
		ContactNickname:  "Inactive Human",
		TGUsername:       "john",
		TGPrivateChatID:  1001,
		TGGroupChatIDs:   []int64{-10001},
		TopicPreferences: []string{"planning"},
	}
	if err := store.PutContact(ctx, active); err != nil {
		t.Fatalf("PutContact(active) error = %v", err)
	}
	if err := store.PutContact(ctx, inactive); err != nil {
		t.Fatalf("PutContact(inactive) error = %v", err)
	}

	activeList, err := store.ListContacts(ctx, StatusActive)
	if err != nil {
		t.Fatalf("ListContacts(active) error = %v", err)
	}
	if len(activeList) != 2 {
		t.Fatalf("active contacts mismatch: got=%v", activeList)
	}
	byID := map[string]Contact{}
	for _, item := range activeList {
		byID[item.ContactID] = item
	}
	if byID[active.ContactID].SlackDMChannelID == "" {
		t.Fatalf("active contact slack_dm_channel_id should be set")
	}
	if byID[inactive.ContactID].TGPrivateChatID != 1001 {
		t.Fatalf("active tg_private_chat_id mismatch: got %d want 1001", byID[inactive.ContactID].TGPrivateChatID)
	}

	inactiveList, err := store.ListContacts(ctx, StatusInactive)
	if err != nil {
		t.Fatalf("ListContacts(inactive) error = %v", err)
	}
	if len(inactiveList) != 0 {
		t.Fatalf("inactive contacts mismatch: got=%v", inactiveList)
	}
}

func TestFileStoreBusRecordsRoundTrip(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	now := time.Date(2026, 2, 8, 11, 0, 0, 0, time.UTC)
	if err := store.PutBusInboxRecord(ctx, BusInboxRecord{
		Channel:           ChannelTelegram,
		PlatformMessageID: "12345",
		ConversationKey:   "tg:-1001",
		SeenAt:            now,
	}); err != nil {
		t.Fatalf("PutBusInboxRecord() error = %v", err)
	}
	inbox, ok, err := store.GetBusInboxRecord(ctx, ChannelTelegram, "12345")
	if err != nil {
		t.Fatalf("GetBusInboxRecord() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetBusInboxRecord() expected ok=true")
	}
	if inbox.ConversationKey != "tg:-1001" {
		t.Fatalf("conversation_key mismatch: got %q", inbox.ConversationKey)
	}

	lastAttemptAt := now.Add(1 * time.Minute)
	if err := store.PutBusOutboxRecord(ctx, BusOutboxRecord{
		Channel:        ChannelTelegram,
		IdempotencyKey: "manual:k1",
		ContactID:      "tg:1001",
		ItemID:         "item-1",
		ContentType:    "text/plain",
		PayloadBase64:  "aGVsbG8",
		Status:         BusDeliveryStatusPending,
		Attempts:       1,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastAttemptAt:  &lastAttemptAt,
	}); err != nil {
		t.Fatalf("PutBusOutboxRecord() error = %v", err)
	}
	outbox, ok, err := store.GetBusOutboxRecord(ctx, ChannelTelegram, "manual:k1")
	if err != nil {
		t.Fatalf("GetBusOutboxRecord() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetBusOutboxRecord() expected ok=true")
	}
	if outbox.Status != BusDeliveryStatusPending {
		t.Fatalf("outbox status mismatch: got %q", outbox.Status)
	}
}

func TestFileStoreBusOutboxRejectsUnknownField(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "bus_outbox.json"),
		[]byte("{\"version\":1,\"records\":[{\"channel\":\"telegram\",\"idempotency_key\":\"k\",\"status\":\"sent\",\"attempts\":1,\"created_at\":\"2026-02-08T12:00:00Z\",\"updated_at\":\"2026-02-08T12:00:00Z\",\"sent_at\":\"2026-02-08T12:00:00Z\",\"unknown\":\"x\"}]}\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, _, err := store.GetBusOutboxRecord(ctx, ChannelTelegram, "k"); err == nil {
		t.Fatalf("GetBusOutboxRecord() expected decode error for unknown field")
	}
}

func TestFileStoreParsesProfileMarkdownTemplate(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	activeBody := `---
created_at: "1970-01-01T00:00:00Z"
updated_at: "1970-01-01T00:00:00Z"
---

# Active Contacts

## Alice

` + "```yaml\n" + `contact_id: "tg:90001"
nickname: "Alice"
kind: "human"
channel: "telegram"
tg_username: "alice"
tg_private_chat_id: "90001"
tg_group_chat_ids:
  - "-100222"
topic_preferences:
  - "golang"
` + "```\n"
	inactiveBody := `---
created_at: "1970-01-01T00:00:00Z"
updated_at: "1970-01-01T00:00:00Z"
---

# Inactive Contacts

## Bob Slack

` + "```yaml\n" + `contact_id: "slack:T111:D222"
kind: "human"
channel: "slack"
slack_team_id: "T111"
slack_dm_channel_id: "D222"
` + "```\n"
	if err := os.WriteFile(filepath.Join(root, "ACTIVE.md"), []byte(activeBody), 0o600); err != nil {
		t.Fatalf("WriteFile(ACTIVE.md) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "INACTIVE.md"), []byte(inactiveBody), 0o600); err != nil {
		t.Fatalf("WriteFile(INACTIVE.md) error = %v", err)
	}

	active, err := store.ListContacts(ctx, StatusActive)
	if err != nil {
		t.Fatalf("ListContacts(active) error = %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active contacts mismatch: got=%d want=1", len(active))
	}
	if active[0].ContactID != "tg:90001" || active[0].Kind != KindHuman {
		t.Fatalf("active contact mismatch: %#v", active[0])
	}
	if active[0].TGPrivateChatID != 90001 {
		t.Fatalf("active private chat id mismatch: %#v", active[0])
	}

	inactive, err := store.ListContacts(ctx, StatusInactive)
	if err != nil {
		t.Fatalf("ListContacts(inactive) error = %v", err)
	}
	if len(inactive) != 1 {
		t.Fatalf("inactive contacts mismatch: got=%d want=1", len(inactive))
	}
	if inactive[0].ContactID != "slack:T111:D222" || inactive[0].Kind != KindHuman {
		t.Fatalf("inactive contact mismatch: %#v", inactive[0])
	}
	if inactive[0].SlackDMChannelID != "D222" {
		t.Fatalf("inactive contact slack_dm_channel_id mismatch: got=%q want=D222", inactive[0].SlackDMChannelID)
	}
}

func TestFileStorePutContact_ActiveOverflowMovesToInactive(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	base := time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= maxActiveContacts+1; i++ {
		last := base.Add(time.Duration(i) * time.Minute)
		record := Contact{
			ContactID:         fmt.Sprintf("tg:%d", i),
			Kind:              KindHuman,
			Channel:           ChannelTelegram,
			ContactNickname:   fmt.Sprintf("User %d", i),
			TGPrivateChatID:   int64(i),
			LastInteractionAt: &last,
		}
		if err := store.PutContact(ctx, record); err != nil {
			t.Fatalf("PutContact(%d) error = %v", i, err)
		}
	}

	active, err := store.ListContacts(ctx, StatusActive)
	if err != nil {
		t.Fatalf("ListContacts(active) error = %v", err)
	}
	if len(active) != maxActiveContacts {
		t.Fatalf("active contacts count mismatch: got=%d want=%d", len(active), maxActiveContacts)
	}

	inactive, err := store.ListContacts(ctx, StatusInactive)
	if err != nil {
		t.Fatalf("ListContacts(inactive) error = %v", err)
	}
	if len(inactive) != 1 {
		t.Fatalf("inactive contacts count mismatch: got=%d want=1", len(inactive))
	}
	if inactive[0].ContactID != "tg:1" {
		t.Fatalf("expected oldest contact moved to inactive: got=%s want=tg:1", inactive[0].ContactID)
	}
}

func TestFileStoreSlackContactRoundTrip(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	record := Contact{
		ContactID:        "slack:T111:U222",
		Kind:             KindHuman,
		Channel:          ChannelSlack,
		ContactNickname:  "Alice Slack",
		SlackTeamID:      "T111",
		SlackUserID:      "U222",
		SlackDMChannelID: "D333",
		SlackChannelIDs:  []string{"C444", "G555"},
	}
	if err := store.PutContact(ctx, record); err != nil {
		t.Fatalf("PutContact() error = %v", err)
	}
	got, ok, err := store.GetContact(ctx, "slack:T111:U222")
	if err != nil {
		t.Fatalf("GetContact() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetContact() expected ok=true")
	}
	if got.Channel != ChannelSlack {
		t.Fatalf("channel mismatch: got %q want %q", got.Channel, ChannelSlack)
	}
	if got.SlackTeamID != "T111" || got.SlackUserID != "U222" {
		t.Fatalf("slack identity mismatch: team=%q user=%q", got.SlackTeamID, got.SlackUserID)
	}
	if got.SlackDMChannelID != "D333" {
		t.Fatalf("slack dm channel mismatch: got %q want %q", got.SlackDMChannelID, "D333")
	}
	if len(got.SlackChannelIDs) != 2 {
		t.Fatalf("slack channel ids count mismatch: got=%v", got.SlackChannelIDs)
	}
}
