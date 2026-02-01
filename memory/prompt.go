package memory

import (
	"sort"
	"strings"
)

type SnapshotOptions struct {
	MaxItems int
	MaxChars int
}

func FormatSnapshotForPrompt(items []Item, opt SnapshotOptions) string {
	if opt.MaxItems <= 0 {
		opt.MaxItems = 50
	}
	if opt.MaxChars <= 0 {
		opt.MaxChars = 6000
	}

	// Stable ordering: namespace, key.
	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace == items[j].Namespace {
			return items[i].Key < items[j].Key
		}
		return items[i].Namespace < items[j].Namespace
	})

	if len(items) > opt.MaxItems {
		items = items[:opt.MaxItems]
	}

	var b strings.Builder
	curNS := ""
	wroteAny := false
	for _, it := range items {
		if strings.TrimSpace(it.Namespace) == "" || strings.TrimSpace(it.Key) == "" {
			continue
		}
		if it.Namespace != curNS {
			if wroteAny {
				b.WriteString("\n")
			}
			curNS = it.Namespace
			b.WriteString(curNS)
			b.WriteString(":\n")
		}
		b.WriteString("  ")
		b.WriteString(it.Key)
		b.WriteString(" = ")
		b.WriteString(strings.TrimSpace(it.Value))
		b.WriteString("\n")
		wroteAny = true

		if b.Len() >= opt.MaxChars {
			break
		}
	}

	out := strings.TrimSpace(b.String())
	if len(out) > opt.MaxChars {
		out = out[:opt.MaxChars]
		out = strings.TrimSpace(out)
	}
	return out
}
