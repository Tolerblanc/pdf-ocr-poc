package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
	runpkg "github.com/Tolerblanc/pdf-ocr-poc/v2/internal/run"
)

type workerSpec struct {
	Label      string `json:"label"`
	MaxWorkers int    `json:"max_workers"`
	Mode       string `json:"mode"`
}

type runReport struct {
	Engine              string             `json:"engine"`
	InputPDF            string             `json:"input_pdf"`
	Profile             string             `json:"profile"`
	EffectiveMaxWorkers int                `json:"effective_max_workers"`
	MaxWorkersMode      string             `json:"max_workers_mode"`
	LocalOnly           bool               `json:"local_only"`
	ElapsedSeconds      float64            `json:"elapsed_seconds"`
	Pages               int                `json:"pages"`
	PagesPerMinute      float64            `json:"pages_per_minute"`
	StageTimings        map[string]float64 `json:"stage_timings"`
	SearchablePDF       string             `json:"searchable_pdf"`
	PagesJSON           string             `json:"pages_json"`
	TextPath            string             `json:"text_path"`
	MarkdownPath        string             `json:"markdown_path"`
	Warnings            []string           `json:"warnings"`
}

type runSummary struct {
	Label                string     `json:"label"`
	OutputDir            string     `json:"output_dir"`
	RunReportPath        string     `json:"run_report_path"`
	LocalOnlyReportPath  string     `json:"local_only_report_path"`
	CompletedAt          string     `json:"completed_at"`
	EffectiveMaxWorkers  int        `json:"effective_max_workers"`
	MaxWorkersMode       string     `json:"max_workers_mode"`
	ElapsedSeconds       float64    `json:"elapsed_seconds"`
	Pages                int        `json:"pages"`
	PagesPerMinute       float64    `json:"pages_per_minute"`
	VisionOCRSeconds     float64    `json:"vision_ocr_seconds"`
	SearchablePDFSeconds float64    `json:"searchable_pdf_seconds"`
	ProviderTotalSeconds float64    `json:"provider_total_seconds"`
	Warnings             []string   `json:"warnings,omitempty"`
	SpeedupVsBaseline    float64    `json:"speedup_vs_baseline,omitempty"`
	ThroughputVsBaseline float64    `json:"throughput_vs_baseline,omitempty"`
	Spec                 workerSpec `json:"spec"`
}

type benchmarkSummary struct {
	GeneratedAt string       `json:"generated_at"`
	InputPDF    string       `json:"input_pdf"`
	OutputRoot  string       `json:"output_root"`
	Profile     string       `json:"profile"`
	LocalOnly   bool         `json:"local_only"`
	Runs        []runSummary `json:"runs"`
}

func main() {
	if err := runMain(); err != nil {
		fmt.Fprintf(os.Stderr, "benchmax error: %v\n", err)
		os.Exit(1)
	}
}

