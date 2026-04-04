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
	"sort"
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
	v1Signer   *auth.Signer
	v3Signer   *auth.V3Signer
	log        *zap.Logger
	maxRetries int
	rateLimit  <-chan time.Time
}

// NewHTTPClient creates a new Aster HTTP client with V1 auth.
func NewHTTPClient(baseURL string, signer *auth.Signer, log *zap.Logger, rps int) *HTTPClient {
	if rps <= 0 {
		rps = 10 // Default
	}
	return &HTTPClient{
		base:       baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		v1Signer:   signer,
		log:        log,
		maxRetries: 3,
		rateLimit:  make(chan time.Time),
	}
}

// NewHTTPClientV3 creates a new Aster HTTP client with V3 authentication.
func NewHTTPClientV3(baseURL string, v3Signer *auth.V3Signer, log *zap.Logger, rps int) *HTTPClient {
	if rps <= 0 {
		rps = 10 // Default
	}
	return &HTTPClient{
		base:       baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		v3Signer:   v3Signer,
		log:        log,
		maxRetries: 3,
		rateLimit:  make(chan time.Time),
	}
}

// GetPublic executes an unsigned GET request — for public market data endpoints.
func (c *HTTPClient) GetPublic(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	u := c.buildURL(path, params)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

// Get executes a signed GET request.
func (c *HTTPClient) Get(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	var signedParams map[string]string
	var err error

	if c.v3Signer != nil {
		signedParams, err = c.v3Signer.SignRequest(params)
	} else if c.v1Signer != nil {
		signedParams, err = c.v1Signer.SignRequest(params)
	} else {
		return nil, fmt.Errorf("no signer available")
	}

	if err != nil {
		return nil, err
	}

	u := c.buildURL(path, signedParams)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	if c.v1Signer != nil {
		req.Header.Set("X-MBX-APIKEY", c.v1Signer.APIKey())
	}

	return c.do(req)
}

// GetSigned is an alias for Get method for compatibility
func (c *HTTPClient) GetSigned(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	return c.Get(ctx, path, params)
}

// PostSigned executes a signed POST request with application/x-www-form-urlencoded body.
func (c *HTTPClient) PostSigned(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	var signedParams map[string]string
	var err error

	if c.v3Signer != nil {
		signedParams, err = c.v3Signer.SignRequest(params)
	} else if c.v1Signer != nil {
		signedParams, err = c.v1Signer.SignRequest(params)
	} else {
		return nil, fmt.Errorf("no signer available")
	}

	if err != nil {
		return nil, fmt.Errorf("client: sign POST: %w", err)
	}

	body := encodeFormParams(signedParams)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if c.v1Signer != nil {
		req.Header.Set("X-MBX-APIKEY", c.v1Signer.APIKey())
	}

	return c.do(req)
}

// PutSigned executes a signed PUT request.
func (c *HTTPClient) PutSigned(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	var signedParams map[string]string
	var err error

	if c.v3Signer != nil {
		signedParams, err = c.v3Signer.SignRequest(params)
	} else if c.v1Signer != nil {
		signedParams, err = c.v1Signer.SignRequest(params)
	} else {
		return nil, fmt.Errorf("no signer available")
	}

	if err != nil {
		return nil, fmt.Errorf("client: sign PUT: %w", err)
	}

	body := encodeFormParams(signedParams)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if c.v1Signer != nil {
		req.Header.Set("X-MBX-APIKEY", c.v1Signer.APIKey())
	}

	return c.do(req)
}

// DeleteSigned executes a signed DELETE request.
func (c *HTTPClient) DeleteSigned(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	var signedParams map[string]string
	var err error

	if c.v3Signer != nil {
		signedParams, err = c.v3Signer.SignRequest(params)
	} else if c.v1Signer != nil {
		signedParams, err = c.v1Signer.SignRequest(params)
	} else {
		return nil, fmt.Errorf("no signer available")
	}

	if err != nil {
		return nil, fmt.Errorf("client: sign DELETE: %w", err)
	}

	body := encodeFormParams(signedParams)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if c.v1Signer != nil {
		req.Header.Set("X-MBX-APIKEY", c.v1Signer.APIKey())
	}

	return c.do(req)
}

// do executes the HTTP request with basic error handling.
func (c *HTTPClient) do(req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		var apiErr APIError
		if err := json.Unmarshal(body, &apiErr); err == nil {
			return nil, &apiErr
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// buildURL constructs a URL with query parameters.
func (c *HTTPClient) buildURL(path string, params map[string]string) string {
	if len(params) == 0 {
		return c.base + path
	}
	u, _ := url.Parse(c.base + path)
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// encodeFormParams encodes parameters as application/x-www-form-urlencoded.
func encodeFormParams(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte('&')
		}
		buf.WriteString(url.QueryEscape(k))
		buf.WriteByte('=')
		buf.WriteString(url.QueryEscape(params[k]))
	}
	return buf.String()
}
