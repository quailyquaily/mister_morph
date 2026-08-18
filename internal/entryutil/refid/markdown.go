package refid

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	markdownReferencePattern = regexp.MustCompile(`\[[^\[\]\n]+\]\(([^()]+)\)`)
	markdownLinkPattern      = regexp.MustCompile(`\[[^\[\]\n]+\]\([^()]+\)`)
)

// ExtractMarkdownReferenceIDs returns unique reference IDs from "[Label](protocol:id)" mentions.
func ExtractMarkdownReferenceIDs(content string) ([]string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	matches := markdownReferencePattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		ref := strings.TrimSpace(m[1])
		if ref == "" {
			return nil, fmt.Errorf("missing reference id")
		}
		if !IsValid(ref) {
			return nil, fmt.Errorf("invalid reference id: %s", ref)
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out, nil
}

// StripMarkdownReferenceLinks removes markdown links like "[John](tg:1001)".
func StripMarkdownReferenceLinks(content string) string {
	return markdownLinkPattern.ReplaceAllString(content, "")
}

// FormatMarkdownReference formats "[Label](protocol:id)" and validates the reference id.
func FormatMarkdownReference(label string, refID string) (string, error) {
	label = strings.TrimSpace(label)
	refID = strings.TrimSpace(refID)
	if label == "" {
		return "", fmt.Errorf("label is required")
	}
	if !IsValid(refID) {
		return "", fmt.Errorf("invalid reference id: %s", refID)
	}
	return "[" + label + "](" + refID + ")", nil
}
