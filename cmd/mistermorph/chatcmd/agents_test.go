package chatcmd

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type canceledChatCommandClient struct {
	calls int
}

func (c *canceledChatCommandClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	c.calls++
	return llm.Result{Text: `{"type":"final","output":"unexpected"}`}, nil
}

type delayedChatCommandClient struct{}

func (c *delayedChatCommandClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	time.Sleep(120 * time.Millisecond)
	return llm.Result{Text: `{"type":"final","output":"# Project"}`}, nil
}

func TestHandleAgentsGenerateUsesSessionContext(t *testing.T) {
	client := &canceledChatCommandClient{}
	engine := agent.New(client, tools.NewRegistry(), agent.Config{MaxSteps: 1, DefaultModel: "test"}, agent.DefaultPromptSpec())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	history, ok := handleAgentsGenerate(ctx, &bytes.Buffer{}, "/update", t.TempDir(), time.Second, engine, "test", nil)
	if ok || history != nil {
		t.Fatalf("handleAgentsGenerate() = history:%#v ok:%v, want nil/false", history, ok)
	}
	if client.calls != 0 {
		t.Fatalf("LLM calls = %d, want 0 after session cancellation", client.calls)
	}
}

func TestHandleAgentsGenerateDoesNotWriteAnimatedFrames(t *testing.T) {
	engine := agent.New(&delayedChatCommandClient{}, tools.NewRegistry(), agent.Config{MaxSteps: 1, DefaultModel: "test"}, agent.DefaultPromptSpec())
	var output bytes.Buffer

	_, ok := handleAgentsGenerate(context.Background(), &output, "/init", t.TempDir(), time.Second, engine, "test", nil)
	if !ok {
		t.Fatalf("handleAgentsGenerate() failed: %s", output.String())
	}
	if bytes.Contains(output.Bytes(), []byte("\r\033[2K")) {
		t.Fatalf("output contains spinner redraw frames: %q", output.String())
	}
}
