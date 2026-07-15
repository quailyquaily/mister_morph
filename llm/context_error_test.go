package llm

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsContextLengthError(t *testing.T) {
	marked := MarkContextLengthError(errors.New("provider rejected request"))
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "marked", err: marked, want: true},
		{name: "wrapped marked", err: fmt.Errorf("chat failed: %w", marked), want: true},
		{name: "openai wording", err: errors.New("maximum context length exceeded"), want: true},
		{name: "generic token wording", err: errors.New("input is too long for the model context window"), want: true},
		{name: "model input wording", err: errors.New("input is too long for requested model"), want: true},
		{name: "token count wording", err: errors.New("input token count exceeds the maximum number of tokens allowed"), want: true},
		{name: "unrelated", err: errors.New("rate limit exceeded"), want: false},
		{name: "nil", err: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsContextLengthError(tt.err); got != tt.want {
				t.Fatalf("IsContextLengthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
