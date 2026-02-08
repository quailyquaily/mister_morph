package contacts

import "time"

const (
	DefaultFreshnessWindow   = 72 * time.Hour
	DefaultMaxTargetsPerTick = 3
	DefaultMaxActiveContacts = 150
)

type Kind string

const (
	KindHuman Kind = "human"
	KindAgent Kind = "agent"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
)

type Contact struct {
	ContactID          string             `json:"contact_id"`
	Kind               Kind               `json:"kind"`
	Status             Status             `json:"status"`
	ContactNickname    string             `json:"contact_nickname,omitempty"`
	PersonaBrief       string             `json:"persona_brief,omitempty"`
	PersonaTraits      map[string]float64 `json:"persona_traits,omitempty"`
	DisplayName        string             `json:"display_name,omitempty"` // Legacy alias for contact_nickname.
	Pronouns           string             `json:"pronouns,omitempty"`
	Timezone           string             `json:"timezone,omitempty"`
	PreferenceContext  string             `json:"preference_context,omitempty"`
	SubjectID          string             `json:"subject_id,omitempty"`
	NodeID             string             `json:"node_id,omitempty"`
	PeerID             string             `json:"peer_id,omitempty"`
	Addresses          []string           `json:"addresses,omitempty"`
	TelegramChats      []TelegramChatRef  `json:"telegram_chats,omitempty"`
	TrustState         string             `json:"trust_state,omitempty"`
	UnderstandingDepth float64            `json:"understanding_depth"`
	TopicWeights       map[string]float64 `json:"topic_weights,omitempty"`
	ReciprocityNorm    float64            `json:"reciprocity_norm"`
	CooldownUntil      *time.Time         `json:"cooldown_until,omitempty"`
	LastInteractionAt  *time.Time         `json:"last_interaction_at,omitempty"`
	LastSharedAt       *time.Time         `json:"last_shared_at,omitempty"`
	LastSharedItemID   string             `json:"last_shared_item_id,omitempty"`
	ShareCount         int                `json:"share_count"`
	RetainScore        float64            `json:"retain_score"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
}

type ShareCandidate struct {
	ItemID           string    `json:"item_id"`
	Topic            string    `json:"topic"`
	Topics           []string  `json:"topics,omitempty"`
	ContentType      string    `json:"content_type"`
	PayloadBase64    string    `json:"payload_base64"`
	SensitivityLevel string    `json:"sensitivity_level,omitempty"`
	DepthHint        float64   `json:"depth_hint,omitempty"`
	SourceChatID     int64     `json:"source_chat_id,omitempty"`
	SourceChatType   string    `json:"source_chat_type,omitempty"`
	LinkedHistoryIDs []string  `json:"linked_history_ids,omitempty"`
	SourceRef        string    `json:"source_ref,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type SessionState struct {
	SessionID            string     `json:"session_id"`
	ContactID            string     `json:"contact_id"`
	SessionInterestLevel float64    `json:"session_interest_level"`
	TurnCount            int        `json:"turn_count"`
	StartedAt            time.Time  `json:"started_at"`
	EndedAt              *time.Time `json:"ended_at,omitempty"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type AuditEvent struct {
	EventID        string            `json:"event_id"`
	TickID         string            `json:"tick_id,omitempty"`
	Action         string            `json:"action"`
	ContactID      string            `json:"contact_id,omitempty"`
	PeerID         string            `json:"peer_id,omitempty"`
	ItemID         string            `json:"item_id,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	Score          float64           `json:"score,omitempty"`
	ScoreBreakdown ScoreBreakdown    `json:"score_breakdown,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
}

type ScoreBreakdown struct {
	OverlapSemantic    float64 `json:"overlap_semantic,omitempty"`
	FreshnessBias      float64 `json:"freshness_bias,omitempty"`
	DepthFit           float64 `json:"depth_fit,omitempty"`
	ReciprocityNorm    float64 `json:"reciprocity_norm,omitempty"`
	Novelty            float64 `json:"novelty,omitempty"`
	SensitivityPenalty float64 `json:"sensitivity_penalty,omitempty"`
}

type ShareDecision struct {
	ContactID        string         `json:"contact_id"`
	PeerID           string         `json:"peer_id,omitempty"`
	ItemID           string         `json:"item_id"`
	Topic            string         `json:"topic"`
	ContentType      string         `json:"content_type"`
	PayloadBase64    string         `json:"payload_base64"`
	IdempotencyKey   string         `json:"idempotency_key"`
	Score            float64        `json:"score"`
	ScoreBreakdown   ScoreBreakdown `json:"score_breakdown"`
	SourceChatID     int64          `json:"source_chat_id,omitempty"`
	SourceChatType   string         `json:"source_chat_type,omitempty"`
	LinkedHistoryIDs []string       `json:"linked_history_ids,omitempty"`
}

type TelegramChatRef struct {
	ChatID     int64      `json:"chat_id"`
	ChatType   string     `json:"chat_type,omitempty"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
}

type ShareOutcome struct {
	ContactID      string    `json:"contact_id"`
	PeerID         string    `json:"peer_id,omitempty"`
	ItemID         string    `json:"item_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	Accepted       bool      `json:"accepted"`
	Deduped        bool      `json:"deduped"`
	Error          string    `json:"error,omitempty"`
	SentAt         time.Time `json:"sent_at"`
}

type TickOptions struct {
	MaxTargets            int
	FreshnessWindow       time.Duration
	PushTopic             string
	Send                  bool
	EnableHumanContacts   bool
	EnableHumanSend       bool
	EnableHumanPublicSend bool
	MaxLinkedHistoryItems int
	FeatureExtractor      FeatureExtractor
	PreferenceExtractor   PreferenceExtractor
	NicknameGenerator     NicknameGenerator
	NicknameMinConfidence float64
}

type TickResult struct {
	TickID    string          `json:"tick_id"`
	StartedAt time.Time       `json:"started_at"`
	EndedAt   time.Time       `json:"ended_at"`
	Planned   int             `json:"planned"`
	Sent      int             `json:"sent"`
	Decisions []ShareDecision `json:"decisions"`
	Outcomes  []ShareOutcome  `json:"outcomes"`
}

type FeedbackSignal string

const (
	FeedbackPositive FeedbackSignal = "positive"
	FeedbackNeutral  FeedbackSignal = "neutral"
	FeedbackNegative FeedbackSignal = "negative"
)

type FeedbackUpdateInput struct {
	ContactID  string         `json:"contact_id"`
	SessionID  string         `json:"session_id,omitempty"`
	Signal     FeedbackSignal `json:"signal"`
	Topic      string         `json:"topic,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	EndSession bool           `json:"end_session,omitempty"`
}
