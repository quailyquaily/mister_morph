package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type memoryApprovalStore struct {
	mu      sync.Mutex
	nextID  int
	records map[string]guard.ApprovalRecord
}

func newMemoryApprovalStore() *memoryApprovalStore {
	return &memoryApprovalStore{records: make(map[string]guard.ApprovalRecord)}
}

func (s *memoryApprovalStore) Create(_ context.Context, rec guard.ApprovalRecord) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := fmt.Sprintf("approval-%d", s.nextID)
	rec.ID = id
	s.records[id] = rec
	return id, nil
}

func (s *memoryApprovalStore) Get(_ context.Context, id string) (guard.ApprovalRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[id]
	return rec, ok, nil
}

func (s *memoryApprovalStore) Resolve(_ context.Context, id string, status guard.ApprovalStatus, actor, comment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[id]
	if !ok {
		return guard.ErrApprovalNotFound
	}
	rec.Status = status
	rec.Actor = actor
	rec.Comment = comment
	s.records[id] = rec
	return nil
}

func (s *memoryApprovalStore) ConsumeApproved(_ context.Context, id string) (guard.ApprovalRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[id]
	if !ok {
		return guard.ApprovalRecord{}, guard.ErrApprovalNotFound
	}
	if rec.ConsumedAt != nil {
		return guard.ApprovalRecord{}, guard.ErrApprovalAlreadyConsumed
	}
	if rec.Status != guard.ApprovalApproved {
		return guard.ApprovalRecord{}, guard.ErrApprovalNotApproved
	}
	now := time.Now().UTC()
	rec.ConsumedAt = &now
	s.records[id] = rec
	return rec, nil
}

func approvalGuard(store guard.ApprovalStore, audit guard.AuditSink) *guard.Guard {
	return guard.New(guard.Config{
		Enabled: true,
		Approvals: guard.ApprovalsConfig{
			Enabled: true,
		},
	}, audit, store)
}

func pendingApprovalID(t *testing.T, final *Final) string {
	t.Helper()
	if final == nil {
		t.Fatal("final is nil")
	}
	pending, ok := final.Output.(PendingOutput)
	if !ok {
		t.Fatalf("output = %#v, want PendingOutput", final.Output)
	}
	if strings.TrimSpace(pending.ApprovalRequestID) == "" {
		t.Fatal("approval request ID is empty")
	}
	return pending.ApprovalRequestID
}

