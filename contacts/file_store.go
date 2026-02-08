package contacts

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
)

const (
	candidatesFileVersion = 1
	sessionsFileVersion   = 1
	contactPronounsMaxLen = 64
	contactTZMaxLen       = 64
	contactPrefMaxLen     = 2000
)

type candidatesFile struct {
	Version int              `json:"version"`
	Records []ShareCandidate `json:"records"`
}

type sessionsFile struct {
	Version int            `json:"version"`
	Records []SessionState `json:"records"`
}

type FileStore struct {
	root string
	mu   sync.Mutex
}

func NewFileStore(root string) *FileStore {
	return &FileStore{root: strings.TrimSpace(root)}
}

func (s *FileStore) Ensure(ctx context.Context) error {
	if err := ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fsstore.EnsureDir(s.rootPath(), 0o700)
}

func (s *FileStore) GetContact(ctx context.Context, contactID string) (Contact, bool, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return Contact{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	active, err := s.loadContactsMarkdownLocked(s.activeContactsPath())
	if err != nil {
		return Contact{}, false, err
	}
	contactID = strings.TrimSpace(contactID)
	for _, item := range active {
		if strings.TrimSpace(item.ContactID) == contactID {
			return item, true, nil
		}
	}

	inactive, err := s.loadContactsMarkdownLocked(s.inactiveContactsPath())
	if err != nil {
		return Contact{}, false, err
	}
	for _, item := range inactive {
		if strings.TrimSpace(item.ContactID) == contactID {
			return item, true, nil
		}
	}
	return Contact{}, false, nil
}

func (s *FileStore) PutContact(ctx context.Context, contact Contact) error {
	if err := ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.withStateLock(ctx, func() error {
		now := time.Now().UTC()
		contact = normalizeContact(contact, now)

		active, err := s.loadContactsMarkdownLocked(s.activeContactsPath())
		if err != nil {
			return err
		}
		inactive, err := s.loadContactsMarkdownLocked(s.inactiveContactsPath())
		if err != nil {
			return err
		}

		createdAt := contact.CreatedAt
		active = removeContactByID(active, contact.ContactID, &createdAt)
		inactive = removeContactByID(inactive, contact.ContactID, &createdAt)
		contact.CreatedAt = createdAt

		switch contact.Status {
		case StatusInactive:
			inactive = append(inactive, contact)
		default:
			contact.Status = StatusActive
			active = append(active, contact)
		}

		if err := s.saveContactsMarkdownLocked(s.activeContactsPath(), "Active Contacts", active); err != nil {
			return err
		}
		if err := s.saveContactsMarkdownLocked(s.inactiveContactsPath(), "Inactive Contacts", inactive); err != nil {
			return err
		}
		return nil
	})
}

func (s *FileStore) ListContacts(ctx context.Context, status Status) ([]Contact, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	status = normalizeStatus(status)
	switch status {
	case StatusActive:
		return s.loadContactsMarkdownLocked(s.activeContactsPath())
	case StatusInactive:
		return s.loadContactsMarkdownLocked(s.inactiveContactsPath())
	default:
		active, err := s.loadContactsMarkdownLocked(s.activeContactsPath())
		if err != nil {
			return nil, err
		}
		inactive, err := s.loadContactsMarkdownLocked(s.inactiveContactsPath())
		if err != nil {
			return nil, err
		}
		out := make([]Contact, 0, len(active)+len(inactive))
		out = append(out, active...)
		out = append(out, inactive...)
		sort.Slice(out, func(i, j int) bool {
			if out[i].Status != out[j].Status {
				return out[i].Status < out[j].Status
			}
			return strings.TrimSpace(out[i].ContactID) < strings.TrimSpace(out[j].ContactID)
		})
		return out, nil
	}
}

func (s *FileStore) GetCandidate(ctx context.Context, itemID string) (ShareCandidate, bool, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return ShareCandidate{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadCandidatesLocked()
	if err != nil {
		return ShareCandidate{}, false, err
	}
	itemID = strings.TrimSpace(itemID)
	for _, item := range records {
		if strings.TrimSpace(item.ItemID) == itemID {
			return item, true, nil
		}
	}
	return ShareCandidate{}, false, nil
}

func (s *FileStore) PutCandidate(ctx context.Context, candidate ShareCandidate) error {
	if err := ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withStateLock(ctx, func() error {
		records, err := s.loadCandidatesLocked()
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		candidate = normalizeCandidate(candidate, now)
		replaced := false
		for i := range records {
			if strings.TrimSpace(records[i].ItemID) != candidate.ItemID {
				continue
			}
			if records[i].CreatedAt.IsZero() {
				records[i].CreatedAt = candidate.CreatedAt
			}
			candidate.CreatedAt = records[i].CreatedAt
			records[i] = candidate
			replaced = true
			break
		}
		if !replaced {
			records = append(records, candidate)
		}
		return s.saveCandidatesLocked(records)
	})
}

func (s *FileStore) ListCandidates(ctx context.Context) ([]ShareCandidate, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadCandidatesLocked()
}

func (s *FileStore) GetSessionState(ctx context.Context, sessionID string) (SessionState, bool, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return SessionState{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadSessionsLocked()
	if err != nil {
		return SessionState{}, false, err
	}
	sessionID = strings.TrimSpace(sessionID)
	for _, item := range records {
		if strings.TrimSpace(item.SessionID) == sessionID {
			return item, true, nil
		}
	}
	return SessionState{}, false, nil
}

func (s *FileStore) PutSessionState(ctx context.Context, state SessionState) error {
	if err := ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withStateLock(ctx, func() error {
		records, err := s.loadSessionsLocked()
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		state = normalizeSessionState(state, now)
		replaced := false
		for i := range records {
			if strings.TrimSpace(records[i].SessionID) != state.SessionID {
				continue
			}
			if records[i].StartedAt.IsZero() {
				records[i].StartedAt = state.StartedAt
			}
			state.StartedAt = records[i].StartedAt
			records[i] = state
			replaced = true
			break
		}
		if !replaced {
			records = append(records, state)
		}
		return s.saveSessionsLocked(records)
	})
}

func (s *FileStore) ListSessionStates(ctx context.Context) ([]SessionState, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadSessionsLocked()
}

func (s *FileStore) AppendAuditEvent(ctx context.Context, event AuditEvent) error {
	if err := ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	event = normalizeAuditEvent(event, time.Now().UTC())
	return s.withAuditLock(ctx, func() error {
		writer, err := fsstore.NewJSONLWriter(s.auditPathJSONL(), fsstore.JSONLOptions{
			DirPerm:        0o700,
			FilePerm:       0o600,
			FlushEachWrite: true,
		})
		if err != nil {
			return fmt.Errorf("open audit writer: %w", err)
		}
		defer writer.Close()
		if err := writer.AppendJSON(event); err != nil {
			return fmt.Errorf("append audit event: %w", err)
		}
		return nil
	})
}

func (s *FileStore) ListAuditEvents(ctx context.Context, tickID string, contactID string, action string, limit int) ([]AuditEvent, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tickID = strings.TrimSpace(tickID)
	contactID = strings.TrimSpace(contactID)
	action = strings.TrimSpace(action)
	var out []AuditEvent
	err := s.withAuditLock(ctx, func() error {
		records, err := s.readAuditEventsJSONL(s.auditPathJSONL())
		if err != nil {
			return err
		}
		filtered := make([]AuditEvent, 0, len(records))
		for _, item := range records {
			if tickID != "" && strings.TrimSpace(item.TickID) != tickID {
				continue
			}
			if contactID != "" && strings.TrimSpace(item.ContactID) != contactID {
				continue
			}
			if action != "" && strings.TrimSpace(item.Action) != action {
				continue
			}
			filtered = append(filtered, item)
		}
		sort.Slice(filtered, func(i, j int) bool {
			if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
				return strings.TrimSpace(filtered[i].EventID) > strings.TrimSpace(filtered[j].EventID)
			}
			return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
		})
		if limit > 0 && len(filtered) > limit {
			filtered = filtered[:limit]
		}
		out = make([]AuditEvent, len(filtered))
		copy(out, filtered)
		return nil
	})
	return out, err
}

func (s *FileStore) loadContactsMarkdownLocked(path string) ([]Contact, error) {
	content, exists, err := fsstore.ReadText(path)
	if err != nil {
		return nil, fmt.Errorf("read contacts markdown %s: %w", path, err)
	}
	if !exists {
		return []Contact{}, nil
	}
	records, err := parseContactsMarkdown(content)
	if err != nil {
		return nil, fmt.Errorf("parse contacts markdown %s: %w", path, err)
	}
	for i := range records {
		records[i] = normalizeContact(records[i], time.Now().UTC())
	}
	sort.Slice(records, func(i, j int) bool {
		return strings.TrimSpace(records[i].ContactID) < strings.TrimSpace(records[j].ContactID)
	})
	return records, nil
}

func (s *FileStore) saveContactsMarkdownLocked(path string, title string, records []Contact) error {
	rendered, err := renderContactsMarkdown(title, records)
	if err != nil {
		return err
	}
	return fsstore.WriteTextAtomic(path, rendered, fsstore.FileOptions{
		DirPerm:  0o700,
		FilePerm: 0o600,
	})
}

func parseContactsMarkdown(content string) ([]Contact, error) {
	out := make([]Contact, 0, 64)
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		raw = strings.Trim(raw, "`")
		if raw == "" {
			continue
		}
		var item Contact
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func renderContactsMarkdown(title string, records []Contact) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Contacts"
	}
	items := make([]Contact, 0, len(records))
	for _, item := range records {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.TrimSpace(items[i].ContactID) < strings.TrimSpace(items[j].ContactID)
	})
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString("<!-- One JSON object per list item. -->\n")
	for _, item := range items {
		raw, err := json.Marshal(item)
		if err != nil {
			return "", err
		}
		b.WriteString("- ")
		b.Write(raw)
		b.WriteString("\n")
	}
	return b.String(), nil
}

