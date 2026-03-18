package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/quailyquaily/mistermorph/internal/slackclient"
)

type slackAPI struct {
	http     *http.Client
	baseURL  string
	botToken string
	appToken string
}

func newSlackAPI(httpClient *http.Client, baseURL, botToken, appToken string) *slackAPI {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL = strings.TrimSpace(strings.TrimRight(baseURL, "/"))
	if baseURL == "" {
		baseURL = "https://slack.com/api"
	}
	return &slackAPI{
		http:     httpClient,
		baseURL:  baseURL,
		botToken: strings.TrimSpace(botToken),
		appToken: strings.TrimSpace(appToken),
	}
}

type slackAuthTestResult struct {
	TeamID  string
	UserID  string
	BotID   string
	URL     string
	Team    string
	User    string
	IsOwner bool
}

type slackAuthTestResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	TeamID  string `json:"team_id,omitempty"`
	UserID  string `json:"user_id,omitempty"`
	BotID   string `json:"bot_id,omitempty"`
	URL     string `json:"url,omitempty"`
	Team    string `json:"team,omitempty"`
	User    string `json:"user,omitempty"`
	IsOwner bool   `json:"is_owner,omitempty"`
}

type slackUserIdentity struct {
	UserID      string
	Username    string
	DisplayName string
}

type slackUserInfoResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	User  struct {
		ID      string `json:"id,omitempty"`
		Name    string `json:"name,omitempty"`
		Profile struct {
			DisplayName string `json:"display_name,omitempty"`
			RealName    string `json:"real_name,omitempty"`
		} `json:"profile,omitempty"`
	} `json:"user,omitempty"`
}

type slackEmojiListResponse struct {
	OK         bool            `json:"ok"`
	Error      string          `json:"error,omitempty"`
	Emoji      map[string]any  `json:"emoji,omitempty"`
	Categories json.RawMessage `json:"categories,omitempty"`
}

var slackEmojiNameRegexp = regexp.MustCompile(`^[A-Za-z0-9_+\-]+$`)

func (api *slackAPI) authTest(ctx context.Context) (slackAuthTestResult, error) {
	if api == nil {
		return slackAuthTestResult{}, fmt.Errorf("slack api is not initialized")
	}
	body, status, _, err := api.postAuthJSON(ctx, api.botToken, "/auth.test", nil)
	if err != nil {
		return slackAuthTestResult{}, err
	}
	if status < 200 || status >= 300 {
		return slackAuthTestResult{}, fmt.Errorf("slack auth.test http %d", status)
	}
	var out slackAuthTestResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return slackAuthTestResult{}, err
	}
	if !out.OK {
		code := strings.TrimSpace(out.Error)
		if code == "" {
			code = "unknown_error"
		}
		return slackAuthTestResult{}, fmt.Errorf("slack auth.test failed: %s", code)
	}
	return slackAuthTestResult{
		TeamID:  strings.TrimSpace(out.TeamID),
		UserID:  strings.TrimSpace(out.UserID),
		BotID:   strings.TrimSpace(out.BotID),
		URL:     strings.TrimSpace(out.URL),
		Team:    strings.TrimSpace(out.Team),
		User:    strings.TrimSpace(out.User),
		IsOwner: out.IsOwner,
	}, nil
}

