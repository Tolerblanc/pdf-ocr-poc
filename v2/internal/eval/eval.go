package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

type goldPage struct {
	Page                 int      `json:"page"`
	ProseKR              string   `json:"prose_kr"`
	ProseMixed           string   `json:"prose_mixed"`
	Code                 string   `json:"code"`
	ExpectedBlockTypes   []string `json:"expected_block_types"`
	ReadingOrderSnippets []string `json:"reading_order_snippets"`
}

type goldDoc struct {
	Version string     `json:"version"`
	Pages   []goldPage `json:"pages"`
}

type predBlock struct {
	Text      string `json:"text"`
	BlockType string `json:"block_type"`
}

type predPage struct {
	Page   int         `json:"page"`
	Text   string      `json:"text"`
	Blocks []predBlock `json:"blocks"`
}

type predDoc struct {
	Pages []predPage `json:"pages"`
}

type Result struct {
	Summary map[string]any         `json:"summary"`
	PerPage map[int]map[string]any `json:"per_page"`
	Meta    map[string]any         `json:"meta"`
}

func Evaluate(goldPath, predPath string) (Result, error) {
	gold, err := loadGold(goldPath)
	if err != nil {
		return Result{}, fmt.Errorf("load gold: %w", err)
	}
	pred, err := loadPred(predPath)
	if err != nil {
		return Result{}, fmt.Errorf("load prediction: %w", err)
	}

	predPages := map[int]predPage{}
	for _, page := range pred.Pages {
		predPages[page.Page] = page
	}

	krSamples := []float64{}
	mixedSamples := []float64{}
	codeSamples := []float64{}
	layoutSamples := []float64{}
	readingOrderErrors := 0
	readingOrderTotal := 0

	perPage := map[int]map[string]any{}

	for _, goldPage := range gold.Pages {
		predPage, ok := predPages[goldPage.Page]
		if !ok {
			perPage[goldPage.Page] = map[string]any{"missing_prediction": true}
			continue
		}

		metrics := map[string]any{}
		predText := predPage.Text

		if strings.TrimSpace(goldPage.ProseKR) != "" {
			value := bestSnippetCER(goldPage.ProseKR, predText)
			krSamples = append(krSamples, value)
			metrics["kr_prose_cer"] = value
		}

		if strings.TrimSpace(goldPage.ProseMixed) != "" {
			value := bestSnippetCER(goldPage.ProseMixed, predText)
			mixedSamples = append(mixedSamples, value)
			metrics["mixed_prose_cer"] = value
		}

		if strings.TrimSpace(goldPage.Code) != "" {
			lines := make([]string, 0, len(predPage.Blocks))
			for _, block := range predPage.Blocks {
				if block.BlockType == "code" {
					lines = append(lines, block.Text)
				}
			}
			value := codeLineAccuracy(goldPage.Code, strings.Join(lines, "\n"))
			codeSamples = append(codeSamples, value)
			metrics["code_line_accuracy"] = value
		}

		expectedTypes := toSet(goldPage.ExpectedBlockTypes)
		if len(expectedTypes) > 0 {
			actualTypes := map[string]struct{}{}
			for _, block := range predPage.Blocks {
				kind := block.BlockType
				if strings.TrimSpace(kind) == "" {
					kind = "paragraph"
				}
				actualTypes[kind] = struct{}{}
			}
			value := f1(expectedTypes, actualTypes)
			layoutSamples = append(layoutSamples, value)
			metrics["layout_f1"] = value
		}

		if len(goldPage.ReadingOrderSnippets) > 0 {
			readingOrderTotal++
			positions := normalizedSnippetPositions(predText, goldPage.ReadingOrderSnippets)
			hasMissing := false
			for _, pos := range positions {
				if pos < 0 {
					hasMissing = true
					break
				}
			}

			if hasMissing {
				readingOrderErrors++
				metrics["reading_order_ok"] = false
			} else {
				inOrder := true
				for i := 0; i+1 < len(positions); i++ {
					if positions[i] >= positions[i+1] {
						inOrder = false
						break
					}
				}
				if !inOrder {
					readingOrderErrors++
				}
				metrics["reading_order_ok"] = inOrder
			}
		}

		perPage[goldPage.Page] = metrics
	}

	summary := map[string]any{
		"kr_prose_cer":              averageOrNil(krSamples),
		"mixed_prose_cer":           averageOrNil(mixedSamples),
		"code_line_accuracy":        averageOrNil(codeSamples),
		"layout_macro_f1":           averageOrNil(layoutSamples),
		"reading_order_error_ratio": ratioOrNil(readingOrderErrors, readingOrderTotal),
	}

	goldVersion := gold.Version
	if strings.TrimSpace(goldVersion) == "" {
		goldVersion = "unknown"
	}

	meta := map[string]any{
		"gold_version":    goldVersion,
		"evaluated_pages": len(gold.Pages),
	}

	return Result{
		Summary: summary,
		PerPage: perPage,
		Meta:    meta,
	}, nil
}

func Save(path string, result Result) error {
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o644)
}

