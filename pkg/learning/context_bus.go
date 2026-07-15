// Package learning implements the Helix Phase 12 learning and knowledge transfer
// subsystem — cross-agent notification bus, pattern discovery, and skill marketplace.
//
// §12.3 — Cross-Agent Notification Bus: agents publish findings to domain-scoped
// topics, subscribe to domains relevant to their active tasks, and receive
// budget-tracked notifications so knowledge transfers between concurrent agents
// without flooding context windows.
package learning

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Domain taxonomy — matches spec §12.3 step 1
// ─────────────────────────────────────────────────────────────────────────────

// Domain categorizes the subject area of a shared finding.
type Domain string

const (
	DomainAuth     Domain = "auth"
	DomainDatabase Domain = "database"
	DomainAPI      Domain = "api"
	DomainInfra    Domain = "infra"
	DomainSecurity Domain = "security"
	DomainTesting  Domain = "testing"
	DomainDocs     Domain = "docs"
)

// AllDomains is the canonical domain list.
var AllDomains = []Domain{
	DomainAuth, DomainDatabase, DomainAPI, DomainInfra,
	DomainSecurity, DomainTesting, DomainDocs,
}

// IsValidDomain reports whether d is a recognized domain.
func IsValidDomain(d Domain) bool {
	for _, v := range AllDomains {
		if v == d {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Priority levels — spec §12.3 step 1
// ─────────────────────────────────────────────────────────────────────────────

// Priority indicates the urgency of a finding. Critical findings bypass
// budget limits.
type Priority string

const (
	PriorityInfo     Priority = "info"
	PriorityWarning  Priority = "warning"
	PriorityCritical Priority = "critical"
)

// ─────────────────────────────────────────────────────────────────────────────
// Tier-based budget constants — spec §12.3 step 3
// ─────────────────────────────────────────────────────────────────────────────

const (
	// FindingTokenCost is the approximate context-window cost of reading one
	// finding (~500 tokens per spec §12.3 step 3).
	FindingTokenCost = 500

	// Daily finding budgets per tier:
	BudgetProvisional = 10 // Provisional agents get 10 findings/day
	BudgetObserved    = 20 // Observed agents get 20 findings/day
	BudgetTrusted     = 35 // Trusted agents get 35 findings/day
	BudgetVeteran     = 50 // Veteran agents get 50 findings/day
)

// DefaultFindingRetention is how long findings persist before expiry
// (30 days per spec §12.3 step 6).
const DefaultFindingRetention = 30 * 24 * time.Hour

// ─────────────────────────────────────────────────────────────────────────────
// SharedFinding — spec §12.3 step 1 schema
// ─────────────────────────────────────────────────────────────────────────────

// SharedFinding is a structured notification from one agent to others about
// a discovery relevant to their domain.
type SharedFinding struct {
	ID            string    `json:"id"`             // unique finding ID (hex)
	FromAgentID   string    `json:"from_agent_id"`  // discovering agent
	ToAgentID     string    `json:"to_agent_id"`    // target agent (empty = broadcast)
	Domain        Domain    `json:"domain"`         // subject area
	Finding       string    `json:"finding"`        // structured description
	EvidenceLinks []string  `json:"evidence_links"` // PRs, commits, incidents
	Priority      Priority  `json:"priority"`       // info / warning / critical
	Timestamp     time.Time `json:"timestamp"`
	Consumed      bool      `json:"consumed"` // has the target agent seen it?
}

// NewFindingID generates a unique hex-encoded ID.
func NewFindingID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("learning: crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

// ─────────────────────────────────────────────────────────────────────────────
// Subscription — spec §12.3 step 2
// ─────────────────────────────────────────────────────────────────────────────

// Subscription represents an agent's interest in one or more domains.
type Subscription struct {
	AgentID   string    `json:"agent_id"`
	Domains   []Domain  `json:"domains"`
	CreatedAt time.Time `json:"created_at"`
}

// ─────────────────────────────────────────────────────────────────────────────
// ContextBus — spec §12.3 step 1–4
// ─────────────────────────────────────────────────────────────────────────────

// ContextBus is the pub/sub notification bus for cross-agent knowledge transfer.
// Agents publish findings to domains; subscribers receive matching findings
// subject to budget constraints.
//
// All findings are persisted to a JSONL store for DuckBrain cross-session recall.
// Subscriptions are persisted as JSON.
type ContextBus struct {
	mu            sync.RWMutex
	findingsPath  string // ~/.helix/context_bus/findings.jsonl
	subsPath      string // ~/.helix/context_bus/subscriptions.json
	findings      []SharedFinding
	subscriptions []Subscription
	dailyCounts   map[string]int // agentID → findings consumed today
	lastReset     time.Time
}

// DefaultStoreDir returns the default directory for ContextBus persistence.
func DefaultStoreDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".helix", "context_bus")
}

// NewContextBus creates or loads a ContextBus backed by the given directory.
// If dir is empty, DefaultStoreDir() is used.
func NewContextBus(dir string) (*ContextBus, error) {
	if dir == "" {
		dir = DefaultStoreDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("learning: create context_bus dir: %w", err)
	}

	cb := &ContextBus{
		findingsPath:  filepath.Join(dir, "findings.jsonl"),
		subsPath:      filepath.Join(dir, "subscriptions.json"),
		findings:      make([]SharedFinding, 0),
		subscriptions: make([]Subscription, 0),
		dailyCounts:   make(map[string]int),
		lastReset:     time.Now(),
	}

	// Load existing state (best-effort).
	cb.loadFindings()
	cb.loadSubscriptions()
	cb.resetDailyIfNeeded()

	return cb, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Persistence helpers
// ─────────────────────────────────────────────────────────────────────────────

func (cb *ContextBus) loadFindings() {
	f, err := os.Open(cb.findingsPath)
	if err != nil {
		return // no existing file
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	for {
		var sf SharedFinding
		if err := dec.Decode(&sf); err != nil {
			break
		}
		cb.findings = append(cb.findings, sf)
	}
}

func (cb *ContextBus) saveFinding(sf SharedFinding) error {
	f, err := os.OpenFile(cb.findingsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("learning: open findings file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	if err := enc.Encode(sf); err != nil {
		return fmt.Errorf("learning: encode finding: %w", err)
	}
	return nil
}

func (cb *ContextBus) loadSubscriptions() {
	data, err := os.ReadFile(cb.subsPath)
	if err != nil {
		return
	}
	json.Unmarshal(data, &cb.subscriptions) // best-effort
}

func (cb *ContextBus) saveSubscriptions() error {
	data, err := json.MarshalIndent(cb.subscriptions, "", "  ")
	if err != nil {
		return fmt.Errorf("learning: marshal subscriptions: %w", err)
	}
	if err := os.WriteFile(cb.subsPath, data, 0o644); err != nil {
		return fmt.Errorf("learning: write subscriptions: %w", err)
	}
	return nil
}

func (cb *ContextBus) resetDailyIfNeeded() {
	now := time.Now()
	if now.YearDay() != cb.lastReset.YearDay() || now.Year() != cb.lastReset.Year() {
		cb.dailyCounts = make(map[string]int)
		cb.lastReset = now
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Publish — spec §12.3 steps 1, 3
// ─────────────────────────────────────────────────────────────────────────────

// Publish creates a new shared finding and persists it. The finding is
// broadcast to all agents subscribed to the domain, or targeted to a specific
// agent if toAgentID is non-empty.
func (cb *ContextBus) Publish(fromAgentID string, toAgentID string, domain Domain, finding string, evidenceLinks []string, priority Priority) (*SharedFinding, error) {
	if fromAgentID == "" {
		return nil, fmt.Errorf("learning: from_agent_id is required")
	}
	if !IsValidDomain(domain) {
		return nil, fmt.Errorf("learning: invalid domain %q (valid: %v)", domain, AllDomains)
	}
	if finding == "" {
		return nil, fmt.Errorf("learning: finding text is required")
	}
	if priority == "" {
		priority = PriorityInfo
	}

	sf := SharedFinding{
		ID:            NewFindingID(),
		FromAgentID:   fromAgentID,
		ToAgentID:     toAgentID,
		Domain:        domain,
		Finding:       finding,
		EvidenceLinks: evidenceLinks,
		Priority:      priority,
		Timestamp:     time.Now(),
		Consumed:      false,
	}

	cb.mu.Lock()
	cb.findings = append(cb.findings, sf)
	if err := cb.saveFinding(sf); err != nil {
		cb.mu.Unlock()
		return nil, err
	}
	cb.mu.Unlock()

	return &sf, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Subscribe / Unsubscribe — spec §12.3 step 2
// ─────────────────────────────────────────────────────────────────────────────

// Subscribe registers an agent's interest in the given domains. Duplicate
// domains are ignored.
func (cb *ContextBus) Subscribe(agentID string, domains []Domain) error {
	if agentID == "" {
		return fmt.Errorf("learning: agent_id is required")
	}
	if len(domains) == 0 {
		return fmt.Errorf("learning: at least one domain is required")
	}
	for _, d := range domains {
		if !IsValidDomain(d) {
			return fmt.Errorf("learning: invalid domain %q", d)
		}
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Find existing subscription or create.
	found := false
	for i, sub := range cb.subscriptions {
		if sub.AgentID == agentID {
			// Merge domains (deduplicate).
			existing := make(map[Domain]bool)
			for _, d := range sub.Domains {
				existing[d] = true
			}
			for _, d := range domains {
				if !existing[d] {
					sub.Domains = append(sub.Domains, d)
					existing[d] = true
				}
			}
			cb.subscriptions[i] = sub
			found = true
			break
		}
	}
	if !found {
		cb.subscriptions = append(cb.subscriptions, Subscription{
			AgentID:   agentID,
			Domains:   domains,
			CreatedAt: time.Now(),
		})
	}

	return cb.saveSubscriptions()
}

// Unsubscribe removes an agent from the given domains. If domains is empty,
// the agent is removed from all domains.
func (cb *ContextBus) Unsubscribe(agentID string, domains []Domain) error {
	if agentID == "" {
		return fmt.Errorf("learning: agent_id is required")
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	for i, sub := range cb.subscriptions {
		if sub.AgentID == agentID {
			if len(domains) == 0 {
				// Remove completely.
				cb.subscriptions = append(cb.subscriptions[:i], cb.subscriptions[i+1:]...)
			} else {
				removeSet := make(map[Domain]bool)
				for _, d := range domains {
					removeSet[d] = true
				}
				remaining := make([]Domain, 0, len(sub.Domains))
				for _, d := range sub.Domains {
					if !removeSet[d] {
						remaining = append(remaining, d)
					}
				}
				if len(remaining) == 0 {
					cb.subscriptions = append(cb.subscriptions[:i], cb.subscriptions[i+1:]...)
				} else {
					sub.Domains = remaining
					cb.subscriptions[i] = sub
				}
			}
			return cb.saveSubscriptions()
		}
	}

	return fmt.Errorf("learning: agent %q has no subscriptions", agentID)
}

// GetSubscription returns the subscription for the given agent, or nil if not found.
func (cb *ContextBus) GetSubscription(agentID string) *Subscription {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	for _, sub := range cb.subscriptions {
		if sub.AgentID == agentID {
			s := sub // copy
			return &s
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Inbox — spec §12.3 step 4
// ─────────────────────────────────────────────────────────────────────────────

// DailyBudget returns the daily finding budget for a given tier.
func DailyBudget(tier string) int {
	switch strings.ToLower(tier) {
	case "provisional":
		return BudgetProvisional
	case "observed":
		return BudgetObserved
	case "trusted":
		return BudgetTrusted
	case "veteran":
		return BudgetVeteran
	default:
		return BudgetProvisional
	}
}

// GetInbox returns unread findings relevant to the given agent, sorted by
// priority (critical first) then recency (newest first). Budget constraints
// are enforced: the agent's daily budget is checked, and only findings within
// budget are returned. Critical findings bypass budget limits.
//
// The tier parameter controls the daily budget cap.
func (cb *ContextBus) GetInbox(agentID string, tier string, unreadOnly bool) ([]SharedFinding, error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.resetDailyIfNeeded()
	budget := DailyBudget(tier)
	used := cb.dailyCounts[agentID]

	// Get agent's subscribed domains.
	domainSet := make(map[Domain]bool)
	for _, sub := range cb.subscriptions {
		if sub.AgentID == agentID {
			for _, d := range sub.Domains {
				domainSet[d] = true
			}
			break
		}
	}

	// Collect matching findings.
	var matches []SharedFinding
	now := time.Now()
	cutoff := now.Add(-DefaultFindingRetention)

	for _, sf := range cb.findings {
		// Skip expired.
		if sf.Timestamp.Before(cutoff) {
			continue
		}
		// Skip if unread-only and already consumed.
		if unreadOnly && sf.Consumed {
			continue
		}
		// Match by domain subscription or direct targeting.
		if sf.ToAgentID != "" {
			if sf.ToAgentID != agentID {
				continue
			}
		} else if !domainSet[sf.Domain] {
			continue
		}

		matches = append(matches, sf)
	}

	// Sort: critical first, then warning, then info; within same priority, newest first.
	sort.Slice(matches, func(i, j int) bool {
		pi := priorityOrder(matches[i].Priority)
		pj := priorityOrder(matches[j].Priority)
		if pi != pj {
			return pi < pj // lower order = higher priority
		}
		return matches[i].Timestamp.After(matches[j].Timestamp)
	})

	// Apply budget. Critical findings bypass budget.
	var result []SharedFinding
	remaining := budget - used
	for _, sf := range matches {
		if remaining <= 0 && sf.Priority != PriorityCritical {
			break
		}
		// Copy and mark consumed.
		sfCopy := sf
		sfCopy.Consumed = true
		result = append(result, sfCopy)
		// Mark original as consumed + increment daily count.
		cb.dailyCounts[agentID]++
		for j := range cb.findings {
			if cb.findings[j].ID == sf.ID {
				cb.findings[j].Consumed = true
				break
			}
		}
		if sf.Priority != PriorityCritical {
			remaining--
		}
	}

	return result, nil
}

func priorityOrder(p Priority) int {
	switch p {
	case PriorityCritical:
		return 0
	case PriorityWarning:
		return 1
	default:
		return 2
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Query helpers
// ─────────────────────────────────────────────────────────────────────────────

// ListFindings returns all findings, optionally filtered by domain.
func (cb *ContextBus) ListFindings(domain Domain) []SharedFinding {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if domain == "" {
		out := make([]SharedFinding, len(cb.findings))
		copy(out, cb.findings)
		return out
	}

	var out []SharedFinding
	for _, sf := range cb.findings {
		if sf.Domain == domain {
			out = append(out, sf)
		}
	}
	return out
}

// DailyUsage returns the number of findings consumed by the agent today.
func (cb *ContextBus) DailyUsage(agentID string) int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	cb.resetDailyIfNeeded()
	return cb.dailyCounts[agentID]
}

// ListSubscriptions returns all subscriptions.
func (cb *ContextBus) ListSubscriptions() []Subscription {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	out := make([]Subscription, len(cb.subscriptions))
	copy(out, cb.subscriptions)
	return out
}
