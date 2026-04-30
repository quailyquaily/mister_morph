package line

import (
	"context"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/chatcommands"
)

func TestLineCommandRegistryHandlesHelpAndModel(t *testing.T) {
	var gotModelText string
	reg := chatcommands.NewRuntimeRegistry(chatcommands.RuntimeRegistryOptions{
		ModelCommand: func(text string) (string, bool, error) {
			gotModelText = text
			return "model ok", true, nil
		},
		WorkspaceKey: "conv",
	})

	help, handled, err := reg.Dispatch(context.Background(), "/help")
	if err != nil {
		t.Fatalf("/help error = %v", err)
	}
	if !handled || help == nil {
		t.Fatalf("expected /help handled")
	}
	for _, want := range []string{"/help", "/model", "/workspace"} {
		if !strings.Contains(help.Reply, want) {
			t.Fatalf("/help reply missing %q: %q", want, help.Reply)
		}
	}

	model, handled, err := reg.Dispatch(context.Background(), "/model set cheap")
	if err != nil {
		t.Fatalf("/model error = %v", err)
	}
	if !handled || model == nil || model.Reply != "model ok" {
		t.Fatalf("unexpected /model result: %#v handled=%v", model, handled)
	}
	if gotModelText != "/model set cheap" {
		t.Fatalf("model text = %q, want %q", gotModelText, "/model set cheap")
	}
}
