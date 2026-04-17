package builtin

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecuteShellCommand_ReturnsObservationOnExitErrorWhenConfigured(t *testing.T) {
	out, err := executeShellCommand(context.Background(), map[string]any{
		"cmd": "printf 'boom'; exit 7",
	}, shellToolCommon{
		ToolName:       "powershell",
		DefaultTimeout: 5 * time.Second,
		MaxOutputBytes: 4096,
	}, shellRunnerSpec{
		Program:                      "bash",
		ArgsPrefix:                   []string{"-lc"},
		BuildEnv:                     bashToolEnv,
		MatchDeniedPath:              bashCommandDenied,
		ReturnObservationOnExitError: true,
	})
	if err == nil {
		t.Fatalf("expected error, got nil (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "powershell exited with code 7") {
		t.Fatalf("error = %v, want powershell exited with code 7", err)
	}
	if !strings.Contains(out, "exit_code: 7") {
		t.Fatalf("expected observation to contain exit_code: 7, got %q", out)
	}
	if !strings.Contains(out, "boom") {
		t.Fatalf("expected observation to contain stdout payload, got %q", out)
	}
}

func TestExecuteShellCommand_DropsObservationOnTimeoutWhenConfigured(t *testing.T) {
	out, err := executeShellCommand(context.Background(), map[string]any{
		"cmd":             "sleep 1",
		"timeout_seconds": 0.05,
	}, shellToolCommon{
		ToolName:       "powershell",
		DefaultTimeout: 5 * time.Second,
		MaxOutputBytes: 4096,
	}, shellRunnerSpec{
		Program:                    "bash",
		ArgsPrefix:                 []string{"-lc"},
		BuildEnv:                   bashToolEnv,
		MatchDeniedPath:            bashCommandDenied,
		ReturnObservationOnTimeout: false,
	})
	if err == nil {
		t.Fatalf("expected timeout error, got nil (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "powershell timed out after") {
		t.Fatalf("error = %v, want powershell timed out after", err)
	}
	if out != "" {
		t.Fatalf("expected timeout path to drop observation, got %q", out)
	}
}
