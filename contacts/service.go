package contacts

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	defaultFailureCooldown            = 72 * time.Hour
	defaultAutoNicknameDepthThreshold = 45.0
	alphaTopicWeight                  = 0.20
	auditActionNicknameAutoAssigned   = "contact_nickname.auto_assigned"
	auditActionNicknameAutoFailed     = "contact_nickname.auto_assign_failed"
)

type Sender interface {
	Send(ctx context.Context, contact Contact, decision ShareDecision) (accepted bool, deduped bool, err error)
}

type CandidateFeature struct {
	HasOverlapSemantic   bool
	OverlapSemantic      float64
	ExplicitHistoryLinks []string
	Confidence           float64
}

type FeatureExtractor interface {
	EvaluateCandidateFeatures(ctx context.Context, contact Contact, candidates []ShareCandidate) (map[string]CandidateFeature, error)
}

type PreferenceFeatures struct {
	TopicAffinity map[string]float64
	PersonaBrief  string
	PersonaTraits map[string]float64
	Confidence    float64
}

type PreferenceExtractor interface {
	EvaluateContactPreferences(ctx context.Context, contact Contact, candidates []ShareCandidate) (PreferenceFeatures, error)
}

type NicknameGenerator interface {
	SuggestNickname(ctx context.Context, contact Contact) (nickname string, confidence float64, err error)
}

type ServiceOptions struct {
	FailureCooldown time.Duration
}

type Service struct {
	store           Store
	failureCooldown time.Duration
}

func NewService(store Store) *Service {
	return NewServiceWithOptions(store, ServiceOptions{})
}

func NewServiceWithOptions(store Store, opts ServiceOptions) *Service {
	opts = normalizeServiceOptions(opts)
	return &Service{
		store:           store,
		failureCooldown: opts.FailureCooldown,
	}
}

func normalizeServiceOptions(opts ServiceOptions) ServiceOptions {
	if opts.FailureCooldown <= 0 {
		opts.FailureCooldown = defaultFailureCooldown
	}
	return opts
}

func (s *Service) UpsertContact(ctx context.Context, contact Contact, now time.Time) (Contact, error) {
	if s == nil || s.store == nil {
		return Contact{}, fmt.Errorf("nil contacts service")
	}
	now = normalizeNow(now)
	if err := s.store.Ensure(ctx); err != nil {
		return Contact{}, err
	}
	contact = normalizeContact(contact, now)
	if strings.TrimSpace(contact.ContactID) == "" {
		contact.ContactID = deriveContactID(contact)
	}
	if strings.TrimSpace(contact.ContactID) == "" {
		return Contact{}, fmt.Errorf("contact_id is required")
	}

	existing, ok, err := s.store.GetContact(ctx, contact.ContactID)
	if err != nil {
		return Contact{}, err
	}
	if ok && !existing.CreatedAt.IsZero() {
		contact.CreatedAt = existing.CreatedAt
	}
	if ok && strings.TrimSpace(contact.ContactNickname) == "" && strings.TrimSpace(existing.ContactNickname) != "" {
		contact.ContactNickname = strings.TrimSpace(existing.ContactNickname)
	}
	if ok && strings.TrimSpace(contact.PersonaBrief) == "" && strings.TrimSpace(existing.PersonaBrief) != "" {
		contact.PersonaBrief = strings.TrimSpace(existing.PersonaBrief)
	}
	if ok && len(contact.PersonaTraits) == 0 && len(existing.PersonaTraits) > 0 {
		contact.PersonaTraits = map[string]float64{}
		for k, v := range existing.PersonaTraits {
			contact.PersonaTraits[k] = v
		}
	}
	if err := s.store.PutContact(ctx, contact); err != nil {
		return Contact{}, err
	}
	return contact, nil
}

func (s *Service) ListContacts(ctx context.Context, status Status) ([]Contact, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil contacts service")
	}
	return s.store.ListContacts(ctx, status)
}

func (s *Service) GetContact(ctx context.Context, contactID string) (Contact, bool, error) {
	if s == nil || s.store == nil {
		return Contact{}, false, fmt.Errorf("nil contacts service")
	}
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return Contact{}, false, fmt.Errorf("contact_id is required")
	}
	return s.store.GetContact(ctx, contactID)
}

func (s *Service) SetContactStatus(ctx context.Context, contactID string, status Status) (Contact, error) {
	if s == nil || s.store == nil {
		return Contact{}, fmt.Errorf("nil contacts service")
	}
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return Contact{}, fmt.Errorf("contact_id is required")
	}
	status = normalizeStatus(status)
	if err := s.store.Ensure(ctx); err != nil {
		return Contact{}, err
	}

	contact, ok, err := s.store.GetContact(ctx, contactID)
	if err != nil {
		return Contact{}, err
	}
	if !ok {
		return Contact{}, fmt.Errorf("contact not found: %s", contactID)
	}
	contact.Status = status
	if err := s.store.PutContact(ctx, contact); err != nil {
		return Contact{}, err
	}
	return contact, nil
}

func (s *Service) AddCandidate(ctx context.Context, candidate ShareCandidate, now time.Time) (ShareCandidate, error) {
	if s == nil || s.store == nil {
		return ShareCandidate{}, fmt.Errorf("nil contacts service")
	}
	now = normalizeNow(now)
	if err := s.store.Ensure(ctx); err != nil {
		return ShareCandidate{}, err
	}
	candidate = normalizeCandidate(candidate, now)
	if strings.TrimSpace(candidate.ItemID) == "" {
		candidate.ItemID = "cand_" + uuid.NewString()
	}
	if strings.TrimSpace(candidate.Topic) == "" && len(candidate.Topics) > 0 {
		candidate.Topic = strings.TrimSpace(candidate.Topics[0])
	}
	if strings.TrimSpace(candidate.Topic) == "" {
		return ShareCandidate{}, fmt.Errorf("candidate topic is required")
	}
	if strings.TrimSpace(candidate.ContentType) == "" {
		candidate.ContentType = "text/plain"
	}
	if strings.TrimSpace(candidate.PayloadBase64) == "" {
		return ShareCandidate{}, fmt.Errorf("candidate payload_base64 is required")
	}
	if _, err := base64.RawURLEncoding.DecodeString(candidate.PayloadBase64); err != nil {
		return ShareCandidate{}, fmt.Errorf("payload_base64 decode failed: %w", err)
	}
	if err := s.store.PutCandidate(ctx, candidate); err != nil {
		return ShareCandidate{}, err
	}
	return candidate, nil
}

