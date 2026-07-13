package contract

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ContractAuthor generates API contracts from spec documents.
type ContractAuthor struct {
	specDir string
}

// NewContractAuthor creates a ContractAuthor that reads specs from specDir.
func NewContractAuthor(specDir string) (*ContractAuthor, error) {
	d, err := resolveSpecDir(specDir)
	if err != nil {
		return nil, err
	}
	return &ContractAuthor{specDir: d}, nil
}

// Generate produces a Contract from specRef using the requested format.
func (a *ContractAuthor) Generate(ctx context.Context, specRef string, format ContractFormat) (*Contract, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if specRef == "" {
		return nil, errors.New("contract: spec ref is required")
	}
	if !ValidFormat(format) {
		return nil, fmt.Errorf("contract: unsupported format %q", format)
	}

	schema := generateSchema(format, specRef)
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("contract: marshal schema: %w", err)
	}

	return &Contract{
		ID:        contractID(specRef, format),
		SpecRef:   specRef,
		Format:    format,
		Schema:    raw,
		Version:   1,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func generateSchema(format ContractFormat, specRef string) interface{} {
	switch format {
	case FormatOpenAPI:
		return map[string]interface{}{
			"openapi": "3.0.3",
			"info": map[string]interface{}{
				"title":       specRef + " API",
				"version":     "1.0.0",
				"description": "Generated contract for spec " + specRef,
			},
			"paths":      map[string]interface{}{},
			"components": map[string]interface{}{},
		}
	case FormatProtobuf:
		return map[string]interface{}{
			"syntax":   "proto3",
			"package":  specRef,
			"services": []interface{}{},
			"messages": []interface{}{},
		}
	case FormatGraphQL:
		return map[string]interface{}{
			"schema": map[string]interface{}{
				"query":        "Query",
				"mutation":     "Mutation",
				"subscription": "Subscription",
			},
			"types": []interface{}{},
		}
	}
	return nil
}

func resolveSpecDir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("contract: home dir: %w", err)
	}
	return filepath.Join(home, ".helix", "specs"), nil
}

func contractID(specRef string, format ContractFormat) string {
	return specRef + "-" + string(format)
}

// ComputeHash returns the SHA-256 hex digest of the canonicalized schema.
func ComputeHash(c *Contract) string {
	var v interface{}
	if err := json.Unmarshal(c.Schema, &v); err != nil {
		h := sha256.Sum256(c.Schema)
		return fmt.Sprintf("%x", h)
	}
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h)
}

// Freeze hashes and marks a contract as immutable.
func Freeze(c *Contract) error {
	if c.IsFrozen() {
		return fmt.Errorf("contract: %s is already frozen", c.ID)
	}
	c.Hash = ComputeHash(c)
	c.FrozenAt = time.Now().UTC()
	return nil
}

// SpecMeta holds YAML frontmatter from spec markdown files.
type SpecMeta struct {
	Title       string   `yaml:"title"`
	Status      string   `yaml:"status"`
	Endpoints   []string `yaml:"endpoints,omitempty"`
	ADRRefs     []string `yaml:"adr_refs,omitempty"`
	Description string   `yaml:"description,omitempty"`
}
