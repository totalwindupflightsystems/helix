package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// readPrivateKeyFile reads an ed25519 private key from disk. It accepts a
// 64-byte raw key, 128 hex chars, or a PEM PKCS8 ed25519 block.
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
//
// We don't depend on crypto/x509 to keep the key-decoding path minimal; we
// walk the PKCS8 structure manually for the ed25519 OID (1.3.101.112).
//
// RFC 8410 layout (as produced by openssl genpkey and Go's
// x509.MarshalPKCS8PrivateKey):
//
//	PrivateKeyInfo ::= SEQUENCE {
//	    version         INTEGER,             -- 02 01 00
//	    algorithm       AlgorithmIdentifier, -- SEQUENCE { OID 1.3.101.112 [, NULL] }
//	    privateKey      OCTET STRING,        -- wraps the inner OCTET STRING
//	}
//
// The outer privateKey OCTET STRING contains an inner OCTET STRING holding
// the 32-byte seed. The full 64-byte PrivateKey is seed || pub, which
// crypto/ed25519.NewKeyFromSeed derives directly from the seed.
func parsePKCS8Ed25519(der []byte) (ed25519.PrivateKey, error) {
	// ed25519 OID = 06 03 2B 65 70 (1.3.101.112).
	oidStart := -1
	for i := 0; i <= len(der)-5; i++ {
		if der[i] == 0x06 && der[i+1] == 0x03 && der[i+2] == 0x2B &&
			der[i+3] == 0x65 && der[i+4] == 0x70 {
			oidStart = i
			break
		}
	}
	if oidStart < 0 {
		return nil, errors.New("ed25519 OID not found in PKCS8 DER")
	}
	rest := der[oidStart+5:]
	// Skip optional NULL parameters (some producers emit 05 00 after the OID).
	if len(rest) >= 2 && rest[0] == 0x05 && rest[1] == 0x00 {
		rest = rest[2:]
	}
	// Outer OCTET STRING wrapping the inner seed OCTET STRING.
	if len(rest) < 2 || rest[0] != 0x04 {
		return nil, errors.New("pkcs8: expected OCTET STRING after ed25519 algorithm identifier")
	}
	outerLen, n, err := derLength(rest[1:])
	if err != nil {
		return nil, fmt.Errorf("pkcs8: outer OCTET STRING: %w", err)
	}
	if 1+n+outerLen > len(rest) {
		return nil, errors.New("pkcs8: truncated outer OCTET STRING")
	}
	inner := rest[1+n : 1+n+outerLen]
	// Inner OCTET STRING holding the 32-byte seed.
	if len(inner) < 2 || inner[0] != 0x04 {
		return nil, errors.New("pkcs8: expected inner OCTET STRING with ed25519 seed")
	}
	seedLen, m, err := derLength(inner[1:])
	if err != nil {
		return nil, fmt.Errorf("pkcs8: inner OCTET STRING: %w", err)
	}
	if seedLen != ed25519.SeedSize {
		return nil, fmt.Errorf("pkcs8: ed25519 seed length %d, want %d", seedLen, ed25519.SeedSize)
	}
	if m+seedLen > len(inner) {
		return nil, errors.New("pkcs8: truncated inner OCTET STRING")
	}
	return ed25519.NewKeyFromSeed(inner[1+m : 1+m+seedLen]), nil
}

// derLength decodes a DER length field (short or long form) from b and
// returns the length value and the number of bytes it occupied.
func derLength(b []byte) (int, int, error) {
	if len(b) == 0 {
		return 0, 0, errors.New("missing DER length")
	}
	first := b[0]
	if first < 0x80 {
		return int(first), 1, nil
	}
	num := int(first & 0x7F)
	if num == 0 || num > 4 || len(b) < 1+num {
		return 0, 0, errors.New("invalid long-form DER length")
	}
	length := 0
	for _, c := range b[1 : 1+num] {
		length = length<<8 | int(c)
	}
	return length, 1 + num, nil
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
