package llm

import (
	"errors"
	"strings"
)

var ErrContextLength = errors.New("llm context length exceeded")

type contextLengthError struct {
	cause error
}

func (e contextLengthError) Error() string {
	if e.cause == nil {
		return ErrContextLength.Error()
	}
	return e.cause.Error()
}

func (e contextLengthError) Unwrap() error {
	return e.cause
}

func (e contextLengthError) Is(target error) bool {
	return target == ErrContextLength
}

func MarkContextLengthError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrContextLength) {
		return err
	}
	return contextLengthError{cause: err}
}

func IsContextLengthError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrContextLength) {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	for _, phrase := range []string{
		"maximum context length",
		"context length exceeded",
		"context_length_exceeded",
		"context window exceeded",
		"prompt is too long",
	} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	if strings.Contains(message, "too long") &&
		(strings.Contains(message, "context window") || strings.Contains(message, "requested model")) {
		return true
	}
	return strings.Contains(message, "input token count") &&
		strings.Contains(message, "exceed") &&
		strings.Contains(message, "maximum")
}
