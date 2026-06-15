package domainjournal

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
)

const (
	segmentPrefix = "events."
	segmentSuffix = ".jsonl"
	firstSegment  = int64(1)
)

type Trace struct {
	TraceID string `json:"trace_id,omitempty"`
	Runtime string `json:"runtime,omitempty"`
	Target  string `json:"target,omitempty"`
	TopicID string `json:"topic_id,omitempty"`
	TaskID  string `json:"task_id,omitempty"`
}

type Event struct {
	ID            string          `json:"id"`
	Time          string          `json:"time"`
	Domain        string          `json:"domain"`
	Type          string          `json:"type"`
	SchemaVersion int             `json:"schema_version"`
	Trace         Trace           `json:"trace,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

type JournalOptions struct {
	Dir            string
	RotateMaxBytes int64
	SyncEachWrite  bool
}

type Journal struct {
	dir            string
	rotateMaxBytes int64
	syncEachWrite  bool
	lockPath       string
	mu             sync.Mutex
	closed         bool
}

type Cursor struct {
	File string `json:"file"`
	Line int64  `json:"line,omitempty"`
	Byte int64  `json:"byte,omitempty"`
}

type RecordRef struct {
	File string `json:"file"`
	Line int64  `json:"line,omitempty"`
	Byte int64  `json:"byte,omitempty"`
}

type Record struct {
	Cursor Cursor    `json:"cursor"`
	Ref    RecordRef `json:"ref"`
	Event  Event     `json:"event"`
}

type IndexRecord struct {
	Key string    `json:"key"`
	Ref RecordRef `json:"ref"`
}

func New(opts JournalOptions) (*Journal, error) {
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		return nil, fmt.Errorf("journal dir is required")
	}
	cleanDir := filepath.Clean(dir)
	if err := fsstore.EnsureDir(cleanDir, 0o700); err != nil {
		return nil, err
	}
	lockPath, err := fsstore.BuildLockPath(filepath.Join(cleanDir, ".locks"), "domainjournal")
	if err != nil {
		return nil, err
	}
	return &Journal{
		dir:            cleanDir,
		rotateMaxBytes: opts.RotateMaxBytes,
		syncEachWrite:  opts.SyncEachWrite,
		lockPath:       lockPath,
	}, nil
}

func (j *Journal) Append(event Event) (Cursor, error) {
	if j == nil {
		return Cursor{}, fmt.Errorf("journal is nil")
	}
	if err := ValidateEvent(event); err != nil {
		return Cursor{}, err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return Cursor{}, fmt.Errorf("encode journal event: %w", err)
	}
	payload = append(payload, '\n')

	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return Cursor{}, fmt.Errorf("journal is closed")
	}

	var cursor Cursor
	err = fsstore.WithLock(context.Background(), j.lockPath, func() error {
		next, ref, err := j.appendLineLocked(payload)
		if err != nil {
			return err
		}
		cursor = next
		return j.appendIndexRecordsLocked(event, ref)
	})
	if err != nil {
		return Cursor{}, err
	}
	return cursor, nil
}

func (j *Journal) Replay(fn func(Record) error) error {
	if j == nil {
		return fmt.Errorf("journal is nil")
	}
	if fn == nil {
		return fmt.Errorf("replay callback is required")
	}
	return j.ReplayFrom(Cursor{}, fn)
}

func (j *Journal) ReplayFrom(cursor Cursor, fn func(Record) error) error {
	if j == nil {
		return fmt.Errorf("journal is nil")
	}
	if fn == nil {
		return fmt.Errorf("replay callback is required")
	}
	files, err := j.listEventFiles()
	if err != nil {
		return err
	}
	start := 0
	if strings.TrimSpace(cursor.File) != "" {
		found := false
		for i, name := range files {
			if name == cursor.File {
				start = i
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("journal cursor file %q not found", cursor.File)
		}
	}
	for i := start; i < len(files); i++ {
		fileCursor := Cursor{}
		if i == start && strings.TrimSpace(cursor.File) != "" {
			fileCursor = cursor
		}
		if err := j.replayFile(files[i], fileCursor, fn); err != nil {
			return err
		}
	}
	return nil
}

func ReplayDir(dir string, fn func(Record) error) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("journal dir is required")
	}
	j := &Journal{dir: filepath.Clean(dir)}
	return j.Replay(fn)
}

func ReplayDirFrom(dir string, cursor Cursor, fn func(Record) error) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("journal dir is required")
	}
	j := &Journal{dir: filepath.Clean(dir)}
	return j.ReplayFrom(cursor, fn)
}

func ReadAtDir(dir string, ref RecordRef) (Record, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return Record{}, fmt.Errorf("journal dir is required")
	}
	j := &Journal{dir: filepath.Clean(dir)}
	return j.ReadAt(ref)
}

func ReadIndexDir(dir string, kind string, key string, limit int) ([]IndexRecord, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("journal dir is required")
	}
	j := &Journal{dir: filepath.Clean(dir)}
	return j.ReadIndex(kind, key, limit)
}

func (j *Journal) Close() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.closed = true
	return nil
}

func ValidateEvent(event Event) error {
	if err := validateRequiredCanonicalString("id", event.ID); err != nil {
		return err
	}
	if err := validateRequiredCanonicalString("time", event.Time); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339Nano, event.Time); err != nil {
		return fmt.Errorf("time must be RFC3339")
	}
	if err := validateRequiredCanonicalString("domain", event.Domain); err != nil {
		return err
	}
	if err := validateRequiredCanonicalString("type", event.Type); err != nil {
		return err
	}
	if event.SchemaVersion <= 0 {
		return fmt.Errorf("schema_version must be >= 1")
	}
	if len(event.Payload) == 0 {
		return fmt.Errorf("payload is required")
	}
	if !json.Valid(event.Payload) {
		return fmt.Errorf("payload must be valid JSON")
	}
	return nil
}

func (j *Journal) listEventFiles() ([]string, error) {
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, ok := segmentIndex(name); ok {
			files = append(files, name)
		}
	}
	sort.Slice(files, func(i, k int) bool {
		left, _ := segmentIndex(files[i])
		right, _ := segmentIndex(files[k])
		return left < right
	})
	return files, nil
}

func (j *Journal) replayFile(name string, cursor Cursor, fn func(Record) error) error {
	path := filepath.Join(j.dir, name)
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	startByte := cursor.Byte
	if startByte < 0 {
		return fmt.Errorf("%s: cursor byte must be >= 0", name)
	}
	if startByte > 0 {
		if _, err := file.Seek(startByte, io.SeekStart); err != nil {
			return err
		}
	}

	reader := bufio.NewReaderSize(file, 64*1024)
	var line int64
	if startByte > 0 {
		line = cursor.Line
	}
	pos := startByte
	for {
		start := pos
		raw, err := reader.ReadBytes('\n')
		if len(raw) == 0 && err == io.EOF {
			break
		}
		if err != nil && err != io.EOF {
			return fmt.Errorf("%s:%d: scan event: %w", name, line+1, err)
		}
		line++
		if startByte == 0 && cursor.Line > 0 && line <= cursor.Line {
			pos += int64(len(raw))
			if err == io.EOF {
				break
			}
			continue
		}
		if len(strings.TrimSpace(string(raw))) == 0 {
			pos += int64(len(raw))
			if err == io.EOF {
				break
			}
			continue
		}
		var event Event
		if err := json.Unmarshal(raw, &event); err != nil {
			return fmt.Errorf("%s:%d: decode event: %w", name, line, err)
		}
		if err := ValidateEvent(event); err != nil {
			return fmt.Errorf("%s:%d: invalid event: %w", name, line, err)
		}
		pos += int64(len(raw))
		if err := fn(Record{
			Cursor: Cursor{File: name, Line: line, Byte: pos},
			Ref:    RecordRef{File: name, Line: line, Byte: start},
			Event:  event,
		}); err != nil {
			return err
		}
		if err == io.EOF {
			break
		}
	}
	return nil
}

func (j *Journal) ReadAt(ref RecordRef) (Record, error) {
	if j == nil {
		return Record{}, fmt.Errorf("journal is nil")
	}
	if strings.TrimSpace(ref.File) == "" {
		return Record{}, fmt.Errorf("journal record ref file is required")
	}
	if _, ok := segmentIndex(ref.File); !ok {
		return Record{}, fmt.Errorf("journal record ref file %q is invalid", ref.File)
	}
	path := filepath.Join(j.dir, ref.File)
	file, err := os.Open(path)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = file.Close() }()
	if ref.Byte < 0 {
		return Record{}, fmt.Errorf("%s: record ref byte must be >= 0", ref.File)
	}
	if _, err := file.Seek(ref.Byte, io.SeekStart); err != nil {
		return Record{}, err
	}
	reader := bufio.NewReaderSize(file, 64*1024)
	raw, err := reader.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return Record{}, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return Record{}, fmt.Errorf("%s:%d: empty event at record ref", ref.File, ref.Line)
	}
	var event Event
	if err := json.Unmarshal(raw, &event); err != nil {
		return Record{}, fmt.Errorf("%s:%d: decode event: %w", ref.File, ref.Line, err)
	}
	if err := ValidateEvent(event); err != nil {
		return Record{}, fmt.Errorf("%s:%d: invalid event: %w", ref.File, ref.Line, err)
	}
	cursor := Cursor{File: ref.File, Line: ref.Line, Byte: ref.Byte + int64(len(raw))}
	return Record{Cursor: cursor, Ref: ref, Event: event}, nil
}

func (j *Journal) ReadIndex(kind string, key string, limit int) ([]IndexRecord, error) {
	if j == nil {
		return nil, fmt.Errorf("journal is nil")
	}
	kind = strings.TrimSpace(kind)
	key = strings.TrimSpace(key)
	if kind != "task" && kind != "topic" {
		return nil, fmt.Errorf("journal index kind %q is invalid", kind)
	}
	if key == "" {
		return nil, fmt.Errorf("journal index key is required")
	}
	if limit <= 0 {
		limit = 50
	}
	path := filepath.Join(j.dir, "index", kind, indexKeyFilename(key))
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()

	out := make([]IndexRecord, 0, limit)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var line int64
	for scanner.Scan() {
		line++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var rec IndexRecord
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			return nil, fmt.Errorf("%s:%d: decode index: %w", filepath.Join("index", kind, indexKeyFilename(key)), line, err)
		}
		if rec.Key != key {
			continue
		}
		out = append(out, rec)
		if len(out) > limit {
			copy(out, out[len(out)-limit:])
			out = out[:limit]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (j *Journal) appendLineLocked(payload []byte) (Cursor, RecordRef, error) {
	if err := fsstore.EnsureDir(j.dir, 0o700); err != nil {
		return Cursor{}, RecordRef{}, err
	}
	name, size, err := j.writableSegmentLocked(int64(len(payload)))
	if err != nil {
		return Cursor{}, RecordRef{}, err
	}
	path := filepath.Join(j.dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Cursor{}, RecordRef{}, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return Cursor{}, RecordRef{}, err
	}
	start := info.Size()
	if start != size {
		size = start
	}
	n, err := file.Write(payload)
	if err != nil {
		return Cursor{}, RecordRef{}, err
	}
	if n != len(payload) {
		return Cursor{}, RecordRef{}, fmt.Errorf("journal append: short write")
	}
	if j.syncEachWrite {
		if err := file.Sync(); err != nil {
			return Cursor{}, RecordRef{}, err
		}
	}
	return Cursor{File: name, Byte: size + int64(n)}, RecordRef{File: name, Byte: size}, nil
}

func (j *Journal) writableSegmentLocked(incomingBytes int64) (string, int64, error) {
	files, err := j.listEventFiles()
	if err != nil {
		return "", 0, err
	}
	if len(files) == 0 {
		return segmentName(firstSegment), 0, nil
	}
	latestName := files[len(files)-1]
	latestIndex, _ := segmentIndex(latestName)
	info, err := os.Stat(filepath.Join(j.dir, latestName))
	if err != nil {
		return "", 0, err
	}
	size := info.Size()
	if j.rotateMaxBytes > 0 && size > 0 && size+incomingBytes > j.rotateMaxBytes {
		return segmentName(latestIndex + 1), 0, nil
	}
	return latestName, size, nil
}

func (j *Journal) appendIndexRecordsLocked(event Event, ref RecordRef) error {
	keys := map[string]string{
		"task":  strings.TrimSpace(event.Trace.TaskID),
		"topic": strings.TrimSpace(event.Trace.TopicID),
	}
	for kind, key := range keys {
		if key == "" {
			continue
		}
		rec := IndexRecord{
			Key: key,
			Ref: ref,
		}
		raw, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		raw = append(raw, '\n')
		path := filepath.Join(j.dir, "index", kind, indexKeyFilename(key))
		if err := fsstore.EnsureDir(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		n, writeErr := file.Write(raw)
		if writeErr == nil && n != len(raw) {
			writeErr = fmt.Errorf("journal index append: short write")
		}
		if writeErr == nil && j.syncEachWrite {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func segmentName(index int64) string {
	return fmt.Sprintf("%s%018d%s", segmentPrefix, index, segmentSuffix)
}

func IsSegmentFile(name string) bool {
	_, ok := segmentIndex(name)
	return ok
}

func segmentIndex(name string) (int64, bool) {
	if !strings.HasPrefix(name, segmentPrefix) || !strings.HasSuffix(name, segmentSuffix) {
		return 0, false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(name, segmentPrefix), segmentSuffix)
	if len(raw) != 18 {
		return 0, false
	}
	idx, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || idx <= 0 {
		return 0, false
	}
	return idx, true
}

func indexKeyFilename(key string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(key)) + ".jsonl"
}

func validateRequiredCanonicalString(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain leading/trailing spaces", field)
	}
	return nil
}
