package consolecmd

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"golang.org/x/crypto/bcrypt"
)

type passwordVerifier struct {
	plain string
	hash  string
}

func newPasswordVerifier(plain, hash string) (*passwordVerifier, error) {
	plain = strings.TrimSpace(plain)
	hash = strings.TrimSpace(hash)
	if plain == "" && hash == "" {
		return nil, fmt.Errorf("missing console password (set console.password/console.password_hash or env)")
	}
	if hash != "" && !strings.HasPrefix(hash, "$2") {
		return nil, fmt.Errorf("console.password_hash currently supports bcrypt hashes only")
	}
	return &passwordVerifier{
		plain: plain,
		hash:  hash,
	}, nil
}

func consolePasswordConfigured(plain, hash string) bool {
	return strings.TrimSpace(plain) != "" || strings.TrimSpace(hash) != ""
}

func (v *passwordVerifier) Verify(candidate string) bool {
	if v == nil {
		return false
	}
	if strings.TrimSpace(v.hash) != "" {
		return bcrypt.CompareHashAndPassword([]byte(v.hash), []byte(candidate)) == nil
	}
	left := []byte(v.plain)
	right := []byte(candidate)
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare(left, right) == 1
}

type tokenSession struct {
	ExpiresAt time.Time
}

type persistedSessions struct {
	Version  int               `json:"version"`
	Sessions map[string]string `json:"sessions"`
}

type sessionStore struct {
	mu       sync.RWMutex
	path     string
	sessions map[string]tokenSession
}

func newSessionStore(path string) *sessionStore {
	store := &sessionStore{
		path:     strings.TrimSpace(path),
		sessions: map[string]tokenSession{},
	}
	store.load()
	return store
}

func (s *sessionStore) load() {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return
	}
	var payload persistedSessions
	ok, err := fsstore.ReadJSON(s.path, &payload)
	if err != nil || !ok {
		return
	}
	now := time.Now().UTC()
	for tokenHash, expiresAtRaw := range payload.Sessions {
		tokenHash = strings.TrimSpace(tokenHash)
		if tokenHash == "" {
			continue
		}
		expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(expiresAtRaw))
		if err != nil {
			continue
		}
		expiresAt = expiresAt.UTC()
		if !expiresAt.After(now) {
			continue
		}
		s.sessions[tokenHash] = tokenSession{ExpiresAt: expiresAt}
	}
}

func (s *sessionStore) persistLocked() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil
	}
	payload := persistedSessions{
		Version:  1,
		Sessions: map[string]string{},
	}
	for tokenHash, item := range s.sessions {
		tokenHash = strings.TrimSpace(tokenHash)
		if tokenHash == "" {
			continue
		}
		expiresAt := item.ExpiresAt.UTC()
		if expiresAt.IsZero() {
			continue
		}
		payload.Sessions[tokenHash] = expiresAt.Format(time.RFC3339Nano)
	}
	return fsstore.WriteJSONAtomic(s.path, payload, fsstore.FileOptions{})
}

func (s *sessionStore) Create(ttl time.Duration) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, fmt.Errorf("nil session store")
	}
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	hash := tokenHash(token)
	expiresAt := time.Now().UTC().Add(ttl)

	s.mu.Lock()
	s.pruneExpiredLocked(time.Now().UTC())
	s.sessions[hash] = tokenSession{ExpiresAt: expiresAt}
	if err := s.persistLocked(); err != nil {
		delete(s.sessions, hash)
		s.mu.Unlock()
		return "", time.Time{}, err
	}
	s.mu.Unlock()

	return token, expiresAt, nil
}

func (s *sessionStore) Validate(token string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	hash := tokenHash(token)
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	changed := s.pruneExpiredLocked(now)
	if changed {
		_ = s.persistLocked()
	}
	session, ok := s.sessions[hash]
	if !ok {
		return time.Time{}, false
	}
	if !session.ExpiresAt.After(now) {
		delete(s.sessions, hash)
		_ = s.persistLocked()
		return time.Time{}, false
	}
	return session.ExpiresAt, true
}

func (s *sessionStore) Delete(token string) {
	if s == nil {
		return
	}
	hash := tokenHash(token)
	s.mu.Lock()
	_, existed := s.sessions[hash]
	delete(s.sessions, hash)
	if existed {
		_ = s.persistLocked()
	}
	s.mu.Unlock()
}

