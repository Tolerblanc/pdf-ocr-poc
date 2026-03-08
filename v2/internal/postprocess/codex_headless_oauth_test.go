package postprocess

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tolerblanc/pdf-ocr-poc/v2/internal/provider"
)

func TestCodexHeadlessOAuthProviderRejectsRemoteDisallowed(t *testing.T) {
	p := newCodexHeadlessOAuthProvider()
	_, err := p.Run(context.Background(), ProviderRequest{
		Document:    testCodexDocument(),
		AllowRemote: false,
	})
	if err == nil || !strings.Contains(err.Error(), "--postprocess-allow-remote") {
		t.Fatalf("expected remote-disallowed rejection, got %v", err)
	}
}

func TestCodexHeadlessOAuthProviderUsesOpencodeAuthFileAndRefreshes(t *testing.T) {
	temp := t.TempDir()
	authFile := filepath.Join(temp, "auth.json")
	fixedNow := time.Unix(1_700_000_000, 0).UTC()
	entry := map[string]codexStoredAuth{
		"openai": {
			Type:      "oauth",
			Access:    "old-access",
			Refresh:   "old-refresh",
			Expires:   fixedNow.Add(-time.Minute).UnixMilli(),
			AccountID: "old-account",
		},
	}
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal auth failed: %v", err)
	}
	if err := os.WriteFile(authFile, append(body, '\n'), 0o600); err != nil {
		t.Fatalf("write auth file failed: %v", err)
	}

	var refreshCalls int
	var correctionCalls int
	progressEvents := make([]provider.ProgressEvent, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			refreshCalls++
			payload, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(payload), "grant_type=refresh_token") {
				t.Fatalf("expected refresh_token grant, got %s", string(payload))
			}
			writeJSONResponse(t, w, map[string]any{
				"access_token":  testJWTWithAccountID("acct-123"),
				"refresh_token": "new-refresh",
				"id_token":      testJWTWithAccountID("acct-123"),
				"expires_in":    3600,
			})
		case "/backend-api/codex/responses":
			correctionCalls++
			payload := decodeJSONBody(t, r)
			if got := r.Header.Get("Authorization"); got != "Bearer "+testJWTWithAccountID("acct-123") {
				t.Fatalf("unexpected authorization header: %s", got)
			}
			if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct-123" {
				t.Fatalf("unexpected account header: %s", got)
			}
			if got := r.Header.Get("originator"); got != "opencode" {
				t.Fatalf("unexpected originator header: %s", got)
			}
			if got, ok := payload["instructions"].(string); !ok || strings.TrimSpace(got) == "" {
				t.Fatalf("expected non-empty instructions, got %+v", payload)
			}
			if got, ok := payload["store"].(bool); !ok || got {
				t.Fatalf("expected store=false, got %+v", payload)
			}
			if got, ok := payload["stream"].(bool); !ok || !got {
				t.Fatalf("expected stream=true, got %+v", payload)
			}
			input, ok := payload["input"].([]any)
			if !ok || len(input) != 1 {
				t.Fatalf("expected single input message, got %+v", payload)
			}
			message, ok := input[0].(map[string]any)
			if !ok || message["role"] != "user" {
				t.Fatalf("expected user input message, got %+v", input[0])
			}
			content, ok := message["content"].([]any)
			if !ok || len(content) != 1 {
				t.Fatalf("expected single input content item, got %+v", message)
			}
			first, ok := content[0].(map[string]any)
			if !ok || first["type"] != "input_text" {
				t.Fatalf("expected input_text content item, got %+v", content[0])
			}
			writeCodexSSEResponse(t, w,
				map[string]any{
					"type":  "response.output_text.delta",
					"delta": `{"pages":[{"page":1,"blocks":[{"block_id":"p1-b1","text":"Hello world","status":"corrected","reasons":["ocr_fix"]}]}]}`,
				},
				map[string]any{
					"type": "response.completed",
				},
			)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := &codexHeadlessOAuthProvider{
		httpClient:            server.Client(),
		now:                   func() time.Time { return fixedNow },
		stderr:                io.Discard,
		sleep:                 sleepWithContext,
		pollingSafetyMargin:   0,
		deviceAuthStatusCodes: []int{http.StatusForbidden, http.StatusNotFound},
	}

	result, err := p.Run(context.Background(), ProviderRequest{
		Document:    testCodexDocument(),
		AllowRemote: true,
		OnProgress: func(event provider.ProgressEvent) {
			progressEvents = append(progressEvents, event)
		},
		Config: Config{
			BaseURL:   server.URL + "/backend-api/codex/responses",
			IssuerURL: server.URL,
			Auth: &AuthConfig{
				Kind:       AuthKindOAuthStoreFile,
				File:       authFile,
				ProviderID: "openai",
			},
		},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected provider to be applied")
	}
	if refreshCalls != 1 {
		t.Fatalf("expected one refresh call, got %d", refreshCalls)
	}
	if correctionCalls != 1 {
		t.Fatalf("expected one correction call, got %d", correctionCalls)
	}
	if len(progressEvents) != 1 {
		t.Fatalf("expected one progress event, got %+v", progressEvents)
	}
	if progressEvents[0].Stage != "postprocess" || progressEvents[0].CompletedPages != 1 || progressEvents[0].TotalPages != 1 {
		t.Fatalf("unexpected progress event: %+v", progressEvents[0])
	}
	if got := result.Document.Pages[0].Blocks[0].Text; got != "Hello world" {
		t.Fatalf("unexpected corrected block text: %s", got)
	}
	if got := result.Document.Pages[0].Text; got != "Hello world" {
		t.Fatalf("unexpected corrected page text: %s", got)
	}
	if result.CredentialKind != AuthKindOAuthStoreFile {
		t.Fatalf("unexpected credential kind: %s", result.CredentialKind)
	}

	updatedBody, err := os.ReadFile(authFile)
	if err != nil {
		t.Fatalf("read updated auth file failed: %v", err)
	}
	updated := map[string]codexStoredAuth{}
	if err := json.Unmarshal(updatedBody, &updated); err != nil {
		t.Fatalf("parse updated auth file failed: %v", err)
	}
	if updated["openai"].Refresh != "new-refresh" {
		t.Fatalf("expected refreshed token to be saved, got %+v", updated["openai"])
	}
	if updated["openai"].AccountID != "acct-123" {
		t.Fatalf("expected account id to be saved, got %+v", updated["openai"])
	}
}

