package run

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/postprocess"
	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
)

type runStubProvider struct {
	result   provider.Result
	progress []provider.ProgressEvent
	calls    int
}

func (p *runStubProvider) Name() string {
	return "stub"
}

func (p *runStubProvider) Run(_ context.Context, req provider.Request) (provider.Result, error) {
	p.calls++
	for _, event := range p.progress {
		if req.OnProgress != nil {
			req.OnProgress(event)
		}
	}
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

func TestExecuteForwardsProviderProgress(t *testing.T) {
	temp := t.TempDir()
	inputPDF := filepath.Join(temp, "in.pdf")
	if err := os.WriteFile(inputPDF, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	outDir := filepath.Join(temp, "out")
	stub := &runStubProvider{progress: []provider.ProgressEvent{{
		Phase:          "page_done",
		Stage:          "vision_ocr",
		CurrentPage:    1,
		CompletedPages: 1,
		TotalPages:     3,
	}}}

	events := []provider.ProgressEvent{}
	_, err := Execute(context.Background(), stub, Options{
		InputPDF:       inputPDF,
		OutputDir:      outDir,
		Profile:        "fast",
		LocalOnly:      false,
		MaxWorkers:     2,
		MaxWorkersMode: "manual",
		OnProgress: func(event provider.ProgressEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	found := false
	for _, event := range events {
		if event.Stage == "vision_ocr" && event.CompletedPages == 1 && event.TotalPages == 3 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected forwarded provider progress event, got %+v", events)
	}
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

func TestExecuteWritesCorrectedPagesReportFields(t *testing.T) {
	temp := t.TempDir()
	inputPDF := filepath.Join(temp, "in.pdf")
	if err := os.WriteFile(inputPDF, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	outDir := filepath.Join(temp, "out")
	output, err := Execute(context.Background(), &runStubProvider{}, Options{
		InputPDF:            inputPDF,
		OutputDir:           outDir,
		Profile:             "fast",
		LocalOnly:           false,
		MaxWorkers:          2,
		MaxWorkersMode:      "manual",
		PostprocessProvider: "none",
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if output.Postprocess.CorrectedPagesJSON == "" {
		t.Fatalf("expected corrected_pages.json path")
	}

	body, err := os.ReadFile(filepath.Join(outDir, "run_report.json"))
	if err != nil {
		t.Fatalf("read run report failed: %v", err)
	}
	report := map[string]any{}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("parse run report failed: %v", err)
	}
	if report["postprocess_provider"] != "none" {
		t.Fatalf("unexpected postprocess_provider: %v", report["postprocess_provider"])
	}
	if report["corrected_pages_json"] == "" {
		t.Fatalf("expected corrected_pages_json in run report: %+v", report)
	}
}

func TestExecuteResolvesPostprocessConfigAndPrimaryArtifacts(t *testing.T) {
	temp := t.TempDir()
	inputPDF := filepath.Join(temp, "in.pdf")
	if err := os.WriteFile(inputPDF, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write input failed: %v", err)
	}
	configPath := filepath.Join(temp, "postprocess.json")
	configBody := `{
	  "version": "v1alpha1",
	  "providers": {
	    "default": {
	      "provider": "none",
	      "output_mode": "primary_artifacts"
	    }
	  },
	  "runtime": {
	    "profile": "default"
	  }
	}`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	outDir := filepath.Join(temp, "out")
	output, err := Execute(context.Background(), &provider.MockProvider{}, Options{
		InputPDF:              inputPDF,
		OutputDir:             outDir,
		Profile:               "fast",
		LocalOnly:             false,
		MaxWorkers:            2,
		MaxWorkersMode:        "manual",
		PostprocessConfigPath: configPath,
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if output.Postprocess.OutputMode != postprocess.OutputModePrimaryArtifacts {
		t.Fatalf("expected primary artifact mode, got %s", output.Postprocess.OutputMode)
	}
	if _, err := os.Stat(filepath.Join(outDir, "pages.json")); err != nil {
		t.Fatalf("expected regenerated pages.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "searchable.pdf")); err != nil {
		t.Fatalf("expected regenerated searchable.pdf: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(outDir, "run_report.json"))
	if err != nil {
		t.Fatalf("read run report failed: %v", err)
	}
	report := map[string]any{}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("parse run report failed: %v", err)
	}
	if report["postprocess_config_path"] != configPath {
		t.Fatalf("unexpected postprocess_config_path: %v", report["postprocess_config_path"])
	}
	if report["postprocess_output_mode"] != postprocess.OutputModePrimaryArtifacts {
		t.Fatalf("unexpected postprocess_output_mode: %v", report["postprocess_output_mode"])
	}
}

func TestRebuildPrimaryArtifactsUsesCorrectedPages(t *testing.T) {
	temp := t.TempDir()
	inputPDF := filepath.Join(temp, "in.pdf")
	if err := os.WriteFile(inputPDF, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write input failed: %v", err)
	}
	correctedPages := filepath.Join(temp, "corrected_pages.json")
	correctedBody := `{
	  "version": "v1alpha1",
	  "kind": "corrected_pages",
	  "engine": "mock",
	  "source_pdf": "` + inputPDF + `",
	  "postprocess": {
	    "provider": "none",
	    "applied": false,
	    "output_mode": "primary_artifacts"
	  },
	  "pages": [
	    {
	      "page": 1,
	      "source_text": "raw",
	      "text": "Corrected text from postprocess",
	      "blocks": [
	        {
	          "block_id": "p1-b1",
	          "text": "Corrected text from postprocess",
	          "source_text": "raw",
	          "bbox": {"x0": 1, "y0": 2, "x1": 3, "y1": 4},
	          "block_type": "paragraph",
	          "confidence": 0.8,
	          "reading_order": 1,
	          "correction": {
	            "status": "corrected",
	            "edited": true,
	            "reasons": ["ocr_fix"]
	          }
	        }
	      ],
	      "correction": {
	        "status": "corrected",
	        "changed_blocks": 1,
	        "total_blocks": 1
	      }
	    }
	  ]
	}`
	if err := os.WriteFile(correctedPages, []byte(correctedBody), 0o644); err != nil {
		t.Fatalf("write corrected pages failed: %v", err)
	}

	outDir := filepath.Join(temp, "out")
	current := provider.Result{
		PagesJSON:     filepath.Join(outDir, "pages.json"),
		TextPath:      filepath.Join(outDir, "document.txt"),
		MarkdownPath:  filepath.Join(outDir, "document.md"),
		SearchablePDF: filepath.Join(outDir, "searchable.pdf"),
	}
	result, err := rebuildPrimaryArtifacts(context.Background(), &provider.MockProvider{}, current, Options{
		InputPDF:       inputPDF,
		OutputDir:      outDir,
		Profile:        "fast",
		LocalOnly:      false,
		MaxWorkers:     1,
		MaxWorkersMode: "manual",
	}, correctedPages)
	if err != nil {
		t.Fatalf("rebuild primary artifacts failed: %v", err)
	}

	textBody, err := os.ReadFile(result.TextPath)
	if err != nil {
		t.Fatalf("read text failed: %v", err)
	}
	if strings.TrimSpace(string(textBody)) != "Corrected text from postprocess" {
		t.Fatalf("unexpected regenerated text: %q", string(textBody))
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

func TestExecuteFailsFastForRemotePostprocessInLocalOnlyMode(t *testing.T) {
	temp := t.TempDir()
	inputPDF := filepath.Join(temp, "in.pdf")
	if err := os.WriteFile(inputPDF, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	stub := &runStubProvider{}
	_, err := Execute(context.Background(), stub, Options{
		InputPDF:            inputPDF,
		OutputDir:           filepath.Join(temp, "out"),
		Profile:             "fast",
		LocalOnly:           true,
		MaxWorkers:          1,
		MaxWorkersMode:      "manual",
		PostprocessProvider: postprocess.ProviderCodexHeadlessOAuth,
	})
	if err == nil {
		t.Fatalf("expected local-only postprocess error")
	}
	if stub.calls != 0 {
		t.Fatalf("expected OCR provider to be skipped, got %d calls", stub.calls)
	}
}
