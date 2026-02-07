package contacts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/jsonutil"
	"github.com/quailyquaily/mistermorph/llm"
)

type LLMNicknameGenerator struct {
	Client llm.Client
	Model  string
}

func NewLLMNicknameGenerator(client llm.Client, model string) *LLMNicknameGenerator {
	return &LLMNicknameGenerator{
		Client: client,
		Model:  strings.TrimSpace(model),
	}
}

func (x *LLMNicknameGenerator) SuggestNickname(ctx context.Context, contact Contact) (string, float64, error) {
	if x == nil || x.Client == nil {
		return "", 0, fmt.Errorf("nil llm nickname generator")
	}
	if strings.TrimSpace(x.Model) == "" {
		return "", 0, fmt.Errorf("empty llm model")
	}
	payload := map[string]any{
		"contact_profile": map[string]any{
			"contact_id":          contact.ContactID,
			"contact_nickname":    contact.ContactNickname,
			"persona_brief":       contact.PersonaBrief,
			"persona_traits":      contact.PersonaTraits,
			"kind":                contact.Kind,
			"subject_id":          contact.SubjectID,
			"node_id":             contact.NodeID,
			"peer_id":             contact.PeerID,
			"understanding_depth": contact.UnderstandingDepth,
			"topic_weights":       contact.TopicWeights,
			"reciprocity_norm":    contact.ReciprocityNorm,
			"trust_state":         contact.TrustState,
		},
		"rules": []string{
			"Return JSON only.",
			"Output schema: {\"nickname\":\"...\",\"confidence\":0..1,\"reason\":\"...\"}.",
			"nickname must be short and stable (max 24 chars).",
			"Avoid sensitive or offensive content.",
		},
	}
	input, _ := json.Marshal(payload)
	system := strings.Join([]string{
		"You generate one concise nickname for a contact profile.",
		"Return JSON only, no markdown.",
	}, " ")
	res, err := x.Client.Chat(ctx, llm.Request{
		Model:     x.Model,
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: string(input)},
		},
		Parameters: map[string]any{
			"temperature": 0,
			"max_tokens":  300,
		},
	})
	if err != nil {
		return "", 0, err
	}
	raw := strings.TrimSpace(res.Text)
	if raw == "" {
		return "", 0, fmt.Errorf("empty llm nickname response")
	}
	var out llmNicknameResponse
	if err := jsonutil.DecodeWithFallback(raw, &out); err != nil {
		return "", 0, fmt.Errorf("decode llm nickname response: %w", err)
	}
	nickname := strings.TrimSpace(out.Nickname)
	if len([]rune(nickname)) > 24 {
		nickname = string([]rune(nickname)[:24])
	}
	nickname = strings.TrimSpace(nickname)
	return nickname, clamp(out.Confidence, 0, 1), nil
}

type llmNicknameResponse struct {
	Nickname   string  `json:"nickname"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}
