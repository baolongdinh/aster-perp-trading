package auth

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// V3Signer implements Aster API V3 authentication with EIP-712 signing
type V3Signer struct {
	UserWallet   string
	APISigner    string
	APISignerKey *ecdsa.PrivateKey
	RecvWindow   int64
	TimeOffset   int64 // server time - local time
}

// NewV3Signer creates a new V3 signer
func NewV3Signer(userWallet, apiSigner, apiSignerKey string, recvWindow int64) (*V3Signer, error) {
	// Validate user wallet address
	if !common.IsHexAddress(userWallet) {
		return nil, fmt.Errorf("invalid user wallet address format: %s", userWallet)
	}

	// Validate API signer address
	if !common.IsHexAddress(apiSigner) {
		return nil, fmt.Errorf("invalid API signer address format: %s", apiSigner)
	}

	// Normalize API signer private key hex encoding.
	key := strings.TrimSpace(apiSignerKey)
	if strings.HasPrefix(key, "0x") || strings.HasPrefix(key, "0X") {
		key = key[2:]
	}
	if len(key) != 64 {
		return nil, fmt.Errorf("invalid API signer private key length: expected 64 hex chars, got %d", len(key))
	}

	// Parse API signer private key
	privateKey, err := crypto.HexToECDSA(key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse API signer private key: %w", err)
	}

	if recvWindow <= 0 {
		recvWindow = 5000
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
	// Copy incoming params to avoid mutating caller data and handle nil maps.
	signedParams := make(map[string]string, len(params)+5)
	for k, v := range params {
		signedParams[k] = v
	}

	// Generate nonce (microseconds)
	nonce := time.Now().UnixNano() / 1000
	// Add timestamp milliseconds, adjusted by a small offset
	timestamp := time.Now().UnixMilli() + s.TimeOffset

	// Add required authentication parameters
	signedParams["user"] = s.UserWallet
	signedParams["signer"] = s.APISigner
	signedParams["nonce"] = fmt.Sprintf("%d", nonce)
	signedParams["timestamp"] = fmt.Sprintf("%d", timestamp)
	signedParams["recvWindow"] = fmt.Sprintf("%d", s.RecvWindow)

	// Create parameter string for signing
	paramString := s.createParamString(signedParams)

	// Generate EIP-712 signature
	signature, err := s.signEIP712(paramString)
	if err != nil {
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	signedParams["signature"] = signature
	return signedParams, nil
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
	// EIP-712 Domain
	domainName := "AsterSignTransaction"
	domainVersion := "1"
	domainChainId := big.NewInt(1666)
	domainVerifyingContract := common.HexToAddress("0x0000000000000000000000000000000000000000")

	// Type hashes
	typeHashDomain := crypto.Keccak256Hash([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	typeHashMessage := crypto.Keccak256Hash([]byte("Message(string msg)"))

	// Hash string values for domain separator
	nameHash := crypto.Keccak256Hash([]byte(domainName))
	versionHash := crypto.Keccak256Hash([]byte(domainVersion))

	// Domain separator: keccak256(abi.encode(typeHashDomain, nameHash, versionHash, chainId, verifyingContract))
	domainArgs := abi.Arguments{
		{Type: abi.Type{T: abi.FixedBytesTy, Size: 32}}, // typeHash (bytes32)
		{Type: abi.Type{T: abi.FixedBytesTy, Size: 32}}, // nameHash (bytes32)
		{Type: abi.Type{T: abi.FixedBytesTy, Size: 32}}, // versionHash (bytes32)
		{Type: abi.Type{T: abi.UintTy, Size: 256}},      // chainId
		{Type: abi.Type{T: abi.AddressTy}},              // verifyingContract
	}
	// Convert hashes to [32]byte arrays for packing
	typeHashBytes := [32]byte(typeHashDomain)
	nameHashBytes := [32]byte(nameHash)
	versionHashBytes := [32]byte(versionHash)

	domainData, err := domainArgs.Pack(typeHashBytes, nameHashBytes, versionHashBytes, domainChainId, domainVerifyingContract)
	if err != nil {
		return "", fmt.Errorf("failed to pack domain data: %w", err)
	}
	domainSeparator := crypto.Keccak256Hash(domainData)

	// Message hash: keccak256(abi.encode(typeHashMessage, msgHash))
	msgHash := crypto.Keccak256Hash([]byte(paramString))
	messageArgs := abi.Arguments{
		{Type: abi.Type{T: abi.FixedBytesTy, Size: 32}}, // typeHash (bytes32)
		{Type: abi.Type{T: abi.FixedBytesTy, Size: 32}}, // msgHash (bytes32)
	}
	// Convert hashes to [32]byte arrays for packing
	typeHashMessageBytes := [32]byte(typeHashMessage)
	msgHashBytes := [32]byte(msgHash)

	messageData, err := messageArgs.Pack(typeHashMessageBytes, msgHashBytes)
	if err != nil {
		return "", fmt.Errorf("failed to pack message data: %w", err)
	}
	messageHash := crypto.Keccak256Hash(messageData)

	// Final hash: keccak256(0x1901 + domainSeparator + messageHash)
	finalHash := crypto.Keccak256Hash(
		append(
			append([]byte{0x19, 0x01}, domainSeparator.Bytes()...),
			messageHash.Bytes()...,
		),
	)

	// Sign with API signer private key
	signature, err := crypto.Sign(finalHash.Bytes(), s.APISignerKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign hash: %w", err)
	}

	// Convert recovery id to Ethereum-style v value (27/28)
	if signature[64] < 27 {
		signature[64] += 27
	}

	// Return standard hex string without 0x prefix.
	return hex.EncodeToString(signature), nil
}

// SetTimeOffset sets the offset between exchange server time and local time.
func (s *V3Signer) SetTimeOffset(offset int64) {
	s.TimeOffset = offset
}

// GetTimestamp returns current timestamp in milliseconds.
func (s *V3Signer) GetTimestamp() int64 {
	return time.Now().UnixMilli() + s.TimeOffset
}

// GetNonce returns current nonce in microseconds.
func (s *V3Signer) GetNonce() int64 {
	return time.Now().UnixNano() / 1000
}
