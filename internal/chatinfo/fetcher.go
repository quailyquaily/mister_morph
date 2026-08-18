package chatinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	refid "github.com/quailyquaily/mistermorph/internal/entryutil/refid"
	"github.com/quailyquaily/mistermorph/internal/larkapi"
)

const (
	defaultTelegramBaseURL = "https://api.telegram.org"
	defaultSlackBaseURL    = "https://slack.com/api"
	defaultLineBaseURL     = "https://api.line.me"
	defaultLarkBaseURL     = "https://open.feishu.cn/open-apis"
)

type FetcherOptions struct {
	HTTPClient       *http.Client
	TelegramBotToken string
	TelegramBaseURL  string
	SlackBotToken    string
	SlackBaseURL     string
	LineChannelToken string
	LineBaseURL      string
	LarkAppID        string
	LarkAppSecret    string
	LarkBaseURL      string
}

type FetcherOptionsReader interface {
	GetString(string) string
}

func FetcherOptionsFromReader(reader FetcherOptionsReader) FetcherOptions {
	if reader == nil {
		return FetcherOptions{}
	}
	return FetcherOptions{
		TelegramBotToken: strings.TrimSpace(reader.GetString("telegram.bot_token")),
		SlackBotToken:    strings.TrimSpace(reader.GetString("slack.bot_token")),
		SlackBaseURL:     strings.TrimSpace(reader.GetString("slack.base_url")),
		LineChannelToken: strings.TrimSpace(reader.GetString("line.channel_access_token")),
		LineBaseURL:      strings.TrimSpace(reader.GetString("line.base_url")),
		LarkAppID:        strings.TrimSpace(reader.GetString("lark.app_id")),
		LarkAppSecret:    strings.TrimSpace(reader.GetString("lark.app_secret")),
		LarkBaseURL:      strings.TrimSpace(reader.GetString("lark.base_url")),
	}
}

type Fetcher struct {
	client           *http.Client
	telegramBotToken string
	telegramBaseURL  string
	slackBotToken    string
	slackBaseURL     string
	lineChannelToken string
	lineBaseURL      string
	larkBaseURL      string
	larkTokenClient  *larkapi.TenantTokenClient
}

func NewFetcher(opts FetcherOptions) *Fetcher {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	larkBaseURL := baseURLOrDefault(opts.LarkBaseURL, defaultLarkBaseURL)
	return &Fetcher{
		client:           client,
		telegramBotToken: strings.TrimSpace(opts.TelegramBotToken),
		telegramBaseURL:  baseURLOrDefault(opts.TelegramBaseURL, defaultTelegramBaseURL),
		slackBotToken:    strings.TrimSpace(opts.SlackBotToken),
		slackBaseURL:     baseURLOrDefault(opts.SlackBaseURL, defaultSlackBaseURL),
		lineChannelToken: strings.TrimSpace(opts.LineChannelToken),
		lineBaseURL:      baseURLOrDefault(opts.LineBaseURL, defaultLineBaseURL),
		larkBaseURL:      larkBaseURL,
		larkTokenClient:  larkapi.NewTenantTokenClient(client, larkBaseURL, opts.LarkAppID, opts.LarkAppSecret),
	}
}

func (f *Fetcher) RefreshChatInfo(ctx context.Context, chatID string) (Info, error) {
	chatID, err := NormalizeChatID(chatID)
	if err != nil {
		return Info{}, err
	}
	protocol, _, _ := refid.Parse(chatID)
	switch protocol {
	case "tg":
		return f.fetchTelegram(ctx, chatID)
	case "slack":
		return f.fetchSlack(ctx, chatID)
	case "line":
		return f.fetchLine(ctx, chatID)
	case "lark":
		return f.fetchLark(ctx, chatID)
	default:
		return Info{}, fmt.Errorf("unsupported chat_id: %s", chatID)
	}
}

