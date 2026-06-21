package marketplace

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Master index management (spec §11).
//
// The _index.yaml file is a fast-lookup cache mapping agent names to their
// index entries (status, trust, tier, capabilities, cost, rating). It enables
// capability/trust filtering without loading full manifests.

// RebuildIndex regenerates the in-memory index from all registered agents and
// persists it to <marketplace_dir>/_index.yaml (spec §11). If the marketplace
// directory does not exist or is not writable, the index is still rebuilt
// in memory and the write error is silently ignored (stub behavior).
func (r *Registry) RebuildIndex() error {
	r.index = make(map[string]*ManifestIndexEntry)
	for name, a := range r.agents {
		r.index[name] = agentToIndexEntry(a)
	}

	// Persist to disk (best-effort in stub mode).
	indexPath := filepath.Join(r.dir, "_index.yaml")
	data, err := yaml.Marshal(r.index)
	if err != nil {
		return nil // in-memory index is valid; skip disk write on marshal error
	}
	_ = os.WriteFile(indexPath, data, 0644)
	return nil
}

// IndexEntry returns the cached index entry for an agent. If the entry is not
// cached, it is computed from the agent manifest on demand.
func (r *Registry) IndexEntry(name string) (*ManifestIndexEntry, error) {
	if entry, ok := r.index[name]; ok {
		return entry, nil
	}
	a, ok := r.agents[name]
	if !ok {
		return nil, &ExitError{
			Code:    ExitAgentNotFound,
			Message: fmt.Sprintf("AGENT_NOT_FOUND: %s not in marketplace", name),
		}
	}
	entry := agentToIndexEntry(a)
	r.index[name] = entry
	return entry, nil
}

// agentToIndexEntry projects an Agent into its ManifestIndexEntry.
func agentToIndexEntry(a *Agent) *ManifestIndexEntry {
	// Copy capabilities slice to prevent aliasing.
	caps := make([]Capability, len(a.Capabilities))
	copy(caps, a.Capabilities)
	return &ManifestIndexEntry{
		Status:       a.Status,
		TrustScore:   a.TrustScore,
		Tier:         a.Tier,
		Capabilities: caps,
		CostProfile:  a.Budget.CostProfile,
		AvgRating:    a.Ratings.Average,
		ActiveTasks:  0, // stub: no active-task tracking in v1
		UpdatedAt:    a.UpdatedAt,
	}
}
