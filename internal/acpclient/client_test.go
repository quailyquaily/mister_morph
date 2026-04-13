package acpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunPrompt_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	readPath := filepath.Join(dir, "input.txt")
	writePath := filepath.Join(dir, "output.txt")
	if err := os.WriteFile(readPath, []byte("alpha\nbravo\ncharlie\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(readPath) error = %v", err)
	}

	cfg := AgentConfig{
		Name:           "helper",
		Enable:         true,
		Type:           "stdio",
		Command:        "helper",
		CWD:            dir,
		ReadRoots:      []string{dir},
		WriteRoots:     []string{dir},
		SessionOptions: map[string]any{"approvalMode": "manual"},
	}
	prepared, err := PrepareAgentConfig(cfg, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}

	var events []Event
	result, err := runPromptWithFactory(context.Background(), prepared, RunRequest{
		Prompt: "summarize this change",
		Observer: ObserverFunc(func(_ context.Context, event Event) {
			events = append(events, event)
		}),
	}, fakeACPConnFactory(t, func(dec *json.Decoder, enc *json.Encoder) {
		initMsg := decodeTestMessage(t, dec)
		if initMsg.Method != methodInitialize {
			t.Fatalf("method = %q, want %q", initMsg.Method, methodInitialize)
		}
		encodeTestResponse(t, enc, initMsg.ID, map[string]any{"protocolVersion": protocolVersion})

		newMsg := decodeTestMessage(t, dec)
		if newMsg.Method != methodSessionNew {
			t.Fatalf("method = %q, want %q", newMsg.Method, methodSessionNew)
		}
		var newParams map[string]any
		if err := json.Unmarshal(newMsg.Params, &newParams); err != nil {
			t.Fatalf("json.Unmarshal(session/new) error = %v", err)
		}
		if got := asString(newParams["cwd"]); got != dir {
			t.Fatalf("cwd = %q, want %q", got, dir)
		}
		if got := asString(newParams["_meta"].(map[string]any)["approvalMode"]); got != "manual" {
			t.Fatalf("_meta.approvalMode = %q, want manual", got)
		}
		encodeTestResponse(t, enc, newMsg.ID, map[string]any{"sessionId": "sess_helper"})

		promptMsg := decodeTestMessage(t, dec)
		if promptMsg.Method != methodSessionPrompt {
			t.Fatalf("method = %q, want %q", promptMsg.Method, methodSessionPrompt)
		}

		encodeTestRequest(t, enc, mustMarshalRaw("perm-1"), methodRequestPerm, map[string]any{
			"sessionId": "sess_helper",
			"toolCall": map[string]any{
				"toolCallId": "call_edit",
				"title":      "Edit file",
				"kind":       "edit",
				"status":     "pending",
			},
			"options": []map[string]any{
				{"optionId": "allow", "kind": permissionAllowOnce},
				{"optionId": "reject", "kind": permissionRejectOnce},
			},
		})
		permResp := decodeTestMessage(t, dec)
		var permResult map[string]any
		if err := json.Unmarshal(permResp.Result, &permResult); err != nil {
			t.Fatalf("json.Unmarshal(permission result) error = %v", err)
		}
		if got := asString(permResult["outcome"]); got != permissionOutcomeSel {
			t.Fatalf("permission outcome = %q, want %q", got, permissionOutcomeSel)
		}
		if got := asString(permResult["optionId"]); got != "allow" {
			t.Fatalf("permission optionId = %q, want allow", got)
		}

		encodeTestRequest(t, enc, mustMarshalRaw("read-1"), methodReadTextFile, map[string]any{
			"sessionId": "sess_helper",
			"path":      readPath,
			"line":      2,
			"limit":     1,
		})
		readResp := decodeTestMessage(t, dec)
		var readResult map[string]any
		if err := json.Unmarshal(readResp.Result, &readResult); err != nil {
			t.Fatalf("json.Unmarshal(read result) error = %v", err)
		}
		if got := asString(readResult["content"]); got != "bravo\n" {
			t.Fatalf("read content = %q, want %q", got, "bravo\n")
		}

		encodeTestRequest(t, enc, mustMarshalRaw("write-1"), methodWriteTextFile, map[string]any{
			"sessionId": "sess_helper",
			"path":      writePath,
			"content":   "updated by helper",
		})
		writeResp := decodeTestMessage(t, dec)
		if string(writeResp.Result) != "null" {
			t.Fatalf("write result = %s, want null", string(writeResp.Result))
		}

		encodeTestNotification(t, enc, methodSessionUpdate, map[string]any{
			"sessionId": "sess_helper",
			"update": map[string]any{
				"sessionUpdate": "tool_call",
				"toolCallId":    "call_edit",
				"title":         "Edit file",
				"kind":          "edit",
				"status":        "pending",
				"content": []map[string]any{
					{"type": "text", "text": "editing file"},
				},
			},
		})
		encodeTestNotification(t, enc, methodSessionUpdate, map[string]any{
			"sessionId": "sess_helper",
			"update": map[string]any{
				"sessionUpdate": "tool_call_update",
				"toolCallId":    "call_edit",
				"status":        "completed",
				"content": []map[string]any{
					{"type": "text", "text": "done editing"},
				},
			},
		})
		encodeTestNotification(t, enc, methodSessionUpdate, map[string]any{
			"sessionId": "sess_helper",
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content": map[string]any{
					"type": "text",
					"text": "done from helper",
				},
			},
		})
		encodeTestResponse(t, enc, promptMsg.ID, map[string]any{"stopReason": "end_turn"})
	}))
	if err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want end_turn", result.StopReason)
	}
	if result.Output != "done from helper" {
		t.Fatalf("Output = %q, want %q", result.Output, "done from helper")
	}
	data, err := os.ReadFile(writePath)
	if err != nil {
		t.Fatalf("ReadFile(writePath) error = %v", err)
	}
	if string(data) != "updated by helper" {
		t.Fatalf("written content = %q, want %q", string(data), "updated by helper")
	}
	if len(events) < 3 {
		t.Fatalf("events len = %d, want >= 3", len(events))
	}
	if got := events[len(events)-1].Kind; got != EventKindAgentMessageChunk {
		t.Fatalf("last event kind = %q, want %q", got, EventKindAgentMessageChunk)
	}
}

