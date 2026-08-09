package pagination

import "strings"

type Page[T any] struct {
	Items      []T    `json:"items"`
	Limit      int    `json:"limit"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasNext    bool   `json:"has_next"`
}

func PageWithCursor[T any](items []T, limit int, nextCursor string) Page[T] {
	nextCursor = strings.TrimSpace(nextCursor)
	return Page[T]{
		Items:      items,
		Limit:      limit,
		NextCursor: nextCursor,
		HasNext:    nextCursor != "",
	}
}

// PageFromLookahead turns at most limit+1 ordered items into one cursor page.
func PageFromLookahead[T any](items []T, limit int, cursorAfter func(T) string) Page[T] {
	page := PageWithCursor(items, limit, "")
	if limit <= 0 || len(items) <= limit {
		return page
	}
	included := items[:limit]
	return PageWithCursor(included, limit, cursorAfter(included[len(included)-1]))
}