func (api *slackAPI) userIdentity(ctx context.Context, userID string) (slackUserIdentity, error) {
	if api == nil {
		return slackUserIdentity{}, fmt.Errorf("slack api is not initialized")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return slackUserIdentity{}, fmt.Errorf("slack user id is required")
	}
	body, status, _, err := api.postAuthForm(ctx, api.botToken, "/users.info", url.Values{
		"user": []string{userID},
	})
	if err != nil {
		return slackUserIdentity{}, err
	}
	if status < 200 || status >= 300 {
		return slackUserIdentity{}, fmt.Errorf("slack users.info http %d", status)
	}
	var out slackUserInfoResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return slackUserIdentity{}, err
	}
	if !out.OK {
		code := strings.TrimSpace(out.Error)
		if code == "" {
			code = "unknown_error"
		}
		// In shared-channel or externally federated cases, Slack may emit a valid
		// user id in events but users.info cannot resolve profile fields.
		// Keep ingress usable by falling back to user id for identity fields.
		if code == "user_not_found" || code == "user_not_visible" {
			return slackUserIdentity{
				UserID:      userID,
				Username:    userID,
				DisplayName: userID,
			}, nil
		}
		return slackUserIdentity{}, fmt.Errorf("slack users.info failed: %s", code)
	}

	resolvedUserID := strings.TrimSpace(out.User.ID)
	if resolvedUserID == "" {
		resolvedUserID = userID
	}

	username := strings.TrimSpace(out.User.Name)
	if username == "" {
		username = resolvedUserID
	}
	displayName := strings.TrimSpace(out.User.Profile.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(out.User.Profile.RealName)
	}
	if displayName == "" {
		displayName = username
	}
	if username == "" || displayName == "" {
		return slackUserIdentity{}, fmt.Errorf("slack users.info returned incomplete identity")
	}
	return slackUserIdentity{
		UserID:      resolvedUserID,
		Username:    username,
		DisplayName: displayName,
	}, nil
}

func (api *slackAPI) listEmojiNames(ctx context.Context) ([]string, error) {
	if api == nil {
		return nil, fmt.Errorf("slack api is not initialized")
	}
	body, status, _, err := api.postAuthForm(ctx, api.botToken, "/emoji.list", url.Values{
		"include_categories": []string{"true"},
	})
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("slack emoji.list http %d", status)
	}

	var out slackEmojiListResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if !out.OK {
		code := strings.TrimSpace(out.Error)
		if code == "" {
			code = "unknown_error"
		}
		return nil, fmt.Errorf("slack emoji.list failed: %s", code)
	}

	seen := make(map[string]bool)
	for rawName := range out.Emoji {
		addSlackEmojiName(seen, rawName)
	}
	collectSlackEmojiNamesFromCategories(out.Categories, seen)
	if len(seen) == 0 {
		return nil, fmt.Errorf("slack emoji.list returned no emoji names")
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func collectSlackEmojiNamesFromCategories(raw json.RawMessage, out map[string]bool) {
	if len(raw) == 0 || out == nil {
		return
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return
	}
	collectSlackEmojiNames(decoded, out, false)
}

func collectSlackEmojiNames(v any, out map[string]bool, allowScalarString bool) {
	if out == nil {
		return
	}
	switch typed := v.(type) {
	case map[string]any:
		for rawKey, item := range typed {
			key := strings.ToLower(strings.TrimSpace(rawKey))
			switch key {
			case "name", "emoji_name", "short_name":
				if s, ok := item.(string); ok {
					addSlackEmojiName(out, s)
				}
			case "emoji_names", "short_names", "aliases", "emoji", "emojis":
				collectSlackEmojiNames(item, out, true)
			default:
				collectSlackEmojiNames(item, out, false)
			}
		}
	case []any:
		for _, item := range typed {
			collectSlackEmojiNames(item, out, allowScalarString)
		}
	case string:
		if allowScalarString {
			addSlackEmojiName(out, typed)
		}
	}
}

func addSlackEmojiName(out map[string]bool, raw string) {
	if out == nil {
		return
	}
	name := strings.TrimSpace(raw)
	if strings.HasPrefix(name, ":") && strings.HasSuffix(name, ":") && len(name) >= 2 {
		name = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(name, ":"), ":"))
	}
	if name == "" {
		return
	}
	if !slackEmojiNameRegexp.MatchString(name) {
		return
	}
	out[strings.ToLower(name)] = true
}

type slackOpenConnectionResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	URL   string `json:"url,omitempty"`
}

type slackReactionResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type slackGetUploadURLExternalResponse struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	UploadURL string `json:"upload_url,omitempty"`
	FileID    string `json:"file_id,omitempty"`
}

type slackCompleteUploadExternalResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (api *slackAPI) openSocketURL(ctx context.Context) (string, error) {
	if api == nil {
		return "", fmt.Errorf("slack api is not initialized")
	}
	body, status, _, err := api.postAuthJSON(ctx, api.appToken, "/apps.connections.open", nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("slack apps.connections.open http %d", status)
	}
	var out slackOpenConnectionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if !out.OK {
		code := strings.TrimSpace(out.Error)
		if code == "" {
			code = "unknown_error"
		}
		return "", fmt.Errorf("slack apps.connections.open failed: %s", code)
	}
	url := strings.TrimSpace(out.URL)
	if url == "" {
		return "", fmt.Errorf("slack apps.connections.open returned empty url")
	}
	return url, nil
}

func (api *slackAPI) connectSocket(ctx context.Context) (*websocket.Conn, error) {
	url, err := api.openSocketURL(ctx)
	if err != nil {
		return nil, err
	}
	dialer := *websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (api *slackAPI) postMessage(ctx context.Context, channelID, text, threadTS string) error {
	client := slackclient.New(api.http, api.baseURL, api.botToken)
	return client.PostMessage(ctx, channelID, text, threadTS)
}

func (api *slackAPI) addReaction(ctx context.Context, channelID, messageTS, emoji string) error {
	if api == nil {
		return fmt.Errorf("slack api is not initialized")
	}
	channelID = strings.TrimSpace(channelID)
	messageTS = strings.TrimSpace(messageTS)
	emoji = strings.TrimSpace(emoji)
	if channelID == "" {
		return fmt.Errorf("channel_id is required")
	}
	if messageTS == "" {
		return fmt.Errorf("message_ts is required")
	}
	if emoji == "" {
		return fmt.Errorf("emoji is required")
	}
	body, status, _, err := api.postAuthJSON(ctx, api.botToken, "/reactions.add", map[string]any{
		"channel":   channelID,
		"timestamp": messageTS,
		"name":      emoji,
	})
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("slack reactions.add http %d", status)
	}
	var out slackReactionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return err
	}
	if !out.OK {
		code := strings.TrimSpace(out.Error)
		if code == "" {
			code = "unknown_error"
		}
		if code == "already_reacted" {
			return nil
		}
		return fmt.Errorf("slack reactions.add failed: %s", code)
	}
	return nil
}

func (api *slackAPI) uploadFile(ctx context.Context, channelID, threadTS, filePath, filename, title, initialComment string) error {
	if api == nil || api.http == nil {
		return fmt.Errorf("slack api is not initialized")
	}
	channelID = strings.TrimSpace(channelID)
	threadTS = strings.TrimSpace(threadTS)
	filePath = strings.TrimSpace(filePath)
	filename = strings.TrimSpace(filename)
	title = strings.TrimSpace(title)
	initialComment = strings.TrimSpace(initialComment)
	if channelID == "" {
		return fmt.Errorf("channel_id is required")
	}
	if filePath == "" {
		return fmt.Errorf("file path is required")
	}
	if filename == "" {
		filename = filepath.Base(filePath)
	}
	if title == "" {
		title = filename
	}
	st, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("file path is a directory: %s", filePath)
	}
	uploadURL, fileID, err := api.getUploadURLExternal(ctx, filename, st.Size())
	if err != nil {
		return err
	}
	if err := api.uploadFileToExternalURL(ctx, uploadURL, filePath, st.Size()); err != nil {
		return err
	}
	if err := api.completeUploadExternal(ctx, channelID, threadTS, fileID, title, initialComment); err != nil {
		return err
	}
	return nil
}

func (api *slackAPI) getUploadURLExternal(ctx context.Context, filename string, length int64) (string, string, error) {
	if api == nil || api.http == nil {
		return "", "", fmt.Errorf("slack api is not initialized")
	}
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "", "", fmt.Errorf("filename is required")
	}
	if length < 0 {
		return "", "", fmt.Errorf("file length is invalid")
	}
	body, status, _, err := api.postAuthJSON(ctx, api.botToken, "/files.getUploadURLExternal", map[string]any{
		"filename": filename,
		"length":   length,
	})
	if err != nil {
		return "", "", err
	}
	if status < 200 || status >= 300 {
		return "", "", fmt.Errorf("slack files.getUploadURLExternal http %d", status)
	}
	var out slackGetUploadURLExternalResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", err
	}
	if !out.OK {
		code := strings.TrimSpace(out.Error)
		if code == "" {
			code = "unknown_error"
		}
		return "", "", fmt.Errorf("slack files.getUploadURLExternal failed: %s", code)
	}
	uploadURL := strings.TrimSpace(out.UploadURL)
	fileID := strings.TrimSpace(out.FileID)
	if uploadURL == "" || fileID == "" {
		return "", "", fmt.Errorf("slack files.getUploadURLExternal returned incomplete payload")
	}
	return uploadURL, fileID, nil
}

