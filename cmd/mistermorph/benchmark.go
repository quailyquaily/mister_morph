package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/llmbench"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/spf13/cobra"
)

type benchmarkSummary struct {
	ProfileCount   int `json:"profile_count"`
	BenchmarkCount int `json:"benchmark_count"`
	Passed         int `json:"passed"`
	Failed         int `json:"failed"`
	ProfileErrors  int `json:"profile_errors"`
}

type benchmarkOutput struct {
	Profiles []llmbench.ProfileResult `json:"profiles"`
	Summary  benchmarkSummary         `json:"summary"`
}

type benchmarkProgressStage string

const (
	benchmarkProgressProfileStarted benchmarkProgressStage = "profile_started"
	benchmarkProgressBenchmarkDone  benchmarkProgressStage = "benchmark_completed"
	benchmarkProgressProfileFailed  benchmarkProgressStage = "profile_failed"
)

type benchmarkProgressEvent struct {
	Stage          benchmarkProgressStage
	Profile        string
	ProfileIndex   int
	ProfileCount   int
	BenchmarkIndex int
	BenchmarkCount int
	Benchmark      llmbench.BenchmarkResult
	Error          string
}

var runBenchmarkCommand = defaultRunBenchmarkCommand

func newBenchmarkCmd() *cobra.Command {
	var jsonOutput bool
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:          "benchmark [profile-name]",
		Short:        "Benchmark configured LLM profiles",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName := ""
			if len(args) == 1 {
				profileName = strings.TrimSpace(args[0])
			}

			ctx := cmd.Context()
			cancel := func() {}
			if timeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, timeout)
			}
			defer cancel()

			progress := benchmarkProgressPrinter{w: cmd.ErrOrStderr()}
			results, err := runBenchmarkCommand(ctx, profileName, progress.Handle)
			if err != nil {
				return err
			}
			if jsonOutput {
				return writeBenchmarkJSON(cmd.OutOrStdout(), results)
			}
			return writeBenchmarkText(cmd.OutOrStdout(), results)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output benchmark results as JSON.")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Overall timeout for the selected benchmarks (0 disables).")

	return cmd
}

func defaultRunBenchmarkCommand(
	ctx context.Context,
	profileName string,
	onProgress func(benchmarkProgressEvent),
) ([]llmbench.ProfileResult, error) {
	logger, err := logutil.LoggerFromViper()
	if err != nil {
		return nil, err
	}

	values := llmutil.RuntimeValuesFromViper()
	profileName = strings.TrimSpace(profileName)
	names := benchmarkProfileNames(values)
	if profileName != "" {
		names = []string{profileName}
	}

	totalProfiles := len(names)
	totalBenchmarks := totalProfiles * llmbench.BenchmarksPerRun
	completedBenchmarks := 0

	if profileName != "" {
		emitBenchmarkProgress(onProgress, benchmarkProgressEvent{
			Stage:        benchmarkProgressProfileStarted,
			Profile:      profileName,
			ProfileIndex: 1,
			ProfileCount: totalProfiles,
		})
		result, err := benchmarkProfile(ctx, values, logger, profileName, func(benchmark llmbench.BenchmarkResult) {
			completedBenchmarks++
			emitBenchmarkProgress(onProgress, benchmarkProgressEvent{
				Stage:          benchmarkProgressBenchmarkDone,
				Profile:        profileName,
				ProfileIndex:   1,
				ProfileCount:   totalProfiles,
				BenchmarkIndex: completedBenchmarks,
				BenchmarkCount: totalBenchmarks,
				Benchmark:      benchmark,
			})
		})
		if err != nil {
			emitBenchmarkProgress(onProgress, benchmarkProgressEvent{
				Stage:        benchmarkProgressProfileFailed,
				Profile:      profileName,
				ProfileIndex: 1,
				ProfileCount: totalProfiles,
				Error:        err.Error(),
			})
			return nil, err
		}
		return []llmbench.ProfileResult{result}, nil
	}

	results := make([]llmbench.ProfileResult, 0, len(names))
	for i, name := range names {
		emitBenchmarkProgress(onProgress, benchmarkProgressEvent{
			Stage:        benchmarkProgressProfileStarted,
			Profile:      name,
			ProfileIndex: i + 1,
			ProfileCount: totalProfiles,
		})
		result, err := benchmarkProfile(ctx, values, logger, name, func(benchmark llmbench.BenchmarkResult) {
			completedBenchmarks++
			emitBenchmarkProgress(onProgress, benchmarkProgressEvent{
				Stage:          benchmarkProgressBenchmarkDone,
				Profile:        name,
				ProfileIndex:   i + 1,
				ProfileCount:   totalProfiles,
				BenchmarkIndex: completedBenchmarks,
				BenchmarkCount: totalBenchmarks,
				Benchmark:      benchmark,
			})
		})
		if err != nil {
			emitBenchmarkProgress(onProgress, benchmarkProgressEvent{
				Stage:        benchmarkProgressProfileFailed,
				Profile:      name,
				ProfileIndex: i + 1,
				ProfileCount: totalProfiles,
				Error:        err.Error(),
			})
			result.Error = err.Error()
		}
		results = append(results, result)
	}
	return results, nil
}

