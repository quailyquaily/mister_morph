package todo

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	refid "github.com/quailyquaily/mistermorph/internal/entryutil/refid"
)

var englishSelfWordPattern = regexp.MustCompile(`(?i)\b(i|me|my|myself|we|us|our|ourselves)\b`)

func ExtractReferenceIDs(content string) ([]string, error) {
	return refid.ExtractMarkdownReferenceIDs(content)
}

// ValidateRequiredReferenceMentions enforces that first-person object mentions
// are explicitly referenceable (e.g. "[我](tg:1001)" / "[me](tg:1001)").
func ValidateRequiredReferenceMentions(content string, snapshot ContactSnapshot) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("content is required")
	}
	stripped := refid.StripMarkdownReferenceLinks(content)
	mention := firstPersonMention(stripped)
	if mention == "" {
		return nil
	}

	item := MissingReference{Mention: mention}
	if ref := suggestSelfReferenceID(snapshot); ref != "" {
		if suggestion, err := refid.FormatMarkdownReference(mention, ref); err == nil {
			item.Suggestion = suggestion
		}
	}
	return &MissingReferenceIDError{Items: []MissingReference{item}}
}

func firstPersonMention(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	for _, token := range []string{"我们", "本人", "我"} {
		if strings.Contains(content, token) {
			return token
		}
	}
	if m := englishSelfWordPattern.FindString(content); strings.TrimSpace(m) != "" {
		return strings.TrimSpace(m)
	}
	return ""
}

func suggestSelfReferenceID(snapshot ContactSnapshot) string {
	ids := dedupeSortedStrings(snapshot.ReachableIDs)
	if len(ids) == 0 {
		return ""
	}
	if len(ids) == 1 {
		return ids[0]
	}

	preferred := make([]string, 0, len(snapshot.Contacts))
	for _, c := range snapshot.Contacts {
		id := strings.TrimSpace(c.PreferredID)
		if id == "" || !refid.IsValid(id) {
			continue
		}
		preferred = append(preferred, id)
	}
	preferred = dedupeSortedStrings(preferred)
	if len(preferred) == 1 {
		return preferred[0]
	}
	return ""
}

func dedupeSortedStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, raw := range items {
		v := strings.TrimSpace(raw)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
