package llmstats

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/pricingutil"
	uniaiapi "github.com/quailyquaily/uniai"
	"github.com/spf13/viper"
)

const projectionSchemaVersion = 2

type ProjectionStore struct {
	journalDir  string
	path        string
	now         func() time.Time
	loadPricing func() (*uniaiapi.PricingCatalog, string, error)
	logger      *slog.Logger
}

type aggregateState struct {
	summary Totals
	skipped int64
	byHost  map[string]*apiHostState
	byModel map[string]*Totals
}

type apiHostState struct {
	totals  Totals
	byModel map[string]*Totals
}

func NewProjectionStore(journalDir, path string) *ProjectionStore {
	return &ProjectionStore{
		journalDir: strings.TrimSpace(journalDir),
		path:       strings.TrimSpace(path),
		now:        time.Now,
		loadPricing: func() (*uniaiapi.PricingCatalog, string, error) {
			return pricingutil.LoadCatalog(viper.GetString("llm.pricing_file"), viper.GetString("config"))
		},
		logger: slog.Default(),
	}
}

func (s *ProjectionStore) Refresh() (Projection, error) {
	startedAt := time.Now()
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	pricing, pricingDigest, err := s.currentPricing()
	if err != nil {
		return Projection{}, err
	}
	proj, ok, err := loadProjection(s.path)
	if err != nil || !ok {
		proj = Projection{}
	}

	segments, err := listSegmentFiles(s.journalDir)
	if err != nil {
		return Projection{}, err
	}
	if len(segments) == 0 {
		zero := Projection{UpdatedAt: s.now().UTC().Format(time.RFC3339)}
		if err := saveProjection(s.path, zero); err != nil {
			return Projection{}, err
		}
		return zero, nil
	}

	start := proj.ProjectedOffset
	rebuildReasons := projectionRebuildReasons(proj, ok, pricingDigest, offsetValidForSegments(s.journalDir, segments, start))
	rebuild := len(rebuildReasons) > 0
	if rebuild {
		logger.Info("llm_usage_projection_rebuild",
			"reasons", strings.Join(rebuildReasons, ","),
			"schema_version", proj.SchemaVersion,
			"expected_schema_version", projectionSchemaVersion,
			"pricing_digest", strings.TrimSpace(proj.PricingDigest),
			"expected_pricing_digest", strings.TrimSpace(pricingDigest),
			"projected_records", proj.ProjectedRecords,
			"from_file", start.File,
			"from_line", start.Line,
		)
		proj = Projection{}
		start = Offset{}
	}

	state := aggregateStateFromProjection(proj)
	nextOffset, skipped, err := scanJournalFrom(s.journalDir, segments, start, func(rec RequestRecord, _ Offset) error {
		rec = backfillRequestCost(rec, pricing)
		state.add(rec)
		return nil
	})
	if err != nil {
		return Projection{}, err
	}
	state.skipped += skipped

	out := state.toProjection()
	out.SchemaVersion = projectionSchemaVersion
	out.PricingDigest = pricingDigest
	out.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	out.ProjectedOffset = nextOffset
	out.ProjectedRecords = out.Summary.Requests
	if err := saveProjection(s.path, out); err != nil {
		return Projection{}, err
	}
	mode := "incremental"
	if rebuild {
		mode = "rebuild"
	}
	logger.Debug("llm_usage_projection_refreshed",
		"mode", mode,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"journal_segments", len(segments),
		"projected_records", out.ProjectedRecords,
		"skipped_records", out.SkippedRecords,
		"to_file", out.ProjectedOffset.File,
		"to_line", out.ProjectedOffset.Line,
	)
	return out, nil
}

func (s *ProjectionStore) currentPricing() (*uniaiapi.PricingCatalog, string, error) {
	if s == nil || s.loadPricing == nil {
		return nil, "", nil
	}
	return s.loadPricing()
}

