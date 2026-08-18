package topiccontext

import "testing"

func TestScopeContextNormalizesValues(t *testing.T) {
	ctx := WithScope(nil, Scope{
		Runtime:         " console ",
		ConversationKey: " console:topic-1 ",
		TopicID:         " topic-1 ",
	})

	got, ok := ScopeFromContext(ctx)
	if !ok {
		t.Fatal("ScopeFromContext() ok = false")
	}
	want := (Scope{Runtime: "console", ConversationKey: "console:topic-1", TopicID: "topic-1"})
	if got != want {
		t.Fatalf("ScopeFromContext() = %#v, want %#v", got, want)
	}
}
