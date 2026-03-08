package postprocess

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCodexClientID            = "app_EMoamEEZ73f0CkXaXp7hrann"
	defaultCodexIssuerURL           = "https://auth.openai.com"
	defaultCodexResponsesURL        = "https://chatgpt.com/backend-api/codex/responses"
	defaultCodexModel               = "gpt-5.3-codex"
	defaultCodexTimeout             = 120 * time.Second
	defaultCodexPageBatchSize       = 1
	defaultCodexPollingSafetyMargin = 3 * time.Second
	defaultCodexUserAgent           = "opencode/0.0.0"
	defaultCodexProviderID          = "openai"
)

var (
	numberTokenRE = regexp.MustCompile(`\d+`)
	urlTokenRE    = regexp.MustCompile(`(?i)(https?://\S+|www\.\S+|\b(?:[a-z0-9-]+\.)+[a-z]{2,}(?:/\S*)?\b)`)
)

type codexHeadlessOAuthProvider struct {
	httpClient            *http.Client
	now                   func() time.Time
	stderr                io.Writer
	sleep                 func(context.Context, time.Duration) error
	pollingSafetyMargin   time.Duration
	deviceAuthStatusCodes []int
}

type codexResolvedConfig struct {
	model         string
	baseURL       string
	issuerURL     string
	clientID      string
	timeout       time.Duration
	pageBatchSize int
	outputMode    string
	temperature   *float64
	systemPrompt  string
	guard         GuardPolicy
	auth          codexResolvedAuth
}

type codexResolvedAuth struct {
	kind         string
	accessToken  string
	refreshToken string
	accountID    string
	expiresAtMS  int64
	file         string
	providerID   string
}

type codexStoredAuth struct {
	Type          string `json:"type"`
	Refresh       string `json:"refresh,omitempty"`
	Access        string `json:"access,omitempty"`
	Expires       int64  `json:"expires,omitempty"`
	AccountID     string `json:"accountId,omitempty"`
	EnterpriseURL string `json:"enterpriseUrl,omitempty"`
}

type codexTokenResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
}

type codexDeviceAuthResponse struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
	Interval     string `json:"interval"`
}

type codexDeviceTokenResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

type codexCompletionRequest struct {
	Model       string                   `json:"model"`
	Messages    []codexCompletionMessage `json:"messages"`
	Temperature *float64                 `json:"temperature,omitempty"`
}

type codexCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type codexCorrectionResponse struct {
	Pages []codexCorrectionPage `json:"pages"`
}

type codexCorrectionPage struct {
	Page   int                    `json:"page"`
	Status string                 `json:"status,omitempty"`
	Notes  []string               `json:"notes,omitempty"`
	Blocks []codexCorrectionBlock `json:"blocks"`
}

type codexCorrectionBlock struct {
	BlockID   string   `json:"block_id"`
	Text      string   `json:"text"`
	Status    string   `json:"status,omitempty"`
	Reasons   []string `json:"reasons,omitempty"`
	Protected bool     `json:"protected,omitempty"`
	Notes     []string `json:"notes,omitempty"`
}

type codexPromptPage struct {
	Page       int                `json:"page"`
	SourceText string             `json:"source_text"`
	Blocks     []codexPromptBlock `json:"blocks"`
}

type codexPromptBlock struct {
	BlockID      string  `json:"block_id"`
	BlockType    string  `json:"block_type"`
	SourceText   string  `json:"source_text"`
	Confidence   float64 `json:"confidence,omitempty"`
	ReadingOrder int     `json:"reading_order"`
	Protected    bool    `json:"protected,omitempty"`
}

type codexJWTClaims struct {
	ChatGPTAccountID string `json:"chatgpt_account_id,omitempty"`
	Organizations    []struct {
		ID string `json:"id"`
	} `json:"organizations,omitempty"`
	NestedAuth struct {
		ChatGPTAccountID string `json:"chatgpt_account_id,omitempty"`
	} `json:"https://api.openai.com/auth,omitempty"`
}

