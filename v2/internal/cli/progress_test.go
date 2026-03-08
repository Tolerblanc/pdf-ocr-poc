package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/batch"
	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
)

type fakeTTYWriter struct {
	bytes.Buffer
}

func (w *fakeTTYWriter) Stat() (os.FileInfo, error) {
	return fakeTTYInfo{}, nil
}

type fakeTTYInfo struct{}

func (fakeTTYInfo) Name() string       { return "tty" }
func (fakeTTYInfo) Size() int64        { return 0 }
func (fakeTTYInfo) Mode() os.FileMode  { return os.ModeCharDevice }
func (fakeTTYInfo) ModTime() time.Time { return time.Time{} }
func (fakeTTYInfo) IsDir() bool        { return false }
func (fakeTTYInfo) Sys() any           { return nil }

func TestRenderProgressBarBoundaries(t *testing.T) {
	bar := renderProgressBar(0, 10, 10)
	if bar != "[----------]" {
		t.Fatalf("unexpected empty bar: %s", bar)
	}

	full := renderProgressBar(10, 10, 10)
	if full != "[==========]" {
		t.Fatalf("unexpected full bar: %s", full)
	}
}

func TestBatchProgressRendererDoneLine(t *testing.T) {
	out := fakeTTYWriter{}
	renderer := newBatchProgressRenderer(&out)
	renderer.Render(batch.ProgressSnapshot{
		Phase:     batch.ProgressPhaseDone,
		Total:     3,
		Completed: 3,
		Succeeded: 3,
		Elapsed:   5 * time.Second,
	})

	if !strings.Contains(out.String(), "3/3") {
		t.Fatalf("expected completed counter in output, got: %q", out.String())
	}
	if !strings.Contains(out.String(), "pdf/s") {
		t.Fatalf("expected pdf throughput label, got: %q", out.String())
	}
	if !strings.HasSuffix(out.String(), "\n") {
		t.Fatalf("expected trailing newline, got: %q", out.String())
	}
}

func TestBatchProgressRendererShowsCurrentPDF(t *testing.T) {
	out := fakeTTYWriter{}
	renderer := newBatchProgressRenderer(&out)
	renderer.Render(batch.ProgressSnapshot{
		Phase:           batch.ProgressPhaseJobStarted,
		Total:           4,
		Completed:       1,
		Succeeded:       1,
		Running:         1,
		CurrentStage:    "vision_ocr",
		CurrentPage:     2,
		CompletedPages:  1,
		TotalPages:      3,
		CurrentInputPDF: "/tmp/contracts/a.pdf",
		Elapsed:         2 * time.Second,
	})

	printed := out.String()
	if !strings.Contains(printed, "active a.pdf") {
		t.Fatalf("expected active pdf name in output, got: %q", printed)
	}
	if !strings.Contains(printed, "1/4 pdf") {
		t.Fatalf("expected pdf unit in output, got: %q", printed)
	}
	if !strings.Contains(printed, "ocr a.pdf 1/3 p2") {
		t.Fatalf("expected page progress in output, got: %q", printed)
	}
	if !strings.Contains(printed, "pdf/s") {
		t.Fatalf("expected pdf throughput label, got: %q", printed)
	}
}

func TestRunProgressRendererShowsPageProgress(t *testing.T) {
	out := fakeTTYWriter{}
	renderer := newRunProgressRenderer(&out, "/tmp/contracts/a.pdf")
	renderer.Render(provider.ProgressEvent{
		Phase:          "page_started",
		Stage:          "vision_ocr",
		CurrentPage:    2,
		CompletedPages: 1,
		TotalPages:     3,
	})
	if out.String() != "" {
		t.Fatalf("expected page_started event to be suppressed, got: %q", out.String())
	}
	renderer.Render(provider.ProgressEvent{
		Phase:          "page_done",
		Stage:          "vision_ocr",
		CurrentPage:    2,
		CompletedPages: 1,
		TotalPages:     3,
	})

	printed := out.String()
	if !strings.Contains(printed, "1/3 pg") {
		t.Fatalf("expected page counter in output, got: %q", printed)
	}
	if !strings.Contains(printed, "| ocr p2") {
		t.Fatalf("expected compact OCR stage marker in output, got: %q", printed)
	}
	if strings.Contains(printed, "a.pdf") {
		t.Fatalf("expected compact output without pdf name, got: %q", printed)
	}
	if !strings.HasPrefix(printed, "\r[") {
		t.Fatalf("expected progress bar prefix, got: %q", printed)
	}
}

func TestRunProgressRendererUsesSparseLogsWhenNotInteractive(t *testing.T) {
	var out bytes.Buffer
	renderer := newRunProgressRenderer(&out, "/tmp/contracts/a.pdf")
	renderer.Render(provider.ProgressEvent{
		Phase:          "document_started",
		Stage:          "vision_ocr",
		CompletedPages: 0,
		TotalPages:     100,
	})
	for page := 1; page <= 4; page++ {
		renderer.Render(provider.ProgressEvent{
			Phase:          "page_done",
			Stage:          "vision_ocr",
			CurrentPage:    page,
			CompletedPages: page,
			TotalPages:     100,
		})
	}

	printed := out.String()
	if strings.Contains(printed, "2/100 pg") || strings.Contains(printed, "3/100 pg") || strings.Contains(printed, "4/100 pg") {
		t.Fatalf("expected sparse non-interactive logs, got: %q", printed)
	}
	if !strings.Contains(printed, "1/100 pg") {
		t.Fatalf("expected first milestone to be printed, got: %q", printed)
	}
	if strings.Contains(printed, "\r[") {
		t.Fatalf("expected newline logging for non-interactive writer, got: %q", printed)
	}
}

func TestRunProgressRendererShowsPostprocessPageProgress(t *testing.T) {
	out := fakeTTYWriter{}
	renderer := newRunProgressRenderer(&out, "/tmp/contracts/a.pdf")
	renderer.Render(provider.ProgressEvent{
		Phase:          "stage_started",
		Stage:          "postprocess",
		CompletedPages: 0,
		TotalPages:     3,
	})
	renderer.Render(provider.ProgressEvent{
		Phase:          "page_done",
		Stage:          "postprocess",
		CurrentPage:    2,
		CompletedPages: 1,
		TotalPages:     3,
	})

	printed := out.String()
	if !strings.Contains(printed, "1/3 pg") {
		t.Fatalf("expected page counter in output, got: %q", printed)
	}
	if !strings.Contains(printed, "| postprocess p2") {
		t.Fatalf("expected compact postprocess stage marker in output, got: %q", printed)
	}
	if strings.Contains(printed, "a.pdf") {
		t.Fatalf("expected compact output without pdf name, got: %q", printed)
	}
	if !strings.HasPrefix(printed, "\r[") {
		t.Fatalf("expected progress bar prefix, got: %q", printed)
	}
}
