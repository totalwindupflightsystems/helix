package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultContractsDir  = ".helix/contracts"
	DefaultConsumersFile = "consumers.yaml"
)

// ContractStore persists contracts as JSON files.
type ContractStore struct {
	root string
}

// NewContractStore creates a store rooted at root.
func NewContractStore(root string) (*ContractStore, error) {
	expanded, err := resolveStoreRoot(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(expanded, 0o755); err != nil {
		return nil, fmt.Errorf("contract: mkdir %s: %w", expanded, err)
	}
	return &ContractStore{root: expanded}, nil
}

// Root returns the absolute store root.
func (s *ContractStore) Root() string { return s.root }

// Save writes the contract as JSON. Rejects mutations to frozen contracts.
func (s *ContractStore) Save(c *Contract) error {
	if c == nil {
		return fmt.Errorf("contract: contract is nil")
	}
	if c.ID == "" {
		return fmt.Errorf("contract: id is required")
	}
	if !ValidFormat(c.Format) {
		return fmt.Errorf("contract: invalid format %q", c.Format)
	}

	// Frozen-contract guard.
	existing, err := s.Load(c.ID)
	if err == nil && existing.IsFrozen() {
		if existing.Hash != c.Hash || existing.Version != c.Version {
			return fmt.Errorf("contract: %s is frozen; bump Version to publish a new revision", c.ID)
		}
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("contract: marshal: %w", err)
	}
	path := filepath.Join(s.root, c.ID+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("contract: write: %w", err)
	}
	return nil
}

// Load reads a contract by ID.
func (s *ContractStore) Load(id string) (*Contract, error) {
	path := filepath.Join(s.root, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("contract: %s not found", id)
		}
		return nil, fmt.Errorf("contract: read: %w", err)
	}
	var c Contract
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("contract: unmarshal: %w", err)
	}
	return &c, nil
}

// List returns all contract IDs.
func (s *ContractStore) List() ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("contract: read dir: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".json") && e.Name() != DefaultConsumersFile {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// LoadPrevious loads version N-1 of the contract.
func (s *ContractStore) LoadPrevious(id string) (*Contract, error) {
	current, err := s.Load(id)
	if err != nil {
		return nil, err
	}
	if current.Version <= 1 {
		return nil, fmt.Errorf("contract: no previous version for %s", id)
	}
	// Try to load from numbered version filenames.
	for v := current.Version - 1; v >= 1; v-- {
		prevID := fmt.Sprintf("%s-v%d", id, v)
		if c, err := s.Load(prevID); err == nil {
			return c, nil
		}
	}
	return nil, fmt.Errorf("contract: no previous version found for %s", id)
}

// LoadConsumerCatalog reads the consumer catalog.
func (s *ContractStore) LoadConsumerCatalog() (map[string][]string, error) {
	path := filepath.Join(s.root, DefaultConsumersFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string][]string), nil
		}
		return nil, fmt.Errorf("contract: read consumers: %w", err)
	}
	var catalog map[string][]string
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("contract: unmarshal consumers: %w", err)
	}
	if catalog == nil {
		catalog = make(map[string][]string)
	}
	return catalog, nil
}

// SaveConsumerCatalog writes the consumer catalog.
func (s *ContractStore) SaveConsumerCatalog(catalog map[string][]string) error {
	path := filepath.Join(s.root, DefaultConsumersFile)
	data, err := yaml.Marshal(catalog)
	if err != nil {
		return fmt.Errorf("contract: marshal consumers: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("contract: write consumers: %w", err)
	}
	return nil
}

func resolveStoreRoot(root string) (string, error) {
	if root != "" {
		return root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("contract: home dir: %w", err)
	}
	return filepath.Join(home, DefaultContractsDir), nil
}
