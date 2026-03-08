package cli

import "testing"

func TestResolveWorkersVisionSwiftAuto(t *testing.T) {
	workers, mode := resolveWorkers(0, "vision-swift", "")
	if mode != "auto" {
		t.Fatalf("expected auto mode, got %s", mode)
	}
	if workers != 2 {
		t.Fatalf("expected auto workers=2 for vision-swift, got %d", workers)
	}
}

func TestResolveWorkersManualOverride(t *testing.T) {
	workers, mode := resolveWorkers(7, "vision-swift", "")
	if mode != "manual" {
		t.Fatalf("expected manual mode, got %s", mode)
	}
	if workers != 7 {
		t.Fatalf("expected manual workers=7, got %d", workers)
	}
}