func (s *Service) ListCandidates(ctx context.Context) ([]ShareCandidate, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil contacts service")
	}
	return s.store.ListCandidates(ctx)
}

func (s *Service) ListAuditEvents(ctx context.Context, tickID string, contactID string, action string, limit int) ([]AuditEvent, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil contacts service")
	}
	return s.store.ListAuditEvents(ctx, tickID, contactID, action, limit)
}

// RankCandidates computes proactive share decisions without sending or mutating contact/session state.
func (s *Service) RankCandidates(ctx context.Context, now time.Time, opts TickOptions) ([]ShareDecision, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil contacts service")
	}
	now = normalizeNow(now)
	if err := s.store.Ensure(ctx); err != nil {
		return nil, err
	}
	opts = normalizeTickOptions(opts)
	opts.Send = false

	contactsList, err := s.store.ListContacts(ctx, StatusActive)
	if err != nil {
		return nil, err
	}
	candidates, err := s.store.ListCandidates(ctx)
	if err != nil {
		return nil, err
	}

	eligible := make([]Contact, 0, len(contactsList))
	for _, contact := range contactsList {
		if !contactAllowedForProactive(contact, now, opts) {
			continue
		}
		eligible = append(eligible, contact)
	}
	freshCandidates := filterFreshCandidates(candidates, now, opts.FreshnessWindow)
	featuresByContact := map[string]map[string]CandidateFeature{}
	if opts.FeatureExtractor != nil && len(freshCandidates) > 0 {
		for _, contact := range eligible {
			features, err := opts.FeatureExtractor.EvaluateCandidateFeatures(ctx, contact, freshCandidates)
			if err != nil {
				continue
			}
			featuresByContact[contact.ContactID] = features
		}
	}

	decisions := buildDecisions("rank_only", eligible, freshCandidates, featuresByContact, opts.PushTopic, opts.MaxLinkedHistoryItems, opts)
	if len(decisions) > opts.MaxTargets {
		decisions = decisions[:opts.MaxTargets]
	}
	return decisions, nil
}

// SendDecision sends one prepared decision through sender and persists outcome effects
// (cooldown/last_shared/share_count/retain_score + audit).
func (s *Service) SendDecision(ctx context.Context, now time.Time, decision ShareDecision, sender Sender) (ShareOutcome, error) {
	if s == nil || s.store == nil {
		return ShareOutcome{}, fmt.Errorf("nil contacts service")
	}
	if sender == nil {
		return ShareOutcome{}, fmt.Errorf("sender is required")
	}
	now = normalizeNow(now)
	if err := s.store.Ensure(ctx); err != nil {
		return ShareOutcome{}, err
	}

	decision.ContactID = strings.TrimSpace(decision.ContactID)
	if decision.ContactID == "" {
		return ShareOutcome{}, fmt.Errorf("contact_id is required")
	}
	contact, ok, err := s.store.GetContact(ctx, decision.ContactID)
	if err != nil {
		return ShareOutcome{}, err
	}
	if !ok {
		return ShareOutcome{}, fmt.Errorf("contact not found: %s", decision.ContactID)
	}
	if ok, reason := contactAllowedForProactiveReason(contact, now, TickOptions{
		Send:                  true,
		EnableHumanContacts:   true,
		EnableHumanSend:       true,
		EnableHumanPublicSend: true,
	}); !ok {
		return ShareOutcome{}, fmt.Errorf("contact is not eligible for send: %s (reason=%s)", decision.ContactID, reason)
	}

	if strings.TrimSpace(decision.PeerID) == "" {
		decision.PeerID = strings.TrimSpace(contact.PeerID)
	}
	decision.Topic = strings.TrimSpace(decision.Topic)
	if decision.Topic == "" {
		decision.Topic = "share.proactive.v1"
	}
	decision.ContentType = strings.TrimSpace(decision.ContentType)
	if decision.ContentType == "" {
		decision.ContentType = "text/plain"
	}
	decision.PayloadBase64 = strings.TrimSpace(decision.PayloadBase64)
	if decision.PayloadBase64 == "" {
		return ShareOutcome{}, fmt.Errorf("payload_base64 is required")
	}
	if _, err := base64.RawURLEncoding.DecodeString(decision.PayloadBase64); err != nil {
		return ShareOutcome{}, fmt.Errorf("payload_base64 decode failed: %w", err)
	}
	decision.ItemID = strings.TrimSpace(decision.ItemID)
	if decision.ItemID == "" {
		decision.ItemID = "manual_" + uuid.NewString()
	}
	decision.IdempotencyKey = strings.TrimSpace(decision.IdempotencyKey)
	if decision.IdempotencyKey == "" {
		decision.IdempotencyKey = fmt.Sprintf("manual:%s:%s", sanitizeToken(contact.ContactID), uuid.NewString())
	}

	outcome := ShareOutcome{
		ContactID:      decision.ContactID,
		PeerID:         decision.PeerID,
		ItemID:         decision.ItemID,
		IdempotencyKey: decision.IdempotencyKey,
		SentAt:         now,
	}
	accepted, deduped, sendErr := sender.Send(ctx, contact, decision)
	if sendErr != nil {
		outcome.Error = sendErr.Error()
		cooldown := now.Add(s.failureCooldown)
		contact.CooldownUntil = &cooldown
	} else {
		outcome.Accepted = accepted
		outcome.Deduped = deduped
		ts := now
		contact.LastSharedAt = &ts
		contact.LastInteractionAt = &ts
		contact.LastSharedItemID = decision.ItemID
		contact.ShareCount++
	}
	contact = recomputeRetainScore(contact, now)
	if err := s.store.PutContact(ctx, contact); err != nil {
		return ShareOutcome{}, err
	}
	action := "proactive_share_send_ok"
	reason := "accepted"
	if outcome.Error != "" {
		action = "proactive_share_send_failed"
		reason = outcome.Error
	}
	_ = s.store.AppendAuditEvent(ctx, AuditEvent{
		EventID:   "evt_" + uuid.NewString(),
		Action:    action,
		ContactID: decision.ContactID,
		PeerID:    decision.PeerID,
		ItemID:    decision.ItemID,
		Reason:    reason,
		CreatedAt: now,
	})
	return outcome, nil
}