func benchmarkProfile(
	ctx context.Context,
	values llmutil.RuntimeValues,
	logger *slog.Logger,
	profileName string,
	onBenchmark func(llmbench.BenchmarkResult),
) (llmbench.ProfileResult, error) {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" {
		profileName = llmutil.RouteProfileDefault
	}

	resolved, err := llmutil.ResolveProfile(values, profileName)
	if err != nil {
		return llmbench.ProfileResult{Profile: profileName}, err
	}

	meta := llmbench.ProfileMetadata{
		Profile:  resolved.Name,
		Provider: resolved.ClientConfig.Provider,
		APIBase:  strings.TrimSpace(resolved.ClientConfig.Endpoint),
		Model:    resolved.ClientConfig.Model,
	}

	client, err := buildBenchmarkClient(resolved, logger)
	if err != nil {
		return llmbench.ProfileResult{
			Profile:  meta.Profile,
			Provider: meta.Provider,
			APIBase:  meta.APIBase,
			Model:    meta.Model,
		}, err
	}

	return llmbench.RunWithProgress(ctx, client, meta, onBenchmark), nil
}

func buildBenchmarkClient(profile llmutil.ResolvedProfile, logger *slog.Logger) (llm.Client, error) {
	client, err := llmutil.ClientFromConfigWithValues(profile.ClientConfig, profile.Values)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return llmstats.WrapRuntimeClient(
		client,
		profile.ClientConfig.Provider,
		profile.ClientConfig.Endpoint,
		profile.ClientConfig.Model,
		logger,
	), nil
}

func benchmarkProfileNames(values llmutil.RuntimeValues) []string {
	names := make([]string, 0, 1+len(values.Profiles))
	if hasBenchmarkableDefaultProfile(values) {
		names = append(names, llmutil.RouteProfileDefault)
	}
	for name := range values.Profiles {
		name = strings.TrimSpace(name)
		if name == "" || name == llmutil.RouteProfileDefault {
			continue
		}
		names = append(names, name)
	}
	if len(names) > 1 {
		sort.Strings(names[1:])
	}
	return names
}

func hasBenchmarkableDefaultProfile(values llmutil.RuntimeValues) bool {
	if !hasExplicitDefaultProfile(values) {
		return false
	}
	resolved, err := llmutil.ResolveProfile(values, llmutil.RouteProfileDefault)
	if err != nil {
		return false
	}
	_, err = buildBenchmarkClient(resolved, nil)
	return err == nil
}

func hasExplicitDefaultProfile(values llmutil.RuntimeValues) bool {
	return strings.TrimSpace(values.Provider) != "" ||
		strings.TrimSpace(values.Endpoint) != "" ||
		strings.TrimSpace(values.APIKey) != "" ||
		strings.TrimSpace(values.Model) != "" ||
		len(values.Headers) > 0 ||
		strings.TrimSpace(values.AzureDeployment) != "" ||
		strings.TrimSpace(values.RequestTimeoutRaw) != "" ||
		strings.TrimSpace(values.ToolsEmulationMode) != "" ||
		strings.TrimSpace(values.TemperatureRaw) != "" ||
		strings.TrimSpace(values.ReasoningEffortRaw) != "" ||
		strings.TrimSpace(values.ReasoningBudgetRaw) != "" ||
		strings.TrimSpace(values.BedrockAWSKey) != "" ||
		strings.TrimSpace(values.BedrockAWSSecret) != "" ||
		strings.TrimSpace(values.BedrockAWSRegion) != "" ||
		strings.TrimSpace(values.BedrockModelARN) != "" ||
		strings.TrimSpace(values.CloudflareAccountID) != "" ||
		strings.TrimSpace(values.CloudflareAPIToken) != ""
}

func summarizeBenchmarkResults(results []llmbench.ProfileResult) benchmarkSummary {
	summary := benchmarkSummary{
		ProfileCount: len(results),
	}
	for _, result := range results {
		if strings.TrimSpace(result.Error) != "" {
			summary.ProfileErrors++
			continue
		}
		for _, benchmark := range result.Benchmarks {
			summary.BenchmarkCount++
			if benchmark.OK {
				summary.Passed++
				continue
			}
			summary.Failed++
		}
	}
	return summary
}

func writeBenchmarkJSON(w io.Writer, results []llmbench.ProfileResult) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(benchmarkOutput{
		Profiles: results,
		Summary:  summarizeBenchmarkResults(results),
	})
}