func TestRunPrompt_ContextCancelSendsSessionCancel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := AgentConfig{
		Name:       "helper",
		Enable:     true,
		Type:       "stdio",
		Command:    "helper",
		CWD:        dir,
		ReadRoots:  []string{dir},
		WriteRoots: []string{dir},
	}
	prepared, err := PrepareAgentConfig(cfg, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan struct{})
	cancelSeen := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		_, runErr := runPromptWithFactory(ctx, prepared, RunRequest{Prompt: "wait"}, fakeACPConnFactory(t, func(dec *json.Decoder, enc *json.Encoder) {
			initMsg := decodeTestMessage(t, dec)
			encodeTestResponse(t, enc, initMsg.ID, map[string]any{"protocolVersion": protocolVersion})

			newMsg := decodeTestMessage(t, dec)
			encodeTestResponse(t, enc, newMsg.ID, map[string]any{"sessionId": "sess_cancel"})

			_ = decodeTestMessage(t, dec)
			close(ready)

			cancelMsg := decodeTestMessage(t, dec)
			if cancelMsg.Method != methodSessionCancel {
				t.Fatalf("method = %q, want %q", cancelMsg.Method, methodSessionCancel)
			}
			close(cancelSeen)
		}))
		errCh <- runErr
	}()

	<-ready
	cancel()

	select {
	case <-cancelSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for session/cancel")
	}

	err = <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunPrompt() error = %v, want context canceled", err)
	}
}