func newCodexHeadlessOAuthProvider() *codexHeadlessOAuthProvider {
	return &codexHeadlessOAuthProvider{
		now:                   time.Now,
		stderr:                os.Stderr,
		sleep:                 sleepWithContext,
		pollingSafetyMargin:   defaultCodexPollingSafetyMargin,
		deviceAuthStatusCodes: []int{http.StatusForbidden, http.StatusNotFound},
	}
}

func (p *codexHeadlessOAuthProvider) Name() string {
	return ProviderCodexHeadlessOAuth
}

func (p *codexHeadlessOAuthProvider) Run(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
	if !req.AllowRemote {
		return ProviderResult{}, fmt.Errorf(
			"postprocess provider %s requires remote access; rerun with --postprocess-allow-remote",
			ProviderCodexHeadlessOAuth,
		)
	}

	cfg := resolveCodexConfig(req.Config)

	client := p.httpClient
	if client == nil {
		client = &http.Client{}
	}

	authState, refreshed, authWarnings, err := p.resolveAuthState(ctx, client, cfg)
	if err != nil {
		return ProviderResult{}, err
	}

	doc := cloneDocument(req.Document)
	pageGroups := groupPagesForCodex(doc.Pages, cfg.pageBatchSize)
	requestCount := 0
	requestSeconds := 0.0
	warnings := append([]string(nil), authWarnings...)

	for _, group := range pageGroups {
		if len(group) == 0 {
			continue
		}
		requestCount++
		started := p.now()
		correction, err := p.requestCorrections(ctx, client, cfg, authState, group)
		requestSeconds += p.now().Sub(started).Seconds()
		if err != nil {
			return ProviderResult{}, err
		}
		applyCodexCorrections(&doc, correction, cfg.guard)
	}

	normalizeDocument(&doc)

	providerMeta := map[string]any{
		"request_count":   requestCount,
		"page_batch_size": cfg.pageBatchSize,
		"auth_source":     authState.kind,
		"provider_id":     authState.providerID,
		"auth_store_file": authState.file,
		"token_refreshed": refreshed,
	}
	if authState.accountID != "" {
		providerMeta["account_id"] = authState.accountID
	}

	return ProviderResult{
		Document: doc,
		Applied:  requestCount > 0,
		Warnings: warnings,
		StageTimings: map[string]float64{
			"codex_request_seconds": requestSeconds,
			"codex_request_count":   float64(requestCount),
		},
		CredentialKind: authState.kind,
		Model:          cfg.model,
		OutputMode:     cfg.outputMode,
		ProviderMeta:   providerMeta,
	}, nil
}

func resolveCodexConfig(config Config) codexResolvedConfig {
	timeoutSeconds := config.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = envInt("OCRPOC_POSTPROCESS_CODEX_TIMEOUT_SECONDS", int(defaultCodexTimeout/time.Second))
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = int(defaultCodexTimeout / time.Second)
	}

	pageBatchSize := config.PageBatchSize
	if pageBatchSize <= 0 {
		pageBatchSize = envInt("OCRPOC_POSTPROCESS_CODEX_PAGE_BATCH_SIZE", defaultCodexPageBatchSize)
	}
	if pageBatchSize <= 0 {
		pageBatchSize = defaultCodexPageBatchSize
	}

	outputMode := firstNonEmpty(
		config.OutputMode,
		strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_OUTPUT_MODE")),
		OutputModeSidecarOnly,
	)
	if outputMode != OutputModePrimaryArtifacts {
		outputMode = OutputModeSidecarOnly
	}

	guard := config.Guard
	if guard == (GuardPolicy{}) {
		guard = GuardPolicy{
			MaxEditDistanceRatio: 0.35,
			ProtectNumbers:       true,
			ProtectURLs:          true,
			ProtectCodeBlocks:    true,
			EmitPageDiff:         true,
		}
	}

	resolved := codexResolvedConfig{
		model: firstNonEmpty(
			config.Model,
			strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_MODEL")),
			defaultCodexModel,
		),
		baseURL: firstNonEmpty(
			config.BaseURL,
			strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_BASE_URL")),
			defaultCodexResponsesURL,
		),
		issuerURL: firstNonEmpty(
			config.IssuerURL,
			strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_ISSUER")),
			defaultCodexIssuerURL,
		),
		clientID: firstNonEmpty(
			strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_CLIENT_ID")),
			defaultCodexClientID,
		),
		timeout:       time.Duration(timeoutSeconds) * time.Second,
		pageBatchSize: pageBatchSize,
		outputMode:    outputMode,
		systemPrompt: firstNonEmpty(
			config.SystemPrompt,
			strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_SYSTEM_PROMPT")),
			defaultCodexSystemPrompt(),
		),
		guard: guard,
		auth:  resolveCodexAuth(config.Auth),
	}

	if config.Temperature != nil {
		value := *config.Temperature
		resolved.temperature = &value
	} else if envValue := strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_TEMPERATURE")); envValue != "" {
		if value, err := strconv.ParseFloat(envValue, 64); err == nil {
			resolved.temperature = &value
		}
	}

	return resolved
}