func (s *Service) UpdateFeedback(ctx context.Context, now time.Time, input FeedbackUpdateInput) (Contact, SessionState, error) {
	if s == nil || s.store == nil {
		return Contact{}, SessionState{}, fmt.Errorf("nil contacts service")
	}
	now = normalizeNow(now)
	if err := s.store.Ensure(ctx); err != nil {
		return Contact{}, SessionState{}, err
	}

	input.ContactID = strings.TrimSpace(input.ContactID)
	if input.ContactID == "" {
		return Contact{}, SessionState{}, fmt.Errorf("contact_id is required")
	}
	signal := normalizeFeedbackSignal(input.Signal)
	if signal == "" {
		return Contact{}, SessionState{}, fmt.Errorf("invalid feedback signal: %s", input.Signal)
	}
	input.Topic = strings.ToLower(strings.TrimSpace(input.Topic))
	input.Reason = strings.TrimSpace(input.Reason)

	contact, ok, err := s.store.GetContact(ctx, input.ContactID)
	if err != nil {
		return Contact{}, SessionState{}, err
	}
	if !ok {
		return Contact{}, SessionState{}, fmt.Errorf("contact not found: %s", input.ContactID)
	}

	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		sessionID = input.ContactID
	}
	session, ok, err := s.store.GetSessionState(ctx, sessionID)
	if err != nil {
		return Contact{}, SessionState{}, err
	}
	if !ok {
		session = SessionState{
			SessionID:            sessionID,
			ContactID:            input.ContactID,
			SessionInterestLevel: 0.60,
			TurnCount:            0,
			StartedAt:            now,
		}
	}
	if strings.TrimSpace(session.ContactID) == "" {
		session.ContactID = input.ContactID
	}

	sessionDelta := 0.0
	reciprocityDelta := 0.0
	depthDelta := 0.0
	topicDelta := 0.0
	switch signal {
	case FeedbackPositive:
		sessionDelta = 0.15
		reciprocityDelta = 0.08
		depthDelta = 1.50
		topicDelta = 0.08
	case FeedbackNegative:
		sessionDelta = -0.20
		reciprocityDelta = -0.10
		depthDelta = -0.80
		topicDelta = -0.10
	default:
		sessionDelta = -0.02
		reciprocityDelta = 0
		depthDelta = 0.20
		topicDelta = 0.01
	}

	session.SessionInterestLevel = clamp(session.SessionInterestLevel+sessionDelta, 0, 1)
	session.TurnCount++
	if input.EndSession {
		ts := now
		session.EndedAt = &ts
	}
	session.UpdatedAt = now

	contact.ReciprocityNorm = clamp(contact.ReciprocityNorm+reciprocityDelta, 0, 1)
	contact.UnderstandingDepth = clamp(contact.UnderstandingDepth+depthDelta, 0, 100)
	if input.Topic != "" {
		if contact.TopicWeights == nil {
			contact.TopicWeights = map[string]float64{}
		}
		contact.TopicWeights[input.Topic] = clamp(contact.TopicWeights[input.Topic]+topicDelta, 0, 1)
		contact.TopicWeights = normalizeTopicWeightsMap(contact.TopicWeights)
	}
	cooldownHours := 0
	if signal == FeedbackNegative {
		if duration, ok := negativeCooldownDuration(session.SessionInterestLevel); ok {
			cooldown := now.Add(duration)
			contact.CooldownUntil = &cooldown
			cooldownHours = int(duration.Hours())
		}
	}
	ts := now
	contact.LastInteractionAt = &ts
	contact = recomputeRetainScore(contact, now)

	if err := s.store.PutSessionState(ctx, session); err != nil {
		return Contact{}, SessionState{}, err
	}
	if err := s.store.PutContact(ctx, contact); err != nil {
		return Contact{}, SessionState{}, err
	}

	metadata := map[string]string{
		"signal":                 string(signal),
		"session_id":             session.SessionID,
		"session_interest_level": fmt.Sprintf("%.3f", session.SessionInterestLevel),
		"reciprocity_norm":       fmt.Sprintf("%.3f", contact.ReciprocityNorm),
		"understanding_depth":    fmt.Sprintf("%.2f", contact.UnderstandingDepth),
	}
	if cooldownHours > 0 {
		metadata["cooldown_hours"] = fmt.Sprintf("%d", cooldownHours)
	}
	if input.Topic != "" {
		metadata["topic"] = input.Topic
		metadata["topic_weight"] = fmt.Sprintf("%.3f", contact.TopicWeights[input.Topic])
	}
	_ = s.store.AppendAuditEvent(ctx, AuditEvent{
		EventID:   "evt_" + uuid.NewString(),
		Action:    "contact_feedback_update",
		ContactID: input.ContactID,
		PeerID:    contact.PeerID,
		Reason:    input.Reason,
		Metadata:  metadata,
		CreatedAt: now,
	})
	return contact, session, nil
}