func removeContactByID(items []Contact, contactID string, createdAt *time.Time) []Contact {
	out := items[:0]
	contactID = strings.TrimSpace(contactID)
	for _, item := range items {
		if strings.TrimSpace(item.ContactID) == contactID {
			if createdAt != nil && !item.CreatedAt.IsZero() {
				*createdAt = item.CreatedAt
			}
			continue
		}
		out = append(out, item)
	}
	return out
}

func (s *FileStore) loadCandidatesLocked() ([]ShareCandidate, error) {
	var file candidatesFile
	ok, err := readJSONFile(s.candidatesPath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []ShareCandidate{}, nil
	}
	out := make([]ShareCandidate, 0, len(file.Records))
	for _, item := range file.Records {
		item = normalizeCandidate(item, time.Now().UTC())
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return strings.TrimSpace(out[i].ItemID) < strings.TrimSpace(out[j].ItemID)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *FileStore) saveCandidatesLocked(records []ShareCandidate) error {
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return strings.TrimSpace(records[i].ItemID) < strings.TrimSpace(records[j].ItemID)
		}
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	file := candidatesFile{Version: candidatesFileVersion, Records: records}
	return writeJSONFileAtomic(s.candidatesPath(), file)
}

func (s *FileStore) loadSessionsLocked() ([]SessionState, error) {
	var file sessionsFile
	ok, err := readJSONFile(s.sessionsPath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []SessionState{}, nil
	}
	out := make([]SessionState, 0, len(file.Records))
	for _, item := range file.Records {
		item = normalizeSessionState(item, time.Now().UTC())
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return strings.TrimSpace(out[i].SessionID) < strings.TrimSpace(out[j].SessionID)
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *FileStore) saveSessionsLocked(records []SessionState) error {
	sort.Slice(records, func(i, j int) bool {
		if records[i].UpdatedAt.Equal(records[j].UpdatedAt) {
			return strings.TrimSpace(records[i].SessionID) < strings.TrimSpace(records[j].SessionID)
		}
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	file := sessionsFile{Version: sessionsFileVersion, Records: records}
	return writeJSONFileAtomic(s.sessionsPath(), file)
}

func (s *FileStore) readAuditEventsJSONL(path string) ([]AuditEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []AuditEvent{}, nil
		}
		return nil, fmt.Errorf("open audit jsonl %s: %w", path, err)
	}
	defer file.Close()

	records := make([]AuditEvent, 0, 64)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event AuditEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("decode audit jsonl %s: %w", path, err)
		}
		records = append(records, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan audit jsonl %s: %w", path, err)
	}
	return records, nil
}

