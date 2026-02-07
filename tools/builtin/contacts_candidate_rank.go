package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/llm"
)

type ContactsCandidateRankToolOptions struct {
	Enabled                      bool
	ContactsDir                  string
	DefaultLimit                 int
	DefaultFreshnessWindow       time.Duration
	DefaultMaxLinkedHistoryItems int
	DefaultHumanEnabled          bool
	DefaultHumanPublicSend       bool
	DefaultLLMProvider           string
	DefaultLLMEndpoint           string
	DefaultLLMAPIKey             string
	DefaultLLMModel              string
	DefaultLLMTimeout            time.Duration
}

type ContactsCandidateRankTool struct {
	opts ContactsCandidateRankToolOptions
}

func NewContactsCandidateRankTool(opts ContactsCandidateRankToolOptions) *ContactsCandidateRankTool {
	if opts.DefaultLimit <= 0 {
		opts.DefaultLimit = contacts.DefaultMaxTargetsPerTick
	}
	if opts.DefaultFreshnessWindow <= 0 {
		opts.DefaultFreshnessWindow = contacts.DefaultFreshnessWindow
	}
	if opts.DefaultMaxLinkedHistoryItems <= 0 {
		opts.DefaultMaxLinkedHistoryItems = 4
	}
	if opts.DefaultLLMTimeout <= 0 {
		opts.DefaultLLMTimeout = 30 * time.Second
	}
	return &ContactsCandidateRankTool{opts: opts}
}

func (t *ContactsCandidateRankTool) Name() string { return "contacts_candidate_rank" }

func (t *ContactsCandidateRankTool) Description() string {
	return "Ranks contacts against current candidate pool and returns top decisions (no sending, no state mutation)."
}

func (t *ContactsCandidateRankTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{
				"type":        "integer",
				"description": "Max ranked contacts to return (default 3).",
			},
			"freshness_window": map[string]any{
				"type":        "string",
				"description": "Freshness window duration, e.g. 72h.",
			},
			"freshness_window_hours": map[string]any{
				"type":        "number",
				"description": "Alternative freshness window in hours.",
			},
			"max_linked_history_items": map[string]any{
				"type":        "integer",
				"description": "Max linked history ids per decision.",
			},
			"human_enabled": map[string]any{
				"type":        "boolean",
				"description": "Enable human contacts in ranking.",
			},
			"human_public_send_enabled": map[string]any{
				"type":        "boolean",
				"description": "If false, rankings skip candidates targeting public human chats.",
			},
			"push_topic": map[string]any{
				"type":        "string",
				"description": "Optional topic to place in returned decisions.",
			},
		},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *ContactsCandidateRankTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || !t.opts.Enabled {
		return "", fmt.Errorf("contacts_candidate_rank tool is disabled")
	}
	contactsDir := pathutil.ExpandHomePath(strings.TrimSpace(t.opts.ContactsDir))
	if contactsDir == "" {
		return "", fmt.Errorf("contacts dir is not configured")
	}
	svc := contacts.NewService(contacts.NewFileStore(contactsDir))

	limit := parseIntDefault(params["limit"], t.opts.DefaultLimit)
	maxLinkedHistoryItems := parseIntDefault(params["max_linked_history_items"], t.opts.DefaultMaxLinkedHistoryItems)
	humanEnabled := parseBoolDefault(params["human_enabled"], t.opts.DefaultHumanEnabled)
	humanPublicEnabled := parseBoolDefault(params["human_public_send_enabled"], t.opts.DefaultHumanPublicSend)
	pushTopic, _ := params["push_topic"].(string)

	freshnessWindow := t.opts.DefaultFreshnessWindow
	if hours := parseFloatDefault(params["freshness_window_hours"], 0); hours > 0 {
		freshnessWindow = time.Duration(hours * float64(time.Hour))
	}
	if d, err := parseDuration(params["freshness_window"], freshnessWindow); err != nil {
		return "", fmt.Errorf("invalid freshness_window: %w", err)
	} else if d > 0 {
		freshnessWindow = d
	}

	client, model, err := t.newLLMClientForFeatures()
	if err != nil {
		return "", err
	}
	extractor := contacts.NewLLMFeatureExtractor(client, model)

	decisions, err := svc.RankCandidates(ctx, time.Now().UTC(), contacts.TickOptions{
		MaxTargets:            limit,
		FreshnessWindow:       freshnessWindow,
		PushTopic:             strings.TrimSpace(pushTopic),
		Send:                  false,
		EnableHumanContacts:   humanEnabled,
		EnableHumanSend:       false,
		EnableHumanPublicSend: humanPublicEnabled,
		MaxLinkedHistoryItems: maxLinkedHistoryItems,
		FeatureExtractor:      extractor,
	})
	if err != nil {
		return "", err
	}
	out, _ := json.MarshalIndent(map[string]any{
		"count":     len(decisions),
		"decisions": decisions,
	}, "", "  ")
	return string(out), nil
}

func (t *ContactsCandidateRankTool) newLLMClientForFeatures() (llm.Client, string, error) {
	provider := strings.TrimSpace(t.opts.DefaultLLMProvider)
	endpoint := strings.TrimSpace(t.opts.DefaultLLMEndpoint)
	apiKey := strings.TrimSpace(t.opts.DefaultLLMAPIKey)
	model := strings.TrimSpace(t.opts.DefaultLLMModel)
	if model == "" {
		return nil, "", fmt.Errorf("empty llm model")
	}

	timeout := t.opts.DefaultLLMTimeout
	client, err := llmutil.ClientFromConfig(llmconfig.ClientConfig{
		Provider:       provider,
		Endpoint:       endpoint,
		APIKey:         apiKey,
		Model:          model,
		RequestTimeout: timeout,
	})
	if err != nil {
		return nil, "", err
	}
	return client, model, nil
}
