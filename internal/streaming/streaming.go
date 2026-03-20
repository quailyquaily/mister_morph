package streaming

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
)

type ReplySink interface {
	Update(ctx context.Context, text string) error
	Finalize(ctx context.Context, text string) error
	Abort(ctx context.Context, err error) error
}

type PartialFinalOutput struct {
	ResponseType   string
	Output         string
	OutputStarted  bool
	OutputComplete bool
}

type FinalOutputStreamerOptions struct {
	Sink        ReplySink
	MinInterval time.Duration
	Next        llm.StreamHandler
	Now         func() time.Time
}

type FinalOutputStreamer struct {
	mu          sync.Mutex
	sink        ReplySink
	minInterval time.Duration
	next        llm.StreamHandler
	now         func() time.Time

	buffer      strings.Builder
	lastText    string
	lastEmitAt  time.Time
	pendingText string
}

func NewFinalOutputStreamer(opts FinalOutputStreamerOptions) *FinalOutputStreamer {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	minInterval := opts.MinInterval
	if minInterval <= 0 {
		minInterval = 250 * time.Millisecond
	}
	return &FinalOutputStreamer{
		sink:        opts.Sink,
		minInterval: minInterval,
		next:        opts.Next,
		now:         now,
	}
}

func (s *FinalOutputStreamer) Handle(event llm.StreamEvent) error {
	if s == nil {
		return nil
	}

	var sinkErr error
	if s.sink != nil {
		sinkErr = s.handleSinkEvent(event)
	}
	if sinkErr != nil {
		return sinkErr
	}
	if s.next != nil {
		return s.next(event)
	}
	return nil
}

func (s *FinalOutputStreamer) handleSinkEvent(event llm.StreamEvent) error {
	s.mu.Lock()
	if event.Delta != "" {
		_, _ = s.buffer.WriteString(event.Delta)
	}
	snapshot := ExtractPartialFinalOutput(s.buffer.String())
	emitText, shouldEmit := s.nextEmissionLocked(snapshot, event.Done)
	reset := event.Done
	if reset {
		s.buffer.Reset()
		s.pendingText = ""
	}
	s.mu.Unlock()

	if !shouldEmit || strings.TrimSpace(emitText) == "" {
		return nil
	}
	return s.sink.Update(context.Background(), emitText)
}

func (s *FinalOutputStreamer) nextEmissionLocked(snapshot PartialFinalOutput, force bool) (string, bool) {
	if !isFinalResponseType(snapshot.ResponseType) || !snapshot.OutputStarted {
		if force {
			s.lastText = ""
			s.lastEmitAt = time.Time{}
		}
		return "", false
	}
	text := snapshot.Output
	if text == s.lastText || text == s.pendingText {
		if force && text != "" && text != s.lastText {
			s.lastText = text
			s.lastEmitAt = s.now().UTC()
			s.pendingText = ""
			return text, true
		}
		return "", false
	}
	now := s.now().UTC()
	if force || s.lastEmitAt.IsZero() || now.Sub(s.lastEmitAt) >= s.minInterval {
		s.lastText = text
		s.lastEmitAt = now
		s.pendingText = ""
		return text, true
	}
	s.pendingText = text
	return "", false
}

