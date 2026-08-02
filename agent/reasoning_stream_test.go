package agent

import "testing"

func TestMainRequestCarriesReasoningDetails(t *testing.T) {
	request := (&Engine{}).mainRequest(&engineLoopState{
		reasoningDetails: true,
	}, nil)

	if !request.ReasoningDetails {
		t.Fatal("request.ReasoningDetails = false, want true")
	}
}
