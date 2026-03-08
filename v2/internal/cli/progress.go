package cli

import (
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/batch"
	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
)

type batchProgressRenderer struct {
	mu                       sync.Mutex
	w                        io.Writer
	lastLen                  int
	maxWorkersWarningPrinted bool
	activePDFs               []string
	activeSet                map[string]struct{}
	lastPDF                  string
}

type runProgressRenderer struct {
	mu       sync.Mutex
	w        io.Writer
	lastLen  int
	inputPDF string
	start    time.Time
}

const maxWorkersNotAppliedWarning = "max_workers_not_applied_yet_in_swift_provider"

func newBatchProgressRenderer(w io.Writer) *batchProgressRenderer {
	return &batchProgressRenderer{
		w:         w,
		activeSet: make(map[string]struct{}),
	}
}

func newRunProgressRenderer(w io.Writer, inputPDF string) *runProgressRenderer {
	return &runProgressRenderer{
		w:        w,
		inputPDF: filepath.Base(inputPDF),
		start:    time.Now(),
	}
}

func (r *batchProgressRenderer) Render(snapshot batch.ProgressSnapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if snapshot.Total <= 0 {
		return
	}
	r.updateActivePDFs(snapshot)
	if snapshot.CurrentError == maxWorkersNotAppliedWarning && !r.maxWorkersWarningPrinted {
		r.finishLocked()
		_, _ = fmt.Fprintln(
			r.w,
			"warning: provider reported max-workers was not applied; page OCR is running serially",
		)
		r.maxWorkersWarningPrinted = true
	}

	percent := 0.0
	if snapshot.Total > 0 {
		percent = (float64(snapshot.Completed) / float64(snapshot.Total)) * 100
	}

	rate := 0.0
	if snapshot.Elapsed > 0 {
		rate = float64(snapshot.Completed) / snapshot.Elapsed.Seconds()
	}

	eta := "--:--"
	if rate > 0 && snapshot.Completed < snapshot.Total {
		remaining := float64(snapshot.Total-snapshot.Completed) / rate
		eta = formatDuration(time.Duration(remaining * float64(time.Second)))
	}

	line := fmt.Sprintf(
		"%s %5.1f%% %d/%d pdf | %s | %.2f pdf/s | %s<%s%s%s",
		renderProgressBar(snapshot.Completed, snapshot.Total, 24),
		percent,
		snapshot.Completed,
		snapshot.Total,
		formatBatchCounts(snapshot),
		rate,
		formatDuration(snapshot.Elapsed),
		eta,
		r.describePDFActivity(),
		describePageProgress(snapshot),
	)

	r.write(line, snapshot.Phase == batch.ProgressPhaseDone)
}

func (r *batchProgressRenderer) updateActivePDFs(snapshot batch.ProgressSnapshot) {
	if snapshot.CurrentInputPDF == "" {
		return
	}
	name := filepath.Base(snapshot.CurrentInputPDF)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return
	}
	r.lastPDF = name

	switch snapshot.Phase {
	case batch.ProgressPhaseJobStarted:
		if _, exists := r.activeSet[name]; exists {
			return
		}
		r.activeSet[name] = struct{}{}
		r.activePDFs = append(r.activePDFs, name)
	case batch.ProgressPhaseJobDone:
		if _, exists := r.activeSet[name]; !exists {
			return
		}
		delete(r.activeSet, name)
		next := r.activePDFs[:0]
		for _, active := range r.activePDFs {
			if active != name {
				next = append(next, active)
			}
		}
		r.activePDFs = next
	}
}

func (r *batchProgressRenderer) describePDFActivity() string {
	if len(r.activePDFs) == 1 {
		return " | active " + r.activePDFs[0]
	}
	if len(r.activePDFs) == 2 {
		return " | active " + r.activePDFs[0] + "," + r.activePDFs[1]
	}
	if len(r.activePDFs) > 2 {
		preview := strings.Join(r.activePDFs[:2], ",")
		return fmt.Sprintf(" | active %d(%s,+%d)", len(r.activePDFs), preview, len(r.activePDFs)-2)
	}
	if r.lastPDF != "" {
		return " | last " + r.lastPDF
	}
	return ""
}

