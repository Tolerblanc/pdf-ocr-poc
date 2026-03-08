package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
)

type Options struct {
	InputPDF       string
	OutputDir      string
	Profile        string
	LocalOnly      bool
	MaxWorkers     int
	MaxWorkersMode string
	OnProgress     provider.ProgressHandler
}

type Output struct {
	RunReportPath       string
	LocalOnlyReportPath string
	Result              provider.Result
}

func Execute(ctx context.Context, p provider.Provider, opts Options) (Output, error) {
	if opts.InputPDF == "" {
		return Output{}, errors.New("input pdf is required")
	}
	if opts.OutputDir == "" {
		return Output{}, errors.New("output directory is required")
	}

	if !strings.EqualFold(filepath.Ext(opts.InputPDF), ".pdf") {
		return Output{}, fmt.Errorf("input file must be .pdf: %s", opts.InputPDF)
	}

	if _, err := os.Stat(opts.InputPDF); err != nil {
		return Output{}, fmt.Errorf("input pdf not accessible: %w", err)
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return Output{}, err
	}

	start := time.Now()
	result, err := p.Run(ctx, provider.Request{
		InputPDF:      opts.InputPDF,
		OutputDir:     opts.OutputDir,
		Profile:       opts.Profile,
		LocalOnly:     opts.LocalOnly,
		MaxWorkers:    opts.MaxWorkers,
		WorkersMode:   opts.MaxWorkersMode,
		RequestSource: "ocrpoc-go",
		OnProgress:    opts.OnProgress,
	})
	if err != nil {
		return Output{}, err
	}
	elapsed := time.Since(start).Seconds()
	pages := countPages(result.PagesJSON)
	pagesPerMinute := 0.0
	if pages > 0 && elapsed > 0 {
		pagesPerMinute = (float64(pages) / elapsed) * 60
	}

	runReportPath := filepath.Join(opts.OutputDir, "run_report.json")
	runReport := map[string]any{
		"engine":                p.Name(),
		"input_pdf":             opts.InputPDF,
		"profile":               opts.Profile,
		"effective_max_workers": opts.MaxWorkers,
		"max_workers_mode":      opts.MaxWorkersMode,
		"local_only":            opts.LocalOnly,
		"elapsed_seconds":       elapsed,
		"pages":                 pages,
		"pages_per_minute":      pagesPerMinute,
		"searchable_pdf":        result.SearchablePDF,
		"pages_json":            result.PagesJSON,
		"text_path":             result.TextPath,
		"markdown_path":         result.MarkdownPath,
		"stage_timings":         result.StageTimings,
		"warnings":              result.Warnings,
	}
	if err := writeJSON(runReportPath, runReport); err != nil {
		return Output{}, err
	}

	localOnlyReportPath := filepath.Join(opts.OutputDir, "local_only_report.json")
	selfcheckOK := true
	selfcheckMessage := "local-only monitor not required for this provider"
	if result.LocalOnlySelfcheckSet {
		selfcheckOK = result.LocalOnlySelfcheckOK
		selfcheckMessage = result.LocalOnlySelfcheckMessage
	}
	monitorDuration := result.MonitorDurationSeconds
	if monitorDuration <= 0 {
		monitorDuration = elapsed
	}
	monitorViolations := make([]string, 0, len(result.RemoteConnectionViolations))
	monitorViolations = append(monitorViolations, result.RemoteConnectionViolations...)
	monitorOK := selfcheckOK && len(monitorViolations) == 0

	localOnlyReport := map[string]any{
		"local_only_mode":              opts.LocalOnly,
		"selfcheck_ok":                 selfcheckOK,
		"selfcheck_message":            selfcheckMessage,
		"monitor_samples":              result.MonitorSamples,
		"monitor_duration_seconds":     monitorDuration,
		"remote_connection_violations": monitorViolations,
		"monitor_ok":                   monitorOK,
	}
	if err := writeJSON(localOnlyReportPath, localOnlyReport); err != nil {
		return Output{}, err
	}
	if opts.LocalOnly && !monitorOK {
		return Output{}, fmt.Errorf("local-only violation detected during provider execution")
	}

	return Output{
		RunReportPath:       runReportPath,
		LocalOnlyReportPath: localOnlyReportPath,
		Result:              result,
	}, nil
}

func countPages(pagesJSONPath string) int {
	body, err := os.ReadFile(pagesJSONPath)
	if err != nil {
		return 0
	}

	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0
	}

	rawPages, ok := payload["pages"].([]any)
	if !ok {
		return 0
	}
	return len(rawPages)
}

func writeJSON(path string, payload any) error {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o644)
}
