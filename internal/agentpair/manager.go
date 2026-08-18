package agentpair

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

const DefaultTTL = 5 * time.Minute

type Status string

const (
	StatusWaiting       Status = "waiting"
	StatusCompleted     Status = "completed"
	StatusAlreadyPaired Status = "already_paired"
)

type Peer struct {
	ID      string
	Contact contacts.Contact
}

type SendFunc func(ctx context.Context, target Peer, body string) error

type Options struct {
	Context               context.Context
	Self                  Peer
	Admins                Admins
	Contacts              *contacts.Service
	JournalDir            string
	JournalRotateMaxBytes int64
	Logger                *slog.Logger
	Now                   func() time.Time
	TTL                   time.Duration
	Send                  SendFunc
}

type localIntent struct {
	PairID   string
	AdminID  string
	Target   Peer
	Expires  time.Time
	OfferRaw string
}

type remoteIntent struct {
	Offer   pairOffer
	Sender  Peer
	Expires time.Time
}

type Manager struct {
	self     Peer
	admins   Admins
	contacts *contacts.Service
	journal  *domainjournal.Journal
	logger   *slog.Logger
	now      func() time.Time
	ttl      time.Duration
	send     SendFunc

	mu     sync.Mutex
	local  map[string]localIntent
	remote map[string]remoteIntent
}

