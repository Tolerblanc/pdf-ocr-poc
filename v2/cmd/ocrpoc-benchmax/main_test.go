package main

import (
	"strings"
	"testing"
)

func TestParseWorkerSpecs(t *testing.T) {
	specs, err := parseWorkerSpecs("1, 4, auto", "vision-swift", "")
	if err != nil {
		t.Fatalf("parseWorkerSpecs failed: %v", err)
	}
	if len(specs) != 3 {
		t.Fatalf("expected 3 specs, got %d", len(specs))
	}
	if specs[0].Label != "w1" || specs[0].MaxWorkers != 1 || specs[0].Mode != "manual" {
		t.Fatalf("unexpected first spec: %+v", specs[0])
	}
	if specs[1].Label != "w4" || specs[1].MaxWorkers != 4 || specs[1].Mode != "manual" {
		t.Fatalf("unexpected second spec: %+v", specs[1])
	}
	if specs[2].Label != "auto" || specs[2].Mode != "auto" || specs[2].MaxWorkers != 2 {
		t.Fatalf("unexpected auto spec: %+v", specs[2])
	}
}

func TestParseWorkerSpecsGenericAutoProvider(t *testing.T) {
	specs, err := parseWorkerSpecs("auto", "mock", "")
	if err != nil {
		t.Fatalf("parseWorkerSpecs failed: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Label != "auto" || specs[0].Mode != "auto" || specs[0].MaxWorkers < 1 {
		t.Fatalf("unexpected auto spec: %+v", specs[0])
	}
}

func TestFormatMarkdownSummary(t *testing.T) {
	summary := benchmarkSummary{
		GeneratedAt: "2026-03-08T00:00:00Z",
		InputPDF:    "fixture_full.pdf",
		Profile:     "fast",
		LocalOnly:   true,
		Runs: []runSummary{
			{
				Label:                "w1",
				EffectiveMaxWorkers:  1,
				MaxWorkersMode:       "manual",
				ElapsedSeconds:       120,
				PagesPerMinute:       10,
				VisionOCRSeconds:     90,
				SearchablePDFSeconds: 20,
				SpeedupVsBaseline:    1,
				ThroughputVsBaseline: 1,
			},
			{
				Label:                "w4",
				EffectiveMaxWorkers:  4,
				MaxWorkersMode:       "manual",
				ElapsedSeconds:       60,
				PagesPerMinute:       20,
				VisionOCRSeconds:     40,
				SearchablePDFSeconds: 15,
				SpeedupVsBaseline:    2,
				ThroughputVsBaseline: 2,
			},
		},
	}

	markdown := formatMarkdownSummary(summary)
	if !strings.Contains(markdown, "# Max Workers Benchmark") {
		t.Fatalf("expected markdown title, got: %q", markdown)
	}
	if !strings.Contains(markdown, "| `w4` | 4 | manual | 60.000 | 20.000 | 40.000 | 15.000 | 2.00x | 2.00x |") {
		t.Fatalf("expected run row, got: %q", markdown)
	}
	if !strings.Contains(markdown, "Fastest run: `w4`") {
		t.Fatalf("expected fastest run line, got: %q", markdown)
	}
}
