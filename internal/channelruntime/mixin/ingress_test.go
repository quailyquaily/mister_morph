package mixin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	mixinbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/mixin"
	"github.com/quailyquaily/mistermorph/internal/mixinapi"
)

const (
	testBotID          = "11111111-1111-1111-1111-111111111111"
	testConversationID = "22222222-2222-2222-2222-222222222222"
	testUserID         = "33333333-3333-3333-3333-333333333333"
)

type fakeMixinAPI struct {
	users                 map[string]mixinapi.User
	readUserErrors        []error
	conversations         map[string]mixinapi.Conversation
	readUserCalls         int
	readConversationCalls int
	readAttachmentCalls   int
	sent                  []mixinapi.MessageRequest
	attachments           map[string]mixinapi.Attachment
	attachmentBody        []byte
	attachmentContentType string
	attachmentError       error
}

func (f *fakeMixinAPI) Me(context.Context) (mixinapi.User, error) { return f.users[testBotID], nil }
func (f *fakeMixinAPI) ReadUser(_ context.Context, id string) (mixinapi.User, error) {
	f.readUserCalls++
	if len(f.readUserErrors) > 0 {
		err := f.readUserErrors[0]
		f.readUserErrors = f.readUserErrors[1:]
		if err != nil {
			return mixinapi.User{}, err
		}
	}
	return f.users[id], nil
}
func (f *fakeMixinAPI) ReadConversation(_ context.Context, id string) (mixinapi.Conversation, error) {
	f.readConversationCalls++
	return f.conversations[id], nil
}
func (f *fakeMixinAPI) SendMessages(_ context.Context, messages []mixinapi.MessageRequest) error {
	f.sent = append(f.sent, messages...)
	return nil
}
func (f *fakeMixinAPI) ReadAttachment(_ context.Context, id string) (mixinapi.Attachment, error) {
	f.readAttachmentCalls++
	if f.attachmentError != nil {
		return mixinapi.Attachment{}, f.attachmentError
	}
	return f.attachments[id], nil
}

func TestMixinIngressAuthorizesBeforeDownloadingAttachment(t *testing.T) {
	attachmentID := "44444444-4444-4444-4444-444444444444"
	api := &fakeMixinAPI{
		users:         map[string]mixinapi.User{testBotID: {UserID: testBotID}, testUserID: {UserID: testUserID}},
		conversations: map[string]mixinapi.Conversation{testConversationID: {ConversationID: testConversationID, Category: mixinapi.ConversationCategoryGroup}},
		attachments:   map[string]mixinapi.Attachment{attachmentID: {AttachmentID: attachmentID, ViewURL: "https://example.invalid/file"}},
	}
	payload, _ := json.Marshal(mixinAttachmentPayload{AttachmentID: attachmentID, Name: "report.txt", Size: 6})
	ingress := newMixinIngress(api, api.users[testBotID], t.TempDir(), nil)
	ingress.authorize = func(context.Context, mixinbus.InboundMessage) (bool, error) { return false, nil }

	_, publish, err := ingress.Normalize(context.Background(), mixinapi.MessageView{
		ConversationID: testConversationID, UserID: testUserID,
		MessageID: "55555555-5555-5555-5555-555555555555", Category: mixinapi.MessageCategoryPlainData,
		DataBase64: base64.RawURLEncoding.EncodeToString(payload),
	})
	if err != nil || publish {
		t.Fatalf("Normalize() publish=%v err=%v", publish, err)
	}
	if api.readAttachmentCalls != 0 {
		t.Fatalf("unauthorized attachment reads = %d, want 0", api.readAttachmentCalls)
	}
}