func TestRunPrompt_ReturnsBeforeConnectionCloseAfterPrompt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := AgentConfig{
		Name:       "helper",
		Enable:     true,
		Type:       "stdio",
		Command:    "helper",
		CWD:        dir,
		ReadRoots:  []string{dir},
		WriteRoots: []string{dir},
	}
	prepared, err := PrepareAgentConfig(cfg, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}

	serverRelease := make(chan struct{})
	defer close(serverRelease)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	startedAt := time.Now()
	result, err := runPromptWithFactory(ctx, prepared, RunRequest{
		Prompt: "reply with ok",
	}, fakeACPConnFactory(t, func(dec *json.Decoder, enc *json.Encoder) {
		initMsg := decodeTestMessage(t, dec)
		encodeTestResponse(t, enc, initMsg.ID, map[string]any{"protocolVersion": protocolVersion})

		newMsg := decodeTestMessage(t, dec)
		encodeTestResponse(t, enc, newMsg.ID, map[string]any{"sessionId": "sess_open"})

		promptMsg := decodeTestMessage(t, dec)
		if promptMsg.Method != methodSessionPrompt {
			t.Fatalf("method = %q, want %q", promptMsg.Method, methodSessionPrompt)
		}
		encodeTestResponse(t, enc, promptMsg.ID, map[string]any{"stopReason": "end_turn"})

		<-serverRelease
	}))
	if err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want end_turn", result.StopReason)
	}
	if elapsed := time.Since(startedAt); elapsed > 300*time.Millisecond {
		t.Fatalf("RunPrompt() took too long: %v", elapsed)
	}
}

func TestRunPrompt_TerminalRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := AgentConfig{
		Name:       "helper",
		Enable:     true,
		Type:       "stdio",
		Command:    "helper",
		CWD:        dir,
		ReadRoots:  []string{dir},
		WriteRoots: []string{dir},
	}
	prepared, err := PrepareAgentConfig(cfg, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}

	result, err := runPromptWithFactory(context.Background(), prepared, RunRequest{
		Prompt: "use terminal",
	}, fakeACPConnFactory(t, func(dec *json.Decoder, enc *json.Encoder) {
		initMsg := decodeTestMessage(t, dec)
		encodeTestResponse(t, enc, initMsg.ID, map[string]any{"protocolVersion": protocolVersion})

		newMsg := decodeTestMessage(t, dec)
		encodeTestResponse(t, enc, newMsg.ID, map[string]any{"sessionId": "sess_term"})

		promptMsg := decodeTestMessage(t, dec)
		if promptMsg.Method != methodSessionPrompt {
			t.Fatalf("method = %q, want %q", promptMsg.Method, methodSessionPrompt)
		}

		encodeTestRequest(t, enc, mustMarshalRaw("term-create"), methodTerminalCreate, map[string]any{
			"sessionId": "sess_term",
			"command":   testTerminalEchoCommand(),
			"args":      testTerminalEchoArgs(),
			"env": []map[string]any{
				{"name": "MM_TERM_TEST", "value": "hello-from-terminal"},
			},
			"cwd":             dir,
			"outputByteLimit": 4096,
		})
		createResp := decodeTestMessage(t, dec)
		var createResult map[string]any
		if err := json.Unmarshal(createResp.Result, &createResult); err != nil {
			t.Fatalf("json.Unmarshal(create result) error = %v", err)
		}
		terminalID := asString(createResult["terminalId"])
		if terminalID == "" {
			t.Fatal("terminalId is empty")
		}

		encodeTestRequest(t, enc, mustMarshalRaw("term-wait"), methodTerminalWaitForExit, map[string]any{
			"sessionId":  "sess_term",
			"terminalId": terminalID,
		})
		waitResp := decodeTestMessage(t, dec)
		var waitResult map[string]any
		if err := json.Unmarshal(waitResp.Result, &waitResult); err != nil {
			t.Fatalf("json.Unmarshal(wait result) error = %v", err)
		}
		if got := intFromAny(waitResult["exitCode"]); got != 0 {
			t.Fatalf("exitCode = %d, want 0", got)
		}

		encodeTestRequest(t, enc, mustMarshalRaw("term-output"), methodTerminalOutput, map[string]any{
			"sessionId":  "sess_term",
			"terminalId": terminalID,
		})
		outputResp := decodeTestMessage(t, dec)
		var outputResult map[string]any
		if err := json.Unmarshal(outputResp.Result, &outputResult); err != nil {
			t.Fatalf("json.Unmarshal(output result) error = %v", err)
		}
		if got := asString(outputResult["output"]); got != "hello-from-terminal\n" {
			t.Fatalf("output = %q, want %q", got, "hello-from-terminal\n")
		}
		if truncated, _ := outputResult["truncated"].(bool); truncated {
			t.Fatal("terminal output should not be truncated")
		}

		encodeTestRequest(t, enc, mustMarshalRaw("term-release"), methodTerminalRelease, map[string]any{
			"sessionId":  "sess_term",
			"terminalId": terminalID,
		})
		releaseResp := decodeTestMessage(t, dec)
		if string(releaseResp.Result) != "{}" {
			t.Fatalf("release result = %s, want {}", string(releaseResp.Result))
		}

		encodeTestNotification(t, enc, methodSessionUpdate, map[string]any{
			"sessionId": "sess_term",
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content": map[string]any{
					"type": "text",
					"text": "done",
				},
			},
		})
		encodeTestResponse(t, enc, promptMsg.ID, map[string]any{"stopReason": "end_turn"})
	}))
	if err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if result.Output != "done" {
		t.Fatalf("Output = %q, want %q", result.Output, "done")
	}
}

