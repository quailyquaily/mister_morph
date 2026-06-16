package taskruntime

import "strings"

type ObservationMetaIDs struct {
	TaskID        string
	TraceID       string
	TopicID       string
	OriginEventID string
}

func ApplyObservationMeta(meta map[string]any, ids ObservationMetaIDs) map[string]any {
	if meta == nil {
		meta = map[string]any{}
	}
	setObservationMetaString(meta, "task_id", ids.TaskID)
	setObservationMetaString(meta, "trace_id", ids.TraceID)
	setObservationMetaString(meta, "topic_id", ids.TopicID)
	setObservationMetaString(meta, "origin_event_id", ids.OriginEventID)
	return meta
}

func setObservationMetaString(meta map[string]any, key string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	meta[key] = value
}
