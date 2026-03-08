package postprocess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
)

const (
	SchemaVersion                  = "v1alpha1"
	DocumentKind                   = "corrected_pages"
	ProviderNone                   = "none"
	ProviderLocalLM                = "local-lm"
	ProviderCloudLLM               = "cloud-llm"
	ProviderFoundationModels       = "foundation-models"
	ProviderCodexHeadlessOAuth     = "codex-headless-oauth"
	OutputModeSidecarOnly          = "sidecar_only"
	OutputModePrimaryArtifacts     = "primary_artifacts"
	AuthKindNone                   = "none"
	AuthKindEnvAPIKey              = "env_api_key"
	AuthKindEnvOAuthAccessToken    = "env_oauth_access_token"
	AuthKindOAuthStoreFile         = "oauth_store_file"
	AuthKindLocalRuntime           = "local_runtime"
	AuthKindUnknown                = "unknown"
	CorrectionStatusUnchanged      = "unchanged"
	CorrectionStatusCorrected      = "corrected"
	CorrectionStatusFlagged        = "flagged"
	CorrectionStatusRejected       = "rejected"
	WarningSkippedMissingPagesJSON = "postprocess_skipped_missing_pages_json"
)

type Config struct {
	Provider            string      `json:"provider,omitempty"`
	AuthRef             string      `json:"auth_ref,omitempty"`
	Auth                *AuthConfig `json:"auth,omitempty"`
	Model               string      `json:"model,omitempty"`
	BaseURL             string      `json:"base_url,omitempty"`
	IssuerURL           string      `json:"issuer_url,omitempty"`
	TimeoutSeconds      int         `json:"timeout_seconds,omitempty"`
	Temperature         *float64    `json:"temperature,omitempty"`
	MaxCompletionTokens int         `json:"max_completion_tokens,omitempty"`
	PageBatchSize       int         `json:"page_batch_size,omitempty"`
	OutputMode          string      `json:"output_mode,omitempty"`
	SystemPrompt        string      `json:"system_prompt,omitempty"`
	Guard               GuardPolicy `json:"guard,omitempty"`
}

type AuthConfig struct {
	Kind            string `json:"kind,omitempty"`
	Env             string `json:"env,omitempty"`
	HeaderName      string `json:"header_name,omitempty"`
	AccessTokenEnv  string `json:"access_token_env,omitempty"`
	RefreshTokenEnv string `json:"refresh_token_env,omitempty"`
	AccountIDEnv    string `json:"account_id_env,omitempty"`
	File            string `json:"file,omitempty"`
	ProviderID      string `json:"provider_id,omitempty"`
	Runtime         string `json:"runtime,omitempty"`
	Endpoint        string `json:"endpoint,omitempty"`
	Binary          string `json:"binary,omitempty"`
}

type GuardPolicy struct {
	MaxEditDistanceRatio float64 `json:"max_edit_distance_ratio,omitempty"`
	ProtectNumbers       bool    `json:"protect_numbers,omitempty"`
	ProtectURLs          bool    `json:"protect_urls,omitempty"`
	ProtectCodeBlocks    bool    `json:"protect_code_blocks,omitempty"`
	EmitPageDiff         bool    `json:"emit_page_diff,omitempty"`
}

type ConfigFile struct {
	Version     string                `json:"version,omitempty"`
	Credentials map[string]AuthConfig `json:"credentials,omitempty"`
	Providers   map[string]Config     `json:"providers,omitempty"`
	Runtime     RuntimeSelection      `json:"runtime,omitempty"`
}

type RuntimeSelection struct {
	Profile     string          `json:"profile,omitempty"`
	Provider    string          `json:"provider,omitempty"`
	AllowRemote *bool           `json:"allow_remote,omitempty"`
	Override    RuntimeOverride `json:"override,omitempty"`
}

type RuntimeOverride struct {
	Provider            string   `json:"provider,omitempty"`
	Model               string   `json:"model,omitempty"`
	BaseURL             string   `json:"base_url,omitempty"`
	IssuerURL           string   `json:"issuer_url,omitempty"`
	TimeoutSeconds      int      `json:"timeout_seconds,omitempty"`
	Temperature         *float64 `json:"temperature,omitempty"`
	MaxCompletionTokens int      `json:"max_completion_tokens,omitempty"`
	OutputMode          string   `json:"output_mode,omitempty"`
}