func TestMixinIngressRetriesUserProfileAfterTransientFailure(t *testing.T) {
	api := &fakeMixinAPI{
		users: map[string]mixinapi.User{
			testBotID:  {UserID: testBotID},
			testUserID: {UserID: testUserID, FullName: "Alice", AppID: "agent-app"},
		},
		readUserErrors: []error{errors.New("temporary profile failure")},
		conversations:  map[string]mixinapi.Conversation{testConversationID: {ConversationID: testConversationID, Category: mixinapi.ConversationCategoryContact}},
	}
	ingress := newMixinIngress(api, api.users[testBotID], "", nil)
	message := mixinapi.MessageView{
		ConversationID: testConversationID, UserID: testUserID,
		MessageID: "55555555-5555-5555-5555-555555555555", Category: mixinapi.MessageCategoryPlainText,
		DataBase64: base64.RawURLEncoding.EncodeToString([]byte("hello")),
	}
	first, publish, err := ingress.Normalize(context.Background(), message)
	if err != nil || !publish || first.DisplayName != "" || first.FromIsAgent {
		t.Fatalf("first Normalize() = %#v, %v, %v", first, publish, err)
	}
	message.MessageID = "66666666-6666-6666-6666-666666666666"
	second, publish, err := ingress.Normalize(context.Background(), message)
	if err != nil || !publish || second.DisplayName != "Alice" || !second.FromIsAgent {
		t.Fatalf("second Normalize() = %#v, %v, %v", second, publish, err)
	}
	if api.readUserCalls != 2 {
		t.Fatalf("user profile calls = %d, want 2", api.readUserCalls)
	}
}

func TestMixinIngressRetriesTransientAttachmentFailureButNotMissingAttachment(t *testing.T) {
	base := &fakeMixinAPI{
		users:         map[string]mixinapi.User{testBotID: {UserID: testBotID}, testUserID: {UserID: testUserID}},
		conversations: map[string]mixinapi.Conversation{testConversationID: {ConversationID: testConversationID, Category: mixinapi.ConversationCategoryContact}},
	}
	payload, _ := json.Marshal(mixinAttachmentPayload{AttachmentID: "44444444-4444-4444-4444-444444444444", Name: "report.txt"})
	message := mixinapi.MessageView{
		ConversationID: testConversationID, UserID: testUserID, MessageID: "55555555-5555-5555-5555-555555555555",
		Category: mixinapi.MessageCategoryPlainData, DataBase64: base64.RawURLEncoding.EncodeToString(payload),
	}
	base.attachmentError = errors.New("temporary network error")
	_, _, err := newMixinIngress(base, base.users[testBotID], t.TempDir(), nil).Normalize(context.Background(), message)
	if !errors.Is(err, errMixinAttachmentDownload) {
		t.Fatalf("transient error = %v", err)
	}
	base.attachmentError = &mixinapi.APIError{HTTPStatus: 404, Description: "not found"}
	_, _, err = newMixinIngress(base, base.users[testBotID], t.TempDir(), nil).Normalize(context.Background(), message)
	if err == nil || errors.Is(err, errMixinAttachmentDownload) {
		t.Fatalf("missing attachment error = %v", err)
	}
}
func (f *fakeMixinAPI) DownloadAttachment(context.Context, mixinapi.Attachment, int64) ([]byte, string, error) {
	return append([]byte(nil), f.attachmentBody...), f.attachmentContentType, nil
}
func (f *fakeMixinAPI) CreateAttachment(context.Context) (mixinapi.Attachment, error) {
	return mixinapi.Attachment{}, nil
}
func (f *fakeMixinAPI) UploadAttachment(context.Context, mixinapi.Attachment, string, int64, io.Reader) error {
	return nil
}
func (f *fakeMixinAPI) CreateContactConversation(_ context.Context, userID string) (mixinapi.Conversation, error) {
	conversationID, err := mixinapi.UniqueConversationID(testBotID, userID)
	return mixinapi.Conversation{ConversationID: conversationID, Category: mixinapi.ConversationCategoryContact}, err
}

func TestSendMixinTextSplitsWithStableIDs(t *testing.T) {
	t.Parallel()

	api := &fakeMixinAPI{}
	tracker := newRecentMessageTracker(4)
	err := sendMixinText(context.Background(), api, tracker, testConversationID, testUserID, "hello", mixinbus.SendTextOptions{
		MessageID:      "44444444-4444-4444-4444-444444444444",
		QuoteMessageID: "55555555-5555-5555-5555-555555555555",
	})
	if err != nil {
		t.Fatalf("sendMixinText() error = %v", err)
	}
	if len(api.sent) != 1 || api.sent[0].MessageID != "44444444-4444-4444-4444-444444444444" || api.sent[0].QuoteMessageID != "55555555-5555-5555-5555-555555555555" {
		t.Fatalf("sent = %#v", api.sent)
	}
	if api.sent[0].RecipientID != testUserID {
		t.Fatalf("recipient_id = %q", api.sent[0].RecipientID)
	}
	if !tracker.Contains(testConversationID, api.sent[0].MessageID) {
		t.Fatal("sent message was not tracked")
	}
}

