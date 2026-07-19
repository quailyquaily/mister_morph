package cron

import "testing"

func TestIsConsoleNotificationChatID(t *testing.T) {
	tests := []struct {
		name   string
		chatID string
		want   bool
	}{
		{name: "exact", chatID: ConsoleNotificationChatID, want: true},
		{name: "trimmed", chatID: "  " + ConsoleNotificationChatID + "  ", want: true},
		{name: "external chat", chatID: "tg:-100", want: false},
		{name: "empty", chatID: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsConsoleNotificationChatID(tt.chatID); got != tt.want {
				t.Fatalf("IsConsoleNotificationChatID(%q) = %v, want %v", tt.chatID, got, tt.want)
			}
		})
	}
}
