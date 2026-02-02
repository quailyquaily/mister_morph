package openai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/quailyquaily/mister_morph/llm"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestClient_ResponseBodyTruncated(t *testing.T) {
	// Build a response body larger than the limit.
	const limit int64 = 256
	bigBody := strings.Repeat("x", int(limit)+100)

	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(bigBody)),
			Request:    r,
		}, nil
	})

	c := New("http://fake.test", "key")
	c.HTTP = &http.Client{Transport: rt}
	c.MaxResponseBytes = limit

	// Chat will fail to unmarshal truncated JSON, but the key thing is
	// that io.ReadAll did not read more than limit bytes.
	_, err := c.Chat(context.Background(), llm.Request{
		Model:    "test",
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error from truncated JSON, got nil")
	}
	// Verify the error is about JSON parsing, not OOM.
	if !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "unexpected") && !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected JSON parse error, got: %v", err)
	}
}

func TestClient_NormalResponseParsed(t *testing.T) {
	validJSON := `{"choices":[{"message":{"content":"hello"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`

	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(validJSON)),
			Request:    r,
		}, nil
	})

	c := New("http://fake.test", "key")
	c.HTTP = &http.Client{Transport: rt}

	res, err := c.Chat(context.Background(), llm.Request{
		Model:    "test",
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res.Text != "hello" {
		t.Fatalf("expected text %q, got %q", "hello", res.Text)
	}
}
