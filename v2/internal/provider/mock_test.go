package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMockProviderRebuildsPrimaryArtifactsFromCorrectedPages(t *testing.T) {
	temp := t.TempDir()
	correctedPages := filepath.Join(temp, "corrected_pages.json")
	body := `{
	  "engine": "mock",
	  "source_pdf": "/tmp/in.pdf",
	  "pages": [
	    {
	      "page": 1,
	      "width": 100,
	      "height": 200,
	      "is_blank": false,
	      "text": "Corrected page text",
	      "blocks": [
	        {
	          "text": "Corrected heading",
	          "bbox": {"x0": 1, "y0": 2, "x1": 3, "y1": 4},
	          "block_type": "heading",
	          "confidence": 0.9,
	          "reading_order": 1
	        },
	        {
	          "text": "Corrected paragraph",
	          "bbox": {"x0": 5, "y0": 6, "x1": 7, "y1": 8},
	          "block_type": "paragraph",
	          "confidence": 0.8,
	          "reading_order": 2
	        }
	      ]
	    }
	  ]
	}`
	if err := os.WriteFile(correctedPages, []byte(body), 0o644); err != nil {
		t.Fatalf("write corrected pages failed: %v", err)
	}

	outDir := filepath.Join(temp, "out")
	result, err := (&MockProvider{}).Run(context.Background(), Request{
		InputPDF:           filepath.Join(temp, "in.pdf"),
		OutputDir:          outDir,
		CorrectedPagesJSON: correctedPages,
	})
	if err != nil {
		t.Fatalf("mock rebuild failed: %v", err)
	}

	pagesBody, err := os.ReadFile(result.PagesJSON)
	if err != nil {
		t.Fatalf("read pages.json failed: %v", err)
	}
	pages := map[string]any{}
	if err := json.Unmarshal(pagesBody, &pages); err != nil {
		t.Fatalf("parse pages.json failed: %v", err)
	}
	rawPages, ok := pages["pages"].([]any)
	if !ok || len(rawPages) != 1 {
		t.Fatalf("unexpected pages payload: %+v", pages)
	}
	page, ok := rawPages[0].(map[string]any)
	if !ok || page["text"] != "Corrected page text" {
		t.Fatalf("expected corrected page text, got %+v", rawPages[0])
	}

	textBody, err := os.ReadFile(result.TextPath)
	if err != nil {
		t.Fatalf("read document.txt failed: %v", err)
	}
	if strings.TrimSpace(string(textBody)) != "Corrected page text" {
		t.Fatalf("unexpected document.txt: %q", string(textBody))
	}

	markdownBody, err := os.ReadFile(result.MarkdownPath)
	if err != nil {
		t.Fatalf("read document.md failed: %v", err)
	}
	markdown := string(markdownBody)
	if !strings.Contains(markdown, "### Corrected heading") || !strings.Contains(markdown, "Corrected paragraph") {
		t.Fatalf("unexpected markdown: %s", markdown)
	}

	if _, err := os.Stat(result.SearchablePDF); err != nil {
		t.Fatalf("expected searchable.pdf to exist: %v", err)
	}
}