func TestChoosePermissionOption_PrefersAllowForTerminalRequests(t *testing.T) {
	optionID, ok := choosePermissionOption("terminal", "Run command", []struct {
		OptionID string `json:"optionId"`
		Kind     string `json:"kind"`
	}{
		{OptionID: "reject", Kind: permissionRejectOnce},
		{OptionID: "allow", Kind: permissionAllowOnce},
	})
	if !ok {
		t.Fatal("choosePermissionOption() returned ok=false, want true")
	}
	if optionID != "allow" {
		t.Fatalf("optionID = %q, want %q", optionID, "allow")
	}
}

func TestRunPrompt_AuthenticatesWithConfiguredEnvVarMethod(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := AgentConfig{
		Name:       "helper",
		Enable:     true,
		Type:       "stdio",
		Command:    "helper",
		CWD:        dir,
		ReadRoots:  []string{dir},
		WriteRoots: []string{dir},
		Env: map[string]string{
			"OPENAI_API_KEY": "sk-test",
		},
	}
	prepared, err := PrepareAgentConfig(cfg, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}

	var authenticateSeen bool
	result, err := runPromptWithFactory(context.Background(), prepared, RunRequest{
		Prompt: "reply with ok",
	}, fakeACPConnFactory(t, func(dec *json.Decoder, enc *json.Encoder) {
		initMsg := decodeTestMessage(t, dec)
		encodeTestResponse(t, enc, initMsg.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"authMethods": []map[string]any{
				{
					"id":   "openai-api-key",
					"type": "env_var",
					"vars": []map[string]any{{"name": "OPENAI_API_KEY"}},
				},
			},
		})

		authMsg := decodeTestMessage(t, dec)
		if authMsg.Method != methodAuthenticate {
			t.Fatalf("method = %q, want %q", authMsg.Method, methodAuthenticate)
		}
		var authParams map[string]any
		if err := json.Unmarshal(authMsg.Params, &authParams); err != nil {
			t.Fatalf("json.Unmarshal(authenticate) error = %v", err)
		}
		if got := asString(authParams["methodId"]); got != "openai-api-key" {
			t.Fatalf("methodId = %q, want openai-api-key", got)
		}
		authenticateSeen = true
		encodeTestResponse(t, enc, authMsg.ID, map[string]any{})

		newMsg := decodeTestMessage(t, dec)
		if newMsg.Method != methodSessionNew {
			t.Fatalf("method = %q, want %q", newMsg.Method, methodSessionNew)
		}
		encodeTestResponse(t, enc, newMsg.ID, map[string]any{"sessionId": "sess_auth"})

		promptMsg := decodeTestMessage(t, dec)
		if promptMsg.Method != methodSessionPrompt {
			t.Fatalf("method = %q, want %q", promptMsg.Method, methodSessionPrompt)
		}
		encodeTestResponse(t, enc, promptMsg.ID, map[string]any{"stopReason": "end_turn"})
		time.Sleep(10 * time.Millisecond)
	}))
	if err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if !authenticateSeen {
		t.Fatal("expected authenticate call before session/new")
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want end_turn", result.StopReason)
	}
}