func New(opts Options) (*Manager, error) {
	self, err := normalizePeer(opts.Self, true)
	if err != nil {
		return nil, fmt.Errorf("local Agent identity: %w", err)
	}
	if opts.Contacts == nil {
		return nil, fmt.Errorf("contacts service is required")
	}
	if opts.Send == nil {
		return nil, fmt.Errorf("Agent pair sender is required")
	}
	journal, err := domainjournal.New(domainjournal.JournalOptions{
		Dir:            strings.TrimSpace(opts.JournalDir),
		RotateMaxBytes: opts.JournalRotateMaxBytes,
		SyncEachWrite:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("open contacts journal: %w", err)
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	manager := &Manager{
		self:     self,
		admins:   opts.Admins,
		contacts: opts.Contacts,
		journal:  journal,
		logger:   logger,
		now:      now,
		ttl:      ttl,
		send:     opts.Send,
		local:    make(map[string]localIntent),
		remote:   make(map[string]remoteIntent),
	}
	if opts.Context != nil {
		go manager.runExpiry(opts.Context)
	}
	return manager, nil
}

func (m *Manager) Start(ctx context.Context, adminID string, target Peer, adminReference string) (Status, error) {
	if m == nil {
		return "", fmt.Errorf("Agent pair manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := m.now().UTC()
	m.expire(now)

	normalizedAdmin, err := normalizeStableIdentity(adminID)
	if err != nil {
		m.logger.Warn("agent_pair_failed", "channel", channelForReference(m.self.ID), "admin_id", strings.TrimSpace(adminID), "reason", "unauthorized_admin")
		return "", fmt.Errorf("not authorized to pair this Agent")
	}
	authorized, err := m.adminAuthorized(ctx, normalizedAdmin, adminReference)
	if err != nil {
		m.logger.Warn("agent_pair_failed", "channel", channelForReference(m.self.ID), "admin_id", normalizedAdmin, "reason", "admin_contact_lookup_failed", "error", err.Error())
		return "", fmt.Errorf("resolve pairing administrator: %w", err)
	}
	if !authorized {
		m.logger.Warn("agent_pair_failed", "channel", channelForReference(m.self.ID), "admin_id", normalizedAdmin, "reason", "unauthorized_admin")
		return "", fmt.Errorf("not authorized to pair this Agent")
	}
	target, err = normalizePeer(target, false)
	if err != nil {
		m.logger.Warn("agent_pair_failed", "channel", channelForReference(m.self.ID), "admin_id", normalizedAdmin, "reason", "invalid_target", "error", err.Error())
		return "", err
	}
	if channelForReference(target.ID) != channelForReference(m.self.ID) {
		return "", fmt.Errorf("pair target must use the current Channel")
	}
	if peersMatch(target, m.self) {
		m.logger.Warn("agent_pair_failed", "channel", channelForReference(m.self.ID), "admin_id", normalizedAdmin, "reason", "self_pair")
		return "", fmt.Errorf("an Agent cannot pair with itself")
	}
	paired, err := m.IsPaired(ctx, target)
	if err != nil {
		return "", err
	}
	if paired {
		return StatusAlreadyPaired, nil
	}

	intent, created := m.prepareLocalIntent(normalizedAdmin, target, now)
	m.logger.Info("agent_pair_started",
		"pair_id", intent.PairID,
		"channel", channelForReference(m.self.ID),
		"admin_id", normalizedAdmin,
		"local_agent_id", m.self.ID,
		"peer_agent_id", target.ID,
	)
	if created {
		if err := m.appendJournal("agent_pair_requested", intent, target.ID, "", now); err != nil {
			m.removeLocal(intent.PairID)
			m.logger.Warn("agent_pair_failed", "pair_id", intent.PairID, "channel", channelForReference(m.self.ID), "peer_agent_id", target.ID, "reason", "journal_append_failed", "error", err.Error())
			return "", err
		}
	}
	if err := m.send(ctx, target, intent.OfferRaw); err != nil {
		m.removeLocal(intent.PairID)
		m.logger.Warn("agent_pair_failed", "pair_id", intent.PairID, "channel", channelForReference(m.self.ID), "peer_agent_id", target.ID, "reason", "offer_send_failed", "error", err.Error())
		_ = m.appendJournal("agent_pair_failed", intent, target.ID, "offer_send_failed", now)
		return "", fmt.Errorf("send Agent pair offer: %w", err)
	}
	m.logger.Info("agent_pair_offer_sent", "pair_id", intent.PairID, "channel", channelForReference(m.self.ID), "peer_agent_id", target.ID)

	completed, err := m.completeMatch(ctx, intent.PairID, now)
	if err != nil {
		return "", err
	}
	if completed {
		return StatusCompleted, nil
	}
	return StatusWaiting, nil
}

func (m *Manager) adminAuthorized(ctx context.Context, stableID, authenticatedReference string) (bool, error) {
	if m.admins.Contains(stableID) {
		return true, nil
	}
	reference, err := normalizeReference(authenticatedReference)
	if err != nil || !m.admins.Contains(reference) {
		return false, nil
	}

	items, err := m.contacts.ListContacts(ctx, "")
	if err != nil {
		return false, err
	}
	for _, contact := range items {
		if peerHasReference(Peer{Contact: contact}, reference) {
			return true, nil
		}
	}
	return false, nil
}

func (m *Manager) Handle(ctx context.Context, sender Peer, text string) (Status, bool, error) {
	if m == nil {
		return "", false, fmt.Errorf("Agent pair manager is nil")
	}
	offer, expiresAt, handled, err := decodeOffer(text)
	if !handled || err != nil {
		return "", handled, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := m.now().UTC()
	m.expire(now)
	sender, err = normalizePeer(sender, true)
	if err != nil {
		return "", true, fmt.Errorf("authenticated Agent sender: %w", err)
	}
	if offer.From != sender.ID {
		return "", true, fmt.Errorf("Agent pair sender does not match platform identity")
	}
	if !peerHasReference(m.self, offer.To) {
		return "", true, fmt.Errorf("Agent pair offer targets another Agent")
	}
	if !expiresAt.After(now) {
		m.logger.Info("agent_pair_expired", "pair_id", offer.PairID, "channel", channelForReference(m.self.ID), "peer_agent_id", sender.ID, "direction", "inbound")
		return StatusWaiting, true, nil
	}
	m.logger.Info("agent_pair_offer_received", "pair_id", offer.PairID, "channel", channelForReference(m.self.ID), "peer_agent_id", sender.ID)

	paired, err := m.IsPaired(ctx, sender)
	if err != nil {
		return "", true, err
	}
	if paired {
		return StatusAlreadyPaired, true, nil
	}
	m.mu.Lock()
	m.remote[sender.ID] = remoteIntent{Offer: offer, Sender: sender, Expires: expiresAt}
	m.mu.Unlock()

	localPairID := m.matchingLocalPairID(sender)
	if localPairID == "" {
		return StatusWaiting, true, nil
	}
	completed, err := m.completeMatch(ctx, localPairID, now)
	if err != nil {
		return "", true, err
	}
	if completed {
		return StatusCompleted, true, nil
	}
	return StatusWaiting, true, nil
}

func (m *Manager) IsPaired(ctx context.Context, peer Peer) (bool, error) {
	if m == nil || m.contacts == nil {
		return false, fmt.Errorf("Agent pair manager is unavailable")
	}
	peer, err := normalizePeer(peer, false)
	if err != nil {
		return false, err
	}
	m.expire(m.now().UTC())
	items, err := m.contacts.ListContacts(ctx, contacts.StatusActive)
	if err != nil {
		return false, err
	}
	for _, contact := range items {
		if contact.Kind != contacts.KindAgent || !contact.Paired {
			continue
		}
		if peersMatch(peer, Peer{ID: contact.ContactID, Contact: contact}) {
			return true, nil
		}
	}
	return false, nil
}

func (m *Manager) prepareLocalIntent(adminID string, target Peer, now time.Time) (localIntent, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, intent := range m.local {
		if intent.Expires.After(now) && peersMatch(intent.Target, target) {
			return intent, false
		}
	}
	intent := localIntent{
		PairID:  uuid.NewString(),
		AdminID: adminID,
		Target:  target,
		Expires: now.Add(m.ttl),
	}
	intent.OfferRaw = encodeOffer(pairOffer{
		PairID:    intent.PairID,
		From:      m.self.ID,
		To:        target.ID,
		ExpiresAt: intent.Expires.Format(time.RFC3339Nano),
	})
	m.local[intent.PairID] = intent
	return intent, true
}

func (m *Manager) completeMatch(ctx context.Context, localPairID string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	intent, ok := m.local[localPairID]
	if !ok || !intent.Expires.After(now) {
		return false, nil
	}
	var remoteKey string
	var remote remoteIntent
	for key, candidate := range m.remote {
		if candidate.Expires.After(now) && peersMatch(intent.Target, candidate.Sender) {
			remoteKey = key
			remote = candidate
			break
		}
	}
	if remoteKey == "" {
		return false, nil
	}
	m.logger.Info("agent_pair_matched", "pair_id", intent.PairID, "remote_pair_id", remote.Offer.PairID, "channel", channelForReference(m.self.ID), "peer_agent_id", remote.Sender.ID)
	contact := remote.Sender.Contact
	pairedContact, err := m.contacts.PairAgent(ctx, contact, now)
	if err != nil {
		delete(m.local, localPairID)
		delete(m.remote, remoteKey)
		m.logger.Warn("agent_pair_failed", "pair_id", intent.PairID, "channel", channelForReference(m.self.ID), "peer_agent_id", remote.Sender.ID, "reason", "contact_update_failed", "error", err.Error())
		_ = m.appendJournal("agent_pair_failed", intent, remote.Sender.ID, "contact_update_failed", now)
		return false, err
	}
	delete(m.local, localPairID)
	delete(m.remote, remoteKey)
	if err := m.appendJournal("agent_pair_completed", intent, remote.Sender.ID, "", now); err != nil {
		m.logger.Error("agent_pair_journal_error", "pair_id", intent.PairID, "event_type", "agent_pair_completed", "peer_agent_id", remote.Sender.ID, "error", err.Error())
	}
	m.logger.Info("agent_pair_completed", "pair_id", intent.PairID, "remote_pair_id", remote.Offer.PairID, "channel", channelForReference(m.self.ID), "peer_agent_id", remote.Sender.ID, "contact_id", pairedContact.ContactID)
	return true, nil
}

func (m *Manager) expire(now time.Time) {
	m.mu.Lock()
	expired := make([]localIntent, 0)
	for pairID, intent := range m.local {
		if !intent.Expires.After(now) {
			expired = append(expired, intent)
			delete(m.local, pairID)
		}
	}
	for key, intent := range m.remote {
		if !intent.Expires.After(now) {
			delete(m.remote, key)
		}
	}
	m.mu.Unlock()
	for _, intent := range expired {
		m.logger.Info("agent_pair_expired", "pair_id", intent.PairID, "channel", channelForReference(m.self.ID), "admin_id", intent.AdminID, "peer_agent_id", intent.Target.ID, "direction", "outbound")
		if err := m.appendJournal("agent_pair_expired", intent, intent.Target.ID, "", now); err != nil {
			m.logger.Error("agent_pair_journal_error", "pair_id", intent.PairID, "event_type", "agent_pair_expired", "error", err.Error())
		}
	}
}

func (m *Manager) runExpiry(ctx context.Context) {
	interval := m.ttl / 2
	if interval > time.Minute {
		interval = time.Minute
	}
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.expire(m.now().UTC())
		}
	}
}

func (m *Manager) appendJournal(eventType string, intent localIntent, peerID, reason string, now time.Time) error {
	fields := map[string]any{
		"pair_id":        intent.PairID,
		"admin_id":       intent.AdminID,
		"local_agent_id": m.self.ID,
		"peer_agent_id":  strings.TrimSpace(peerID),
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		fields["reason"] = reason
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	_, err = m.journal.Append(domainjournal.Event{
		ID:            uuid.NewString(),
		Time:          now.UTC().Format(time.RFC3339Nano),
		Domain:        "contacts",
		Type:          eventType,
		SchemaVersion: 1,
		Trace:         domainjournal.Trace{Runtime: channelForReference(m.self.ID), Target: peerID},
		Payload:       payload,
	})
	if err != nil {
		return fmt.Errorf("append contacts journal: %w", err)
	}
	return nil
}

func (m *Manager) removeLocal(pairID string) {
	m.mu.Lock()
	delete(m.local, strings.TrimSpace(pairID))
	m.mu.Unlock()
}

func (m *Manager) matchingLocalPairID(sender Peer) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for pairID, intent := range m.local {
		if peersMatch(intent.Target, sender) {
			return pairID
		}
	}
	return ""
}

func normalizePeer(peer Peer, stableID bool) (Peer, error) {
	var err error
	if stableID {
		peer.ID, err = normalizeStableIdentity(peer.ID)
	} else {
		peer.ID, err = normalizeReference(peer.ID)
	}
	if err != nil {
		return Peer{}, err
	}
	peer.Contact.Kind = contacts.KindAgent
	return peer, nil
}

func normalizeReference(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "tg:@") {
		username := strings.TrimSpace(value[len("tg:@"):])
		if username == "" || strings.ContainsAny(username, " \t\r\n:@()") {
			return "", fmt.Errorf("invalid Telegram username reference")
		}
		return "tg:@" + username, nil
	}
	return normalizeStableIdentity(value)
}

func peersMatch(a, b Peer) bool {
	aKeys := peerKeys(a)
	for key := range peerKeys(b) {
		if aKeys[key] {
			return true
		}
	}
	return false
}

func peerHasReference(peer Peer, raw string) bool {
	ref, err := normalizeReference(raw)
	if err != nil {
		return false
	}
	return peerKeys(peer)[referenceKey(ref)]
}

func peerKeys(peer Peer) map[string]bool {
	refs := []string{peer.ID}
	refs = append(refs, contactReferences(peer.Contact)...)
	out := make(map[string]bool, len(refs))
	for _, raw := range refs {
		ref, err := normalizeReference(raw)
		if err == nil {
			out[referenceKey(ref)] = true
		}
	}
	return out
}

func contactReferences(contact contacts.Contact) []string {
	refs := make([]string, 0, 6)
	if id := strings.TrimSpace(contact.ContactID); id != "" {
		refs = append(refs, id)
	}
	if contact.TGPrivateChatID > 0 {
		refs = append(refs, "tg:"+strconv.FormatInt(contact.TGPrivateChatID, 10))
	}
	if username := strings.TrimSpace(strings.TrimPrefix(contact.TGUsername, "@")); username != "" {
		refs = append(refs, "tg:@"+username)
	}
	if teamID, userID := strings.TrimSpace(contact.SlackTeamID), strings.TrimSpace(contact.SlackUserID); teamID != "" && userID != "" {
		refs = append(refs, "slack:"+teamID+":"+userID)
	}
	if userID := strings.TrimSpace(contact.LineUserID); userID != "" {
		refs = append(refs, "line_user:"+userID)
	}
	if openID := strings.TrimSpace(contact.LarkOpenID); openID != "" {
		refs = append(refs, "lark_user:"+openID)
	}
	return refs
}

func referenceKey(ref string) string {
	return strings.ToLower(ref)
}

func channelForReference(ref string) string {
	lower := strings.ToLower(strings.TrimSpace(ref))
	switch {
	case strings.HasPrefix(lower, "tg:"):
		return contacts.ChannelTelegram
	case strings.HasPrefix(lower, "slack:"):
		return contacts.ChannelSlack
	case strings.HasPrefix(lower, "line_user:"):
		return contacts.ChannelLine
	case strings.HasPrefix(lower, "lark_user:"):
		return contacts.ChannelLark
	default:
		return ""
	}
}
