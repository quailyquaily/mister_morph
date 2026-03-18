package memoryruntime

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/memory"
)

func TestRecentSummaryItems(t *testing.T) {
	t.Parallel()

	t.Run("returns all items when length is within limit", func(t *testing.T) {
		t.Parallel()

		items := []memory.SummaryItem{
			{Created: "2026-03-09T00:00:01Z", Content: "first"},
			{Created: "2026-03-09T00:00:02Z", Content: "second"},
		}
		got := recentSummaryItems(items, 5)
		if len(got) != 2 {
			t.Fatalf("len(got) = %d, want 2", len(got))
		}
		if got[0].Content != "first" || got[1].Content != "second" {
			t.Fatalf("got contents = %#v, want first/second", got)
		}
	})

	t.Run("keeps only the most recent items and preserves order", func(t *testing.T) {
		t.Parallel()

		var items []memory.SummaryItem
		for i := 1; i <= 7; i++ {
			items = append(items, memory.SummaryItem{
				Created: fmt.Sprintf("2026-03-09T00:00:0%dZ", i),
				Content: fmt.Sprintf("item-%d", i),
			})
		}
		got := recentSummaryItems(items, 5)
		if len(got) != 5 {
			t.Fatalf("len(got) = %d, want 5", len(got))
		}
		for i, want := range []string{"item-3", "item-4", "item-5", "item-6", "item-7"} {
			if got[i].Content != want {
				t.Fatalf("got[%d].Content = %q, want %q", i, got[i].Content, want)
			}
		}
	})
}

func TestRenderMemoryDraftPrompts_LimitsExistingSummaryItemsToRecentFive(t *testing.T) {
	t.Parallel()

	var items []memory.SummaryItem
	for i := 1; i <= 7; i++ {
		items = append(items, memory.SummaryItem{
			Created: fmt.Sprintf("2026-03-09T00:00:0%dZ", i),
			Content: fmt.Sprintf("item-%d", i),
		})
	}

	_, userPrompt, err := renderMemoryDraftPrompts(
		memory.SessionContext{},
		nil,
		"say hi",
		"hi",
		memory.ShortTermContent{SummaryItems: items},
	)
	if err != nil {
		t.Fatalf("renderMemoryDraftPrompts() error = %v", err)
	}

	var payload struct {
		ExistingSummaryItems []memory.SummaryItem `json:"existing_summary_items"`
	}
	if err := json.Unmarshal([]byte(userPrompt), &payload); err != nil {
		t.Fatalf("Unmarshal(userPrompt) error = %v", err)
	}
	if len(payload.ExistingSummaryItems) != 5 {
		t.Fatalf("len(existing_summary_items) = %d, want 5", len(payload.ExistingSummaryItems))
	}
	for i, want := range []string{"item-3", "item-4", "item-5", "item-6", "item-7"} {
		if payload.ExistingSummaryItems[i].Content != want {
			t.Fatalf("existing_summary_items[%d].Content = %q, want %q", i, payload.ExistingSummaryItems[i].Content, want)
		}
	}
}

func TestRenderMemoryDraftPrompts_UsesContentOnlySummaryItemsContract(t *testing.T) {
	t.Parallel()

	systemPrompt, _, err := renderMemoryDraftPrompts(
		memory.SessionContext{},
		nil,
		"say hi",
		"hi",
		memory.ShortTermContent{},
	)
	if err != nil {
		t.Fatalf("renderMemoryDraftPrompts() error = %v", err)
	}
	if !strings.Contains(systemPrompt, "must be only the content sentence") {
		t.Fatalf("system prompt missing content-only summary contract: %q", systemPrompt)
	}
	if strings.Contains(systemPrompt, "- [Created](") {
		t.Fatalf("system prompt still includes created metadata example in summary_items: %q", systemPrompt)
	}
}

func TestEnforceLongTermPromotionRules(t *testing.T) {
	t.Parallel()

	promote := memory.PromoteDraft{
		GoalsProjects: []string{"remember goal", "extra"},
		KeyFacts: []memory.KVItem{
			{Title: "Project", Value: "mistermorph"},
		},
	}

	t.Run("drops promotion without explicit memory request", func(t *testing.T) {
		t.Parallel()

		got := EnforceLongTermPromotionRules(promote, nil, "just replying")
		if len(got.GoalsProjects) != 0 || len(got.KeyFacts) != 0 {
			t.Fatalf("got = %#v, want empty promote", got)
		}
	})

	t.Run("keeps only one promoted item with explicit request", func(t *testing.T) {
		t.Parallel()

		got := EnforceLongTermPromotionRules(promote, nil, "remember this for later")
		if len(got.GoalsProjects) != 1 || got.GoalsProjects[0] != "remember goal" {
			t.Fatalf("goals = %#v, want [\"remember goal\"]", got.GoalsProjects)
		}
		if len(got.KeyFacts) != 0 {
			t.Fatalf("key facts = %#v, want empty", got.KeyFacts)
		}
	})
}