func TestRunPrompt_AppliesSessionOptionsViaConfigRequests(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := AgentConfig{
		Name:       "helper",
		Enable:     true,
		Type:       "stdio",
		Command:    "helper",
		CWD:        dir,
		ReadRoots:  []string{dir},
		WriteRoots: []string{dir},
		SessionOptions: map[string]any{
			"mode":             "read-only",
			"reasoning_effort": "low",
			"brave_mode":       true,
		},
	}
	prepared, err := PrepareAgentConfig(cfg, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}

	seen := map[string]any{}
	result, err := runPromptWithFactory(context.Background(), prepared, RunRequest{
		Prompt: "reply with ok",
	}, fakeACPConnFactory(t, func(dec *json.Decoder, enc *json.Encoder) {
		initMsg := decodeTestMessage(t, dec)
		encodeTestResponse(t, enc, initMsg.ID, map[string]any{"protocolVersion": protocolVersion})

		newMsg := decodeTestMessage(t, dec)
		encodeTestResponse(t, enc, newMsg.ID, map[string]any{
			"sessionId": "sess_opts",
			"configOptions": []map[string]any{
				{"id": "mode"},
				{"id": "reasoning_effort"},
				{"id": "brave_mode"},
			},
		})

		for i := 0; i < 3; i++ {
			cfgMsg := decodeTestMessage(t, dec)
			if cfgMsg.Method != methodSessionSetConfig {
				t.Fatalf("method = %q, want %q", cfgMsg.Method, methodSessionSetConfig)
			}
			var params map[string]any
			if err := json.Unmarshal(cfgMsg.Params, &params); err != nil {
				t.Fatalf("json.Unmarshal(session/set_config_option) error = %v", err)
			}
			configID := asString(params["configId"])
			seen[configID] = params["value"]
			if configID == "brave_mode" {
				if got := asString(params["type"]); got != "boolean" {
					t.Fatalf("type = %q, want boolean", got)
				}
			}
			encodeTestResponse(t, enc, cfgMsg.ID, map[string]any{"configOptions": []any{}})
		}

		promptMsg := decodeTestMessage(t, dec)
		if promptMsg.Method != methodSessionPrompt {
			t.Fatalf("method = %q, want %q", promptMsg.Method, methodSessionPrompt)
		}
		encodeTestResponse(t, enc, promptMsg.ID, map[string]any{"stopReason": "end_turn"})
		time.Sleep(10 * time.Millisecond)
	}))
	if err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if got := asString(seen["mode"]); got != "read-only" {
		t.Fatalf("mode = %q, want read-only", got)
	}
	if got := asString(seen["reasoning_effort"]); got != "low" {
		t.Fatalf("reasoning_effort = %q, want low", got)
	}
	if got, _ := seen["brave_mode"].(bool); !got {
		t.Fatalf("brave_mode = %v, want true", seen["brave_mode"])
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want end_turn", result.StopReason)
	}
}

