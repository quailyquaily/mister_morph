package llminspect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
)

type Options struct {
	Mode            string
	Task            string
	TimestampFormat string
	DumpDir         string
}

type PromptInspector struct {
	mu           sync.Mutex
	file         *os.File
	startedAt    time.Time
	mode         string
	task         string
	requestCount int
}

func NewPromptInspector(opts Options) (*PromptInspector, error) {
	startedAt := time.Now()
	dumpDir := strings.TrimSpace(opts.DumpDir)
	if dumpDir == "" {
		dumpDir = "dump"
	}
	if err := os.MkdirAll(dumpDir, 0o755); err != nil {
		return nil, fmt.Errorf("create dump dir: %w", err)
	}
	path := filepath.Join(dumpDir, buildFilename("prompt", opts.Mode, startedAt, opts.TimestampFormat))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open prompt dump file: %w", err)
	}
	inspector := &PromptInspector{
		file:      file,
		startedAt: startedAt,
		mode:      strings.TrimSpace(opts.Mode),
		task:      strings.TrimSpace(opts.Task),
	}
	if err := inspector.writeHeader(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return inspector, nil
}

func (p *PromptInspector) Close() error {
	if p == nil || p.file == nil {
		return nil
	}
	return p.file.Close()
}

func (p *PromptInspector) Dump(messages []llm.Message) error {
	if p == nil || p.file == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	p.requestCount++
	var b strings.Builder
	fmt.Fprintf(&b, "\n## Request #%d\n\n", p.requestCount)
	for i, msg := range messages {
		fmt.Fprintf(&b, "### Message #%d-%d\n\n", p.requestCount, i+1)
		b.WriteString("```\n")
		fmt.Fprintf(&b, "role: %s\n\n", msg.Role)
		if strings.TrimSpace(msg.ToolCallID) != "" {
			fmt.Fprintf(&b, "tool_call_id: %s\n\n", msg.ToolCallID)
		}
		if len(msg.ToolCalls) > 0 {
			toolCallsJSON, err := json.MarshalIndent(msg.ToolCalls, "", "  ")
			if err != nil {
				fmt.Fprintf(&b, "tool_calls: <error: %s>\n\n", err.Error())
			} else {
				fmt.Fprintf(&b, "tool_calls: %s\n\n", string(toolCallsJSON))
			}
		}
		fmt.Fprintf(&b, "content: %s\n", msg.Content)
		b.WriteString("```\n\n")
	}

	if _, err := p.file.WriteString(b.String()); err != nil {
		return err
	}
	return p.file.Sync()
}

func (p *PromptInspector) writeHeader() error {
	header := fmt.Sprintf(
		"---\nmode: %s\ntask: %s\ndatetime: %s\n---\n\n",
		strconv.Quote(p.mode),
		strconv.Quote(p.task),
		strconv.Quote(p.startedAt.Format(time.RFC3339)),
	)
	if _, err := p.file.WriteString(header); err != nil {
		return err
	}
	return p.file.Sync()
}

type RequestInspector struct {
	mu        sync.Mutex
	file      *os.File
	startedAt time.Time
	mode      string
	task      string
	count     int
}

func NewRequestInspector(opts Options) (*RequestInspector, error) {
	startedAt := time.Now()
	dumpDir := strings.TrimSpace(opts.DumpDir)
	if dumpDir == "" {
		dumpDir = "dump"
	}
	if err := os.MkdirAll(dumpDir, 0o755); err != nil {
		return nil, fmt.Errorf("create dump dir: %w", err)
	}
	path := filepath.Join(dumpDir, buildFilename("request", opts.Mode, startedAt, opts.TimestampFormat))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open request dump file: %w", err)
	}
	inspector := &RequestInspector{
		file:      file,
		startedAt: startedAt,
		mode:      strings.TrimSpace(opts.Mode),
		task:      strings.TrimSpace(opts.Task),
	}
	if err := inspector.writeHeader(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return inspector, nil
}

func (r *RequestInspector) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	return r.file.Close()
}

func (r *RequestInspector) Dump(label, payload string) {
	if r == nil || r.file == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.count++
	var b strings.Builder
	fmt.Fprintf(&b, "\n## Event #%d\n\n", r.count)
	fmt.Fprintf(&b, "### %s\n\n", label)
	b.WriteString("```\n")
	b.WriteString(payload)
	if !strings.HasSuffix(payload, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("```\n\n")

	_, _ = r.file.WriteString(b.String())
	_ = r.file.Sync()
}

func (r *RequestInspector) writeHeader() error {
	header := fmt.Sprintf(
		"---\nmode: %s\ntask: %s\ndatetime: %s\n---\n\n",
		strconv.Quote(r.mode),
		strconv.Quote(r.task),
		strconv.Quote(r.startedAt.Format(time.RFC3339)),
	)
	if _, err := r.file.WriteString(header); err != nil {
		return err
	}
	return r.file.Sync()
}

type PromptClient struct {
	Base      llm.Client
	Inspector *PromptInspector
}

func (c *PromptClient) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	if c == nil || c.Base == nil {
		return llm.Result{}, fmt.Errorf("inspect client is not initialized")
	}
	if c.Inspector != nil {
		if err := c.Inspector.Dump(req.Messages); err != nil {
			return llm.Result{}, err
		}
	}
	return c.Base.Chat(ctx, req)
}

func SetDebugHook(client llm.Client, dumpFn func(label, payload string)) error {
	setter, ok := client.(interface {
		SetDebugFn(func(label, payload string))
	})
	if !ok {
		return fmt.Errorf("client does not support debug hook")
	}
	setter.SetDebugFn(dumpFn)
	return nil
}

func buildFilename(kind string, mode string, t time.Time, tsFormat string) string {
	mode = strings.TrimSpace(mode)
	if tsFormat == "" {
		tsFormat = "20060102_1504"
	}
	ts := t.Format(tsFormat)
	if mode == "" {
		return fmt.Sprintf("%s_%s.md", kind, ts)
	}
	return fmt.Sprintf("%s_%s_%s.md", kind, mode, ts)
}