// RefreshContactPreferences applies preference extraction to one contact using
// provided candidates. It does not send data and is intended for post-session
// profile refresh.
func (s *Service) RefreshContactPreferences(
	ctx context.Context,
	now time.Time,
	contactID string,
	candidates []ShareCandidate,
	extractor PreferenceExtractor,
	reason string,
) (Contact, bool, error) {
	if s == nil || s.store == nil {
		return Contact{}, false, fmt.Errorf("nil contacts service")
	}
	if extractor == nil {
		return Contact{}, false, fmt.Errorf("preference extractor is required")
	}
	now = normalizeNow(now)
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return Contact{}, false, fmt.Errorf("contact_id is required")
	}
	if err := s.store.Ensure(ctx); err != nil {
		return Contact{}, false, err
	}
	contact, ok, err := s.store.GetContact(ctx, contactID)
	if err != nil {
		return Contact{}, false, err
	}
	if !ok {
		return Contact{}, false, fmt.Errorf("contact not found: %s", contactID)
	}

	normalized := make([]ShareCandidate, 0, len(candidates))
	for i := range candidates {
		item := normalizeCandidate(candidates[i], now)
		if strings.TrimSpace(item.ItemID) == "" {
			item.ItemID = "pref_" + uuid.NewString()
		}
		if strings.TrimSpace(item.Topic) == "" {
			item.Topic = "dialogue.session"
		}
		if strings.TrimSpace(item.ContentType) == "" {
			item.ContentType = "application/json"
		}
		if strings.TrimSpace(item.PayloadBase64) == "" {
			continue
		}
		normalized = append(normalized, item)
	}
	if len(normalized) == 0 {
		return contact, false, nil
	}

	before := contact
	tickID := "pref_" + uuid.NewString()
	updated, err := s.applyContactPreferences(ctx, []Contact{contact}, normalized, now, tickID, extractor)
	if err != nil {
		return Contact{}, false, err
	}
	if len(updated) == 0 {
		return contact, false, nil
	}
	after := updated[0]
	changed := contactPreferenceChanged(before, after)
	if !changed {
		_ = s.store.AppendAuditEvent(ctx, AuditEvent{
			EventID:   "evt_" + uuid.NewString(),
			TickID:    tickID,
			Action:    "contact_preference_extract_no_change",
			ContactID: after.ContactID,
			PeerID:    after.PeerID,
			Reason:    strings.TrimSpace(reason),
			Metadata: map[string]string{
				"candidate_count": fmt.Sprintf("%d", len(normalized)),
			},
			CreatedAt: now,
		})
		return after, false, nil
	}
	return after, true, nil
}

