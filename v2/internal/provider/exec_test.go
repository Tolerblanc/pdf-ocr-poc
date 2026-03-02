package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeExecScript(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script failed: %v", err)
	}
}

func jsonSuccessScript() string {
	return "#!/usr/bin/env bash\nset -euo pipefail\ncat >/dev/null\ncat <<'JSON'\n{\"searchable_pdf\":\"/tmp/a.pdf\",\"pages_json\":\"/tmp/pages.json\",\"text_path\":\"/tmp/document.txt\",\"markdown_path\":\"/tmp/document.md\"}\nJSON\n"
}

func TestExecProviderRunSuccess(t *testing.T) {
	temp := t.TempDir()
	script := filepath.Join(temp, "provider.sh")
	writeExecScript(t, script, jsonSuccessScript())

	p := &ExecProvider{providerBin: script, displayName: "exec"}
	result, err := p.Run(context.Background(), Request{InputPDF: "in.pdf", OutputDir: temp})
	if err != nil {
		t.Fatalf("exec run failed: %v", err)
	}
	if result.SearchablePDF != "/tmp/a.pdf" {
		t.Fatalf("unexpected searchable_pdf: %s", result.SearchablePDF)
	}
}

func TestExecProviderRunFailure(t *testing.T) {
	temp := t.TempDir()
	script := filepath.Join(temp, "provider-fail.sh")
	body := "#!/usr/bin/env bash\nset -euo pipefail\ncat >/dev/null\necho 'boom' >&2\nexit 1\n"
	writeExecScript(t, script, body)

	p := &ExecProvider{providerBin: script, displayName: "exec"}
	_, err := p.Run(context.Background(), Request{InputPDF: "in.pdf", OutputDir: temp})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestExecProviderLocalOnlyToolingMissing(t *testing.T) {
	original := checkLocalOnlyToolsFn
	checkLocalOnlyToolsFn = func() (bool, string) {
		return false, "missing tools"
	}
	t.Cleanup(func() {
		checkLocalOnlyToolsFn = original
	})

	temp := t.TempDir()
	script := filepath.Join(temp, "provider.sh")
	writeExecScript(t, script, jsonSuccessScript())

	p := &ExecProvider{providerBin: script, displayName: "exec"}
	_, err := p.Run(context.Background(), Request{InputPDF: "in.pdf", OutputDir: temp, LocalOnly: true})
	if err == nil {
		t.Fatalf("expected local-only tooling error")
	}
	if !strings.Contains(err.Error(), "missing tools") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecProviderLocalOnlyViolationCaptured(t *testing.T) {
	originalCheck := checkLocalOnlyToolsFn
	originalMonitor := monitorProcessTreeNetworkFn
	checkLocalOnlyToolsFn = func() (bool, string) {
		return true, "monitor active"
	}
	monitorProcessTreeNetworkFn = func(
		ctx context.Context,
		rootPID int,
		sampleInterval time.Duration,
	) (int, float64, []string, error) {
		return 3, 1.5, []string{"pid=123 remote=1.1.1.1:443"}, nil
	}
	t.Cleanup(func() {
		checkLocalOnlyToolsFn = originalCheck
		monitorProcessTreeNetworkFn = originalMonitor
	})

	temp := t.TempDir()
	script := filepath.Join(temp, "provider.sh")
	writeExecScript(t, script, jsonSuccessScript())

	p := &ExecProvider{providerBin: script, displayName: "exec"}
	result, err := p.Run(context.Background(), Request{InputPDF: "in.pdf", OutputDir: temp, LocalOnly: true})
	if err != nil {
		t.Fatalf("expected success with captured violations, got err=%v", err)
	}
	if len(result.RemoteConnectionViolations) != 1 {
		t.Fatalf("expected one violation, got %+v", result.RemoteConnectionViolations)
	}
	if !result.LocalOnlySelfcheckSet || !result.LocalOnlySelfcheckOK {
		t.Fatalf("expected local-only selfcheck metadata, got %+v", result)
	}
}
