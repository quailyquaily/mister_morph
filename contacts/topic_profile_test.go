package contacts

import "testing"

func TestNormalizeTopicWeightsMapCanonicalizesAndPrunes(t *testing.T) {
	got := normalizeTopicWeightsMap(map[string]float64{
		"agent-ops / workflow":           0.16,
		"agent_ops+workflow":             0.8880799999999999,
		" healthy/light flavors (清爽) ": 0.07192,
		"soup recipes":                   0.09920000000000001,
	})

	if got["agent_ops_workflow"] != 0.8881 {
		t.Fatalf("agent_ops_workflow mismatch: got %.4f want 0.8881", got["agent_ops_workflow"])
	}
	if _, ok := got["healthy_light_flavors_清爽"]; ok {
		t.Fatalf("low-score topic should be pruned")
	}
	if _, ok := got["soup_recipes"]; !ok {
		t.Fatalf("soup_recipes should remain after canonicalization")
	}
	if len(got) != 2 {
		t.Fatalf("normalized topic count mismatch: got %d want 2", len(got))
	}
}
