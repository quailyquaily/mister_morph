package fsstore

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pagination"
)

const reverseLineBlockSize int64 = 64 * 1024
const lineFilesCursorKind = "fsstore.line-files"

type LineFile struct {
	ID   string
	Path string
}

type LineFilePage struct {
	pagination.Page[string]
	FileID    string
	Exists    bool
	SizeBytes int64
	ModTime   time.Time
}

type lineFilesCursor struct {
	FileID   string `json:"file"`
	Position int64  `json:"position"`
}

type reverseLinePage struct {
	Lines       []string
	StartOffset int64
	HasOlder    bool
}

type byteLineRange struct {
	start int
	end   int
}

// ReadLineFilesPage reads one page from files ordered newest to oldest. The
// returned cursor is opaque and can be passed back unchanged for the next page.
func ReadLineFilesPage(files []LineFile, rawCursor string, limit int) (LineFilePage, error) {
	result := LineFilePage{Page: pagination.PageWithCursor([]string{}, limit, "")}
	if limit <= 0 {
		return result, fmt.Errorf("line page limit must be > 0")
	}
	if len(files) == 0 {
		return result, nil
	}

	startIndex := 0
	position := int64(-1)
	if strings.TrimSpace(rawCursor) != "" {
		var cursor lineFilesCursor
		if err := pagination.DecodeCursor(rawCursor, lineFilesCursorKind, &cursor); err != nil {
			return result, err
		}
		cursor.FileID = strings.TrimSpace(cursor.FileID)
		if cursor.FileID == "" || cursor.Position < -1 {
			return result, pagination.ErrInvalidCursor
		}
		startIndex = -1
		for i, file := range files {
			if strings.TrimSpace(file.ID) == cursor.FileID {
				startIndex = i
				break
			}
		}
		if startIndex < 0 {
			return result, pagination.ErrInvalidCursor
		}
		position = cursor.Position
	}

	for i := startIndex; i < len(files); i++ {
		file := files[i]
		file.ID = strings.TrimSpace(file.ID)
		file.Path = strings.TrimSpace(file.Path)
		if file.ID == "" || file.Path == "" {
			return result, fmt.Errorf("line file id and path are required")
		}
		result.FileID = file.ID
		fd, err := os.Open(file.Path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if i == len(files)-1 {
					return result, nil
				}
				position = -1
				continue
			}
			return result, err
		}
		info, err := fd.Stat()
		if err != nil {
			_ = fd.Close()
			return result, err
		}
		if info.IsDir() {
			_ = fd.Close()
			return result, fmt.Errorf("line file path is a directory")
		}

		result.Exists = true
		result.SizeBytes = info.Size()
		result.ModTime = info.ModTime().UTC()
		end := info.Size()
		if position >= 0 {
			if position > end {
				_ = fd.Close()
				return result, pagination.ErrInvalidCursor
			}
			end = position
		}
		page, err := readLinesBefore(fd, end, limit)
		closeErr := fd.Close()
		if err != nil {
			return result, err
		}
		if closeErr != nil {
			return result, closeErr
		}

		nextCursor := ""
		if page.HasOlder {
			nextCursor, err = pagination.EncodeCursor(lineFilesCursorKind, lineFilesCursor{
				FileID:   file.ID,
				Position: page.StartOffset,
			})
		} else if i+1 < len(files) {
			nextCursor, err = pagination.EncodeCursor(lineFilesCursorKind, lineFilesCursor{
				FileID:   strings.TrimSpace(files[i+1].ID),
				Position: -1,
			})
		}
		if err != nil {
			return result, err
		}
		result.Page = pagination.PageWithCursor(page.Lines, limit, nextCursor)
		if len(page.Lines) > 0 || i == len(files)-1 {
			return result, nil
		}
		position = -1
	}
	return result, nil
}

func readLinesBefore(reader io.ReaderAt, endOffset int64, limit int) (reverseLinePage, error) {
	page := reverseLinePage{Lines: []string{}}
	if reader == nil {
		return page, fmt.Errorf("missing line reader")
	}
	if endOffset < 0 {
		return page, fmt.Errorf("invalid end offset")
	}
	if limit <= 0 {
		return page, nil
	}

	loadedStart := endOffset
	data := []byte{}
	for {
		ranges := completeNonEmptyLineRanges(data, loadedStart > 0)
		if len(ranges) > limit || loadedStart == 0 {
			if len(ranges) == 0 {
				return page, nil
			}
			selectedStart := len(ranges) - limit
			if selectedStart < 0 {
				selectedStart = 0
			}
			selected := ranges[selectedStart:]
			page.Lines = make([]string, 0, len(selected))
			for _, line := range selected {
				page.Lines = append(page.Lines, string(data[line.start:line.end]))
			}
			page.StartOffset = loadedStart + int64(selected[0].start)
			page.HasOlder = selectedStart > 0
			return page, nil
		}

		readStart := loadedStart - reverseLineBlockSize
		if readStart < 0 {
			readStart = 0
		}
		block := make([]byte, loadedStart-readStart)
		n, err := reader.ReadAt(block, readStart)
		if err != nil && err != io.EOF {
			return page, err
		}
		if n == 0 {
			loadedStart = 0
			continue
		}
		next := make([]byte, n+len(data))
		copy(next, block[:n])
		copy(next[n:], data)
		data = next
		loadedStart = readStart
	}
}

func completeNonEmptyLineRanges(data []byte, skipFirst bool) []byteLineRange {
	ranges := []byteLineRange{}
	lineStart := 0
	appendLine := func(lineEnd int) {
		if skipFirst && lineStart == 0 {
			return
		}
		if lineEnd > lineStart && data[lineEnd-1] == '\r' {
			lineEnd--
		}
		if lineEnd > lineStart {
			ranges = append(ranges, byteLineRange{start: lineStart, end: lineEnd})
		}
	}
	for i, value := range data {
		if value != '\n' {
			continue
		}
		appendLine(i)
		lineStart = i + 1
	}
	if lineStart < len(data) {
		appendLine(len(data))
	}
	return ranges
}
