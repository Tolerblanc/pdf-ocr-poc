package batch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/postprocess"
	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
)

type flakyProvider struct {
	mu    sync.Mutex
	calls map[string]int
}

func (p *flakyProvider) Name() string {
	return "flaky"
}

func (p *flakyProvider) Run(_ context.Context, req provider.Request) (provider.Result, error) {
	p.mu.Lock()
	if p.calls == nil {
		p.calls = map[string]int{}
	}
	p.calls[req.InputPDF]++
	attempt := p.calls[req.InputPDF]
	p.mu.Unlock()

	if strings.HasSuffix(req.InputPDF, "a.pdf") && attempt == 1 {
		return provider.Result{}, errors.New("temporary provider failure")
	}

	return writeProviderArtifacts(req.OutputDir)
}

type countingProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *countingProvider) Name() string {
	return "counting"
}

func (p *countingProvider) Run(_ context.Context, req provider.Request) (provider.Result, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return writeProviderArtifacts(req.OutputDir)
}

type workersCaptureProvider struct {
	mu      sync.Mutex
	workers []int
}

func (p *workersCaptureProvider) Name() string {
	return "capture"
}

func (p *workersCaptureProvider) Run(_ context.Context, req provider.Request) (provider.Result, error) {
	time.Sleep(20 * time.Millisecond)
	p.mu.Lock()
	p.workers = append(p.workers, req.MaxWorkers)
	p.mu.Unlock()
	return writeProviderArtifacts(req.OutputDir)
}

type progressProvider struct{}

func (p *progressProvider) Name() string {
	return "progress"
}

func (p *progressProvider) Run(_ context.Context, req provider.Request) (provider.Result, error) {
	if req.OnProgress != nil {
		req.OnProgress(provider.ProgressEvent{
			Phase:          "page_started",
			Stage:          "vision_ocr",
			CurrentPage:    1,
			CompletedPages: 0,
			TotalPages:     2,
		})
		req.OnProgress(provider.ProgressEvent{
			Phase:          "page_done",
			Stage:          "vision_ocr",
			CurrentPage:    1,
			CompletedPages: 1,
			TotalPages:     2,
		})
	}
	return writeProviderArtifacts(req.OutputDir)
}

func writeProviderArtifacts(outDir string) (provider.Result, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return provider.Result{}, err
	}

	searchable := filepath.Join(outDir, "searchable.pdf")
	if err := os.WriteFile(searchable, []byte("%PDF-1.4\n"), 0o644); err != nil {
		return provider.Result{}, err
	}

	pagesJSON := filepath.Join(outDir, "pages.json")
	if err := os.WriteFile(pagesJSON, []byte("{}\n"), 0o644); err != nil {
		return provider.Result{}, err
	}

	textPath := filepath.Join(outDir, "document.txt")
	if err := os.WriteFile(textPath, []byte(""), 0o644); err != nil {
		return provider.Result{}, err
	}

	markdown := filepath.Join(outDir, "document.md")
	if err := os.WriteFile(markdown, []byte("# mock\n"), 0o644); err != nil {
		return provider.Result{}, err
	}

	return provider.Result{
		SearchablePDF: searchable,
		PagesJSON:     pagesJSON,
		TextPath:      textPath,
		MarkdownPath:  markdown,
	}, nil
}

func writePDF(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write pdf failed: %v", err)
	}
}