func (s *sessionStore) pruneExpiredLocked(now time.Time) bool {
	changed := false
	for k, v := range s.sessions {
		if !v.ExpiresAt.After(now) {
			delete(s.sessions, k)
			changed = true
		}
	}
	return changed
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type loginLimiter struct {
	mu                sync.Mutex
	window            time.Duration
	maxFailuresPerIP  int
	maxFailuresPerKey int
	lockDuration      time.Duration
	delayMin          time.Duration
	delayMax          time.Duration
	ipFailures        map[string][]time.Time
	keyFailures       map[string][]time.Time
	locks             map[string]time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		window:            10 * time.Minute,
		maxFailuresPerIP:  20,
		maxFailuresPerKey: 5,
		lockDuration:      15 * time.Minute,
		delayMin:          200 * time.Millisecond,
		delayMax:          1200 * time.Millisecond,
		ipFailures:        map[string][]time.Time{},
		keyFailures:       map[string][]time.Time{},
		locks:             map[string]time.Time{},
	}
}

func (l *loginLimiter) CheckLocked(key string, now time.Time) (time.Duration, bool) {
	if l == nil {
		return 0, false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return 0, false
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneLocked(now)
	until, ok := l.locks[key]
	if !ok || !until.After(now) {
		return 0, false
	}
	return until.Sub(now), true
}

func (l *loginLimiter) RecordFailure(ip, key string, now time.Time) time.Time {
	if l == nil {
		return time.Time{}
	}
	ip = strings.TrimSpace(ip)
	key = strings.TrimSpace(key)

	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneFailuresLocked(now)
	l.pruneLocked(now)

	if ip != "" {
		l.ipFailures[ip] = append(l.ipFailures[ip], now)
	}
	if key != "" {
		l.keyFailures[key] = append(l.keyFailures[key], now)
	}

	reachedIP := ip != "" && len(l.ipFailures[ip]) >= l.maxFailuresPerIP
	reachedKey := key != "" && len(l.keyFailures[key]) >= l.maxFailuresPerKey
	if reachedIP || reachedKey {
		lockUntil := now.Add(l.lockDuration)
		if key != "" {
			l.locks[key] = lockUntil
		}
		return lockUntil
	}
	return time.Time{}
}

func (l *loginLimiter) RecordSuccess(ip, key string, now time.Time) {
	if l == nil {
		return
	}
	ip = strings.TrimSpace(ip)
	key = strings.TrimSpace(key)

	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneFailuresLocked(now)
	l.pruneLocked(now)
	if ip != "" {
		delete(l.ipFailures, ip)
	}
	if key != "" {
		delete(l.keyFailures, key)
		delete(l.locks, key)
	}
}

func (l *loginLimiter) FailureDelay() time.Duration {
	if l == nil {
		return 0
	}
	if l.delayMax <= l.delayMin {
		return l.delayMin
	}
	span := l.delayMax - l.delayMin
	// 8 random bytes -> uint64.
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return l.delayMin
	}
	var n uint64
	for _, b := range buf {
		n = (n << 8) | uint64(b)
	}
	return l.delayMin + time.Duration(n%uint64(span))
}

func (l *loginLimiter) pruneFailuresLocked(now time.Time) {
	cutoff := now.Add(-l.window)
	for ip, items := range l.ipFailures {
		next := filterRecent(items, cutoff)
		if len(next) == 0 {
			delete(l.ipFailures, ip)
			continue
		}
		l.ipFailures[ip] = next
	}
	for key, items := range l.keyFailures {
		next := filterRecent(items, cutoff)
		if len(next) == 0 {
			delete(l.keyFailures, key)
			continue
		}
		l.keyFailures[key] = next
	}
}

func (l *loginLimiter) pruneLocked(now time.Time) {
	for key, until := range l.locks {
		if !until.After(now) {
			delete(l.locks, key)
		}
	}
}

func filterRecent(items []time.Time, cutoff time.Time) []time.Time {
	if len(items) == 0 {
		return nil
	}
	out := make([]time.Time, 0, len(items))
	for _, ts := range items {
		if ts.After(cutoff) {
			out = append(out, ts)
		}
	}
	return out
}