func writeBenchmarkText(w io.Writer, results []llmbench.ProfileResult) error {
	for i, result := range results {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}

		if err := writeBenchmarkField(w, "Profile", result.Profile); err != nil {
			return err
		}
		if err := writeOptionalBenchmarkField(w, "Provider", result.Provider); err != nil {
			return err
		}
		if err := writeOptionalBenchmarkField(w, "API Base", result.APIBase); err != nil {
			return err
		}
		if err := writeOptionalBenchmarkField(w, "Model", result.Model); err != nil {
			return err
		}
		if strings.TrimSpace(result.Error) != "" {
			if err := writeBenchmarkField(w, "Error", result.Error); err != nil {
				return err
			}
			continue
		}

		if len(result.Benchmarks) == 0 {
			if err := writeBenchmarkField(w, "Benchmarks", "none"); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if err := writeBenchmarkTable(w, result.Benchmarks); err != nil {
			return err
		}
	}

	summary := summarizeBenchmarkResults(results)
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Summary"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "-------"); err != nil {
		return err
	}
	if err := writeSummaryField(w, "Profiles", summary.ProfileCount); err != nil {
		return err
	}
	if err := writeSummaryField(w, "Benchmarks", summary.BenchmarkCount); err != nil {
		return err
	}
	if err := writeSummaryField(w, "Passed", summary.Passed); err != nil {
		return err
	}
	if err := writeSummaryField(w, "Failed", summary.Failed); err != nil {
		return err
	}
	return writeSummaryField(w, "Profile Errors", summary.ProfileErrors)
}

func benchmarkStatusText(ok bool) string {
	if ok {
		return "OK"
	}
	return "FAILED"
}

func formatBenchmarkSeconds(durationMS int64) string {
	return fmt.Sprintf("%.3fs", float64(durationMS)/1000)
}

func writeBenchmarkField(w io.Writer, label, value string) error {
	_, err := fmt.Fprintf(w, "%-8s  %s\n", strings.TrimSpace(label), strings.TrimSpace(value))
	return err
}

func writeOptionalBenchmarkField(w io.Writer, label, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return writeBenchmarkField(w, label, value)
}

func writeSummaryField(w io.Writer, label string, value int) error {
	_, err := fmt.Fprintf(w, "%-14s %d\n", strings.TrimSpace(label), value)
	return err
}

func emitBenchmarkProgress(onProgress func(benchmarkProgressEvent), event benchmarkProgressEvent) {
	if onProgress != nil {
		onProgress(event)
	}
}

func writeBenchmarkTable(w io.Writer, benchmarks []llmbench.BenchmarkResult) error {
	nameWidth := len("Benchmark")
	statusWidth := len("Status")
	timeWidth := len("Time")
	for _, benchmark := range benchmarks {
		if width := len(strings.TrimSpace(benchmark.ID)); width > nameWidth {
			nameWidth = width
		}
		if width := len(benchmarkStatusText(benchmark.OK)); width > statusWidth {
			statusWidth = width
		}
		if width := len(formatBenchmarkSeconds(benchmark.DurationMS)); width > timeWidth {
			timeWidth = width
		}
	}

	if _, err := fmt.Fprintf(
		w,
		"  %-*s  %-*s  %-*s  %s\n",
		nameWidth,
		"Benchmark",
		statusWidth,
		"Status",
		timeWidth,
		"Time",
		"Detail",
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		w,
		"  %-*s  %-*s  %-*s  %s\n",
		nameWidth,
		strings.Repeat("-", nameWidth),
		statusWidth,
		strings.Repeat("-", statusWidth),
		timeWidth,
		strings.Repeat("-", timeWidth),
		"------",
	); err != nil {
		return err
	}

	for _, benchmark := range benchmarks {
		if _, err := fmt.Fprintf(
			w,
			"  %-*s  %-*s  %-*s  %s\n",
			nameWidth,
			strings.TrimSpace(benchmark.ID),
			statusWidth,
			benchmarkStatusText(benchmark.OK),
			timeWidth,
			formatBenchmarkSeconds(benchmark.DurationMS),
			benchmarkDetailText(benchmark),
		); err != nil {
			return err
		}
	}
	return nil
}

func benchmarkDetailText(result llmbench.BenchmarkResult) string {
	if !result.OK {
		if text := strings.TrimSpace(result.Error); text != "" {
			return text
		}
	}
	if text := strings.TrimSpace(result.Detail); text != "" {
		return text
	}
	return "-"
}

type benchmarkProgressPrinter struct {
	w io.Writer
}

func (p benchmarkProgressPrinter) Handle(event benchmarkProgressEvent) {
	if p.w == nil {
		return
	}

	switch event.Stage {
	case benchmarkProgressProfileStarted:
		_, _ = fmt.Fprintf(
			p.w,
			"==> [%d/%d] %s\n",
			event.ProfileIndex,
			event.ProfileCount,
			strings.TrimSpace(event.Profile),
		)
	case benchmarkProgressBenchmarkDone:
		_, _ = fmt.Fprintf(
			p.w,
			"    [%d/%d] %-13s %-6s %-7s %s\n",
			event.BenchmarkIndex,
			event.BenchmarkCount,
			strings.TrimSpace(event.Benchmark.ID),
			benchmarkStatusText(event.Benchmark.OK),
			formatBenchmarkSeconds(event.Benchmark.DurationMS),
			benchmarkDetailText(event.Benchmark),
		)
	case benchmarkProgressProfileFailed:
		_, _ = fmt.Fprintf(
			p.w,
			"    error: %s\n",
			strings.TrimSpace(event.Error),
		)
	}
}
