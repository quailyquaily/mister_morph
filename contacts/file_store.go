package contacts

import (
	"bufio"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	refid "github.com/quailyquaily/mistermorph/internal/entryutil/refid"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"gopkg.in/yaml.v3"
)

const (
	busInboxFileVersion  = 1
	busOutboxFileVersion = 1
	maxActiveContacts    = 150
)

type busInboxFile struct {
	Version int              `json:"version"`
	Records []BusInboxRecord `json:"records"`
}

type busOutboxFile struct {
	Version int               `json:"version"`
	Records []BusOutboxRecord `json:"records"`
}

type FileStore struct {
	root string
	mu   sync.Mutex
}

type ContactYAMLBlock struct {
	ContactID string
	Status    Status
	Heading   string
	YAML      string
}

type contactMarkdownBlock struct {
	HeadingLine int
	Heading     string
	YAMLStart   int
	YAMLEnd     int
	YAML        string
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

	active, err := s.loadContactsMarkdownLocked(s.activeContactsPath(), StatusActive)
	if err != nil {
		return Contact{}, false, err
	}
	contactID = strings.TrimSpace(contactID)
	for _, item := range active {
		if strings.TrimSpace(item.ContactID) == contactID {
			return item, true, nil
		}
	}

	inactive, err := s.loadContactsMarkdownLocked(s.inactiveContactsPath(), StatusInactive)
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

func (s *FileStore) GetContactYAML(ctx context.Context, contactID string) (ContactYAMLBlock, bool, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return ContactYAMLBlock{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	activeRaw, activeExists, err := fsstore.ReadText(s.activeContactsPath())
	if err != nil {
		return ContactYAMLBlock{}, false, fmt.Errorf("read contacts markdown %s: %w", s.activeContactsPath(), err)
	}
	if activeExists {
		block, ok, findErr := findContactMarkdownBlock(activeRaw, contactID)
		if findErr != nil {
			return ContactYAMLBlock{}, false, fmt.Errorf("parse contacts markdown %s: %w", s.activeContactsPath(), findErr)
		}
		if ok {
			return ContactYAMLBlock{
				ContactID: strings.TrimSpace(contactID),
				Status:    StatusActive,
				Heading:   block.Heading,
				YAML:      block.YAML,
			}, true, nil
		}
	}

	inactiveRaw, inactiveExists, err := fsstore.ReadText(s.inactiveContactsPath())
	if err != nil {
		return ContactYAMLBlock{}, false, fmt.Errorf("read contacts markdown %s: %w", s.inactiveContactsPath(), err)
	}
	if inactiveExists {
		block, ok, findErr := findContactMarkdownBlock(inactiveRaw, contactID)
		if findErr != nil {
			return ContactYAMLBlock{}, false, fmt.Errorf("parse contacts markdown %s: %w", s.inactiveContactsPath(), findErr)
		}
		if ok {
			return ContactYAMLBlock{
				ContactID: strings.TrimSpace(contactID),
				Status:    StatusInactive,
				Heading:   block.Heading,
				YAML:      block.YAML,
			}, true, nil
		}
	}
	return ContactYAMLBlock{}, false, nil
}

func (s *FileStore) PutContactYAML(ctx context.Context, contactID string, rawYAML string) (ContactYAMLBlock, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return ContactYAMLBlock{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	lockPath, err := s.stateLockPath()
	if err != nil {
		return ContactYAMLBlock{}, err
	}

	var result ContactYAMLBlock
	err = fsstore.WithLock(ctx, lockPath, func() error {
		activeRaw, activeExists, err := fsstore.ReadText(s.activeContactsPath())
		if err != nil {
			return fmt.Errorf("read contacts markdown %s: %w", s.activeContactsPath(), err)
		}
		if activeExists {
			block, ok, findErr := findContactMarkdownBlock(activeRaw, contactID)
			if findErr != nil {
				return fmt.Errorf("parse contacts markdown %s: %w", s.activeContactsPath(), findErr)
			}
			if ok {
				updated, updateErr := updateContactMarkdownBlock(activeRaw, block, StatusActive, contactID, rawYAML)
				if updateErr != nil {
					return updateErr
				}
				if err := fsstore.WriteTextAtomic(s.activeContactsPath(), updated.Content, fsstore.FileOptions{
					DirPerm:  0o700,
					FilePerm: 0o600,
				}); err != nil {
					return err
				}
				result = ContactYAMLBlock{
					ContactID: strings.TrimSpace(contactID),
					Status:    StatusActive,
					Heading:   updated.Heading,
					YAML:      updated.YAML,
				}
				return nil
			}
		}

		inactiveRaw, inactiveExists, err := fsstore.ReadText(s.inactiveContactsPath())
		if err != nil {
			return fmt.Errorf("read contacts markdown %s: %w", s.inactiveContactsPath(), err)
		}
		if inactiveExists {
			block, ok, findErr := findContactMarkdownBlock(inactiveRaw, contactID)
			if findErr != nil {
				return fmt.Errorf("parse contacts markdown %s: %w", s.inactiveContactsPath(), findErr)
			}
			if ok {
				updated, updateErr := updateContactMarkdownBlock(inactiveRaw, block, StatusInactive, contactID, rawYAML)
				if updateErr != nil {
					return updateErr
				}
				if err := fsstore.WriteTextAtomic(s.inactiveContactsPath(), updated.Content, fsstore.FileOptions{
					DirPerm:  0o700,
					FilePerm: 0o600,
				}); err != nil {
					return err
				}
				result = ContactYAMLBlock{
					ContactID: strings.TrimSpace(contactID),
					Status:    StatusInactive,
					Heading:   updated.Heading,
					YAML:      updated.YAML,
				}
				return nil
			}
		}
		return fmt.Errorf("contact not found: %s", strings.TrimSpace(contactID))
	})
	if err != nil {
		return ContactYAMLBlock{}, err
	}
	return result, nil
}

func (s *FileStore) DeleteContactYAML(ctx context.Context, contactID string) (ContactYAMLBlock, bool, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return ContactYAMLBlock{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	lockPath, err := s.stateLockPath()
	if err != nil {
		return ContactYAMLBlock{}, false, err
	}

	var result ContactYAMLBlock
	var deleted bool
	err = fsstore.WithLock(ctx, lockPath, func() error {
		activeRaw, activeExists, err := fsstore.ReadText(s.activeContactsPath())
		if err != nil {
			return fmt.Errorf("read contacts markdown %s: %w", s.activeContactsPath(), err)
		}
		if activeExists {
			block, ok, findErr := findContactMarkdownBlock(activeRaw, contactID)
			if findErr != nil {
				return fmt.Errorf("parse contacts markdown %s: %w", s.activeContactsPath(), findErr)
			}
			if ok {
				next, deleteErr := deleteContactMarkdownBlock(activeRaw, block)
				if deleteErr != nil {
					return deleteErr
				}
				if err := fsstore.WriteTextAtomic(s.activeContactsPath(), next, fsstore.FileOptions{
					DirPerm:  0o700,
					FilePerm: 0o600,
				}); err != nil {
					return err
				}
				result = ContactYAMLBlock{
					ContactID: strings.TrimSpace(contactID),
					Status:    StatusActive,
					Heading:   block.Heading,
					YAML:      block.YAML,
				}
				deleted = true
				return nil
			}
		}

		inactiveRaw, inactiveExists, err := fsstore.ReadText(s.inactiveContactsPath())
		if err != nil {
			return fmt.Errorf("read contacts markdown %s: %w", s.inactiveContactsPath(), err)
		}
		if inactiveExists {
			block, ok, findErr := findContactMarkdownBlock(inactiveRaw, contactID)
			if findErr != nil {
				return fmt.Errorf("parse contacts markdown %s: %w", s.inactiveContactsPath(), findErr)
			}
			if ok {
				next, deleteErr := deleteContactMarkdownBlock(inactiveRaw, block)
				if deleteErr != nil {
					return deleteErr
				}
				if err := fsstore.WriteTextAtomic(s.inactiveContactsPath(), next, fsstore.FileOptions{
					DirPerm:  0o700,
					FilePerm: 0o600,
				}); err != nil {
					return err
				}
				result = ContactYAMLBlock{
					ContactID: strings.TrimSpace(contactID),
					Status:    StatusInactive,
					Heading:   block.Heading,
					YAML:      block.YAML,
				}
				deleted = true
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return ContactYAMLBlock{}, false, err
	}
	return result, deleted, nil
}

func (s *FileStore) PutContact(ctx context.Context, contact Contact) error {
	if err := ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lockPath, err := s.stateLockPath()
	if err != nil {
		return err
	}

	return fsstore.WithLock(ctx, lockPath, func() error {
		now := time.Now().UTC()
		contact = normalizeContact(contact, now)

		active, err := s.loadContactsMarkdownLocked(s.activeContactsPath(), StatusActive)
		if err != nil {
			return err
		}
		inactive, err := s.loadContactsMarkdownLocked(s.inactiveContactsPath(), StatusInactive)
		if err != nil {
			return err
		}

		targetStatus := StatusActive
		if hasContactID(inactive, contact.ContactID) {
			targetStatus = StatusInactive
		}

		active = removeContactByID(active, contact.ContactID)
		inactive = removeContactByID(inactive, contact.ContactID)

		if targetStatus == StatusInactive {
			inactive = append(inactive, contact)
		} else {
			active = append(active, contact)
		}
		active, overflow := trimActiveContactsByInteraction(active, maxActiveContacts)
		if len(overflow) > 0 {
			inactive = append(inactive, overflow...)
		}

		if err := s.saveContactsMarkdownLocked(s.activeContactsPath(), "Active Contacts", StatusActive, active); err != nil {
			return err
		}
		if err := s.saveContactsMarkdownLocked(s.inactiveContactsPath(), "Inactive Contacts", StatusInactive, inactive); err != nil {
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

	switch strings.ToLower(strings.TrimSpace(string(status))) {
	case string(StatusActive):
		return s.loadContactsMarkdownLocked(s.activeContactsPath(), StatusActive)
	case string(StatusInactive):
		return s.loadContactsMarkdownLocked(s.inactiveContactsPath(), StatusInactive)
	default:
		active, err := s.loadContactsMarkdownLocked(s.activeContactsPath(), StatusActive)
		if err != nil {
			return nil, err
		}
		inactive, err := s.loadContactsMarkdownLocked(s.inactiveContactsPath(), StatusInactive)
		if err != nil {
			return nil, err
		}
		out := make([]Contact, 0, len(active)+len(inactive))
		out = append(out, active...)
		out = append(out, inactive...)
		sort.Slice(out, func(i, j int) bool {
			return strings.TrimSpace(out[i].ContactID) < strings.TrimSpace(out[j].ContactID)
		})
		return out, nil
	}
}

func (s *FileStore) GetBusInboxRecord(ctx context.Context, channel string, platformMessageID string) (BusInboxRecord, bool, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return BusInboxRecord{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadBusInboxLocked()
	if err != nil {
		return BusInboxRecord{}, false, err
	}
	key, err := busInboxRecordKey(channel, platformMessageID)
	if err != nil {
		return BusInboxRecord{}, false, err
	}
	for _, item := range records {
		itemKey, keyErr := busInboxRecordKey(item.Channel, item.PlatformMessageID)
		if keyErr != nil {
			return BusInboxRecord{}, false, keyErr
		}
		if itemKey == key {
			return item, true, nil
		}
	}
	return BusInboxRecord{}, false, nil
}

func (s *FileStore) PutBusInboxRecord(ctx context.Context, record BusInboxRecord) error {
	if err := ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lockPath, err := s.stateLockPath()
	if err != nil {
		return err
	}
	return fsstore.WithLock(ctx, lockPath, func() error {
		records, err := s.loadBusInboxLocked()
		if err != nil {
			return err
		}
		normalized, err := normalizeBusInboxRecord(record)
		if err != nil {
			return err
		}
		key, err := busInboxRecordKey(normalized.Channel, normalized.PlatformMessageID)
		if err != nil {
			return err
		}
		replaced := false
		for i := range records {
			itemKey, keyErr := busInboxRecordKey(records[i].Channel, records[i].PlatformMessageID)
			if keyErr != nil {
				return keyErr
			}
			if itemKey != key {
				continue
			}
			records[i] = normalized
			replaced = true
			break
		}
		if !replaced {
			records = append(records, normalized)
		}
		return s.saveBusInboxLocked(records)
	})
}

func (s *FileStore) GetBusOutboxRecord(ctx context.Context, channel string, idempotencyKey string) (BusOutboxRecord, bool, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return BusOutboxRecord{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadBusOutboxLocked()
	if err != nil {
		return BusOutboxRecord{}, false, err
	}
	key, err := busOutboxRecordKey(channel, idempotencyKey)
	if err != nil {
		return BusOutboxRecord{}, false, err
	}
	for _, item := range records {
		itemKey, keyErr := busOutboxRecordKey(item.Channel, item.IdempotencyKey)
		if keyErr != nil {
			return BusOutboxRecord{}, false, keyErr
		}
		if itemKey == key {
			return item, true, nil
		}
	}
	return BusOutboxRecord{}, false, nil
}

func (s *FileStore) PutBusOutboxRecord(ctx context.Context, record BusOutboxRecord) error {
	if err := ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lockPath, err := s.stateLockPath()
	if err != nil {
		return err
	}
	return fsstore.WithLock(ctx, lockPath, func() error {
		records, err := s.loadBusOutboxLocked()
		if err != nil {
			return err
		}
		normalized, err := normalizeBusOutboxRecord(record)
		if err != nil {
			return err
		}
		key, err := busOutboxRecordKey(normalized.Channel, normalized.IdempotencyKey)
		if err != nil {
			return err
		}
		replaced := false
		for i := range records {
			itemKey, keyErr := busOutboxRecordKey(records[i].Channel, records[i].IdempotencyKey)
			if keyErr != nil {
				return keyErr
			}
			if itemKey != key {
				continue
			}
			if records[i].CreatedAt.IsZero() {
				records[i].CreatedAt = normalized.CreatedAt
			}
			normalized.CreatedAt = records[i].CreatedAt
			records[i] = normalized
			replaced = true
			break
		}
		if !replaced {
			records = append(records, normalized)
		}
		return s.saveBusOutboxLocked(records)
	})
}

func (s *FileStore) loadContactsMarkdownLocked(path string, status Status) ([]Contact, error) {
	content, exists, err := fsstore.ReadText(path)
	if err != nil {
		return nil, fmt.Errorf("read contacts markdown %s: %w", path, err)
	}
	if !exists {
		return []Contact{}, nil
	}
	records, err := parseContactsMarkdown(content, status)
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

func (s *FileStore) saveContactsMarkdownLocked(path string, title string, status Status, records []Contact) error {
	rendered, err := renderContactsMarkdown(title, status, records)
	if err != nil {
		return err
	}
	return fsstore.WriteTextAtomic(path, rendered, fsstore.FileOptions{
		DirPerm:  0o700,
		FilePerm: 0o600,
	})
}

func parseContactsMarkdown(content string, status Status) ([]Contact, error) {
	return parseContactsProfileMarkdown(content, status)
}

type contactProfileSection struct {
	ContactID         string   `yaml:"contact_id"`
	Nickname          string   `yaml:"nickname"`
	Kind              string   `yaml:"kind"`
	Channel           string   `yaml:"channel"`
	TGUsername        string   `yaml:"tg_username"`
	TGPrivateChatID   string   `yaml:"tg_private_chat_id"`
	TGGroupChatIDs    []string `yaml:"tg_group_chat_ids"`
	LineUserID        string   `yaml:"line_user_id"`
	LineChatIDs       []string `yaml:"line_chat_ids"`
	LarkOpenID        string   `yaml:"lark_open_id"`
	LarkChatIDs       []string `yaml:"lark_chat_ids"`
	SlackTeamID       string   `yaml:"slack_team_id"`
	SlackUserID       string   `yaml:"slack_user_id"`
	SlackDMChannelID  string   `yaml:"slack_dm_channel_id"`
	SlackChannelIDs   []string `yaml:"slack_channel_ids"`
	PersonaBrief      string   `yaml:"persona_brief"`
	TopicPreferences  []string `yaml:"topic_preferences"`
	CooldownUntil     string   `yaml:"cooldown_until"`
	LastInteractionAt string   `yaml:"last_interaction_at"`
}

type updatedContactMarkdownBlock struct {
	Content string
	Heading string
	YAML    string
}

func parseContactsProfileMarkdown(content string, status Status) ([]Contact, error) {
	clean := stripMarkdownHTMLComments(content)
	scanner := bufio.NewScanner(strings.NewReader(clean))
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	out := make([]Contact, 0, 64)
	sectionTitle := ""
	inYAML := false
	yamlLines := make([]string, 0, 32)

	flushProfile := func() error {
		if len(yamlLines) == 0 {
			yamlLines = yamlLines[:0]
			return nil
		}
		var profile contactProfileSection
		if err := yaml.Unmarshal([]byte(strings.Join(yamlLines, "\n")), &profile); err != nil {
			return err
		}
		item, err := contactFromProfileSection(sectionTitle, profile, status)
		if err != nil {
			return err
		}
		out = append(out, item)
		yamlLines = yamlLines[:0]
		return nil
	}

	for scanner.Scan() {
		rawLine := scanner.Text()
		line := strings.TrimSpace(rawLine)

		if !inYAML && strings.HasPrefix(line, "## ") {
			sectionTitle = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}

		if !inYAML && strings.HasPrefix(line, "```") {
			lowerFence := strings.ToLower(line)
			if strings.HasPrefix(lowerFence, "```yaml") || strings.HasPrefix(lowerFence, "```yml") {
				inYAML = true
				yamlLines = yamlLines[:0]
			}
			continue
		}

		if inYAML && strings.HasPrefix(line, "```") {
			if err := flushProfile(); err != nil {
				return nil, err
			}
			inYAML = false
			continue
		}

		if inYAML {
			yamlLines = append(yamlLines, rawLine)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if inYAML {
		if err := flushProfile(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func findContactMarkdownBlock(content string, contactID string) (contactMarkdownBlock, bool, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	contactID = strings.TrimSpace(contactID)
	sectionTitle := ""
	headingLine := -1
	inYAML := false
	yamlStart := -1

	flush := func(yamlEnd int) (contactMarkdownBlock, bool, error) {
		if yamlStart < 0 || yamlEnd < yamlStart {
			return contactMarkdownBlock{}, false, nil
		}
		raw := strings.Join(lines[yamlStart:yamlEnd], "\n")
		var profile contactProfileSection
		if err := yaml.Unmarshal([]byte(raw), &profile); err != nil {
			return contactMarkdownBlock{}, false, err
		}
		if strings.TrimSpace(profile.ContactID) != contactID {
			return contactMarkdownBlock{}, false, nil
		}
		return contactMarkdownBlock{
			HeadingLine: headingLine,
			Heading:     sectionTitle,
			YAMLStart:   yamlStart,
			YAMLEnd:     yamlEnd,
			YAML:        raw,
		}, true, nil
	}

	for i, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if !inYAML && strings.HasPrefix(line, "## ") {
			sectionTitle = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			headingLine = i
			continue
		}
		if !inYAML && strings.HasPrefix(line, "```") {
			lowerFence := strings.ToLower(line)
			if strings.HasPrefix(lowerFence, "```yaml") || strings.HasPrefix(lowerFence, "```yml") {
				inYAML = true
				yamlStart = i + 1
			}
			continue
		}
		if inYAML && strings.HasPrefix(line, "```") {
			block, ok, err := flush(i)
			if err != nil || ok {
				return block, ok, err
			}
			inYAML = false
			yamlStart = -1
		}
	}
	if inYAML {
		return flush(len(lines))
	}
	return contactMarkdownBlock{}, false, nil
}

func updateContactMarkdownBlock(content string, block contactMarkdownBlock, status Status, contactID string, rawYAML string) (updatedContactMarkdownBlock, error) {
	normalizedYAML := strings.Trim(strings.ReplaceAll(rawYAML, "\r\n", "\n"), "\n")
	if normalizedYAML == "" {
		return updatedContactMarkdownBlock{}, fmt.Errorf("yaml is required")
	}
	profile, _, err := validateContactYAMLBlock(normalizedYAML, contactID, status, block.Heading)
	if err != nil {
		return updatedContactMarkdownBlock{}, err
	}

	nextHeading := strings.TrimSpace(profile.Nickname)
	if nextHeading == "" {
		nextHeading = strings.TrimSpace(profile.ContactID)
	}
	if nextHeading == "" {
		nextHeading = strings.TrimSpace(block.Heading)
	}

	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if block.HeadingLine >= 0 && block.HeadingLine < len(lines) && nextHeading != "" {
		lines[block.HeadingLine] = "## " + nextHeading
	}
	replacement := []string{}
	if normalizedYAML != "" {
		replacement = strings.Split(normalizedYAML, "\n")
	}
	out := make([]string, 0, len(lines)-max(0, block.YAMLEnd-block.YAMLStart)+len(replacement))
	out = append(out, lines[:block.YAMLStart]...)
	out = append(out, replacement...)
	out = append(out, lines[block.YAMLEnd:]...)
	return updatedContactMarkdownBlock{
		Content: strings.Join(out, "\n"),
		Heading: nextHeading,
		YAML:    normalizedYAML,
	}, nil
}

func deleteContactMarkdownBlock(content string, block contactMarkdownBlock) (string, error) {
	if block.HeadingLine < 0 {
		return "", fmt.Errorf("contact section heading not found")
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	sectionEnd := len(lines)
	for i := block.YAMLEnd; i < len(lines); i++ {
		if i <= block.YAMLEnd {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			sectionEnd = i
			break
		}
	}
	out := make([]string, 0, len(lines)-max(0, sectionEnd-block.HeadingLine))
	out = append(out, lines[:block.HeadingLine]...)
	out = append(out, lines[sectionEnd:]...)
	return strings.Join(out, "\n"), nil
}

func validateContactYAMLBlock(rawYAML string, contactID string, status Status, heading string) (contactProfileSection, Contact, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(rawYAML), &root); err != nil {
		return contactProfileSection{}, Contact{}, err
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = *root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return contactProfileSection{}, Contact{}, fmt.Errorf("contact yaml must be a mapping")
	}
	var profile contactProfileSection
	if err := yaml.Unmarshal([]byte(rawYAML), &profile); err != nil {
		return contactProfileSection{}, Contact{}, err
	}
	profile.ContactID = strings.TrimSpace(profile.ContactID)
	if profile.ContactID == "" {
		return contactProfileSection{}, Contact{}, fmt.Errorf("contact_id is required")
	}
	if profile.ContactID != strings.TrimSpace(contactID) {
		return contactProfileSection{}, Contact{}, fmt.Errorf("contact_id cannot change")
	}
	contact, err := contactFromProfileSection(heading, profile, status)
	if err != nil {
		return contactProfileSection{}, Contact{}, err
	}
	return profile, contact, nil
}

func renderContactsMarkdown(title string, status Status, records []Contact) (string, error) {
	status = normalizeStatus(status)
	title = strings.TrimSpace(title)
	if title == "" {
		switch status {
		case StatusInactive:
			title = "Inactive Contacts"
		default:
			title = "Active Contacts"
		}
	}

	items := make([]Contact, 0, len(records))
	for _, item := range records {
		items = append(items, normalizeContact(item, time.Now().UTC()))
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.TrimSpace(items[i].ContactID) < strings.TrimSpace(items[j].ContactID)
	})

	nowRFC3339 := time.Now().UTC().Format(time.RFC3339)
	fm := map[string]string{
		"created_at": nowRFC3339,
		"updated_at": nowRFC3339,
	}
	fmRaw, err := yaml.Marshal(fm)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(string(fmRaw))
	b.WriteString("---\n\n")
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString("<!-- One contact profile per section, as YAML code block. -->\n\n")

	for _, item := range items {
		profile, heading := profileSectionFromContact(item)
		raw, err := yaml.Marshal(profile)
		if err != nil {
			return "", err
		}
		if heading == "" {
			heading = strings.TrimSpace(item.ContactID)
		}
		if heading == "" {
			heading = "Unnamed Contact"
		}
		b.WriteString("## ")
		b.WriteString(heading)
		b.WriteString("\n\n```yaml\n")
		b.Write(raw)
		b.WriteString("```\n\n")
	}
	return b.String(), nil
}

func stripMarkdownHTMLComments(content string) string {
	out := content
	for {
		start := strings.Index(out, "<!--")
		if start < 0 {
			return out
		}
		end := strings.Index(out[start+4:], "-->")
		if end < 0 {
			return out[:start]
		}
		out = out[:start] + out[start+4+end+3:]
	}
}

func contactFromProfileSection(title string, profile contactProfileSection, status Status) (Contact, error) {
	now := time.Now().UTC()
	_ = status
	contact := Contact{
		ContactID:        strings.TrimSpace(profile.ContactID),
		ContactNickname:  strings.TrimSpace(profile.Nickname),
		Channel:          strings.ToLower(strings.TrimSpace(profile.Channel)),
		TGUsername:       normalizeTelegramUsername(profile.TGUsername),
		LineUserID:       refid.NormalizeLineID(profile.LineUserID),
		LineChatIDs:      normalizeStringSlice(profile.LineChatIDs),
		LarkOpenID:       refid.NormalizeLarkID(profile.LarkOpenID),
		LarkChatIDs:      normalizeStringSlice(profile.LarkChatIDs),
		SlackTeamID:      normalizeSlackID(profile.SlackTeamID),
		SlackUserID:      normalizeSlackID(profile.SlackUserID),
		SlackDMChannelID: normalizeSlackID(profile.SlackDMChannelID),
		SlackChannelIDs:  normalizeStringSlice(profile.SlackChannelIDs),
		PersonaBrief:     strings.TrimSpace(profile.PersonaBrief),
		TopicPreferences: normalizeStringSlice(profile.TopicPreferences),
	}
	if contact.ContactNickname == "" && strings.TrimSpace(profile.ContactID) == "" {
		contact.ContactNickname = strings.TrimSpace(title)
	}

	privateChatID, err := parseTelegramChatID(profile.TGPrivateChatID)
	if err != nil {
		return Contact{}, err
	}
	if privateChatID > 0 {
		contact.TGPrivateChatID = privateChatID
	}
	groupChatIDs := make([]int64, 0, len(profile.TGGroupChatIDs))
	for _, raw := range profile.TGGroupChatIDs {
		id, parseErr := parseTelegramChatID(raw)
		if parseErr != nil {
			return Contact{}, parseErr
		}
		if id != 0 {
			groupChatIDs = append(groupChatIDs, id)
		}
	}
	contact.TGGroupChatIDs = groupChatIDs

	kindRaw := strings.ToLower(strings.TrimSpace(profile.Kind))
	switch kindRaw {
	case string(KindAgent):
		contact.Kind = KindAgent
	case string(KindHuman):
		contact.Kind = KindHuman
	case "":
		channel := strings.ToLower(strings.TrimSpace(profile.Channel))
		switch channel {
		case "", ChannelTelegram, ChannelSlack, ChannelLine, ChannelLark, "discord":
			contact.Kind = KindHuman
		default:
			contact.Kind = KindAgent
		}
	default:
		return Contact{}, fmt.Errorf("invalid contact kind: %s", profile.Kind)
	}

	if profile.CooldownUntil != "" {
		ts, parseErr := parseRFC3339Timestamp(profile.CooldownUntil)
		if parseErr != nil {
			return Contact{}, parseErr
		}
		contact.CooldownUntil = &ts
	}
	if profile.LastInteractionAt != "" {
		ts, parseErr := parseRFC3339Timestamp(profile.LastInteractionAt)
		if parseErr != nil {
			return Contact{}, parseErr
		}
		contact.LastInteractionAt = &ts
	}

	contact = normalizeContact(contact, now)
	if contact.ContactID == "" {
		contact.ContactID = deriveContactID(contact)
	}
	if contact.ContactID == "" {
		contact.ContactID = "contact:" + slugToken(contact.ContactNickname)
	}
	contact = normalizeContact(contact, now)
	return contact, nil
}

func profileSectionFromContact(contact Contact) (contactProfileSection, string) {
	contact = normalizeContact(contact, time.Now().UTC())
	profile := contactProfileSection{
		ContactID:        strings.TrimSpace(contact.ContactID),
		Nickname:         strings.TrimSpace(contact.ContactNickname),
		Kind:             string(normalizeKind(contact.Kind)),
		Channel:          strings.TrimSpace(contact.Channel),
		TGUsername:       normalizeTelegramUsername(contact.TGUsername),
		LineUserID:       refid.NormalizeLineID(contact.LineUserID),
		LineChatIDs:      normalizeStringSlice(contact.LineChatIDs),
		LarkOpenID:       refid.NormalizeLarkID(contact.LarkOpenID),
		LarkChatIDs:      normalizeStringSlice(contact.LarkChatIDs),
		SlackTeamID:      normalizeSlackID(contact.SlackTeamID),
		SlackUserID:      normalizeSlackID(contact.SlackUserID),
		SlackDMChannelID: normalizeSlackID(contact.SlackDMChannelID),
		SlackChannelIDs:  normalizeStringSlice(contact.SlackChannelIDs),
		PersonaBrief:     strings.TrimSpace(contact.PersonaBrief),
		TopicPreferences: normalizeStringSlice(contact.TopicPreferences),
	}

	if profile.Channel == "" {
		switch {
		case contact.TGPrivateChatID != 0 || len(contact.TGGroupChatIDs) > 0 || profile.TGUsername != "":
			profile.Channel = ChannelTelegram
		case profile.LineUserID != "" || len(profile.LineChatIDs) > 0 || strings.HasPrefix(strings.ToLower(profile.ContactID), "line:") || strings.HasPrefix(strings.ToLower(profile.ContactID), "line_user:"):
			profile.Channel = ChannelLine
		case profile.LarkOpenID != "" || len(profile.LarkChatIDs) > 0 || strings.HasPrefix(strings.ToLower(profile.ContactID), "lark:") || strings.HasPrefix(strings.ToLower(profile.ContactID), "lark_user:"):
			profile.Channel = ChannelLark
		case profile.SlackTeamID != "" || profile.SlackUserID != "" || profile.SlackDMChannelID != "" || len(profile.SlackChannelIDs) > 0 || strings.HasPrefix(strings.ToLower(profile.ContactID), "slack:"):
			profile.Channel = ChannelSlack
		default:
			profile.Channel = strings.TrimSpace(string(normalizeKind(contact.Kind)))
		}
	}

	if profile.TGUsername == "" {
		if alias := extractTelegramAlias(profile.ContactID); alias != "" {
			profile.TGUsername = alias
		}
	}
	if contact.TGPrivateChatID != 0 {
		profile.TGPrivateChatID = strconv.FormatInt(contact.TGPrivateChatID, 10)
	}
	if len(contact.TGGroupChatIDs) > 0 {
		ids := make([]int64, 0, len(contact.TGGroupChatIDs))
		ids = append(ids, contact.TGGroupChatIDs...)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		out := make([]string, 0, len(ids))
		seen := map[string]bool{}
		for _, id := range ids {
			value := strconv.FormatInt(id, 10)
			if seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
		profile.TGGroupChatIDs = out
	}
	if profile.SlackTeamID == "" || profile.SlackUserID == "" {
		teamID, userOrChannelID, ok := parseSlackContactID(profile.ContactID)
		if ok {
			if profile.SlackTeamID == "" {
				profile.SlackTeamID = teamID
			}
			if profile.SlackUserID == "" && strings.HasPrefix(strings.ToUpper(userOrChannelID), "U") {
				profile.SlackUserID = userOrChannelID
			}
		}
	}
	if profile.SlackDMChannelID == "" || len(profile.SlackChannelIDs) == 0 {
		_, userOrChannelID, ok := parseSlackContactID(profile.ContactID)
		if ok {
			id := normalizeSlackID(userOrChannelID)
			switch {
			case profile.SlackDMChannelID == "" && strings.HasPrefix(strings.ToUpper(id), "D"):
				profile.SlackDMChannelID = id
			case len(profile.SlackChannelIDs) == 0 && (strings.HasPrefix(strings.ToUpper(id), "C") || strings.HasPrefix(strings.ToUpper(id), "G")):
				profile.SlackChannelIDs = []string{id}
			}
		}
	}
	if profile.LineUserID == "" {
		if userID, ok := refid.ParseLineUserContactID(profile.ContactID); ok {
			profile.LineUserID = userID
		}
	}
	if len(profile.LineChatIDs) == 0 {
		if chatID, ok := refid.ParseLineChatContactID(profile.ContactID); ok {
			profile.LineChatIDs = []string{chatID}
		}
	}
	if profile.LarkOpenID == "" {
		if openID, ok := refid.ParseLarkUserContactID(profile.ContactID); ok {
			profile.LarkOpenID = openID
		}
	}
	if len(profile.LarkChatIDs) == 0 {
		if chatID, ok := refid.ParseLarkChatContactID(profile.ContactID); ok {
			profile.LarkChatIDs = []string{chatID}
		}
	}
	if contact.CooldownUntil != nil && !contact.CooldownUntil.IsZero() {
		profile.CooldownUntil = contact.CooldownUntil.UTC().Format(time.RFC3339)
	}
	if contact.LastInteractionAt != nil && !contact.LastInteractionAt.IsZero() {
		profile.LastInteractionAt = contact.LastInteractionAt.UTC().Format(time.RFC3339)
	}

	heading := strings.TrimSpace(contact.ContactNickname)
	if heading == "" {
		heading = strings.TrimSpace(contact.ContactID)
	}
	return profile, heading
}

func parseRFC3339Timestamp(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid RFC3339 timestamp %q", raw)
	}
	return ts.UTC(), nil
}

func parseTelegramChatID(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	if strings.HasPrefix(strings.ToLower(value), "tg:") {
		value = strings.TrimSpace(value[len("tg:"):])
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid telegram chat id %q", raw)
	}
	return id, nil
}

func normalizeTelegramUsername(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "@")
	return strings.TrimSpace(value)
}

func extractTelegramAlias(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "tg:@") {
		return strings.TrimPrefix(raw, "tg:@")
	}
	if strings.HasPrefix(raw, "@") {
		return strings.TrimPrefix(raw, "@")
	}
	return ""
}

func normalizeSlackID(raw string) string {
	return strings.TrimSpace(raw)
}

func parseSlackContactID(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), "slack:") {
		return "", "", false
	}
	parts := strings.Split(raw[len("slack:"):], ":")
	if len(parts) != 2 {
		return "", "", false
	}
	teamID := normalizeSlackID(parts[0])
	userOrChannelID := normalizeSlackID(parts[1])
	if teamID == "" || userOrChannelID == "" {
		return "", "", false
	}
	return teamID, userOrChannelID, true
}

func slugToken(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "unnamed"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		isLetter := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		if isLetter || isDigit {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	token := strings.Trim(b.String(), "-")
	if token == "" {
		return "unnamed"
	}
	return token
}

func removeContactByID(items []Contact, contactID string) []Contact {
	out := items[:0]
	contactID = strings.TrimSpace(contactID)
	for _, item := range items {
		if strings.TrimSpace(item.ContactID) == contactID {
			continue
		}
		out = append(out, item)
	}
	return out
}

func trimActiveContactsByInteraction(active []Contact, limit int) ([]Contact, []Contact) {
	if limit <= 0 || len(active) <= limit {
		return active, nil
	}
	ranked := append([]Contact(nil), active...)
	sort.Slice(ranked, func(i, j int) bool {
		left := contactInteractionTimestamp(ranked[i])
		right := contactInteractionTimestamp(ranked[j])
		if left.Equal(right) {
			return strings.TrimSpace(ranked[i].ContactID) < strings.TrimSpace(ranked[j].ContactID)
		}
		return left.After(right)
	})
	kept := append([]Contact(nil), ranked[:limit]...)
	moved := append([]Contact(nil), ranked[limit:]...)
	return kept, moved
}

func contactInteractionTimestamp(contact Contact) time.Time {
	if contact.LastInteractionAt == nil || contact.LastInteractionAt.IsZero() {
		return time.Time{}
	}
	return contact.LastInteractionAt.UTC()
}

func hasContactID(items []Contact, contactID string) bool {
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return false
	}
	for _, item := range items {
		if strings.TrimSpace(item.ContactID) == contactID {
			return true
		}
	}
	return false
}

func (s *FileStore) loadBusInboxLocked() ([]BusInboxRecord, error) {
	var file busInboxFile
	ok, err := fsstore.ReadJSONStrict(s.busInboxPath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []BusInboxRecord{}, nil
	}
	if file.Version != busInboxFileVersion {
		return nil, fmt.Errorf("unsupported bus inbox file version: %d", file.Version)
	}
	out := make([]BusInboxRecord, 0, len(file.Records))
	for _, item := range file.Records {
		normalized, normalizeErr := normalizeBusInboxRecord(item)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		out = append(out, normalized)
	}
	sortBusInboxRecords(out)
	return out, nil
}

func (s *FileStore) saveBusInboxLocked(records []BusInboxRecord) error {
	sortBusInboxRecords(records)
	file := busInboxFile{Version: busInboxFileVersion, Records: records}
	return fsstore.WriteJSONAtomic(s.busInboxPath(), file, fsstore.FileOptions{
		DirPerm:  0o700,
		FilePerm: 0o600,
	})
}

func (s *FileStore) loadBusOutboxLocked() ([]BusOutboxRecord, error) {
	var file busOutboxFile
	ok, err := fsstore.ReadJSONStrict(s.busOutboxPath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []BusOutboxRecord{}, nil
	}
	if file.Version != busOutboxFileVersion {
		return nil, fmt.Errorf("unsupported bus outbox file version: %d", file.Version)
	}
	out := make([]BusOutboxRecord, 0, len(file.Records))
	for _, item := range file.Records {
		normalized, normalizeErr := normalizeBusOutboxRecord(item)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		out = append(out, normalized)
	}
	sortBusOutboxRecords(out)
	return out, nil
}

func (s *FileStore) saveBusOutboxLocked(records []BusOutboxRecord) error {
	sortBusOutboxRecords(records)
	file := busOutboxFile{Version: busOutboxFileVersion, Records: records}
	return fsstore.WriteJSONAtomic(s.busOutboxPath(), file, fsstore.FileOptions{
		DirPerm:  0o700,
		FilePerm: 0o600,
	})
}

func (s *FileStore) stateLockPath() (string, error) {
	return fsstore.BuildLockPath(filepath.Join(s.rootPath(), ".fslocks"), "state.main")
}

func (s *FileStore) rootPath() string {
	root := strings.TrimSpace(s.root)
	if root == "" {
		return "contacts"
	}
	return filepath.Clean(root)
}

func (s *FileStore) activeContactsPath() string {
	return filepath.Join(s.rootPath(), "ACTIVE.md")
}

func (s *FileStore) inactiveContactsPath() string {
	return filepath.Join(s.rootPath(), "INACTIVE.md")
}

func (s *FileStore) busInboxPath() string {
	return filepath.Join(s.rootPath(), "bus_inbox.json")
}

func (s *FileStore) busOutboxPath() string {
	return filepath.Join(s.rootPath(), "bus_outbox.json")
}

func normalizeContact(c Contact, now time.Time) Contact {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	c.ContactID = strings.TrimSpace(c.ContactID)
	c.ContactNickname = strings.TrimSpace(c.ContactNickname)
	c.PersonaBrief = strings.TrimSpace(c.PersonaBrief)
	c.TGUsername = normalizeTelegramUsername(c.TGUsername)
	c.LineUserID = refid.NormalizeLineID(c.LineUserID)
	c.LineChatIDs = normalizeStringSlice(c.LineChatIDs)
	for i := range c.LineChatIDs {
		c.LineChatIDs[i] = refid.NormalizeLineID(c.LineChatIDs[i])
	}
	c.LineChatIDs = normalizeStringSlice(c.LineChatIDs)
	c.LarkOpenID = refid.NormalizeLarkID(c.LarkOpenID)
	c.LarkChatIDs = normalizeStringSlice(c.LarkChatIDs)
	for i := range c.LarkChatIDs {
		c.LarkChatIDs[i] = refid.NormalizeLarkID(c.LarkChatIDs[i])
	}
	c.LarkChatIDs = normalizeStringSlice(c.LarkChatIDs)
	c.SlackTeamID = normalizeSlackID(c.SlackTeamID)
	c.SlackUserID = normalizeSlackID(c.SlackUserID)
	c.SlackDMChannelID = normalizeSlackID(c.SlackDMChannelID)
	c.SlackChannelIDs = normalizeStringSlice(c.SlackChannelIDs)
	for i := range c.SlackChannelIDs {
		c.SlackChannelIDs[i] = normalizeSlackID(c.SlackChannelIDs[i])
	}
	c.SlackChannelIDs = normalizeStringSlice(c.SlackChannelIDs)
	c.Kind = normalizeKind(c.Kind)
	c.Channel = normalizeContactChannel(c.Channel)
	c.TGGroupChatIDs = normalizeInt64Slice(c.TGGroupChatIDs)
	if len(c.LineChatIDs) == 0 {
		c.LineChatIDs = nil
	}
	if len(c.LarkChatIDs) == 0 {
		c.LarkChatIDs = nil
	}
	if len(c.SlackChannelIDs) == 0 {
		c.SlackChannelIDs = nil
	}
	if c.TGPrivateChatID <= 0 {
		c.TGPrivateChatID = 0
	}
	c.TopicPreferences = normalizeStringSlice(c.TopicPreferences)
	if len(c.TopicPreferences) == 0 {
		c.TopicPreferences = nil
	}

	if c.CooldownUntil != nil {
		ts := c.CooldownUntil.UTC()
		if ts.IsZero() {
			c.CooldownUntil = nil
		} else {
			c.CooldownUntil = &ts
		}
	}
	if c.LastInteractionAt != nil {
		ts := c.LastInteractionAt.UTC()
		if ts.IsZero() {
			c.LastInteractionAt = nil
		} else {
			c.LastInteractionAt = &ts
		}
	}

	if c.ContactID == "" {
		c.ContactID = deriveContactID(c)
	}
	if c.ContactID == "" {
		c.ContactID = "contact:" + slugToken(c.ContactNickname)
	}

	if c.Channel == "" {
		switch {
		case strings.HasPrefix(strings.ToLower(c.ContactID), "tg:"), c.TGPrivateChatID != 0, len(c.TGGroupChatIDs) > 0, c.TGUsername != "":
			c.Channel = ChannelTelegram
		case strings.HasPrefix(strings.ToLower(c.ContactID), "line:"), strings.HasPrefix(strings.ToLower(c.ContactID), "line_user:"), c.LineUserID != "", len(c.LineChatIDs) > 0:
			c.Channel = ChannelLine
		case strings.HasPrefix(strings.ToLower(c.ContactID), "lark:"), strings.HasPrefix(strings.ToLower(c.ContactID), "lark_user:"), c.LarkOpenID != "", len(c.LarkChatIDs) > 0:
			c.Channel = ChannelLark
		case strings.HasPrefix(strings.ToLower(c.ContactID), "slack:"), c.SlackTeamID != "", c.SlackUserID != "", c.SlackDMChannelID != "", len(c.SlackChannelIDs) > 0:
			c.Channel = ChannelSlack
		}
	}
	if c.Channel == "" && c.Kind == KindHuman {
		c.Channel = ChannelTelegram
	}

	if strings.HasPrefix(strings.ToLower(c.ContactID), "tg:@") && c.TGUsername == "" {
		c.TGUsername = normalizeTelegramUsername(c.ContactID[len("tg:@"):])
	}
	if strings.HasPrefix(strings.ToLower(c.ContactID), "tg:") && c.TGPrivateChatID == 0 {
		id, err := parseTelegramChatID(c.ContactID[len("tg:"):])
		if err == nil && id > 0 {
			c.TGPrivateChatID = id
		}
	}
	if strings.HasPrefix(strings.ToLower(c.ContactID), "slack:") {
		teamID, userOrChannelID, ok := parseSlackContactID(c.ContactID)
		if ok {
			if c.SlackTeamID == "" {
				c.SlackTeamID = teamID
			}
			userOrChannelIDUpper := strings.ToUpper(userOrChannelID)
			switch {
			case c.SlackUserID == "" && strings.HasPrefix(userOrChannelIDUpper, "U"):
				c.SlackUserID = userOrChannelID
			case c.SlackDMChannelID == "" && strings.HasPrefix(userOrChannelIDUpper, "D"):
				c.SlackDMChannelID = userOrChannelID
			case strings.HasPrefix(userOrChannelIDUpper, "C") || strings.HasPrefix(userOrChannelIDUpper, "G"):
				c.SlackChannelIDs = normalizeStringSlice(append(c.SlackChannelIDs, userOrChannelID))
			}
		}
	}
	if strings.HasPrefix(strings.ToLower(c.ContactID), "line_user:") && c.LineUserID == "" {
		if userID, ok := refid.ParseLineUserContactID(c.ContactID); ok {
			c.LineUserID = userID
		}
	}
	if strings.HasPrefix(strings.ToLower(c.ContactID), "line:") {
		if chatID, ok := refid.ParseLineChatContactID(c.ContactID); ok {
			c.LineChatIDs = normalizeStringSlice(append(c.LineChatIDs, chatID))
			if c.LineUserID == "" && refid.LineIDLooksLikeUserID(chatID) {
				c.LineUserID = chatID
			}
		}
	}
	if strings.HasPrefix(strings.ToLower(c.ContactID), "lark_user:") && c.LarkOpenID == "" {
		if openID, ok := refid.ParseLarkUserContactID(c.ContactID); ok {
			c.LarkOpenID = openID
		}
	}
	if strings.HasPrefix(strings.ToLower(c.ContactID), "lark:") {
		if chatID, ok := refid.ParseLarkChatContactID(c.ContactID); ok {
			c.LarkChatIDs = normalizeStringSlice(append(c.LarkChatIDs, chatID))
		}
	}

	if c.TGUsername == "" && c.TGPrivateChatID == 0 && len(c.TGGroupChatIDs) == 0 && c.Channel == ChannelTelegram {
		if alias := extractTelegramAlias(c.ContactID); alias != "" {
			c.TGUsername = alias
		}
	}

	if c.ContactID == "" {
		c.ContactID = deriveContactID(c)
	}
	return c
}

func normalizeContactChannel(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case ChannelConsole, ChannelTelegram, ChannelSlack, ChannelLine, ChannelLark, "discord":
		return value
	default:
		return ""
	}
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

func normalizeInt64Slice(input []int64) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(input))
	for _, raw := range input {
		if raw == 0 || seen[raw] {
			continue
		}
		seen[raw] = true
		out = append(out, raw)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	if len(out) == 0 {
		return nil
	}
	return out
}

func busInboxRecordKey(channel string, platformMessageID string) (string, error) {
	normalizedChannel, err := normalizeBusChannel(channel)
	if err != nil {
		return "", err
	}
	normalizedMessageID := strings.TrimSpace(platformMessageID)
	if normalizedMessageID == "" {
		return "", fmt.Errorf("platform_message_id is required")
	}
	return normalizedChannel + ":" + normalizedMessageID, nil
}

func busOutboxRecordKey(channel string, idempotencyKey string) (string, error) {
	normalizedChannel, err := normalizeBusChannel(channel)
	if err != nil {
		return "", err
	}
	normalizedKey := strings.TrimSpace(idempotencyKey)
	if normalizedKey == "" {
		return "", fmt.Errorf("idempotency_key is required")
	}
	return normalizedChannel + ":" + normalizedKey, nil
}

func sortBusInboxRecords(records []BusInboxRecord) {
	sort.Slice(records, func(i, j int) bool {
		return lessBusInboxRecord(records[i], records[j])
	})
}

func lessBusInboxRecord(a BusInboxRecord, b BusInboxRecord) bool {
	if a.SeenAt.Equal(b.SeenAt) {
		if a.Channel != b.Channel {
			return a.Channel < b.Channel
		}
		return a.PlatformMessageID < b.PlatformMessageID
	}
	return a.SeenAt.After(b.SeenAt)
}

func sortBusOutboxRecords(records []BusOutboxRecord) {
	sort.Slice(records, func(i, j int) bool {
		return lessBusOutboxRecord(records[i], records[j])
	})
}

func lessBusOutboxRecord(a BusOutboxRecord, b BusOutboxRecord) bool {
	if a.UpdatedAt.Equal(b.UpdatedAt) {
		if a.Channel != b.Channel {
			return a.Channel < b.Channel
		}
		return a.IdempotencyKey < b.IdempotencyKey
	}
	return a.UpdatedAt.After(b.UpdatedAt)
}

func normalizeBusInboxRecord(record BusInboxRecord) (BusInboxRecord, error) {
	channel, err := normalizeBusChannel(record.Channel)
	if err != nil {
		return BusInboxRecord{}, err
	}
	platformMessageID := strings.TrimSpace(record.PlatformMessageID)
	if platformMessageID == "" {
		return BusInboxRecord{}, fmt.Errorf("platform_message_id is required")
	}
	seenAt := record.SeenAt.UTC()
	if seenAt.IsZero() {
		return BusInboxRecord{}, fmt.Errorf("seen_at is required")
	}
	return BusInboxRecord{
		Channel:           channel,
		PlatformMessageID: platformMessageID,
		ConversationKey:   strings.TrimSpace(record.ConversationKey),
		SeenAt:            seenAt,
	}, nil
}

func normalizeBusOutboxRecord(record BusOutboxRecord) (BusOutboxRecord, error) {
	channel, err := normalizeBusChannel(record.Channel)
	if err != nil {
		return BusOutboxRecord{}, err
	}
	idempotencyKey := strings.TrimSpace(record.IdempotencyKey)
	if idempotencyKey == "" {
		return BusOutboxRecord{}, fmt.Errorf("idempotency_key is required")
	}
	status, err := normalizeBusDeliveryStatus(record.Status)
	if err != nil {
		return BusOutboxRecord{}, err
	}
	if record.Attempts <= 0 {
		return BusOutboxRecord{}, fmt.Errorf("attempts must be > 0")
	}
	createdAt := record.CreatedAt.UTC()
	updatedAt := record.UpdatedAt.UTC()
	if createdAt.IsZero() {
		return BusOutboxRecord{}, fmt.Errorf("created_at is required")
	}
	if updatedAt.IsZero() {
		return BusOutboxRecord{}, fmt.Errorf("updated_at is required")
	}
	if updatedAt.Before(createdAt) {
		return BusOutboxRecord{}, fmt.Errorf("updated_at must be >= created_at")
	}
	normalized := BusOutboxRecord{
		Channel:        channel,
		IdempotencyKey: idempotencyKey,
		ContactID:      strings.TrimSpace(record.ContactID),
		PeerID:         strings.TrimSpace(record.PeerID),
		ItemID:         strings.TrimSpace(record.ItemID),
		ContentType:    strings.TrimSpace(record.ContentType),
		PayloadBase64:  strings.TrimSpace(record.PayloadBase64),
		Status:         status,
		Attempts:       record.Attempts,
		Accepted:       record.Accepted,
		Deduped:        record.Deduped,
		LastError:      strings.TrimSpace(record.LastError),
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
	if record.LastAttemptAt != nil {
		ts := record.LastAttemptAt.UTC()
		if ts.IsZero() {
			return BusOutboxRecord{}, fmt.Errorf("last_attempt_at must not be zero")
		}
		normalized.LastAttemptAt = &ts
	}
	if record.SentAt != nil {
		ts := record.SentAt.UTC()
		if ts.IsZero() {
			return BusOutboxRecord{}, fmt.Errorf("sent_at must not be zero")
		}
		normalized.SentAt = &ts
	}
	if normalized.Status == BusDeliveryStatusSent && normalized.SentAt == nil {
		return BusOutboxRecord{}, fmt.Errorf("sent_at is required when status=sent")
	}
	if normalized.Status != BusDeliveryStatusSent && normalized.SentAt != nil {
		return BusOutboxRecord{}, fmt.Errorf("sent_at must be empty when status is not sent")
	}
	if normalized.Status == BusDeliveryStatusFailed && normalized.LastError == "" {
		return BusOutboxRecord{}, fmt.Errorf("last_error is required when status=failed")
	}
	if normalized.Status != BusDeliveryStatusFailed && normalized.LastError != "" {
		return BusOutboxRecord{}, fmt.Errorf("last_error must be empty when status is not failed")
	}
	return normalized, nil
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
