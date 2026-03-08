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

const maxWorkersNotAppliedWarning = "max_workers_not_applied_yet_in_swift_provider"

func newBatchProgressRenderer(w io.Writer) *batchProgressRenderer {
	return &batchProgressRenderer{
		w:         w,
		activeSet: make(map[string]struct{}),
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
		"%s %6.2f%% %d/%d pdf | ok=%d fail=%d skip=%d run=%d | %.2f pdf/s | elapsed=%s eta=%s%s",
		renderProgressBar(snapshot.Completed, snapshot.Total, 24),
		percent,
		snapshot.Completed,
		snapshot.Total,
		snapshot.Succeeded,
		snapshot.Failed,
		snapshot.Skipped,
		snapshot.Running,
		rate,
		formatDuration(snapshot.Elapsed),
		eta,
		r.describePDFActivity(),
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
		return " | active=" + r.activePDFs[0]
	}
	if len(r.activePDFs) == 2 {
		return " | active=" + r.activePDFs[0] + "," + r.activePDFs[1]
	}
	if len(r.activePDFs) > 2 {
		preview := strings.Join(r.activePDFs[:2], ",")
		return fmt.Sprintf(" | active=%d (%s,+%d)", len(r.activePDFs), preview, len(r.activePDFs)-2)
	}
	if r.lastPDF != "" {
		return " | last=" + r.lastPDF
	}
	return ""
}

func (r *batchProgressRenderer) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishLocked()
}

func (r *batchProgressRenderer) finishLocked() {
	if r.lastLen == 0 {
		return
	}
	_, _ = fmt.Fprintln(r.w)
	r.lastLen = 0
}

func (r *batchProgressRenderer) write(line string, done bool) {
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
