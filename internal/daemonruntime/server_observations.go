package daemonruntime

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

const (
	observationDefaultLimit int64 = 50
	observationMinLimit     int64 = 1
	observationMaxLimit     int64 = 200
	observationLogScanLimit int64 = 1000
)

type observationView struct {
	Items []observationItem    `json:"items"`
	Logs  []observationLogLine `json:"logs"`
	Limit int                  `json:"limit"`
}

type observationItem struct {
	Ref     domainjournal.RecordRef `json:"ref"`
	ID      string                  `json:"id"`
	Time    string                  `json:"time"`
	Domain  string                  `json:"domain"`
	Type    string                  `json:"type"`
	Trace   domainjournal.Trace     `json:"trace,omitempty"`
	Payload json.RawMessage         `json:"payload,omitempty"`
}

type observationLogLine struct {
	File string `json:"file,omitempty"`
	Line string `json:"line"`
}

func readObservationView(journalDir string, logDir string, taskID string, topicID string, limit int) (observationView, error) {
	taskID = strings.TrimSpace(taskID)
	topicID = strings.TrimSpace(topicID)
	if taskID == "" && topicID == "" {
		return observationView{}, BadRequest("task_id or topic_id is required")
	}
	if limit <= 0 {
		limit = int(observationDefaultLimit)
	}

	view := observationView{
		Items: []observationItem{},
		Logs:  []observationLogLine{},
		Limit: limit,
	}
	traceIDs := map[string]bool{}
	indexRecords, err := readObservationIndexRecords(journalDir, taskID, topicID, limit)
	if err != nil {
		return view, err
	}
	for _, indexRecord := range indexRecords {
		record, err := domainjournal.ReadAtDir(journalDir, indexRecord.Ref)
		if err != nil {
			return view, err
		}
		event := record.Event
		if !matchesObservationEvent(event, taskID, topicID) {
			continue
		}
		item := observationItem{
			Ref:     record.Ref,
			ID:      event.ID,
			Time:    event.Time,
			Domain:  event.Domain,
			Type:    event.Type,
			Trace:   event.Trace,
			Payload: redactJSONRaw(event.Payload),
		}
		view.Items = append(view.Items, item)
		if traceID := strings.TrimSpace(event.Trace.TraceID); traceID != "" {
			traceIDs[traceID] = true
		}
	}
	if len(view.Items) > limit {
		view.Items = view.Items[len(view.Items)-limit:]
	}

	logs, err := readObservationLogs(logDir, traceIDs, limit)
	if err != nil {
		return view, err
	}
	view.Logs = logs
	return view, nil
}

func readObservationIndexRecords(journalDir string, taskID string, topicID string, limit int) ([]domainjournal.IndexRecord, error) {
	type keyedRecord struct {
		key string
		rec domainjournal.IndexRecord
	}
	records := []keyedRecord{}
	appendRecords := func(kind string, key string) error {
		if strings.TrimSpace(key) == "" {
			return nil
		}
		items, err := domainjournal.ReadIndexDir(journalDir, kind, key, limit)
		if err != nil {
			return err
		}
		for _, item := range items {
			refKey := item.Ref.File + ":" + fmt.Sprint(item.Ref.Byte)
			records = append(records, keyedRecord{key: refKey, rec: item})
		}
		return nil
	}
	if err := appendRecords("task", taskID); err != nil {
		return nil, err
	}
	if err := appendRecords("topic", topicID); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]domainjournal.IndexRecord, 0, len(records))
	sort.SliceStable(records, func(i, j int) bool {
		left := records[i].rec.Ref
		right := records[j].rec.Ref
		if left.File == right.File {
			return left.Byte < right.Byte
		}
		return left.File < right.File
	})
	for _, item := range records {
		if seen[item.key] {
			continue
		}
		seen[item.key] = true
		out = append(out, item.rec)
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func matchesObservationEvent(event domainjournal.Event, taskID string, topicID string) bool {
	if taskID != "" {
		if strings.TrimSpace(event.Trace.TaskID) == taskID {
			return true
		}
		if payloadStringAt(event.Payload, "task", "id") == taskID || payloadStringAt(event.Payload, "task_id") == taskID {
			return true
		}
	}
	if topicID != "" {
		if strings.TrimSpace(event.Trace.TopicID) == topicID {
			return true
		}
		if payloadStringAt(event.Payload, "topic", "id") == topicID ||
			payloadStringAt(event.Payload, "topic_id") == topicID ||
			payloadStringAt(event.Payload, "task", "topic_id") == topicID {
			return true
		}
	}
	return false
}

func payloadStringAt(raw json.RawMessage, path ...string) string {
	if len(path) == 0 || len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	current := value
	for _, part := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = obj[part]
	}
	text, ok := current.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func readObservationLogs(logDir string, traceIDs map[string]bool, limit int) ([]observationLogLine, error) {
	out := []observationLogLine{}
	if len(traceIDs) == 0 || limit <= 0 {
		return out, nil
	}
	chunk, err := readLatestLogChunk(logDir, "", observationLogScanLimit)
	if err != nil {
		return nil, err
	}
	for _, line := range chunk.Items {
		if !lineMatchesTrace(line, traceIDs) {
			continue
		}
		out = append(out, observationLogLine{
			File: chunk.File,
			Line: redactLogLine(line),
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func lineMatchesTrace(line string, traceIDs map[string]bool) bool {
	for traceID := range traceIDs {
		if strings.Contains(line, traceID) {
			return true
		}
	}
	return false
}

func redactJSONRaw(raw json.RawMessage) json.RawMessage {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		fallback, _ := json.Marshal(map[string]string{
			"raw": truncateText(string(raw), 500),
		})
		return fallback
	}
	out, err := json.Marshal(redactJSONValue(value, 4))
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return out
}

func redactLogLine(line string) string {
	var value any
	if err := json.Unmarshal([]byte(line), &value); err != nil {
		return truncateText(line, 1000)
	}
	out, err := json.Marshal(redactJSONValue(value, 4))
	if err != nil {
		return truncateText(line, 1000)
	}
	return string(out)
}

func redactJSONValue(value any, depth int) any {
	if depth <= 0 {
		return "[truncated]"
	}
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		count := 0
		for key, item := range v {
			if count >= 30 {
				out["_truncated"] = fmt.Sprintf("%d more fields", len(v)-count)
				break
			}
			if isSensitiveKey(key) {
				out[key] = "[redacted]"
			} else {
				out[key] = redactJSONValue(item, depth-1)
			}
			count++
		}
		return out
	case []any:
		max := len(v)
		if max > 8 {
			max = 8
		}
		out := make([]any, 0, max)
		for i := 0; i < max; i++ {
			out = append(out, redactJSONValue(v[i], depth-1))
		}
		if len(v) > max {
			out = append(out, fmt.Sprintf("[%d more items]", len(v)-max))
		}
		return out
	case string:
		return truncateText(v, 500)
	default:
		return v
	}
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(key, "token") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "password") ||
		strings.Contains(key, "api_key") ||
		strings.Contains(key, "apikey") ||
		strings.Contains(key, "authorization")
}

func truncateText(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
