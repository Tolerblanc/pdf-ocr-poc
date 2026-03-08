package main

import (
	"strings"
	"testing"
)

func TestParseShardSpecs(t *testing.T) {
	specs, err := parseShardSpecs("1, 4", 1, "manual")
	if err != nil {
		t.Fatalf("parseShardSpecs failed: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}
	if specs[0].Label != "s1-w1" || specs[0].Shards != 1 || specs[0].MaxWorkers != 1 || specs[0].Mode != "manual" {
		t.Fatalf("unexpected first spec: %+v", specs[0])
	}
	if specs[1].Label != "s4-w1" || specs[1].Shards != 4 || specs[1].MaxWorkers != 1 || specs[1].Mode != "manual" {
		t.Fatalf("unexpected second spec: %+v", specs[1])
	}
}

func TestParseShardSpecsAuto(t *testing.T) {
	specs, err := parseShardSpecs("2", 2, "auto")
	if err != nil {
		t.Fatalf("parseShardSpecs failed: %v", err)
	}
	if len(specs) != 1 || specs[0].Label != "s2-auto" {
		t.Fatalf("unexpected auto spec: %+v", specs)
	}
}

func TestFormatMarkdownSummary(t *testing.T) {
	summary := benchmarkSummary{
		GeneratedAt:        "2026-03-08T00:00:00Z",
		InputPDF:           "fixture_full.pdf",
		Profile:            "fast",
		LocalOnly:          true,
		MaxWorkersPerShard: 1,
		MaxWorkersMode:     "manual",
		Runs: []runSummary{
			{
				Label:                 "s1-w1",
				Shards:                1,
				MaxWorkersPerShard:    1,
				MaxWorkersMode:        "manual",
				ElapsedSeconds:        120,
				PagesPerMinute:        10,
				VisionOCRSecondsTotal: 100,
				SlowestShardSeconds:   120,
				SpeedupVsBaseline:     1,
				ThroughputVsBaseline:  1,
			},
			{
				Label:                 "s4-w1",
				Shards:                4,
				MaxWorkersPerShard:    1,
				MaxWorkersMode:        "manual",
				ElapsedSeconds:        60,
				PagesPerMinute:        20,
				VisionOCRSecondsTotal: 160,
				SlowestShardSeconds:   18,
				SpeedupVsBaseline:     2,
				ThroughputVsBaseline:  2,
			},
		},
	}

	markdown := formatMarkdownSummary(summary)
	if !strings.Contains(markdown, "# Process Shard Benchmark") {
		t.Fatalf("expected markdown title, got: %q", markdown)
	}
	if !strings.Contains(markdown, "| `s4-w1` | 4 | 1 | manual | 60.000 | 20.000 | 160.000 | 18.000 | 2.00x | 2.00x |") {
		t.Fatalf("expected run row, got: %q", markdown)
	}
	if !strings.Contains(markdown, "Fastest run: `s4-w1`") {
		t.Fatalf("expected fastest run line, got: %q", markdown)
	}
}
