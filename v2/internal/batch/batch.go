package batch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/postprocess"
	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
	runpkg "github.com/Tolerblanc/pdf-ocr-poc/v2/internal/run"
)

type JobStatus string

const (
	StatusPending   JobStatus = "pending"
	StatusRunning   JobStatus = "running"
	StatusSucceeded JobStatus = "succeeded"
	StatusFailed    JobStatus = "failed"
	StatusSkipped   JobStatus = "skipped"
)

type Options struct {
	InputPath             string
	OutputRoot            string
	Profile               string
	LocalOnly             bool
	MaxWorkers            int
	MaxWorkersMode        string
	PostprocessProvider   string
	PostprocessConfigPath string
	Workers               int
	Recursive             bool
	Resume                bool
	RetryFailed           int
	OnProgress            func(ProgressSnapshot)
}

type ProgressPhase string

const (
	ProgressPhaseStart      ProgressPhase = "start"
	ProgressPhaseJobStarted ProgressPhase = "job_started"
	ProgressPhaseJobDone    ProgressPhase = "job_done"
	ProgressPhaseDone       ProgressPhase = "done"
)

type ProgressSnapshot struct {
	Phase            ProgressPhase
	Attempt          int
	TotalAttempts    int
	Total            int
	Completed        int
	Pending          int
	Running          int
	Succeeded        int
	Failed           int
	Skipped          int
	EffectiveWorkers int
	CurrentInputPDF  string
	CurrentStage     string
	CurrentPage      int
	CompletedPages   int
	TotalPages       int
	CurrentError     string
	Elapsed          time.Duration
}

type Job struct {
	InputPDF    string    `json:"input_pdf"`
	RunDir      string    `json:"run_dir"`
	Status      JobStatus `json:"status"`
	Attempts    int       `json:"attempts"`
	Retryable   bool      `json:"retryable"`
	Error       string    `json:"error,omitempty"`
	StartedAt   string    `json:"started_at,omitempty"`
	CompletedAt string    `json:"completed_at,omitempty"`
}

