package maep

import (
	"encoding/json"
	"time"
)

const (
	NodeIDPrefix             = "maep:"
	ContactCardVersionV1     = 1
	ProtocolVersionV1        = 1
	ProtocolHelloIDV1        = "/maep/hello/1.0.0"
	ProtocolRPCIDV1          = "/maep/rpc/1.0.0"
	CapabilityDataPushV1     = "rpc.data.push.v1"
	JSONRPCVersion           = "2.0"
	DefaultHelloTimeout      = 3 * time.Second
	DefaultRPCTimeout        = 10 * time.Second
	DefaultDialAddrTimeout   = 3 * time.Second
	DefaultDedupeTTL         = 7 * 24 * time.Hour
	DefaultDedupeMaxPerPeer  = 100000
	DefaultDataPushRateLimit = 120
	MaxRPCRequestBytesV1     = 256 * 1024
	MaxPayloadBytesV1        = 128 * 1024
	ContactCardSigAlgEd25519 = "ed25519"
	ContactCardSigFormatJCS  = "jcs-rfc8785-detached"
	ContactCardSignDomainV1  = "maep-contact-card-v1\n"
)

type TrustState string

const (
	TrustStateTOFU       TrustState = "tofu"
	TrustStateVerified   TrustState = "verified"
	TrustStateConflicted TrustState = "conflicted"
	TrustStateRevoked    TrustState = "revoked"
)

type Identity struct {
	NodeUUID            string    `json:"node_uuid"`
	PeerID              string    `json:"peer_id"`
	NodeID              string    `json:"node_id"`
	IdentityPubEd25519  string    `json:"identity_pub_ed25519"`
	IdentityPrivEd25519 string    `json:"identity_priv_ed25519"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type ContactCardPayload struct {
	Version              int        `json:"version"`
	NodeUUID             string     `json:"node_uuid"`
	PeerID               string     `json:"peer_id"`
	NodeID               string     `json:"node_id,omitempty"`
	IdentityPubEd25519   string     `json:"identity_pub_ed25519"`
	Addresses            []string   `json:"addresses"`
	MinSupportedProtocol int        `json:"min_supported_protocol"`
	MaxSupportedProtocol int        `json:"max_supported_protocol"`
	IssuedAt             time.Time  `json:"issued_at"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	KeyRotationOf        string     `json:"key_rotation_of,omitempty"`
}

type ContactCardEnvelope struct {
	Payload   json.RawMessage `json:"payload"`
	SigAlg    string          `json:"sig_alg"`
	SigFormat string          `json:"sig_format"`
	Sig       string          `json:"sig"`
}

type ContactCard struct {
	Payload   ContactCardPayload `json:"payload"`
	SigAlg    string             `json:"sig_alg"`
	SigFormat string             `json:"sig_format"`
	Sig       string             `json:"sig"`
}

type ParsedContactCard struct {
	Card             ContactCard
	CanonicalPayload []byte
}

type Contact struct {
	NodeUUID             string     `json:"node_uuid"`
	PeerID               string     `json:"peer_id"`
	NodeID               string     `json:"node_id"`
	DisplayName          string     `json:"display_name,omitempty"`
	IdentityPubEd25519   string     `json:"identity_pub_ed25519"`
	Addresses            []string   `json:"addresses"`
	MinSupportedProtocol int        `json:"min_supported_protocol"`
	MaxSupportedProtocol int        `json:"max_supported_protocol"`
	IssuedAt             time.Time  `json:"issued_at"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	KeyRotationOf        string     `json:"key_rotation_of,omitempty"`
	CardSigAlg           string     `json:"card_sig_alg"`
	CardSigFormat        string     `json:"card_sig_format"`
	CardSig              string     `json:"card_sig"`
	TrustState           TrustState `json:"trust_state"`
	LastSeen             *time.Time `json:"last_seen,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type ImportContactResult struct {
	Contact    Contact
	Created    bool
	Updated    bool
	Conflicted bool
}

type DedupeRecord struct {
	FromPeerID     string    `json:"from_peer_id"`
	Topic          string    `json:"topic"`
	IdempotencyKey string    `json:"idempotency_key"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type ProtocolHistory struct {
	PeerID                 string    `json:"peer_id"`
	LastRemoteMaxProtocol  int       `json:"last_remote_max_protocol"`
	LastNegotiatedProtocol int       `json:"last_negotiated_protocol"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type DataPushEvent struct {
	FromPeerID     string    `json:"from_peer_id"`
	Topic          string    `json:"topic"`
	ContentType    string    `json:"content_type"`
	PayloadBase64  string    `json:"payload_base64"`
	PayloadBytes   []byte    `json:"payload_bytes,omitempty"`
	IdempotencyKey string    `json:"idempotency_key"`
	ReceivedAt     time.Time `json:"received_at"`
	Deduped        bool      `json:"deduped"`
}

type DataPushRequest struct {
	Topic          string `json:"topic"`
	ContentType    string `json:"content_type"`
	PayloadBase64  string `json:"payload_base64"`
	IdempotencyKey string `json:"idempotency_key"`
}

type DataPushResult struct {
	Accepted bool `json:"accepted"`
	Deduped  bool `json:"deduped"`
}

type InboxMessage struct {
	MessageID      string    `json:"message_id"`
	FromPeerID     string    `json:"from_peer_id"`
	Topic          string    `json:"topic"`
	ContentType    string    `json:"content_type"`
	PayloadBase64  string    `json:"payload_base64"`
	IdempotencyKey string    `json:"idempotency_key"`
	ReceivedAt     time.Time `json:"received_at"`
}
