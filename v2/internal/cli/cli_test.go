package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExecScript(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script failed: %v", err)
	}
}

func writeTestPDF(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func TestRunCommandWithMockProvider(t *testing.T) {
	temp := t.TempDir()
	inputPDF := filepath.Join(temp, "in.pdf")
	writeTestPDF(t, inputPDF)
	outDir := filepath.Join(temp, "out")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute([]string{
		"run",
		"--input", inputPDF,
		"--out", outDir,
		"--provider", "mock",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "run_report=") {
		t.Fatalf("expected run_report output, got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "corrected_pages=") {
		t.Fatalf("expected corrected_pages output, got: %s", stdout.String())
	}

	body, err := os.ReadFile(filepath.Join(outDir, "run_report.json"))
	if err != nil {
		t.Fatalf("read run report failed: %v", err)
	}
	report := map[string]any{}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("parse run report failed: %v", err)
	}
	if report["max_workers_mode"] != "auto" {
		t.Fatalf("expected auto max_workers_mode, got %v", report["max_workers_mode"])
	}
	if report["postprocess_provider"] != "none" {
		t.Fatalf("expected postprocess_provider=none, got %v", report["postprocess_provider"])
	}
}

func TestRunCommandManualWorkers(t *testing.T) {
	temp := t.TempDir()
	inputPDF := filepath.Join(temp, "in.pdf")
	writeTestPDF(t, inputPDF)
	outDir := filepath.Join(temp, "out")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute([]string{
		"run",
		"--input", inputPDF,
		"--out", outDir,
		"--provider", "mock",
		"--max-workers", "7",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%s", code, stderr.String())
	}
	body, err := os.ReadFile(filepath.Join(outDir, "run_report.json"))
	if err != nil {
		t.Fatalf("read run report failed: %v", err)
	}
	report := map[string]any{}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("parse run report failed: %v", err)
	}
	if report["effective_max_workers"] != float64(7) {
		t.Fatalf("expected effective_max_workers=7, got %v", report["effective_max_workers"])
	}
	if report["max_workers_mode"] != "manual" {
		t.Fatalf("expected manual max_workers_mode, got %v", report["max_workers_mode"])
	}
}

func TestRunCommandShowsProviderProgress(t *testing.T) {
	temp := t.TempDir()
	inputPDF := filepath.Join(temp, "in.pdf")
	writeTestPDF(t, inputPDF)
	outDir := filepath.Join(temp, "out")
	script := filepath.Join(temp, "provider.sh")
	writeExecScript(t, script, "#!/usr/bin/env bash\nset -euo pipefail\ncat >/dev/null\necho 'OCRPOC_PROGRESS {\"phase\":\"page_done\",\"stage\":\"vision_ocr\",\"current_page\":2,\"completed_pages\":1,\"total_pages\":3}' >&2\ncat <<'JSON'\n{\"searchable_pdf\":\"/tmp/a.pdf\",\"pages_json\":\"/tmp/pages.json\",\"text_path\":\"/tmp/document.txt\",\"markdown_path\":\"/tmp/document.md\"}\nJSON\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute([]string{
		"run",
		"--input", inputPDF,
		"--out", outDir,
		"--provider", "exec",
		"--provider-bin", script,
		"--ocr-local-only=false",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "1/3 pg") {
		t.Fatalf("expected page progress in stderr, got: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "| ocr p2") {
		t.Fatalf("expected OCR progress label in stderr, got: %s", stderr.String())
	}
}

func TestBatchCommandWithMockProvider(t *testing.T) {
	temp := t.TempDir()
	inputDir := filepath.Join(temp, "in")
	writeTestPDF(t, filepath.Join(inputDir, "a.pdf"))
	writeTestPDF(t, filepath.Join(inputDir, "b.pdf"))
	outDir := filepath.Join(temp, "out")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute([]string{
		"batch",
		"--input", inputDir,
		"--out", outDir,
		"--provider", "mock",
		"--workers", "2",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "batch_report=") {
		t.Fatalf("expected batch_report output, got: %s", stdout.String())
	}
}

func TestRunCommandRejectsNegativeMaxWorkers(t *testing.T) {
	temp := t.TempDir()
	inputPDF := filepath.Join(temp, "in.pdf")
	writeTestPDF(t, inputPDF)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute([]string{
		"run",
		"--input", inputPDF,
		"--out", filepath.Join(temp, "out"),
		"--provider", "mock",
		"--max-workers", "-1",
	}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "--max-workers >= 0") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestExecutePlatformGuard(t *testing.T) {
	original := platformSupportedFn
	platformSupportedFn = func() bool { return false }
	t.Cleanup(func() {
		platformSupportedFn = original
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute([]string{"help"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "macOS arm64") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestSelfcheckLocalOnlyCommandPass(t *testing.T) {
	original := providerLocalOnlySelfcheckFn
	providerLocalOnlySelfcheckFn = func() (bool, string) {
		return true, "monitor ready"
	}
	t.Cleanup(func() {
		providerLocalOnlySelfcheckFn = original
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute([]string{"selfcheck-local-only"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "monitor ready") {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

func TestSelfcheckLocalOnlyCommandFail(t *testing.T) {
	original := providerLocalOnlySelfcheckFn
	providerLocalOnlySelfcheckFn = func() (bool, string) {
		return false, "monitor missing"
	}
	t.Cleanup(func() {
		providerLocalOnlySelfcheckFn = original
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute([]string{"selfcheck-local-only"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "monitor missing") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestEvalCommand(t *testing.T) {
	temp := t.TempDir()
	goldPath := filepath.Join(temp, "gold.json")
	predPath := filepath.Join(temp, "pred.json")
	outPath := filepath.Join(temp, "eval.json")

	gold := `{"pages":[{"page":1,"text":"hello","is_blank":false}]}`
	pred := `{"pages":[{"page":1,"text":"hello"}]}`
	if err := os.WriteFile(goldPath, []byte(gold), 0o644); err != nil {
		t.Fatalf("write gold failed: %v", err)
	}
	if err := os.WriteFile(predPath, []byte(pred), 0o644); err != nil {
		t.Fatalf("write pred failed: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute([]string{
		"eval",
		"--gold", goldPath,
		"--pred", predPath,
		"--out", outPath,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "eval_report=") {
		t.Fatalf("expected eval_report output, got: %s", stdout.String())
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output file, got err=%v", err)
	}
}