func runMain() error {
	fs := flag.NewFlagSet("ocrpoc-benchmax", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	input := fs.String("input", "", "input PDF path")
	outRoot := fs.String("out-root", "", "output root directory")
	providerName := fs.String("provider", "vision-swift", "provider name")
	providerBin := fs.String("provider-bin", "", "provider executable path")
	profile := fs.String("profile", "fast", "profile name")
	values := fs.String("values", "1,2,4,8", "comma-separated max-workers values, supports auto")
	localOnly := fs.Bool("local-only", true, "enable local-only mode")
	reuseExisting := fs.Bool("reuse-existing", false, "reuse existing run_report.json under each output directory")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(*input) == "" {
		return errors.New("--input is required")
	}
	if strings.TrimSpace(*outRoot) == "" {
		return errors.New("--out-root is required")
	}

	specs, err := parseWorkerSpecs(*values, *providerName, *providerBin)
	if err != nil {
		return err
	}
	if _, err := os.Stat(*input); err != nil {
		return fmt.Errorf("input pdf not accessible: %w", err)
	}
	if err := os.MkdirAll(*outRoot, 0o755); err != nil {
		return err
	}

	p, err := provider.New(*providerName, *providerBin)
	if err != nil {
		return err
	}

	summary := benchmarkSummary{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		InputPDF:    *input,
		OutputRoot:  *outRoot,
		Profile:     *profile,
		LocalOnly:   *localOnly,
		Runs:        make([]runSummary, 0, len(specs)),
	}

	for idx, spec := range specs {
		fmt.Fprintf(
			os.Stderr,
			"[%d/%d] label=%s max-workers=%d mode=%s\n",
			idx+1,
			len(specs),
			spec.Label,
			spec.MaxWorkers,
			spec.Mode,
		)
		run, err := executeBenchmarkRun(context.Background(), p, *input, *outRoot, *profile, *localOnly, spec, *reuseExisting)
		if err != nil {
			return err
		}
		summary.Runs = append(summary.Runs, run)
	}

	annotateRelativeMetrics(summary.Runs)
	summary.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	jsonPath := filepath.Join(*outRoot, "summary.json")
	if err := writeJSON(jsonPath, summary); err != nil {
		return err
	}
	markdownPath := filepath.Join(*outRoot, "summary.md")
	if err := os.WriteFile(markdownPath, []byte(formatMarkdownSummary(summary)), 0o644); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "summary_json=%s\n", jsonPath)
	fmt.Fprintf(os.Stdout, "summary_md=%s\n", markdownPath)
	fmt.Fprintln(os.Stdout)
	fmt.Fprint(os.Stdout, formatMarkdownSummary(summary))
	return nil
}

func parseWorkerSpecs(values, providerName, providerBin string) ([]workerSpec, error) {
	tokens := strings.Split(values, ",")
	specs := make([]workerSpec, 0, len(tokens))
	seen := map[string]struct{}{}
	for _, token := range tokens {
		value := strings.TrimSpace(token)
		if value == "" {
			continue
		}
		if strings.EqualFold(value, "auto") {
			maxWorkers := provider.ResolveAutoMaxWorkers(providerName, providerBin)
			label := "auto"
			if _, exists := seen[label]; exists {
				return nil, fmt.Errorf("duplicate worker spec: %s", value)
			}
			seen[label] = struct{}{}
			specs = append(specs, workerSpec{Label: label, MaxWorkers: maxWorkers, Mode: "auto"})
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("invalid worker value %q", value)
		}
		if parsed < 1 {
			return nil, fmt.Errorf("worker value must be >= 1: %d", parsed)
		}
		label := fmt.Sprintf("w%d", parsed)
		if _, exists := seen[label]; exists {
			return nil, fmt.Errorf("duplicate worker spec: %s", value)
		}
		seen[label] = struct{}{}
		specs = append(specs, workerSpec{Label: label, MaxWorkers: parsed, Mode: "manual"})
	}
	if len(specs) == 0 {
		return nil, errors.New("at least one worker value is required")
	}
	return specs, nil
}

func executeBenchmarkRun(
	ctx context.Context,
	p provider.Provider,
	inputPDF string,
	outRoot string,
	profile string,
	localOnly bool,
	spec workerSpec,
	reuseExisting bool,
) (runSummary, error) {
	outputDir := filepath.Join(outRoot, spec.Label)
	reportPath := filepath.Join(outputDir, "run_report.json")
	localOnlyReportPath := filepath.Join(outputDir, "local_only_report.json")
	if reuseExisting {
		if report, err := loadRunReport(reportPath); err == nil {
			return buildRunSummary(spec, outputDir, reportPath, localOnlyReportPath, report), nil
		}
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return runSummary{}, err
	}
	if _, err := runpkg.Execute(ctx, p, runpkg.Options{
		InputPDF:       inputPDF,
		OutputDir:      outputDir,
		Profile:        profile,
		LocalOnly:      localOnly,
		MaxWorkers:     spec.MaxWorkers,
		MaxWorkersMode: spec.Mode,
	}); err != nil {
		return runSummary{}, err
	}
	report, err := loadRunReport(reportPath)
	if err != nil {
		return runSummary{}, err
	}
	return buildRunSummary(spec, outputDir, reportPath, localOnlyReportPath, report), nil
}