func describePageProgress(snapshot batch.ProgressSnapshot) string {
	if snapshot.CurrentStage == "" {
		return ""
	}
	pdfName := filepath.Base(snapshot.CurrentInputPDF)
	stage := displayStageName(snapshot.CurrentStage)
	if snapshot.CurrentStage == "vision_ocr" && snapshot.TotalPages > 0 {
		if snapshot.CurrentPage > 0 {
			return fmt.Sprintf(" | %s %s %d/%d p%d", stage, pdfName, snapshot.CompletedPages, snapshot.TotalPages, snapshot.CurrentPage)
		}
		return fmt.Sprintf(" | %s %s %d/%d", stage, pdfName, snapshot.CompletedPages, snapshot.TotalPages)
	}
	if pdfName != "" {
		return fmt.Sprintf(" | %s %s", stage, pdfName)
	}
	return " | " + stage
}

func displayStageName(stage string) string {
	switch stage {
	case "vision_ocr":
		return "ocr"
	case "serialization":
		return "serialize"
	case "searchable_pdf":
		return "searchable"
	default:
		return stage
	}
}

func (r *batchProgressRenderer) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishLocked()
}

func (r *runProgressRenderer) Render(event provider.ProgressEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	stage := displayStageName(event.Stage)
	if stage == "" {
		stage = "run"
	}

	elapsed := time.Since(r.start)
	line := fmt.Sprintf("%s %s | %s", stage, r.inputPDF, formatDuration(elapsed))
	if event.Stage == "vision_ocr" && event.TotalPages > 0 {
		percent := (float64(event.CompletedPages) / float64(event.TotalPages)) * 100
		rate := 0.0
		if elapsed > 0 {
			rate = float64(event.CompletedPages) / elapsed.Seconds()
		}
		eta := "--:--"
		if rate > 0 && event.CompletedPages < event.TotalPages {
			remaining := float64(event.TotalPages-event.CompletedPages) / rate
			eta = formatDuration(time.Duration(remaining * float64(time.Second)))
		}
		line = fmt.Sprintf(
			"%s %5.1f%% %d/%d pg | %.2f pg/s | %s<%s | %s %s",
			renderProgressBar(event.CompletedPages, event.TotalPages, 24),
			percent,
			event.CompletedPages,
			event.TotalPages,
			rate,
			formatDuration(elapsed),
			eta,
			stage,
			r.inputPDF,
		)
		if event.CurrentPage > 0 {
			line += fmt.Sprintf(" p%d", event.CurrentPage)
		}
	}

	r.writeLocked(line, false)
}

func (r *runProgressRenderer) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastLen == 0 {
		return
	}
	_, _ = fmt.Fprintln(r.w)
	r.lastLen = 0
}

func (r *batchProgressRenderer) finishLocked() {
	if r.lastLen == 0 {
		return
	}
	_, _ = fmt.Fprintln(r.w)
	r.lastLen = 0
}

func (r *batchProgressRenderer) write(line string, done bool) {
	r.writeLocked(line, done)
}

func (r *batchProgressRenderer) writeLocked(line string, done bool) {
	if len(line) < r.lastLen {
		line += strings.Repeat(" ", r.lastLen-len(line))
	}
	_, _ = fmt.Fprintf(r.w, "\r%s", line)
	r.lastLen = len(line)
	if done {
		_, _ = fmt.Fprintln(r.w)
		r.lastLen = 0
	}
}

func (r *runProgressRenderer) writeLocked(line string, done bool) {
	if len(line) < r.lastLen {
		line += strings.Repeat(" ", r.lastLen-len(line))
	}
	_, _ = fmt.Fprintf(r.w, "\r%s", line)
	r.lastLen = len(line)
	if done {
		_, _ = fmt.Fprintln(r.w)
		r.lastLen = 0
	}
}

func formatBatchCounts(snapshot batch.ProgressSnapshot) string {
	parts := []string{
		fmt.Sprintf("ok=%d", snapshot.Succeeded),
		fmt.Sprintf("fail=%d", snapshot.Failed),
		fmt.Sprintf("run=%d", snapshot.Running),
	}
	if snapshot.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("skip=%d", snapshot.Skipped))
	}
	return strings.Join(parts, " ")
}

func renderProgressBar(completed, total, width int) string {
	if width < 8 {
		width = 8
	}
	if total <= 0 {
		return "[" + strings.Repeat("-", width) + "]"
	}

	ratio := float64(completed) / float64(total)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	filled := int(math.Round(ratio * float64(width)))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	return "[" + strings.Repeat("=", filled) + strings.Repeat("-", width-filled) + "]"
}

func formatDuration(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	rounded := value.Round(time.Second)
	hours := int(rounded / time.Hour)
	minutes := int((rounded % time.Hour) / time.Minute)
	seconds := int((rounded % time.Minute) / time.Second)
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}
