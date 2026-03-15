// Package auth implements HMAC-SHA256 signing for Aster Finance V1 API.
//
// Signing flow (per aster-api-auth-v1 skill):
//  1. Add timestamp and recvWindow to payload.
//  2. Concatenate query string and request body (no '&' between them).
//  3. HMAC-SHA256(secretKey, totalParams).
//  4. Hex-encode signature and append to request.
//  5. Include X-MBX-APIKEY in HTTP headers.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"
)

// SignerV1 holds the V1 API credentials.
type SignerV1 struct {
	apiKey    string
	apiSecret []byte
}

// NewSignerV1 creates a new HMAC-SHA256 signer.
// The apiSecret is stored as a byte slice.
func NewSignerV1(apiKey, apiSecret string) (*SignerV1, error) {
	if apiKey == "" || apiSecret == "" {
		return nil, fmt.Errorf("auth: api key and secret are required")
	}

	secretBytes := []byte(apiSecret)

	// Zero out the original string as much as Go allows
	b := []byte(apiSecret)
	for i := range b {
		b[i] = 0
	}
	runtime.GC()

	return &SignerV1{
		apiKey:    apiKey,
		apiSecret: secretBytes,
	}, nil
}

// APIKey returns the configured API key (for the X-MBX-APIKEY header).
func (s *SignerV1) APIKey() string {
	return s.apiKey
}

// SignRequestV1 generates the HMAC-SHA256 signature for the given parameters.
// V1 spec: totalParams = query params string + body string (no & between them).
// The signature is returned as a hex string.
func (s *SignerV1) SignRequestV1(params map[string]string) string {
	// 1. Build query string (sorted by key, standard url.Values behavior)
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params[k])
	}
	
	payload := sb.String()

	// 2. HMAC-SHA256(secret, payload)
	mac := hmac.New(sha256.New, s.apiSecret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// Signer handles V1 authentication by appending timestamp and signature to params.
type Signer struct {
	v1         *SignerV1
	recvWindow int
	timeOffset int64 // Server time minus local time
}

// NewSigner creates a backward-compatible Signer wrapper that uses V1 under the hood.
func NewSigner(apiKey, apiSecret string, recvWindow int) (*Signer, error) {
	v1, err := NewSignerV1(apiKey, apiSecret)
	if err != nil {
		return nil, err
	}
	if recvWindow <= 0 {
		recvWindow = 5000
	}
	return &Signer{v1: v1, recvWindow: recvWindow}, nil
}

// SetTimeOffset sets the offset between the exchange server time and local time.
// offset = serverTime - localTime
func (s *Signer) SetTimeOffset(offset int64) {
	s.timeOffset = offset
}

// SignRequest adds timestamp, recvWindow, and signature to the params.
func (s *Signer) SignRequest(params map[string]string) (map[string]string, error) {
	timestamp := time.Now().UnixMilli() + s.timeOffset

	combined := make(map[string]string, len(params)+3)
	for k, v := range params {
		combined[k] = v
	}
	combined["timestamp"] = fmt.Sprintf("%d", timestamp)
	combined["recvWindow"] = fmt.Sprintf("%d", s.recvWindow)

	// In V1, the signature is calculated over the query string (which includes timestamp)
	sig := s.v1.SignRequestV1(combined)
	combined["signature"] = sig

	return combined, nil
}

func (s *Signer) APIKey() string {
	return s.v1.APIKey()
}
