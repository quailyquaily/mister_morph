package llm

import "testing"

func TestUsageCostField(t *testing.T) {
	u := Usage{
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
		Cost:         0.05,
	}
	if u.Cost != 0.05 {
		t.Errorf("expected Cost 0.05, got %f", u.Cost)
	}
}

func TestUsageCostDefaultsToZero(t *testing.T) {
	u := Usage{}
	if u.Cost != 0 {
		t.Errorf("expected Cost to default to 0, got %f", u.Cost)
	}
}
