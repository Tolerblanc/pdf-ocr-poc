package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

type ExecProvider struct {
	providerBin string
	displayName string
}

type monitorOutcome struct {
	samples         int
	durationSeconds float64
	violations      []string
	err             error
}

type stderrOutcome struct {
	text string
	err  error
}

const progressLinePrefix = "OCRPOC_PROGRESS "

func (p *ExecProvider) Name() string {
	if p.displayName != "" {
		return p.displayName
	}
	return "exec"
}

func (p *ExecProvider) Run(ctx context.Context, req Request) (Result, error) {
	input, err := json.Marshal(req)
	if err != nil {
		return Result{}, err
	}

	cmd := exec.CommandContext(ctx, p.providerBin)
	cmd.Stdin = bytes.NewReader(input)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, err
	}

	result := Result{}

	if req.LocalOnly {
		ok, message := checkLocalOnlyToolsFn()
		result.LocalOnlySelfcheckSet = true
		result.LocalOnlySelfcheckOK = ok
		result.LocalOnlySelfcheckMessage = message
		if !ok {
			return Result{}, fmt.Errorf("local-only selfcheck failed: %s", message)
		}
	}

	if err := cmd.Start(); err != nil {
		return Result{}, err
	}

	stderrCh := make(chan stderrOutcome, 1)
	go captureStderr(stderrPipe, req.OnProgress, stderrCh)

	monitorCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	monitorCh := make(chan monitorOutcome, 1)
	if req.LocalOnly {
		go func(pid int) {
			samples, duration, violations, monitorErr := monitorProcessTreeNetworkFn(
				monitorCtx,
				pid,
				200*time.Millisecond,
			)
			monitorCh <- monitorOutcome{
				samples:         samples,
				durationSeconds: duration,
				violations:      violations,
				err:             monitorErr,
			}
		}(cmd.Process.Pid)
	}

	if err := cmd.Wait(); err != nil {
		cancel()
		if req.LocalOnly {
			<-monitorCh
		}
		stderr := <-stderrCh
		if stderr.err != nil {
			return Result{}, fmt.Errorf("provider stderr read failed: %w", stderr.err)
		}
		msg := strings.TrimSpace(stderr.text)
		if msg == "" {
			msg = strings.TrimSpace(err.Error())
		}
		return Result{}, fmt.Errorf("provider execution failed: %s", msg)
	}

	stderr := <-stderrCh
	if stderr.err != nil {
		return Result{}, fmt.Errorf("provider stderr read failed: %w", stderr.err)
	}

	if req.LocalOnly {
		cancel()
		outcome := <-monitorCh
		if outcome.err != nil {
			return Result{}, fmt.Errorf("local-only monitor failed: %w", outcome.err)
		}
		result.MonitorSamples = outcome.samples
		result.MonitorDurationSeconds = outcome.durationSeconds
		result.RemoteConnectionViolations = outcome.violations
	}

	var parsed Result
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return Result{}, fmt.Errorf("provider output parse failed: %w", err)
	}

	parsed.MonitorSamples = result.MonitorSamples
	parsed.MonitorDurationSeconds = result.MonitorDurationSeconds
	parsed.RemoteConnectionViolations = result.RemoteConnectionViolations
	parsed.LocalOnlySelfcheckSet = result.LocalOnlySelfcheckSet
	parsed.LocalOnlySelfcheckOK = result.LocalOnlySelfcheckOK
	parsed.LocalOnlySelfcheckMessage = result.LocalOnlySelfcheckMessage
	if req.LocalOnly && len(parsed.RemoteConnectionViolations) > 0 {
		parsed.Warnings = append(
			parsed.Warnings,
			"local_only_violation_detected",
		)
	}

	return parsed, nil
}

func captureStderr(r io.Reader, onProgress ProgressHandler, out chan<- stderrOutcome) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lines := make([]string, 0, 8)
	for scanner.Scan() {
		line := scanner.Text()
		if emitProgressLine(line, onProgress) {
			continue
		}
		lines = append(lines, line)
	}
	out <- stderrOutcome{
		text: strings.Join(lines, "\n"),
		err:  scanner.Err(),
	}
}

func emitProgressLine(line string, onProgress ProgressHandler) bool {
	if !strings.HasPrefix(line, progressLinePrefix) {
		return false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, progressLinePrefix))
	if payload == "" {
		return false
	}
	var event ProgressEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return false
	}
	if onProgress != nil {
		onProgress(event)
	}
	return true
}
