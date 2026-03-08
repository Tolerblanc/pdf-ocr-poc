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
	"sync"
	"time"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
)

type shardSpec struct {
	Label      string `json:"label"`
	Shards     int    `json:"shards"`
	MaxWorkers int    `json:"max_workers"`
	Mode       string `json:"mode"`
}

type shardDetail struct {
	ShardIndex     int                `json:"shard_index"`
	ShardTotal     int                `json:"shard_total"`
	OutputDir      string             `json:"output_dir"`
	ElapsedSeconds float64            `json:"elapsed_seconds"`
	Pages          int                `json:"pages"`
	PagesPerMinute float64            `json:"pages_per_minute"`
	SearchablePDF  string             `json:"searchable_pdf"`
	PagesJSON      string             `json:"pages_json"`
	TextPath       string             `json:"text_path"`
	MarkdownPath   string             `json:"markdown_path"`
	StageTimings   map[string]float64 `json:"stage_timings"`
	Warnings       []string           `json:"warnings,omitempty"`
}

type aggregateReport struct {
	Engine                    string        `json:"engine"`
	InputPDF                  string        `json:"input_pdf"`
	Profile                   string        `json:"profile"`
	Shards                    int           `json:"shards"`
	MaxWorkersPerShard        int           `json:"max_workers_per_shard"`
	MaxWorkersMode            string        `json:"max_workers_mode"`
	LocalOnly                 bool          `json:"local_only"`
	ElapsedSeconds            float64       `json:"elapsed_seconds"`
	Pages                     int           `json:"pages"`
	PagesPerMinute            float64       `json:"pages_per_minute"`
	VisionOCRSecondsTotal     float64       `json:"vision_ocr_seconds_total"`
	SearchablePDFSecondsTotal float64       `json:"searchable_pdf_seconds_total"`
	ProviderTotalSecondsTotal float64       `json:"provider_total_seconds_total"`
	SlowestShardSeconds       float64       `json:"slowest_shard_seconds"`
	CombinedPagesJSON         string        `json:"combined_pages_json"`
	CombinedTextPath          string        `json:"combined_text_path"`
	CombinedMarkdownPath      string        `json:"combined_markdown_path"`
	Warnings                  []string      `json:"warnings,omitempty"`
	ShardReports              []shardDetail `json:"shard_reports"`
	Spec                      shardSpec     `json:"spec"`
}

type runSummary struct {
	Label                     string    `json:"label"`
	OutputDir                 string    `json:"output_dir"`
	AggregateReportPath       string    `json:"aggregate_report_path"`
	CompletedAt               string    `json:"completed_at"`
	Shards                    int       `json:"shards"`
	MaxWorkersPerShard        int       `json:"max_workers_per_shard"`
	MaxWorkersMode            string    `json:"max_workers_mode"`
	ElapsedSeconds            float64   `json:"elapsed_seconds"`
	Pages                     int       `json:"pages"`
	PagesPerMinute            float64   `json:"pages_per_minute"`
	VisionOCRSecondsTotal     float64   `json:"vision_ocr_seconds_total"`
	SearchablePDFSecondsTotal float64   `json:"searchable_pdf_seconds_total"`
	ProviderTotalSecondsTotal float64   `json:"provider_total_seconds_total"`
	SlowestShardSeconds       float64   `json:"slowest_shard_seconds"`
	Warnings                  []string  `json:"warnings,omitempty"`
	SpeedupVsBaseline         float64   `json:"speedup_vs_baseline,omitempty"`
	ThroughputVsBaseline      float64   `json:"throughput_vs_baseline,omitempty"`
	Spec                      shardSpec `json:"spec"`
}

type benchmarkSummary struct {
	GeneratedAt        string       `json:"generated_at"`
	InputPDF           string       `json:"input_pdf"`
	OutputRoot         string       `json:"output_root"`
	Profile            string       `json:"profile"`
	LocalOnly          bool         `json:"local_only"`
	MaxWorkersPerShard int          `json:"max_workers_per_shard"`
	MaxWorkersMode     string       `json:"max_workers_mode"`
	Runs               []runSummary `json:"runs"`
}

type ocrDocument struct {
	Engine    string            `json:"engine"`
	SourcePDF string            `json:"source_pdf"`
	Pages     []json.RawMessage `json:"pages"`
}

type numberedPage struct {
	Page int `json:"page"`
}

func main() {
	if err := runMain(); err != nil {
		fmt.Fprintf(os.Stderr, "benchshard error: %v\n", err)
		os.Exit(1)
	}
}