func TestMixinIngressNormalizesAndCachesProfiles(t *testing.T) {
	t.Parallel()

	api := &fakeMixinAPI{
		users: map[string]mixinapi.User{
			testBotID:  {UserID: testBotID, IdentityNumber: "7000", FullName: "Morph"},
			testUserID: {UserID: testUserID, IdentityNumber: "8000", FullName: "Alice"},
		},
		conversations: map[string]mixinapi.Conversation{
			testConversationID: {ConversationID: testConversationID, Category: mixinapi.ConversationCategoryGroup, Name: "Group"},
		},
	}
	ingress := newMixinIngress(api, api.users[testBotID], "", nil)
	for _, messageID := range []string{"44444444-4444-4444-4444-444444444444", "55555555-5555-5555-5555-555555555555"} {
		inbound, publish, err := ingress.Normalize(context.Background(), mixinapi.MessageView{
			ConversationID: testConversationID,
			UserID:         testUserID,
			MessageID:      messageID,
			Category:       mixinapi.MessageCategoryPlainText,
			DataBase64:     base64.RawURLEncoding.EncodeToString([]byte("@7000 hello")),
		})
		if err != nil || !publish {
			t.Fatalf("Normalize() = %#v, %v, %v", inbound, publish, err)
		}
		if inbound.ChatType != mixinapi.ConversationCategoryGroup || inbound.Text != "hello" || len(inbound.MentionUserIDs) != 1 || inbound.MentionUserIDs[0] != testBotID {
			t.Fatalf("inbound = %#v", inbound)
		}
		if inbound.DisplayName != "Alice" || inbound.IdentityNumber != "8000" {
			t.Fatalf("sender profile = %#v", inbound)
		}
		if inbound.ConversationName != "Group" {
			t.Fatalf("conversation name = %q", inbound.ConversationName)
		}
	}
	if api.readUserCalls != 1 || api.readConversationCalls != 1 {
		t.Fatalf("profile calls = user %d, conversation %d", api.readUserCalls, api.readConversationCalls)
	}
}

func TestMixinIngressDownloadsImageBeforePublishing(t *testing.T) {
	attachmentID := "44444444-4444-4444-4444-444444444444"
	api := &fakeMixinAPI{
		users: map[string]mixinapi.User{
			testBotID:  {UserID: testBotID},
			testUserID: {UserID: testUserID},
		},
		conversations: map[string]mixinapi.Conversation{
			testConversationID: {ConversationID: testConversationID, Category: mixinapi.ConversationCategoryContact},
		},
		attachments: map[string]mixinapi.Attachment{
			attachmentID: {AttachmentID: attachmentID, ViewURL: "https://example.invalid/image"},
		},
		attachmentBody:        []byte("image bytes"),
		attachmentContentType: "image/png",
	}
	payload, _ := json.Marshal(mixinAttachmentPayload{AttachmentID: attachmentID, MimeType: "image/png", Size: int64(len(api.attachmentBody))})
	ingress := newMixinIngress(api, api.users[testBotID], t.TempDir(), nil)
	inbound, publish, err := ingress.Normalize(context.Background(), mixinapi.MessageView{
		ConversationID: testConversationID,
		UserID:         testUserID,
		MessageID:      "55555555-5555-5555-5555-555555555555",
		Category:       mixinapi.MessageCategoryPlainImage,
		DataBase64:     base64.RawURLEncoding.EncodeToString(payload),
	})
	if err != nil || !publish {
		t.Fatalf("Normalize() = %#v, %v, %v", inbound, publish, err)
	}
	if inbound.Text != "User sent an image." || len(inbound.ImageAttachments) != 1 {
		t.Fatalf("inbound = %#v", inbound)
	}
	if !strings.HasSuffix(inbound.ImageAttachments[0].Path, ".png") {
		t.Fatalf("image path = %q, want .png extension", inbound.ImageAttachments[0].Path)
	}
	raw, err := os.ReadFile(inbound.ImageAttachments[0].Path)
	if err != nil || string(raw) != "image bytes" {
		t.Fatalf("downloaded image = %q, %v", raw, err)
	}
}

