package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeJSON(t *testing.T, path string, payload any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func TestEvaluateAgainstGoldBasic(t *testing.T) {
	temp := t.TempDir()
	gold := map[string]any{
		"version": "v1",
		"pages": []any{
			map[string]any{
				"page":                   1,
				"prose_kr":               "안녕하세요",
				"expected_block_types":   []string{"paragraph"},
				"reading_order_snippets": []string{"안녕하세요"},
			},
		},
	}
	pred := map[string]any{
		"pages": []any{
			map[string]any{
				"page": 1,
				"text": "안녕하세요",
				"blocks": []any{
					map[string]any{"text": "안녕하세요", "block_type": "paragraph"},
				},
			},
		},
	}

	goldPath := filepath.Join(temp, "gold.json")
	predPath := filepath.Join(temp, "pred.json")
	writeJSON(t, goldPath, gold)
	writeJSON(t, predPath, pred)

	result, err := Evaluate(goldPath, predPath)
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	if result.Summary["kr_prose_cer"] != float64(0) {
		t.Fatalf("unexpected kr_prose_cer: %v", result.Summary["kr_prose_cer"])
	}
	if result.Summary["layout_macro_f1"] != float64(1) {
		t.Fatalf("unexpected layout_macro_f1: %v", result.Summary["layout_macro_f1"])
	}
	if result.Summary["reading_order_error_ratio"] != float64(0) {
		t.Fatalf("unexpected reading_order_error_ratio: %v", result.Summary["reading_order_error_ratio"])
	}
}

func TestEvaluateAgainstGoldMissingPrediction(t *testing.T) {
	temp := t.TempDir()
	gold := map[string]any{"version": "v1", "pages": []any{map[string]any{"page": 1, "prose_kr": "abc"}}}
	pred := map[string]any{"pages": []any{}}

	goldPath := filepath.Join(temp, "gold.json")
	predPath := filepath.Join(temp, "pred.json")
	writeJSON(t, goldPath, gold)
	writeJSON(t, predPath, pred)

	result, err := Evaluate(goldPath, predPath)
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	if result.PerPage[1]["missing_prediction"] != true {
		t.Fatalf("expected missing_prediction=true, got %v", result.PerPage[1]["missing_prediction"])
	}
}

func TestEvaluateAgainstGoldCodeMetric(t *testing.T) {
	temp := t.TempDir()
	gold := map[string]any{
		"version": "v1",
		"pages":   []any{map[string]any{"page": 1, "code": "a=1\nb=2"}},
	}
	pred := map[string]any{
		"pages": []any{
			map[string]any{
				"page": 1,
				"text": "a=1\nb=2",
				"blocks": []any{
					map[string]any{"text": "a=1", "block_type": "code"},
					map[string]any{"text": "b=3", "block_type": "code"},
				},
			},
		},
	}

	goldPath := filepath.Join(temp, "gold.json")
	predPath := filepath.Join(temp, "pred.json")
	writeJSON(t, goldPath, gold)
	writeJSON(t, predPath, pred)

	result, err := Evaluate(goldPath, predPath)
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	if result.Summary["code_line_accuracy"] != float64(0.5) {
		t.Fatalf("unexpected code_line_accuracy: %v", result.Summary["code_line_accuracy"])
	}
}

func TestEvaluateAgainstGoldReadingOrderError(t *testing.T) {
	temp := t.TempDir()
	gold := map[string]any{
		"version": "v1",
		"pages":   []any{map[string]any{"page": 1, "reading_order_snippets": []string{"B", "A"}}},
	}
	pred := map[string]any{"pages": []any{map[string]any{"page": 1, "text": "A ... B", "blocks": []any{}}}}

	goldPath := filepath.Join(temp, "gold.json")
	predPath := filepath.Join(temp, "pred.json")
	writeJSON(t, goldPath, gold)
	writeJSON(t, predPath, pred)

	result, err := Evaluate(goldPath, predPath)
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	if result.Summary["reading_order_error_ratio"] != float64(1) {
		t.Fatalf("unexpected reading_order_error_ratio: %v", result.Summary["reading_order_error_ratio"])
	}
}

func TestEvaluateAgainstGoldReadingOrderNormalizesPunctuation(t *testing.T) {
	temp := t.TempDir()
	gold := map[string]any{
		"version": "v1",
		"pages":   []any{map[string]any{"page": 1, "reading_order_snippets": []string{"GET /users/12", "JSON"}}},
	}
	pred := map[string]any{
		"pages": []any{map[string]any{"page": 1, "text": "GET users/12 then JSON", "blocks": []any{}}},
	}

	goldPath := filepath.Join(temp, "gold.json")
	predPath := filepath.Join(temp, "pred.json")
	writeJSON(t, goldPath, gold)
	writeJSON(t, predPath, pred)

	result, err := Evaluate(goldPath, predPath)
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	if result.Summary["reading_order_error_ratio"] != float64(0) {
		t.Fatalf("unexpected reading_order_error_ratio: %v", result.Summary["reading_order_error_ratio"])
	}
}
