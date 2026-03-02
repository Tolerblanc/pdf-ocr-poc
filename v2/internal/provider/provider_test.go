package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewExecRequiresBinary(t *testing.T) {
	_, err := New("exec", "")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestNewVisionSwiftUsesEnvOverride(t *testing.T) {
	temp := t.TempDir()
	binPath := filepath.Join(temp, "vision-provider")
	if err := os.WriteFile(binPath, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("write binary failed: %v", err)
	}
	t.Setenv("OCRPOC_VISION_PROVIDER_BIN", binPath)

	p, err := New("vision-swift", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	execProvider, ok := p.(*ExecProvider)
	if !ok {
		t.Fatalf("expected exec provider, got %T", p)
	}
	if execProvider.providerBin != binPath {
		t.Fatalf("unexpected provider bin: %s", execProvider.providerBin)
	}
}

func TestNewVisionSwiftMissingBinaryReturnsHelpfulError(t *testing.T) {
	t.Setenv("OCRPOC_VISION_PROVIDER_BIN", "/definitely/not/executable")
	_, err := New("vision-swift", "")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "OCRPOC_VISION_PROVIDER_BIN") {
		t.Fatalf("unexpected error: %v", err)
	}
}