func (s *Service) RunTick(ctx context.Context, now time.Time, opts TickOptions, sender Sender) (TickResult, error) {
	if s == nil || s.store == nil {
		return TickResult{}, fmt.Errorf("nil contacts service")
	}
	now = normalizeNow(now)
	if err := s.store.Ensure(ctx); err != nil {
		return TickResult{}, err
	}
	opts = normalizeTickOptions(opts)
	if opts.Send && sender == nil {
		return TickResult{}, fmt.Errorf("sender is required when send=true")
	}

	tickID := "tick_" + uuid.NewString()
	result := TickResult{
		TickID:    tickID,
		StartedAt: now,
	}
	_ = s.store.AppendAuditEvent(ctx, AuditEvent{
		EventID:   "evt_" + uuid.NewString(),
		TickID:    tickID,
		Action:    "proactive_share_tick_start",
		CreatedAt: now,
	})

	contacts, err := s.store.ListContacts(ctx, StatusActive)
	if err != nil {
		return TickResult{}, err
	}
	if opts.NicknameGenerator != nil {
		contacts, err = s.assignNicknames(ctx, contacts, now, tickID, opts.NicknameGenerator, opts.NicknameMinConfidence)
		if err != nil {
			return TickResult{}, err
		}
	}
	candidates, err := s.store.ListCandidates(ctx)
	if err != nil {
		return TickResult{}, err
	}

	eligible := make([]Contact, 0, len(contacts))
	contactByID := make(map[string]Contact, len(contacts))
	for _, contact := range contacts {
		if !contactAllowedForProactive(contact, now, opts) {
			continue
		}
		eligible = append(eligible, contact)
		contactByID[contact.ContactID] = contact
	}
	freshCandidates := filterFreshCandidates(candidates, now, opts.FreshnessWindow)
	if opts.PreferenceExtractor != nil && len(freshCandidates) > 0 {
		updatedEligible, updateErr := s.applyContactPreferences(ctx, eligible, freshCandidates, now, tickID, opts.PreferenceExtractor)
		if updateErr != nil {
			return TickResult{}, updateErr
		}
		eligible = updatedEligible
		for _, updated := range eligible {
			contactByID[updated.ContactID] = updated
		}
	}

	featuresByContact := map[string]map[string]CandidateFeature{}
	if opts.FeatureExtractor != nil && len(freshCandidates) > 0 {
		for _, contact := range eligible {
			features, featureErr := opts.FeatureExtractor.EvaluateCandidateFeatures(ctx, contact, freshCandidates)
			if featureErr != nil {
				_ = s.store.AppendAuditEvent(ctx, AuditEvent{
					EventID:   "evt_" + uuid.NewString(),
					TickID:    tickID,
					Action:    "proactive_share_llm_features_failed",
					ContactID: contact.ContactID,
					PeerID:    contact.PeerID,
					Reason:    featureErr.Error(),
					CreatedAt: now,
				})
				continue
			}
			featuresByContact[contact.ContactID] = features
			_ = s.store.AppendAuditEvent(ctx, AuditEvent{
				EventID:   "evt_" + uuid.NewString(),
				TickID:    tickID,
				Action:    "proactive_share_llm_features_ok",
				ContactID: contact.ContactID,
				PeerID:    contact.PeerID,
				Metadata:  map[string]string{"feature_count": fmt.Sprintf("%d", len(features))},
				CreatedAt: now,
			})
		}
	}

	decisions := buildDecisions(tickID, eligible, freshCandidates, featuresByContact, opts.PushTopic, opts.MaxLinkedHistoryItems, opts)
	if len(decisions) > opts.MaxTargets {
		decisions = decisions[:opts.MaxTargets]
	}
	result.Decisions = decisions
	result.Planned = len(decisions)

	for _, decision := range decisions {
		_ = s.store.AppendAuditEvent(ctx, AuditEvent{
			EventID:        "evt_" + uuid.NewString(),
			TickID:         tickID,
			Action:         "proactive_share_decision",
			ContactID:      decision.ContactID,
			PeerID:         decision.PeerID,
			ItemID:         decision.ItemID,
			Score:          decision.Score,
			ScoreBreakdown: decision.ScoreBreakdown,
			Reason:         "selected_by_rule_score",
			CreatedAt:      now,
		})
	}

	if opts.Send {
		for _, decision := range decisions {
			contact := contactByID[decision.ContactID]
			outcome := ShareOutcome{
				ContactID:      decision.ContactID,
				PeerID:         decision.PeerID,
				ItemID:         decision.ItemID,
				IdempotencyKey: decision.IdempotencyKey,
				SentAt:         now,
			}
			accepted, deduped, sendErr := sender.Send(ctx, contact, decision)
			if sendErr != nil {
				outcome.Error = sendErr.Error()
				cooldown := now.Add(s.failureCooldown)
				contact.CooldownUntil = &cooldown
			} else {
				outcome.Accepted = accepted
				outcome.Deduped = deduped
				ts := now
				contact.LastSharedAt = &ts
				contact.LastInteractionAt = &ts
				contact.LastSharedItemID = decision.ItemID
				contact.ShareCount++
				result.Sent++
			}
			contact = recomputeRetainScore(contact, now)
			if err := s.store.PutContact(ctx, contact); err != nil {
				return TickResult{}, err
			}
			result.Outcomes = append(result.Outcomes, outcome)
			action := "proactive_share_send_ok"
			reason := "accepted"
			if outcome.Error != "" {
				action = "proactive_share_send_failed"
				reason = outcome.Error
			}
			_ = s.store.AppendAuditEvent(ctx, AuditEvent{
				EventID:   "evt_" + uuid.NewString(),
				TickID:    tickID,
				Action:    action,
				ContactID: decision.ContactID,
				PeerID:    decision.PeerID,
				ItemID:    decision.ItemID,
				Reason:    reason,
				CreatedAt: now,
			})
		}
	}

	if err := s.rebalanceActiveContacts(ctx, now); err != nil {
		return TickResult{}, err
	}

	result.EndedAt = normalizeNow(time.Now().UTC())
	_ = s.store.AppendAuditEvent(ctx, AuditEvent{
		EventID:   "evt_" + uuid.NewString(),
		TickID:    tickID,
		Action:    "proactive_share_tick_end",
		Metadata:  map[string]string{"planned": fmt.Sprintf("%d", result.Planned), "sent": fmt.Sprintf("%d", result.Sent)},
		CreatedAt: result.EndedAt,
	})
	return result, nil
}

func (s *Service) rebalanceActiveContacts(ctx context.Context, now time.Time) error {
	contacts, err := s.store.ListContacts(ctx, "")
	if err != nil {
		return err
	}
	if len(contacts) <= DefaultMaxActiveContacts {
		return nil
	}

	type ranked struct {
		Contact Contact
		Score   float64
	}
	rankedContacts := make([]ranked, 0, len(contacts))
	for _, contact := range contacts {
		updated := recomputeRetainScore(contact, now)
		rankedContacts = append(rankedContacts, ranked{Contact: updated, Score: updated.RetainScore})
	}
	sort.Slice(rankedContacts, func(i, j int) bool {
		if rankedContacts[i].Score == rankedContacts[j].Score {
			return strings.TrimSpace(rankedContacts[i].Contact.ContactID) < strings.TrimSpace(rankedContacts[j].Contact.ContactID)
		}
		return rankedContacts[i].Score > rankedContacts[j].Score
	})

	for i := range rankedContacts {
		updated := rankedContacts[i].Contact
		if i < DefaultMaxActiveContacts {
			updated.Status = StatusActive
		} else {
			updated.Status = StatusInactive
		}
		if err := s.store.PutContact(ctx, updated); err != nil {
			return err
		}
	}
	return nil
}

