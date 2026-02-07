package contacts

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	maxTopicWeights   = 16
	minTopicWeight    = 0.08
	topicScoreDecimals = 4
)

type weightedTopic struct {
	Topic string
	Score float64
}

func normalizeTopicWeightsMap(weights map[string]float64) map[string]float64 {
	if len(weights) == 0 {
		return nil
	}
	items := make([]weightedTopic, 0, len(weights))
	for rawTopic, rawScore := range weights {
		topic := canonicalTopicKey(rawTopic)
		if topic == "" {
			continue
		}
		score := roundScore(clamp(rawScore, 0, 1))
		if score < minTopicWeight {
			continue
		}
		items = append(items, weightedTopic{Topic: topic, Score: score})
	}
	if len(items) == 0 {
		return nil
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			return items[i].Topic < items[j].Topic
		}
		return items[i].Score > items[j].Score
	})
	if len(items) > maxTopicWeights {
		items = items[:maxTopicWeights]
	}
	out := make(map[string]float64, len(items))
	for _, item := range items {
		// sorted items keep the first (highest score) when same topic appears.
		if _, exists := out[item.Topic]; exists {
			continue
		}
		out[item.Topic] = item.Score
	}
	return out
}

func canonicalTopicKey(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	prevSep := false
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSep = false
			continue
		}
		if b.Len() == 0 || prevSep {
			continue
		}
		b.WriteByte('_')
		prevSep = true
	}
	return strings.Trim(b.String(), "_")
}

func roundScore(v float64) float64 {
	p := math.Pow10(topicScoreDecimals)
	return math.Round(v*p) / p
}
