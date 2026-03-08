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

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/postprocess"
	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
)

type Options struct {
	InputPDF               string
	OutputDir              string
	Profile                string
	LocalOnly              bool
	PostprocessAllowRemote bool
	MaxWorkers             int
	MaxWorkersMode         string
	PostprocessProvider    string
	PostprocessConfigPath  string
	OnProgress             provider.ProgressHandler
}

type Output struct {
	RunReportPath       string
	LocalOnlyReportPath string
	Result              provider.Result
	Postprocess         postprocess.Output
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

	resolvedPostprocess, err := postprocess.ResolveConfig(opts.PostprocessProvider, opts.PostprocessConfigPath)
	if err != nil {
		return Output{}, err
	}
	if err := postprocess.ValidateExecution(resolvedPostprocess, opts.PostprocessAllowRemote); err != nil {
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

	ocrElapsed := time.Since(start).Seconds()
	localOnlyReportPath := filepath.Join(opts.OutputDir, "local_only_report.json")
	monitorOK, err := writeLocalOnlyReport(localOnlyReportPath, opts.LocalOnly, result, ocrElapsed)
	if err != nil {
		return Output{}, err
	}
	if opts.LocalOnly && !monitorOK {
		return Output{}, fmt.Errorf("local-only violation detected during provider execution")
	}

	postprocessProvider, err := postprocess.New(resolvedPostprocess.Config.Provider)
	if err != nil {
		return Output{}, err
	}
	postprocessOutput, err := postprocess.Execute(ctx, postprocessProvider, postprocess.Request{
		InputPDF:    opts.InputPDF,
		OutputDir:   opts.OutputDir,
		OCRProvider: p.Name(),
		OCRResult:   result,
		Config:      resolvedPostprocess.Config,
		AllowRemote: opts.PostprocessAllowRemote,
		OnProgress:  opts.OnProgress,
	})
	if err != nil {
		return Output{}, err
	}
	if postprocessOutput.OutputMode == postprocess.OutputModePrimaryArtifacts {
		if supportsPrimaryArtifactRegeneration(result) {
			result, err = rebuildPrimaryArtifacts(ctx, p, result, opts, postprocessOutput.CorrectedPagesJSON)
			if err != nil {
				return Output{}, err
			}
		} else {
			postprocessOutput.OutputMode = postprocess.OutputModeSidecarOnly
			postprocessOutput.Warnings = mergeWarnings(
				postprocessOutput.Warnings,
				[]string{"primary_artifacts_not_supported_by_provider"},
			)
			postprocessOutput.Document.Postprocess.OutputMode = postprocess.OutputModeSidecarOnly
			postprocessOutput.Document.Postprocess.Warnings = postprocessOutput.Warnings
			if err := rewriteCorrectedPagesDocument(postprocessOutput.CorrectedPagesJSON, postprocessOutput.Document); err != nil {
				return Output{}, err
			}
		}
	}

	stageTimings := mergeStageTimings(result.StageTimings, postprocessOutput.StageTimings)
	warnings := mergeWarnings(result.Warnings, postprocessOutput.Warnings)
	result.StageTimings = stageTimings
	result.Warnings = warnings
	elapsed := time.Since(start).Seconds()
	pages := countPages(result.PagesJSON)
	pagesPerMinute := 0.0
	if pages > 0 && elapsed > 0 {
		pagesPerMinute = (float64(pages) / elapsed) * 60
	}

	runReportPath := filepath.Join(opts.OutputDir, "run_report.json")
	runReport := map[string]any{
		"engine":                    p.Name(),
		"input_pdf":                 opts.InputPDF,
		"profile":                   opts.Profile,
		"effective_max_workers":     opts.MaxWorkers,
		"max_workers_mode":          opts.MaxWorkersMode,
		"local_only":                opts.LocalOnly,
		"ocr_local_only":            opts.LocalOnly,
		"postprocess_allow_remote":  opts.PostprocessAllowRemote,
		"elapsed_seconds":           elapsed,
		"pages":                     pages,
		"pages_per_minute":          pagesPerMinute,
		"searchable_pdf":            result.SearchablePDF,
		"pages_json":                result.PagesJSON,
		"text_path":                 result.TextPath,
		"markdown_path":             result.MarkdownPath,
		"stage_timings":             stageTimings,
		"warnings":                  warnings,
		"postprocess_provider":      postprocessProvider.Name(),
		"postprocess_config_path":   resolvedPostprocess.Path,
		"postprocess_profile":       resolvedPostprocess.Profile,
		"postprocess_applied":       postprocessOutput.Applied,
		"postprocess_changed_pages": postprocessOutput.ChangedPages,
		"corrected_pages_json":      postprocessOutput.CorrectedPagesJSON,
		"corrected_text_path":       postprocessOutput.CorrectedTextPath,
		"corrected_markdown_path":   postprocessOutput.CorrectedMarkdownPath,
		"postprocess_output_mode":   postprocessOutput.OutputMode,
		"postprocess_warnings":      postprocessOutput.Warnings,
	}
	if err := writeJSON(runReportPath, runReport); err != nil {
		return Output{}, err
	}

	return Output{
		RunReportPath:       runReportPath,
		LocalOnlyReportPath: localOnlyReportPath,
		Result:              result,
		Postprocess:         postprocessOutput,
	}, nil
}

func writeLocalOnlyReport(path string, localOnly bool, result provider.Result, elapsed float64) (bool, error) {
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
		"scope":                        "ocr_provider",
		"local_only_mode":              localOnly,
		"ocr_local_only_mode":          localOnly,
		"selfcheck_ok":                 selfcheckOK,
		"selfcheck_message":            selfcheckMessage,
		"monitor_samples":              result.MonitorSamples,
		"monitor_duration_seconds":     monitorDuration,
		"remote_connection_violations": monitorViolations,
		"monitor_ok":                   monitorOK,
	}
	if err := writeJSON(path, localOnlyReport); err != nil {
		return false, err
	}
	return monitorOK, nil
}
func rebuildPrimaryArtifacts(
	ctx context.Context,
	p provider.Provider,
	current provider.Result,
	opts Options,
	correctedPagesJSON string,
) (provider.Result, error) {
	if strings.TrimSpace(correctedPagesJSON) == "" {
		return provider.Result{}, errors.New("primary artifact regeneration requires corrected_pages.json")
	}

	rebuilt, err := p.Run(ctx, provider.Request{
		InputPDF:           opts.InputPDF,
		OutputDir:          opts.OutputDir,
		Profile:            opts.Profile,
		LocalOnly:          opts.LocalOnly,
		MaxWorkers:         opts.MaxWorkers,
		WorkersMode:        opts.MaxWorkersMode,
		CorrectedPagesJSON: correctedPagesJSON,
		RequestSource:      "ocrpoc-go/postprocess-primary-artifacts",
		OnProgress:         opts.OnProgress,
	})
	if err != nil {
		return provider.Result{}, err
	}
	if rebuilt.ArtifactSource != provider.ArtifactSourceCorrectedPages {
		return provider.Result{}, fmt.Errorf(
			"provider %s did not confirm corrected_pages artifact regeneration",
			p.Name(),
		)
	}
	return mergeProviderResults(current, rebuilt), nil
}

