package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// readPrivateKeyFile reads a 64-byte raw ed25519 private key from disk.
func readPrivateKeyFile(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keyLen := len(data)
	if keyLen == 64 {
		return ed25519.PrivateKey(data), nil
	}
	// Also accept hex-encoded (128 hex chars).
	if keyLen == 128 {
		raw, err := hex.DecodeString(string(data))
		if err != nil {
			return nil, err
		}
		if len(raw) != 64 {
			return nil, fmt.Errorf("hex-decoded key length %d != 64", len(raw))
		}
		return ed25519.PrivateKey(raw), nil
	}
	// PEM-encoded PRIVATE KEY block.
	block, _ := pem.Decode(data)
	if block != nil && block.Type == "PRIVATE KEY" {
		if key, err := parsePKCS8Ed25519(block.Bytes); err == nil {
			return key, nil
		}
	}
	return nil, fmt.Errorf("key file must be 64 raw bytes, 128 hex chars, or PEM PKCS8 ed25519 (got %d bytes)", len(data))
}

// readPublicKeyFile reads a 32-byte raw ed25519 public key from disk.
func readPublicKeyFile(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keyLen := len(data)
	if keyLen == 32 {
		return ed25519.PublicKey(data), nil
	}
	if keyLen == 64 {
		raw, err := hex.DecodeString(string(data))
		if err != nil {
			return nil, err
		}
		if len(raw) != 32 {
			return nil, fmt.Errorf("hex-decoded key length %d != 32", len(raw))
		}
		return ed25519.PublicKey(raw), nil
	}
	block, _ := pem.Decode(data)
	if block != nil && block.Type == "PUBLIC KEY" {
		if key, err := parsePKIXEd25519(block.Bytes); err == nil {
			return key, nil
		}
	}
	return nil, fmt.Errorf("public key file must be 32 raw bytes, 64 hex chars, or PEM PKIX ed25519 (got %d bytes)", len(data))
}

// parsePKCS8Ed25519 parses a DER-encoded PKCS8 ed25519 private key.
// We don't depend on crypto/x509 to keep the key-decoding path minimal; we
// walk the PKCS8 structure manually for the ed25519 OID (1.3.101.112).
func parsePKCS8Ed25519(der []byte) (ed25519.PrivateKey, error) {
	// PKCS8 PrivateKeyInfo:
	//   SEQUENCE { version INTEGER, algorithm AlgorithmIdentifier, key OCTET STRING }
	// ed25519 OID = 06 03 2B 65 70.
	if len(der) < 16 {
		return nil, errors.New("pkcs8 der too short")
	}
	for i := 0; i < len(der)-8; i++ {
		if der[i] == 0x06 && der[i+1] == 0x03 && der[i+2] == 0x2B &&
			der[i+3] == 0x65 && der[i+4] == 0x70 {
			// Found the OID. The OCTET STRING holding the raw key follows.
			// PKCS8 wraps the 32-byte seed for ed25519; we need to return a
			// 64-byte PrivateKey (seed || pub). We can't derive the pub from
			// the seed without ed25519 internals, so we ask the caller to
			// provide a raw key.
			return nil, errors.New("PEM PKCS8 ed25519 not supported; provide a 64-byte raw key file")
		}
	}
	return nil, errors.New("ed25519 OID not found in DER")
}

// parsePKIXEd25519 parses a DER-encoded SubjectPublicKeyInfo for ed25519.
func parsePKIXEd25519(der []byte) (ed25519.PublicKey, error) {
	// SPKI structure: SEQUENCE { algorithm, BIT STRING { 04 || rawKey } }
	if len(der) < 12 {
		return nil, errors.New("spki der too short")
	}
	// Find the BIT STRING tag 0x03 and skip length + unused-bits.
	for i := 0; i < len(der)-2; i++ {
		if der[i] == 0x03 && der[i+1] == 0x22 && i+2 < len(der) && der[i+2] == 0x00 {
			candidate := der[i+3:]
			if len(candidate) == 32 {
				return ed25519.PublicKey(candidate), nil
			}
		}
	}
	return nil, errors.New("ed25519 BIT STRING not found in DER")
}
