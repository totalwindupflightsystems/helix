package prompt

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Verbose enables verbose logging to stderr when true (set from CLI --verbose).
var Verbose bool

// RegistryDir is the root directory for prompt storage (spec §3.1).
var RegistryDir = "prompts"

// RegisterOptions holds optional parameters for Register.
type RegisterOptions struct {
	DryRun   bool
	Author   string
	WorkItem string
}

// ListFilter holds filter criteria for List. Empty fields match all.
type ListFilter struct {
	Component string
	Status    LifecycleStatus
	Model     string
}

// Register reads a prompt file, computes its hash, creates the prompt
// directory structure, writes prompt.md + metadata.yaml, and updates the
// registry index. Returns the resolved PromptVersion (spec §18).
func Register(component, version, promptFilePath, model, provider, specRef string, opts *RegisterOptions) (*PromptVersion, error) {
	if opts == nil {
		opts = &RegisterOptions{}
	}

	// Read prompt content
	content, err := os.ReadFile(promptFilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read prompt file %s: %w", promptFilePath, err)
	}

	// Compute content-addressed hash
	hash := Hash(string(content))

	// Build paths
	dir := filepath.Join(RegistryDir, component, version)
	promptPath := filepath.Join(dir, "prompt.md")
	metadataPath := filepath.Join(dir, "metadata.yaml")

	if opts.DryRun {
		if Verbose {
			fmt.Fprintf(os.Stderr, "[INFO] DRY RUN: would register %s/%s (hash=%s)\n", component, version, hash)
		}
		return &PromptVersion{
			Version:      version,
			Component:    component,
			Hash:         hash,
			Status:       StatusDraft,
			Model:        model,
			Provider:     provider,
			PromptPath:   promptPath,
			MetadataPath: metadataPath,
		}, nil
	}

	// Create directory structure
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create prompt directory: %w", err)
	}

	// Write prompt.md
	if err := writePrompt(promptPath, string(content)); err != nil {
		return nil, err
	}

	// Build and write metadata.yaml
	now := time.Now().UTC()
	meta := &Metadata{
		Version:   version,
		Component: component,
		Hash:      hash,
		Model:     model,
		Provider:  provider,
		Author:    opts.Author,
		SpecRef:   specRef,
		WorkItem:  opts.WorkItem,
		CreatedAt: now,
		Status:    StatusDraft,
	}
	if err := writeMetadata(metadataPath, meta); err != nil {
		return nil, err
	}

	// Update index
	idx, err := loadIndex()
	if err != nil {
		return nil, err
	}
	if *idx == nil {
		empty := Index{}
		idx = &empty
	}
	if (*idx)[component] == nil {
		(*idx)[component] = map[string]*PromptEntry{}
	}
	(*idx)[component][version] = &PromptEntry{
		Hash:     hash,
		Status:   StatusDraft,
		Model:    model,
		Provider: provider,
	}
	if err := saveIndex(idx); err != nil {
		return nil, err
	}

	if Verbose {
		fmt.Fprintf(os.Stderr, "[PASS] registered %s/%s (hash=%s)\n", component, version, hash)
	}

	return &PromptVersion{
		Version:      version,
		Component:    component,
		Hash:         hash,
		Status:       StatusDraft,
		Model:        model,
		Provider:     provider,
		PromptPath:   promptPath,
		MetadataPath: metadataPath,
	}, nil
}

// Lookup finds a prompt version by its content hash (spec §3.3).
func Lookup(hash string) (*PromptVersion, error) {
	idx, err := loadIndex()
	if err != nil {
		return nil, err
	}
	for component, versions := range *idx {
		for version, entry := range versions {
			if entry.Hash == hash {
				return entryToPromptVersion(component, version, entry), nil
			}
		}
	}
	return nil, fmt.Errorf("PROMPT_NOT_FOUND: sha256:%s not in registry", hash)
}

// LookupByComponent finds a prompt version by component and version string.
func LookupByComponent(component, version string) (*PromptVersion, error) {
	idx, err := loadIndex()
	if err != nil {
		return nil, err
	}
	if (*idx)[component] == nil || (*idx)[component][version] == nil {
		return nil, fmt.Errorf("PROMPT_NOT_FOUND: %s/%s not in registry", component, version)
	}
	return entryToPromptVersion(component, version, (*idx)[component][version]), nil
}

