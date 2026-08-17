package agentpair

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const controlPrefix = "morph-agent-pair:v1:"

type pairOffer struct {
	PairID    string `json:"pair_id"`
	From      string `json:"from"`
	To        string `json:"to"`
	ExpiresAt string `json:"expires_at"`
}

func IsControlMessage(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), controlPrefix)
}

func encodeOffer(offer pairOffer) (string, error) {
	raw, err := json.Marshal(offer)
	if err != nil {
		return "", err
	}
	return controlPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeOffer(text string) (pairOffer, time.Time, bool, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, controlPrefix) {
		return pairOffer{}, time.Time{}, false, nil
	}
	payload := strings.TrimPrefix(text, controlPrefix)
	if payload == "" || strings.ContainsAny(payload, " \t\r\n") {
		return pairOffer{}, time.Time{}, true, fmt.Errorf("invalid Agent pair control message")
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return pairOffer{}, time.Time{}, true, fmt.Errorf("decode Agent pair offer: %w", err)
	}
	var offer pairOffer
	if err := json.Unmarshal(raw, &offer); err != nil {
		return pairOffer{}, time.Time{}, true, fmt.Errorf("decode Agent pair offer: %w", err)
	}
	if _, err := uuid.Parse(strings.TrimSpace(offer.PairID)); err != nil {
		return pairOffer{}, time.Time{}, true, fmt.Errorf("invalid Agent pair id")
	}
	from, err := normalizeStableIdentity(offer.From)
	if err != nil {
		return pairOffer{}, time.Time{}, true, fmt.Errorf("invalid Agent pair sender: %w", err)
	}
	to, err := normalizeReference(offer.To)
	if err != nil {
		return pairOffer{}, time.Time{}, true, fmt.Errorf("invalid Agent pair target: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(offer.ExpiresAt))
	if err != nil {
		return pairOffer{}, time.Time{}, true, fmt.Errorf("invalid Agent pair expiry")
	}
	offer.PairID = strings.TrimSpace(offer.PairID)
	offer.From = from
	offer.To = to
	offer.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)
	return offer, expiresAt.UTC(), true, nil
}
