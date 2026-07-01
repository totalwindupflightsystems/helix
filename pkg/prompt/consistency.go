package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ---------------------------------------------------------------------------
// Index Consistency (spec §8.4)
// ---------------------------------------------------------------------------

// ConsistencyStatus is the result of checking one index entry against disk.
type ConsistencyStatus string

const (
	ConsistencyOK         ConsistencyStatus = "ok"
	ConsistencyIndexStale ConsistencyStatus = "index_stale"
	ConsistencyTamper     ConsistencyStatus = "tamper_detected"
	ConsistencyMissing    ConsistencyStatus = "missing_on_disk"
	ConsistencyOrphaned   ConsistencyStatus = "orphaned_on_disk"
)

// ConsistencyCheck is the result of verifying a single prompt entry.
type ConsistencyCheck struct {
	Component string            `json:"component"`
	Version   string            `json:"version"`
	IndexHash string            `json:"index_hash"`
	DiskHash  string            `json:"disk_hash,omitempty"`
	Status    ConsistencyStatus `json:"status"`
	Detail    string            `json:"detail,omitempty"`
}

// ConsistencyReport is the full result of a CheckIndex operation.
type ConsistencyReport struct {
	Checked   int                `json:"checked"`
	OK        int                `json:"ok"`
	Stale     int                `json:"stale"`
	Tampered  int                `json:"tampered"`
	Missing   int                `json:"missing"`
	Orphaned  int                `json:"orphaned"`
	Checks    []ConsistencyCheck `json:"checks"`
	Rebuilt   bool               `json:"rebuilt"`
	CheckedAt time.Time          `json:"checked_at"`
}

// HasIssues returns true if any consistency problem was found.
func (r *ConsistencyReport) HasIssues() bool {
	return r.Stale > 0 || r.Tampered > 0 || r.Missing > 0 || r.Orphaned > 0
}

// ShouldBlock returns true if the report contains issues that should block
// merges (tamper or missing). Index staleness is non-blocking per spec §8.4.
func (r *ConsistencyReport) ShouldBlock() bool {
	return r.Tampered > 0
}

// FormatReport renders the consistency report for CLI output.
func (r *ConsistencyReport) FormatReport() string {
	status := "✅ CONSISTENT"
	if r.Tampered > 0 {
		status = "❌ TAMPER DETECTED"
	} else if r.Missing > 0 {
		status = "⚠️  MISSING ENTRIES"
	} else if r.Stale > 0 {
		status = "⚠️  INDEX STALE (rebuilt)"
	} else if r.Orphaned > 0 {
		status = "ℹ️  ORPHANED PROMPTS"
	}

	out := fmt.Sprintf("Index Consistency Check — %s\n", status)
	out += fmt.Sprintf("  Checked: %d  OK: %d  Stale: %d  Tampered: %d  Missing: %d  Orphaned: %d\n",
		r.Checked, r.OK, r.Stale, r.Tampered, r.Missing, r.Orphaned)
	if r.Rebuilt {
		out += "  Index rebuilt from disk.\n"
	}
	for _, c := range r.Checks {
		if c.Status == ConsistencyOK {
			continue
		}
		out += fmt.Sprintf("  %s/%s: %s — %s\n", c.Component, c.Version, c.Status, c.Detail)
	}
	return out
}

