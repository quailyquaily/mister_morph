package guard

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type JSONLAuditSink struct {
	Path          string
	RotateMaxBytes int64

	mu   sync.Mutex
	f    *os.File
	w    *bufio.Writer
	size int64
}

func NewJSONLAuditSink(path string, rotateMaxBytes int64) (*JSONLAuditSink, error) {
	path = stringsTrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("missing jsonl path")
	}
	if rotateMaxBytes <= 0 {
		rotateMaxBytes = 100 * 1024 * 1024
	}
	s := &JSONLAuditSink{
		Path:           path,
		RotateMaxBytes: rotateMaxBytes,
	}
	if err := s.openLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *JSONLAuditSink) Emit(ctx context.Context, e AuditEvent) error {
	_ = ctx
	if s == nil {
		return nil
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.rotateIfNeededLocked(int64(len(b)) + 1); err != nil {
		return err
	}
	if s.w == nil {
		return fmt.Errorf("audit sink is not initialized")
	}
	n, err := s.w.Write(append(b, '\n'))
	if err != nil {
		return err
	}
	s.size += int64(n)
	return s.w.Flush()
}

func (s *JSONLAuditSink) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w != nil {
		_ = s.w.Flush()
	}
	if s.f != nil {
		err := s.f.Close()
		s.f = nil
		s.w = nil
		s.size = 0
		return err
	}
	return nil
}

func (s *JSONLAuditSink) openLocked() error {
	dir := filepath.Dir(s.Path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}

	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	var st os.FileInfo
	st, err = f.Stat()
	if err == nil {
		s.size = st.Size()
	}
	s.f = f
	s.w = bufio.NewWriterSize(f, 64*1024)
	return nil
}

func (s *JSONLAuditSink) rotateIfNeededLocked(addBytes int64) error {
	if s.RotateMaxBytes <= 0 {
		return nil
	}
	if s.size+addBytes <= s.RotateMaxBytes {
		return nil
	}

	if s.w != nil {
		_ = s.w.Flush()
	}
	if s.f != nil {
		_ = s.f.Close()
	}

	ts := time.Now().UTC().Format("20060102T150405Z")
	rotated := fmt.Sprintf("%s.%s", s.Path, ts)
	if err := os.Rename(s.Path, rotated); err != nil {
		// If rename fails, try reopening without rotation.
		return s.openLocked()
	}
	s.f = nil
	s.w = nil
	s.size = 0
	return s.openLocked()
}

func stringsTrimSpace(s string) string {
	// Avoid importing strings in multiple guard files just for TrimSpace.
	i := 0
	j := len(s)
	for i < j && (s[i] == ' ' || s[i] == '\n' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\n' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