func runMain() error {
	fs := flag.NewFlagSet("ocrpoc-benchshard", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	input := fs.String("input", "", "input PDF path")
	outRoot := fs.String("out-root", "", "output root directory")
	providerName := fs.String("provider", "vision-swift", "provider name")
	providerBin := fs.String("provider-bin", "", "provider executable path")
	profile := fs.String("profile", "fast", "profile name")
	shards := fs.String("shards", "1,2,4,8", "comma-separated shard counts")
	maxWorkersPerShard := fs.Int("max-workers-per-shard", 1, "worker count inside each shard process; 0 uses provider auto")
	localOnly := fs.Bool("local-only", true, "enable local-only mode")
	reuseExisting := fs.Bool("reuse-existing", false, "reuse existing aggregate_report.json under each output directory")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(*input) == "" {
		return errors.New("--input is required")
	}
	if strings.TrimSpace(*outRoot) == "" {
		return errors.New("--out-root is required")
	}
	if *maxWorkersPerShard < 0 {
		return errors.New("--max-workers-per-shard must be >= 0")
	}

	shardWorkers, mode := resolveShardWorkers(*maxWorkersPerShard, *providerName, *providerBin)
	specs, err := parseShardSpecs(*shards, shardWorkers, mode)
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
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
		InputPDF:           *input,
		OutputRoot:         *outRoot,
		Profile:            *profile,
		LocalOnly:          *localOnly,
		MaxWorkersPerShard: shardWorkers,
		MaxWorkersMode:     mode,
		Runs:               make([]runSummary, 0, len(specs)),
	}

	for idx, spec := range specs {
		fmt.Fprintf(
			os.Stderr,
			"[%d/%d] label=%s shards=%d max-workers-per-shard=%d mode=%s\n",
			idx+1,
			len(specs),
			spec.Label,
			spec.Shards,
			spec.MaxWorkers,
			spec.Mode,
		)
		run, err := executeShardBenchmarkRun(context.Background(), p, *input, *outRoot, *profile, *localOnly, spec, *reuseExisting)
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

func resolveShardWorkers(maxWorkersPerShard int, providerName, providerBin string) (int, string) {
	if maxWorkersPerShard > 0 {
		return maxWorkersPerShard, "manual"
	}
	return provider.ResolveAutoMaxWorkers(providerName, providerBin), "auto"
}

func parseShardSpecs(values string, maxWorkers int, mode string) ([]shardSpec, error) {
	tokens := strings.Split(values, ",")
	specs := make([]shardSpec, 0, len(tokens))
	seen := map[string]struct{}{}
	for _, token := range tokens {
		value := strings.TrimSpace(token)
		if value == "" {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("invalid shard value %q", value)
		}
		if parsed < 1 {
			return nil, fmt.Errorf("shard value must be >= 1: %d", parsed)
		}
		label := fmt.Sprintf("s%d", parsed)
		if mode == "auto" {
			label += "-auto"
		} else {
			label += fmt.Sprintf("-w%d", maxWorkers)
		}
		if _, exists := seen[label]; exists {
			return nil, fmt.Errorf("duplicate shard spec: %s", value)
		}
		seen[label] = struct{}{}
		specs = append(specs, shardSpec{Label: label, Shards: parsed, MaxWorkers: maxWorkers, Mode: mode})
	}
	if len(specs) == 0 {
		return nil, errors.New("at least one shard value is required")
	}
	return specs, nil
}

func executeShardBenchmarkRun(
	ctx context.Context,
	p provider.Provider,
	inputPDF string,
	outRoot string,
	profile string,
	localOnly bool,
	spec shardSpec,
	reuseExisting bool,
) (runSummary, error) {
	outputDir := filepath.Join(outRoot, spec.Label)
	aggregateReportPath := filepath.Join(outputDir, "aggregate_report.json")
	if reuseExisting {
		if report, err := loadAggregateReport(aggregateReportPath); err == nil {
			return buildRunSummary(spec, outputDir, aggregateReportPath, report), nil
		}
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return runSummary{}, err
	}

	start := time.Now()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	shards := make([]shardDetail, spec.Shards)
	var once sync.Once
	var firstErr error
	var wg sync.WaitGroup

	for shardIndex := 1; shardIndex <= spec.Shards; shardIndex++ {
		wg.Add(1)
		go func(shardIndex int) {
			defer wg.Done()

			shardOutputDir := filepath.Join(outputDir, fmt.Sprintf("shard-%02d-of-%02d", shardIndex, spec.Shards))
			if err := os.MkdirAll(shardOutputDir, 0o755); err != nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}

			shardStart := time.Now()
			result, err := p.Run(runCtx, provider.Request{
				InputPDF:      inputPDF,
				OutputDir:     shardOutputDir,
				Profile:       profile,
				LocalOnly:     localOnly,
				MaxWorkers:    spec.MaxWorkers,
				WorkersMode:   spec.Mode,
				ShardIndex:    shardIndex,
				ShardTotal:    spec.Shards,
				RequestSource: "ocrpoc-benchshard",
			})
			if err != nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}

			elapsedSeconds := time.Since(shardStart).Seconds()
			pages := countPages(result.PagesJSON)
			pagesPerMinute := 0.0
			if pages > 0 && elapsedSeconds > 0 {
				pagesPerMinute = (float64(pages) / elapsedSeconds) * 60
			}
			detail := shardDetail{
				ShardIndex:     shardIndex,
				ShardTotal:     spec.Shards,
				OutputDir:      shardOutputDir,
				ElapsedSeconds: elapsedSeconds,
				Pages:          pages,
				PagesPerMinute: pagesPerMinute,
				SearchablePDF:  result.SearchablePDF,
				PagesJSON:      result.PagesJSON,
				TextPath:       result.TextPath,
				MarkdownPath:   result.MarkdownPath,
				StageTimings:   cloneTimingMap(result.StageTimings),
				Warnings:       append([]string(nil), result.Warnings...),
			}
			if err := writeJSON(filepath.Join(shardOutputDir, "run_report.json"), detail); err != nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			shards[shardIndex-1] = detail
		}(shardIndex)
	}

	wg.Wait()
	if firstErr != nil {
		return runSummary{}, firstErr
	}

	sort.SliceStable(shards, func(i, j int) bool {
		return shards[i].ShardIndex < shards[j].ShardIndex
	})
	combinedPagesJSON, combinedTextPath, combinedMarkdownPath, mergeWarnings, err := mergeShardArtifacts(outputDir, inputPDF, shards)
	if err != nil {
		return runSummary{}, err
	}
	elapsedSeconds := time.Since(start).Seconds()
	aggregate := aggregateReport{
		Engine:               p.Name(),
		InputPDF:             inputPDF,
		Profile:              profile,
		Shards:               spec.Shards,
		MaxWorkersPerShard:   spec.MaxWorkers,
		MaxWorkersMode:       spec.Mode,
		LocalOnly:            localOnly,
		ElapsedSeconds:       elapsedSeconds,
		CombinedPagesJSON:    combinedPagesJSON,
		CombinedTextPath:     combinedTextPath,
		CombinedMarkdownPath: combinedMarkdownPath,
		Warnings:             mergeWarnings,
		ShardReports:         shards,
		Spec:                 spec,
	}
	for _, shard := range shards {
		aggregate.Pages += shard.Pages
		aggregate.VisionOCRSecondsTotal += shard.StageTimings["vision_ocr_seconds"]
		aggregate.SearchablePDFSecondsTotal += shard.StageTimings["searchable_pdf_seconds"]
		aggregate.ProviderTotalSecondsTotal += shard.StageTimings["provider_total_seconds"]
		aggregate.SlowestShardSeconds = maxFloat(aggregate.SlowestShardSeconds, shard.ElapsedSeconds)
		aggregate.Warnings = mergeStringSlices(aggregate.Warnings, shard.Warnings)
	}
	if aggregate.Pages > 0 && aggregate.ElapsedSeconds > 0 {
		aggregate.PagesPerMinute = (float64(aggregate.Pages) / aggregate.ElapsedSeconds) * 60
	}
	if err := writeJSON(aggregateReportPath, aggregate); err != nil {
		return runSummary{}, err
	}
	return buildRunSummary(spec, outputDir, aggregateReportPath, aggregate), nil
}

