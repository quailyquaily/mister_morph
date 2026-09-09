package taskdomain

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// TopicIconsJSON maps theme IDs to descriptions for topic naming. IDs match the
// console's bundled SVG filenames.
//
//go:embed topic_icons.json
var TopicIconsJSON string

var topicIcons = func() map[string]string {
	var icons map[string]string
	if err := json.Unmarshal([]byte(TopicIconsJSON), &icons); err != nil {
		panic(err)
	}
	return icons
}()

func NormalizeTopicIcon(icon string) string {
	icon = strings.TrimSpace(icon)
	if _, ok := topicIcons[icon]; ok {
		return icon
	}
	return "chat"
}