func resolveCodexAuth(config *AuthConfig) codexResolvedAuth {
	if config != nil {
		switch config.Kind {
		case AuthKindEnvOAuthAccessToken:
			return codexResolvedAuth{
				kind:         AuthKindEnvOAuthAccessToken,
				accessToken:  firstEnv(config.AccessTokenEnv, "OCRPOC_POSTPROCESS_CODEX_ACCESS_TOKEN"),
				refreshToken: firstEnv(config.RefreshTokenEnv, "OCRPOC_POSTPROCESS_CODEX_REFRESH_TOKEN"),
				accountID:    firstEnv(config.AccountIDEnv, "OCRPOC_POSTPROCESS_CODEX_ACCOUNT_ID"),
			}
		case AuthKindOAuthStoreFile:
			return codexResolvedAuth{
				kind:       AuthKindOAuthStoreFile,
				file:       expandUserPath(firstNonEmpty(config.File, strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_AUTH_FILE")), defaultOpencodeAuthFile())),
				providerID: firstNonEmpty(config.ProviderID, strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_PROVIDER_ID")), defaultCodexProviderID),
			}
		}
	}

	if accessToken := strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_ACCESS_TOKEN")); accessToken != "" || strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_REFRESH_TOKEN")) != "" {
		return codexResolvedAuth{
			kind:         AuthKindEnvOAuthAccessToken,
			accessToken:  accessToken,
			refreshToken: strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_REFRESH_TOKEN")),
			accountID:    strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_ACCOUNT_ID")),
		}
	}

	return codexResolvedAuth{
		kind:       AuthKindOAuthStoreFile,
		file:       expandUserPath(firstNonEmpty(strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_AUTH_FILE")), defaultOpencodeAuthFile())),
		providerID: firstNonEmpty(strings.TrimSpace(os.Getenv("OCRPOC_POSTPROCESS_CODEX_PROVIDER_ID")), defaultCodexProviderID),
	}
}

func (p *codexHeadlessOAuthProvider) resolveAuthState(
	ctx context.Context,
	client *http.Client,
	cfg codexResolvedConfig,
) (codexResolvedAuth, bool, []string, error) {
	state := cfg.auth
	refreshed := false
	warnings := []string{}

	switch state.kind {
	case AuthKindEnvOAuthAccessToken:
		if state.refreshToken != "" && (state.accessToken == "" || state.expiresAtMS > 0 && state.expiresAtMS <= p.now().UnixMilli()) {
			tokens, err := p.refreshAccessToken(ctx, client, cfg, state.refreshToken)
			if err != nil {
				return codexResolvedAuth{}, false, nil, err
			}
			state.accessToken = firstNonEmpty(tokens.AccessToken, state.accessToken)
			state.refreshToken = firstNonEmpty(tokens.RefreshToken, state.refreshToken)
			state.accountID = firstNonEmpty(extractCodexAccountID(tokens), state.accountID)
			state.expiresAtMS = p.now().Add(time.Duration(tokens.ExpiresIn) * time.Second).UnixMilli()
			refreshed = true
		}
	case AuthKindOAuthStoreFile:
		entry, allEntries, err := loadCodexAuthEntry(state.file, state.providerID)
		if err != nil && !os.IsNotExist(err) {
			return codexResolvedAuth{}, false, nil, err
		}
		if entry.Type == "oauth" {
			state.accessToken = entry.Access
			state.refreshToken = entry.Refresh
			state.accountID = entry.AccountID
			state.expiresAtMS = entry.Expires
		}
		if state.refreshToken == "" && state.accessToken == "" {
			entry, err = p.deviceAuthorize(ctx, client, cfg)
			if err != nil {
				return codexResolvedAuth{}, false, nil, err
			}
			state.accessToken = entry.Access
			state.refreshToken = entry.Refresh
			state.accountID = entry.AccountID
			state.expiresAtMS = entry.Expires
			refreshed = true
			if err := saveCodexAuthEntry(state.file, state.providerID, entry, allEntries); err != nil {
				return codexResolvedAuth{}, false, nil, err
			}
		} else if state.accessToken == "" || state.expiresAtMS <= p.now().UnixMilli() {
			tokens, err := p.refreshAccessToken(ctx, client, cfg, state.refreshToken)
			if err != nil {
				return codexResolvedAuth{}, false, nil, err
			}
			state.accessToken = firstNonEmpty(tokens.AccessToken, state.accessToken)
			state.refreshToken = firstNonEmpty(tokens.RefreshToken, state.refreshToken)
			state.accountID = firstNonEmpty(extractCodexAccountID(tokens), state.accountID)
			state.expiresAtMS = p.now().Add(time.Duration(tokens.ExpiresIn) * time.Second).UnixMilli()
			refreshed = true
			entry.Access = state.accessToken
			entry.Refresh = state.refreshToken
			entry.AccountID = state.accountID
			entry.Expires = state.expiresAtMS
			entry.Type = "oauth"
			if err := saveCodexAuthEntry(state.file, state.providerID, entry, allEntries); err != nil {
				return codexResolvedAuth{}, false, nil, err
			}
		}
	default:
		return codexResolvedAuth{}, false, nil, fmt.Errorf("unsupported codex auth kind: %s", state.kind)
	}

	if state.accessToken == "" {
		return codexResolvedAuth{}, false, nil, errors.New("codex oauth access token is missing")
	}
	if state.accountID == "" {
		warnings = append(warnings, "codex_account_id_missing")
	}
	return state, refreshed, warnings, nil
}

func loadCodexAuthEntry(path string, providerID string) (codexStoredAuth, map[string]codexStoredAuth, error) {
	path = expandUserPath(path)
	body, err := os.ReadFile(path)
	if err != nil {
		return codexStoredAuth{}, nil, err
	}
	entries := map[string]codexStoredAuth{}
	if err := json.Unmarshal(body, &entries); err != nil {
		return codexStoredAuth{}, nil, err
	}
	entry, ok := entries[providerID]
	if !ok {
		return codexStoredAuth{}, entries, nil
	}
	return entry, entries, nil
}

func saveCodexAuthEntry(path string, providerID string, entry codexStoredAuth, existing map[string]codexStoredAuth) error {
	path = expandUserPath(path)
	if existing == nil {
		existing = map[string]codexStoredAuth{}
	}
	existing[providerID] = entry
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o600)
}

func (p *codexHeadlessOAuthProvider) deviceAuthorize(
	ctx context.Context,
	client *http.Client,
	cfg codexResolvedConfig,
) (codexStoredAuth, error) {
	requestPayload := map[string]string{
		"client_id": cfg.clientID,
	}
	deviceReq, err := json.Marshal(requestPayload)
	if err != nil {
		return codexStoredAuth{}, err
	}

	responseBody, err := p.doJSONRequest(
		ctx,
		client,
		cfg.timeout,
		http.MethodPost,
		cfg.issuerURL+"/api/accounts/deviceauth/usercode",
		bytes.NewReader(deviceReq),
		map[string]string{
			"Content-Type": "application/json",
			"User-Agent":   defaultCodexUserAgent,
		},
	)
	if err != nil {
		return codexStoredAuth{}, err
	}

	device := codexDeviceAuthResponse{}
	if err := json.Unmarshal(responseBody, &device); err != nil {
		return codexStoredAuth{}, err
	}
	intervalSeconds, _ := strconv.Atoi(device.Interval)
	if intervalSeconds < 1 {
		intervalSeconds = 5
	}
	if p.stderr != nil {
		_, _ = fmt.Fprintf(
			p.stderr,
			"codex-headless-oauth login: open %s/codex/device and enter code %s\n",
			strings.TrimRight(cfg.issuerURL, "/"),
			device.UserCode,
		)
	}

	for {
		pollReqBody, err := json.Marshal(map[string]string{
			"device_auth_id": device.DeviceAuthID,
			"user_code":      device.UserCode,
		})
		if err != nil {
			return codexStoredAuth{}, err
		}

		requestCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
		req, err := http.NewRequestWithContext(
			requestCtx,
			http.MethodPost,
			cfg.issuerURL+"/api/accounts/deviceauth/token",
			bytes.NewReader(pollReqBody),
		)
		if err != nil {
			cancel()
			return codexStoredAuth{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", defaultCodexUserAgent)

		resp, err := client.Do(req)
		cancel()
		if err != nil {
			return codexStoredAuth{}, err
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return codexStoredAuth{}, readErr
		}

		if resp.StatusCode == http.StatusOK {
			deviceToken := codexDeviceTokenResponse{}
			if err := json.Unmarshal(body, &deviceToken); err != nil {
				return codexStoredAuth{}, err
			}
			tokens, err := p.exchangeAuthorizationCode(ctx, client, cfg, deviceToken.AuthorizationCode, deviceToken.CodeVerifier)
			if err != nil {
				return codexStoredAuth{}, err
			}
			return codexStoredAuth{
				Type:      "oauth",
				Refresh:   tokens.RefreshToken,
				Access:    tokens.AccessToken,
				Expires:   p.now().Add(time.Duration(tokens.ExpiresIn) * time.Second).UnixMilli(),
				AccountID: extractCodexAccountID(tokens),
			}, nil
		}

		if !slices.Contains(p.deviceAuthStatusCodes, resp.StatusCode) {
			return codexStoredAuth{}, fmt.Errorf("codex device authorization failed: %s", strings.TrimSpace(string(body)))
		}

		if err := p.sleep(ctx, time.Duration(intervalSeconds)*time.Second+p.pollingSafetyMargin); err != nil {
			return codexStoredAuth{}, err
		}
	}
}

func (p *codexHeadlessOAuthProvider) exchangeAuthorizationCode(
	ctx context.Context,
	client *http.Client,
	cfg codexResolvedConfig,
	code string,
	codeVerifier string,
) (codexTokenResponse, error) {
	body := strings.NewReader(url.Values{
		"grant_type":    []string{"authorization_code"},
		"code":          []string{code},
		"redirect_uri":  []string{strings.TrimRight(cfg.issuerURL, "/") + "/deviceauth/callback"},
		"client_id":     []string{cfg.clientID},
		"code_verifier": []string{codeVerifier},
	}.Encode())
	responseBody, err := p.doJSONRequest(
		ctx,
		client,
		cfg.timeout,
		http.MethodPost,
		cfg.issuerURL+"/oauth/token",
		body,
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
	)
	if err != nil {
		return codexTokenResponse{}, err
	}
	tokens := codexTokenResponse{}
	if err := json.Unmarshal(responseBody, &tokens); err != nil {
		return codexTokenResponse{}, err
	}
	return tokens, nil
}

func (p *codexHeadlessOAuthProvider) refreshAccessToken(
	ctx context.Context,
	client *http.Client,
	cfg codexResolvedConfig,
	refreshToken string,
) (codexTokenResponse, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return codexTokenResponse{}, errors.New("codex oauth refresh token is missing")
	}
	responseBody, err := p.doJSONRequest(
		ctx,
		client,
		cfg.timeout,
		http.MethodPost,
		cfg.issuerURL+"/oauth/token",
		strings.NewReader(url.Values{
			"grant_type":    []string{"refresh_token"},
			"refresh_token": []string{refreshToken},
			"client_id":     []string{cfg.clientID},
		}.Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
	)
	if err != nil {
		return codexTokenResponse{}, err
	}
	tokens := codexTokenResponse{}
	if err := json.Unmarshal(responseBody, &tokens); err != nil {
		return codexTokenResponse{}, err
	}
	return tokens, nil
}

func (p *codexHeadlessOAuthProvider) requestCorrections(
	ctx context.Context,
	client *http.Client,
	cfg codexResolvedConfig,
	authState codexResolvedAuth,
	pages []*Page,
) (codexCorrectionResponse, error) {
	promptPages := make([]codexPromptPage, 0, len(pages))
	for _, page := range pages {
		if page == nil {
			continue
		}
		promptBlocks := make([]codexPromptBlock, 0, len(page.Blocks))
		for _, block := range page.Blocks {
			promptBlocks = append(promptBlocks, codexPromptBlock{
				BlockID:      block.BlockID,
				BlockType:    block.BlockType,
				SourceText:   block.SourceText,
				Confidence:   block.Confidence,
				ReadingOrder: block.ReadingOrder,
				Protected:    isProtectedBlock(block, cfg.guard),
			})
		}
		promptPages = append(promptPages, codexPromptPage{
			Page:       page.Page,
			SourceText: page.SourceText,
			Blocks:     promptBlocks,
		})
	}

	promptJSON, err := json.Marshal(promptPages)
	if err != nil {
		return codexCorrectionResponse{}, err
	}

	requestBody := codexCompletionRequest{
		Model: cfg.model,
		Messages: []codexCompletionMessage{
			{Role: "system", Content: cfg.systemPrompt},
			{Role: "user", Content: buildCodexUserPrompt(string(promptJSON))},
		},
		Temperature: cfg.temperature,
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return codexCorrectionResponse{}, err
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + authState.accessToken,
		"User-Agent":    defaultCodexUserAgent,
		"originator":    "opencode",
		"session_id":    randomSessionID(),
	}
	if authState.accountID != "" {
		headers["ChatGPT-Account-Id"] = authState.accountID
	}

	responseBody, err := p.doJSONRequest(
		ctx,
		client,
		cfg.timeout,
		http.MethodPost,
		cfg.baseURL,
		bytes.NewReader(body),
		headers,
	)
	if err != nil {
		return codexCorrectionResponse{}, err
	}

	responseText, err := extractCodexResponseText(responseBody)
	if err != nil {
		return codexCorrectionResponse{}, err
	}
	responseText = extractJSONObject(responseText)
	correction := codexCorrectionResponse{}
	if err := json.Unmarshal([]byte(responseText), &correction); err != nil {
		return codexCorrectionResponse{}, fmt.Errorf("parse codex correction response: %w", err)
	}
	return correction, nil
}

func (p *codexHeadlessOAuthProvider) doJSONRequest(
	ctx context.Context,
	client *http.Client,
	timeout time.Duration,
	method string,
	url string,
	body io.Reader,
	headers map[string]string,
) ([]byte, error) {
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, method, url, body)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		if strings.TrimSpace(value) == "" {
			continue
		}
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("codex request failed: %s", strings.TrimSpace(string(responseBody)))
	}
	return responseBody, nil
}

func applyCodexCorrections(doc *Document, correction codexCorrectionResponse, guard GuardPolicy) {
	pagesByNumber := map[int]*Page{}
	for index := range doc.Pages {
		pagesByNumber[doc.Pages[index].Page] = &doc.Pages[index]
	}

	for _, correctedPage := range correction.Pages {
		page := pagesByNumber[correctedPage.Page]
		if page == nil {
			continue
		}
		if len(correctedPage.Notes) > 0 {
			page.Correction.Notes = append([]string(nil), correctedPage.Notes...)
		}
		if correctedPage.Status != "" {
			page.Correction.Status = correctedPage.Status
		}

		blocksByID := map[string]*Block{}
		for index := range page.Blocks {
			blocksByID[page.Blocks[index].BlockID] = &page.Blocks[index]
		}

		for _, correctedBlock := range correctedPage.Blocks {
			block := blocksByID[correctedBlock.BlockID]
			if block == nil {
				continue
			}

			candidate := strings.TrimSpace(correctedBlock.Text)
			if candidate == "" {
				candidate = block.SourceText
			}

			allowed, reason := guardCodexBlockCorrection(*block, candidate, guard)
			if !allowed {
				block.Text = block.SourceText
				block.Correction.Status = CorrectionStatusRejected
				block.Correction.Reasons = append(uniqueStrings(correctedBlock.Reasons), reason)
				block.Correction.Notes = uniqueStrings(correctedBlock.Notes)
				block.Correction.Protected = correctedBlock.Protected || isProtectedBlock(*block, guard)
				continue
			}

			block.Text = candidate
			block.Correction.Protected = correctedBlock.Protected || isProtectedBlock(*block, guard)
			block.Correction.Notes = uniqueStrings(correctedBlock.Notes)
			block.Correction.Reasons = uniqueStrings(correctedBlock.Reasons)
			if len(block.Correction.Reasons) == 0 {
				if candidate == block.SourceText {
					block.Correction.Reasons = []string{"no_change"}
				} else {
					block.Correction.Reasons = []string{"provider_output"}
				}
			}
			if correctedBlock.Status != "" {
				block.Correction.Status = correctedBlock.Status
			} else if candidate == block.SourceText {
				block.Correction.Status = CorrectionStatusUnchanged
			} else {
				block.Correction.Status = CorrectionStatusCorrected
			}
		}
		page.Text = joinBlockTexts(page.Blocks, false)
	}
}

func groupPagesForCodex(pages []Page, batchSize int) [][]*Page {
	if batchSize < 1 {
		batchSize = 1
	}
	groups := [][]*Page{}
	current := []*Page{}
	for index := range pages {
		page := &pages[index]
		if strings.TrimSpace(page.SourceText) == "" && len(page.Blocks) == 0 {
			continue
		}
		current = append(current, page)
		if len(current) >= batchSize {
			groups = append(groups, current)
			current = []*Page{}
		}
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	return groups
}

func cloneDocument(doc Document) Document {
	cloned := doc
	cloned.Pages = make([]Page, 0, len(doc.Pages))
	for _, page := range doc.Pages {
		pageCopy := page
		pageCopy.Correction.Notes = append([]string(nil), page.Correction.Notes...)
		pageCopy.Blocks = make([]Block, 0, len(page.Blocks))
		for _, block := range page.Blocks {
			blockCopy := block
			blockCopy.Correction.Reasons = append([]string(nil), block.Correction.Reasons...)
			blockCopy.Correction.Notes = append([]string(nil), block.Correction.Notes...)
			pageCopy.Blocks = append(pageCopy.Blocks, blockCopy)
		}
		cloned.Pages = append(cloned.Pages, pageCopy)
	}
	return cloned
}

func guardCodexBlockCorrection(block Block, corrected string, guard GuardPolicy) (bool, string) {
	corrected = strings.TrimSpace(corrected)
	source := strings.TrimSpace(block.SourceText)
	if corrected == source {
		return true, ""
	}
	if guard.ProtectCodeBlocks && strings.EqualFold(block.BlockType, "code") {
		return false, "protected_code_block"
	}
	if guard.ProtectNumbers && !slices.Equal(extractNumberTokens(source), extractNumberTokens(corrected)) {
		return false, "protected_number_sequence"
	}
	if guard.ProtectURLs && !slices.Equal(extractURLTokens(source), extractURLTokens(corrected)) {
		return false, "protected_url"
	}
	if guard.MaxEditDistanceRatio > 0 && source != "" {
		ratio := float64(levenshteinRunes([]rune(source), []rune(corrected))) / float64(len([]rune(source)))
		if ratio > guard.MaxEditDistanceRatio {
			return false, "edit_distance_limit_exceeded"
		}
	}
	return true, ""
}

func isProtectedBlock(block Block, guard GuardPolicy) bool {
	if guard.ProtectCodeBlocks && strings.EqualFold(block.BlockType, "code") {
		return true
	}
	if guard.ProtectURLs && len(extractURLTokens(block.SourceText)) > 0 {
		return true
	}
	return guard.ProtectNumbers && len(extractNumberTokens(block.SourceText)) > 0
}

func extractNumberTokens(text string) []string {
	return numberTokenRE.FindAllString(text, -1)
}

func extractURLTokens(text string) []string {
	matches := urlTokenRE.FindAllString(text, -1)
	for index := range matches {
		matches[index] = strings.ToLower(matches[index])
	}
	return matches
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func extractCodexAccountID(tokens codexTokenResponse) string {
	if claims, ok := parseCodexJWTClaims(tokens.IDToken); ok {
		if accountID := accountIDFromClaims(claims); accountID != "" {
			return accountID
		}
	}
	if claims, ok := parseCodexJWTClaims(tokens.AccessToken); ok {
		return accountIDFromClaims(claims)
	}
	return ""
}

func parseCodexJWTClaims(token string) (codexJWTClaims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return codexJWTClaims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return codexJWTClaims{}, false
	}
	claims := codexJWTClaims{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return codexJWTClaims{}, false
	}
	return claims, true
}

func accountIDFromClaims(claims codexJWTClaims) string {
	if claims.ChatGPTAccountID != "" {
		return claims.ChatGPTAccountID
	}
	if claims.NestedAuth.ChatGPTAccountID != "" {
		return claims.NestedAuth.ChatGPTAccountID
	}
	if len(claims.Organizations) > 0 {
		return claims.Organizations[0].ID
	}
	return ""
}

func extractCodexResponseText(body []byte) (string, error) {
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if value, ok := payload["output_text"].(string); ok && strings.TrimSpace(value) != "" {
		return value, nil
	}
	if choices, ok := payload["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if message, ok := choice["message"].(map[string]any); ok {
				if text := extractTextValue(message["content"]); strings.TrimSpace(text) != "" {
					return text, nil
				}
			}
		}
	}
	if output, ok := payload["output"].([]any); ok {
		parts := []string{}
		for _, item := range output {
			mapped, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text := extractTextValue(mapped["content"]); strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n"), nil
		}
	}
	return "", errors.New("codex response did not contain text output")
}

func extractTextValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := []string{}
		for _, item := range typed {
			switch content := item.(type) {
			case string:
				parts = append(parts, content)
			case map[string]any:
				if text, ok := content["text"].(string); ok {
					parts = append(parts, text)
					continue
				}
				if value, ok := content["value"].(string); ok {
					parts = append(parts, value)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func extractJSONObject(text string) string {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	start := strings.Index(trimmed, "{")
	if start < 0 {
		return trimmed
	}
	depth := 0
	inString := false
	escaped := false
	for index := start; index < len(trimmed); index++ {
		char := trimmed[index]
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' && inString {
			escaped = true
			continue
		}
		if char == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch char {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return trimmed[start : index+1]
			}
		}
	}
	return trimmed[start:]
}

func buildCodexUserPrompt(pagesJSON string) string {
	return "Return JSON only. Correct OCR conservatively. Keep page numbers, block ids, block count, and block order unchanged. If unsure, keep source text. Do not invent content. Protect numbers, URLs, and code-like text unless the OCR mistake is obvious.\n\nReturn this shape:\n{\"pages\":[{\"page\":1,\"status\":\"unchanged|corrected\",\"notes\":[\"optional\"],\"blocks\":[{\"block_id\":\"p1-b1\",\"text\":\"...\",\"status\":\"unchanged|corrected\",\"reasons\":[\"...\"],\"protected\":false,\"notes\":[\"optional\"]}]}]}\n\nInput pages JSON:\n" + pagesJSON
}

func defaultCodexSystemPrompt() string {
	return "You are correcting OCR output for searchable PDF regeneration. Preserve meaning and formatting. Never merge or split blocks. Prefer minimal edits."
}

func defaultOpencodeAuthFile() string {
	if xdgDataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdgDataHome != "" {
		return filepath.Join(xdgDataHome, "opencode", "auth.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".local", "share", "opencode", "auth.json")
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}

func expandUserPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func randomSessionID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "ocrpoc-codex"
	}
	return hex.EncodeToString(buf)
}

func sleepWithContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func levenshteinRunes(a []rune, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	for index := range prev {
		prev[index] = index
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
