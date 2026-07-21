package textutil

import "testing"

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name string
		text string
		max  int
		want string
	}{
		{name: "unicode", text: "  你好世界  ", max: 3, want: "你好世"},
		{name: "within limit", text: "  hello  ", max: 5, want: "hello"},
		{name: "disabled", text: "  hello  ", max: 0, want: "hello"},
		{name: "empty", text: "   ", max: 2, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateRunes(tt.text, tt.max); got != tt.want {
				t.Fatalf("TruncateRunes(%q, %d) = %q, want %q", tt.text, tt.max, got, tt.want)
			}
		})
	}
}
