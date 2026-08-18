package jsonutil

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/quailyquaily/uniai"
)

var (
	ErrEmptyInput       = errors.New("empty json input")
	ErrNoJSONCandidates = errors.New("no json candidates")
)

// FindJSONPayload attempts to locate a valid JSON payload in the input text.
// It uses uniai helpers to collect and repair candidates, returning the first
// candidate that parses as JSON.
func FindJSONPayload(text string) ([]byte, error) {
	candidates, err := FindJSONCandidates(text)
	if err != nil {
		return nil, err
	}
	return candidates[0], nil
}

// FindJSONCandidates extracts and repairs every distinct candidate that parses as JSON.
// Callers remain responsible for validating their own response schema.
func FindJSONCandidates(text string) ([][]byte, error) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return nil, ErrEmptyInput
	}

	candidates := collectCandidates(raw)
	out := make([][]byte, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	var lastErr error
	for _, cand := range candidates {
		for _, variant := range candidateVariants(cand) {
			if seen[variant] {
				continue
			}
			var decoded any
			if err := json.Unmarshal([]byte(variant), &decoded); err != nil {
				lastErr = err
				continue
			}
			seen[variant] = true
			out = append(out, []byte(variant))
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrNoJSONCandidates
}

// DecodeWithFallback finds a JSON payload and unmarshals it into dst.
func DecodeWithFallback(text string, dst any) error {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return ErrEmptyInput
	}

	// Fast path: most providers already return plain JSON when ForceJSON is on.
	if err := json.Unmarshal([]byte(raw), dst); err == nil {
		return nil
	}

	data, err := FindJSONPayload(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func collectCandidates(raw string) []string {
	out := make([]string, 0, 8)
	seen := make(map[string]bool, 8)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}

	add(raw)

	if cands, err := uniai.CollectJSONCandidates(raw); err == nil {
		for _, c := range cands {
			add(c)
		}
	}
	for _, c := range uniai.FindJSONSnippets(raw) {
		add(c)
	}

	return out
}

func candidateVariants(candidate string) []string {
	out := make([]string, 0, 4)
	seen := make(map[string]bool, 4)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}

	add(candidate)

	stripped := strings.TrimSpace(uniai.StripNonJSONLines(candidate))
	add(stripped)

	repaired := strings.TrimSpace(uniai.AttemptJSONRepair(candidate))
	add(repaired)

	if stripped != "" && stripped != candidate {
		repairedStripped := strings.TrimSpace(uniai.AttemptJSONRepair(stripped))
		add(repairedStripped)
	}

	return out
}
