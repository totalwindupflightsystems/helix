package marketplace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Registry is an in-memory agent store backed by YAML manifests on disk
// (spec §6, §11). On creation it loads all manifests from
// <marketplace_dir>/agents/*.yaml into memory. Mutations (Register,
// UpdateStatus, Rate) update the in-memory copy and, where applicable,
// persist back to disk.
type Registry struct {
	dir    string
	agents map[string]*Agent
	index  map[string]*ManifestIndexEntry
}

// NewRegistry creates a Registry backed by marketplaceDir. If the directory
// or its agents/ subdirectory does not exist, an empty Registry is returned
// (no error) — callers can Register agents into it.
func NewRegistry(marketplaceDir string) (*Registry, error) {
	r := &Registry{
		dir:    marketplaceDir,
		agents: make(map[string]*Agent),
		index:  make(map[string]*ManifestIndexEntry),
	}

	agentsDir := filepath.Join(marketplaceDir, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil // empty registry — dir not created yet
		}
		return nil, fmt.Errorf("read agents dir %q: %w", agentsDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(agentsDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue // skip unreadable manifests
		}
		var a Agent
		if err := yaml.Unmarshal(data, &a); err != nil {
			continue // skip malformed manifests
		}
		if a.Name == "" {
			continue
		}
		if a.Status == "" {
			a.Status = StatusActive
		}
		r.agents[a.Name] = &a
	}
	return r, nil
}

// Register adds or replaces an agent in the registry. The agent's capabilities
// are validated against the closed set (spec §3.2). Returns ExitInvalidCapability
// for unknown tags or ExitManifestInvalid for a missing name.
func (r *Registry) Register(agent *Agent) error {
	if agent == nil {
		return &ExitError{Code: ExitManifestInvalid, Message: "MANIFEST_INVALID: agent is nil"}
	}
	if agent.Name == "" {
		return &ExitError{Code: ExitManifestInvalid, Message: "MANIFEST_INVALID: name is required"}
	}
	for _, c := range agent.Capabilities {
		if !c.Valid() {
			return &ExitError{
				Code:    ExitInvalidCapability,
				Message: fmt.Sprintf("INVALID_CAPABILITY: %s not a recognized capability", c),
			}
		}
	}
	if agent.Status == "" {
		agent.Status = StatusActive
	}
	if agent.CreatedAt == "" {
		agent.CreatedAt = nowISO()
	}
	agent.UpdatedAt = nowISO()
	r.agents[agent.Name] = agent
	return nil
}

// Get returns the agent with the given name, or ExitAgentNotFound if unknown.
func (r *Registry) Get(name string) (*Agent, error) {
	a, ok := r.agents[name]
	if !ok {
		return nil, &ExitError{
			Code:    ExitAgentNotFound,
			Message: fmt.Sprintf("AGENT_NOT_FOUND: %s not in marketplace", name),
		}
	}
	return a, nil
}

// List returns all agents passing filter (nil filter = all agents). The result
// is sorted by agent name for deterministic ordering.
func (r *Registry) List(filter func(*Agent) bool) ([]*Agent, error) {
	var result []*Agent
	for _, a := range r.agents {
		if filter == nil || filter(a) {
			result = append(result, a)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// Search returns agents matching the query, ranked by trust score (descending).
// Capability filtering is AND semantics: the agent must have ALL specified
// capabilities. The result is trimmed to q.Limit if non-zero.
func (r *Registry) Search(q SearchQuery) ([]*Agent, error) {
	matched, err := r.List(func(a *Agent) bool {
		// Retired agents are never discoverable (spec §10.1).
		if a.Status == StatusRetired {
			return false
		}
		if q.MinTrust > 0 && a.TrustScore < q.MinTrust {
			return false
		}
		if q.MaxCost > 0 && a.Budget.AverageTaskCost > q.MaxCost {
			return false
		}
		for _, c := range q.Capabilities {
			if !hasCapability(a, c) {
				return false
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].TrustScore > matched[j].TrustScore
	})

	if q.Limit > 0 && len(matched) > q.Limit {
		matched = matched[:q.Limit]
	}
	return matched, nil
}

// UpdateStatus transitions an agent to a new lifecycle state (spec §10.1).
// Only active → deprecated → retired (and deprecated → active via Reactivate)
// are valid. Setting DeprecatedAt when transitioning to deprecated.
func (r *Registry) UpdateStatus(name string, status AgentStatus) error {
	a, ok := r.agents[name]
	if !ok {
		return &ExitError{
			Code:    ExitAgentNotFound,
			Message: fmt.Sprintf("AGENT_NOT_FOUND: %s not in marketplace", name),
		}
	}
	if !status.Valid() {
		return &ExitError{
			Code:    ExitManifestInvalid,
			Message: fmt.Sprintf("MANIFEST_INVALID: %s is not a valid status", status),
		}
	}
	a.Status = status
	a.UpdatedAt = nowISO()
	if status == StatusDeprecated && a.DeprecatedAt == "" {
		a.DeprecatedAt = nowISO()
	}
	if status == StatusActive {
		a.DeprecatedAt = ""
	}
	return nil
}

// ListByCapability returns all non-retired agents that have the given capability,
// sorted by trust score descending (spec §8.1).
func (r *Registry) ListByCapability(cap Capability) ([]AgentListing, error) {
	matched, err := r.List(func(a *Agent) bool {
		if a.Status == StatusRetired {
			return false
		}
		return hasCapability(a, cap)
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].TrustScore > matched[j].TrustScore
	})

	listings := make([]AgentListing, len(matched))
	for i, a := range matched {
		listings[i] = AgentListing{
			Name:           a.Name,
			Description:    a.DisplayName,
			Capabilities:   a.Capabilities,
			Reputation:     float64(a.TrustScore),
			Reviews:        a.Ratings.Count,
			ActiveProjects: 0, // stub: no active-task tracking in v1
		}
	}
	return listings, nil
}

// GetAgent returns the full AgentProfile for a named agent, including
// reputation history and review summary (spec §8.1). Returns ExitAgentNotFound
// if the agent is not in the marketplace.
func (r *Registry) GetAgent(name string) (*AgentProfile, error) {
	a, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	// Build review summary (most recent first).
	reviewsCopy := make([]Review, len(a.Reviews))
	copy(reviewsCopy, a.Reviews)
	sort.Slice(reviewsCopy, func(i, j int) bool {
		return reviewsCopy[i].Date > reviewsCopy[j].Date
	})

	recentCount := 5
	if len(reviewsCopy) < recentCount {
		recentCount = len(reviewsCopy)
	}

	return &AgentProfile{
		Agent: *a,
		ReputationHistory: []ReputationPoint{
			{Date: a.UpdatedAt, Score: float64(a.TrustScore)},
		},
		ReviewSummary: ReviewSummary{
			Average: a.Ratings.Average,
			Count:   a.Ratings.Count,
			Recent:  reviewsCopy[:recentCount],
		},
	}, nil
}

// nowISO returns the current time in RFC 3339 UTC format.
func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}
