package llminspect

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
	uniaiapi "github.com/quailyquaily/uniai"
)

type Options struct {
	Mode            string
	Task            string
	TimestampFormat string
	DumpDir         string
	Prefix          string
}

type PromptInspector struct {
	mu           sync.Mutex
	file         *os.File
	startedAt    time.Time
	mode         string
	task         string
	requestCount int
}

const defaultInspectValue = "unknown"

//go:embed tmpl/prompt.md
var promptInspectorTemplateSource string

var promptInspectorTemplate = template.Must(template.New("prompt_inspector").Parse(promptInspectorTemplateSource))

type promptInspectorHeaderView struct {
	Mode     string
	Task     string
	Datetime string
}

type promptInspectorRequestView struct {
	RequestNumber int
	APIBase       string
	Model         string
	Scene         string
	Messages      []promptInspectorMessageView
}

type promptInspectorMessageView struct {
	Number        int
	Role          string
	HasToolCallID bool
	ToolCallID    string
	HasToolCalls  bool
	ToolCalls     string
	Content       string
}

type InspectMetadata struct {
	APIBase string
	Model   string
	Scene   string
}

func NewPromptInspector(opts Options) (*PromptInspector, error) {
	file, startedAt, err := openDumpFile(opts, "prompt")
	if err != nil {
		return nil, err
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

func (p *PromptInspector) DumpWithMetadata(meta InspectMetadata, messages []llm.Message) error {
	if p == nil || p.file == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	meta = normalizeInspectMetadata(meta)
	p.requestCount++

	view := promptInspectorRequestView{
		RequestNumber: p.requestCount,
		APIBase:       meta.APIBase,
		Model:         meta.Model,
		Scene:         meta.Scene,
		Messages:      make([]promptInspectorMessageView, 0, len(messages)),
	}
	for i, msg := range messages {
		mv := promptInspectorMessageView{
			Number:        i + 1,
			Role:          msg.Role,
			HasToolCallID: strings.TrimSpace(msg.ToolCallID) != "",
			ToolCallID:    msg.ToolCallID,
			Content:       msg.Content,
		}
		if len(msg.ToolCalls) > 0 {
			toolCallsJSON, err := json.MarshalIndent(msg.ToolCalls, "", "  ")
			if err != nil {
				mv.HasToolCalls = true
				mv.ToolCalls = fmt.Sprintf("<error: %s>", err.Error())
			} else {
				mv.HasToolCalls = true
				mv.ToolCalls = string(toolCallsJSON)
			}
		}
		view.Messages = append(view.Messages, mv)
	}

	var b strings.Builder
	if err := promptInspectorTemplate.ExecuteTemplate(&b, "request", view); err != nil {
		return fmt.Errorf("render prompt request dump: %w", err)
	}

	if _, err := p.file.WriteString(b.String()); err != nil {
		return err
	}
	return p.file.Sync()
}

func (p *PromptInspector) writeHeader() error {
	view := promptInspectorHeaderView{
		Mode:     strconv.Quote(p.mode),
		Task:     strconv.Quote(p.task),
		Datetime: strconv.Quote(p.startedAt.Format(time.RFC3339)),
	}
	var b strings.Builder
	if err := promptInspectorTemplate.ExecuteTemplate(&b, "header", view); err != nil {
		return fmt.Errorf("render prompt header dump: %w", err)
	}
	if _, err := p.file.WriteString(b.String()); err != nil {
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

type RequestEvent struct {
	inspector     *RequestInspector
	number        int
	meta          InspectMetadata
	itemCount     int
	headerWritten bool
}

func NewRequestInspector(opts Options) (*RequestInspector, error) {
	file, startedAt, err := openDumpFile(opts, "request")
	if err != nil {
		return nil, err
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

func (r *RequestInspector) NewEvent(meta InspectMetadata) *RequestEvent {
	if r == nil || r.file == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.count++
	return &RequestEvent{
		inspector: r,
		number:    r.count,
		meta:      normalizeInspectMetadata(meta),
	}
}

func (e *RequestEvent) Dump(label, payload string) {
	if e == nil || e.inspector == nil || e.inspector.file == nil {
		return
	}
	e.inspector.mu.Lock()
	defer e.inspector.mu.Unlock()

	payload = normalizeDumpDebugPayload(payload)
	e.itemCount++
	var b strings.Builder
	if !e.headerWritten {
		fmt.Fprintf(&b, "\n===[ Event #%d ]===========================\n", e.number)
		fmt.Fprintf(&b, "api_base: %s\n", e.meta.APIBase)
		fmt.Fprintf(&b, "model: %s\n", e.meta.Model)
		fmt.Fprintf(&b, "scene: `%s`\n\n", e.meta.Scene)
		e.headerWritten = true
	}
	fmt.Fprintf(&b, "---[ %s #%d-%d ]---------------------------\n", strings.TrimSpace(label), e.number, e.itemCount)
	b.WriteString("```\n")
	b.WriteString(payload)
	if !strings.HasSuffix(payload, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("```\n\n")

	_, _ = e.inspector.file.WriteString(b.String())
	_ = e.inspector.file.Sync()
}

func normalizeDumpDebugPayload(payload string) string {
	if normalized, ok := normalizeFencedJSONText(payload); ok {
		return normalized
	}
	if !strings.Contains(payload, "```") {
		return payload
	}

	var value any
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return payload
	}
	normalized, changed := normalizeFencedJSONStrings(value)
	if !changed {
		return payload
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return payload
	}
	return string(data)
}

func normalizeFencedJSONStrings(value any) (any, bool) {
	switch v := value.(type) {
	case string:
		if normalized, ok := normalizeFencedJSONText(v); ok {
			return normalized, true
		}
		return v, false
	case []any:
		changed := false
		for i := range v {
			normalized, ok := normalizeFencedJSONStrings(v[i])
			if ok {
				v[i] = normalized
				changed = true
			}
		}
		return v, changed
	case map[string]any:
		changed := false
		for key, item := range v {
			normalized, ok := normalizeFencedJSONStrings(item)
			if ok {
				v[key] = normalized
				changed = true
			}
		}
		return v, changed
	default:
		return v, false
	}
}

func normalizeFencedJSONText(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") {
		return "", false
	}
	candidates, err := uniaiapi.CollectJSONCandidates(trimmed)
	if err != nil {
		return "", false
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == trimmed {
			continue
		}
		if json.Valid([]byte(candidate)) {
			return candidate, true
		}
	}
	return "", false
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

type ClientOptions struct {
	PromptInspector  *PromptInspector
	RequestInspector *RequestInspector
	APIBase          string
	Model            string
}

type client struct {
	base             llm.Client
	promptInspector  *PromptInspector
	requestInspector *RequestInspector
	apiBase          string
	model            string
}

func WrapClient(base llm.Client, opts ClientOptions) llm.Client {
	if base == nil {
		return nil
	}
	if opts.PromptInspector == nil && opts.RequestInspector == nil {
		return base
	}
	return &client{
		base:             base,
		promptInspector:  opts.PromptInspector,
		requestInspector: opts.RequestInspector,
		apiBase:          opts.APIBase,
		model:            opts.Model,
	}
}

func (c *client) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	if c == nil || c.base == nil {
		return llm.Result{}, fmt.Errorf("inspect client is not initialized")
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(c.model)
	}
	meta := InspectMetadata{
		APIBase: c.apiBase,
		Model:   model,
		Scene:   req.Scene,
	}
	if c.promptInspector != nil {
		if err := c.promptInspector.DumpWithMetadata(meta, req.Messages); err != nil {
			return llm.Result{}, err
		}
	}
	if c.requestInspector != nil {
		if event := c.requestInspector.NewEvent(meta); event != nil {
			req.DebugFn = chainDebugFns(req.DebugFn, event.Dump)
		}
	}
	return c.base.Chat(ctx, req)
}

func (c *client) Close() error {
	if c == nil || c.base == nil {
		return nil
	}
	closer, ok := c.base.(io.Closer)
	if !ok {
		return nil
	}
	return closer.Close()
}

func normalizeInspectMetadata(meta InspectMetadata) InspectMetadata {
	meta.APIBase = strings.TrimSpace(meta.APIBase)
	if meta.APIBase == "" {
		meta.APIBase = defaultInspectValue
	}
	meta.Model = strings.TrimSpace(meta.Model)
	if meta.Model == "" {
		meta.Model = defaultInspectValue
	}
	meta.Scene = strings.TrimSpace(meta.Scene)
	if meta.Scene == "" {
		meta.Scene = defaultInspectValue
	}
	return meta
}

func openDumpFile(opts Options, kind string) (*os.File, time.Time, error) {
	startedAt := time.Now()
	dumpDir := strings.TrimSpace(opts.DumpDir)
	if dumpDir == "" {
		dumpDir = "dump"
	}
	if err := os.MkdirAll(dumpDir, 0o755); err != nil {
		return nil, time.Time{}, fmt.Errorf("create dump dir: %w", err)
	}
	path := filepath.Join(dumpDir, buildFilename(opts.Prefix, kind, opts.Mode, startedAt, opts.TimestampFormat))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("open %s dump file: %w", kind, err)
	}
	return file, startedAt, nil
}

func buildFilename(prefix, kind string, mode string, t time.Time, tsFormat string) string {
	prefix = strings.TrimSpace(prefix)
	mode = strings.TrimSpace(mode)
	if tsFormat == "" {
		tsFormat = "20060102_1504"
	}
	ts := t.Format(tsFormat)

	name := kind
	if prefix != "" {
		name = prefix + "-" + kind
	}
	if mode == "" {
		return fmt.Sprintf("%s_%s.md", name, ts)
	}
	return fmt.Sprintf("%s_%s_%s.md", name, mode, ts)
}

func chainDebugFns(fns ...func(label, payload string)) func(label, payload string) {
	active := make([]func(label, payload string), 0, len(fns))
	for _, fn := range fns {
		if fn != nil {
			active = append(active, fn)
		}
	}
	if len(active) == 0 {
		return nil
	}
	if len(active) == 1 {
		return active[0]
	}
	return func(label, payload string) {
		for _, fn := range active {
			fn(label, payload)
		}
	}
}