func mergeShardArtifacts(outputDir, inputPDF string, shards []shardDetail) (string, string, string, []string, error) {
	pages := make([]struct {
		Number int
		Raw    json.RawMessage
	}, 0)
	textParts := make([]string, 0, len(shards))
	markdownParts := make([]string, 0, len(shards))
	warnings := []string{"searchable_pdf_not_merged_across_shards"}

	for _, shard := range shards {
		doc, err := loadOCRDocument(shard.PagesJSON)
		if err != nil {
			return "", "", "", nil, err
		}
		for _, rawPage := range doc.Pages {
			var numbered numberedPage
			if err := json.Unmarshal(rawPage, &numbered); err != nil {
				return "", "", "", nil, err
			}
			pages = append(pages, struct {
				Number int
				Raw    json.RawMessage
			}{Number: numbered.Page, Raw: rawPage})
		}
		textPart, err := os.ReadFile(shard.TextPath)
		if err != nil {
			return "", "", "", nil, err
		}
		markdownPart, err := os.ReadFile(shard.MarkdownPath)
		if err != nil {
			return "", "", "", nil, err
		}
		textParts = append(textParts, strings.TrimSpace(string(textPart)))
		markdownParts = append(markdownParts, strings.TrimSpace(string(markdownPart)))
	}

	sort.SliceStable(pages, func(i, j int) bool {
		return pages[i].Number < pages[j].Number
	})
	merged := ocrDocument{
		Engine:    "vision-swift",
		SourcePDF: inputPDF,
		Pages:     make([]json.RawMessage, 0, len(pages)),
	}
	for _, page := range pages {
		merged.Pages = append(merged.Pages, page.Raw)
	}

	combinedPagesJSON := filepath.Join(outputDir, "combined_pages.json")
	if err := writeJSON(combinedPagesJSON, merged); err != nil {
		return "", "", "", nil, err
	}
	combinedTextPath := filepath.Join(outputDir, "combined_document.txt")
	if err := os.WriteFile(combinedTextPath, []byte(joinNonEmpty(textParts)+"\n"), 0o644); err != nil {
		return "", "", "", nil, err
	}
	combinedMarkdownPath := filepath.Join(outputDir, "combined_document.md")
	if err := os.WriteFile(combinedMarkdownPath, []byte(joinNonEmpty(markdownParts)+"\n"), 0o644); err != nil {
		return "", "", "", nil, err
	}
	return combinedPagesJSON, combinedTextPath, combinedMarkdownPath, warnings, nil
}