func (s *FileStore) withStateLock(ctx context.Context, fn func() error) error {
	return s.withLock(ctx, "state.main", fn)
}

func (s *FileStore) withAuditLock(ctx context.Context, fn func() error) error {
	return s.withLock(ctx, "audit.share_decisions_jsonl", fn)
}

func (s *FileStore) withLock(ctx context.Context, key string, fn func() error) error {
	lockPath, err := fsstore.BuildLockPath(s.lockRootPath(), key)
	if err != nil {
		return err
	}
	return fsstore.WithLock(ctx, lockPath, fn)
}

func (s *FileStore) rootPath() string {
	root := strings.TrimSpace(s.root)
	if root == "" {
		return "contacts"
	}
	return filepath.Clean(root)
}

func (s *FileStore) lockRootPath() string {
	return filepath.Join(s.rootPath(), ".fslocks")
}

func (s *FileStore) activeContactsPath() string {
	return filepath.Join(s.rootPath(), "active.md")
}

func (s *FileStore) inactiveContactsPath() string {
	return filepath.Join(s.rootPath(), "inactive.md")
}

func (s *FileStore) candidatesPath() string {
	return filepath.Join(s.rootPath(), "share_candidates.json")
}

func (s *FileStore) sessionsPath() string {
	return filepath.Join(s.rootPath(), "share_sessions.json")
}

