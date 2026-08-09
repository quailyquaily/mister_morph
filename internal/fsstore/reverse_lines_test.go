package fsstore

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReadLineFilesPageUsesOpaqueCursorAcrossFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	newPath := filepath.Join(dir, "new.jsonl")
	oldPath := filepath.Join(dir, "old.jsonl")
	if err := os.WriteFile(newPath, []byte("new-1\nnew-2\nnew-3\nnew-4\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(new) error = %v", err)
	}
	if err := os.WriteFile(oldPath, []byte("old-1\nold-2\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(old) error = %v", err)
	}
	files := []LineFile{{ID: "new", Path: newPath}, {ID: "old", Path: oldPath}}

	first, err := ReadLineFilesPage(files, "", 2)
	if err != nil {
		t.Fatalf("ReadLineFilesPage(first) error = %v", err)
	}
	if first.FileID != "new" || strings.Join(first.Items, "\n") != "new-3\nnew-4" {
		t.Fatalf("first = %+v", first)
	}
	if !first.HasNext || first.NextCursor == "" {
		t.Fatalf("first page missing continuation: %+v", first)
	}
	if _, err := strconv.ParseInt(first.NextCursor, 10, 64); err == nil {
		t.Fatalf("cursor exposes a numeric file position: %q", first.NextCursor)
	}

	second, err := ReadLineFilesPage(files, first.NextCursor, 2)
	if err != nil {
		t.Fatalf("ReadLineFilesPage(second) error = %v", err)
	}
	if second.FileID != "new" || strings.Join(second.Items, "\n") != "new-1\nnew-2" || !second.HasNext {
		t.Fatalf("second = %+v", second)
	}

	third, err := ReadLineFilesPage(files, second.NextCursor, 2)
	if err != nil {
		t.Fatalf("ReadLineFilesPage(third) error = %v", err)
	}
	if third.FileID != "old" || strings.Join(third.Items, "\n") != "old-1\nold-2" || third.HasNext {
		t.Fatalf("third = %+v", third)
	}
}

func TestReadLinesBeforeReadsOnlyTailWindow(t *testing.T) {
	t.Parallel()

	prefix := bytes.Repeat([]byte(`{"msg":"old"}`+"\n"), 256*1024)
	data := append(prefix, []byte(`{"msg":"tail-1"}`+"\n"+`{"msg":"tail-2"}`+"\n")...)
	reader := &countingReaderAt{reader: bytes.NewReader(data)}

	page, err := readLinesBefore(reader, int64(len(data)), 2)
	if err != nil {
		t.Fatalf("readLinesBefore() error = %v", err)
	}
	if got := strings.Join(page.Lines, "\n"); got != `{"msg":"tail-1"}`+"\n"+`{"msg":"tail-2"}` {
		t.Fatalf("lines = %q", got)
	}
	if !page.HasOlder || page.StartOffset <= 0 {
		t.Fatalf("page = %+v, want older data and a start position", page)
	}
	if reader.bytesRead >= int64(len(data))/2 {
		t.Fatalf("bytes read = %d, file size = %d; tail read scanned too much", reader.bytesRead, len(data))
	}

	older, err := readLinesBefore(bytes.NewReader(data), page.StartOffset, 2)
	if err != nil {
		t.Fatalf("readLinesBefore(older) error = %v", err)
	}
	if len(older.Lines) != 2 {
		t.Fatalf("older lines = %#v, want two lines", older.Lines)
	}
}

type countingReaderAt struct {
	reader    io.ReaderAt
	bytesRead int64
}

func (r *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.reader.ReadAt(p, off)
	r.bytesRead += int64(n)
	return n, err
}
