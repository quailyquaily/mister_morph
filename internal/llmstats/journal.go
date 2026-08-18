package llmstats

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
)

const (
	defaultJournalMaxFileBytes = 64 * 1024 * 1024
	journalDateLayout          = "2006-01-02"
)

var journalKeyRe = regexp.MustCompile(`^since-([0-9]{4}-[0-9]{2}-[0-9]{2})-([0-9]{4})\.jsonl$`)

type JournalOptions struct {
	MaxFileBytes int64
}

type Journal struct {
	dir  string
	opts JournalOptions

	now func() time.Time

	mu       sync.Mutex
	file     *os.File
	fileName string
	fileSize int64
	fileLine int64
}

type journalSegmentFile struct {
	Key   string
	Date  string
	Index int
}

func NewJournal(dir string, opts JournalOptions) *Journal {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = filepath.Join(".", "stats", "llm_usage")
	}
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = defaultJournalMaxFileBytes
	}
	return &Journal{dir: dir, opts: opts, now: time.Now}
}

func (j *Journal) Append(rec RequestRecord) (Offset, error) {
	rec = normalizeRequestRecord(rec)
	if err := validateRequestRecord(rec); err != nil {
		return Offset{}, err
	}
	payload, err := jsonLine(rec)
	if err != nil {
		return Offset{}, fmt.Errorf("usage journal append: encode record: %w", err)
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	if err := j.ensureWritableFileLocked(int64(len(payload))); err != nil {
		return Offset{}, err
	}
	if j.file == nil {
		return Offset{}, fmt.Errorf("usage journal append: no writable file")
	}

	prevLine := j.fileLine
	n, err := j.file.Write(payload)
	if err != nil {
		return Offset{}, err
	}
	if int64(n) != int64(len(payload)) {
		return Offset{}, fmt.Errorf("usage journal append: short write")
	}
	if err := j.file.Sync(); err != nil {
		return Offset{}, err
	}
	j.fileSize += int64(n)
	j.fileLine++
	return Offset{File: j.fileName, Line: prevLine + 1, Byte: j.fileSize}, nil
}

func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.closeFileLocked()
}

func (j *Journal) ensureWritableFileLocked(incomingBytes int64) error {
	if j.file == nil {
		if err := j.reopenLatestLocked(); err != nil {
			return err
		}
	}
	if j.opts.MaxFileBytes > 0 && j.fileSize > 0 && j.fileSize+incomingBytes > j.opts.MaxFileBytes {
		startDate := j.now().UTC().Format(journalDateLayout)
		if err := j.rotateToNewSegmentLocked(startDate); err != nil {
			return err
		}
	}
	return nil
}

func (j *Journal) reopenLatestLocked() error {
	if err := j.closeFileLocked(); err != nil {
		return err
	}
	if err := fsstore.EnsureDir(j.dir, 0o700); err != nil {
		return err
	}
	segments, err := listSegmentFiles(j.dir)
	if err != nil {
		return err
	}
	if len(segments) == 0 {
		date := j.now().UTC().Format(journalDateLayout)
		return j.openNewFileForDateAndIndexLocked(date, 1)
	}
	latest := segments[len(segments)-1]
	return j.openExistingFileForDateAndIndexLocked(latest.Date, latest.Index)
}

func (j *Journal) rotateToNewSegmentLocked(startDate string) error {
	maxIdx, err := maxIndexForDate(j.dir, startDate)
	if err != nil {
		return err
	}
	if err := j.closeFileLocked(); err != nil {
		return err
	}
	return j.openNewFileForDateAndIndexLocked(startDate, maxIdx+1)
}

func (j *Journal) openExistingFileForDateAndIndexLocked(date string, idx int) error {
	if err := fsstore.EnsureDir(j.dir, 0o700); err != nil {
		return err
	}
	name := formatJournalFileName(date, idx)
	path := filepath.Join(j.dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	lineCount, err := countLines(path)
	if err != nil {
		_ = file.Close()
		return err
	}
	j.file = file
	j.fileName = name
	j.fileSize = info.Size()
	j.fileLine = lineCount
	return nil
}

func (j *Journal) openNewFileForDateAndIndexLocked(date string, idx int) error {
	if err := fsstore.EnsureDir(j.dir, 0o700); err != nil {
		return err
	}
	name := formatJournalFileName(date, idx)
	path := filepath.Join(j.dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	j.file = file
	j.fileName = name
	j.fileSize = 0
	j.fileLine = 0
	return nil
}

func (j *Journal) closeFileLocked() error {
	if j.file == nil {
		return nil
	}
	err := j.file.Close()
	j.file = nil
	j.fileName = ""
	j.fileSize = 0
	j.fileLine = 0
	return err
}

func listSegmentFiles(dir string) ([]journalSegmentFile, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]journalSegmentFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		seg, ok := parseJournalSegmentFile(entry.Name())
		if !ok {
			continue
		}
		out = append(out, seg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func parseJournalSegmentFile(name string) (journalSegmentFile, bool) {
	name = strings.TrimSpace(name)
	m := journalKeyRe.FindStringSubmatch(name)
	if len(m) != 3 {
		return journalSegmentFile{}, false
	}
	idx, err := strconv.Atoi(m[2])
	if err != nil || idx <= 0 {
		return journalSegmentFile{}, false
	}
	return journalSegmentFile{Key: name, Date: m[1], Index: idx}, true
}

func formatJournalFileName(date string, idx int) string {
	return fmt.Sprintf("since-%s-%04d.jsonl", date, idx)
}

func maxIndexForDate(dir string, date string) (int, error) {
	segments, err := listSegmentFiles(dir)
	if err != nil {
		return 0, err
	}
	maxIdx := 0
	for _, seg := range segments {
		if seg.Date == date && seg.Index > maxIdx {
			maxIdx = seg.Index
		}
	}
	return maxIdx, nil
}

func countLines(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	var count int64
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			count++
		}
		if readErr != nil {
			if readErr == io.EOF {
				return count, nil
			}
			return 0, readErr
		}
	}
}

func jsonLine(v any) ([]byte, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	payload = append(payload, '\n')
	return payload, nil
}
