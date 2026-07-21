package llmutil

import (
	"hash/fnv"
	"strings"
)

// SelectRouteCandidate fixes a weighted route to one concrete primary for a run.
// The remaining candidates retain their original order as fallbacks.
func SelectRouteCandidate(route ResolvedRoute, selectionKey string) ResolvedRoute {
	if len(route.Candidates) == 0 {
		return route
	}
	weights := make([]int, len(route.Candidates))
	for i, candidate := range route.Candidates {
		weights[i] = candidate.Weight
	}
	primaryIndex := weightedIndex(selectionKey, weights)
	primary := route.Candidates[primaryIndex]

	fallbacks := make([]ResolvedFallback, 0, len(route.Candidates)-1+len(route.Fallbacks))
	for i, candidate := range route.Candidates {
		if i == primaryIndex {
			continue
		}
		fallbacks = append(fallbacks, ResolvedFallback{
			Profile:      candidate.Profile,
			Source:       candidate.Source,
			Values:       candidate.Values,
			ClientConfig: candidate.ClientConfig,
		})
	}
	fallbacks = append(fallbacks, route.Fallbacks...)

	route.Profile = primary.Profile
	route.Source = primary.Source
	route.Values = primary.Values
	route.ClientConfig = primary.ClientConfig
	route.Candidates = nil
	route.Fallbacks = fallbacks
	return route
}

func weightedIndex(selectionKey string, weights []int) int {
	totalWeight := 0
	for _, weight := range weights {
		if weight > 0 {
			totalWeight += weight
		}
	}
	if totalWeight <= 0 || strings.TrimSpace(selectionKey) == "" {
		return 0
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(selectionKey))
	target := int(hasher.Sum32() % uint32(totalWeight))
	acc := 0
	for i, weight := range weights {
		acc += weight
		if target < acc {
			return i
		}
	}
	return 0
}
