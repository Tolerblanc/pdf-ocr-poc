package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

type MockProvider struct{}

func (p *MockProvider) Name() string {
	return "mock"
}

func (p *MockProvider) Run(_ context.Context, req Request) (Result, error) {
	if err := os.MkdirAll(req.OutputDir, 0o755); err != nil {
		return Result{}, err
	}

	searchablePDF := filepath.Join(req.OutputDir, "searchable.pdf")
	if err := os.WriteFile(searchablePDF, []byte("%PDF-1.4\n% mock searchable pdf\n"), 0o644); err != nil {
		return Result{}, err
	}

	pagesJSON := filepath.Join(req.OutputDir, "pages.json")
	pagesPayload := map[string]any{
		"engine":     "mock",
		"source_pdf": req.InputPDF,
		"pages": []any{
			map[string]any{
				"page":   1,
				"text":   "mock text",
				"blocks": []any{},
			},
		},
	}
	pagesBody, err := json.MarshalIndent(pagesPayload, "", "  ")
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(pagesJSON, append(pagesBody, '\n'), 0o644); err != nil {
		return Result{}, err
	}

	textPath := filepath.Join(req.OutputDir, "document.txt")
	if err := os.WriteFile(textPath, []byte(""), 0o644); err != nil {
		return Result{}, err
	}

	markdownPath := filepath.Join(req.OutputDir, "document.md")
	if err := os.WriteFile(markdownPath, []byte("# Mock OCR\n"), 0o644); err != nil {
		return Result{}, err
	}

	return Result{
		SearchablePDF: searchablePDF,
		PagesJSON:     pagesJSON,
		TextPath:      textPath,
		MarkdownPath:  markdownPath,
		StageTimings: map[string]float64{
			"provider_seconds": 0.005,
		},
	}, nil
}
