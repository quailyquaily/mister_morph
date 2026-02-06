package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"
)

func main() {
	var (
		task   = flag.String("task", "Search for OpenAI and fetch the first result.", "Task to run.")
		model  = flag.String("model", "gpt-5.2", "Model name.")
		apiKey = flag.String("api-key", os.Getenv("OPENAI_API_KEY"), "API key (defaults to OPENAI_API_KEY).")
	)
	flag.Parse()

	bin := os.Getenv("MISTER_MORPH_BIN")
	if bin == "" {
		var err error
		bin, err = exec.LookPath("mistermorph")
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "Set MISTER_MORPH_BIN to the built mistermorph binary, or add it to PATH.")
			os.Exit(2)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin,
		"run",
		"--task", *task,
		"--provider", "openai",
		"--model", *model,
		"--api-key", *apiKey,
		"--log-level", "info",
		"--log-format", "text",
	)

	// Stream agent logs to our stderr.
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	var out any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "failed to parse agent output as JSON:", err.Error())
		_, _ = fmt.Fprintln(os.Stderr, "raw stdout:\n"+stdout.String())
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}