type Request struct {
	InputPDF    string
	OutputDir   string
	OCRProvider string
	OCRResult   provider.Result
	Config      Config
	AllowRemote bool
	OnProgress  provider.ProgressHandler
}

type Output struct {
	CorrectedPagesJSON    string
	CorrectedTextPath     string
	CorrectedMarkdownPath string
	Document              Document
	Applied               bool
	ChangedPages          int
	Warnings              []string
	StageTimings          map[string]float64
	OutputMode            string
}

type Provider interface {
	Name() string
	Run(ctx context.Context, req ProviderRequest) (ProviderResult, error)
}

type ProviderRequest struct {
	Document    Document
	Config      Config
	AllowRemote bool
	OnProgress  provider.ProgressHandler
}

type ProviderResult struct {
	Document       Document
	Applied        bool
	Warnings       []string
	StageTimings   map[string]float64
	CredentialKind string
	Model          string
	OutputMode     string
	ProviderMeta   map[string]any
}

type SourceArtifacts struct {
	PagesJSON     string `json:"pages_json,omitempty"`
	TextPath      string `json:"text_path,omitempty"`
	MarkdownPath  string `json:"markdown_path,omitempty"`
	SearchablePDF string `json:"searchable_pdf,omitempty"`
}

type PostprocessMetadata struct {
	Provider       string             `json:"provider"`
	Applied        bool               `json:"applied"`
	OutputMode     string             `json:"output_mode"`
	CredentialKind string             `json:"credential_kind,omitempty"`
	Model          string             `json:"model,omitempty"`
	Warnings       []string           `json:"warnings,omitempty"`
	StageTimings   map[string]float64 `json:"stage_timings,omitempty"`
	ProviderMeta   map[string]any     `json:"provider_metadata,omitempty"`
}

type Document struct {
	Version         string              `json:"version"`
	Kind            string              `json:"kind"`
	Engine          string              `json:"engine"`
	SourcePDF       string              `json:"source_pdf"`
	GeneratedAt     string              `json:"generated_at,omitempty"`
	SourceArtifacts SourceArtifacts     `json:"source_artifacts,omitempty"`
	Postprocess     PostprocessMetadata `json:"postprocess"`
	Pages           []Page              `json:"pages"`
}

type Page struct {
	Page       int            `json:"page"`
	Width      int            `json:"width,omitempty"`
	Height     int            `json:"height,omitempty"`
	IsBlank    bool           `json:"is_blank,omitempty"`
	SourceText string         `json:"source_text"`
	Text       string         `json:"text"`
	Blocks     []Block        `json:"blocks"`
	Correction PageCorrection `json:"correction"`
}

type PageCorrection struct {
	Status        string   `json:"status"`
	ChangedBlocks int      `json:"changed_blocks"`
	TotalBlocks   int      `json:"total_blocks"`
	Notes         []string `json:"notes,omitempty"`
}

type Block struct {
	BlockID      string          `json:"block_id"`
	Text         string          `json:"text"`
	SourceText   string          `json:"source_text"`
	BBox         BBox            `json:"bbox"`
	BlockType    string          `json:"block_type"`
	Confidence   float64         `json:"confidence"`
	ReadingOrder int             `json:"reading_order"`
	Correction   BlockCorrection `json:"correction"`
}

type BlockCorrection struct {
	Status    string   `json:"status"`
	Edited    bool     `json:"edited"`
	Reasons   []string `json:"reasons"`
	Protected bool     `json:"protected,omitempty"`
	Notes     []string `json:"notes,omitempty"`
}

type BBox struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}

type sourceDocument struct {
	Engine    string       `json:"engine"`
	SourcePDF string       `json:"source_pdf"`
	Pages     []sourcePage `json:"pages"`
}

type sourcePage struct {
	Page    int           `json:"page"`
	Width   int           `json:"width"`
	Height  int           `json:"height"`
	IsBlank bool          `json:"is_blank"`
	Text    string        `json:"text"`
	Blocks  []sourceBlock `json:"blocks"`
}

type sourceBlock struct {
	Text         string  `json:"text"`
	BBox         BBox    `json:"bbox"`
	BlockType    string  `json:"block_type"`
	Confidence   float64 `json:"confidence"`
	ReadingOrder int     `json:"reading_order"`
}

type NoneProvider struct{}

