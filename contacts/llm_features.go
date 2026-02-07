package contacts

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/jsonutil"
	"github.com/quailyquaily/mistermorph/llm"
)

type LLMFeatureExtractor struct {
	Client llm.Client
	Model  string
}

func NewLLMFeatureExtractor(client llm.Client, model string) *LLMFeatureExtractor {
	return &LLMFeatureExtractor{
		Client: client,
		Model:  strings.TrimSpace(model),
	}
}

func (x *LLMFeatureExtractor) EvaluateCandidateFeatures(ctx context.Context, contact Contact, candidates []ShareCandidate) (map[string]CandidateFeature, error) {
	if x == nil || x.Client == nil {
		return nil, fmt.Errorf("nil llm feature extractor")
	}
	if strings.TrimSpace(x.Model) == "" {
		return nil, fmt.Errorf("empty llm model")
	}
	payload := buildLLMFeaturePayload(contact, candidates)
	input, _ := json.Marshal(payload)

	systemPrompt := strings.Join([]string{
		"You evaluate share candidates for one contact.",
		"Return JSON only, no markdown, no prose.",
		"Output schema: {\"ranked_candidates\":[{\"item_id\":\"...\",\"overlap_semantic\":0..1,\"explicit_history_links\":[...],\"confidence\":0..1}]}",
		"Use explicit_history_links only when linkage is explicit.",
		"Do not include candidates that are not in input.",
	}, " ")

	res, err := x.Client.Chat(ctx, llm.Request{
		Model:     x.Model,
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(input)},
		},
		Parameters: map[string]any{
			"temperature": 0,
			"max_tokens":  1000,
		},
	})
	if err != nil {
		return nil, err
	}

	raw := strings.TrimSpace(res.Text)
	if raw == "" {
		return nil, fmt.Errorf("empty llm feature response")
	}

	var out llmFeatureResponse
	if err := jsonutil.DecodeWithFallback(raw, &out); err != nil {
		return nil, fmt.Errorf("decode llm feature response: %w", err)
	}

	allowed := map[string]bool{}
	for _, item := range candidates {
		id := strings.TrimSpace(item.ItemID)
		if id != "" {
			allowed[id] = true
		}
	}
	features := map[string]CandidateFeature{}
	for _, item := range out.RankedCandidates {
		itemID := strings.TrimSpace(item.ItemID)
		if itemID == "" || !allowed[itemID] {
			continue
		}
		features[itemID] = CandidateFeature{
			HasOverlapSemantic:   true,
			OverlapSemantic:      clamp(item.OverlapSemantic, 0, 1),
			ExplicitHistoryLinks: normalizeStringSlice(item.ExplicitHistoryLinks),
			Confidence:           clamp(item.Confidence, 0, 1),
		}
	}
	return features, nil
}

