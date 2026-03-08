package postprocess

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
)

func TestExecuteWritesCanonicalCorrectedArtifacts(t *testing.T) {
	temp := t.TempDir()
	pagesPath := filepath.Join(temp, "pages.json")
	body := []byte(`{
	  "engine": "mock",
	  "source_pdf": "/tmp/in.pdf",
	  "pages": [
	    {
	      "page": 1,
	      "width": 100,
	      "height": 200,
	      "is_blank": false,
	      "text": "hello world",
	      "blocks": [
	        {
	          "text": "hello world",
	          "bbox": {"x0": 1, "y0": 2, "x1": 3, "y1": 4},
	          "block_type": "paragraph",
	          "confidence": 0.9,
	          "reading_order": 1
	        }
	      ]
	    }
	  ]
	}`)
	if err := os.WriteFile(pagesPath, body, 0o644); err != nil {
		t.Fatalf("write pages failed: %v", err)
	}

	output, err := Execute(context.Background(), &NoneProvider{}, Request{
		InputPDF:    "/tmp/in.pdf",
		OutputDir:   temp,
		OCRProvider: "mock",
		OCRResult: provider.Result{
			PagesJSON:     pagesPath,
			TextPath:      filepath.Join(temp, "document.txt"),
			MarkdownPath:  filepath.Join(temp, "document.md"),
			SearchablePDF: filepath.Join(temp, "searchable.pdf"),
		},
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if output.CorrectedPagesJSON == "" {
		t.Fatalf("expected corrected_pages.json path")
	}

	body, err = os.ReadFile(output.CorrectedPagesJSON)
	if err != nil {
		t.Fatalf("read corrected pages failed: %v", err)
	}
	doc := Document{}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parse corrected pages failed: %v", err)
	}
	if doc.Version != SchemaVersion || doc.Kind != DocumentKind {
		t.Fatalf("unexpected document identity: %+v", doc)
	}
	if doc.Postprocess.Provider != ProviderNone {
		t.Fatalf("unexpected postprocess provider: %+v", doc.Postprocess)
	}
	if len(doc.Pages) != 1 {
		t.Fatalf("expected one page, got %d", len(doc.Pages))
	}
	if doc.Pages[0].Text != "hello world" || doc.Pages[0].SourceText != "hello world" {
		t.Fatalf("unexpected page text: %+v", doc.Pages[0])
	}
	if doc.Pages[0].Correction.Status != CorrectionStatusUnchanged {
		t.Fatalf("unexpected page correction: %+v", doc.Pages[0].Correction)
	}
	if doc.Pages[0].Blocks[0].Correction.Status != CorrectionStatusUnchanged {
		t.Fatalf("unexpected block correction: %+v", doc.Pages[0].Blocks[0].Correction)
	}
}

func TestExecuteSkipsMissingPagesJSONForNoneProvider(t *testing.T) {
	output, err := Execute(context.Background(), &NoneProvider{}, Request{
		OutputDir: "/tmp",
		OCRResult: provider.Result{PagesJSON: filepath.Join(t.TempDir(), "missing.json")},
	})
	if err != nil {
		t.Fatalf("expected skip, got err=%v", err)
	}
	if len(output.Warnings) != 1 || output.Warnings[0] != WarningSkippedMissingPagesJSON {
		t.Fatalf("unexpected warnings: %+v", output.Warnings)
	}
}

func TestExecutePreservesPrimaryOutputModeForNoneProvider(t *testing.T) {
	temp := t.TempDir()
	pagesPath := filepath.Join(temp, "pages.json")
	body := []byte(`{"pages":[{"page":1,"text":"hello","blocks":[]}]}`)
	if err := os.WriteFile(pagesPath, body, 0o644); err != nil {
		t.Fatalf("write pages failed: %v", err)
	}

	output, err := Execute(context.Background(), &NoneProvider{}, Request{
		InputPDF:  "/tmp/in.pdf",
		OutputDir: temp,
		OCRResult: provider.Result{PagesJSON: pagesPath},
		Config: Config{
			OutputMode: OutputModePrimaryArtifacts,
		},
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if output.OutputMode != OutputModePrimaryArtifacts {
		t.Fatalf("expected primary_artifacts output mode, got %s", output.OutputMode)
	}
}