func aggregateStateFromProjection(p Projection) *aggregateState {
	st := &aggregateState{
		summary: p.Summary,
		skipped: p.SkippedRecords,
		byHost:  map[string]*apiHostState{},
		byModel: map[string]*Totals{},
	}
	for _, host := range p.APIHosts {
		hs := &apiHostState{totals: host.Totals, byModel: map[string]*Totals{}}
		for _, model := range host.Models {
			m := model.Totals
			hs.byModel[model.Model] = &m
		}
		st.byHost[host.APIHost] = hs
	}
	for _, model := range p.Models {
		m := model.Totals
		st.byModel[model.Model] = &m
	}
	return st
}

func (s *aggregateState) add(rec RequestRecord) {
	if s == nil {
		return
	}
	rec = normalizeRequestRecord(rec)
	s.summary.AddRecord(rec)
	if s.byModel == nil {
		s.byModel = map[string]*Totals{}
	}
	if s.byHost == nil {
		s.byHost = map[string]*apiHostState{}
	}

	modelTotals, ok := s.byModel[rec.Model]
	if !ok {
		modelTotals = &Totals{}
		s.byModel[rec.Model] = modelTotals
	}
	modelTotals.AddRecord(rec)

	hostState, ok := s.byHost[rec.APIHost]
	if !ok {
		hostState = &apiHostState{byModel: map[string]*Totals{}}
		s.byHost[rec.APIHost] = hostState
	}
	hostState.totals.AddRecord(rec)

	hostModelTotals, ok := hostState.byModel[rec.Model]
	if !ok {
		hostModelTotals = &Totals{}
		hostState.byModel[rec.Model] = hostModelTotals
	}
	hostModelTotals.AddRecord(rec)
}

func (s *aggregateState) toProjection() Projection {
	if s == nil {
		return Projection{}
	}
	models := make([]ModelSummary, 0, len(s.byModel))
	for model, totals := range s.byModel {
		models = append(models, ModelSummary{Model: model, Totals: *totals})
	}
	sortModelSummaries(models)

	hosts := make([]APIHostSummary, 0, len(s.byHost))
	for host, hostState := range s.byHost {
		hostSummary := APIHostSummary{APIHost: host, Totals: hostState.totals}
		hostSummary.Models = make([]ModelSummary, 0, len(hostState.byModel))
		for model, totals := range hostState.byModel {
			hostSummary.Models = append(hostSummary.Models, ModelSummary{Model: model, Totals: *totals})
		}
		hosts = append(hosts, hostSummary)
	}
	sortAPIHostSummaries(hosts)

	return Projection{
		SchemaVersion:  projectionSchemaVersion,
		Summary:        s.summary,
		APIHosts:       hosts,
		Models:         models,
		SkippedRecords: s.skipped,
	}
}

func loadProjection(path string) (Projection, bool, error) {
	if strings.TrimSpace(path) == "" {
		return Projection{}, false, nil
	}
	var proj Projection
	ok, err := fsstore.ReadJSON(path, &proj)
	if err != nil {
		return Projection{}, false, err
	}
	if !ok {
		return Projection{}, false, nil
	}
	return proj, true, nil
}

func saveProjection(path string, proj Projection) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return fsstore.WriteJSONAtomic(path, proj, fsstore.FileOptions{})
}

func projectionCompatible(proj Projection, pricingDigest string) bool {
	if proj.SchemaVersion != projectionSchemaVersion {
		return false
	}
	return strings.TrimSpace(proj.PricingDigest) == strings.TrimSpace(pricingDigest)
}

func projectionRebuildReasons(proj Projection, projectionExists bool, pricingDigest string, offsetValid bool) []string {
	reasons := make([]string, 0, 3)
	if !projectionExists {
		reasons = append(reasons, "missing_projection")
	}
	if !offsetValid {
		reasons = append(reasons, "offset_invalid")
	}
	if proj.SchemaVersion != projectionSchemaVersion {
		reasons = append(reasons, "schema_version_changed")
	}
	if strings.TrimSpace(proj.PricingDigest) != strings.TrimSpace(pricingDigest) {
		reasons = append(reasons, "pricing_digest_changed")
	}
	return reasons
}

