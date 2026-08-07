package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/entryutil"
)

const (
	defaultProjectorCheckpointBatch = 10
)

type ProjectorOptions struct {
	CheckpointBatch  int
	SemanticResolver entryutil.SemanticResolver
	DraftResolver    DraftResolver
}

type Projector struct {
	manager *Manager
	journal EventJournal
	opts    ProjectorOptions
	mu      sync.Mutex
}

type ProjectOnceResult struct {
	NextCursor JournalCursor
	Processed  int
	Exhausted  bool
}

type shortTermTarget struct {
	DayUTC    time.Time
	SessionID string
	Key       string
}

type shortTermBucket struct {
	Target  shortTermTarget
	Records []JournalRecord
}

type DraftResolver interface {
	ResolveDraft(ctx context.Context, event MemoryEvent, existing ShortTermContent) (SessionDraft, error)
}

type projectedPromote struct {
	SubjectID string
	Promote   PromoteDraft
	Cursor    JournalCursor
}

func NewProjector(manager *Manager, journal EventJournal, opts ProjectorOptions) *Projector {
	if opts.CheckpointBatch <= 0 {
		opts.CheckpointBatch = defaultProjectorCheckpointBatch
	}
	return &Projector{
		manager: manager,
		journal: journal,
		opts:    opts,
	}
}

// ProjectOnce replays at most `limit` journal records from checkpoint and projects
// them into markdown files. The caller controls when this method is triggered.
func (p *Projector) ProjectOnce(ctx context.Context, limit int) (ProjectOnceResult, error) {
	if limit <= 0 {
		return ProjectOnceResult{}, fmt.Errorf("limit must be > 0")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	result := ProjectOnceResult{}

	start, err := p.loadCheckpointOffset()
	if err != nil {
		return result, err
	}

	records := make([]JournalRecord, 0, limit)
	next, exhausted, err := p.journal.ReplayFrom(start, limit, func(rec JournalRecord) error {
		records = append(records, rec)
		return nil
	})
	if err != nil {
		return result, err
	}
	result.NextCursor = next
	result.Exhausted = exhausted
	if len(records) == 0 {
		if next != start {
			if err := p.journal.SaveCheckpoint(JournalCheckpoint{
				File: next.File,
				Line: next.Line,
				Byte: next.Byte,
			}); err != nil {
				return result, fmt.Errorf("save scan checkpoint %s:%d: %w", next.File, next.Line, err)
			}
		}
		return result, nil
	}

	buckets, order, err := buildShortTermBuckets(records)
	if err != nil {
		return result, err
	}
	result.Processed = len(records)

	promotes, errs := p.projectShortTermBuckets(ctx, buckets, order)

	for _, promote := range promotes {
		if !hasDraftPromote(promote.Promote) {
			continue
		}
		if _, err := p.manager.UpdateLongTerm(promote.SubjectID, promote.Promote); err != nil {
			errs = append(errs, fmt.Errorf("long-term projection failed at %s:%d: %w", promote.Cursor.File, promote.Cursor.Line, err))
		}
	}

	if err := p.saveCheckpointInBatches(records, next); err != nil {
		errs = append(errs, err)
	}

	return result, errors.Join(errs...)
}

func (p *Projector) loadCheckpointOffset() (JournalCursor, error) {
	cp, ok, err := p.journal.LoadCheckpoint()
	if err != nil {
		return JournalCursor{}, err
	}
	if !ok {
		return JournalCursor{}, nil
	}
	return JournalCursor{
		File: cp.File,
		Line: cp.Line,
		Byte: cp.Byte,
	}, nil
}

func (p *Projector) projectShortTermBucket(ctx context.Context, bucket shortTermBucket) ([]projectedPromote, error) {
	_, existing, _, err := p.manager.LoadShortTerm(bucket.Target.DayUTC, bucket.Target.SessionID)
	if err != nil {
		return nil, fmt.Errorf("load short-term %s: %w", bucket.Target.Key, err)
	}

	merged := existing
	hasIncomingSummary := false
	promotes := make([]projectedPromote, 0, len(bucket.Records))

	for _, rec := range bucket.Records {
		createdAt, err := memorySummaryCreatedAt(rec.Event.TSUTC)
		if err != nil {
			return nil, fmt.Errorf("invalid ts_utc for %s:%d: %w", rec.Cursor.File, rec.Cursor.Line, err)
		}
		draft, err := p.resolveDraft(ctx, rec.Event, merged)
		if err != nil {
			return nil, fmt.Errorf("resolve draft failed for %s at %s:%d: %w", bucket.Target.Key, rec.Cursor.File, rec.Cursor.Line, err)
		}
		if hasDraftPromote(draft.Promote) {
			promotes = append(promotes, projectedPromote{
				SubjectID: rec.Event.SubjectID,
				Promote:   draft.Promote,
				Cursor:    rec.Cursor,
			})
		}
		beforeCount := len(merged.SummaryItems)
		merged = MergeShortTerm(merged, draft, createdAt)
		if len(merged.SummaryItems) > beforeCount {
			hasIncomingSummary = true
		}
	}

	if !hasIncomingSummary {
		return promotes, nil
	}

	if len(existing.SummaryItems) > 0 && p.opts.SemanticResolver != nil {
		deduped, err := SemanticDedupeSummaryItems(ctx, merged.SummaryItems, p.opts.SemanticResolver)
		if err != nil {
			last := bucket.Records[len(bucket.Records)-1]
			return nil, fmt.Errorf("semantic dedupe failed for %s at %s:%d: %w", bucket.Target.Key, last.Cursor.File, last.Cursor.Line, err)
		}
		merged = NormalizeShortTermContent(ShortTermContent{SummaryItems: deduped})
	}

	_, err = p.manager.WriteShortTerm(bucket.Target.DayUTC, merged, WriteMeta{SessionID: bucket.Target.SessionID})
	if err != nil {
		return nil, fmt.Errorf("write short-term %s: %w", bucket.Target.Key, err)
	}
	return promotes, nil
}

func (p *Projector) saveCheckpointInBatches(records []JournalRecord, finalCursor JournalCursor) error {
	if len(records) == 0 {
		return nil
	}
	for i, rec := range records {
		lastInBatch := (i+1)%p.opts.CheckpointBatch == 0
		lastRecord := i == len(records)-1
		if !lastInBatch && !lastRecord {
			continue
		}
		cursor := rec.Cursor
		if lastRecord {
			cursor = finalCursor
		}
		cp := JournalCheckpoint{
			File: cursor.File,
			Line: cursor.Line,
			Byte: cursor.Byte,
		}
		if err := p.journal.SaveCheckpoint(cp); err != nil {
			return fmt.Errorf("save checkpoint %s:%d: %w", cursor.File, cursor.Line, err)
		}
	}
	return nil
}

func (p *Projector) projectShortTermBuckets(ctx context.Context, buckets map[string]shortTermBucket, order []string) ([]projectedPromote, []error) {
	if len(order) == 0 {
		return nil, nil
	}
	workers := p.currentDayBucketWorkers()
	if workers > len(order) {
		workers = len(order)
	}
	if workers <= 0 {
		workers = 1
	}

	jobs := make(chan shortTermBucket, len(order))
	for _, key := range order {
		jobs <- buckets[key]
	}
	close(jobs)

	errs := make([]error, 0, 4)
	promotes := make([]projectedPromote, 0, len(order))
	var errsMu sync.Mutex
	var promotesMu sync.Mutex
	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for bucket := range jobs {
			bucketPromotes, err := p.projectShortTermBucket(ctx, bucket)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, err)
				errsMu.Unlock()
				continue
			}
			if len(bucketPromotes) > 0 {
				promotesMu.Lock()
				promotes = append(promotes, bucketPromotes...)
				promotesMu.Unlock()
			}
		}
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker()
	}
	wg.Wait()
	return promotes, errs
}

