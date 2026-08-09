package daemonruntime

import (
	"sort"
	"strings"
)

func rebuildOrderedTopicIDs(topics map[string]TopicInfo) []string {
	ids := make([]string, 0, len(topics))
	for id, topic := range topics {
		if !topicDeleted(topic) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		return topicComesBefore(topics[ids[i]], topics[ids[j]])
	})
	return ids
}

func upsertOrderedTopicID(ids []string, topics map[string]TopicInfo, id string) []string {
	id = strings.TrimSpace(id)
	topic, ok := topics[id]
	if !ok || id == "" || topicDeleted(topic) {
		return removeOrderedTopicID(ids, id)
	}
	next := removeOrderedTopicID(ids, id)
	index := sort.Search(len(next), func(i int) bool {
		return topicComesBefore(topic, topics[next[i]])
	})
	next = append(next, "")
	copy(next[index+1:], next[index:])
	next[index] = id
	return next
}

func removeOrderedTopicID(ids []string, id string) []string {
	next := make([]string, 0, len(ids))
	for _, current := range ids {
		if current != id {
			next = append(next, current)
		}
	}
	return next
}

func topicComesBefore(left TopicInfo, right TopicInfo) bool {
	if left.UpdatedAt.Equal(right.UpdatedAt) {
		return strings.TrimSpace(left.ID) > strings.TrimSpace(right.ID)
	}
	return left.UpdatedAt.After(right.UpdatedAt)
}

func topicOrderChanged(previous TopicInfo, next TopicInfo) bool {
	return !previous.UpdatedAt.Equal(next.UpdatedAt) || strings.TrimSpace(previous.ID) != strings.TrimSpace(next.ID)
}