func ExtractPartialFinalOutput(raw string) PartialFinalOutput {
	text := strings.TrimSpace(raw)
	if text == "" {
		return PartialFinalOutput{}
	}

	pos := strings.IndexByte(text, '{')
	if pos < 0 {
		return PartialFinalOutput{}
	}
	pos++

	out := PartialFinalOutput{}
	for pos < len(text) {
		pos = skipWhitespace(text, pos)
		if pos >= len(text) {
			return out
		}
		if text[pos] == '}' {
			return out
		}

		key, next, complete := parseJSONStringPartial(text, pos)
		if !complete {
			return out
		}
		pos = skipWhitespace(text, next)
		if pos >= len(text) || text[pos] != ':' {
			return out
		}
		pos = skipWhitespace(text, pos+1)
		if pos >= len(text) {
			return out
		}

		switch text[pos] {
		case '"':
			value, nextPos, valueComplete := parseJSONStringPartial(text, pos)
			switch key {
			case "type":
				if valueComplete {
					out.ResponseType = value
				}
			case "output":
				out.OutputStarted = true
				out.Output = value
				out.OutputComplete = valueComplete
				return out
			}
			if !valueComplete {
				return out
			}
			pos = nextPos
		case '{', '[':
			nextPos, valueComplete := skipCompositeValue(text, pos)
			if !valueComplete {
				return out
			}
			pos = nextPos
		default:
			nextPos, valueComplete := skipScalarValue(text, pos)
			if !valueComplete {
				return out
			}
			pos = nextPos
		}

		pos = skipWhitespace(text, pos)
		if pos >= len(text) {
			return out
		}
		if text[pos] == ',' {
			pos++
			continue
		}
		if text[pos] == '}' {
			return out
		}
		return out
	}
	return out
}

func isFinalResponseType(value string) bool {
	switch strings.TrimSpace(value) {
	case "final", "final_answer":
		return true
	default:
		return false
	}
}

func skipWhitespace(text string, pos int) int {
	for pos < len(text) {
		switch text[pos] {
		case ' ', '\n', '\r', '\t':
			pos++
		default:
			return pos
		}
	}
	return pos
}

func parseJSONStringPartial(text string, pos int) (string, int, bool) {
	if pos >= len(text) || text[pos] != '"' {
		return "", pos, false
	}
	pos++
	var out strings.Builder
	for pos < len(text) {
		ch := text[pos]
		switch ch {
		case '"':
			return out.String(), pos + 1, true
		case '\\':
			pos++
			if pos >= len(text) {
				return out.String(), len(text), false
			}
			decoded, width, ok := decodeEscape(text[pos:])
			if !ok {
				return out.String(), len(text), false
			}
			out.WriteString(decoded)
			pos += width
		default:
			out.WriteByte(ch)
			pos++
		}
	}
	return out.String(), len(text), false
}

func decodeEscape(text string) (string, int, bool) {
	if text == "" {
		return "", 0, false
	}
	switch text[0] {
	case '"', '\\', '/':
		return string(text[0]), 1, true
	case 'b':
		return "\b", 1, true
	case 'f':
		return "\f", 1, true
	case 'n':
		return "\n", 1, true
	case 'r':
		return "\r", 1, true
	case 't':
		return "\t", 1, true
	case 'u':
		if len(text) < 5 {
			return "", 0, false
		}
		r, ok := decodeUnicodeEscape(text[1:5])
		if !ok {
			return "", 0, false
		}
		return string(r), 5, true
	default:
		return string(text[0]), 1, true
	}
}

func decodeUnicodeEscape(text string) (rune, bool) {
	if len(text) != 4 {
		return 0, false
	}
	var out rune
	for i := 0; i < len(text); i++ {
		out <<= 4
		switch ch := text[i]; {
		case ch >= '0' && ch <= '9':
			out |= rune(ch - '0')
		case ch >= 'a' && ch <= 'f':
			out |= rune(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			out |= rune(ch-'A') + 10
		default:
			return 0, false
		}
	}
	return out, true
}

func skipCompositeValue(text string, pos int) (int, bool) {
	if pos >= len(text) {
		return pos, false
	}
	open := text[pos]
	close := byte('}')
	if open == '[' {
		close = ']'
	}
	depth := 0
	inString := false
	escaped := false
	for pos < len(text) {
		ch := text[pos]
		if inString {
			if escaped {
				escaped = false
				pos++
				continue
			}
			if ch == '\\' {
				escaped = true
				pos++
				continue
			}
			if ch == '"' {
				inString = false
			}
			pos++
			continue
		}
		switch ch {
		case '"':
			inString = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return pos + 1, true
			}
		}
		pos++
	}
	return len(text), false
}

func skipScalarValue(text string, pos int) (int, bool) {
	for pos < len(text) {
		switch text[pos] {
		case ',', '}':
			return pos, true
		default:
			pos++
		}
	}
	return len(text), true
}
