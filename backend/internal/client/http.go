// Package client provides a rate-limited HTTP client for the Aster Finance API.
// Handles auth injection, retry/backoff, and error parsing per aster-api-errors-v3 skill.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"aster-bot/internal/auth"
	"go.uber.org/zap"
)

// APIError represents an Aster API error response.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("aster api error %d: %s", e.Code, e.Message)
}

// IsRateLimit returns true if the error is a rate-limit (429) response.
func (e *APIError) IsRateLimit() bool { return e.Code == -1003 }

// IsSignatureError returns true if the error is an auth/signature error.
func (e *APIError) IsSignatureError() bool { return e.Code == -1022 }

// HTTPClient wraps http.Client with Aster-specific logic.
type HTTPClient struct {
	base       string
	httpClient *http.Client
	signer     *auth.Signer
	log        *zap.Logger
	maxRetries int
	rateLimit  <-chan time.Time // NEW
}

// NewHTTPClient creates a new Aster HTTP client.
func NewHTTPClient(baseURL string, signer *auth.Signer, log *zap.Logger, rps int) *HTTPClient {
	if rps <= 0 {
		rps = 10 // Default
	}
	ticker := time.NewTicker(time.Second / time.Duration(rps))
	return &HTTPClient{
		base:       strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 15 * time.Second},
		signer:     signer,
		log:        log,
		maxRetries: 3,
		rateLimit:  ticker.C,
	}
}

// GetPublic executes an unsigned GET request — for public market data endpoints.
func (c *HTTPClient) GetPublic(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	u := c.buildURL(path, params)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return c.doWithRetry(req, false)
}

// GetSigned executes a signed GET request.
func (c *HTTPClient) GetSigned(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	signed, err := c.signer.SignRequest(params)
	if err != nil {
		return nil, fmt.Errorf("client: sign GET: %w", err)
	}
	u := c.buildURL(path, signed)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", c.signer.APIKey())
	return c.doWithRetry(req, true)
}

// PostSigned executes a signed POST request with application/x-www-form-urlencoded body.
func (c *HTTPClient) PostSigned(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	signed, err := c.signer.SignRequest(params)
	if err != nil {
		return nil, fmt.Errorf("client: sign POST: %w", err)
	}
	body := encodeFormParams(signed)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", c.signer.APIKey())
	return c.doWithRetry(req, true)
}

// DeleteSigned executes a signed DELETE request.
func (c *HTTPClient) DeleteSigned(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	signed, err := c.signer.SignRequest(params)
	if err != nil {
		return nil, fmt.Errorf("client: sign DELETE: %w", err)
	}
	body := encodeFormParams(signed)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", c.signer.APIKey())
	return c.doWithRetry(req, true)
}

// PutSigned executes a signed PUT request.
func (c *HTTPClient) PutSigned(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	signed, err := c.signer.SignRequest(params)
	if err != nil {
		return nil, fmt.Errorf("client: sign PUT: %w", err)
	}
	body := encodeFormParams(signed)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", c.signer.APIKey())
	return c.doWithRetry(req, true)
}

func (c *HTTPClient) doWithRetry(req *http.Request, signed bool) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*attempt) * 500 * time.Millisecond
			c.log.Warn("retry request", zap.Int("attempt", attempt), zap.Duration("backoff", backoff))
			time.Sleep(backoff)
		}

		// Clone request body for retry
		var bodyBytes []byte
		if req.Body != nil {
			var err error
			bodyBytes, err = io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// --- Rate Limiting ---
		select {
		case <-c.rateLimit:
			// wait for ticker
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		// Try to parse as API error
		if resp.StatusCode != http.StatusOK {
			var apiErr APIError
			if jerr := json.Unmarshal(data, &apiErr); jerr == nil && apiErr.Code != 0 {
				// 429 / 418 — don't retry immediately, let backoff handle
				if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 418 {
					c.log.Error("rate limited by exchange",
						zap.Int("http_status", resp.StatusCode),
						zap.Int("api_code", apiErr.Code))
					lastErr = &apiErr
					// Longer sleep for rate limits
					time.Sleep(time.Duration(attempt+2) * 2 * time.Second)
					if bodyBytes != nil {
						req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
					}
					continue
				}
				// Signature errors: no point retrying with same key
				if apiErr.IsSignatureError() {
					return nil, &apiErr
				}
				return nil, &apiErr
			}
			return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(data))
		}

		return data, nil
	}
	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func (c *HTTPClient) buildURL(path string, params map[string]string) string {
	if len(params) == 0 {
		return c.base + path
	}
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	return c.base + path + "?" + q.Encode()
}

func encodeFormParams(params map[string]string) string {
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	return q.Encode()
}