func (s *FileStore) auditPathJSONL() string {
	return filepath.Join(s.rootPath(), "share_decisions_audit.jsonl")
}

func normalizeContact(c Contact, now time.Time) Contact {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	c.ContactID = strings.TrimSpace(c.ContactID)
	c.ContactNickname = strings.TrimSpace(c.ContactNickname)
	c.PersonaBrief = strings.TrimSpace(c.PersonaBrief)
	c.Pronouns = clipString(strings.TrimSpace(c.Pronouns), contactPronounsMaxLen)
	c.Timezone = normalizeTimezone(strings.TrimSpace(c.Timezone))
	c.PreferenceContext = clipString(strings.TrimSpace(c.PreferenceContext), contactPrefMaxLen)
	c.DisplayName = strings.TrimSpace(c.DisplayName)
	if c.ContactNickname == "" && c.DisplayName != "" {
		c.ContactNickname = c.DisplayName
	}
	c.DisplayName = ""
	c.SubjectID = strings.TrimSpace(c.SubjectID)
	c.NodeID = strings.TrimSpace(c.NodeID)
	c.PeerID = strings.TrimSpace(c.PeerID)
	c.TrustState = strings.TrimSpace(strings.ToLower(c.TrustState))
	c.LastSharedItemID = strings.TrimSpace(c.LastSharedItemID)
	c.Kind = normalizeKind(c.Kind)
	c.Status = normalizeStatus(c.Status)
	c.Addresses = normalizeStringSlice(c.Addresses)
	c.TelegramChats = normalizeTelegramChats(c.TelegramChats)
	c.TopicWeights = normalizeTopicWeightsMap(cloneFloatMap(c.TopicWeights))
	c.PersonaTraits = normalizeTraitMap(c.PersonaTraits)
	c.UnderstandingDepth = clamp(c.UnderstandingDepth, 0, 100)
	c.ReciprocityNorm = clamp(c.ReciprocityNorm, 0, 1)
	c.RetainScore = clamp(c.RetainScore, 0, 1)
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if len(c.Addresses) == 0 {
		c.Addresses = nil
	}
	if len(c.TopicWeights) == 0 {
		c.TopicWeights = nil
	}
	if len(c.PersonaTraits) == 0 {
		c.PersonaTraits = nil
	}
	return c
}

func clipString(input string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(input) <= max {
		return input
	}
	return input[:max]
}

func normalizeTimezone(raw string) string {
	raw = clipString(strings.TrimSpace(raw), contactTZMaxLen)
	if raw == "" {
		return ""
	}
	if _, err := time.LoadLocation(raw); err != nil {
		return ""
	}
	return raw
}