func TestRunPrompt_PreservesWhitespaceInAgentChunks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := AgentConfig{
		Name:       "helper",
		Enable:     true,
		Type:       "stdio",
		Command:    "helper",
		CWD:        dir,
		ReadRoots:  []string{dir},
		WriteRoots: []string{dir},
	}
	prepared, err := PrepareAgentConfig(cfg, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}

	var events []Event
	result, err := runPromptWithFactory(context.Background(), prepared, RunRequest{
		Prompt: "preserve spacing",
		Observer: ObserverFunc(func(_ context.Context, event Event) {
			events = append(events, event)
		}),
	}, fakeACPConnFactory(t, func(dec *json.Decoder, enc *json.Encoder) {
		initMsg := decodeTestMessage(t, dec)
		encodeTestResponse(t, enc, initMsg.ID, map[string]any{"protocolVersion": protocolVersion})

		newMsg := decodeTestMessage(t, dec)
		encodeTestResponse(t, enc, newMsg.ID, map[string]any{"sessionId": "sess_space"})

		promptMsg := decodeTestMessage(t, dec)
		if promptMsg.Method != methodSessionPrompt {
			t.Fatalf("method = %q, want %q", promptMsg.Method, methodSessionPrompt)
		}

		for _, chunk := range []string{"Hello", " ", "world\n"} {
			encodeTestNotification(t, enc, methodSessionUpdate, map[string]any{
				"sessionId": "sess_space",
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"content": []map[string]any{
						{"type": "text", "text": chunk},
					},
				},
			})
		}
		encodeTestResponse(t, enc, promptMsg.ID, map[string]any{"stopReason": "end_turn"})
	}))
	if err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if result.Output != "Hello world\n" {
		t.Fatalf("Output = %q, want %q", result.Output, "Hello world\n")
	}
	if len(events) != 3 {
		t.Fatalf("events len = %d, want 3", len(events))
	}
	if events[1].Text != " " {
		t.Fatalf("events[1].Text = %q, want single space", events[1].Text)
	}
}

func TestResolveAllowedPath_RejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior varies on windows")
	}

	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatalf("MkdirAll(allowed) error = %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}

	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	link := filepath.Join(allowed, "secret.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("Symlink() unavailable: %v", err)
	}

	if _, err := resolveAllowedPath(link, []string{allowed}); err == nil {
		t.Fatal("resolveAllowedPath() error = nil, want outside allowed roots")
	}
}

func TestReadTextFileContent_RejectsOversizeFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "large.txt")
	data := strings.Repeat("a", maxReadTextFileBytes+1)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile(path) error = %v", err)
	}

	if _, err := readTextFileContent(path, 1, 0); err == nil {
		t.Fatal("readTextFileContent() error = nil, want byte limit")
	}
}

func TestRPCConnStderrString_TruncatesLargeOutput(t *testing.T) {
	t.Parallel()

	conn := &rpcConn{
		stderr: io.NopCloser(strings.NewReader(strings.Repeat("x", maxRPCStderrBytes+32))),
		stderrBuf: cappedTailBuffer{
			limit: maxRPCStderrBytes,
		},
	}
	conn.drainStderr()

	got := conn.stderrString()
	if !strings.HasPrefix(got, "[stderr truncated]\n") {
		t.Fatalf("stderrString() = %q, want truncated prefix", got[:minInt(len(got), 32)])
	}
	if len(got) > len("[stderr truncated]\n")+maxRPCStderrBytes {
		t.Fatalf("stderrString() len = %d, want <= %d", len(got), len("[stderr truncated]\n")+maxRPCStderrBytes)
	}
}

