package llmstats

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/spf13/viper"
)

const testCostEpsilon = 1e-9

func costAlmostEqual(a, b float64) bool {
	return math.Abs(a-b) < testCostEpsilon
}

type stubUsageClient struct{}

func (stubUsageClient) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	return llm.Result{
		Text: "ok",
		Usage: llm.Usage{
			InputTokens:  11,
			OutputTokens: 7,
			TotalTokens:  18,
			Cache: llm.UsageCache{
				CachedInputTokens:        5,
				CacheCreationInputTokens: 3,
				Details: map[string]int{
					"ephemeral_5m_input_tokens": 3,
				},
			},
			Cost: &llm.UsageCost{
				Currency:           "USD",
				Estimated:          true,
				Input:              0.01,
				CachedInput:        0.002,
				CacheCreationInput: 0.003,
				Output:             0.02,
				Total:              0.035,
			},
		},
		Duration: 250 * time.Millisecond,
	}, nil
}

type stubImageUsageClient struct{}

func (stubImageUsageClient) GenerateImage(ctx context.Context, req llm.ImageRequest) (llm.ImageResult, error) {
	return llm.ImageResult{
		Usage: llm.Usage{
			InputTokens:  17,
			OutputTokens: 3,
			TotalTokens:  20,
			Cache: llm.UsageCache{
				CachedInputTokens: 2,
				Details: map[string]int{
					"input_image_tokens": 9,
				},
			},
			Cost: &llm.UsageCost{
				Currency:  "USD",
				Estimated: true,
				Input:     0.03,
				Output:    0.04,
				Total:     0.07,
			},
		},
		Duration: 125 * time.Millisecond,
	}, nil
}

func (stubImageUsageClient) EditImage(ctx context.Context, req llm.ImageEditRequest) (llm.ImageResult, error) {
	return llm.ImageResult{
		Usage: llm.Usage{
			InputTokens:  19,
			OutputTokens: 5,
			TotalTokens:  24,
			Cost: &llm.UsageCost{
				Currency: "USD",
				Total:    0.11,
			},
		},
		Duration: 225 * time.Millisecond,
	}, nil
}

func TestUsageClientRecordsRequestMetadata(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := WrapClient(stubUsageClient{}, ClientOptions{
		Provider:     "openai",
		APIBase:      "https://api.openai.com",
		DefaultModel: "gpt-5.2",
		JournalDir:   root,
	}).(*UsageClient)
	client.now = func() time.Time {
		return time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	}
	defer func() { _ = client.Close() }()

	ctx := WithMetadata(context.Background(), "run_test_1", "evt_test_1")
	_, err := client.Chat(ctx, llm.Request{Model: "gpt-5.2", Scene: "agent.step"})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(root, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var rec RequestRecord
	if err := json.Unmarshal(data[:len(data)-1], &rec); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if rec.RunID != "run_test_1" || rec.OriginEventID != "evt_test_1" {
		t.Fatalf("record metadata = %+v", rec)
	}
	if rec.Scene != "agent.step" || rec.APIHost != "api.openai.com" || rec.TotalTokens != 18 {
		t.Fatalf("record content = %+v", rec)
	}
	if rec.CachedInputTokens != 5 || rec.CacheCreationInputTokens != 3 {
		t.Fatalf("record cache tokens = %+v", rec)
	}
	if got := rec.CacheDetails["ephemeral_5m_input_tokens"]; got != 3 {
		t.Fatalf("record cache details = %+v", rec.CacheDetails)
	}
	if rec.CostCurrency != "USD" || !rec.CostEstimated || !costAlmostEqual(rec.TotalCost, 0.035) {
		t.Fatalf("record cost = %+v", rec)
	}
}

func TestUsageClientObservesTopicContextInCapturedStore(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	rootA := t.TempDir()
	rootB := t.TempDir()
	globalRoot := t.TempDir()
	pathA := filepath.Join(rootA, "topic_context.json")
	pathB := filepath.Join(rootB, "topic_context.json")
	globalPath := filepath.Join(globalRoot, "topic_context.json")
	viper.Set("file_state_dir", globalRoot)

	clientA := WrapClient(stubUsageClient{}, ClientOptions{
		Provider:          "openai",
		DefaultModel:      "model-a",
		JournalDir:        filepath.Join(rootA, "usage"),
		TopicContextStore: topiccontext.NewStore(pathA),
	}).(*UsageClient)
	clientB := WrapClient(stubUsageClient{}, ClientOptions{
		Provider:          "openai",
		DefaultModel:      "model-b",
		JournalDir:        filepath.Join(rootB, "usage"),
		TopicContextStore: topiccontext.NewStore(pathB),
	}).(*UsageClient)
	t.Cleanup(func() {
		_ = clientA.Close()
		_ = clientB.Close()
	})

	ctxA := topiccontext.WithScope(context.Background(), topiccontext.Scope{ConversationKey: "shared", Runtime: "a"})
	ctxB := topiccontext.WithScope(context.Background(), topiccontext.Scope{ConversationKey: "shared", Runtime: "b"})
	if _, err := clientA.Chat(ctxA, llm.Request{Model: "model-a", Scene: "runtime.loop"}); err != nil {
		t.Fatalf("clientA.Chat() error = %v", err)
	}
	if _, err := clientB.Chat(ctxB, llm.Request{Model: "model-b", Scene: "runtime.loop"}); err != nil {
		t.Fatalf("clientB.Chat() error = %v", err)
	}

	itemA, ok, err := topiccontext.NewStore(pathA).Get("shared")
	if err != nil || !ok || itemA.Model != "model-a" || itemA.Runtime != "a" {
		t.Fatalf("store A item = %#v, ok=%v, err=%v", itemA, ok, err)
	}
	itemB, ok, err := topiccontext.NewStore(pathB).Get("shared")
	if err != nil || !ok || itemB.Model != "model-b" || itemB.Runtime != "b" {
		t.Fatalf("store B item = %#v, ok=%v, err=%v", itemB, ok, err)
	}
	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Fatalf("global topic context path was used: %v", err)
	}
}