func buildDecisions(
	tickID string,
	contacts []Contact,
	candidates []ShareCandidate,
	featuresByContact map[string]map[string]CandidateFeature,
	pushTopic string,
	maxLinkedHistoryItems int,
	opts TickOptions,
) []ShareDecision {
	if len(contacts) == 0 || len(candidates) == 0 {
		return nil
	}
	decisions := make([]ShareDecision, 0, len(contacts))
	for _, contact := range contacts {
		bestScore := -1.0
		var bestCandidate ShareCandidate
		var bestBreakdown ScoreBreakdown
		var bestFeature CandidateFeature
		contactFeatures := featuresByContact[contact.ContactID]
		for _, candidate := range candidates {
			if contact.Kind == KindHuman && opts.Send && !opts.EnableHumanPublicSend && candidateTargetsPublicChat(contact, candidate) {
				continue
			}
			feature := contactFeatures[candidate.ItemID]
			score, breakdown := scoreCandidate(contact, candidate, feature)
			if score > bestScore {
				bestScore = score
				bestCandidate = candidate
				bestBreakdown = breakdown
				bestFeature = feature
			}
		}
		if bestScore <= 0 {
			continue
		}
		idempotencyKey := fmt.Sprintf("proactive:%s:%s:%s", tickID, sanitizeToken(contact.ContactID), sanitizeToken(bestCandidate.ItemID))
		topic := strings.TrimSpace(pushTopic)
		if topic == "" {
			topic = "share.proactive.v1"
		}
		linkedHistoryIDs := mergeLinkedHistoryIDs(bestCandidate.LinkedHistoryIDs, bestFeature.ExplicitHistoryLinks, bestFeature.Confidence, maxLinkedHistoryItems)
		decisions = append(decisions, ShareDecision{
			ContactID:        contact.ContactID,
			PeerID:           strings.TrimSpace(contact.PeerID),
			ItemID:           bestCandidate.ItemID,
			Topic:            topic,
			ContentType:      bestCandidate.ContentType,
			PayloadBase64:    bestCandidate.PayloadBase64,
			IdempotencyKey:   idempotencyKey,
			Score:            bestScore,
			ScoreBreakdown:   bestBreakdown,
			SourceChatID:     bestCandidate.SourceChatID,
			SourceChatType:   bestCandidate.SourceChatType,
			LinkedHistoryIDs: linkedHistoryIDs,
		})
	}
	sort.Slice(decisions, func(i, j int) bool {
		if decisions[i].Score == decisions[j].Score {
			return strings.TrimSpace(decisions[i].ContactID) < strings.TrimSpace(decisions[j].ContactID)
		}
		return decisions[i].Score > decisions[j].Score
	})
	return decisions
}

func scoreCandidate(contact Contact, candidate ShareCandidate, feature CandidateFeature) (float64, ScoreBreakdown) {
	now := time.Now().UTC()
	ageHours := now.Sub(candidate.CreatedAt).Hours()
	if ageHours < 0 {
		ageHours = 0
	}
	freshnessBias := math.Exp(-ageHours / 24.0)
	overlapSemantic := overlapScore(contact, candidate)
	if feature.HasOverlapSemantic {
		overlapSemantic = clamp(feature.OverlapSemantic, 0, 1)
	}
	depthFit := depthFitScore(contact, candidate)
	reciprocityNorm := clamp(contact.ReciprocityNorm, 0, 1)
	novelty := noveltyScore(contact, candidate)
	sensitivityPenalty := 0.0
	if strings.EqualFold(strings.TrimSpace(contact.TrustState), "tofu") && strings.EqualFold(strings.TrimSpace(candidate.SensitivityLevel), "high") {
		sensitivityPenalty = 0.25
	}

	score := 0.40*overlapSemantic +
		0.25*freshnessBias +
		0.15*depthFit +
		0.10*reciprocityNorm +
		0.10*novelty -
		sensitivityPenalty
	score = clamp(score, 0, 1)
	return score, ScoreBreakdown{
		OverlapSemantic:    overlapSemantic,
		FreshnessBias:      freshnessBias,
		DepthFit:           depthFit,
		ReciprocityNorm:    reciprocityNorm,
		Novelty:            novelty,
		SensitivityPenalty: sensitivityPenalty,
	}
}

func overlapScore(contact Contact, candidate ShareCandidate) float64 {
	weights := contact.TopicWeights
	if len(weights) == 0 {
		return 0.3
	}
	topics := make([]string, 0, len(candidate.Topics)+1)
	if strings.TrimSpace(candidate.Topic) != "" {
		topics = append(topics, strings.ToLower(strings.TrimSpace(candidate.Topic)))
	}
	for _, topic := range candidate.Topics {
		v := strings.ToLower(strings.TrimSpace(topic))
		if v != "" {
			topics = append(topics, v)
		}
	}
	if len(topics) == 0 {
		return 0.2
	}
	var sum float64
	var count int
	for _, topic := range topics {
		value := 0.2
		if score, ok := weights[topic]; ok {
			value = clamp(score, 0, 1)
		}
		sum += value
		count++
	}
	if count == 0 {
		return 0.2
	}
	return clamp(sum/float64(count), 0, 1)
}

func depthFitScore(contact Contact, candidate ShareCandidate) float64 {
	depthNorm := clamp(contact.UnderstandingDepth/100.0, 0, 1)
	depthHint := clamp(candidate.DepthHint, 0, 1)
	if depthHint == 0 {
		depthHint = depthNorm
	}
	return clamp(1.0-math.Abs(depthNorm-depthHint), 0, 1)
}

func noveltyScore(contact Contact, candidate ShareCandidate) float64 {
	if strings.TrimSpace(contact.LastSharedItemID) == "" {
		return 1
	}
	if strings.TrimSpace(contact.LastSharedItemID) == strings.TrimSpace(candidate.ItemID) {
		return 0
	}
	return 1
}