func TestBatchRetryFailedAtEnd(t *testing.T) {
	temp := t.TempDir()
	inputDir := filepath.Join(temp, "in")
	writePDF(t, filepath.Join(inputDir, "a.pdf"))
	writePDF(t, filepath.Join(inputDir, "b.pdf"))

	outDir := filepath.Join(temp, "out")
	report, err := Run(context.Background(), &flakyProvider{}, Options{
		InputPath:      inputDir,
		OutputRoot:     outDir,
		Profile:        "fast",
		LocalOnly:      true,
		MaxWorkers:     0,
		MaxWorkersMode: "auto",
		Workers:        2,
		Resume:         false,
		Recursive:      false,
		RetryFailed:    1,
	})
	if err != nil {
		t.Fatalf("batch run failed: %v", err)
	}

	if report.Total != 2 || report.Succeeded != 2 || report.Failed != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestBatchBasicAndResume(t *testing.T) {
	temp := t.TempDir()
	inputDir := filepath.Join(temp, "in")
	writePDF(t, filepath.Join(inputDir, "a.pdf"))
	writePDF(t, filepath.Join(inputDir, "b.pdf"))

	outDir := filepath.Join(temp, "out")
	first, err := Run(context.Background(), &countingProvider{}, Options{
		InputPath:      inputDir,
		OutputRoot:     outDir,
		Profile:        "fast",
		LocalOnly:      true,
		MaxWorkers:     0,
		MaxWorkersMode: "auto",
		Workers:        2,
		Resume:         false,
		Recursive:      false,
		RetryFailed:    1,
	})
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if first.Total != 2 || first.Succeeded != 2 || first.Failed != 0 || first.Skipped != 0 {
		t.Fatalf("unexpected first report: %+v", first)
	}

	second, err := Run(context.Background(), &countingProvider{}, Options{
		InputPath:      inputDir,
		OutputRoot:     outDir,
		Profile:        "fast",
		LocalOnly:      true,
		MaxWorkers:     0,
		MaxWorkersMode: "auto",
		Workers:        2,
		Resume:         true,
		Recursive:      false,
		RetryFailed:    1,
	})
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if second.Total != 2 || second.Succeeded != 0 || second.Failed != 0 || second.Skipped != 2 {
		t.Fatalf("unexpected second report: %+v", second)
	}
}

func TestBatchResumeDoesNotRerunSucceededJobs(t *testing.T) {
	temp := t.TempDir()
	inputDir := filepath.Join(temp, "in")
	writePDF(t, filepath.Join(inputDir, "a.pdf"))

	outDir := filepath.Join(temp, "out")
	firstProvider := &countingProvider{}
	_, err := Run(context.Background(), firstProvider, Options{
		InputPath:      inputDir,
		OutputRoot:     outDir,
		Profile:        "fast",
		LocalOnly:      true,
		MaxWorkers:     0,
		MaxWorkersMode: "auto",
		Workers:        1,
		Resume:         false,
		Recursive:      false,
		RetryFailed:    1,
	})
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	secondProvider := &countingProvider{}
	secondReport, err := Run(context.Background(), secondProvider, Options{
		InputPath:      inputDir,
		OutputRoot:     outDir,
		Profile:        "fast",
		LocalOnly:      true,
		MaxWorkers:     0,
		MaxWorkersMode: "auto",
		Workers:        1,
		Resume:         true,
		Recursive:      false,
		RetryFailed:    1,
	})
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}

	secondProvider.mu.Lock()
	calls := secondProvider.calls
	secondProvider.mu.Unlock()

	if calls != 0 {
		t.Fatalf("expected no provider calls on resume, got %d", calls)
	}
	if secondReport.Failed != 0 || secondReport.Succeeded != 0 || secondReport.Skipped != 1 {
		t.Fatalf("unexpected second report: %+v", secondReport)
	}
}

func TestBatchParallelWorkersAndOverride(t *testing.T) {
	temp := t.TempDir()
	inputDir := filepath.Join(temp, "in")
	writePDF(t, filepath.Join(inputDir, "a.pdf"))
	writePDF(t, filepath.Join(inputDir, "b.pdf"))
	writePDF(t, filepath.Join(inputDir, "c.pdf"))

	providerCapture := &workersCaptureProvider{}
	outDir := filepath.Join(temp, "out")
	report, err := Run(context.Background(), providerCapture, Options{
		InputPath:      inputDir,
		OutputRoot:     outDir,
		Profile:        "fast",
		LocalOnly:      true,
		MaxWorkers:     6,
		MaxWorkersMode: "manual",
		Workers:        3,
		Resume:         false,
		Recursive:      false,
		RetryFailed:    1,
	})
	if err != nil {
		t.Fatalf("batch run failed: %v", err)
	}
	if report.Total != 3 || report.Succeeded != 3 || report.Failed != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}

	providerCapture.mu.Lock()
	workers := append([]int(nil), providerCapture.workers...)
	providerCapture.mu.Unlock()
	sort.Ints(workers)
	if len(workers) != 3 || workers[0] != 6 || workers[1] != 6 || workers[2] != 6 {
		t.Fatalf("unexpected captured max_workers: %+v", workers)
	}

	body, err := os.ReadFile(filepath.Join(outDir, "batch_report.json"))
	if err != nil {
		t.Fatalf("read batch_report failed: %v", err)
	}
	reportJSON := map[string]any{}
	if err := json.Unmarshal(body, &reportJSON); err != nil {
		t.Fatalf("parse batch_report failed: %v", err)
	}
	if reportJSON["workers_requested"] != float64(3) {
		t.Fatalf("expected workers_requested=3, got %v", reportJSON["workers_requested"])
	}
	if reportJSON["effective_workers"] != float64(3) {
		t.Fatalf("expected effective_workers=3, got %v", reportJSON["effective_workers"])
	}
	if reportJSON["max_workers_override"] != float64(6) {
		t.Fatalf("expected max_workers_override=6, got %v", reportJSON["max_workers_override"])
	}
}