func SupportedProviders() []string {
	return []string{
		ProviderNone,
		ProviderLocalLM,
		ProviderCloudLLM,
		ProviderFoundationModels,
		ProviderCodexHeadlessOAuth,
	}
}

func NormalizeProviderName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ProviderNone
	}
	return name
}

func New(name string) (Provider, error) {
	switch NormalizeProviderName(name) {
	case ProviderNone:
		return &NoneProvider{}, nil
	case ProviderCodexHeadlessOAuth:
		return newCodexHeadlessOAuthProvider(), nil
	case ProviderLocalLM, ProviderCloudLLM, ProviderFoundationModels:
		return &unimplementedProvider{name: NormalizeProviderName(name)}, nil
	default:
		return nil, fmt.Errorf("unknown postprocess provider: %s", name)
	}
}

func (p *NoneProvider) Name() string {
	return ProviderNone
}

func (p *NoneProvider) Run(_ context.Context, req ProviderRequest) (ProviderResult, error) {
	outputMode := strings.TrimSpace(req.Config.OutputMode)
	if outputMode == "" {
		outputMode = OutputModeSidecarOnly
	}
	return ProviderResult{
		Document:       req.Document,
		Applied:        false,
		CredentialKind: AuthKindNone,
		OutputMode:     outputMode,
	}, nil
}

type unimplementedProvider struct {
	name string
}

func (p *unimplementedProvider) Name() string {
	return p.name
}

func (p *unimplementedProvider) Run(_ context.Context, req ProviderRequest) (ProviderResult, error) {
	if !req.AllowRemote && providerRequiresRemote(p.name) {
		return ProviderResult{}, fmt.Errorf(
			"postprocess provider %s requires remote access; rerun with --postprocess-allow-remote",
			p.name,
		)
	}
	return ProviderResult{}, fmt.Errorf("postprocess provider %s is not implemented yet", p.name)
}

func Execute(ctx context.Context, p Provider, req Request) (Output, error) {
	if p == nil {
		return Output{}, errors.New("postprocess provider is required")
	}
	if req.OutputDir == "" {
		return Output{}, errors.New("postprocess output directory is required")
	}

	pagesJSON := strings.TrimSpace(req.OCRResult.PagesJSON)
	if pagesJSON == "" {
		if p.Name() == ProviderNone {
			return Output{Warnings: []string{WarningSkippedMissingPagesJSON}}, nil
		}
		return Output{}, errors.New("postprocess requires pages_json from OCR provider")
	}
	if _, err := os.Stat(pagesJSON); err != nil {
		if p.Name() == ProviderNone {
			return Output{Warnings: []string{WarningSkippedMissingPagesJSON}}, nil
		}
		return Output{}, fmt.Errorf("postprocess source pages_json not accessible: %w", err)
	}

	startedAt := time.Now()
	base, err := loadDocument(req)
	if err != nil {
		return Output{}, err
	}

	if req.OnProgress != nil {
		req.OnProgress(provider.ProgressEvent{
			Phase:          "stage_started",
			Stage:          "postprocess",
			CompletedPages: 0,
			TotalPages:     len(base.Pages),
		})
	}

	result, err := p.Run(ctx, ProviderRequest{
		Document:    base,
		Config:      req.Config,
		AllowRemote: req.AllowRemote,
		OnProgress:  req.OnProgress,
	})
	if err != nil {
		return Output{}, err
	}

	doc := result.Document
	normalizeDocument(&doc)
	stageTimings := cloneStageTimings(result.StageTimings)
	stageTimings["postprocess_seconds"] = time.Since(startedAt).Seconds()

	outputMode := strings.TrimSpace(result.OutputMode)
	if outputMode == "" {
		outputMode = OutputModeSidecarOnly
	}

	doc.Version = SchemaVersion
	doc.Kind = DocumentKind
	if strings.TrimSpace(doc.Engine) == "" {
		doc.Engine = base.Engine
	}
	if strings.TrimSpace(doc.SourcePDF) == "" {
		doc.SourcePDF = base.SourcePDF
	}
	doc.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	doc.SourceArtifacts = SourceArtifacts{
		PagesJSON:     req.OCRResult.PagesJSON,
		TextPath:      req.OCRResult.TextPath,
		MarkdownPath:  req.OCRResult.MarkdownPath,
		SearchablePDF: req.OCRResult.SearchablePDF,
	}
	doc.Postprocess = PostprocessMetadata{
		Provider:       p.Name(),
		Applied:        result.Applied,
		OutputMode:     outputMode,
		CredentialKind: credentialKindOrDefault(result.CredentialKind, p.Name()),
		Model:          result.Model,
		Warnings:       append([]string(nil), result.Warnings...),
		StageTimings:   stageTimings,
		ProviderMeta:   result.ProviderMeta,
	}

	correctedPagesJSON := filepath.Join(req.OutputDir, "corrected_pages.json")
	correctedTextPath := filepath.Join(req.OutputDir, "corrected_document.txt")
	correctedMarkdownPath := filepath.Join(req.OutputDir, "corrected_document.md")

	if err := writeJSON(correctedPagesJSON, doc); err != nil {
		return Output{}, err
	}
	if err := os.WriteFile(correctedTextPath, []byte(renderText(doc.Pages)+"\n"), 0o644); err != nil {
		return Output{}, err
	}
	markdown := strings.TrimSpace(renderMarkdown(doc.Pages))
	if markdown != "" {
		markdown += "\n"
	} else {
		markdown = "\n"
	}
	if err := os.WriteFile(correctedMarkdownPath, []byte(markdown), 0o644); err != nil {
		return Output{}, err
	}

	if req.OnProgress != nil {
		req.OnProgress(provider.ProgressEvent{
			Phase:          "stage_done",
			Stage:          "postprocess",
			CompletedPages: len(doc.Pages),
			TotalPages:     len(doc.Pages),
		})
	}

	return Output{
		CorrectedPagesJSON:    correctedPagesJSON,
		CorrectedTextPath:     correctedTextPath,
		CorrectedMarkdownPath: correctedMarkdownPath,
		Document:              doc,
		Applied:               result.Applied,
		ChangedPages:          countChangedPages(doc.Pages),
		Warnings:              append([]string(nil), result.Warnings...),
		StageTimings:          stageTimings,
		OutputMode:            outputMode,
	}, nil
}

