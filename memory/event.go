package memory

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
)

const CurrentMemoryEventSchemaVersion = 3

type SessionContext struct {
	ConversationID     string `json:"conversation_id,omitempty"`
	ConversationType   string `json:"conversation_type,omitempty"`
	CounterpartyID     string `json:"counterparty_id,omitempty"`
	CounterpartyName   string `json:"counterparty_name,omitempty"`
	CounterpartyHandle string `json:"counterparty_handle,omitempty"`
	CounterpartyLabel  string `json:"counterparty_label,omitempty"`
}

type MemoryEvent struct {
	SchemaVersion  int                           `json:"schema_version"`
	EventID        string                        `json:"event_id"`
	TaskRunID      string                        `json:"task_run_id"`
	TSUTC          string                        `json:"ts_utc"`
	SessionID      string                        `json:"session_id"`
	SubjectID      string                        `json:"subject_id"`
	Channel        string                        `json:"channel"`
	Participants   []MemoryParticipant           `json:"participants"`
	TaskText       string                        `json:"task_text"`
	FinalOutput    string                        `json:"final_output"`
	SourceHistory  []chathistory.ChatHistoryItem `json:"source_history,omitempty"`
	SessionContext SessionContext                `json:"session_context,omitempty"`
}

type MemoryParticipant struct {
	ID       any    `json:"id"`
	Nickname string `json:"nickname"`
	Protocol string `json:"protocol"`
}

func (e MemoryEvent) ValidateForAppend() error {
	return ValidateMemoryEventForAppend(e)
}

func ValidateMemoryEventForAppend(e MemoryEvent) error {
	if e.SchemaVersion <= 0 {
		return fmt.Errorf("schema_version must be >= 1")
	}
	if err := validateRequiredCanonicalString("event_id", e.EventID); err != nil {
		return err
	}
	if err := validateRequiredCanonicalString("task_run_id", e.TaskRunID); err != nil {
		return err
	}
	if err := validateRequiredCanonicalString("ts_utc", e.TSUTC); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339, e.TSUTC); err != nil {
		return fmt.Errorf("ts_utc must be RFC3339")
	}
	if err := validateRequiredCanonicalString("session_id", e.SessionID); err != nil {
		return err
	}
	if err := validateRequiredCanonicalString("subject_id", e.SubjectID); err != nil {
		return err
	}
	if err := validateRequiredCanonicalString("channel", e.Channel); err != nil {
		return err
	}
	for i, p := range e.Participants {
		if err := validateMemoryParticipant(i, p); err != nil {
			return err
		}
	}
	return nil
}

func validateMemoryParticipant(index int, p MemoryParticipant) error {
	nicknameField := fmt.Sprintf("participants[%d].nickname", index)
	protocolField := fmt.Sprintf("participants[%d].protocol", index)
	idField := fmt.Sprintf("participants[%d].id", index)

	if err := validateRequiredCanonicalString(nicknameField, p.Nickname); err != nil {
		return err
	}
	if err := validateOptionalCanonicalString(protocolField, p.Protocol); err != nil {
		return err
	}

	if p.Protocol == "" {
		ok, err := isNumericZeroID(p.ID)
		if err != nil {
			return fmt.Errorf("%s: %w", idField, err)
		}
		if !ok {
			return fmt.Errorf("participants[%d] agent-self marker requires id=0 when protocol is empty", index)
		}
		return nil
	}

	if err := validateParticipantID(idField, p.ID); err != nil {
		return err
	}
	ok, err := isNumericZeroID(p.ID)
	if err != nil {
		return fmt.Errorf("%s: %w", idField, err)
	}
	if ok {
		return fmt.Errorf("participants[%d].id=0 is reserved for agent-self marker when protocol is empty", index)
	}
	return nil
}

func validateParticipantID(field string, id any) error {
	switch v := id.(type) {
	case nil:
		return fmt.Errorf("%s is required", field)
	case string:
		if err := validateRequiredCanonicalString(field, v); err != nil {
			return err
		}
		return nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return nil
	case float32:
		if !isFiniteWholeNumber(float64(v)) {
			return fmt.Errorf("%s must be integer-like number", field)
		}
		return nil
	case float64:
		if !isFiniteWholeNumber(v) {
			return fmt.Errorf("%s must be integer-like number", field)
		}
		return nil
	case json.Number:
		if _, err := v.Int64(); err == nil {
			return nil
		}
		f, err := strconv.ParseFloat(v.String(), 64)
		if err != nil || !isFiniteWholeNumber(f) {
			return fmt.Errorf("%s must be integer-like number", field)
		}
		return nil
	default:
		return fmt.Errorf("%s has unsupported type %T", field, id)
	}
}

func isNumericZeroID(id any) (bool, error) {
	switch v := id.(type) {
	case int:
		return v == 0, nil
	case int8:
		return v == 0, nil
	case int16:
		return v == 0, nil
	case int32:
		return v == 0, nil
	case int64:
		return v == 0, nil
	case uint:
		return v == 0, nil
	case uint8:
		return v == 0, nil
	case uint16:
		return v == 0, nil
	case uint32:
		return v == 0, nil
	case uint64:
		return v == 0, nil
	case float32:
		f := float64(v)
		if !isFiniteWholeNumber(f) {
			return false, fmt.Errorf("id must be integer-like number")
		}
		return f == 0, nil
	case float64:
		if !isFiniteWholeNumber(v) {
			return false, fmt.Errorf("id must be integer-like number")
		}
		return v == 0, nil
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i == 0, nil
		}
		f, err := strconv.ParseFloat(v.String(), 64)
		if err != nil || !isFiniteWholeNumber(f) {
			return false, fmt.Errorf("id must be integer-like number")
		}
		return f == 0, nil
	case string:
		if strings.TrimSpace(v) != v {
			return false, fmt.Errorf("id must not contain leading/trailing spaces")
		}
		return false, nil
	case nil:
		return false, fmt.Errorf("id is required")
	default:
		return false, fmt.Errorf("id has unsupported type %T", id)
	}
}

func isFiniteWholeNumber(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Trunc(v) == v
}

func validateRequiredCanonicalString(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain leading/trailing spaces", field)
	}
	return nil
}

func validateOptionalCanonicalString(field, value string) error {
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain leading/trailing spaces", field)
	}
	return nil
}
