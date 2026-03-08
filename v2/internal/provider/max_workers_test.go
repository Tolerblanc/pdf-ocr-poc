package provider

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveAutoMaxWorkersVisionSwift(t *testing.T) {
	if got := ResolveAutoMaxWorkers("vision-swift", ""); got != 2 {
		t.Fatalf("expected vision-swift auto max workers=2, got %d", got)
	}
}

func TestResolveAutoMaxWorkersVisionProviderBinary(t *testing.T) {
	providerBin := filepath.Join(t.TempDir(), "vision-provider")
	if got := ResolveAutoMaxWorkers("exec", providerBin); got != 2 {
		t.Fatalf("expected vision-provider exec auto max workers=2, got %d", got)
	}
}

func TestResolveAutoMaxWorkersGenericProvider(t *testing.T) {
	want := runtime.NumCPU() - 1
	if want < 1 {
		want = 1
	}
	if want > 8 {
		want = 8
	}
	if got := ResolveAutoMaxWorkers("mock", ""); got != want {
		t.Fatalf("expected generic auto max workers=%d, got %d", want, got)
	}
}