func TestImageUsageClientRecordsRequestMetadata(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := WrapImageClient(stubImageUsageClient{}, ClientOptions{
		Provider:     "openai",
		APIBase:      "https://api.openai.com",
		DefaultModel: "gpt-image-1",
		JournalDir:   root,
	}).(*ImageUsageClient)
	client.now = func() time.Time {
		return time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	}
	defer func() { _ = client.Close() }()

	ctx := WithMetadata(context.Background(), "run_image_1", "evt_image_1")
	if _, err := client.GenerateImage(ctx, llm.ImageRequest{Model: "gpt-image-1"}); err != nil {
		t.Fatalf("GenerateImage() error = %v", err)
	}
	if _, err := client.EditImage(ctx, llm.ImageEditRequest{}); err != nil {
		t.Fatalf("EditImage() error = %v", err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(root, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}

	var generateRec RequestRecord
	if err := json.Unmarshal([]byte(lines[0]), &generateRec); err != nil {
		t.Fatalf("Unmarshal(generate) error = %v", err)
	}
	if generateRec.Operation != operationImageGenerate || generateRec.Scene != "tool.image_generate" {
		t.Fatalf("generate record operation = %+v", generateRec)
	}
	if generateRec.RunID != "run_image_1" || generateRec.OriginEventID != "evt_image_1" {
		t.Fatalf("generate record metadata = %+v", generateRec)
	}
	if generateRec.TotalTokens != 20 || generateRec.CachedInputTokens != 2 || generateRec.CacheDetails["input_image_tokens"] != 9 {
		t.Fatalf("generate record usage = %+v", generateRec)
	}
	if generateRec.CostCurrency != "USD" || !generateRec.CostEstimated || !costAlmostEqual(generateRec.TotalCost, 0.07) {
		t.Fatalf("generate record cost = %+v", generateRec)
	}
	if generateRec.DurationMs != 125 {
		t.Fatalf("generate duration = %d, want 125", generateRec.DurationMs)
	}

	var editRec RequestRecord
	if err := json.Unmarshal([]byte(lines[1]), &editRec); err != nil {
		t.Fatalf("Unmarshal(edit) error = %v", err)
	}
	if editRec.Operation != operationImageEdit || editRec.Scene != "tool.image_edit" {
		t.Fatalf("edit record operation = %+v", editRec)
	}
	if editRec.Model != "gpt-image-1" || editRec.TotalTokens != 24 || !costAlmostEqual(editRec.TotalCost, 0.11) {
		t.Fatalf("edit record content = %+v", editRec)
	}
}