func filterFreshCandidates(candidates []ShareCandidate, now time.Time, window time.Duration) []ShareCandidate {
	if window <= 0 {
		window = DefaultFreshnessWindow
	}
	threshold := now.Add(-window)
	out := make([]ShareCandidate, 0, len(candidates))
	for _, item := range candidates {
		if item.CreatedAt.IsZero() || item.CreatedAt.Before(threshold) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func contactAllowedForProactive(contact Contact, now time.Time, opts TickOptions) bool {
	ok, _ := contactAllowedForProactiveReason(contact, now, opts)
	return ok
}

func contactAllowedForProactiveReason(contact Contact, now time.Time, opts TickOptions) (bool, string) {
	if contact.Status != StatusActive {
		return false, fmt.Sprintf("status=%s", strings.TrimSpace(string(contact.Status)))
	}
	trust := strings.TrimSpace(strings.ToLower(contact.TrustState))
	switch contact.Kind {
	case KindHuman:
		if !opts.EnableHumanContacts {
			return false, "human_contacts_disabled"
		}
		if !hasTelegramTarget(contact) {
			return false, "human_missing_telegram_target"
		}
		if opts.Send && !opts.EnableHumanSend {
			return false, "human_send_disabled"
		}
	default:
		if strings.TrimSpace(contact.PeerID) == "" {
			return false, "agent_missing_peer_id"
		}
		if trust != "" && trust != "verified" {
			return false, fmt.Sprintf("agent_trust_state=%s", trust)
		}
	}
	if contact.CooldownUntil != nil && contact.CooldownUntil.After(now) {
		return false, fmt.Sprintf("cooldown_until=%s", contact.CooldownUntil.UTC().Format(time.RFC3339))
	}
	return true, "ok"
}

func hasTelegramTarget(contact Contact) bool {
	for _, raw := range []string{contact.SubjectID, contact.ContactID} {
		v := strings.TrimSpace(strings.ToLower(raw))
		if strings.HasPrefix(v, "tg:@") || strings.HasPrefix(v, "tg:id:") {
			return true
		}
	}
	return false
}

func candidateTargetsPublicChat(contact Contact, candidate ShareCandidate) bool {
	chatType := strings.ToLower(strings.TrimSpace(candidate.SourceChatType))
	if chatType == "group" || chatType == "supergroup" {
		return true
	}
	if candidate.SourceChatID == 0 {
		return false
	}
	for _, chat := range contact.TelegramChats {
		if chat.ChatID != candidate.SourceChatID {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(chat.ChatType))
		return t == "group" || t == "supergroup"
	}
	return candidate.SourceChatID < 0
}

func recomputeRetainScore(contact Contact, now time.Time) Contact {
	depthNorm := clamp(contact.UnderstandingDepth/100.0, 0, 1)
	reciprocityNorm := clamp(contact.ReciprocityNorm, 0, 1)
	recentAt := contact.UpdatedAt
	if contact.LastInteractionAt != nil && contact.LastInteractionAt.After(recentAt) {
		recentAt = *contact.LastInteractionAt
	}
	if contact.LastSharedAt != nil && contact.LastSharedAt.After(recentAt) {
		recentAt = *contact.LastSharedAt
	}
	if recentAt.IsZero() {
		recentAt = now
	}
	ageHours := now.Sub(recentAt).Hours()
	if ageHours < 0 {
		ageHours = 0
	}
	recencyNorm := clamp(math.Exp(-ageHours/(24.0*14.0)), 0, 1)
	contact.RetainScore = clamp(0.45*depthNorm+0.35*recencyNorm+0.20*reciprocityNorm, 0, 1)
	return contact
}

func normalizeTickOptions(opts TickOptions) TickOptions {
	if opts.MaxTargets <= 0 {
		opts.MaxTargets = DefaultMaxTargetsPerTick
	}
	if opts.FreshnessWindow <= 0 {
		opts.FreshnessWindow = DefaultFreshnessWindow
	}
	opts.PushTopic = strings.TrimSpace(opts.PushTopic)
	if opts.PushTopic == "" {
		opts.PushTopic = "share.proactive.v1"
	}
	if opts.MaxLinkedHistoryItems <= 0 {
		opts.MaxLinkedHistoryItems = 4
	}
	opts.NicknameMinConfidence = clamp(opts.NicknameMinConfidence, 0, 1)
	if opts.NicknameMinConfidence == 0 {
		opts.NicknameMinConfidence = 0.70
	}
	return opts
}

func normalizeFeedbackSignal(signal FeedbackSignal) FeedbackSignal {
	switch FeedbackSignal(strings.ToLower(strings.TrimSpace(string(signal)))) {
	case FeedbackPositive:
		return FeedbackPositive
	case FeedbackNeutral:
		return FeedbackNeutral
	case FeedbackNegative:
		return FeedbackNegative
	default:
		return ""
	}
}

func negativeCooldownDuration(interestLevel float64) (time.Duration, bool) {
	interestLevel = clamp(interestLevel, 0, 1)
	switch {
	case interestLevel < 0.10:
		return 72 * time.Hour, true
	case interestLevel < 0.20:
		return 48 * time.Hour, true
	case interestLevel < 0.30:
		return 24 * time.Hour, true
	default:
		return 0, false
	}
}

func blendPreference(oldValue float64, targetValue float64, alpha float64) float64 {
	alpha = clamp(alpha, 0, 1)
	oldValue = clamp(oldValue, 0, 1)
	targetValue = clamp(targetValue, 0, 1)
	return clamp((1-alpha)*oldValue+alpha*targetValue, 0, 1)
}

func contactPreferenceChanged(before Contact, after Contact) bool {
	if strings.TrimSpace(before.PersonaBrief) != strings.TrimSpace(after.PersonaBrief) {
		return true
	}
	if !floatMapEqual(before.TopicWeights, after.TopicWeights) {
		return true
	}
	if !floatMapEqual(before.PersonaTraits, after.PersonaTraits) {
		return true
	}
	return false
}

func floatMapEqual(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if bv, ok := b[k]; !ok || math.Abs(av-bv) > 1e-9 {
			return false
		}
	}
	return true
}

func mergeLinkedHistoryIDs(base []string, llmLinks []string, confidence float64, maxItems int) []string {
	combined := normalizeStringSlice(base)
	if confidence >= 0.7 {
		combined = append(combined, llmLinks...)
		combined = normalizeStringSlice(combined)
	}
	if maxItems > 0 && len(combined) > maxItems {
		combined = combined[:maxItems]
	}
	if len(combined) == 0 {
		return nil
	}
	return combined
}

func deriveContactID(contact Contact) string {
	if v := strings.TrimSpace(contact.SubjectID); v != "" {
		return v
	}
	if v := strings.TrimSpace(contact.NodeID); v != "" {
		return v
	}
	if v := strings.TrimSpace(contact.PeerID); v != "" {
		return v
	}
	return ""
}

func sanitizeToken(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return "x"
	}
	var b strings.Builder
	b.Grow(len(input))
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func normalizeNow(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func (s *Service) applyContactPreferences(
	ctx context.Context,
	contacts []Contact,
	candidates []ShareCandidate,
	now time.Time,
	tickID string,
	extractor PreferenceExtractor,
) ([]Contact, error) {
	if extractor == nil || len(contacts) == 0 || len(candidates) == 0 {
		return contacts, nil
	}
	out := make([]Contact, len(contacts))
	copy(out, contacts)
	for i := range out {
		contact := out[i]
		features, err := extractor.EvaluateContactPreferences(ctx, contact, candidates)
		if err != nil {
			_ = s.store.AppendAuditEvent(ctx, AuditEvent{
				EventID:   "evt_" + uuid.NewString(),
				TickID:    tickID,
				Action:    "contact_preference_extract_failed",
				ContactID: contact.ContactID,
				PeerID:    contact.PeerID,
				Reason:    err.Error(),
				CreatedAt: now,
			})
			continue
		}

		features.Confidence = clamp(features.Confidence, 0, 1)
		topicAlpha := alphaTopicWeight * features.Confidence
		topicUpdates := 0
		personaTraitsUpdates := 0
		personaBriefUpdated := false

		if len(features.TopicAffinity) > 0 {
			if contact.TopicWeights == nil {
				contact.TopicWeights = map[string]float64{}
			}
			for topic, target := range features.TopicAffinity {
				normalizedTopic := strings.ToLower(strings.TrimSpace(topic))
				if normalizedTopic == "" {
					continue
				}
				oldValue := clamp(contact.TopicWeights[normalizedTopic], 0, 1)
				newValue := blendPreference(oldValue, target, topicAlpha)
				if oldValue == newValue {
					continue
				}
				contact.TopicWeights[normalizedTopic] = newValue
				topicUpdates++
			}
			contact.TopicWeights = normalizeTopicWeightsMap(contact.TopicWeights)
		}

		if brief := strings.TrimSpace(features.PersonaBrief); brief != "" && brief != contact.PersonaBrief {
			contact.PersonaBrief = brief
			personaBriefUpdated = true
		}

		if len(features.PersonaTraits) > 0 {
			if contact.PersonaTraits == nil {
				contact.PersonaTraits = map[string]float64{}
			}
			for trait, target := range features.PersonaTraits {
				normalizedTrait := strings.ToLower(strings.TrimSpace(trait))
				if normalizedTrait == "" {
					continue
				}
				oldValue := clamp(contact.PersonaTraits[normalizedTrait], 0, 1)
				newValue := blendPreference(oldValue, target, topicAlpha)
				if oldValue == newValue {
					continue
				}
				contact.PersonaTraits[normalizedTrait] = newValue
				personaTraitsUpdates++
			}
		}

		if topicUpdates == 0 && !personaBriefUpdated && personaTraitsUpdates == 0 {
			continue
		}
		if err := s.store.PutContact(ctx, contact); err != nil {
			return nil, err
		}
		out[i] = contact
		_ = s.store.AppendAuditEvent(ctx, AuditEvent{
			EventID:   "evt_" + uuid.NewString(),
			TickID:    tickID,
			Action:    "contact_preference_extract_applied",
			ContactID: contact.ContactID,
			PeerID:    contact.PeerID,
			Metadata: map[string]string{
				"confidence":             fmt.Sprintf("%.3f", features.Confidence),
				"topic_updates":          fmt.Sprintf("%d", topicUpdates),
				"topic_alpha":            fmt.Sprintf("%.3f", topicAlpha),
				"persona_brief_updated":  fmt.Sprintf("%t", personaBriefUpdated),
				"persona_traits_updates": fmt.Sprintf("%d", personaTraitsUpdates),
			},
			CreatedAt: now,
		})
	}
	return out, nil
}

func (s *Service) assignNicknames(
	ctx context.Context,
	contacts []Contact,
	now time.Time,
	tickID string,
	generator NicknameGenerator,
	minConfidence float64,
) ([]Contact, error) {
	if generator == nil || len(contacts) == 0 {
		return contacts, nil
	}
	out := make([]Contact, len(contacts))
	copy(out, contacts)
	for i := range out {
		contact := out[i]
		if strings.TrimSpace(contact.ContactNickname) != "" {
			continue
		}
		if contact.UnderstandingDepth < defaultAutoNicknameDepthThreshold {
			continue
		}
		nickname, confidence, err := generator.SuggestNickname(ctx, contact)
		if err != nil {
			_ = s.store.AppendAuditEvent(ctx, AuditEvent{
				EventID:   "evt_" + uuid.NewString(),
				TickID:    tickID,
				Action:    auditActionNicknameAutoFailed,
				ContactID: contact.ContactID,
				PeerID:    contact.PeerID,
				Reason:    err.Error(),
				CreatedAt: now,
			})
			continue
		}
		nickname = strings.TrimSpace(nickname)
		confidence = clamp(confidence, 0, 1)
		if nickname == "" || confidence < minConfidence {
			continue
		}
		contact.ContactNickname = nickname
		if err := s.store.PutContact(ctx, contact); err != nil {
			return nil, err
		}
		out[i] = contact
		_ = s.store.AppendAuditEvent(ctx, AuditEvent{
			EventID:   "evt_" + uuid.NewString(),
			TickID:    tickID,
			Action:    auditActionNicknameAutoAssigned,
			ContactID: contact.ContactID,
			PeerID:    contact.PeerID,
			Reason:    "llm_generated",
			Metadata: map[string]string{
				"contact_nickname":    contact.ContactNickname,
				"confidence":          fmt.Sprintf("%.3f", confidence),
				"understanding_depth": fmt.Sprintf("%.2f", contact.UnderstandingDepth),
				"threshold":           fmt.Sprintf("%.2f", defaultAutoNicknameDepthThreshold),
			},
			CreatedAt: now,
		})
	}
	return out, nil
}