func TestBatchProgressSnapshots(t *testing.T) {
	temp := t.TempDir()
	inputDir := filepath.Join(temp, "in")
	writePDF(t, filepath.Join(inputDir, "a.pdf"))
	writePDF(t, filepath.Join(inputDir, "b.pdf"))

	outDir := filepath.Join(temp, "out")
	var mu sync.Mutex
	snapshots := []ProgressSnapshot{}
	_, err := Run(context.Background(), &countingProvider{}, Options{
		InputPath:      inputDir,
		OutputRoot:     outDir,
		Profile:        "fast",
		LocalOnly:      true,
		MaxWorkers:     0,
		MaxWorkersMode: "auto",
		Workers:        2,
		Resume:         false,
		Recursive:      false,
		RetryFailed:    0,
		OnProgress: func(snapshot ProgressSnapshot) {
			mu.Lock()
			snapshots = append(snapshots, snapshot)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("batch run failed: %v", err)
	}

	mu.Lock()
	copied := append([]ProgressSnapshot(nil), snapshots...)
	mu.Unlock()
	if len(copied) < 3 {
		t.Fatalf("expected at least 3 progress snapshots, got %d", len(copied))
	}
	if copied[0].Phase != ProgressPhaseStart {
		t.Fatalf("expected first snapshot to be start, got %s", copied[0].Phase)
	}
	last := copied[len(copied)-1]
	if last.Phase != ProgressPhaseDone {
		t.Fatalf("expected final snapshot to be done, got %s", last.Phase)
	}
	if last.Completed != 2 || last.Succeeded != 2 || last.Total != 2 {
		t.Fatalf("unexpected final snapshot: %+v", last)
	}
}

func TestBatchProgressSnapshotsIncludeProviderPageProgress(t *testing.T) {
	temp := t.TempDir()
	inputDir := filepath.Join(temp, "in")
	writePDF(t, filepath.Join(inputDir, "a.pdf"))

	outDir := filepath.Join(temp, "out")
	var mu sync.Mutex
	snapshots := []ProgressSnapshot{}
	_, err := Run(context.Background(), &progressProvider{}, Options{
		InputPath:      inputDir,
		OutputRoot:     outDir,
		Profile:        "fast",
		LocalOnly:      true,
		MaxWorkers:     2,
		MaxWorkersMode: "manual",
		Workers:        1,
		Resume:         false,
		Recursive:      false,
		RetryFailed:    0,
		OnProgress: func(snapshot ProgressSnapshot) {
			mu.Lock()
			snapshots = append(snapshots, snapshot)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("batch run failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, snapshot := range snapshots {
		if snapshot.CurrentStage == "vision_ocr" && snapshot.TotalPages == 2 && snapshot.CurrentPage == 1 && snapshot.CompletedPages == 1 {
			return
		}
	}
	t.Fatalf("expected provider page progress snapshot, got %+v", snapshots)
}

func TestBatchFailsFastWhenRemotePostprocessIsNotAllowed(t *testing.T) {
	temp := t.TempDir()
	inputDir := filepath.Join(temp, "in")
	writePDF(t, filepath.Join(inputDir, "a.pdf"))

	providerCapture := &countingProvider{}
	_, err := Run(context.Background(), providerCapture, Options{
		InputPath:           inputDir,
		OutputRoot:          filepath.Join(temp, "out"),
		Profile:             "fast",
		LocalOnly:           true,
		MaxWorkers:          1,
		MaxWorkersMode:      "manual",
		PostprocessProvider: postprocess.ProviderCodexHeadlessOAuth,
		Workers:             1,
		Resume:              false,
		Recursive:           false,
		RetryFailed:         0,
	})
	if err == nil {
		t.Fatalf("expected local-only postprocess error")
	}

	providerCapture.mu.Lock()
	calls := providerCapture.calls
	providerCapture.mu.Unlock()
	if calls != 0 {
		t.Fatalf("expected provider not to run, got %d calls", calls)
	}
}