func TestToolBatchApprovalPreservesPrefixAndApprovesOneAction(t *testing.T) {
	store := newMemoryApprovalStore()
	g := approvalGuard(store, nil)

	var readCalls, bashCalls int
	registry := tools.NewRegistry()
	registry.Register(&countingTool{name: "read_file", result: "ordinary-result", count: &readCalls})
	registry.Register(&countingTool{name: "bash", result: "bash-result", count: &bashCalls})

	client := newMockClient(
		llm.Result{ToolCalls: []llm.ToolCall{
			{ID: "read-1", Name: "read_file", Arguments: map[string]any{"path": "README.md"}},
			{ID: "bash-1", Name: "bash", Arguments: map[string]any{"cmd": "echo same"}},
			{ID: "read-2", Name: "read_file", Arguments: map[string]any{"path": "README.md"}},
			{ID: "bash-2", Name: "bash", Arguments: map[string]any{"cmd": "echo same"}},
		}},
		finalResponse("done"),
	)
	engine := New(client, registry, Config{MaxSteps: 4}, DefaultPromptSpec(), WithGuard(g))

	firstPending, firstCtx, err := engine.Run(context.Background(), "run batch", RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	firstApprovalID := pendingApprovalID(t, firstPending)
	if readCalls != 1 || bashCalls != 0 {
		t.Fatalf("calls after first pause = read:%d bash:%d, want 1/0", readCalls, bashCalls)
	}
	if len(firstCtx.Steps) != 1 || firstCtx.Steps[0].Action != "read_file" || firstCtx.Steps[0].Observation != "ordinary-result" {
		t.Fatalf("steps after first pause = %#v, want preserved read_file result", firstCtx.Steps)
	}
	if err := g.ResolveApproval(context.Background(), firstApprovalID, guard.ApprovalApproved, "tester", ""); err != nil {
		t.Fatalf("ResolveApproval(first) error = %v", err)
	}

	secondPending, secondCtx, err := engine.Resume(context.Background(), firstApprovalID)
	if err != nil {
		t.Fatalf("Resume(first) error = %v", err)
	}
	secondApprovalID := pendingApprovalID(t, secondPending)
	if secondApprovalID == firstApprovalID {
		t.Fatalf("second approval ID = %q, want a new request", secondApprovalID)
	}
	secondApproval, ok, err := store.Get(context.Background(), secondApprovalID)
	if err != nil || !ok {
		t.Fatalf("Get(second approval) found = %v, error = %v", ok, err)
	}
	if secondApproval.ToolName != "bash" {
		t.Fatalf("second approval tool = %q, want bash", secondApproval.ToolName)
	}
	firstApproval, ok, err := store.Get(context.Background(), firstApprovalID)
	if err != nil || !ok {
		t.Fatalf("Get(first approval) found = %v, error = %v", ok, err)
	}
	if firstApproval.ActionHash == secondApproval.ActionHash {
		t.Fatalf("identical calls have the same approval action hash %q", firstApproval.ActionHash)
	}
	if readCalls != 2 || bashCalls != 1 {
		t.Fatalf("calls after second pause = read:%d bash:%d, want 2/1", readCalls, bashCalls)
	}
	if len(secondCtx.Steps) != 3 ||
		secondCtx.Steps[0].Action != "read_file" ||
		secondCtx.Steps[1].Action != "bash" ||
		secondCtx.Steps[2].Action != "read_file" ||
		secondCtx.Steps[0].Observation != "ordinary-result" ||
		secondCtx.Steps[1].Observation != "bash-result" ||
		secondCtx.Steps[2].Observation != "ordinary-result" {
		t.Fatalf("steps after second pause = %#v, want read, bash, read in provider order", secondCtx.Steps)
	}
	if err := g.ResolveApproval(context.Background(), secondApprovalID, guard.ApprovalApproved, "tester", ""); err != nil {
		t.Fatalf("ResolveApproval(second) error = %v", err)
	}

	final, finalCtx, err := engine.Resume(context.Background(), secondApprovalID)
	if err != nil {
		t.Fatalf("Resume(second) error = %v", err)
	}
	if final == nil || final.Output != "done" {
		t.Fatalf("final = %#v, want done", final)
	}
	if readCalls != 2 || bashCalls != 2 {
		t.Fatalf("final calls = read:%d bash:%d, want 2/2", readCalls, bashCalls)
	}
	if len(finalCtx.Steps) != 4 {
		t.Fatalf("final steps = %d, want 4", len(finalCtx.Steps))
	}

	calls := client.allCalls()
	if len(calls) != 2 {
		t.Fatalf("LLM calls = %d, want initial batch and final", len(calls))
	}
	toolResults := make(map[string]string)
	for _, message := range calls[1].Messages {
		if message.Role == "tool" {
			toolResults[message.ToolCallID] = message.Content
		}
	}
	for id, want := range map[string]string{
		"read-1": "ordinary-result",
		"bash-1": "bash-result",
		"read-2": "ordinary-result",
		"bash-2": "bash-result",
	} {
		if !strings.Contains(toolResults[id], want) {
			t.Errorf("tool result %s = %q, want it to contain %q", id, toolResults[id], want)
		}
	}
}

func TestApprovalCanResumeOnlyOnce(t *testing.T) {
	store := newMemoryApprovalStore()
	g := approvalGuard(store, nil)
	var bashCalls int
	registry := tools.NewRegistry()
	registry.Register(&countingTool{name: "bash", result: "bash-result", count: &bashCalls})
	client := newMockClient(
		llm.Result{ToolCalls: []llm.ToolCall{{
			ID:        "bash-once",
			Name:      "bash",
			Arguments: map[string]any{"cmd": "echo once"},
		}}},
		finalResponse("done"),
	)
	engine := New(client, registry, Config{MaxSteps: 3}, DefaultPromptSpec(), WithGuard(g))

	pending, _, err := engine.Run(context.Background(), "run once", RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	approvalID := pendingApprovalID(t, pending)
	if err := g.ResolveApproval(context.Background(), approvalID, guard.ApprovalApproved, "tester", ""); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	final, _, err := engine.Resume(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("first Resume() error = %v", err)
	}
	if final == nil || final.Output != "done" || bashCalls != 1 {
		t.Fatalf("first Resume() final = %#v, bash calls = %d, want done and 1", final, bashCalls)
	}

	secondFinal, _, err := engine.Resume(context.Background(), approvalID)
	if !errors.Is(err, guard.ErrApprovalAlreadyConsumed) {
		t.Fatalf("second Resume() error = %v, want ErrApprovalAlreadyConsumed", err)
	}
	if secondFinal != nil {
		t.Fatalf("second Resume() final = %#v, want nil", secondFinal)
	}
	if bashCalls != 1 {
		t.Fatalf("bash calls after second Resume = %d, want 1", bashCalls)
	}
}

func TestCanceledResumeDoesNotConsumeApproval(t *testing.T) {
	store := newMemoryApprovalStore()
	g := approvalGuard(store, nil)
	var bashCalls int
	registry := tools.NewRegistry()
	registry.Register(&countingTool{name: "bash", result: "bash-result", count: &bashCalls})
	client := newMockClient(
		llm.Result{ToolCalls: []llm.ToolCall{{
			ID:        "bash-after-cancel",
			Name:      "bash",
			Arguments: map[string]any{"cmd": "echo later"},
		}}},
		finalResponse("done"),
	)
	engine := New(client, registry, Config{MaxSteps: 3}, DefaultPromptSpec(), WithGuard(g))
	pending, _, err := engine.Run(context.Background(), "run after cancel", RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	approvalID := pendingApprovalID(t, pending)
	if err := g.ResolveApproval(context.Background(), approvalID, guard.ApprovalApproved, "tester", ""); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := engine.Resume(canceledCtx, approvalID); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resume(canceled) error = %v, want context.Canceled", err)
	}
	if bashCalls != 0 {
		t.Fatalf("bash calls after canceled Resume = %d, want 0", bashCalls)
	}

	final, _, err := engine.Resume(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("Resume(valid) error = %v", err)
	}
	if final == nil || final.Output != "done" || bashCalls != 1 {
		t.Fatalf("Resume(valid) final = %#v, bash calls = %d, want done and 1", final, bashCalls)
	}
}

func TestResumeRejectsApprovalWhenPendingToolParamsChange(t *testing.T) {
	store := newMemoryApprovalStore()
	g := approvalGuard(store, nil)
	var bashCalls int
	registry := tools.NewRegistry()
	registry.Register(&countingTool{name: "bash", result: "bash-result", count: &bashCalls})
	engine := New(newMockClient(llm.Result{ToolCalls: []llm.ToolCall{{
		ID:        "bash-bound-action",
		Name:      "bash",
		Arguments: map[string]any{"cmd": "echo approved"},
	}}}), registry, Config{MaxSteps: 2}, DefaultPromptSpec(), WithGuard(g))

	pending, _, err := engine.Run(context.Background(), "bind approval", RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	approvalID := pendingApprovalID(t, pending)
	if err := g.ResolveApproval(context.Background(), approvalID, guard.ApprovalApproved, "tester", ""); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	store.mu.Lock()
	rec := store.records[approvalID]
	resumeState, err := unmarshalResumeState(rec.ResumeState)
	if err != nil {
		store.mu.Unlock()
		t.Fatalf("unmarshalResumeState() error = %v", err)
	}
	resumeState.PendingTool.ToolCall.Params["cmd"] = "echo changed"
	rec.ResumeState, err = marshalResumeState(resumeState)
	if err != nil {
		store.mu.Unlock()
		t.Fatalf("marshalResumeState() error = %v", err)
	}
	store.records[approvalID] = rec
	store.mu.Unlock()

	final, _, err := engine.Resume(context.Background(), approvalID)
	if err == nil || !strings.Contains(err.Error(), "approval action_hash mismatch") {
		t.Fatalf("Resume() error = %v, want action hash mismatch", err)
	}
	if final != nil || bashCalls != 0 {
		t.Fatalf("Resume() final = %#v, bash calls = %d, want nil and 0", final, bashCalls)
	}
	rec, _, _ = store.Get(context.Background(), approvalID)
	if rec.ConsumedAt != nil {
		t.Fatal("mismatched approval was consumed")
	}
}

func TestResumeRejectsApprovalWithoutActionHash(t *testing.T) {
	store := newMemoryApprovalStore()
	g := approvalGuard(store, nil)
	var bashCalls int
	registry := tools.NewRegistry()
	registry.Register(&countingTool{name: "bash", result: "bash-result", count: &bashCalls})
	engine := New(newMockClient(llm.Result{ToolCalls: []llm.ToolCall{{
		ID:        "bash-missing-hash",
		Name:      "bash",
		Arguments: map[string]any{"cmd": "echo approved"},
	}}}), registry, Config{MaxSteps: 2}, DefaultPromptSpec(), WithGuard(g))

	pending, _, err := engine.Run(context.Background(), "require action hash", RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	approvalID := pendingApprovalID(t, pending)
	if err := g.ResolveApproval(context.Background(), approvalID, guard.ApprovalApproved, "tester", ""); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	store.mu.Lock()
	rec := store.records[approvalID]
	rec.ActionHash = ""
	store.records[approvalID] = rec
	store.mu.Unlock()

	final, _, err := engine.Resume(context.Background(), approvalID)
	if err == nil || !strings.Contains(err.Error(), "approval has no action_hash") {
		t.Fatalf("Resume() error = %v, want missing action hash error", err)
	}
	if final != nil || bashCalls != 0 {
		t.Fatalf("Resume() final = %#v, bash calls = %d, want nil and 0", final, bashCalls)
	}
	rec, _, _ = store.Get(context.Background(), approvalID)
	if rec.ConsumedAt != nil {
		t.Fatal("unbound approval was consumed")
	}
}

const finalSecret = "supersecretvalue12345"

func assertFinalHasNoSecret(t *testing.T, final *Final, agentCtx *Context) {
	t.Helper()
	encodedFinal, err := json.Marshal(final)
	if err != nil {
		t.Fatalf("json.Marshal(final) error = %v", err)
	}
	if strings.Contains(string(encodedFinal), finalSecret) {
		t.Fatalf("final leaked secret: %s", encodedFinal)
	}
	if agentCtx != nil && strings.Contains(string(agentCtx.RawFinalAnswer), finalSecret) {
		t.Fatalf("RawFinalAnswer leaked secret: %s", agentCtx.RawFinalAnswer)
	}
	if agentCtx != nil && agentCtx.Plan != nil {
		encodedPlan, err := json.Marshal(agentCtx.Plan)
		if err != nil {
			t.Fatalf("json.Marshal(agentCtx.Plan) error = %v", err)
		}
		if strings.Contains(string(encodedPlan), finalSecret) {
			t.Fatalf("agent context plan leaked secret: %s", encodedPlan)
		}
	}
}

func TestPlanResponseUsesOutputGuardBeforeUpdateHook(t *testing.T) {
	client := newMockClient(
		llm.Result{Text: fmt.Sprintf(`{"type":"plan","thought":"token=%s","steps":[{"step":"password=%s","status":"in_progress"}]}`, finalSecret, finalSecret)},
		llm.Result{Text: finalResponse("done").Text},
	)
	var publishedPlans [][]byte
	engine := New(client, tools.NewRegistry(), baseCfg(), DefaultPromptSpec(),
		WithGuard(approvalGuard(nil, nil)),
		WithPlanStepUpdate(func(runCtx *Context, _ PlanStepUpdate) {
			encoded, err := json.Marshal(runCtx.Plan)
			if err != nil {
				t.Errorf("json.Marshal(plan) error = %v", err)
				return
			}
			publishedPlans = append(publishedPlans, encoded)
		}),
	)

	final, agentCtx, err := engine.Run(context.Background(), "guard plan progress", RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(publishedPlans) == 0 {
		t.Fatal("plan update hook was not called")
	}
	for _, encoded := range publishedPlans {
		if strings.Contains(string(encoded), finalSecret) {
			t.Fatalf("plan update hook leaked secret: %s", encoded)
		}
	}
	assertFinalHasNoSecret(t, final, agentCtx)
}

func TestFinalEgressSynchronizesGuardedPlanToContext(t *testing.T) {
	plan := &Plan{
		Thought: "token=" + finalSecret,
		Steps: []PlanStep{{
			Step:   "password=" + finalSecret,
			Status: PlanStatusCompleted,
		}},
	}
	agentCtx := NewContext("guard final plan", baseCfg().MaxSteps)
	agentCtx.Plan = plan
	engine := New(newMockClient(), tools.NewRegistry(), baseCfg(), DefaultPromptSpec(), WithGuard(approvalGuard(nil, nil)))
	final, err := engine.finalEgress(context.Background(), &engineLoopState{
		agentCtx: agentCtx,
		runID:    "guard-final-plan",
	}, 1, &Final{Output: "done", Plan: plan}, nil)
	if err != nil {
		t.Fatalf("finalEgress() error = %v", err)
	}
	assertFinalHasNoSecret(t, final, agentCtx)
}

func TestFinalEgressRecursivelyRedactsOutputAndRawPayload(t *testing.T) {
	response := fmt.Sprintf(`{"type":"final","reasoning":"token=%s","output":{"text":"password=%s","items":["Bearer %s"]},"extra":{"token=%s":"safe","nested":"secret=%s"}}`, finalSecret, finalSecret, finalSecret, finalSecret, finalSecret)
	engine := New(newMockClient(llm.Result{Text: response}), tools.NewRegistry(), baseCfg(), DefaultPromptSpec(), WithGuard(approvalGuard(nil, nil)))

	final, agentCtx, err := engine.Run(context.Background(), "redact final", RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertFinalHasNoSecret(t, final, agentCtx)
	output, ok := final.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %#v, want object", final.Output)
	}
	if _, ok := output["items"].([]any); !ok {
		t.Fatalf("output.items = %#v, want array", output["items"])
	}
}

func TestFinalEgressRedactsEachJSONOutputShape(t *testing.T) {
	tests := []struct {
		name       string
		outputJSON string
		assertType func(*testing.T, any)
	}{
		{
			name:       "string",
			outputJSON: fmt.Sprintf(`"password=%s"`, finalSecret),
			assertType: func(t *testing.T, value any) {
				t.Helper()
				if _, ok := value.(string); !ok {
					t.Fatalf("output = %#v, want string", value)
				}
			},
		},
		{
			name:       "object",
			outputJSON: fmt.Sprintf(`{"nested":"secret=%s"}`, finalSecret),
			assertType: func(t *testing.T, value any) {
				t.Helper()
				if _, ok := value.(map[string]any); !ok {
					t.Fatalf("output = %#v, want object", value)
				}
			},
		},
		{
			name:       "array",
			outputJSON: fmt.Sprintf(`["Bearer %s",{"nested":"token=%s"}]`, finalSecret, finalSecret),
			assertType: func(t *testing.T, value any) {
				t.Helper()
				if _, ok := value.([]any); !ok {
					t.Fatalf("output = %#v, want array", value)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := fmt.Sprintf(`{"type":"final","output":%s}`, test.outputJSON)
			engine := New(newMockClient(llm.Result{Text: response}), tools.NewRegistry(), baseCfg(), DefaultPromptSpec(), WithGuard(approvalGuard(nil, nil)))
			final, agentCtx, err := engine.Run(context.Background(), "redact output", RunOptions{})
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			assertFinalHasNoSecret(t, final, agentCtx)
			test.assertType(t, final.Output)
		})
	}
}

func TestForceConclusionUsesFinalEgress(t *testing.T) {
	response := fmt.Sprintf(`{"type":"final","output":{"nested":["password=%s"]},"extra":"token=%s"}`, finalSecret, finalSecret)
	client := newMockClient(
		llm.Result{Text: "not json"},
		llm.Result{Text: response},
	)
	engine := New(client, tools.NewRegistry(), Config{MaxSteps: 2, ParseRetries: 0}, DefaultPromptSpec(), WithGuard(approvalGuard(nil, nil)))

	final, agentCtx, err := engine.Run(context.Background(), "force final", RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertFinalHasNoSecret(t, final, agentCtx)
}

func TestFallbackUsesFinalEgress(t *testing.T) {
	client := newMockClient(llm.Result{Text: "not json"})
	engine := New(client, tools.NewRegistry(), Config{MaxSteps: 2, ParseRetries: 0}, DefaultPromptSpec(),
		WithGuard(approvalGuard(nil, nil)),
		WithFallbackFinal(func() *Final {
			return &Final{Output: map[string]any{"nested": []any{"password=" + finalSecret}}}
		}),
	)

	final, agentCtx, err := engine.Run(context.Background(), "fallback final", RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertFinalHasNoSecret(t, final, agentCtx)
}

func TestStopAfterSuccessUsesFinalEgress(t *testing.T) {
	registry := tools.NewRegistry()
	planObservation := fmt.Sprintf(`{"plan":{"thought":"token=%s","steps":[{"step":"password=%s"}]}}`, finalSecret, finalSecret)
	registry.Register(&countingTool{name: "plan_create", result: planObservation, count: new(int)})
	registry.Register(&stopAfterSuccessTool{name: "stopper", result: "done"})
	client := newMockClient(llm.Result{ToolCalls: []llm.ToolCall{
		{ID: "plan", Name: "plan_create", Arguments: map[string]any{}},
		{ID: "stop", Name: "stopper", Arguments: map[string]any{}},
	}})
	audit := &captureActionAuditSink{}
	engine := New(client, registry, baseCfg(), DefaultPromptSpec(), WithGuard(approvalGuard(nil, audit)))

	final, agentCtx, err := engine.Run(context.Background(), "stop after success", RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if final == nil || final.Plan == nil {
		t.Fatalf("final = %#v, want plan", final)
	}
	assertFinalHasNoSecret(t, final, agentCtx)
	if !audit.hasAction(guard.ActionOutputPublish) {
		t.Fatal("StopAfterSuccess did not pass through the final output Guard action")
	}
}

type captureActionAuditSink struct {
	actions []guard.ActionType
}

func (s *captureActionAuditSink) Emit(_ context.Context, event guard.AuditEvent) error {
	s.actions = append(s.actions, event.ActionType)
	return nil
}

func (*captureActionAuditSink) Close() error { return nil }

func (s *captureActionAuditSink) hasAction(action guard.ActionType) bool {
	for _, item := range s.actions {
		if item == action {
			return true
		}
	}
	return false
}

type failingAuditSink struct{ err error }

func (s *failingAuditSink) Emit(context.Context, guard.AuditEvent) error { return s.err }
func (s *failingAuditSink) Close() error                                 { return nil }

type nthFailingAuditSink struct {
	err    error
	failAt int
	calls  int
}

func (s *nthFailingAuditSink) Emit(context.Context, guard.AuditEvent) error {
	s.calls++
	if s.calls == s.failAt {
		return s.err
	}
	return nil
}

func (s *nthFailingAuditSink) Close() error { return nil }

func TestFinalEgressReturnsAuditError(t *testing.T) {
	auditErr := errors.New("audit write failed")
	engine := New(newMockClient(finalResponse("done")), tools.NewRegistry(), baseCfg(), DefaultPromptSpec(),
		WithGuard(approvalGuard(nil, &failingAuditSink{err: auditErr})),
	)

	final, _, err := engine.Run(context.Background(), "audit failure", RunOptions{})
	if !errors.Is(err, auditErr) {
		t.Fatalf("Run() error = %v, want %v", err, auditErr)
	}
	if final != nil {
		t.Fatalf("final = %#v, want nil when output audit fails", final)
	}
}

func TestToolPreCheckReturnsAuditErrorWithoutExecutingTool(t *testing.T) {
	auditErr := errors.New("pre-tool audit write failed")
	var calls int
	registry := tools.NewRegistry()
	registry.Register(&countingTool{name: "bash", result: "bash-result", count: &calls})
	engine := New(newMockClient(toolCallResponse("bash")), registry, baseCfg(), DefaultPromptSpec(),
		WithGuard(approvalGuard(newMemoryApprovalStore(), &failingAuditSink{err: auditErr})),
	)

	final, _, err := engine.Run(context.Background(), "audit tool input", RunOptions{})
	if !errors.Is(err, auditErr) {
		t.Fatalf("Run() error = %v, want %v", err, auditErr)
	}
	if final != nil || calls != 0 {
		t.Fatalf("Run() final = %#v, tool calls = %d, want nil and 0", final, calls)
	}
}

func TestApprovalRequestReturnsAuditErrorWithoutExecutingTool(t *testing.T) {
	auditErr := errors.New("approval audit write failed")
	audit := &nthFailingAuditSink{err: auditErr, failAt: 2}
	store := newMemoryApprovalStore()
	var calls int
	registry := tools.NewRegistry()
	registry.Register(&countingTool{name: "bash", result: "bash-result", count: &calls})
	engine := New(newMockClient(toolCallResponse("bash")), registry, baseCfg(), DefaultPromptSpec(),
		WithGuard(approvalGuard(store, audit)),
	)

	final, _, err := engine.Run(context.Background(), "audit approval request", RunOptions{})
	if !errors.Is(err, auditErr) {
		t.Fatalf("Run() error = %v, want %v", err, auditErr)
	}
	if final != nil || calls != 0 {
		t.Fatalf("Run() final = %#v, tool calls = %d, want nil and 0", final, calls)
	}
}

func TestApprovalResolutionReturnsAuditError(t *testing.T) {
	auditErr := errors.New("approval resolution audit write failed")
	audit := &nthFailingAuditSink{err: auditErr, failAt: 3}
	store := newMemoryApprovalStore()
	registry := tools.NewRegistry()
	registry.Register(&countingTool{name: "bash", result: "bash-result", count: new(int)})
	engine := New(newMockClient(toolCallResponse("bash")), registry, baseCfg(), DefaultPromptSpec(),
		WithGuard(approvalGuard(store, audit)),
	)

	pending, _, err := engine.Run(context.Background(), "audit approval resolution", RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	approvalID := pendingApprovalID(t, pending)
	if err := engine.guard.ResolveApproval(context.Background(), approvalID, guard.ApprovalApproved, "tester", ""); !errors.Is(err, auditErr) {
		t.Fatalf("ResolveApproval() error = %v, want %v", err, auditErr)
	}
}

func TestToolPostCheckReturnsAuditError(t *testing.T) {
	auditErr := errors.New("post-tool audit write failed")
	audit := &nthFailingAuditSink{err: auditErr, failAt: 2}
	registry := tools.NewRegistry()
	registry.Register(&mockTool{name: "read_file", result: "content"})
	engine := New(newMockClient(toolCallResponse("read_file")), registry, baseCfg(), DefaultPromptSpec(),
		WithGuard(approvalGuard(nil, audit)),
	)

	final, _, err := engine.Run(context.Background(), "audit tool output", RunOptions{})
	if !errors.Is(err, auditErr) {
		t.Fatalf("Run() error = %v, want %v", err, auditErr)
	}
	if final != nil {
		t.Fatalf("final = %#v, want nil when post-tool audit fails", final)
	}
}
