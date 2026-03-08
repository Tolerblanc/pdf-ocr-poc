package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type MockProvider struct{}

type mockCorrectedDocument struct {
	Engine    string              `json:"engine"`
	SourcePDF string              `json:"source_pdf"`
	Pages     []mockCorrectedPage `json:"pages"`
}

type mockCorrectedPage struct {
	Page    int                  `json:"page"`
	Width   int                  `json:"width"`
	Height  int                  `json:"height"`
	IsBlank bool                 `json:"is_blank"`
	Text    string               `json:"text"`
	Blocks  []mockCorrectedBlock `json:"blocks"`
}

type mockCorrectedBlock struct {
	Text         string   `json:"text"`
	BBox         mockBBox `json:"bbox"`
	BlockType    string   `json:"block_type"`
	Confidence   float64  `json:"confidence"`
	ReadingOrder int      `json:"reading_order"`
}

type mockBBox struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}

func (p *MockProvider) Name() string {
	return "mock"
}

func (p *MockProvider) Run(_ context.Context, req Request) (Result, error) {
	if err := os.MkdirAll(req.OutputDir, 0o755); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(req.CorrectedPagesJSON) != "" {
		return writeCorrectedArtifacts(req.OutputDir, req.InputPDF, req.CorrectedPagesJSON)
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
		SearchablePDF:  searchablePDF,
		PagesJSON:      pagesJSON,
		TextPath:       textPath,
		MarkdownPath:   markdownPath,
		ArtifactSource: ArtifactSourceOCR,
		Capabilities:   &Capabilities{CorrectedArtifactRebuild: true},
		StageTimings: map[string]float64{
			"provider_seconds": 0.005,
		},
	}, nil
}

func writeCorrectedArtifacts(outDir string, inputPDF string, correctedPagesPath string) (Result, error) {
	body, err := os.ReadFile(correctedPagesPath)
	if err != nil {
		return Result{}, err
	}

	doc := mockCorrectedDocument{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return Result{}, err
	}

	searchablePDF := filepath.Join(outDir, "searchable.pdf")
	if err := os.WriteFile(searchablePDF, []byte("%PDF-1.4\n% mock searchable pdf\n"), 0o644); err != nil {
		return Result{}, err
	}

	pagesJSON := filepath.Join(outDir, "pages.json")
	pages := make([]map[string]any, 0, len(doc.Pages))
	pagesPayload := map[string]any{
		"engine":     firstNonEmpty(doc.Engine, "mock"),
		"source_pdf": firstNonEmpty(doc.SourcePDF, inputPDF),
		"pages":      pages,
	}
	textChunks := make([]string, 0, len(doc.Pages))
	markdownLines := make([]string, 0, len(doc.Pages)*4)
	for _, page := range doc.Pages {
		sort.SliceStable(page.Blocks, func(i, j int) bool {
			return page.Blocks[i].ReadingOrder < page.Blocks[j].ReadingOrder
		})

		pageText := strings.TrimSpace(page.Text)
		if pageText == "" {
			pageText = joinMockBlockTexts(page.Blocks)
		}
		textChunks = append(textChunks, pageText)

		blocks := make([]map[string]any, 0, len(page.Blocks))
		markdownLines = append(markdownLines, "## Page "+itoa(page.Page))
		for index, block := range page.Blocks {
			readingOrder := block.ReadingOrder
			if readingOrder < 1 {
				readingOrder = index + 1
			}
			blockType := strings.TrimSpace(block.BlockType)
			if blockType == "" {
				blockType = "paragraph"
			}
			blocks = append(blocks, map[string]any{
				"text":          block.Text,
				"bbox":          block.BBox,
				"block_type":    blockType,
				"confidence":    block.Confidence,
				"reading_order": readingOrder,
			})

			text := strings.TrimSpace(block.Text)
			if text == "" {
				continue
			}
			switch blockType {
			case "heading":
				markdownLines = append(markdownLines, "### "+text)
			case "code":
				markdownLines = append(markdownLines, "```text", text, "```")
			case "caption":
				markdownLines = append(markdownLines, "*"+text+"*")
			default:
				markdownLines = append(markdownLines, text)
			}
		}
		markdownLines = append(markdownLines, "")

		pages = append(pages, map[string]any{
			"page":     page.Page,
			"width":    page.Width,
			"height":   page.Height,
			"is_blank": page.IsBlank,
			"text":     pageText,
			"blocks":   blocks,
		})
	}
	pagesPayload["pages"] = pages

	pagesBody, err := json.MarshalIndent(pagesPayload, "", "  ")
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(pagesJSON, append(pagesBody, '\n'), 0o644); err != nil {
		return Result{}, err
	}

	textPath := filepath.Join(outDir, "document.txt")
	if err := os.WriteFile(textPath, []byte(strings.Join(textChunks, "\n\n")+"\n"), 0o644); err != nil {
		return Result{}, err
	}

	markdownPath := filepath.Join(outDir, "document.md")
	if err := os.WriteFile(markdownPath, []byte(strings.TrimSpace(strings.Join(markdownLines, "\n"))+"\n"), 0o644); err != nil {
		return Result{}, err
	}

	return Result{
		SearchablePDF:  searchablePDF,
		PagesJSON:      pagesJSON,
		TextPath:       textPath,
		MarkdownPath:   markdownPath,
		ArtifactSource: ArtifactSourceCorrectedPages,
		Capabilities:   &Capabilities{CorrectedArtifactRebuild: true},
		StageTimings: map[string]float64{
			"serialization_seconds":  0,
			"searchable_pdf_seconds": 0,
		},
	}, nil
}

func joinMockBlockTexts(blocks []mockCorrectedBlock) string {
	lines := make([]string, 0, len(blocks))
	for _, block := range blocks {
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
