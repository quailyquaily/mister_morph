package maep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	contactsFileVersion        = 1
	inboxFileVersion           = 1
	dedupeFileVersion          = 1
	protocolHistoryFileVersion = 1
)

type FileStore struct {
	root string

	mu sync.Mutex
}

type contactsFile struct {
	Version  int       `json:"version"`
	Contacts []Contact `json:"contacts"`
}

type dedupeFile struct {
	Version int            `json:"version"`
	Records []DedupeRecord `json:"records"`
}

type inboxFile struct {
	Version int            `json:"version"`
	Records []InboxMessage `json:"records"`
}

type protocolHistoryFile struct {
	Version int               `json:"version"`
	Records []ProtocolHistory `json:"records"`
}

func NewFileStore(root string) *FileStore {
	return &FileStore{root: strings.TrimSpace(root)}
}

func (s *FileStore) Ensure(ctx context.Context) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.MkdirAll(s.rootPath(), 0o700)
}

func (s *FileStore) GetIdentity(ctx context.Context) (Identity, bool, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return Identity{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var identity Identity
	ok, err := s.readJSONFile(s.identityPath(), &identity)
	if err != nil {
		return Identity{}, false, err
	}
	if !ok {
		return Identity{}, false, nil
	}
	return identity, true, nil
}

func (s *FileStore) PutIdentity(ctx context.Context, identity Identity) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.rootPath(), 0o700); err != nil {
		return fmt.Errorf("create maep state dir: %w", err)
	}
	return s.writeJSONFileAtomic(s.identityPath(), identity, 0o600)
}

func (s *FileStore) GetContactByPeerID(ctx context.Context, peerID string) (Contact, bool, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return Contact{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	contacts, err := s.loadContactsLocked()
	if err != nil {
		return Contact{}, false, err
	}
	peerID = strings.TrimSpace(peerID)
	for _, contact := range contacts {
		if strings.TrimSpace(contact.PeerID) == peerID {
			return contact, true, nil
		}
	}
	return Contact{}, false, nil
}

func (s *FileStore) GetContactByNodeUUID(ctx context.Context, nodeUUID string) (Contact, bool, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return Contact{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	contacts, err := s.loadContactsLocked()
	if err != nil {
		return Contact{}, false, err
	}
	nodeUUID = strings.TrimSpace(nodeUUID)
	for _, contact := range contacts {
		if strings.TrimSpace(contact.NodeUUID) == nodeUUID {
			return contact, true, nil
		}
	}
	return Contact{}, false, nil
}

func (s *FileStore) PutContact(ctx context.Context, contact Contact) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	contacts, err := s.loadContactsLocked()
	if err != nil {
		return err
	}

	replaced := false
	for i := range contacts {
		if strings.TrimSpace(contacts[i].PeerID) == strings.TrimSpace(contact.PeerID) {
			if contacts[i].CreatedAt.IsZero() {
				contacts[i].CreatedAt = contact.CreatedAt
			}
			contact.CreatedAt = contacts[i].CreatedAt
			contacts[i] = contact
			replaced = true
			break
		}
	}
	if !replaced {
		contacts = append(contacts, contact)
	}

	return s.saveContactsLocked(contacts)
}

func (s *FileStore) ListContacts(ctx context.Context) ([]Contact, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	contacts, err := s.loadContactsLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Contact, len(contacts))
	copy(out, contacts)
	return out, nil
}

func (s *FileStore) AppendInboxMessage(ctx context.Context, message InboxMessage) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadInboxMessagesLocked()
	if err != nil {
		return err
	}
	message.MessageID = strings.TrimSpace(message.MessageID)
	message.FromPeerID = strings.TrimSpace(message.FromPeerID)
	message.Topic = strings.TrimSpace(message.Topic)
	message.ContentType = strings.TrimSpace(message.ContentType)
	message.PayloadBase64 = strings.TrimSpace(message.PayloadBase64)
	message.IdempotencyKey = strings.TrimSpace(message.IdempotencyKey)
	if message.ReceivedAt.IsZero() {
		message.ReceivedAt = time.Now().UTC()
	}
	records = append(records, message)
	return s.saveInboxMessagesLocked(records)
}

func (s *FileStore) ListInboxMessages(ctx context.Context, fromPeerID string, topic string, limit int) ([]InboxMessage, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadInboxMessagesLocked()
	if err != nil {
		return nil, err
	}
	fromPeerID = strings.TrimSpace(fromPeerID)
	topic = strings.TrimSpace(topic)

	filtered := make([]InboxMessage, 0, len(records))
	for _, record := range records {
		if fromPeerID != "" && strings.TrimSpace(record.FromPeerID) != fromPeerID {
			continue
		}
		if topic != "" && strings.TrimSpace(record.Topic) != topic {
			continue
		}
		filtered = append(filtered, record)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].ReceivedAt.Equal(filtered[j].ReceivedAt) {
			return strings.TrimSpace(filtered[i].MessageID) > strings.TrimSpace(filtered[j].MessageID)
		}
		return filtered[i].ReceivedAt.After(filtered[j].ReceivedAt)
	})

	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	out := make([]InboxMessage, len(filtered))
	copy(out, filtered)
	return out, nil
}