func loadGold(path string) (goldDoc, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return goldDoc{}, err
	}

	doc := goldDoc{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return goldDoc{}, err
	}

	sort.Slice(doc.Pages, func(i, j int) bool {
		return doc.Pages[i].Page < doc.Pages[j].Page
	})

	return doc, nil
}

func loadPred(path string) (predDoc, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return predDoc{}, err
	}

	doc := predDoc{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return predDoc{}, err
	}

	sort.Slice(doc.Pages, func(i, j int) bool {
		return doc.Pages[i].Page < doc.Pages[j].Page
	})

	return doc, nil
}

func averageOrNil(values []float64) any {
	if len(values) == 0 {
		return nil
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func ratioOrNil(numerator, denominator int) any {
	if denominator == 0 {
		return nil
	}
	return float64(numerator) / float64(denominator)
}

func normalizeProse(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for idx, line := range lines {
		lines[idx] = strings.TrimRight(line, " \t")
	}
	text = strings.Join(lines, "\n")
	spaceRe := regexp.MustCompile(`[\t ]+`)
	text = spaceRe.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func normalizeCode(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for idx, line := range lines {
		lines[idx] = strings.TrimRight(line, " \t")
	}
	text = strings.Join(lines, "\n")
	return strings.Trim(text, "\n")
}

func levenshteinDistance(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	for i := range prev {
		prev[i] = i
	}

	for i, ra := range a {
		curr := make([]int, len(b)+1)
		curr[0] = i + 1
		for j, rb := range b {
			insertCost := curr[j] + 1
			deleteCost := prev[j+1] + 1
			replaceCost := prev[j]
			if ra != rb {
				replaceCost++
			}
			curr[j+1] = min(insertCost, deleteCost, replaceCost)
		}
		prev = curr
	}

	return prev[len(prev)-1]
}

func cer(reference, prediction string) float64 {
	ref := []rune(normalizeProse(reference))
	pred := []rune(normalizeProse(prediction))
	if len(ref) == 0 {
		if len(pred) == 0 {
			return 0
		}
		return 1
	}
	return float64(levenshteinDistance(ref, pred)) / float64(len(ref))
}

func bestSnippetCER(reference, predictionText string) float64 {
	ref := normalizeProse(reference)
	pred := normalizeProse(predictionText)
	if ref == "" {
		return 0
	}
	if pred == "" {
		return 1
	}
	if strings.Contains(pred, ref) {
		return 0
	}

	best := 1.0
	for _, line := range strings.Split(pred, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		score := cer(ref, line)
		if score < best {
			best = score
		}
	}
	return best
}

func codeLineAccuracy(reference, prediction string) float64 {
	normalizeLine := func(line string) string {
		line = strings.TrimRight(line, " \t")
		line = regexp.MustCompile(`\s+`).ReplaceAllString(line, " ")
		line = regexp.MustCompile(`\s*([{}\[\],:])\s*`).ReplaceAllString(line, "$1")
		line = regexp.MustCompile(`\s*/\s*`).ReplaceAllString(line, "/")
		return strings.TrimSpace(line)
	}

	refLines := []string{}
	for _, line := range strings.Split(normalizeCode(reference), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		refLines = append(refLines, normalizeLine(line))
	}

	predLines := []string{}
	for _, line := range strings.Split(normalizeCode(prediction), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		predLines = append(predLines, normalizeLine(line))
	}

	if len(refLines) == 0 {
		if len(predLines) == 0 {
			return 1
		}
		return 0
	}

	dp := make([][]int, len(refLines)+1)
	for i := range dp {
		dp[i] = make([]int, len(predLines)+1)
	}

	for i := 1; i <= len(refLines); i++ {
		for j := 1; j <= len(predLines); j++ {
			if refLines[i-1] == predLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				dp[i][j] = max(dp[i-1][j], dp[i][j-1])
			}
		}
	}

	matches := dp[len(refLines)][len(predLines)]
	return float64(matches) / float64(len(refLines))
}

func f1(expected, actual map[string]struct{}) float64 {
	if len(expected) == 0 && len(actual) == 0 {
		return 1
	}
	if len(expected) == 0 || len(actual) == 0 {
		return 0
	}
	tp := 0
	for key := range expected {
		if _, ok := actual[key]; ok {
			tp++
		}
	}
	precision := float64(tp) / float64(len(actual))
	recall := float64(tp) / float64(len(expected))
	if precision+recall == 0 {
		return 0
	}
	return (2 * precision * recall) / (precision + recall)
}

func normalizedSnippetPositions(text string, snippets []string) []int {
	normalizeForSearch := func(value string) string {
		compact := strings.ToLower(normalizeProse(value))
		re := regexp.MustCompile(`[0-9a-z가-힣]+`)
		parts := re.FindAllString(compact, -1)
		return strings.Join(parts, "")
	}

	normalizedText := normalizeForSearch(text)
	positions := make([]int, 0, len(snippets))
	for _, snippet := range snippets {
		needle := normalizeForSearch(snippet)
		positions = append(positions, strings.Index(normalizedText, needle))
	}
	return positions
}

func toSet(items []string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		result[item] = struct{}{}
	}
	return result
}

func min(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	v := values[0]
	for _, item := range values[1:] {
		if item < v {
			v = item
		}
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
