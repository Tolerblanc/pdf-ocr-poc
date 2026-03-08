package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/batch"
	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/eval"
	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
	runpkg "github.com/Tolerblanc/pdf-ocr-poc/v2/internal/run"
)

var platformSupportedFn = isSupportedPlatform
var providerLocalOnlySelfcheckFn = provider.LocalOnlySelfcheck

func Execute(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printRootHelp(stderr)
		return 2
	}

	if !platformSupportedFn() {
		fmt.Fprintln(stderr, "v2 supports macOS arm64 only")
		return 1
	}

	switch args[0] {
	case "run":
		return runCommand(args[1:], stdout, stderr)
	case "batch":
		return batchCommand(args[1:], stdout, stderr)
	case "eval":
		return evalCommand(args[1:], stdout, stderr)
	case "selfcheck-local-only":
		return selfcheckLocalOnlyCommand(stdout, stderr)
	case "help", "-h", "--help":
		printRootHelp(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n\n", args[0])
		printRootHelp(stderr)
		return 2
	}
}

func runCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)

	input := fs.String("input", "", "input PDF path")
	out := fs.String("out", "", "output directory")
	profile := fs.String("profile", "fast", "profile name")
	providerName := fs.String("provider", "mock", "provider name (mock|exec|custom)")
	providerBin := fs.String("provider-bin", "", "provider executable path")
	postprocessProvider := fs.String(
		"postprocess-provider",
		"",
		"postprocess provider/profile override (none|local-lm|cloud-llm|foundation-models|codex-headless-oauth)",
	)
	postprocessConfig := fs.String(
		"postprocess-config",
		"",
		"postprocess config file path (defaults to OCRPOC_POSTPROCESS_CONFIG)",
	)
	postprocessAllowRemote := fs.Bool(
		"postprocess-allow-remote",
		false,
		"allow postprocess providers that require remote access",
	)
	maxWorkers := fs.Int("max-workers", 0, "optional manual worker override")
	ocrLocalOnly := fs.Bool("ocr-local-only", true, "enforce local-only mode for the OCR provider")
	fs.BoolVar(ocrLocalOnly, "local-only", true, "deprecated alias for --ocr-local-only")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *input == "" || *out == "" {
		fs.Usage()
		fmt.Fprintln(stderr, "run requires --input and --out")
		return 2
	}
	if *maxWorkers < 0 {
		fmt.Fprintln(stderr, "run requires --max-workers >= 0")
		return 2
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintf(stderr, "failed to prepare output parent directory: %v\n", err)
		return 1
	}

	workers, mode := resolveWorkers(*maxWorkers, *providerName, *providerBin)
	p, err := provider.New(*providerName, *providerBin)
	if err != nil {
		fmt.Fprintf(stderr, "provider error: %v\n", err)
		return 2
	}
	fmt.Fprintf(
		stderr,
		"run config: provider=%s postprocess=%s max-workers=%d mode=%s ocr-local-only=%t postprocess-allow-remote=%t\n",
		p.Name(),
		displayPostprocessProvider(*postprocessProvider),
		workers,
		mode,
		*ocrLocalOnly,
		*postprocessAllowRemote,
	)
	renderer := newRunProgressRenderer(stderr, *input)

	output, err := runpkg.Execute(context.Background(), p, runpkg.Options{
		InputPDF:               *input,
		OutputDir:              *out,
		Profile:                *profile,
		LocalOnly:              *ocrLocalOnly,
		PostprocessAllowRemote: *postprocessAllowRemote,
		MaxWorkers:             workers,
		MaxWorkersMode:         mode,
		PostprocessProvider:    *postprocessProvider,
		PostprocessConfigPath:  *postprocessConfig,
		OnProgress:             renderer.Render,
	})
	renderer.Finish()
	if err != nil {
		fmt.Fprintf(stderr, "run failed: %v\n", err)
		return 1
	}
	if hasString(output.Result.Warnings, "max_workers_not_applied_yet_in_swift_provider") {
		fmt.Fprintln(
			stderr,
			"warning: provider reported max-workers was not applied; page OCR is running serially",
		)
	}

	fmt.Fprintf(stdout, "run_report=%s\n", output.RunReportPath)
	fmt.Fprintf(stdout, "local_only_report=%s\n", output.LocalOnlyReportPath)
	fmt.Fprintf(stdout, "searchable_pdf=%s\n", output.Result.SearchablePDF)
	if output.Postprocess.CorrectedPagesJSON != "" {
		fmt.Fprintf(stdout, "corrected_pages=%s\n", output.Postprocess.CorrectedPagesJSON)
	}
	return 0
}

func batchCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("batch", flag.ContinueOnError)
	fs.SetOutput(stderr)

	input := fs.String("input", "", "input PDF file or folder")
	out := fs.String("out", "", "output root")
	profile := fs.String("profile", "fast", "profile name")
	providerName := fs.String("provider", "mock", "provider name (mock|exec|custom)")
	providerBin := fs.String("provider-bin", "", "provider executable path")
	postprocessProvider := fs.String(
		"postprocess-provider",
		"",
		"postprocess provider/profile override (none|local-lm|cloud-llm|foundation-models|codex-headless-oauth)",
	)
	postprocessConfig := fs.String(
		"postprocess-config",
		"",
		"postprocess config file path (defaults to OCRPOC_POSTPROCESS_CONFIG)",
	)
	postprocessAllowRemote := fs.Bool(
		"postprocess-allow-remote",
		false,
		"allow postprocess providers that require remote access",
	)
	workers := fs.Int("workers", 1, "number of concurrent PDF jobs")
	maxWorkers := fs.Int("max-workers", 0, "optional manual worker override")
	resume := fs.Bool("resume", true, "resume from batch_state.json when present")
	recursive := fs.Bool("recursive", false, "scan input directory recursively")
	retryFailed := fs.Int("retry-failed", 1, "retry failed jobs at end")
	ocrLocalOnly := fs.Bool("ocr-local-only", true, "enforce local-only mode for the OCR provider")
	fs.BoolVar(ocrLocalOnly, "local-only", true, "deprecated alias for --ocr-local-only")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *input == "" || *out == "" {
		fmt.Fprintln(stderr, "batch requires --input and --out")
		return 2
	}
	if *workers < 1 {
		fmt.Fprintln(stderr, "batch requires --workers >= 1")
		return 2
	}
	if *maxWorkers < 0 {
		fmt.Fprintln(stderr, "batch requires --max-workers >= 0")
		return 2
	}
	if *retryFailed < 0 {
		fmt.Fprintln(stderr, "batch requires --retry-failed >= 0")
		return 2
	}

	resolvedMaxWorkers, mode := resolveWorkers(*maxWorkers, *providerName, *providerBin)
	p, err := provider.New(*providerName, *providerBin)
	if err != nil {
		fmt.Fprintf(stderr, "provider error: %v\n", err)
		return 2
	}
	fmt.Fprintf(
		stderr,
		"batch config: provider=%s postprocess=%s workers=%d max-workers=%d mode=%s ocr-local-only=%t postprocess-allow-remote=%t\n",
		p.Name(),
		displayPostprocessProvider(*postprocessProvider),
		*workers,
		resolvedMaxWorkers,
		mode,
		*ocrLocalOnly,
		*postprocessAllowRemote,
	)
	renderer := newBatchProgressRenderer(stderr)
	defer renderer.Finish()

	report, err := batch.Run(context.Background(), p, batch.Options{
		InputPath:              *input,
		OutputRoot:             *out,
		Profile:                *profile,
		LocalOnly:              *ocrLocalOnly,
		PostprocessAllowRemote: *postprocessAllowRemote,
		MaxWorkers:             resolvedMaxWorkers,
		MaxWorkersMode:         mode,
		PostprocessProvider:    *postprocessProvider,
		PostprocessConfigPath:  *postprocessConfig,
		Workers:                *workers,
		Recursive:              *recursive,
		Resume:                 *resume,
		RetryFailed:            *retryFailed,
		OnProgress:             renderer.Render,
	})
	if err != nil {
		fmt.Fprintf(stderr, "batch failed: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "batch_report=%s\n", report.ReportPath)
	fmt.Fprintf(stdout, "batch_state=%s\n", report.StatePath)
	fmt.Fprintf(stdout, "total=%d succeeded=%d failed=%d skipped=%d\n", report.Total, report.Succeeded, report.Failed, report.Skipped)
	if report.Failed > 0 {
		return 1
	}
	return 0
}

func displayPostprocessProvider(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "auto"
	}
	return value
}

func evalCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(stderr)

	gold := fs.String("gold", "", "gold pages json path")
	pred := fs.String("pred", "", "prediction pages json path")
	out := fs.String("out", "", "evaluation output json path")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *gold == "" || *pred == "" || *out == "" {
		fmt.Fprintln(stderr, "eval requires --gold, --pred, and --out")
		return 2
	}

	result, err := eval.Evaluate(*gold, *pred)
	if err != nil {
		fmt.Fprintf(stderr, "eval failed: %v\n", err)
		return 1
	}
	if err := eval.Save(*out, result); err != nil {
		fmt.Fprintf(stderr, "eval save failed: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "eval_report=%s\n", *out)
	return 0
}

func selfcheckLocalOnlyCommand(stdout, stderr io.Writer) int {
	ok, message := providerLocalOnlySelfcheckFn()
	if ok {
		fmt.Fprintln(stdout, message)
		return 0
	}
	fmt.Fprintln(stderr, message)
	return 1
}

func resolveWorkers(maxWorkers int, providerName, providerBin string) (int, string) {
	if maxWorkers > 0 {
		return maxWorkers, "manual"
	}
	return provider.ResolveAutoMaxWorkers(providerName, providerBin), "auto"
}

func isSupportedPlatform() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}

func printRootHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "ocrpoc-go <command> [flags]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(w, "  run    run OCR provider once")
	_, _ = fmt.Fprintln(w, "  batch  run OCR provider in batch mode with resume/retry")
	_, _ = fmt.Fprintln(w, "  eval   evaluate predicted pages against gold pages")
	_, _ = fmt.Fprintln(w, "  selfcheck-local-only  verify local-only monitor prerequisites")
}

func hasString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func Main() int {
	return Execute(os.Args[1:], os.Stdout, os.Stderr)
}