func TestCodexHeadlessOAuthProviderPerformsHeadlessDeviceAuth(t *testing.T) {
	temp := t.TempDir()
	authFile := filepath.Join(temp, "auth.json")
	fixedNow := time.Unix(1_700_000_000, 0).UTC()
	var mu sync.Mutex
	var pollCalls int
	stderr := bytes.Buffer{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			writeJSONResponse(t, w, map[string]any{
				"device_auth_id": "device-123",
				"user_code":      "ABCD-1234",
				"interval":       "0",
			})
		case "/api/accounts/deviceauth/token":
			mu.Lock()
			pollCalls++
			current := pollCalls
			mu.Unlock()
			if current == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			writeJSONResponse(t, w, map[string]any{
				"authorization_code": "auth-code-123",
				"code_verifier":      "verifier-123",
			})
		case "/oauth/token":
			payload, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(payload), "grant_type=authorization_code") {
				t.Fatalf("expected authorization_code exchange, got %s", string(payload))
			}
			writeJSONResponse(t, w, map[string]any{
				"access_token":  testJWTWithAccountID("acct-456"),
				"refresh_token": "refresh-456",
				"id_token":      testJWTWithAccountID("acct-456"),
				"expires_in":    3600,
			})
		case "/backend-api/codex/responses":
			writeCodexSSEResponse(t, w,
				map[string]any{
					"type":  "response.output_text.delta",
					"delta": "```json\n",
				},
				map[string]any{
					"type":  "response.output_text.delta",
					"delta": `{"pages":[{"page":1,"blocks":[{"block_id":"p1-b1","text":"Hello world","status":"corrected","reasons":["ocr_fix"]}]}]}`,
				},
				map[string]any{
					"type":  "response.output_text.delta",
					"delta": "\n```",
				},
				map[string]any{
					"type": "response.completed",
				},
			)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := &codexHeadlessOAuthProvider{
		httpClient:            server.Client(),
		now:                   func() time.Time { return fixedNow },
		stderr:                &stderr,
		sleep:                 func(context.Context, time.Duration) error { return nil },
		pollingSafetyMargin:   0,
		deviceAuthStatusCodes: []int{http.StatusForbidden, http.StatusNotFound},
	}

	result, err := p.Run(context.Background(), ProviderRequest{
		Document:    testCodexDocument(),
		AllowRemote: true,
		Config: Config{
			BaseURL:   server.URL + "/backend-api/codex/responses",
			IssuerURL: server.URL,
			Auth: &AuthConfig{
				Kind:       AuthKindOAuthStoreFile,
				File:       authFile,
				ProviderID: "openai",
			},
		},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(stderr.String(), "ABCD-1234") {
		t.Fatalf("expected device code in stderr, got %s", stderr.String())
	}
	if got := result.Document.Pages[0].Blocks[0].Text; got != "Hello world" {
		t.Fatalf("unexpected corrected text: %s", got)
	}
	body, err := os.ReadFile(authFile)
	if err != nil {
		t.Fatalf("expected auth file to be created: %v", err)
	}
	stored := map[string]codexStoredAuth{}
	if err := json.Unmarshal(body, &stored); err != nil {
		t.Fatalf("parse auth file failed: %v", err)
	}
	if stored["openai"].Refresh != "refresh-456" {
		t.Fatalf("unexpected stored auth: %+v", stored["openai"])
	}
	mu.Lock()
	defer mu.Unlock()
	if pollCalls < 2 {
		t.Fatalf("expected polling to occur more than once, got %d", pollCalls)
	}
}

func TestCodexHeadlessOAuthProviderParsesSSEBodyWithoutSSEContentType(t *testing.T) {
	temp := t.TempDir()
	authFile := filepath.Join(temp, "auth.json")
	body, err := json.Marshal(map[string]codexStoredAuth{
		"openai": {
			Type:      "oauth",
			Access:    testJWTWithAccountID("acct-789"),
			Refresh:   "refresh-789",
			Expires:   time.Now().Add(time.Hour).UnixMilli(),
			AccountID: "acct-789",
		},
	})
	if err != nil {
		t.Fatalf("marshal auth failed: %v", err)
	}
	if err := os.WriteFile(authFile, append(body, '\n'), 0o600); err != nil {
		t.Fatalf("write auth file failed: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/backend-api/codex/responses":
			writeCodexSSEResponseWithContentType(t, w, "application/json",
				map[string]any{
					"type":  "response.output_text.delta",
					"delta": `{"pages":[{"page":1,"blocks":[{"block_id":"p1-b1","text":"Hello world","status":"corrected","reasons":["ocr_fix"]}]}]}`,
				},
				map[string]any{
					"type": "response.completed",
				},
			)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := &codexHeadlessOAuthProvider{
		httpClient:            server.Client(),
		now:                   time.Now,
		stderr:                io.Discard,
		sleep:                 sleepWithContext,
		pollingSafetyMargin:   0,
		deviceAuthStatusCodes: []int{http.StatusForbidden, http.StatusNotFound},
	}

	result, err := p.Run(context.Background(), ProviderRequest{
		Document:    testCodexDocument(),
		AllowRemote: true,
		Config: Config{
			BaseURL: server.URL + "/backend-api/codex/responses",
			Auth: &AuthConfig{
				Kind:       AuthKindOAuthStoreFile,
				File:       authFile,
				ProviderID: "openai",
			},
		},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if got := result.Document.Pages[0].Blocks[0].Text; got != "Hello world" {
		t.Fatalf("unexpected corrected block text: %s", got)
	}
}

func TestExpandUserPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve home failed: %v", err)
	}
	if got := expandUserPath("~/auth.json"); got != filepath.Join(home, "auth.json") {
		t.Fatalf("expected expanded path, got %s", got)
	}
}

func testCodexDocument() Document {
	doc := Document{
		Version:   SchemaVersion,
		Kind:      DocumentKind,
		Engine:    "mock",
		SourcePDF: "/tmp/in.pdf",
		Pages: []Page{{
			Page:       1,
			Width:      100,
			Height:     100,
			IsBlank:    false,
			SourceText: "Hcllo world",
			Text:       "Hcllo world",
			Blocks: []Block{{
				BlockID:      "p1-b1",
				Text:         "Hcllo world",
				SourceText:   "Hcllo world",
				BlockType:    "paragraph",
				Confidence:   0.8,
				ReadingOrder: 1,
				Correction: BlockCorrection{
					Status:  CorrectionStatusUnchanged,
					Reasons: []string{"no_change"},
				},
			}},
			Correction: PageCorrection{
				Status:        CorrectionStatusUnchanged,
				ChangedBlocks: 0,
				TotalBlocks:   1,
			},
		}},
	}
	normalizeDocument(&doc)
	return doc
}

func testJWTWithAccountID(accountID string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(`{"chatgpt_account_id":"` + accountID + `"}`))
	return header + "." + body + ".sig"
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal response failed: %v", err)
	}
	_, _ = w.Write(body)
}

func decodeJSONBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body failed: %v", err)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("parse request body failed: %v body=%s", err, string(body))
	}
	return payload
}

func writeCodexSSEResponse(t *testing.T, w http.ResponseWriter, events ...any) {
	t.Helper()
	writeCodexSSEResponseWithContentType(t, w, "text/event-stream", events...)
}

func writeCodexSSEResponseWithContentType(t *testing.T, w http.ResponseWriter, contentType string, events ...any) {
	t.Helper()
	w.Header().Set("Content-Type", contentType)
	for _, event := range events {
		body, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal sse event failed: %v", err)
		}
		if _, err := w.Write([]byte("data: ")); err != nil {
			t.Fatalf("write sse prefix failed: %v", err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatalf("write sse body failed: %v", err)
		}
		if _, err := w.Write([]byte("\n\n")); err != nil {
			t.Fatalf("write sse separator failed: %v", err)
		}
	}
	if _, err := w.Write([]byte("data: [DONE]\n\n")); err != nil {
		t.Fatalf("write sse done failed: %v", err)
	}
}
