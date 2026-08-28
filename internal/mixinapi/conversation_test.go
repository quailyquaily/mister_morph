package mixinapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestUniqueConversationID(t *testing.T) {
	left := "773e5e77-4107-45c2-b648-8fc722ed77f5"
	right := "e8e5b807-fa8b-455a-8dfa-b189d28310ff"
	forward, err := UniqueConversationID(left, right)
	if err != nil {
		t.Fatalf("UniqueConversationID() error = %v", err)
	}
	reverse, err := UniqueConversationID(right, left)
	if err != nil {
		t.Fatalf("UniqueConversationID() reverse error = %v", err)
	}
	if forward != reverse {
		t.Fatalf("forward=%q reverse=%q", forward, reverse)
	}
	id, err := uuid.Parse(forward)
	if err != nil {
		t.Fatalf("result is not UUID: %v", err)
	}
	if id.Version() != 3 || id.Variant() != uuid.RFC4122 {
		t.Fatalf("version/variant = %v %v", id.Version(), id.Variant())
	}
	if _, err := UniqueConversationID("bad", right); err == nil {
		t.Fatal("invalid user id should fail")
	}
}

func TestCreateContactConversation(t *testing.T) {
	peerID := "e8e5b807-fa8b-455a-8dfa-b189d28310ff"
	wantConversationID, err := UniqueConversationID(testCredentials().ClientID, peerID)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var request CreateConversationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.Category != "CONTACT" || request.ConversationID != wantConversationID || len(request.Participants) != 1 || request.Participants[0].UserID != peerID {
			t.Errorf("request = %#v", request)
		}
		_, _ = io.WriteString(w, `{"data":{"conversation_id":"`+wantConversationID+`","category":"CONTACT"}}`)
	}))
	defer server.Close()
	client, err := NewClient(testCredentials(), ClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := client.CreateContactConversation(context.Background(), peerID)
	if err != nil {
		t.Fatalf("CreateContactConversation() error = %v", err)
	}
	if conversation.ConversationID != wantConversationID {
		t.Fatalf("conversation = %#v", conversation)
	}
}