func buildRunSummary(spec workerSpec, outputDir, reportPath, localOnlyReportPath string, report runReport) runSummary {
	return runSummary{
		Label:                spec.Label,
		OutputDir:            outputDir,
		RunReportPath:        reportPath,
		LocalOnlyReportPath:  localOnlyReportPath,
		CompletedAt:          time.Now().UTC().Format(time.RFC3339),
		EffectiveMaxWorkers:  report.EffectiveMaxWorkers,
		MaxWorkersMode:       report.MaxWorkersMode,
		ElapsedSeconds:       report.ElapsedSeconds,
		Pages:                report.Pages,
		PagesPerMinute:       report.PagesPerMinute,
		VisionOCRSeconds:     report.StageTimings["vision_ocr_seconds"],
		SearchablePDFSeconds: report.StageTimings["searchable_pdf_seconds"],
		ProviderTotalSeconds: report.StageTimings["provider_total_seconds"],
		Warnings:             append([]string(nil), report.Warnings...),
		Spec:                 spec,
	}
}

func loadRunReport(path string) (runReport, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return runReport{}, err
	}
	var report runReport
	if err := json.Unmarshal(body, &report); err != nil {
		return runReport{}, err
	}
	return report, nil
}

func annotateRelativeMetrics(runs []runSummary) {
	if len(runs) == 0 {
		return
	}
	baseline := runs[0]
	if baseline.ElapsedSeconds <= 0 {
		return
	}
	for idx := range runs {
		runs[idx].SpeedupVsBaseline = baseline.ElapsedSeconds / maxFloat(runs[idx].ElapsedSeconds, 0.000001)
		if baseline.PagesPerMinute > 0 {
			runs[idx].ThroughputVsBaseline = runs[idx].PagesPerMinute / baseline.PagesPerMinute
		}
	}
}

func formatMarkdownSummary(summary benchmarkSummary) string {
	var b strings.Builder
	b.WriteString("# Max Workers Benchmark\n\n")
	b.WriteString(fmt.Sprintf("- Generated at: `%s`\n", summary.GeneratedAt))
	b.WriteString(fmt.Sprintf("- Input: `%s`\n", summary.InputPDF))
	b.WriteString(fmt.Sprintf("- Profile: `%s`\n", summary.Profile))
	b.WriteString(fmt.Sprintf("- Local only: `%t`\n\n", summary.LocalOnly))
	b.WriteString("| Label | Max Workers | Mode | Seconds | Pages/min | OCR(s) | Searchable(s) | Speedup | Throughput |\n")
	b.WriteString("|---|---:|---|---:|---:|---:|---:|---:|---:|\n")

	runs := append([]runSummary(nil), summary.Runs...)
	sort.SliceStable(runs, func(i, j int) bool {
		return runs[i].ElapsedSeconds < runs[j].ElapsedSeconds
	})
	bestLabel := ""
	if len(runs) > 0 {
		bestLabel = runs[0].Label
	}
	for _, run := range summary.Runs {
		speedup := fmt.Sprintf("%.2fx", run.SpeedupVsBaseline)
		throughput := fmt.Sprintf("%.2fx", run.ThroughputVsBaseline)
		b.WriteString(fmt.Sprintf(
			"| `%s` | %d | %s | %.3f | %.3f | %.3f | %.3f | %s | %s |\n",
			run.Label,
			run.EffectiveMaxWorkers,
			run.MaxWorkersMode,
			run.ElapsedSeconds,
			run.PagesPerMinute,
			run.VisionOCRSeconds,
			run.SearchablePDFSeconds,
			speedup,
			throughput,
		))
	}
	if bestLabel != "" {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("- Fastest run: `%s`\n", bestLabel))
	}
	return b.String()
}

func writeJSON(path string, payload any) error {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o644)
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
