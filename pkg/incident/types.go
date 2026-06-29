// Package incident implements the incident learning database schema and store
// for tracking AI-agent-caused production incidents and feeding them into
// the trust penalty pipeline.
package incident

import (
	"fmt"
	"sync"
	"time"
)

// Incident represents a production incident attributable to one or more agents.
type Incident struct {
	ID          string     `json:"id"`
	AgentID     string     `json:"agent_id"`
	PRURL       string     `json:"pr_url"`
	Severity    string     `json:"severity"` // "low", "medium", "high", "critical"
	CausalChain []string   `json:"causal_chain"`
	Timestamp   time.Time  `json:"timestamp"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	Description string     `json:"description"`
	Evidence    []string   `json:"evidence"`
}

// Severity constants.
const (
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

// Store is a thread-safe in-memory store for incidents.
// Designed to be backed by a persistent database in production.
type Store struct {
	mu        sync.RWMutex
	incidents map[string]*Incident
	byAgent   map[string][]string // agentID → []incidentID
}

// NewStore creates an empty incident store.
func NewStore() *Store {
	return &Store{
		incidents: make(map[string]*Incident),
		byAgent:   make(map[string][]string),
	}
}

// Add inserts an incident into the store.
func (s *Store) Add(inc *Incident) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if inc.ID == "" {
		return fmt.Errorf("incident ID is required")
	}
	if _, exists := s.incidents[inc.ID]; exists {
		return fmt.Errorf("incident %q already exists", inc.ID)
	}

	s.incidents[inc.ID] = inc
	s.byAgent[inc.AgentID] = append(s.byAgent[inc.AgentID], inc.ID)
	return nil
}

// Get retrieves an incident by ID.
func (s *Store) Get(id string) *Incident {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.incidents[id]
}

// ListByAgent returns all incidents for a given agent.
func (s *Store) ListByAgent(agentID string) []*Incident {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.byAgent[agentID]
	result := make([]*Incident, 0, len(ids))
	for _, id := range ids {
		if inc, ok := s.incidents[id]; ok {
			result = append(result, inc)
		}
	}
	return result
}

// ListBySeverity returns all incidents with the given severity.
func (s *Store) ListBySeverity(severity string) []*Incident {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Incident
	for _, inc := range s.incidents {
		if inc.Severity == severity {
			result = append(result, inc)
		}
	}
	return result
}

// CountInWindow returns the number of incidents for an agent within the
// last d duration.
func (s *Store) CountInWindow(agentID string, d time.Duration) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cutoff := time.Now().Add(-d)
	count := 0
	for _, id := range s.byAgent[agentID] {
		if inc, ok := s.incidents[id]; ok {
			if inc.Timestamp.After(cutoff) {
				count++
			}
		}
	}
	return count
}

// All returns all incidents in the store.
func (s *Store) All() []*Incident {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Incident, 0, len(s.incidents))
	for _, inc := range s.incidents {
		result = append(result, inc)
	}
	return result
}

// Count returns the total number of incidents.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.incidents)
}
