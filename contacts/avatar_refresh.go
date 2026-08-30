package contacts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	ContactAvatarTTL           = 7 * 24 * time.Hour
	contactAvatarFetchTimeout  = 10 * time.Second
	contactAvatarQueueSize     = 2 * maxActiveContacts
	contactAvatarPruneInterval = time.Hour
)

type ContactAvatarFetchFunc func(context.Context) ([]byte, bool, error)

type contactAvatarRefreshRequest struct {
	contactID string
	fetch     ContactAvatarFetchFunc
}

type ContactAvatarRefresher struct {
	ctx     context.Context
	cancel  context.CancelFunc
	store   *FileStore
	logger  *slog.Logger
	queue   chan contactAvatarRefreshRequest
	mu      sync.Mutex
	pending map[string]struct{}
	missing map[string]time.Time
	wg      sync.WaitGroup
}

func NewContactAvatarRefresher(ctx context.Context, store *FileStore, logger *slog.Logger) (*ContactAvatarRefresher, error) {
	if store == nil {
		return nil, fmt.Errorf("contacts store is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}
	workerCtx, cancel := context.WithCancel(ctx)
	refresher := &ContactAvatarRefresher{
		ctx:     workerCtx,
		cancel:  cancel,
		store:   store,
		logger:  logger,
		queue:   make(chan contactAvatarRefreshRequest, contactAvatarQueueSize),
		pending: make(map[string]struct{}),
		missing: make(map[string]time.Time),
	}
	refresher.wg.Add(1)
	go refresher.run()
	return refresher, nil
}

func (r *ContactAvatarRefresher) Enqueue(contactID string, fetch ContactAvatarFetchFunc) bool {
	if r == nil || fetch == nil {
		return false
	}
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return false
	}
	select {
	case <-r.ctx.Done():
		return false
	default:
	}

	r.mu.Lock()
	if until, exists := r.missing[contactID]; exists {
		if until.After(time.Now()) {
			r.mu.Unlock()
			return false
		}
		delete(r.missing, contactID)
	}
	if _, exists := r.pending[contactID]; exists {
		r.mu.Unlock()
		return false
	}
	r.pending[contactID] = struct{}{}
	r.mu.Unlock()

	request := contactAvatarRefreshRequest{contactID: contactID, fetch: fetch}
	select {
	case r.queue <- request:
		return true
	case <-r.ctx.Done():
		r.finish(contactID)
		return false
	default:
		r.finish(contactID)
		r.logger.Debug("contact_avatar_refresh_queue_full", "contact_id", contactID)
		return false
	}
}

func (r *ContactAvatarRefresher) Prewarm(channel string, fetch func(Contact) ContactAvatarFetchFunc) error {
	if r == nil || fetch == nil {
		return fmt.Errorf("contact avatar prewarm requires a refresher and fetch function")
	}
	channel = normalizeContactChannel(channel)
	items, err := r.store.ListContacts(r.ctx, StatusActive)
	if err != nil {
		return err
	}
	for _, contact := range items {
		if normalizeContactChannel(contact.Channel) != channel {
			continue
		}
		if contactFetch := fetch(contact); contactFetch != nil {
			r.Enqueue(contact.ContactID, contactFetch)
		}
	}
	return nil
}

func (r *ContactAvatarRefresher) Close() {
	if r == nil {
		return
	}
	r.cancel()
	r.wg.Wait()
}

func (r *ContactAvatarRefresher) run() {
	defer r.wg.Done()
	ticker := time.NewTicker(contactAvatarPruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case now := <-ticker.C:
			r.pruneMissing(now)
		case request := <-r.queue:
			r.refresh(request)
		}
	}
}

func (r *ContactAvatarRefresher) refresh(request contactAvatarRefreshRequest) {
	defer r.finish(request.contactID)
	startedAt := time.Now()
	now := time.Now().UTC()
	fresh, err := r.store.ContactAvatarFresh(r.ctx, request.contactID, now, ContactAvatarTTL)
	if err != nil {
		if r.ctx.Err() == nil {
			r.logger.Warn("contact_avatar_cache_stat_failed", "contact_id", request.contactID, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err.Error())
		}
		return
	}
	if fresh {
		return
	}

	fetchCtx, cancel := context.WithTimeout(r.ctx, contactAvatarFetchTimeout)
	raw, found, err := request.fetch(fetchCtx)
	cancel()
	if err != nil {
		if r.ctx.Err() == nil {
			r.logger.Debug("contact_avatar_fetch_failed", "contact_id", request.contactID, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err.Error())
		}
		return
	}
	if !found {
		if err := r.store.DeleteContactAvatar(r.ctx, request.contactID); err != nil && r.ctx.Err() == nil {
			r.logger.Warn("contact_avatar_delete_failed", "contact_id", request.contactID, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err.Error())
		}
		r.mu.Lock()
		r.missing[request.contactID] = now.Add(ContactAvatarTTL)
		r.mu.Unlock()
		r.logger.Debug("contact_avatar_missing", "contact_id", request.contactID, "duration_ms", time.Since(startedAt).Milliseconds())
		return
	}
	if _, exists, err := r.store.GetContact(r.ctx, request.contactID); err != nil {
		if r.ctx.Err() == nil {
			r.logger.Warn("contact_avatar_contact_check_failed", "contact_id", request.contactID, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err.Error())
		}
		return
	} else if !exists {
		r.logger.Debug("contact_avatar_contact_deleted", "contact_id", request.contactID, "duration_ms", time.Since(startedAt).Milliseconds())
		return
	}
	if err := r.store.PutContactAvatar(r.ctx, request.contactID, raw); err != nil && r.ctx.Err() == nil {
		r.logger.Warn("contact_avatar_cache_write_failed", "contact_id", request.contactID, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err.Error())
	}
}

func (r *ContactAvatarRefresher) pruneMissing(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for contactID, until := range r.missing {
		if !until.After(now) {
			delete(r.missing, contactID)
		}
	}
}

func (r *ContactAvatarRefresher) finish(contactID string) {
	r.mu.Lock()
	delete(r.pending, contactID)
	r.mu.Unlock()
}

func FetchContactAvatarURL(ctx context.Context, client *http.Client, rawURL string) ([]byte, bool, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, false, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, false, fmt.Errorf("contact avatar URL is invalid")
	}
	if !strings.EqualFold(parsed.Scheme, "https") || strings.TrimSpace(parsed.Host) == "" {
		return nil, false, fmt.Errorf("contact avatar URL must use HTTPS")
	}
	if parsed.User != nil {
		return nil, false, fmt.Errorf("contact avatar URL must not include credentials")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{Timeout: contactAvatarFetchTimeout}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, false, err
	}
	request.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/gif;q=0.8,*/*;q=0.1")

	httpClient := *client
	originalRedirect := httpClient.CheckRedirect
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("contact avatar redirect limit exceeded")
		}
		if !strings.EqualFold(req.URL.Scheme, "https") || req.URL.User != nil {
			return fmt.Errorf("contact avatar redirect must use HTTPS without credentials")
		}
		if originalRedirect != nil {
			return originalRedirect(req, via)
		}
		return nil
	}
	response, err := httpClient.Do(request)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, false, fmt.Errorf("contact avatar request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return nil, false, fmt.Errorf("contact avatar HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, ContactAvatarMaxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(raw) > ContactAvatarMaxBytes {
		return nil, false, fmt.Errorf("contact avatar exceeds %d bytes", ContactAvatarMaxBytes)
	}
	if len(raw) == 0 {
		return nil, false, fmt.Errorf("contact avatar response is empty")
	}
	return raw, true, nil
}