func supportsPrimaryArtifactRegeneration(result provider.Result) bool {
	return result.Capabilities != nil && result.Capabilities.CorrectedArtifactRebuild
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

func rewriteCorrectedPagesDocument(path string, doc postprocess.Document) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return writeJSON(path, doc)
}

func mergeStageTimings(groups ...map[string]float64) map[string]float64 {
	merged := map[string]float64{}
	for _, group := range groups {
		for key, value := range group {
			merged[key] = value
		}
	}
	return merged
}

func mergeWarnings(groups ...[]string) []string {
	seen := map[string]struct{}{}
	merged := []string{}
	for _, group := range groups {
		for _, warning := range group {
			warning = strings.TrimSpace(warning)
			if warning == "" {
				continue
			}
			if _, ok := seen[warning]; ok {
				continue
			}
			seen[warning] = struct{}{}
			merged = append(merged, warning)
		}
	}
	return merged
}

func mergeProviderResults(base, overlay provider.Result) provider.Result {
	merged := overlay
	if merged.SearchablePDF == "" {
		merged.SearchablePDF = base.SearchablePDF
	}
	if merged.PagesJSON == "" {
		merged.PagesJSON = base.PagesJSON
	}
	if merged.TextPath == "" {
		merged.TextPath = base.TextPath
	}
	if merged.MarkdownPath == "" {
		merged.MarkdownPath = base.MarkdownPath
	}
	merged.StageTimings = sumStageTimings(base.StageTimings, overlay.StageTimings)
	merged.Warnings = mergeWarnings(base.Warnings, overlay.Warnings)
	merged.MonitorSamples = base.MonitorSamples + overlay.MonitorSamples
	merged.MonitorDurationSeconds = base.MonitorDurationSeconds + overlay.MonitorDurationSeconds
	merged.RemoteConnectionViolations = mergeWarnings(base.RemoteConnectionViolations, overlay.RemoteConnectionViolations)
	if overlay.LocalOnlySelfcheckSet {
		merged.LocalOnlySelfcheckSet = true
		merged.LocalOnlySelfcheckOK = overlay.LocalOnlySelfcheckOK
		if base.LocalOnlySelfcheckSet {
			merged.LocalOnlySelfcheckOK = base.LocalOnlySelfcheckOK && overlay.LocalOnlySelfcheckOK
		}
		merged.LocalOnlySelfcheckMessage = overlay.LocalOnlySelfcheckMessage
		if strings.TrimSpace(merged.LocalOnlySelfcheckMessage) == "" {
			merged.LocalOnlySelfcheckMessage = base.LocalOnlySelfcheckMessage
		}
	} else {
		merged.LocalOnlySelfcheckSet = base.LocalOnlySelfcheckSet
		merged.LocalOnlySelfcheckOK = base.LocalOnlySelfcheckOK
		merged.LocalOnlySelfcheckMessage = base.LocalOnlySelfcheckMessage
	}
	return merged
}

func sumStageTimings(groups ...map[string]float64) map[string]float64 {
	merged := map[string]float64{}
	for _, group := range groups {
		for key, value := range group {
			merged[key] += value
		}
	}
	return merged
}