func loadDocument(req Request) (Document, error) {
	body, err := os.ReadFile(req.OCRResult.PagesJSON)
	if err != nil {
		return Document{}, err
	}

	source := sourceDocument{}
	if err := json.Unmarshal(body, &source); err != nil {
		return Document{}, err
	}

	doc := Document{
		Version:   SchemaVersion,
		Kind:      DocumentKind,
		Engine:    firstNonEmpty(source.Engine, req.OCRProvider),
		SourcePDF: firstNonEmpty(source.SourcePDF, req.InputPDF),
		Pages:     make([]Page, 0, len(source.Pages)),
	}

	for pageIndex, srcPage := range source.Pages {
		pageNumber := srcPage.Page
		if pageNumber < 1 {
			pageNumber = pageIndex + 1
		}
		blocks := make([]Block, 0, len(srcPage.Blocks))
		for blockIndex, srcBlock := range srcPage.Blocks {
			readingOrder := srcBlock.ReadingOrder
			if readingOrder < 1 {
				readingOrder = blockIndex + 1
			}
			blocks = append(blocks, Block{
				Text:         srcBlock.Text,
				SourceText:   srcBlock.Text,
				BBox:         srcBlock.BBox,
				BlockType:    defaultBlockType(srcBlock.BlockType),
				Confidence:   srcBlock.Confidence,
				ReadingOrder: readingOrder,
			})
		}

		pageText := strings.TrimSpace(srcPage.Text)
		if pageText == "" {
			pageText = joinBlockTexts(blocks, false)
		}

		doc.Pages = append(doc.Pages, Page{
			Page:       pageNumber,
			Width:      maxInt(srcPage.Width, 0),
			Height:     maxInt(srcPage.Height, 0),
			IsBlank:    srcPage.IsBlank,
			SourceText: pageText,
			Text:       pageText,
			Blocks:     blocks,
		})
	}

	normalizeDocument(&doc)
	return doc, nil
}

