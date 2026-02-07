package contacts

import (
	"context"
	"testing"
)

func TestLLMNicknameGeneratorSuggestNickname(t *testing.T) {
	client := &stubLLMClient{
		reply: `{"nickname":"Alice Architect","confidence":0.88,"reason":"topic overlap"}`,
	}
	gen := NewLLMNicknameGenerator(client, "gpt-5.2")
	nickname, confidence, err := gen.SuggestNickname(context.Background(), Contact{
		ContactID:          "tg:@alice",
		Kind:               KindHuman,
		SubjectID:          "tg:@alice",
		UnderstandingDepth: 60,
	})
	if err != nil {
		t.Fatalf("SuggestNickname() error = %v", err)
	}
	if nickname != "Alice Architect" {
		t.Fatalf("nickname mismatch: got %q want %q", nickname, "Alice Architect")
	}
	if confidence != 0.88 {
		t.Fatalf("confidence mismatch: got %v want %v", confidence, 0.88)
	}
}