// List returns all prompt versions matching the given filter.
func List(filter ListFilter) ([]*PromptVersion, error) {
	idx, err := loadIndex()
	if err != nil {
		return nil, err
	}
	result := []*PromptVersion{}
	for component, versions := range *idx {
		if filter.Component != "" && component != filter.Component {
			continue
		}
		for version, entry := range versions {
			if filter.Status != "" && entry.Status != filter.Status {
				continue
			}
			if filter.Model != "" && entry.Model != filter.Model {
				continue
			}
			result = append(result, entryToPromptVersion(component, version, entry))
		}
	}
	return result, nil
}

// UpdateStatus changes the lifecycle status of a prompt version and persists
// the change to both the index and metadata.yaml.
func UpdateStatus(component, version string, newStatus LifecycleStatus) error {
	idx, err := loadIndex()
	if err != nil {
		return err
	}
	if (*idx)[component] == nil || (*idx)[component][version] == nil {
		return fmt.Errorf("PROMPT_NOT_FOUND: %s/%s not in registry", component, version)
	}

	(*idx)[component][version].Status = newStatus
	if err := saveIndex(idx); err != nil {
		return err
	}

	// Sync metadata.yaml
	metaPath := filepath.Join(RegistryDir, component, version, "metadata.yaml")
	if meta, err := readMetadata(metaPath); err == nil {
		meta.Status = newStatus
		if newStatus == StatusAttested && meta.AttestedAt.IsZero() {
			meta.AttestedAt = time.Now().UTC()
		}
		_ = writeMetadata(metaPath, meta)
	}

	if Verbose {
		fmt.Fprintf(os.Stderr, "[INFO] status updated: %s/%s → %s\n", component, version, newStatus)
	}
	return nil
}

// TransitionStatus validates and executes a lifecycle transition, logging it
// to the audit trail (spec §7, §15).
func TransitionStatus(component, version string, to LifecycleStatus, trigger string) error {
	idx, err := loadIndex()
	if err != nil {
		return err
	}
	if (*idx)[component] == nil || (*idx)[component][version] == nil {
		return fmt.Errorf("PROMPT_NOT_FOUND: %s/%s not in registry", component, version)
	}
	from := (*idx)[component][version].Status

	// Load metadata for transition validation (rollback check)
	metaPath := filepath.Join(RegistryDir, component, version, "metadata.yaml")
	meta, _ := readMetadata(metaPath)

	if err := ValidateTransition(from, to, meta); err != nil {
		return err
	}

	if err := UpdateStatus(component, version, to); err != nil {
		return err
	}

	if Verbose {
		fmt.Fprintf(os.Stderr, "[INFO] transition: %s/%s %s → %s (trigger: %s)\n",
			component, version, from, to, trigger)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Index persistence
// ---------------------------------------------------------------------------

// loadIndex reads the registry index from prompts/_index.yaml. Returns an
// empty Index if the file does not exist (fresh repo).
func loadIndex() (*Index, error) {
	idx := Index{}
	path := filepath.Join(RegistryDir, "_index.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &idx, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return &idx, nil
	}
	if err := yaml.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse _index.yaml: %w", err)
	}
	return &idx, nil
}

// saveIndex writes the registry index to prompts/_index.yaml, creating the
// directory if needed.
func saveIndex(index *Index) error {
	if index == nil {
		empty := Index{}
		index = &empty
	}
	data, err := yaml.Marshal(index)
	if err != nil {
		return err
	}
	path := filepath.Join(RegistryDir, "_index.yaml")
	if err := os.MkdirAll(RegistryDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// writeMetadata marshals a Metadata struct to YAML and writes it to path.
func writeMetadata(path string, m *Metadata) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// readMetadata reads and unmarshals a metadata.yaml file.
func readMetadata(path string) (*Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Metadata
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// writePrompt writes raw prompt content to a file path.
func writePrompt(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// entryToPromptVersion builds a PromptVersion from an index entry.
func entryToPromptVersion(component, version string, entry *PromptEntry) *PromptVersion {
	return &PromptVersion{
		Version:      version,
		Component:    component,
		Hash:         entry.Hash,
		Status:       entry.Status,
		Model:        entry.Model,
		Provider:     entry.Provider,
		PromptPath:   filepath.Join(RegistryDir, component, version, "prompt.md"),
		MetadataPath: filepath.Join(RegistryDir, component, version, "metadata.yaml"),
	}
}