func normalizeCandidate(c ShareCandidate, now time.Time) ShareCandidate {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	c.ItemID = strings.TrimSpace(c.ItemID)
	c.Topic = strings.TrimSpace(c.Topic)
	c.ContentType = strings.TrimSpace(c.ContentType)
	c.PayloadBase64 = strings.TrimSpace(c.PayloadBase64)
	c.SensitivityLevel = strings.TrimSpace(strings.ToLower(c.SensitivityLevel))
	c.SourceRef = strings.TrimSpace(c.SourceRef)
	c.Topics = normalizeStringSlice(c.Topics)
	c.LinkedHistoryIDs = normalizeStringSlice(c.LinkedHistoryIDs)
	c.DepthHint = clamp(c.DepthHint, 0, 1)
	c.SourceChatType = strings.ToLower(strings.TrimSpace(c.SourceChatType))
	switch c.SourceChatType {
	case "private", "group", "supergroup":
	default:
		c.SourceChatType = ""
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if len(c.Topics) == 0 {
		c.Topics = nil
	}
	if len(c.LinkedHistoryIDs) == 0 {
		c.LinkedHistoryIDs = nil
	}
	return c
}

func normalizeSessionState(s SessionState, now time.Time) SessionState {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.SessionID = strings.TrimSpace(s.SessionID)
	s.ContactID = strings.TrimSpace(s.ContactID)
	s.SessionInterestLevel = clamp(s.SessionInterestLevel, 0, 1)
	if s.StartedAt.IsZero() {
		s.StartedAt = now
	}
	s.UpdatedAt = now
	return s
}

func normalizeAuditEvent(e AuditEvent, now time.Time) AuditEvent {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	e.EventID = strings.TrimSpace(e.EventID)
	e.TickID = strings.TrimSpace(e.TickID)
	e.Action = strings.TrimSpace(e.Action)
	e.ContactID = strings.TrimSpace(e.ContactID)
	e.PeerID = strings.TrimSpace(e.PeerID)
	e.ItemID = strings.TrimSpace(e.ItemID)
	e.Reason = strings.TrimSpace(e.Reason)
	if e.Metadata != nil && len(e.Metadata) == 0 {
		e.Metadata = nil
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	return e
}

func normalizeKind(kind Kind) Kind {
	switch kind {
	case KindHuman, KindAgent:
		return kind
	default:
		return KindAgent
	}
}

func normalizeStatus(status Status) Status {
	switch status {
	case StatusInactive:
		return StatusInactive
	case StatusActive:
		return StatusActive
	default:
		return StatusActive
	}
}

func normalizeStringSlice(input []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(input))
	for _, raw := range input {
		v := strings.TrimSpace(raw)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func normalizeTelegramChats(input []TelegramChatRef) []TelegramChatRef {
	if len(input) == 0 {
		return nil
	}
	type keyed struct {
		item TelegramChatRef
	}
	byID := map[int64]TelegramChatRef{}
	for _, raw := range input {
		if raw.ChatID == 0 {
			continue
		}
		item := TelegramChatRef{
			ChatID:   raw.ChatID,
			ChatType: strings.ToLower(strings.TrimSpace(raw.ChatType)),
		}
		switch item.ChatType {
		case "private", "group", "supergroup":
		default:
			item.ChatType = ""
		}
		if raw.LastSeenAt != nil && !raw.LastSeenAt.IsZero() {
			ts := raw.LastSeenAt.UTC()
			item.LastSeenAt = &ts
		}
		prev, exists := byID[item.ChatID]
		if !exists {
			byID[item.ChatID] = item
			continue
		}
		merged := prev
		if merged.ChatType == "" && item.ChatType != "" {
			merged.ChatType = item.ChatType
		}
		if merged.LastSeenAt == nil || (item.LastSeenAt != nil && item.LastSeenAt.After(*merged.LastSeenAt)) {
			merged.LastSeenAt = item.LastSeenAt
		}
		byID[item.ChatID] = merged
	}
	if len(byID) == 0 {
		return nil
	}
	out := make([]TelegramChatRef, 0, len(byID))
	for _, item := range byID {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		ti := time.Time{}
		tj := time.Time{}
		if out[i].LastSeenAt != nil {
			ti = *out[i].LastSeenAt
		}
		if out[j].LastSeenAt != nil {
			tj = *out[j].LastSeenAt
		}
		if ti.Equal(tj) {
			return out[i].ChatID < out[j].ChatID
		}
		return ti.After(tj)
	})
	return out
}

func cloneFloatMap(input map[string]float64) map[string]float64 {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]float64, len(input))
	for k, v := range input {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeTraitMap(input map[string]float64) map[string]float64 {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]float64, len(input))
	for rawKey, rawValue := range input {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		if key == "" {
			continue
		}
		out[key] = clamp(rawValue, 0, 1)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func clamp(v float64, min float64, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func readJSONFile(path string, out any) (bool, error) {
	ok, err := fsstore.ReadJSON(path, out)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	return ok, nil
}

func writeJSONFileAtomic(path string, v any) error {
	return fsstore.WriteJSONAtomic(path, v, fsstore.FileOptions{
		DirPerm:  0o700,
		FilePerm: 0o600,
	})
}

func ensureNotCanceled(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