func (f *Fetcher) fetchTelegram(ctx context.Context, chatID string) (Info, error) {
	if f == nil || f.telegramBotToken == "" {
		return Info{}, fmt.Errorf("telegram bot token is required")
	}
	numericChatID, _, err := refid.ParseTelegramChatIDHint(chatID)
	if err != nil {
		return Info{}, err
	}
	reqURL, err := url.Parse(joinURL(f.telegramBaseURL, "/bot"+f.telegramBotToken+"/getChat"))
	if err != nil {
		return Info{}, err
	}
	q := reqURL.Query()
	q.Set("chat_id", strconv.FormatInt(numericChatID, 10))
	reqURL.RawQuery = q.Encode()
	var resp struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			ID        int64  `json:"id"`
			Type      string `json:"type"`
			Title     string `json:"title"`
			Username  string `json:"username"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		} `json:"result"`
	}
	if err := f.doJSON(ctx, http.MethodGet, reqURL.String(), "", nil, &resp); err != nil {
		return Info{}, err
	}
	if !resp.OK {
		return Info{}, fmt.Errorf("telegram getChat failed: %s", strings.TrimSpace(resp.Description))
	}
	name := firstNonEmpty(resp.Result.Title, resp.Result.Username, strings.TrimSpace(strings.Join([]string{resp.Result.FirstName, resp.Result.LastName}, " ")))
	return Info{
		ChatID:   chatID,
		Platform: "telegram",
		Type:     strings.TrimSpace(resp.Result.Type),
		Name:     name,
	}, nil
}

func (f *Fetcher) fetchSlack(ctx context.Context, chatID string) (Info, error) {
	if f == nil || f.slackBotToken == "" {
		return Info{}, fmt.Errorf("slack bot token is required")
	}
	_, channelID, _, err := refid.ParseSlackChatIDHint(chatID)
	if err != nil {
		return Info{}, err
	}
	reqURL, err := url.Parse(joinURL(f.slackBaseURL, "/conversations.info"))
	if err != nil {
		return Info{}, err
	}
	q := reqURL.Query()
	q.Set("channel", channelID)
	reqURL.RawQuery = q.Encode()
	var resp struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Channel struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			IsChannel bool   `json:"is_channel"`
			IsGroup   bool   `json:"is_group"`
			IsIM      bool   `json:"is_im"`
			IsMPIM    bool   `json:"is_mpim"`
		} `json:"channel"`
	}
	if err := f.doJSON(ctx, http.MethodGet, reqURL.String(), "Bearer "+f.slackBotToken, nil, &resp); err != nil {
		return Info{}, err
	}
	if !resp.OK {
		return Info{}, fmt.Errorf("slack conversations.info failed: %s", strings.TrimSpace(resp.Error))
	}
	return Info{
		ChatID:   chatID,
		Platform: "slack",
		Type:     slackChatType(resp.Channel.IsIM, resp.Channel.IsMPIM, resp.Channel.IsGroup),
		Name:     strings.TrimSpace(resp.Channel.Name),
	}, nil
}

func (f *Fetcher) fetchLine(ctx context.Context, chatID string) (Info, error) {
	if f == nil || f.lineChannelToken == "" {
		return Info{}, fmt.Errorf("line channel access token is required")
	}
	lineID, _, err := refid.ParseLineChatIDHint(chatID)
	if err != nil {
		return Info{}, err
	}
	if strings.HasPrefix(strings.ToUpper(lineID), "R") {
		return Info{}, fmt.Errorf("line room summary is not supported: %s", chatID)
	}
	if !strings.HasPrefix(strings.ToUpper(lineID), "C") {
		return Info{}, fmt.Errorf("line group chat_id is required: %s", chatID)
	}
	var resp struct {
		GroupID   string `json:"groupId"`
		GroupName string `json:"groupName"`
	}
	reqURL := joinURL(f.lineBaseURL, "/v2/bot/group/"+url.PathEscape(lineID)+"/summary")
	if err := f.doJSON(ctx, http.MethodGet, reqURL, "Bearer "+f.lineChannelToken, nil, &resp); err != nil {
		return Info{}, err
	}
	return Info{
		ChatID:   chatID,
		Platform: "line",
		Type:     "group",
		Name:     strings.TrimSpace(resp.GroupName),
	}, nil
}

func (f *Fetcher) fetchLark(ctx context.Context, chatID string) (Info, error) {
	if f == nil || f.larkTokenClient == nil {
		return Info{}, fmt.Errorf("lark token client is required")
	}
	larkID, _, err := refid.ParseLarkChatIDHint(chatID)
	if err != nil {
		return Info{}, err
	}
	token, err := f.larkTokenClient.Token(ctx)
	if err != nil {
		return Info{}, err
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ChatID   string `json:"chat_id"`
			Name     string `json:"name"`
			ChatType string `json:"chat_type"`
		} `json:"data"`
	}
	reqURL := joinURL(f.larkBaseURL, "/im/v1/chats/"+url.PathEscape(larkID))
	if err := f.doJSON(ctx, http.MethodGet, reqURL, "Bearer "+token, nil, &resp); err != nil {
		return Info{}, err
	}
	if resp.Code != 0 {
		return Info{}, fmt.Errorf("lark chat get failed: %s", strings.TrimSpace(resp.Msg))
	}
	return Info{
		ChatID:   chatID,
		Platform: "lark",
		Type:     strings.TrimSpace(resp.Data.ChatType),
		Name:     strings.TrimSpace(resp.Data.Name),
	}, nil
}

func (f *Fetcher) doJSON(ctx context.Context, method string, reqURL string, auth string, body []byte, out any) error {
	if f == nil || f.client == nil {
		return fmt.Errorf("http client is required")
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth = strings.TrimSpace(auth); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("chat profile fetch failed: %s", resp.Status)
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(out); err != nil {
		return err
	}
	return nil
}

func baseURLOrDefault(raw string, fallback string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return fallback
	}
	return raw
}

func joinURL(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func slackChatType(isIM bool, isMPIM bool, isGroup bool) string {
	switch {
	case isIM:
		return "im"
	case isMPIM:
		return "mpim"
	case isGroup:
		return "private_channel"
	default:
		return "channel"
	}
}
