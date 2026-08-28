package mixinapi

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const authenticationTokenTTL = 5 * time.Minute

type authenticationClaims struct {
	UID string `json:"uid"`
	SID string `json:"sid"`
	IAT int64  `json:"iat"`
	EXP int64  `json:"exp"`
	JTI string `json:"jti"`
	SIG string `json:"sig"`
	SCP string `json:"scp"`
}

func signAuthenticationToken(credentials Credentials, method, uri string, body []byte, now time.Time, requestID string) (string, error) {
	if err := credentials.validate(); err != nil {
		return "", fmt.Errorf("invalid mixin credentials: %w", err)
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return "", fmt.Errorf("method is required")
	}
	uri = strings.TrimSpace(uri)
	parsedURI, err := url.ParseRequestURI(uri)
	if err != nil || parsedURI.IsAbs() || !strings.HasPrefix(uri, "/") {
		return "", fmt.Errorf("uri must be an absolute request path")
	}
	requestID, err = normalizeUUID("request_id", requestID)
	if err != nil {
		return "", err
	}
	now = now.UTC()
	digestInput := make([]byte, 0, len(method)+len(uri)+len(body))
	digestInput = append(digestInput, method...)
	digestInput = append(digestInput, uri...)
	digestInput = append(digestInput, body...)
	digest := sha256.Sum256(digestInput)
	header, err := json.Marshal(struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}{Algorithm: "EdDSA", Type: "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(authenticationClaims{
		UID: credentials.ClientID,
		SID: credentials.SessionID,
		IAT: now.Unix(),
		EXP: now.Add(authenticationTokenTTL).Unix(),
		JTI: requestID,
		SIG: hex.EncodeToString(digest[:]),
		SCP: "FULL",
	})
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	signature := ed25519.Sign(credentials.privateKey, []byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func newRequestID() string {
	return uuid.NewString()
}
