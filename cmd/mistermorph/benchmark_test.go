package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmbench"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
)

func TestBenchmarkCmdTextOutput(t *testing.T) {
	prev := runBenchmarkCommand
	runBenchmarkCommand = func(_ context.Context, profileName string, _ func(benchmarkProgressEvent)) ([]llmbench.ProfileResult, error) {
		if profileName != "cheap" {
			t.Fatalf("profileName = %q, want cheap", profileName)
		}
		return []llmbench.ProfileResult{
			{
				Profile:  "cheap",
				Provider: "openai",
				APIBase:  "https://api.openai.com/v1",
				Model:    "gpt-5-mini",
				Benchmarks: []llmbench.BenchmarkResult{
					{ID: "text_reply", OK: true, DurationMS: 912, Detail: "OK"},
					{ID: "tool_calling", OK: false, DurationMS: 1350, Error: "model replied without calling the tool"},
				},
			},
		}, nil
	}
	t.Cleanup(func() {
		runBenchmarkCommand = prev
	})

	cmd := newBenchmarkCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"cheap"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"Profile   cheap",
		"Provider  openai",
		"API Base  https://api.openai.com/v1",
		"Model     gpt-5-mini",
		"Benchmark     Status  Time    Detail",
		"text_reply    OK      0.912s  OK",
		"tool_calling  FAILED  1.350s  model replied without calling the tool",
		"Summary",
		"Profiles       1",
		"Benchmarks     2",
		"Passed         1",
		"Failed         1",
		"Profile Errors 0",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestBenchmarkCmdJSONOutput(t *testing.T) {
	prev := runBenchmarkCommand
	runBenchmarkCommand = func(_ context.Context, profileName string, _ func(benchmarkProgressEvent)) ([]llmbench.ProfileResult, error) {
		if profileName != "" {
			t.Fatalf("profileName = %q, want empty", profileName)
		}
		return []llmbench.ProfileResult{
			{
				Profile:  "default",
				Provider: "openai",
				Model:    "gpt-5",
				Benchmarks: []llmbench.BenchmarkResult{
					{ID: "text_reply", OK: true, DurationMS: 1},
				},
			},
			{
				Profile: "broken",
				Error:   "missing env \"BROKEN_API_KEY\"",
			},
		}, nil
	}
	t.Cleanup(func() {
		runBenchmarkCommand = prev
	})

	cmd := newBenchmarkCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var payload benchmarkOutput
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput=%s", err, out.String())
	}
	if len(payload.Profiles) != 2 {
		t.Fatalf("len(payload.Profiles) = %d, want 2", len(payload.Profiles))
	}
	if payload.Summary.ProfileCount != 2 {
		t.Fatalf("ProfileCount = %d, want 2", payload.Summary.ProfileCount)
	}
	if payload.Summary.BenchmarkCount != 1 {
		t.Fatalf("BenchmarkCount = %d, want 1", payload.Summary.BenchmarkCount)
	}
	if payload.Summary.Passed != 1 || payload.Summary.Failed != 0 || payload.Summary.ProfileErrors != 1 {
		t.Fatalf("unexpected summary: %+v", payload.Summary)
	}
}

func TestBenchmarkCmdProgressOutput(t *testing.T) {
	prev := runBenchmarkCommand
	runBenchmarkCommand = func(_ context.Context, profileName string, onProgress func(benchmarkProgressEvent)) ([]llmbench.ProfileResult, error) {
		if profileName != "" {
			t.Fatalf("profileName = %q, want empty", profileName)
		}
		onProgress(benchmarkProgressEvent{
			Stage:        benchmarkProgressProfileStarted,
			Profile:      "default",
			ProfileIndex: 1,
			ProfileCount: 2,
		})
		onProgress(benchmarkProgressEvent{
			Stage:          benchmarkProgressBenchmarkDone,
			Profile:        "default",
			ProfileIndex:   1,
			ProfileCount:   2,
			BenchmarkIndex: 1,
			BenchmarkCount: 6,
			Benchmark: llmbench.BenchmarkResult{
				ID:         "text_reply",
				OK:         true,
				DurationMS: 912,
				Detail:     "OK",
			},
		})
		onProgress(benchmarkProgressEvent{
			Stage:        benchmarkProgressProfileStarted,
			Profile:      "broken",
			ProfileIndex: 2,
			ProfileCount: 2,
		})
		onProgress(benchmarkProgressEvent{
			Stage:        benchmarkProgressProfileFailed,
			Profile:      "broken",
			ProfileIndex: 2,
			ProfileCount: 2,
			Error:        "missing env \"BROKEN_API_KEY\"",
		})
		return []llmbench.ProfileResult{
			{
				Profile: "default",
				Benchmarks: []llmbench.BenchmarkResult{
					{ID: "text_reply", OK: true, DurationMS: 912, Detail: "OK"},
				},
			},
			{
				Profile: "broken",
				Error:   "missing env \"BROKEN_API_KEY\"",
			},
		}, nil
	}
	t.Cleanup(func() {
		runBenchmarkCommand = prev
	})

	cmd := newBenchmarkCmd()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	got := errOut.String()
	for _, want := range []string{
		"==> [1/2] default",
		"[1/6] text_reply    OK",
		"==> [2/2] broken",
		"error: missing env \"BROKEN_API_KEY\"",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("progress output missing %q:\n%s", want, got)
		}
	}
}

func TestBenchmarkProfileNames_SkipsUnusableDefault(t *testing.T) {
	values := llmutil.RuntimeValues{
		Profiles: map[string]llmutil.ProfileConfig{
			"cheap": {
				Provider: "openai",
				Model:    "gpt-5-mini",
			},
			"strong": {
				Provider: "openai",
				Model:    "gpt-5.2",
			},
		},
	}

	got := benchmarkProfileNames(values)
	want := []string{"cheap", "strong"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestBenchmarkProfileNames_IncludesUsableDefaultFirst(t *testing.T) {
	values := llmutil.RuntimeValues{
		Provider: "openai",
		Model:    "gpt-5",
		Profiles: map[string]llmutil.ProfileConfig{
			"cheap": {
				Provider: "openai",
				Model:    "gpt-5-mini",
			},
		},
	}

	got := benchmarkProfileNames(values)
	want := []string{"default", "cheap"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}
