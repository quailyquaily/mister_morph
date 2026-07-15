package runcmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
)

func TestCLIContextCompactionStatusWritesOnlySuccessToStatusStream(t *testing.T) {
	var output bytes.Buffer
	ctx := withCLIContextCompactionStatus(context.Background(), nil, &output)

	agent.EmitEvent(ctx, nil, agent.Event{Kind: agent.EventKindContextCompactionStart})
	agent.EmitEvent(ctx, nil, agent.Event{Kind: agent.EventKindContextCompactionDone})

	want := taskruntime.ContextCompactionDoneText + "\n"
	if output.String() != want {
		t.Fatalf("status output = %q, want %q", output.String(), want)
	}
}
