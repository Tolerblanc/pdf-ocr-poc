package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

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
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(err.Error())
		}
		return Result{}, fmt.Errorf("provider execution failed: %s", msg)
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