func (s *FileStore) GetDedupeRecord(ctx context.Context, fromPeerID string, topic string, idempotencyKey string) (DedupeRecord, bool, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return DedupeRecord{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadDedupeRecordsLocked()
	if err != nil {
		return DedupeRecord{}, false, err
	}
	fromPeerID = strings.TrimSpace(fromPeerID)
	topic = strings.TrimSpace(topic)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	now := time.Now().UTC()
	for _, record := range records {
		if strings.TrimSpace(record.FromPeerID) != fromPeerID {
			continue
		}
		if strings.TrimSpace(record.Topic) != topic {
			continue
		}
		if strings.TrimSpace(record.IdempotencyKey) != idempotencyKey {
			continue
		}
		if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(now) {
			continue
		}
		return record, true, nil
	}
	return DedupeRecord{}, false, nil
}

func (s *FileStore) PutDedupeRecord(ctx context.Context, record DedupeRecord) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadDedupeRecordsLocked()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	record.FromPeerID = strings.TrimSpace(record.FromPeerID)
	record.Topic = strings.TrimSpace(record.Topic)
	record.IdempotencyKey = strings.TrimSpace(record.IdempotencyKey)
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = record.CreatedAt.Add(DefaultDedupeTTL)
	}

	replaced := false
	for i := range records {
		if strings.TrimSpace(records[i].FromPeerID) != record.FromPeerID {
			continue
		}
		if strings.TrimSpace(records[i].Topic) != record.Topic {
			continue
		}
		if strings.TrimSpace(records[i].IdempotencyKey) != record.IdempotencyKey {
			continue
		}
		records[i] = record
		replaced = true
		break
	}
	if !replaced {
		records = append(records, record)
	}

	return s.saveDedupeRecordsLocked(records)
}

func (s *FileStore) PruneDedupeRecords(ctx context.Context, now time.Time, maxPerPeer int) (int, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return 0, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if maxPerPeer <= 0 {
		maxPerPeer = DefaultDedupeMaxPerPeer
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadDedupeRecordsLocked()
	if err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}

	active := make([]DedupeRecord, 0, len(records))
	for _, record := range records {
		if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(now) {
			continue
		}
		active = append(active, record)
	}

	sort.Slice(active, func(i, j int) bool {
		leftPeer := strings.TrimSpace(active[i].FromPeerID)
		rightPeer := strings.TrimSpace(active[j].FromPeerID)
		if leftPeer != rightPeer {
			return leftPeer < rightPeer
		}
		if active[i].CreatedAt.Equal(active[j].CreatedAt) {
			leftTopic := strings.TrimSpace(active[i].Topic)
			rightTopic := strings.TrimSpace(active[j].Topic)
			if leftTopic != rightTopic {
				return leftTopic < rightTopic
			}
			return strings.TrimSpace(active[i].IdempotencyKey) < strings.TrimSpace(active[j].IdempotencyKey)
		}
		return active[i].CreatedAt.After(active[j].CreatedAt)
	})

	kept := make([]DedupeRecord, 0, len(active))
	peerCounts := map[string]int{}
	for _, record := range active {
		peerID := strings.TrimSpace(record.FromPeerID)
		if peerCounts[peerID] >= maxPerPeer {
			continue
		}
		peerCounts[peerID]++
		kept = append(kept, record)
	}

	removed := len(records) - len(kept)
	if removed <= 0 {
		return 0, nil
	}
	if err := s.saveDedupeRecordsLocked(kept); err != nil {
		return 0, err
	}
	return removed, nil
}

func (s *FileStore) GetProtocolHistory(ctx context.Context, peerID string) (ProtocolHistory, bool, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return ProtocolHistory{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	history, err := s.loadProtocolHistoryLocked()
	if err != nil {
		return ProtocolHistory{}, false, err
	}
	peerID = strings.TrimSpace(peerID)
	for _, record := range history {
		if strings.TrimSpace(record.PeerID) == peerID {
			return record, true, nil
		}
	}
	return ProtocolHistory{}, false, nil
}

func (s *FileStore) PutProtocolHistory(ctx context.Context, history ProtocolHistory) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadProtocolHistoryLocked()
	if err != nil {
		return err
	}

	history.PeerID = strings.TrimSpace(history.PeerID)
	if history.UpdatedAt.IsZero() {
		history.UpdatedAt = time.Now().UTC()
	}
	replaced := false
	for i := range records {
		if strings.TrimSpace(records[i].PeerID) == history.PeerID {
			records[i] = history
			replaced = true
			break
		}
	}
	if !replaced {
		records = append(records, history)
	}
	return s.saveProtocolHistoryLocked(records)
}