type State struct {
	Version      int    `json:"version"`
	InputPath    string `json:"input_path"`
	OutputRoot   string `json:"output_root"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	Workers      int    `json:"workers"`
	RetryFailed  int    `json:"retry_failed"`
	ProviderName string `json:"provider"`
	Jobs         []Job  `json:"jobs"`
}

type Report struct {
	Total      int    `json:"total"`
	Succeeded  int    `json:"succeeded"`
	Failed     int    `json:"failed"`
	Skipped    int    `json:"skipped"`
	StatePath  string `json:"state_path"`
	ReportPath string `json:"report_path"`
}

func Run(ctx context.Context, p provider.Provider, opts Options) (Report, error) {
	if opts.InputPath == "" {
		return Report{}, errors.New("input path is required")
	}
	if opts.OutputRoot == "" {
		return Report{}, errors.New("output root is required")
	}
	if opts.Workers < 1 {
		return Report{}, errors.New("workers must be >= 1")
	}
	if opts.RetryFailed < 0 {
		return Report{}, errors.New("retry-failed must be >= 0")
	}

	pdfs, err := discoverPDFs(opts.InputPath, opts.Recursive)
	if err != nil {
		return Report{}, err
	}

	if err := os.MkdirAll(opts.OutputRoot, 0o755); err != nil {
		return Report{}, err
	}

	resolvedPostprocess, err := postprocess.ResolveConfig(opts.PostprocessProvider, opts.PostprocessConfigPath)
	if err != nil {
		return Report{}, err
	}
	if err := postprocess.ValidateExecution(resolvedPostprocess, opts.LocalOnly); err != nil {
		return Report{}, err
	}

	statePath := filepath.Join(opts.OutputRoot, "batch_state.json")
	state, err := loadOrInitState(statePath, p.Name(), pdfs, opts)
	if err != nil {
		return Report{}, err
	}

	start := time.Now()
	totalAttempts := opts.RetryFailed + 1
	postprocessProvider := strings.TrimSpace(opts.PostprocessProvider)
	effectiveWorkers := effectiveWorkersForJobs(opts.Workers, state.Jobs)
	emitProgress(opts, buildProgressSnapshot(
		state,
		effectiveWorkers,
		start,
		ProgressPhaseStart,
		0,
		totalAttempts,
		"",
		provider.ProgressEvent{},
		"",
	))

	var mu sync.Mutex
	for attempt := 0; attempt <= opts.RetryFailed; attempt++ {
		indices := runnableJobIndices(state.Jobs, attempt)
		if len(indices) == 0 {
			continue
		}
		runJobs(
			ctx,
			p,
			postprocessProvider,
			opts,
			statePath,
			state,
			indices,
			attempt+1,
			totalAttempts,
			effectiveWorkers,
			start,
			&mu,
		)
	}
	emitProgress(opts, buildProgressSnapshot(
		state,
		effectiveWorkers,
		start,
		ProgressPhaseDone,
		totalAttempts,
		totalAttempts,
		"",
		provider.ProgressEvent{},
		"",
	))

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveState(statePath, state); err != nil {
		return Report{}, err
	}

	reportPath := filepath.Join(opts.OutputRoot, "batch_report.json")
	report := buildReport(reportPath, statePath, state)
	maxWorkersOverride := any(nil)
	if opts.MaxWorkersMode == "manual" {
		maxWorkersOverride = opts.MaxWorkers
	}
	if err := writeJSON(reportPath, map[string]any{
		"input_path":              opts.InputPath,
		"output_root":             opts.OutputRoot,
		"provider":                p.Name(),
		"postprocess_provider":    resolvedPostprocess.Config.Provider,
		"postprocess_profile":     resolvedPostprocess.Profile,
		"postprocess_config_path": resolvedPostprocess.Path,
		"profile":                 opts.Profile,
		"local_only":              opts.LocalOnly,
		"workers_requested":       opts.Workers,
		"effective_workers":       effectiveWorkers,
		"retry_failed":            opts.RetryFailed,
		"max_workers":             opts.MaxWorkers,
		"workers_mode":            opts.MaxWorkersMode,
		"max_workers_override":    maxWorkersOverride,
		"state_path":              statePath,
		"generated_at":            time.Now().UTC().Format(time.RFC3339),
		"total":                   report.Total,
		"succeeded":               report.Succeeded,
		"failed":                  report.Failed,
		"skipped":                 report.Skipped,
		"jobs":                    state.Jobs,
	}); err != nil {
		return Report{}, err
	}

	return report, nil
}

func runJobs(
	ctx context.Context,
	p provider.Provider,
	postprocessProvider string,
	opts Options,
	statePath string,
	state *State,
	indices []int,
	attempt int,
	totalAttempts int,
	effectiveWorkers int,
	start time.Time,
	mu *sync.Mutex,
) {
	jobsCh := make(chan int)
	var wg sync.WaitGroup

	workerCount := opts.Workers
	if workerCount > len(indices) {
		workerCount = len(indices)
	}
	if workerCount < 1 {
		workerCount = 1
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobsCh {
				var startSnapshot ProgressSnapshot
				var emitStart bool

				mu.Lock()
				job := state.Jobs[idx]
				job.Status = StatusRunning
				job.StartedAt = time.Now().UTC().Format(time.RFC3339)
				job.Error = ""
				state.Jobs[idx] = job
				state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				_ = saveState(statePath, state)
				startSnapshot = buildProgressSnapshot(
					state,
					effectiveWorkers,
					start,
					ProgressPhaseJobStarted,
					attempt,
					totalAttempts,
					job.InputPDF,
					provider.ProgressEvent{},
					"",
				)
				emitStart = true
				mu.Unlock()
				if emitStart {
					emitProgress(opts, startSnapshot)
				}

				execOutput, err := runpkg.Execute(ctx, p, runpkg.Options{
					InputPDF:              job.InputPDF,
					OutputDir:             job.RunDir,
					Profile:               opts.Profile,
					LocalOnly:             opts.LocalOnly,
					MaxWorkers:            opts.MaxWorkers,
					MaxWorkersMode:        opts.MaxWorkersMode,
					PostprocessProvider:   postprocessProvider,
					PostprocessConfigPath: opts.PostprocessConfigPath,
					OnProgress: func(event provider.ProgressEvent) {
						mu.Lock()
						snapshot := buildProgressSnapshot(
							state,
							effectiveWorkers,
							start,
							ProgressPhaseJobStarted,
							attempt,
							totalAttempts,
							job.InputPDF,
							event,
							"",
						)
						mu.Unlock()
						emitProgress(opts, snapshot)
					},
				})

				var doneSnapshot ProgressSnapshot
				var emitDone bool
				mu.Lock()
				job = state.Jobs[idx]
				job.Attempts++
				job.CompletedAt = time.Now().UTC().Format(time.RFC3339)
				jobErr := ""
				if err == nil {
					job.Status = StatusSucceeded
					job.Retryable = false
					job.Error = ""
					if hasWarning(execOutput.Result.Warnings, "max_workers_not_applied_yet_in_swift_provider") {
						jobErr = "max_workers_not_applied_yet_in_swift_provider"
					}
				} else {
					job.Status = StatusFailed
					job.Retryable = isRetryable(err)
					job.Error = err.Error()
					jobErr = err.Error()
				}
				state.Jobs[idx] = job
				state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				_ = saveState(statePath, state)
				doneSnapshot = buildProgressSnapshot(
					state,
					effectiveWorkers,
					start,
					ProgressPhaseJobDone,
					attempt,
					totalAttempts,
					job.InputPDF,
					provider.ProgressEvent{},
					jobErr,
				)
				emitDone = true
				mu.Unlock()
				if emitDone {
					emitProgress(opts, doneSnapshot)
				}
			}
		}()
	}

	for _, idx := range indices {
		jobsCh <- idx
	}
	close(jobsCh)
	wg.Wait()
}

func buildProgressSnapshot(
	state *State,
	effectiveWorkers int,
	start time.Time,
	phase ProgressPhase,
	attempt int,
	totalAttempts int,
	currentInputPDF string,
	progress provider.ProgressEvent,
	currentError string,
) ProgressSnapshot {
	pending, running, succeeded, failed, skipped := countJobStatuses(state.Jobs)
	return ProgressSnapshot{
		Phase:            phase,
		Attempt:          attempt,
		TotalAttempts:    totalAttempts,
		Total:            len(state.Jobs),
		Completed:        succeeded + failed + skipped,
		Pending:          pending,
		Running:          running,
		Succeeded:        succeeded,
		Failed:           failed,
		Skipped:          skipped,
		EffectiveWorkers: effectiveWorkers,
		CurrentInputPDF:  currentInputPDF,
		CurrentStage:     progress.Stage,
		CurrentPage:      progress.CurrentPage,
		CompletedPages:   progress.CompletedPages,
		TotalPages:       progress.TotalPages,
		CurrentError:     currentError,
		Elapsed:          time.Since(start),
	}
}

func emitProgress(opts Options, snapshot ProgressSnapshot) {
	if opts.OnProgress == nil {
		return
	}
	opts.OnProgress(snapshot)
}

func countJobStatuses(jobs []Job) (int, int, int, int, int) {
	pending := 0
	running := 0
	succeeded := 0
	failed := 0
	skipped := 0
	for _, job := range jobs {
		switch job.Status {
		case StatusPending:
			pending++
		case StatusRunning:
			running++
		case StatusSucceeded:
			succeeded++
		case StatusFailed:
			failed++
		case StatusSkipped:
			skipped++
		}
	}
	return pending, running, succeeded, failed, skipped
}

func hasWarning(warnings []string, target string) bool {
	for _, warning := range warnings {
		if warning == target {
			return true
		}
	}
	return false
}

func runnableJobIndices(jobs []Job, attempt int) []int {
	indices := make([]int, 0, len(jobs))
	for idx := range jobs {
		job := jobs[idx]
		switch {
		case attempt == 0 && job.Status == StatusPending:
			indices = append(indices, idx)
		case attempt > 0 && job.Status == StatusFailed && job.Retryable && job.Attempts == attempt:
			indices = append(indices, idx)
		}
	}
	return indices
}

func discoverPDFs(inputPath string, recursive bool) ([]string, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(inputPath), ".pdf") {
			return nil, fmt.Errorf("input file must be .pdf: %s", inputPath)
		}
		return []string{inputPath}, nil
	}

	pdfs := []string{}
	if recursive {
		err = filepath.WalkDir(inputPath, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".pdf") {
				pdfs = append(pdfs, path)
			}
			return nil
		})
	} else {
		entries, readErr := os.ReadDir(inputPath)
		if readErr != nil {
			return nil, readErr
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.EqualFold(filepath.Ext(name), ".pdf") {
				pdfs = append(pdfs, filepath.Join(inputPath, name))
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if len(pdfs) == 0 {
		return nil, fmt.Errorf("no pdf files found under: %s", inputPath)
	}
	sort.Strings(pdfs)
	return pdfs, nil
}

func loadOrInitState(statePath, providerName string, pdfs []string, opts Options) (*State, error) {
	existing := &State{}
	hasExisting := false
	if opts.Resume {
		if body, err := os.ReadFile(statePath); err == nil {
			if err := json.Unmarshal(body, existing); err != nil {
				return nil, fmt.Errorf("state parse failed: %w", err)
			}
			hasExisting = true
		}
	}

	jobsByPDF := map[string]Job{}
	if hasExisting {
		for _, job := range existing.Jobs {
			if job.Status == StatusRunning || job.Status == StatusFailed {
				job.Status = StatusPending
				job.Attempts = 0
				job.Retryable = true
				job.Error = ""
			}

			if job.Status == StatusSucceeded || job.Status == StatusSkipped {
				if _, err := os.Stat(filepath.Join(job.RunDir, "run_report.json")); err == nil {
					job.Status = StatusSkipped
					job.Retryable = false
				} else {
					job.Status = StatusPending
					job.Attempts = 0
					job.Retryable = true
				}
			}
			jobsByPDF[job.InputPDF] = job
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	jobs := make([]Job, 0, len(pdfs))
	for _, pdfPath := range pdfs {
		runDir := runDirForPDF(pdfPath, opts.InputPath, opts.OutputRoot)
		if job, ok := jobsByPDF[pdfPath]; ok {
			job.RunDir = runDir
			jobs = append(jobs, job)
			continue
		}

		job := Job{
			InputPDF:  pdfPath,
			RunDir:    runDir,
			Status:    StatusPending,
			Attempts:  0,
			Retryable: true,
		}
		if opts.Resume {
			if _, err := os.Stat(filepath.Join(runDir, "run_report.json")); err == nil {
				job.Status = StatusSkipped
				job.Retryable = false
			}
		}
		jobs = append(jobs, job)
	}

	state := &State{
		Version:      1,
		InputPath:    opts.InputPath,
		OutputRoot:   opts.OutputRoot,
		CreatedAt:    now,
		UpdatedAt:    now,
		Workers:      opts.Workers,
		RetryFailed:  opts.RetryFailed,
		ProviderName: providerName,
		Jobs:         jobs,
	}
	if hasExisting {
		state.CreatedAt = existing.CreatedAt
	}

	if err := saveState(statePath, state); err != nil {
		return nil, err
	}

	return state, nil
}

func runDirForPDF(pdfPath, inputPath, outputRoot string) string {
	inputInfo, err := os.Stat(inputPath)
	if err == nil && !inputInfo.IsDir() {
		return filepath.Join(outputRoot, strings.TrimSuffix(filepath.Base(pdfPath), filepath.Ext(pdfPath)))
	}

	rel, relErr := filepath.Rel(inputPath, pdfPath)
	if relErr != nil {
		return filepath.Join(outputRoot, strings.TrimSuffix(filepath.Base(pdfPath), filepath.Ext(pdfPath)))
	}
	withoutExt := strings.TrimSuffix(rel, filepath.Ext(rel))
	return filepath.Join(outputRoot, withoutExt)
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "must be .pdf") {
		return false
	}
	if strings.Contains(msg, "not accessible") {
		return false
	}
	return true
}

func saveState(path string, state *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeJSON(path, state)
}

func buildReport(reportPath, statePath string, state *State) Report {
	report := Report{
		Total:      len(state.Jobs),
		StatePath:  statePath,
		ReportPath: reportPath,
	}
	for _, job := range state.Jobs {
		switch job.Status {
		case StatusSucceeded:
			report.Succeeded++
		case StatusSkipped:
			report.Skipped++
		case StatusFailed:
			report.Failed++
		}
	}
	return report
}

func effectiveWorkersForJobs(workers int, jobs []Job) int {
	pending := 0
	for _, job := range jobs {
		if job.Status == StatusPending {
			pending++
		}
	}
	if pending == 0 {
		return 0
	}
	if workers < 1 {
		return 1
	}
	if workers > pending {
		return pending
	}
	return workers
}

func writeJSON(path string, payload any) error {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o644)
}
