package lark

import (
	"testing"
	"time"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestInboundMessageFromSDKEvent(t *testing.T) {
	t.Parallel()

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{EventID: "ev_123"},
		},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderType: strPtr("user"),
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_123")},
			},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_123"),
				CreateTime:  strPtr("1710000000123"),
				ChatId:      strPtr("oc_123"),
				ChatType:    strPtr("group"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"hello"}`),
				Mentions: []*larkim.MentionEvent{
					{Id: &larkim.UserId{OpenId: strPtr("ou_456")}},
					{Id: &larkim.UserId{OpenId: strPtr("ou_456")}},
					{Id: &larkim.UserId{OpenId: strPtr("ou_789")}},
				},
			},
		},
	}

	msg, ok, err := inboundMessageFromSDKEvent(event, map[string]bool{})
	if err != nil {
		t.Fatalf("inboundMessageFromSDKEvent() error = %v", err)
	}
	if !ok {
		t.Fatalf("inboundMessageFromSDKEvent() ok=false, want true")
	}
	if msg.ChatID != "oc_123" || msg.MessageID != "om_123" || msg.ChatType != "group" || msg.FromUserID != "ou_123" {
		t.Fatalf("unexpected message ids: %#v", msg)
	}
	if msg.Text != "hello" {
		t.Fatalf("text = %q, want hello", msg.Text)
	}
	if msg.EventID != "ev_123" {
		t.Fatalf("event_id = %q, want ev_123", msg.EventID)
	}
	wantTime := time.Unix(0, 1710000000123*int64(time.Millisecond)).UTC()
	if !msg.SentAt.Equal(wantTime) {
		t.Fatalf("sent_at = %s, want %s", msg.SentAt, wantTime)
	}
	if len(msg.MentionUsers) != 2 || msg.MentionUsers[0] != "ou_456" || msg.MentionUsers[1] != "ou_789" {
		t.Fatalf("mention users = %#v, want [ou_456 ou_789]", msg.MentionUsers)
	}
}

func TestInboundMessageFromSDKEventImage(t *testing.T) {
	t.Parallel()

	event := baseLarkSDKMessageEvent("image", `{"image_key":"img_123"}`)
	msg, ok, err := inboundMessageFromSDKEvent(event, nil)
	if err != nil {
		t.Fatalf("inboundMessageFromSDKEvent() error = %v", err)
	}
	if !ok {
		t.Fatalf("inboundMessageFromSDKEvent() ok=false, want true")
	}
	if msg.Text != "User sent an image." {
		t.Fatalf("text = %q, want image fallback", msg.Text)
	}
	if len(msg.ImageKeys) != 1 || msg.ImageKeys[0] != "img_123" {
		t.Fatalf("image keys = %#v, want [img_123]", msg.ImageKeys)
	}
}

func TestInboundMessageFromSDKEventAllowlist(t *testing.T) {
	t.Parallel()

	_, ok, err := inboundMessageFromSDKEvent(baseLarkSDKMessageEvent("text", `{"text":"hello"}`), map[string]bool{"oc_allowed": true})
	if err != nil {
		t.Fatalf("inboundMessageFromSDKEvent() error = %v", err)
	}
	if ok {
		t.Fatalf("inboundMessageFromSDKEvent() ok=true, want false")
	}
}

func TestLarkWebSocketDomainFromBaseURL(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"https://open.feishu.cn/open-apis":      "https://open.feishu.cn",
		"https://open.larksuite.com/open-apis/": "https://open.larksuite.com",
		"http://localhost:8080/open-apis":       "http://localhost:8080",
		"open.feishu.cn/open-apis":              "https://open.feishu.cn",
		"":                                      "https://open.feishu.cn",
	}
	for in, want := range cases {
		if got := larkWebSocketDomainFromBaseURL(in); got != want {
			t.Fatalf("domain(%q) = %q, want %q", in, got, want)
		}
	}
}

func baseLarkSDKMessageEvent(messageType string, content string) *larkim.P2MessageReceiveV1 {
	return &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "ev_123"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderType: strPtr("user"),
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_123")},
			},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_123"),
				CreateTime:  strPtr("1710000000123"),
				ChatId:      strPtr("oc_123"),
				ChatType:    strPtr("p2p"),
				MessageType: strPtr(messageType),
				Content:     strPtr(content),
			},
		},
	}
}

func strPtr(v string) *string {
	return &v
}