func (s *FileStore) loadContactsLocked() ([]Contact, error) {
	var file contactsFile
	ok, err := s.readJSONFile(s.contactsPath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []Contact{}, nil
	}
	out := make([]Contact, 0, len(file.Contacts))
	for _, c := range file.Contacts {
		out = append(out, c)
	}
	return out, nil
}

func (s *FileStore) saveContactsLocked(contacts []Contact) error {
	if err := os.MkdirAll(s.rootPath(), 0o700); err != nil {
		return fmt.Errorf("create maep state dir: %w", err)
	}

	sort.Slice(contacts, func(i, j int) bool {
		left := strings.TrimSpace(contacts[i].PeerID)
		right := strings.TrimSpace(contacts[j].PeerID)
		if left == right {
			return contacts[i].UpdatedAt.Before(contacts[j].UpdatedAt)
		}
		return left < right
	})

	file := contactsFile{
		Version:  contactsFileVersion,
		Contacts: contacts,
	}
	return s.writeJSONFileAtomic(s.contactsPath(), file, 0o600)
}

func (s *FileStore) loadDedupeRecordsLocked() ([]DedupeRecord, error) {
	var file dedupeFile
	ok, err := s.readJSONFile(s.dedupePath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []DedupeRecord{}, nil
	}
	out := make([]DedupeRecord, 0, len(file.Records))
	for _, record := range file.Records {
		out = append(out, record)
	}
	return out, nil
}

func (s *FileStore) saveDedupeRecordsLocked(records []DedupeRecord) error {
	if err := os.MkdirAll(s.rootPath(), 0o700); err != nil {
		return fmt.Errorf("create maep state dir: %w", err)
	}
	file := dedupeFile{Version: dedupeFileVersion, Records: records}
	return s.writeJSONFileAtomic(s.dedupePath(), file, 0o600)
}

func (s *FileStore) loadInboxMessagesLocked() ([]InboxMessage, error) {
	var file inboxFile
	ok, err := s.readJSONFile(s.inboxPath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []InboxMessage{}, nil
	}
	out := make([]InboxMessage, 0, len(file.Records))
	for _, record := range file.Records {
		out = append(out, record)
	}
	return out, nil
}

func (s *FileStore) saveInboxMessagesLocked(records []InboxMessage) error {
	if err := os.MkdirAll(s.rootPath(), 0o700); err != nil {
		return fmt.Errorf("create maep state dir: %w", err)
	}
	file := inboxFile{Version: inboxFileVersion, Records: records}
	return s.writeJSONFileAtomic(s.inboxPath(), file, 0o600)
}

func (s *FileStore) loadProtocolHistoryLocked() ([]ProtocolHistory, error) {
	var file protocolHistoryFile
	ok, err := s.readJSONFile(s.protocolHistoryPath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []ProtocolHistory{}, nil
	}
	out := make([]ProtocolHistory, 0, len(file.Records))
	for _, record := range file.Records {
		out = append(out, record)
	}
	return out, nil
}

func (s *FileStore) saveProtocolHistoryLocked(records []ProtocolHistory) error {
	if err := os.MkdirAll(s.rootPath(), 0o700); err != nil {
		return fmt.Errorf("create maep state dir: %w", err)
	}
	sort.Slice(records, func(i, j int) bool {
		return strings.TrimSpace(records[i].PeerID) < strings.TrimSpace(records[j].PeerID)
	})
	file := protocolHistoryFile{Version: protocolHistoryFileVersion, Records: records}
	return s.writeJSONFileAtomic(s.protocolHistoryPath(), file, 0o600)
}

func (s *FileStore) readJSONFile(path string, out any) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return false, fmt.Errorf("decode %s: %w", path, err)
	}
	return true, nil
}

func (s *FileStore) writeJSONFileAtomic(path string, v any, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create parent dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json for %s: %w", path, err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file %s: %w", tmpPath, err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("chmod temp file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file to %s: %w", path, err)
	}
	return nil
}

func (s *FileStore) ensureNotCanceled(ctx context.Context) error {
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

func (s *FileStore) rootPath() string {
	root := strings.TrimSpace(s.root)
	if root == "" {
		return "maep"
	}
	return filepath.Clean(root)
}

func (s *FileStore) identityPath() string {
	return filepath.Join(s.rootPath(), "identity.json")
}

func (s *FileStore) contactsPath() string {
	return filepath.Join(s.rootPath(), "contacts.json")
}

func (s *FileStore) dedupePath() string {
	return filepath.Join(s.rootPath(), "dedupe_records.json")
}

func (s *FileStore) inboxPath() string {
	return filepath.Join(s.rootPath(), "inbox_messages.json")
}

func (s *FileStore) protocolHistoryPath() string {
	return filepath.Join(s.rootPath(), "protocol_history.json")
}
