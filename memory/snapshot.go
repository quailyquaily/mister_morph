package memory

import "context"

func LoadSnapshot(ctx context.Context, store Store, subjectID string, reqCtx RequestContext, maxItems int) ([]Item, error) {
	if store == nil || subjectID == "" {
		return nil, nil
	}
	if maxItems <= 0 {
		maxItems = 50
	}

	var out []Item
	remaining := maxItems
	for _, ns := range NamespacesAllowlist {
		if remaining <= 0 {
			break
		}
		items, err := store.List(ctx, subjectID, ns, ReadOptions{Context: reqCtx, Limit: remaining})
		if err != nil {
			return nil, err
		}
		if len(items) > remaining {
			items = items[:remaining]
		}
		out = append(out, items...)
		remaining = maxItems - len(out)
	}
	return out, nil
}
