package taskdomain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeTopicIcon(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{"code", "code"}, {" book-open ", "book-open"}, {"", "chat"}, {"unknown", "chat"}, {"<svg/>", "chat"},
		{"hand-waving", "hand-waving"}, {"paw-print", "paw-print"}, {"baby", "baby"},
		{"house", "house"}, {"fork-knife", "fork-knife"}, {"barbell", "barbell"},
		{"music-notes", "music-notes"}, {"airplane", "airplane"},
	} {
		if got := NormalizeTopicIcon(tt.input); got != tt.want {
			t.Errorf("NormalizeTopicIcon(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTopicIconCatalogDescribesThemes(t *testing.T) {
	var catalog map[string]string
	if err := json.Unmarshal([]byte(TopicIconsJSON), &catalog); err != nil {
		t.Fatal(err)
	}
	for id, theme := range catalog {
		if strings.TrimSpace(theme) == "" {
			t.Errorf("icon %q needs a theme description", id)
		}
	}
	for id, theme := range map[string]string{"hand-waving": "Greetings", "paw-print": "Pets", "baby": "Babies"} {
		if !strings.Contains(catalog[id], theme) {
			t.Errorf("%q missing theme %q", id, theme)
		}
	}
}
