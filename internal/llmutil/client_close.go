package llmutil

import (
	"errors"
	"io"
	"reflect"

	"github.com/quailyquaily/mistermorph/llm"
)

// closeDistinctClients closes every owned client once. Route construction can
// legitimately reuse one client for more than one route entry.
func closeDistinctClients(clients ...llm.Client) error {
	seen := make(map[io.Closer]struct{}, len(clients))
	var errs []error
	for _, client := range clients {
		closer, ok := client.(io.Closer)
		if !ok || closer == nil {
			continue
		}
		closerType := reflect.TypeOf(closer)
		if closerType != nil && closerType.Comparable() {
			if _, exists := seen[closer]; exists {
				continue
			}
			seen[closer] = struct{}{}
		}
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
