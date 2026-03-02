package run

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
)

type runStubProvider struct {
	result provider.Result
}

func (p *runStubProvider) Name() string {
	return "stub"
}

func (p *runStubProvider) Run(_ context.Context, req provider.Request) (provider.Result, error) {
	if err := os.MkdirAll(req.OutputDir, 0o755); err != nil {
		return provider.Result{}, err
	}
	pagesPath := filepath.Join(req.OutputDir, "pages.json")
	body := []byte(`{"pages":[{"page":1,"text":"a"}]}`)
	if err := os.WriteFile(pagesPath, body, 0o644); err != nil {
		return provider.Result{}, err
	}
	searchable := filepath.Join(req.OutputDir, "searchable.pdf")
	if err := os.WriteFile(searchable, []byte("%PDF-1.4\n"), 0o644); err != nil {
		return provider.Result{}, err
	}
	textPath := filepath.Join(req.OutputDir, "document.txt")
	if err := os.WriteFile(textPath, []byte("a"), 0o644); err != nil {
		return provider.Result{}, err
	}
	markdownPath := filepath.Join(req.OutputDir, "document.md")
	if err := os.WriteFile(markdownPath, []byte("# a\n"), 0o644); err != nil {
		return provider.Result{}, err
	}

	result := p.result
	result.PagesJSON = pagesPath
	result.SearchablePDF = searchable
	result.TextPath = textPath
	result.MarkdownPath = markdownPath
	return result, nil
}

func TestExecuteWritesLocalOnlyReportFromProviderMetadata(t *testing.T) {
	temp := t.TempDir()
	inputPDF := filepath.Join(temp, "in.pdf")
	if err := os.WriteFile(inputPDF, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	outDir := filepath.Join(temp, "out")
	stub := &runStubProvider{result: provider.Result{
		MonitorSamples:             10,
		MonitorDurationSeconds:     2.5,
		RemoteConnectionViolations: []string{},
		LocalOnlySelfcheckSet:      true,
		LocalOnlySelfcheckOK:       true,
		LocalOnlySelfcheckMessage:  "monitor active",
	}}

	_, err := Execute(context.Background(), stub, Options{
		InputPDF:       inputPDF,
		OutputDir:      outDir,
		Profile:        "fast",
		LocalOnly:      true,
		MaxWorkers:     4,
		MaxWorkersMode: "manual",
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(outDir, "local_only_report.json"))
	if err != nil {
		t.Fatalf("read report failed: %v", err)
	}
	report := map[string]any{}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("parse report failed: %v", err)
	}
	if report["monitor_samples"] != float64(10) {
		t.Fatalf("unexpected monitor_samples: %v", report["monitor_samples"])
	}
	if report["monitor_ok"] != true {
		t.Fatalf("unexpected monitor_ok: %v", report["monitor_ok"])
	}
}

func TestExecuteFailsOnLocalOnlyViolation(t *testing.T) {
	temp := t.TempDir()
	inputPDF := filepath.Join(temp, "in.pdf")
	if err := os.WriteFile(inputPDF, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	outDir := filepath.Join(temp, "out")
	stub := &runStubProvider{result: provider.Result{
		MonitorSamples:             5,
		MonitorDurationSeconds:     1.0,
		RemoteConnectionViolations: []string{"pid=1 remote=1.1.1.1:443"},
		LocalOnlySelfcheckSet:      true,
		LocalOnlySelfcheckOK:       true,
		LocalOnlySelfcheckMessage:  "monitor active",
	}}

	_, err := Execute(context.Background(), stub, Options{
		InputPDF:       inputPDF,
		OutputDir:      outDir,
		Profile:        "fast",
		LocalOnly:      true,
		MaxWorkers:     4,
		MaxWorkersMode: "manual",
	})
	if err == nil {
		t.Fatalf("expected local-only violation error")
	}

	body, readErr := os.ReadFile(filepath.Join(outDir, "local_only_report.json"))
	if readErr != nil {
		t.Fatalf("expected local_only_report to be written, got err=%v", readErr)
	}
	report := map[string]any{}
	if parseErr := json.Unmarshal(body, &report); parseErr != nil {
		t.Fatalf("parse report failed: %v", parseErr)
	}
	if report["monitor_ok"] != false {
		t.Fatalf("expected monitor_ok=false, got %v", report["monitor_ok"])
	}
}
