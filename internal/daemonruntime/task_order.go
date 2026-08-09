package daemonruntime

import (
	"sort"
	"strings"
)

func rebuildOrderedTaskIDs(items map[string]TaskInfo) []string {
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return taskComesBefore(items[ids[i]], items[ids[j]])
	})
	return ids
}

func groupOrderedTaskIDsByTopic(ids []string, items map[string]TaskInfo) map[string][]string {
	grouped := make(map[string][]string)
	for _, id := range ids {
		item, ok := items[id]
		if !ok {
			continue
		}
		topicID := strings.TrimSpace(item.TopicID)
		grouped[topicID] = append(grouped[topicID], id)
	}
	return grouped
}

func upsertOrderedTaskID(ids []string, items map[string]TaskInfo, id string) []string {
	id = strings.TrimSpace(id)
	item, ok := items[id]
	if !ok || id == "" {
		return ids
	}
	next := make([]string, 0, len(ids)+1)
	for _, current := range ids {
		if current != id {
			next = append(next, current)
		}
	}
	index := sort.Search(len(next), func(i int) bool {
		return taskComesBefore(item, items[next[i]])
	})
	next = append(next, "")
	copy(next[index+1:], next[index:])
	next[index] = id
	return next
}

func removeOrderedTaskID(ids []string, id string) []string {
	next := make([]string, 0, len(ids))
	for _, current := range ids {
		if current != id {
			next = append(next, current)
		}
	}
	return next
}

func taskComesBefore(left TaskInfo, right TaskInfo) bool {
	if left.CreatedAt.Equal(right.CreatedAt) {
		return strings.TrimSpace(left.ID) > strings.TrimSpace(right.ID)
	}
	return left.CreatedAt.After(right.CreatedAt)
}

func taskOrderChanged(previous TaskInfo, next TaskInfo) bool {
	return !previous.CreatedAt.Equal(next.CreatedAt) || strings.TrimSpace(previous.ID) != strings.TrimSpace(next.ID)
}
