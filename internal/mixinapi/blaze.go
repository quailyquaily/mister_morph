package mixinapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	DefaultBlazeURL          = "wss://blaze.mixin.one/"
	blazeSubprotocol         = "Mixin-Blaze-1"
	blazeActionListPending   = "LIST_PENDING_MESSAGES"
	blazeActionCreateMessage = "CREATE_MESSAGE"
	blazeActionAcknowledge   = "ACKNOWLEDGE_MESSAGE_RECEIPT"
	maxBlazeFrameBytes       = 4 << 20
	defaultBlazeMinBackoff   = time.Second
	defaultBlazeMaxBackoff   = 30 * time.Second
	defaultBlazeWriteTimeout = 10 * time.Second
	defaultBlazePongWait     = 10 * time.Second
	defaultBlazePingPeriod   = 9 * time.Second
)

type BlazeError struct {
	Status      int    `json:"status"`
	Code        int    `json:"code"`
	Description string `json:"description"`
}

type BlazeEnvelope struct {
	ID     string          `json:"id"`
	Action string          `json:"action"`
	Params map[string]any  `json:"params,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  *BlazeError     `json:"error,omitempty"`
}

type MessageView struct {
	ConversationID   string    `json:"conversation_id"`
	UserID           string    `json:"user_id"`
	MessageID        string    `json:"message_id"`
	Category         string    `json:"category"`
	DataBase64       string    `json:"data_base64"`
	RepresentativeID string    `json:"representative_id"`
	QuoteMessageID   string    `json:"quote_message_id"`
	Status           string    `json:"status"`
	Source           string    `json:"source"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type MessageHandler func(context.Context, MessageView) error

type BlazeOptions struct {
	URL                string
	Dialer             *websocket.Dialer
	MinBackoff         time.Duration
	MaxBackoff         time.Duration
	Now                func() time.Time
	NewRequestID       func() string
	OnConnectionChange func(bool)
	OnReconnect        func(error, time.Duration)
}

type BlazeClient struct {
	credentials        Credentials
	url                string
	dialer             *websocket.Dialer
	minBackoff         time.Duration
	maxBackoff         time.Duration
	now                func() time.Time
	newRequestID       func() string
	onConnectionChange func(bool)
	onReconnect        func(error, time.Duration)
	pongWait           time.Duration
	pingPeriod         time.Duration
}

func NewBlazeClient(credentials Credentials, opts BlazeOptions) (*BlazeClient, error) {
	if err := credentials.validate(); err != nil {
		return nil, fmt.Errorf("invalid mixin credentials: %w", err)
	}
	blazeURL := strings.TrimSpace(opts.URL)
	if blazeURL == "" {
		blazeURL = DefaultBlazeURL
	}
	parsed, err := url.Parse(blazeURL)
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.Host == "" {
		return nil, fmt.Errorf("mixin blaze url is invalid")
	}
	dialer := opts.Dialer
	if dialer == nil {
		clone := *websocket.DefaultDialer
		dialer = &clone
	}
	dialerClone := *dialer
	dialerClone.Subprotocols = []string{blazeSubprotocol}
	minBackoff := opts.MinBackoff
	if minBackoff <= 0 {
		minBackoff = defaultBlazeMinBackoff
	}
	maxBackoff := opts.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = defaultBlazeMaxBackoff
	}
	if maxBackoff < minBackoff {
		maxBackoff = minBackoff
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	requestID := opts.NewRequestID
	if requestID == nil {
		requestID = newRequestID
	}
	return &BlazeClient{
		credentials:        credentials,
		url:                parsed.String(),
		dialer:             &dialerClone,
		minBackoff:         minBackoff,
		maxBackoff:         maxBackoff,
		now:                now,
		newRequestID:       requestID,
		onConnectionChange: opts.OnConnectionChange,
		onReconnect:        opts.OnReconnect,
		pongWait:           defaultBlazePongWait,
		pingPeriod:         defaultBlazePingPeriod,
	}, nil
}

func (c *BlazeClient) Run(ctx context.Context, handler MessageHandler) error {
	if c == nil {
		return fmt.Errorf("mixin blaze client is not initialized")
	}
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if handler == nil {
		return fmt.Errorf("mixin blaze message handler is required")
	}
	backoff := c.minBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := c.runConnection(ctx, handler)
		if err == nil {
			backoff = c.minBackoff
		} else if IsUnauthorized(err) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		delay := retryAfter(err)
		if delay <= 0 {
			delay = jitterBackoff(backoff)
		}
		if c.onReconnect != nil {
			c.onReconnect(err, delay)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < c.maxBackoff {
			backoff *= 2
			if backoff > c.maxBackoff {
				backoff = c.maxBackoff
			}
		}
	}
}

func (c *BlazeClient) runConnection(ctx context.Context, handler MessageHandler) error {
	token, err := signAuthenticationToken(c.credentials, http.MethodGet, "/", nil, c.now(), c.newRequestID())
	if err != nil {
		return err
	}
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+token)
	conn, response, err := c.dialer.DialContext(ctx, c.url, header)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		if response != nil && response.StatusCode == http.StatusUnauthorized {
			return &APIError{HTTPStatus: http.StatusUnauthorized, Status: http.StatusUnauthorized, Description: "blaze authentication failed"}
		}
		return fmt.Errorf("connect mixin blaze: %w", err)
	}
	if c.onConnectionChange != nil {
		c.onConnectionChange(true)
		defer c.onConnectionChange(false)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(c.pongWait)); err != nil {
		return fmt.Errorf("set mixin blaze read deadline: %w", err)
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(c.pongWait))
	})
	connectionDone := make(chan struct{})
	defer close(connectionDone)
	go func() {
		ticker := time.NewTicker(c.pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-connectionDone:
				return
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(defaultBlazeWriteTimeout)); err != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-connectionDone:
		}
	}()
	if err := writeBlazeEnvelope(conn, BlazeEnvelope{ID: c.newRequestID(), Action: blazeActionListPending}); err != nil {
		return fmt.Errorf("list mixin pending messages: %w", err)
	}
	for {
		var envelope BlazeEnvelope
		if err := readBlazeEnvelope(conn, &envelope); err != nil {
			return fmt.Errorf("read mixin blaze: %w", err)
		}
		if envelope.Error != nil {
			apiErr := &APIError{Status: envelope.Error.Status, Code: envelope.Error.Code, Description: envelope.Error.Description}
			if apiErr.Status == http.StatusUnauthorized || apiErr.Code == http.StatusUnauthorized {
				apiErr.HTTPStatus = http.StatusUnauthorized
			}
			return apiErr
		}
		if envelope.Action != blazeActionCreateMessage {
			continue
		}
		var message MessageView
		if err := json.Unmarshal(envelope.Data, &message); err != nil {
			return fmt.Errorf("decode mixin message: %w", err)
		}
		if err := handler(ctx, message); err != nil {
			return fmt.Errorf("handle mixin message: %w", err)
		}
		if err := writeBlazeEnvelope(conn, BlazeEnvelope{
			ID:     c.newRequestID(),
			Action: blazeActionAcknowledge,
			Params: map[string]any{"message_id": message.MessageID, "status": "READ"},
		}); err != nil {
			return fmt.Errorf("acknowledge mixin message: %w", err)
		}
	}
}

