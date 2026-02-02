package main

import "testing"

func TestEscapeTelegramMarkdownUnderscores(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain_identifier",
			in:   "new_york",
			want: "new\\_york",
		},
		{
			name: "already_escaped",
			in:   "auth\\_profile",
			want: "auth\\_profile",
		},
		{
			name: "inline_code_unmodified",
			in:   "`new_york`",
			want: "`new_york`",
		},
		{
			name: "fenced_code_unmodified",
			in:   "```json\nnew_york\n```",
			want: "```json\nnew_york\n```",
		},
		{
			name: "mixed",
			in:   "use new_york and `new_york`",
			want: "use new\\_york and `new_york`",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := escapeTelegramMarkdownUnderscores(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
