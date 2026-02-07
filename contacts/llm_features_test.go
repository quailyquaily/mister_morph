package contacts

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
)

type stubLLMClient struct {
	reply string
	err   error
	req   llm.Request
}

func (s *stubLLMClient) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	s.req = req
	if s.err != nil {
		return llm.Result{}, s.err
	}
	return llm.Result{Text: s.reply}, nil
}

func TestLLMFeatureExtractorEvaluateCandidateFeatures(t *testing.T) {
	client := &stubLLMClient{
		reply: `{
		  "ranked_candidates": [
		    {
		      "item_id": "cand-1",
		      "overlap_semantic": 1.4,
		      "explicit_history_links": ["h1", "h2", "h1"],
		      "confidence": 0.82
		    },
		    {
		      "item_id": "cand-unknown",
		      "overlap_semantic": 0.9,
		      "explicit_history_links": ["h3"],
		      "confidence": 0.7
		    }
		  ]
		}`,
	}
	extractor := NewLLMFeatureExtractor(client, "gpt-5.2")
	features, err := extractor.EvaluateCandidateFeatures(context.Background(), Contact{
		ContactID: "maep:test",
	}, []ShareCandidate{
		{ItemID: "cand-1", Topic: "maep", ContentType: "text/plain", PayloadBase64: "aGVsbG8"},
		{ItemID: "cand-2", Topic: "ops", ContentType: "text/plain", PayloadBase64: "d29ybGQ"},
	})
	if err != nil {
		t.Fatalf("EvaluateCandidateFeatures() error = %v", err)
	}
	if !strings.Contains(strings.ToLower(client.req.Messages[0].Content), "return json only") {
		t.Fatalf("system prompt missing json-only guard")
	}
	got, ok := features["cand-1"]
	if !ok {
		t.Fatalf("missing features for cand-1")
	}
	if !got.HasOverlapSemantic {
		t.Fatalf("expected HasOverlapSemantic=true")
	}
	if got.OverlapSemantic != 1 {
		t.Fatalf("overlap clamp mismatch: got %v want 1", got.OverlapSemantic)
	}
	if len(got.ExplicitHistoryLinks) != 2 {
		t.Fatalf("explicit history links dedupe mismatch: got %v", got.ExplicitHistoryLinks)
	}
	if _, exists := features["cand-unknown"]; exists {
		t.Fatalf("unexpected unknown candidate in output: %v", features["cand-unknown"])
	}
}

func TestLLMFeatureExtractorEvaluateContactPreferences(t *testing.T) {
	topics := make([]string, 0, 14)
	for i := 0; i < 14; i++ {
		topics = append(topics, fmt.Sprintf(`{"topic":"t%d","score":%.2f}`, i, 1.0-float64(i)*0.03))
	}
	client := &stubLLMClient{
		reply: `{
		  "topic_affinity": [` + strings.Join(topics, ",") + `],
		  "persona_brief": "情感细腻，偶尔忧郁",
		  "persona_traits": {"Warm": 1.2, "Sensitive": 0.81, "": 0.4},
		  "confidence": 0.91
		}`,
	}
	extractor := NewLLMFeatureExtractor(client, "gpt-5.2")
	features, err := extractor.EvaluateContactPreferences(context.Background(), Contact{
		ContactID: "maep:test",
	}, []ShareCandidate{
		{ItemID: "cand-1", Topic: "maep", ContentType: "text/plain", PayloadBase64: "aGVsbG8"},
	})
	if err != nil {
		t.Fatalf("EvaluateContactPreferences() error = %v", err)
	}
	if !strings.Contains(strings.ToLower(client.req.Messages[0].Content), "return json only") {
		t.Fatalf("system prompt missing json-only guard")
	}
	if features.Confidence != 0.91 {
		t.Fatalf("confidence mismatch: got %.2f want 0.91", features.Confidence)
	}
	if len(features.TopicAffinity) != 12 {
		t.Fatalf("topic affinity limit mismatch: got %d want 12", len(features.TopicAffinity))
	}
	if features.PersonaBrief != "情感细腻，偶尔忧郁" {
		t.Fatalf("persona brief mismatch: got %q", features.PersonaBrief)
	}
	if features.PersonaTraits["warm"] != 1 {
		t.Fatalf("persona trait clamp mismatch: got %.2f want 1", features.PersonaTraits["warm"])
	}
	if features.PersonaTraits["sensitive"] != 0.81 {
		t.Fatalf("persona trait normalize mismatch: got %.2f want 0.81", features.PersonaTraits["sensitive"])
	}
}