func backfillRequestCost(rec RequestRecord, pricing *uniaiapi.PricingCatalog) RequestRecord {
	rec = normalizeRequestRecord(rec)
	if pricing == nil || requestRecordHasCost(rec) || rec.Operation != operationChat {
		return rec
	}
	usage := uniaiapi.Usage{
		InputTokens:  int(rec.InputTokens),
		OutputTokens: int(rec.OutputTokens),
		TotalTokens:  int(rec.TotalTokens),
		Cache: uniaiapi.UsageCache{
			CachedInputTokens:        int(rec.CachedInputTokens),
			CacheCreationInputTokens: int(rec.CacheCreationInputTokens),
			Details:                  toIntMap(rec.CacheDetails),
		},
	}
	cost, ok := pricing.EstimateChatCost(rec.Model, usage)
	if !ok || cost == nil {
		return rec
	}
	rec.CostCurrency = cost.Currency
	rec.CostEstimated = cost.Estimated
	rec.InputCost = cost.Input
	rec.CachedInputCost = cost.CachedInput
	rec.CacheCreationInputCost = cost.CacheCreationInput
	rec.OutputCost = cost.Output
	rec.TotalCost = cost.Total
	return normalizeRequestRecord(rec)
}

func requestRecordHasCost(rec RequestRecord) bool {
	return strings.TrimSpace(rec.CostCurrency) != "" ||
		rec.CostEstimated ||
		rec.InputCost != 0 ||
		rec.CachedInputCost != 0 ||
		rec.CacheCreationInputCost != 0 ||
		rec.OutputCost != 0 ||
		rec.TotalCost != 0
}

func toIntMap(in map[string]int64) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = int(nonNegative(value))
	}
	return out
}

func offsetValidForSegments(dir string, segments []journalSegmentFile, off Offset) bool {
	off.File = strings.TrimSpace(off.File)
	if off.File == "" {
		return off.Line == 0
	}
	if off.Line < 0 {
		return false
	}
	var target *journalSegmentFile
	for i := range segments {
		if segments[i].Key == off.File {
			target = &segments[i]
			break
		}
	}
	if target == nil {
		return false
	}
	lines, err := countLines(filepath.Join(dir, target.ActualName))
	if err != nil {
		return false
	}
	return off.Line <= lines
}

func scanJournalFrom(dir string, segments []journalSegmentFile, from Offset, fn func(RequestRecord, Offset) error) (Offset, int64, error) {
	if fn == nil {
		return from, 0, fmt.Errorf("scan callback is required")
	}
	next := from
	var skipped int64
	for _, seg := range segments {
		if from.File != "" && seg.Key < from.File {
			continue
		}
		path := filepath.Join(dir, seg.ActualName)
		file, err := os.Open(path)
		if err != nil {
			return next, skipped, err
		}
		reader := bufio.NewReader(file)
		var lineNo int64
		for {
			line, readErr := reader.ReadBytes('\n')
			if len(line) > 0 {
				if line[len(line)-1] != '\n' && readErr == io.EOF {
					break
				}
				lineNo++
				current := Offset{File: seg.Key, Line: lineNo}
				if from.File == seg.Key && lineNo <= from.Line {
					if readErr == io.EOF {
						break
					}
					if readErr != nil {
						_ = file.Close()
						return next, skipped, readErr
					}
					continue
				}

				raw := bytes.TrimSpace(line)
				if len(raw) == 0 {
					next = current
					skipped++
				} else {
					var rec RequestRecord
					if err := json.Unmarshal(raw, &rec); err != nil {
						next = current
						skipped++
					} else {
						rec = normalizeRequestRecord(rec)
						if err := validateRequestRecord(rec); err != nil {
							next = current
							skipped++
						} else {
							if err := fn(rec, current); err != nil {
								_ = file.Close()
								return next, skipped, err
							}
							next = current
						}
					}
				}
			}
			if readErr != nil {
				if readErr == io.EOF {
					break
				}
				_ = file.Close()
				return next, skipped, readErr
			}
		}
		if err := file.Close(); err != nil {
			return next, skipped, err
		}
	}
	return next, skipped, nil
}