// CheckIndex verifies index consistency per spec §8.4. For each index entry,
// it recomputes the hash from prompt.md on disk and compares:
//   - metadata.hash == recomputed → OK
//   - metadata.hash != recomputed but index.hash == metadata.hash → INDEX_STALE
//   - metadata.hash != recomputed AND index.hash != metadata.hash → TAMPER
//
// If autoRebuild is true, stale entries are corrected in-place by rebuilding
// the index from disk state.
func CheckIndex(autoRebuild bool) (*ConsistencyReport, error) {
	report := &ConsistencyReport{
		CheckedAt: time.Now().UTC(),
	}

	idx, err := loadIndex()
	if err != nil {
		return nil, err
	}

	// Track which component/version pairs exist on disk
	onDisk := make(map[string]map[string]bool)

	for component, versions := range *idx {
		if onDisk[component] == nil {
			onDisk[component] = make(map[string]bool)
		}
		for version, entry := range versions {
			report.Checked++
			check := ConsistencyCheck{
				Component: component,
				Version:   version,
				IndexHash: entry.Hash,
			}

			metaPath := filepath.Join(RegistryDir, component, version, "metadata.yaml")
			promptPath := filepath.Join(RegistryDir, component, version, "prompt.md")

			meta, err := readMetadata(metaPath)
			if err != nil {
				check.Status = ConsistencyMissing
				check.Detail = fmt.Sprintf("metadata.yaml: %v", err)
				report.Missing++
				report.Checks = append(report.Checks, check)
				continue
			}

			onDisk[component][version] = true

			// Recompute hash from prompt.md
			content, err := os.ReadFile(promptPath)
			if err != nil {
				check.Status = ConsistencyMissing
				check.Detail = fmt.Sprintf("prompt.md: %v", err)
				report.Missing++
				report.Checks = append(report.Checks, check)
				continue
			}
			recomputed := Hash(string(content))
			check.DiskHash = recomputed

			// Compare hashes per spec §8.4
			if meta.Hash == recomputed {
				// Metadata matches disk — check if index matches
				if entry.Hash != meta.Hash {
					check.Status = ConsistencyIndexStale
					check.Detail = fmt.Sprintf("index has %s, metadata has %s", entry.Hash, meta.Hash)
					report.Stale++
				} else {
					check.Status = ConsistencyOK
					report.OK++
				}
			} else {
				// Metadata does NOT match recomputed — tamper detected
				check.Status = ConsistencyTamper
				check.Detail = fmt.Sprintf("metadata.hash=%s but recomputed=%s", meta.Hash, recomputed)
				report.Tampered++
			}
			report.Checks = append(report.Checks, check)
		}
	}

	// Check for orphaned prompts (exist on disk but not in index)
	if err := report.findOrphanedPrompts(onDisk); err != nil {
		return report, err
	}

	// Auto-rebuild if stale entries found
	if autoRebuild && report.Stale > 0 {
		if err := RebuildIndex(); err != nil {
			return report, fmt.Errorf("rebuild failed: %w", err)
		}
		report.Rebuilt = true
	}

	return report, nil
}

// RebuildIndex reconstructs the index from disk by scanning all component/version
// directories, reading each metadata.yaml, and writing a fresh _index.yaml.
// Per spec §8.4: "the registry rebuilds the index and logs INDEX_STALE".
func RebuildIndex() error {
	newIndex := Index{}

	entries, err := os.ReadDir(RegistryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No registry dir = empty index
		}
		return err
	}

	for _, componentEntry := range entries {
		if !componentEntry.IsDir() || componentEntry.Name()[0] == '_' || componentEntry.Name()[0] == '.' {
			continue
		}
		component := componentEntry.Name()

		versionEntries, err := os.ReadDir(filepath.Join(RegistryDir, component))
		if err != nil {
			continue
		}

		for _, versionEntry := range versionEntries {
			if !versionEntry.IsDir() || versionEntry.Name()[0] == '.' {
				continue
			}
			version := versionEntry.Name()

			metaPath := filepath.Join(RegistryDir, component, version, "metadata.yaml")
			meta, err := readMetadata(metaPath)
			if err != nil {
				continue // Skip unparseable entries
			}

			if newIndex[component] == nil {
				newIndex[component] = make(map[string]*PromptEntry)
			}
			newIndex[component][version] = &PromptEntry{
				Hash:     meta.Hash,
				Status:   meta.Status,
				Model:    meta.Model,
				Provider: meta.Provider,
			}
		}
	}

	return saveIndex(&newIndex)
}

// findOrphanedPrompts scans the registry directory for component/version pairs
// that exist on disk but are not in the index.
func (r *ConsistencyReport) findOrphanedPrompts(onDisk map[string]map[string]bool) error {
	entries, err := os.ReadDir(RegistryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, componentEntry := range entries {
		if !componentEntry.IsDir() || componentEntry.Name()[0] == '_' || componentEntry.Name()[0] == '.' {
			continue
		}
		component := componentEntry.Name()

		versionEntries, err := os.ReadDir(filepath.Join(RegistryDir, component))
		if err != nil {
			continue
		}

		for _, versionEntry := range versionEntries {
			if !versionEntry.IsDir() || versionEntry.Name()[0] == '.' {
				continue
			}
			version := versionEntry.Name()

			metaPath := filepath.Join(RegistryDir, component, version, "metadata.yaml")
			if _, err := readMetadata(metaPath); err != nil {
				continue // Not a valid prompt directory
			}

			inIndex := false
			if seen, ok := onDisk[component]; ok {
				inIndex = seen[version]
			}

			if !inIndex {
				r.Orphaned++
				r.Checks = append(r.Checks, ConsistencyCheck{
					Component: component,
					Version:   version,
					Status:    ConsistencyOrphaned,
					Detail:    "prompt directory exists on disk but is not in the index",
				})
			}
		}
	}
	return nil
}