func (x *LLMFeatureExtractor) EvaluateContactPreferences(ctx context.Context, contact Contact, candidates []ShareCandidate) (PreferenceFeatures, error) {
	if x == nil || x.Client == nil {
		return PreferenceFeatures{}, fmt.Errorf("nil llm feature extractor")
	}
	if strings.TrimSpace(x.Model) == "" {
		return PreferenceFeatures{}, fmt.Errorf("empty llm model")
	}
	payload := buildLLMPreferencePayload(contact, candidates)
	input, _ := json.Marshal(payload)

	systemPrompt := strings.Join([]string{
		"You extract stable preference signals for one contact.",
		"Return JSON only, no markdown, no prose.",
		"Output schema: {\"topic_affinity\":[{\"topic\":\"...\",\"score\":0..1}],\"persona_brief\":\"...\",\"persona_traits\":{\"warm\":0.7},\"confidence\":0..1}",
		"Keep at most 12 topics in topic_affinity.",
		"Lower confidence when evidence is weak.",
	}, " ")

	res, err := x.Client.Chat(ctx, llm.Request{
		Model:     x.Model,
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(input)},
		},
		Parameters: map[string]any{
			"temperature": 0,
			"max_tokens":  1000,
		},
	})
	if err != nil {
		return PreferenceFeatures{}, err
	}

	raw := strings.TrimSpace(res.Text)
	if raw == "" {
		return PreferenceFeatures{}, fmt.Errorf("empty llm preference response")
	}

	var out llmPreferenceResponse
	if err := jsonutil.DecodeWithFallback(raw, &out); err != nil {
		return PreferenceFeatures{}, fmt.Errorf("decode llm preference response: %w", err)
	}

	topicAffinity := map[string]float64{}
	type rankedTopic struct {
		topic string
		score float64
	}
	topics := make([]rankedTopic, 0, len(out.TopicAffinity))
	for _, item := range out.TopicAffinity {
		topic := strings.ToLower(strings.TrimSpace(item.Topic))
		if topic == "" {
			continue
		}
		topics = append(topics, rankedTopic{topic: topic, score: clamp(item.Score, 0, 1)})
	}
	sort.Slice(topics, func(i, j int) bool {
		if topics[i].score == topics[j].score {
			return topics[i].topic < topics[j].topic
		}
		return topics[i].score > topics[j].score
	})
	if len(topics) > 12 {
		topics = topics[:12]
	}
	for _, item := range topics {
		if prev, exists := topicAffinity[item.topic]; !exists || item.score > prev {
			topicAffinity[item.topic] = item.score
		}
	}

	personaTraits := map[string]float64{}
	for rawTrait, rawValue := range out.PersonaTraits {
		trait := strings.ToLower(strings.TrimSpace(rawTrait))
		if trait == "" {
			continue
		}
		personaTraits[trait] = clamp(rawValue, 0, 1)
	}

	return PreferenceFeatures{
		TopicAffinity: topicAffinity,
		PersonaBrief:  strings.TrimSpace(out.PersonaBrief),
		PersonaTraits: personaTraits,
		Confidence:    clamp(out.Confidence, 0, 1),
	}, nil
}

type llmFeatureResponse struct {
	RankedCandidates []llmFeatureCandidate `json:"ranked_candidates"`
}

type llmFeatureCandidate struct {
	ItemID               string   `json:"item_id"`
	OverlapSemantic      float64  `json:"overlap_semantic"`
	ExplicitHistoryLinks []string `json:"explicit_history_links"`
	Confidence           float64  `json:"confidence"`
}

type llmPreferenceResponse struct {
	TopicAffinity []llmPreferenceTopic `json:"topic_affinity"`
	PersonaBrief  string               `json:"persona_brief"`
	PersonaTraits map[string]float64   `json:"persona_traits"`
	Confidence    float64              `json:"confidence"`
}

type llmPreferenceTopic struct {
	Topic string  `json:"topic"`
	Score float64 `json:"score"`
}

func buildLLMFeaturePayload(contact Contact, candidates []ShareCandidate) map[string]any {
	candidatesPayload := make([]map[string]any, 0, len(candidates))
	for _, item := range candidates {
		candidatesPayload = append(candidatesPayload, map[string]any{
			"item_id":            item.ItemID,
			"topic":              item.Topic,
			"topics":             item.Topics,
			"content_type":       item.ContentType,
			"sensitivity_level":  item.SensitivityLevel,
			"depth_hint":         item.DepthHint,
			"created_at":         item.CreatedAt,
			"linked_history_ids": item.LinkedHistoryIDs,
		})
	}

	return map[string]any{
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
		"candidates": candidatesPayload,
		"rules": []string{
			"overlap_semantic must be within [0,1]",
			"confidence must be within [0,1]",
			"explicit_history_links should be empty unless explicit",
			"no candidate id outside the provided list",
		},
	}
}

func buildLLMPreferencePayload(contact Contact, candidates []ShareCandidate) map[string]any {
	candidatesPayload := make([]map[string]any, 0, len(candidates))
	for _, item := range candidates {
		candidatesPayload = append(candidatesPayload, map[string]any{
			"item_id":      item.ItemID,
			"topic":        item.Topic,
			"topics":       item.Topics,
			"content_type": item.ContentType,
			"created_at":   item.CreatedAt,
		})
	}

	return map[string]any{
		"contact_profile_snapshot": map[string]any{
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
		"memory_summaries": candidatesPayload,
		"rules": []string{
			"topic_affinity scores must be in [0,1]",
			"persona_traits values must be in [0,1]",
			"confidence must be in [0,1]",
			"topic_affinity max size is 12",
		},
	}
}