func TestRunPrompt_AuthenticatesWithChatGPTFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := AgentConfig{
		Name:       "helper",
		Enable:     true,
		Type:       "stdio",
		Command:    "helper",
		CWD:        dir,
		ReadRoots:  []string{dir},
		WriteRoots: []string{dir},
	}
	prepared, err := PrepareAgentConfig(cfg, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}

	result, err := runPromptWithFactory(context.Background(), prepared, RunRequest{
		Prompt: "reply with ok",
	}, fakeACPConnFactory(t, func(dec *json.Decoder, enc *json.Encoder) {
		initMsg := decodeTestMessage(t, dec)
		encodeTestResponse(t, enc, initMsg.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"authMethods": []map[string]any{
				{"id": "chatgpt"},
				{
					"id":   "openai-api-key",
					"type": "env_var",
					"vars": []map[string]any{{"name": "OPENAI_API_KEY"}},
				},
			},
		})

		authMsg := decodeTestMessage(t, dec)
		if authMsg.Method != methodAuthenticate {
			t.Fatalf("method = %q, want %q", authMsg.Method, methodAuthenticate)
		}
		var authParams map[string]any
		if err := json.Unmarshal(authMsg.Params, &authParams); err != nil {
			t.Fatalf("json.Unmarshal(authenticate) error = %v", err)
		}
		if got := asString(authParams["methodId"]); got != "chatgpt" {
			t.Fatalf("methodId = %q, want chatgpt", got)
		}
		encodeTestResponse(t, enc, authMsg.ID, map[string]any{})

		newMsg := decodeTestMessage(t, dec)
		encodeTestResponse(t, enc, newMsg.ID, map[string]any{"sessionId": "sess_chatgpt"})

		promptMsg := decodeTestMessage(t, dec)
		encodeTestResponse(t, enc, promptMsg.ID, map[string]any{"stopReason": "end_turn"})
		time.Sleep(10 * time.Millisecond)
	}))
	if err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want end_turn", result.StopReason)
	}
}

func fakeACPConnFactory(t *testing.T, server func(dec *json.Decoder, enc *json.Encoder)) connFactory {
	t.Helper()
	return func(parent context.Context, _ PreparedAgentConfig) (*rpcConn, error) {
		clientSide, serverSide := net.Pipe()
		conn := &rpcConn{
			ctx:     parent,
			cancel:  func() {},
			stdin:   clientSide,
			stdout:  clientSide,
			stderr:  io.NopCloser(bytes.NewReader(nil)),
			enc:     json.NewEncoder(clientSide),
			pending: map[string]chan pendingResponse{},
			done:    make(chan struct{}),
		}
		go conn.readLoop()
		go func() {
			defer serverSide.Close()
			server(json.NewDecoder(serverSide), json.NewEncoder(serverSide))
		}()
		return conn, nil
	}
}

func decodeTestMessage(t *testing.T, dec *json.Decoder) rpcMessage {
	t.Helper()
	var msg rpcMessage
	if err := dec.Decode(&msg); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	return msg
}

func encodeTestRequest(t *testing.T, enc *json.Encoder, id json.RawMessage, method string, params any) {
	t.Helper()
	if err := enc.Encode(rpcMessage{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Method:  method,
		Params:  mustMarshalRaw(params),
	}); err != nil {
		t.Fatalf("Encode(request) error = %v", err)
	}
}

func encodeTestNotification(t *testing.T, enc *json.Encoder, method string, params any) {
	t.Helper()
	if err := enc.Encode(rpcMessage{
		JSONRPC: jsonRPCVersion,
		Method:  method,
		Params:  mustMarshalRaw(params),
	}); err != nil {
		t.Fatalf("Encode(notification) error = %v", err)
	}
}

func encodeTestResponse(t *testing.T, enc *json.Encoder, id json.RawMessage, result any) {
	t.Helper()
	if err := enc.Encode(rpcMessage{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Result:  mustMarshalRaw(result),
	}); err != nil {
		t.Fatalf("Encode(response) error = %v", err)
	}
}

func asString(value any) string {
	s, _ := value.(string)
	return s
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return -1
	}
}

func testTerminalEchoCommand() string {
	switch runtime.GOOS {
	case "windows":
		return "cmd"
	default:
		return "sh"
	}
}

func testTerminalEchoArgs() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"/C", "echo %MM_TERM_TEST%"}
	default:
		return []string{"-lc", `printf '%s\n' "$MM_TERM_TEST"`}
	}
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