func (api *slackAPI) uploadFileToExternalURL(ctx context.Context, uploadURL, filePath string, contentLength int64) error {
	if api == nil || api.http == nil {
		return fmt.Errorf("slack api is not initialized")
	}
	uploadURL = strings.TrimSpace(uploadURL)
	filePath = strings.TrimSpace(filePath)
	if uploadURL == "" {
		return fmt.Errorf("upload url is required")
	}
	if filePath == "" {
		return fmt.Errorf("file path is required")
	}

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, f)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if contentLength >= 0 {
		req.ContentLength = contentLength
	}
	resp, err := api.http.Do(req)
	if err != nil {
		return err
	}
	raw, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			return fmt.Errorf("slack external file upload http %d", resp.StatusCode)
		}
		return fmt.Errorf("slack external file upload http %d: %s", resp.StatusCode, msg)
	}
	return nil
}

func (api *slackAPI) completeUploadExternal(ctx context.Context, channelID, threadTS, fileID, title, initialComment string) error {
	if api == nil || api.http == nil {
		return fmt.Errorf("slack api is not initialized")
	}
	channelID = strings.TrimSpace(channelID)
	threadTS = strings.TrimSpace(threadTS)
	fileID = strings.TrimSpace(fileID)
	title = strings.TrimSpace(title)
	initialComment = strings.TrimSpace(initialComment)
	if channelID == "" {
		return fmt.Errorf("channel_id is required")
	}
	if fileID == "" {
		return fmt.Errorf("file_id is required")
	}
	if title == "" {
		title = "file"
	}

	payload := map[string]any{
		"channel_id": channelID,
		"files": []map[string]string{
			{
				"id":    fileID,
				"title": title,
			},
		},
	}
	if threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	if initialComment != "" {
		payload["initial_comment"] = initialComment
	}

	body, status, _, err := api.postAuthJSON(ctx, api.botToken, "/files.completeUploadExternal", payload)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("slack files.completeUploadExternal http %d", status)
	}
	var out slackCompleteUploadExternalResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return err
	}
	if !out.OK {
		code := strings.TrimSpace(out.Error)
		if code == "" {
			code = "unknown_error"
		}
		return fmt.Errorf("slack files.completeUploadExternal failed: %s", code)
	}
	return nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (api *slackAPI) postAuthJSON(ctx context.Context, token, path string, payload any) ([]byte, int, http.Header, error) {
	if api == nil || api.http == nil {
		return nil, 0, nil, fmt.Errorf("slack api is not initialized")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, 0, nil, fmt.Errorf("slack token is required")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, 0, nil, fmt.Errorf("slack api path is required")
	}

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api.baseURL+path, body)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := api.http.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	raw, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, resp.StatusCode, resp.Header, readErr
	}
	return raw, resp.StatusCode, resp.Header, nil
}

func (api *slackAPI) postAuthForm(ctx context.Context, token, path string, payload url.Values) ([]byte, int, http.Header, error) {
	if api == nil || api.http == nil {
		return nil, 0, nil, fmt.Errorf("slack api is not initialized")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, 0, nil, fmt.Errorf("slack token is required")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, 0, nil, fmt.Errorf("slack api path is required")
	}
	if payload == nil {
		payload = url.Values{}
	}
	body := strings.NewReader(payload.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api.baseURL+path, body)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")

	resp, err := api.http.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	raw, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, resp.StatusCode, resp.Header, readErr
	}
	return raw, resp.StatusCode, resp.Header, nil
}
