package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Request struct {
	InputPDF           string          `json:"input_pdf"`
	OutputDir          string          `json:"output_dir"`
	Profile            string          `json:"profile"`
	LocalOnly          bool            `json:"local_only"`
	MaxWorkers         int             `json:"max_workers"`
	WorkersMode        string          `json:"workers_mode"`
	CorrectedPagesJSON string          `json:"corrected_pages_json,omitempty"`
	ShardIndex         int             `json:"shard_index,omitempty"`
	ShardTotal         int             `json:"shard_total,omitempty"`
	RequestSource      string          `json:"request_source,omitempty"`
	OnProgress         ProgressHandler `json:"-"`
}

type ProgressHandler func(ProgressEvent)

type ProgressEvent struct {
	Phase          string `json:"phase"`
	Stage          string `json:"stage,omitempty"`
	CurrentPage    int    `json:"current_page,omitempty"`
	CompletedPages int    `json:"completed_pages,omitempty"`
	TotalPages     int    `json:"total_pages,omitempty"`
}

type Result struct {
	SearchablePDF  string             `json:"searchable_pdf"`
	PagesJSON      string             `json:"pages_json"`
	TextPath       string             `json:"text_path"`
	MarkdownPath   string             `json:"markdown_path"`
	ArtifactSource string             `json:"artifact_source,omitempty"`
	Capabilities   *Capabilities      `json:"capabilities,omitempty"`
	StageTimings   map[string]float64 `json:"stage_timings,omitempty"`
	Warnings       []string           `json:"warnings,omitempty"`

	MonitorSamples             int      `json:"-"`
	MonitorDurationSeconds     float64  `json:"-"`
	RemoteConnectionViolations []string `json:"-"`
	LocalOnlySelfcheckSet      bool     `json:"-"`
	LocalOnlySelfcheckOK       bool     `json:"-"`
	LocalOnlySelfcheckMessage  string   `json:"-"`
}

const (
	ArtifactSourceOCR            = "ocr"
	ArtifactSourceCorrectedPages = "corrected_pages"
)

type Capabilities struct {
	CorrectedArtifactRebuild bool `json:"corrected_artifact_rebuild,omitempty"`
}

type Provider interface {
	Name() string
	Run(ctx context.Context, req Request) (Result, error)
}

func New(providerName string, providerBin string) (Provider, error) {
	switch providerName {
	case "mock":
		return &MockProvider{}, nil
	case "vision-swift":
		resolved, err := resolveVisionProviderBinary(providerBin)
		if err != nil {
			return nil, err
		}
		return &ExecProvider{providerBin: resolved, displayName: "vision-swift"}, nil
	case "exec":
		if providerBin == "" {
			return nil, errors.New("provider binary is required for exec provider")
		}
		return &ExecProvider{providerBin: providerBin, displayName: "exec"}, nil
	default:
		if providerBin == "" {
			return nil, fmt.Errorf("unknown provider: %s", providerName)
		}
		return &ExecProvider{providerBin: providerBin, displayName: providerName}, nil
	}
}

func resolveVisionProviderBinary(providerBin string) (string, error) {
	if strings.TrimSpace(providerBin) != "" {
		if isExecutable(providerBin) {
			return providerBin, nil
		}
		return "", fmt.Errorf("vision-swift provider binary not executable: %s", providerBin)
	}

	envBin := strings.TrimSpace(os.Getenv("OCRPOC_VISION_PROVIDER_BIN"))
	if envBin != "" {
		if isExecutable(envBin) {
			return envBin, nil
		}
		return "", fmt.Errorf("OCRPOC_VISION_PROVIDER_BIN is not executable: %s", envBin)
	}

	candidates := []string{
		"providers/vision-swift/bin/vision-provider",
		"v2/providers/vision-swift/bin/vision-provider",
	}

	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates,
			filepath.Join(exeDir, "providers/vision-swift/bin/vision-provider"),
			filepath.Join(exeDir, "../providers/vision-swift/bin/vision-provider"),
		)
	}

	for _, candidate := range candidates {
		if isExecutable(candidate) {
			return candidate, nil
		}
	}

	return "", errors.New(
		"vision-swift provider binary not found; build with: (cd v2/providers/vision-swift && ./build.sh)",
	)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}
