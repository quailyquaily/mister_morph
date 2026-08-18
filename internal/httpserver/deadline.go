package httpserver

import (
	"errors"
	"net/http"
	"time"
)

const (
	ReadHeaderTimeout     = 5 * time.Second
	IdleTimeout           = 60 * time.Second
	BodyReadTimeout       = 30 * time.Second
	UploadBodyReadTimeout = 5 * time.Minute
)

// WithBodyReadDeadline applies a request-body deadline selected by the caller.
// Returning a non-positive duration leaves stream and download routes relaxed.
func WithBodyReadDeadline(next http.Handler, timeoutFor func(*http.Request) time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if timeoutFor != nil {
			timeout := timeoutFor(r)
			if timeout > 0 {
				controller := http.NewResponseController(w)
				if err := controller.SetReadDeadline(time.Now().Add(timeout)); err != nil {
					if !errors.Is(err, http.ErrNotSupported) {
						http.Error(w, "could not set request body deadline", http.StatusServiceUnavailable)
						return
					}
				} else {
					defer func() { _ = controller.SetReadDeadline(time.Time{}) }()
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