func normalizeDocument(doc *Document) {
	if doc.Pages == nil {
		doc.Pages = []Page{}
	}
	for pageIndex := range doc.Pages {
		page := &doc.Pages[pageIndex]
		if page.Page < 1 {
			page.Page = pageIndex + 1
		}
		if page.Width < 0 {
			page.Width = 0
		}
		if page.Height < 0 {
			page.Height = 0
		}
		if page.Blocks == nil {
			page.Blocks = []Block{}
		}

		for blockIndex := range page.Blocks {
			block := &page.Blocks[blockIndex]
			if block.ReadingOrder < 1 {
				block.ReadingOrder = blockIndex + 1
			}
			if strings.TrimSpace(block.BlockType) == "" {
				block.BlockType = "paragraph"
			}
		}

		sort.SliceStable(page.Blocks, func(i, j int) bool {
			return page.Blocks[i].ReadingOrder < page.Blocks[j].ReadingOrder
		})

		changedBlocks := 0
		for blockIndex := range page.Blocks {
			block := &page.Blocks[blockIndex]
			if strings.TrimSpace(block.BlockID) == "" {
				block.BlockID = fmt.Sprintf("p%d-b%d", page.Page, blockIndex+1)
			}
			if block.SourceText == "" {
				block.SourceText = block.Text
			}
			if block.Text == "" {
				block.Text = block.SourceText
			}
			block.Correction.Edited = block.Text != block.SourceText
			if block.Correction.Edited {
				changedBlocks++
			}
			if strings.TrimSpace(block.Correction.Status) == "" {
				if block.Correction.Edited {
					block.Correction.Status = CorrectionStatusCorrected
				} else {
					block.Correction.Status = CorrectionStatusUnchanged
				}
			}
			if len(block.Correction.Reasons) == 0 {
				if block.Correction.Edited {
					block.Correction.Reasons = []string{"provider_output"}
				} else {
					block.Correction.Reasons = []string{"no_change"}
				}
			}
		}

		if strings.TrimSpace(page.SourceText) == "" {
			page.SourceText = joinBlockTexts(page.Blocks, true)
		}
		if strings.TrimSpace(page.Text) == "" {
			page.Text = joinBlockTexts(page.Blocks, false)
		}
		if strings.TrimSpace(page.Correction.Status) == "" {
			if page.Text != page.SourceText || changedBlocks > 0 {
				page.Correction.Status = CorrectionStatusCorrected
			} else {
				page.Correction.Status = CorrectionStatusUnchanged
			}
		}
		page.Correction.ChangedBlocks = changedBlocks
		page.Correction.TotalBlocks = len(page.Blocks)
		if strings.TrimSpace(page.Text) == "" && len(page.Blocks) == 0 {
			page.IsBlank = true
		}
	}
}

func joinBlockTexts(blocks []Block, useSource bool) string {
	lines := make([]string, 0, len(blocks))
	for _, block := range blocks {
		value := block.Text
		if useSource {
			value = block.SourceText
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		lines = append(lines, value)
	}
	return strings.Join(lines, "\n")
}

func renderText(pages []Page) string {
	chunks := make([]string, 0, len(pages))
	for _, page := range pages {
		text := strings.TrimSpace(page.Text)
		chunks = append(chunks, text)
	}
	return strings.Join(chunks, "\n\n")
}

func renderMarkdown(pages []Page) string {
	lines := make([]string, 0, len(pages)*4)
	for _, page := range pages {
		lines = append(lines, fmt.Sprintf("## Page %d", page.Page))
		for _, block := range page.Blocks {
			text := strings.TrimSpace(block.Text)
			if text == "" {
				continue
			}
			switch block.BlockType {
			case "heading":
				lines = append(lines, "### "+text)
			case "code":
				lines = append(lines, "```text")
				lines = append(lines, text)
				lines = append(lines, "```")
			case "caption":
				lines = append(lines, "*"+text+"*")
			default:
				lines = append(lines, text)
			}
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func countChangedPages(pages []Page) int {
	changed := 0
	for _, page := range pages {
		if page.Text != page.SourceText || page.Correction.ChangedBlocks > 0 || page.Correction.Status != CorrectionStatusUnchanged {
			changed++
		}
	}
	return changed
}

func credentialKindOrDefault(kind, providerName string) string {
	kind = strings.TrimSpace(kind)
	if kind != "" {
		return kind
	}
	if providerName == ProviderNone {
		return AuthKindNone
	}
	return AuthKindUnknown
}

func cloneStageTimings(input map[string]float64) map[string]float64 {
	if len(input) == 0 {
		return map[string]float64{}
	}
	output := make(map[string]float64, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func writeJSON(path string, payload any) error {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o644)
}

func defaultBlockType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "paragraph"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func maxInt(value, floor int) int {
	if value < floor {
		return floor
	}
	return value
}
