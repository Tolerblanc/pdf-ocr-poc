package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/batch"
	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
)

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
	var out bytes.Buffer
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
	var out bytes.Buffer
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
	if !strings.Contains(printed, "active=a.pdf") {
		t.Fatalf("expected active pdf name in output, got: %q", printed)
	}
	if !strings.Contains(printed, "1/4 pdf") {
		t.Fatalf("expected pdf unit in output, got: %q", printed)
	}
	if !strings.Contains(printed, "ocr=a.pdf 1/3 pages (p2)") {
		t.Fatalf("expected page progress in output, got: %q", printed)
	}
	if !strings.Contains(printed, "pdf/s") {
		t.Fatalf("expected pdf throughput label, got: %q", printed)
	}
}

func TestRunProgressRendererShowsPageProgress(t *testing.T) {
	var out bytes.Buffer
	renderer := newRunProgressRenderer(&out, "/tmp/contracts/a.pdf")
	renderer.Render(provider.ProgressEvent{
		Stage:          "vision_ocr",
		CurrentPage:    2,
		CompletedPages: 1,
		TotalPages:     3,
	})

	printed := out.String()
	if !strings.Contains(printed, "1/3 pages") {
		t.Fatalf("expected page counter in output, got: %q", printed)
	}
	if !strings.Contains(printed, "ocr=a.pdf") {
		t.Fatalf("expected stage and pdf name in output, got: %q", printed)
	}
	if !strings.Contains(printed, "(p2)") {
		t.Fatalf("expected current page marker in output, got: %q", printed)
	}
	if !strings.Contains(printed, "pages/s") {
		t.Fatalf("expected pages throughput label, got: %q", printed)
	}
	if !strings.HasPrefix(printed, "\r[") {
		t.Fatalf("expected progress bar prefix, got: %q", printed)
	}
}