func loadOCRDocument(path string) (ocrDocument, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return ocrDocument{}, err
	}
	var doc ocrDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return ocrDocument{}, err
	}
	return doc, nil
}

func joinNonEmpty(parts []string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		nonEmpty = append(nonEmpty, part)
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	return strings.Join(nonEmpty, "\n\n")
}

func loadAggregateReport(path string) (aggregateReport, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return aggregateReport{}, err
	}
	var report aggregateReport
	if err := json.Unmarshal(body, &report); err != nil {
		return aggregateReport{}, err
	}
	return report, nil
}

func buildRunSummary(spec shardSpec, outputDir, aggregateReportPath string, report aggregateReport) runSummary {
	return runSummary{
		Label:                     spec.Label,
		OutputDir:                 outputDir,
		AggregateReportPath:       aggregateReportPath,
		CompletedAt:               time.Now().UTC().Format(time.RFC3339),
		Shards:                    report.Shards,
		MaxWorkersPerShard:        report.MaxWorkersPerShard,
		MaxWorkersMode:            report.MaxWorkersMode,
		ElapsedSeconds:            report.ElapsedSeconds,
		Pages:                     report.Pages,
		PagesPerMinute:            report.PagesPerMinute,
		VisionOCRSecondsTotal:     report.VisionOCRSecondsTotal,
		SearchablePDFSecondsTotal: report.SearchablePDFSecondsTotal,
		ProviderTotalSecondsTotal: report.ProviderTotalSecondsTotal,
		SlowestShardSeconds:       report.SlowestShardSeconds,
		Warnings:                  append([]string(nil), report.Warnings...),
		Spec:                      spec,
	}
}

func countPages(pagesJSONPath string) int {
	body, err := os.ReadFile(pagesJSONPath)
	if err != nil {
		return 0
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0
	}
	rawPages, ok := payload["pages"].([]any)
	if !ok {
		return 0
	}
	return len(rawPages)
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
	b.WriteString("# Process Shard Benchmark\n\n")
	b.WriteString(fmt.Sprintf("- Generated at: `%s`\n", summary.GeneratedAt))
	b.WriteString(fmt.Sprintf("- Input: `%s`\n", summary.InputPDF))
	b.WriteString(fmt.Sprintf("- Profile: `%s`\n", summary.Profile))
	b.WriteString(fmt.Sprintf("- Local only: `%t`\n", summary.LocalOnly))
	b.WriteString(fmt.Sprintf("- Max workers per shard: `%d` (%s)\n\n", summary.MaxWorkersPerShard, summary.MaxWorkersMode))
	b.WriteString("| Label | Shards | Workers/Shard | Mode | Seconds | Pages/min | OCR Total(s) | Slowest Shard(s) | Speedup | Throughput |\n")
	b.WriteString("|---|---:|---:|---|---:|---:|---:|---:|---:|---:|\n")

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
			"| `%s` | %d | %d | %s | %.3f | %.3f | %.3f | %.3f | %s | %s |\n",
			run.Label,
			run.Shards,
			run.MaxWorkersPerShard,
			run.MaxWorkersMode,
			run.ElapsedSeconds,
			run.PagesPerMinute,
			run.VisionOCRSecondsTotal,
			run.SlowestShardSeconds,
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

func cloneTimingMap(stageTimings map[string]float64) map[string]float64 {
	if len(stageTimings) == 0 {
		return nil
	}
	clone := make(map[string]float64, len(stageTimings))
	for key, value := range stageTimings {
		clone[key] = value
	}
	return clone
}

func mergeStringSlices(groups ...[]string) []string {
	seen := map[string]struct{}{}
	merged := []string{}
	for _, group := range groups {
		for _, value := range group {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			merged = append(merged, value)
		}
	}
	return merged
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
