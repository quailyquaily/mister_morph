package lark

import (
	"context"
	"strings"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
)

func TestLarkWebSocketEventDispatcherIgnoresReactionEvents(t *testing.T) {
	dispatcher := newLarkWebSocketEventDispatcher(larkWebSocketIngressOptions{})
	for _, eventType := range []string{
		"im.message.reaction.created_v1",
		"im.message.reaction.deleted_v1",
	} {
		t.Run(eventType, func(t *testing.T) {
			resp, err := dispatcher.DoHandle(context.Background(), larkevent.ReqTypeEventCallBack, eventType, "", "", `{}`, "", &larkevent.EventReq{})
			if err != nil {
				t.Fatalf("DoHandle() error = %v", err)
			}
			if resp == nil {
				t.Fatal("DoHandle() response is nil")
			}
			body := string(resp.Body)
			if strings.Contains(body, "not found handler") {
				t.Fatalf("reaction event should be ignored by a registered handler, got %s", body)
			}
			if !strings.Contains(body, "success") {
				t.Fatalf("response body = %s, want success", body)
			}
		})
	}
}
