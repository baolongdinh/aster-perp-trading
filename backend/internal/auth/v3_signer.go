package auth

import (
	"crypto/ecdsa"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// V3Signer implements Aster API V3 authentication with EIP-712 signing
type V3Signer struct {
	UserWallet   string
	APISigner    string
	APISignerKey *ecdsa.PrivateKey
	RecvWindow   int64
}

// NewV3Signer creates a new V3 signer
func NewV3Signer(userWallet, apiSigner, apiSignerKey string, recvWindow int64) (*V3Signer, error) {
	// Parse API signer private key
	privateKey, err := crypto.HexToECDSA(apiSignerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse API signer private key: %w", err)
	}

	return &V3Signer{
		UserWallet:   userWallet,
		APISigner:    apiSigner,
		APISignerKey: privateKey,
		RecvWindow:   recvWindow,
	}, nil
}

// SignRequest signs a request according to Aster API V3 specification
func (s *V3Signer) SignRequest(params map[string]string) (map[string]string, error) {
	// Generate nonce (microseconds)
	nonce := time.Now().UnixNano() / 1000

	// Add required authentication parameters
	params["user"] = s.UserWallet
	params["signer"] = s.APISigner
	params["nonce"] = fmt.Sprintf("%d", nonce)

	// Create parameter string for signing
	paramString := s.createParamString(params)

	// Generate EIP-712 signature
	signature, err := s.signEIP712(paramString)
	if err != nil {
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	params["signature"] = signature
	return params, nil
}

// createParamString creates the parameter string for signing
func (s *V3Signer) createParamString(params map[string]string) string {
	// Sort keys by ASCII order
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build parameter string
	var parts []string
	for _, key := range keys {
		value := params[key]
		// Convert all values to strings
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}

	return strings.Join(parts, "&")
}

// signEIP712 creates an EIP-712 signature for the parameter string
func (s *V3Signer) signEIP712(paramString string) (string, error) {
	// Simplified EIP-712-like signing for Aster API
	// Create message hash
	messageHash := crypto.Keccak256Hash([]byte(paramString))

	// Create domain separator
	domainSeparator := crypto.Keccak256Hash([]byte("AsterSignTransaction1"))

	// Create final hash: keccak256(0x1901 + domainSeparator + messageHash)
	finalHash := crypto.Keccak256Hash(
		append(
			append([]byte{0x19, 0x01}, domainSeparator[:]...),
			messageHash[:]...,
		),
	)

	// Sign with API signer private key
	signature, err := crypto.Sign(finalHash.Bytes(), s.APISignerKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign hash: %w", err)
	}

	// Convert to hex string
	return fmt.Sprintf("0x%x", signature), nil
}

// GetTimestamp returns current timestamp in milliseconds
func (s *V3Signer) GetTimestamp() int64 {
	return time.Now().UnixMilli()
}

// GetNonce returns current nonce in microseconds
func (s *V3Signer) GetNonce() int64 {
	return time.Now().UnixNano() / 1000
}