func retryAfter(err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	return 0
}

func readBlazeEnvelope(conn *websocket.Conn, target *BlazeEnvelope) error {
	if conn == nil || target == nil {
		return fmt.Errorf("mixin blaze connection and target are required")
	}
	messageType, reader, err := conn.NextReader()
	if err != nil {
		return err
	}
	if messageType != websocket.BinaryMessage {
		return fmt.Errorf("mixin blaze frame must be binary")
	}
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("open mixin blaze gzip frame: %w", err)
	}
	defer gzipReader.Close()
	raw, err := io.ReadAll(io.LimitReader(gzipReader, maxBlazeFrameBytes+1))
	if err != nil {
		return fmt.Errorf("read mixin blaze gzip frame: %w", err)
	}
	if len(raw) > maxBlazeFrameBytes {
		return fmt.Errorf("mixin blaze frame exceeds %d bytes", maxBlazeFrameBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode mixin blaze frame: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("decode mixin blaze frame: trailing data")
	}
	return nil
}

func writeBlazeEnvelope(conn *websocket.Conn, envelope BlazeEnvelope) error {
	if conn == nil {
		return fmt.Errorf("mixin blaze connection is required")
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	var compressed bytes.Buffer
	gzipWriter, err := gzip.NewWriterLevel(&compressed, 3)
	if err != nil {
		return err
	}
	if _, err := gzipWriter.Write(raw); err != nil {
		_ = gzipWriter.Close()
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(defaultBlazeWriteTimeout))
	return conn.WriteMessage(websocket.BinaryMessage, compressed.Bytes())
}

func jitterBackoff(delay time.Duration) time.Duration {
	if delay <= 2*time.Millisecond {
		return delay
	}
	spread := delay / 5
	return delay - spread + time.Duration(rand.Int64N(int64(spread*2)+1))
}
