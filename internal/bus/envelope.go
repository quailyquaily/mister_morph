package bus

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

type MessageEnvelope struct {
	MessageID string `json:"message_id"`
	Text      string `json:"text"`
	SentAt    string `json:"sent_at"`              // RFC3339
	SessionID string `json:"session_id,omitempty"` // dialogue topic required
	ReplyTo   string `json:"reply_to,omitempty"`
}

func (e MessageEnvelope) Validate(topic string) error {
	if err := validateRequiredCanonicalString("message_id", e.MessageID); err != nil {
		return err
	}
	if err := validateRequiredCanonicalString("text", e.Text); err != nil {
		return err
	}
	if err := validateRequiredCanonicalString("sent_at", e.SentAt); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339, e.SentAt); err != nil {
		return fmt.Errorf("sent_at must be RFC3339")
	}
	if IsDialogueTopic(topic) {
		if err := validateRequiredCanonicalString("session_id", e.SessionID); err != nil {
			return fmt.Errorf("session_id is required for dialogue topic %q", topic)
		}
	}
	if e.SessionID != "" {
		if err := validateUUIDv7Field("session_id", e.SessionID); err != nil {
			return err
		}
	}
	if e.ReplyTo != "" {
		if err := validateOptionalCanonicalString("reply_to", e.ReplyTo); err != nil {
			return err
		}
	}
	return nil
}

func EncodeMessageEnvelope(topic string, env MessageEnvelope) (string, error) {
	if err := env.Validate(topic); err != nil {
		return "", err
	}
	payload, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("marshal message envelope: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecodeMessageEnvelope(topic string, payloadBase64 string) (MessageEnvelope, error) {
	if err := validateRequiredCanonicalString("payload_base64", payloadBase64); err != nil {
		return MessageEnvelope{}, err
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadBase64)
	if err != nil {
		return MessageEnvelope{}, fmt.Errorf("payload_base64 decode failed: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(payloadBytes))
	dec.DisallowUnknownFields()

	var env MessageEnvelope
	if err := dec.Decode(&env); err != nil {
		return MessageEnvelope{}, fmt.Errorf("invalid message envelope json: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return MessageEnvelope{}, fmt.Errorf("invalid message envelope json: trailing data")
	}

	if err := env.Validate(topic); err != nil {
		return MessageEnvelope{}, err
	}
	return env, nil
}

func validateUUIDv7Field(field, value string) error {
	id, err := uuid.Parse(value)
	if err != nil {
		return fmt.Errorf("%s must be uuid_v7", field)
	}
	if id.Version() != uuid.Version(7) {
		return fmt.Errorf("%s must be uuid_v7", field)
	}
	return nil
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