func TestMixinIngressAddsDownloadedFilePathToText(t *testing.T) {
	attachmentID := "44444444-4444-4444-4444-444444444444"
	api := &fakeMixinAPI{
		users:          map[string]mixinapi.User{testBotID: {UserID: testBotID}, testUserID: {UserID: testUserID}},
		conversations:  map[string]mixinapi.Conversation{testConversationID: {ConversationID: testConversationID, Category: mixinapi.ConversationCategoryContact}},
		attachments:    map[string]mixinapi.Attachment{attachmentID: {AttachmentID: attachmentID, ViewURL: "https://example.invalid/file"}},
		attachmentBody: []byte("report"), attachmentContentType: "text/plain",
	}
	payload, _ := json.Marshal(mixinAttachmentPayload{AttachmentID: attachmentID, MimeType: "text/plain", Name: "report.txt", Size: 6})
	ingress := newMixinIngress(api, api.users[testBotID], t.TempDir(), nil)
	inbound, publish, err := ingress.Normalize(context.Background(), mixinapi.MessageView{
		ConversationID: testConversationID, UserID: testUserID,
		MessageID: "55555555-5555-5555-5555-555555555555", Category: mixinapi.MessageCategoryPlainData,
		DataBase64: base64.RawURLEncoding.EncodeToString(payload),
	})
	if err != nil || !publish || !strings.Contains(inbound.Text, "report.txt") || !strings.Contains(inbound.Text, "file_cache_dir/") {
		t.Fatalf("Normalize() = %#v, %v, %v", inbound, publish, err)
	}
}

func TestMixinIngressIgnoresOwnAndUnsupportedMessages(t *testing.T) {
	t.Parallel()

	api := &fakeMixinAPI{users: map[string]mixinapi.User{testBotID: {UserID: testBotID}}, conversations: map[string]mixinapi.Conversation{}}
	ingress := newMixinIngress(api, api.users[testBotID], "", nil)
	for _, message := range []mixinapi.MessageView{
		{ConversationID: testConversationID, UserID: testBotID, MessageID: "44444444-4444-4444-4444-444444444444", Category: mixinapi.MessageCategoryPlainText},
		{ConversationID: testConversationID, UserID: testUserID, MessageID: "55555555-5555-5555-5555-555555555555", Category: mixinapi.MessageCategoryPlainSticker},
	} {
		if _, publish, err := ingress.Normalize(context.Background(), message); err != nil || publish {
			t.Fatalf("Normalize(%s) publish=%v err=%v", message.MessageID, publish, err)
		}
	}
	if api.readUserCalls != 0 || api.readConversationCalls != 0 {
		t.Fatalf("ignored messages fetched profiles")
	}
}

func TestMixinSystemMessageInvalidatesConversationCache(t *testing.T) {
	t.Parallel()

	api := &fakeMixinAPI{
		users:         map[string]mixinapi.User{testBotID: {UserID: testBotID}, testUserID: {UserID: testUserID}},
		conversations: map[string]mixinapi.Conversation{testConversationID: {ConversationID: testConversationID, Category: mixinapi.ConversationCategoryGroup}},
	}
	ingress := newMixinIngress(api, api.users[testBotID], "", nil)
	invalidatedConversationID := ""
	ingress.onConversationInvalidated = func(conversationID string) {
		invalidatedConversationID = conversationID
	}
	text := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	_, _, _ = ingress.Normalize(context.Background(), mixinapi.MessageView{ConversationID: testConversationID, UserID: testUserID, MessageID: "44444444-4444-4444-4444-444444444444", Category: mixinapi.MessageCategoryPlainText, DataBase64: text})
	_, publish, err := ingress.Normalize(context.Background(), mixinapi.MessageView{ConversationID: testConversationID, UserID: testUserID, MessageID: "55555555-5555-5555-5555-555555555555", Category: mixinapi.MessageCategorySystem})
	if err != nil || publish {
		t.Fatalf("system message publish=%v err=%v", publish, err)
	}
	_, _, _ = ingress.Normalize(context.Background(), mixinapi.MessageView{ConversationID: testConversationID, UserID: testUserID, MessageID: "66666666-6666-6666-6666-666666666666", Category: mixinapi.MessageCategoryPlainText, DataBase64: text})
	if api.readConversationCalls != 2 {
		t.Fatalf("conversation calls = %d, want 2", api.readConversationCalls)
	}
	if invalidatedConversationID != testConversationID {
		t.Fatalf("invalidated conversation = %q, want %q", invalidatedConversationID, testConversationID)
	}
}