func (p *Projector) resolveDraft(ctx context.Context, event MemoryEvent, existing ShortTermContent) (SessionDraft, error) {
	if p.opts.DraftResolver == nil {
		return SessionDraft{}, nil
	}
	return p.opts.DraftResolver.ResolveDraft(ctx, event, existing)
}

func (p *Projector) currentDayBucketWorkers() int {
	dayDir, _ := p.manager.ShortTermDayDir(time.Now().UTC())
	entries, err := os.ReadDir(dayDir)
	if err != nil {
		return 1
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(entry.Name()))
		if strings.HasSuffix(name, ".md") {
			count++
		}
	}
	if count <= 0 {
		return 1
	}
	return count
}

func buildShortTermBuckets(records []JournalRecord) (map[string]shortTermBucket, []string, error) {
	buckets := make(map[string]shortTermBucket, len(records))
	order := make([]string, 0, len(records))

	for _, rec := range records {
		target, err := shortTermTargetFromEvent(rec.Event)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve projection target at %s:%d: %w", rec.Cursor.File, rec.Cursor.Line, err)
		}
		bucket, exists := buckets[target.Key]
		if !exists {
			bucket = shortTermBucket{Target: target}
			order = append(order, target.Key)
		}
		bucket.Records = append(bucket.Records, rec)
		buckets[target.Key] = bucket
	}

	return buckets, order, nil
}

func shortTermTargetFromEvent(event MemoryEvent) (shortTermTarget, error) {
	ts, err := time.Parse(time.RFC3339, event.TSUTC)
	if err != nil {
		return shortTermTarget{}, fmt.Errorf("ts_utc must be RFC3339")
	}
	sessionID := strings.TrimSpace(event.SubjectID)
	if sessionID == "" {
		return shortTermTarget{}, fmt.Errorf("subject_id is required")
	}
	day := ts.UTC()
	dayKey := day.Format("2006-01-02")
	return shortTermTarget{
		DayUTC:    day,
		SessionID: sessionID,
		Key:       dayKey + "/" + sessionID,
	}, nil
}

func memorySummaryCreatedAt(tsUTC string) (string, error) {
	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(tsUTC))
	if err != nil {
		return "", err
	}
	return ts.UTC().Format(entryutil.TimestampLayout), nil
}

func hasDraftPromote(promote PromoteDraft) bool {
	return len(promote.GoalsProjects) > 0 || len(promote.KeyFacts) > 0
}
