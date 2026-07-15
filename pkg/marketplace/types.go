// Package marketplace implements the Helix Agent Marketplace — the discoverable
// registry where agents are listed, searched, rated, and selected for work
// items. See specs/agent-marketplace.md for the full design.
//
// This file defines the core domain types: Capability tags, Agent manifests,
// reviews, search queries, and exit codes.
package marketplace

import "strings"

// ---------------------------------------------------------------------------
// Capability tags (spec §3.2 — closed set of 11)
// ---------------------------------------------------------------------------

// Capability is a closed-set tag describing what an agent can do (spec §3.2).
type Capability string

const (
	CapGo             Capability = "go"
	CapTypeScript     Capability = "typescript"
	CapPython         Capability = "python"
	CapCodeReview     Capability = "code-review"
	CapSpecWriting    Capability = "spec-writing"
	CapSecurityReview Capability = "security-review"
	CapTesting        Capability = "testing"
	CapRefactoring    Capability = "refactoring"
	CapDocs           Capability = "docs"
	CapDevOps         Capability = "devops"
	CapNegotiation    Capability = "negotiation"
)

// validCapabilities is the canonical set used by Valid and ValidCapability.
var validCapabilities = map[Capability]bool{
	CapGo: true, CapTypeScript: true, CapPython: true,
	CapCodeReview: true, CapSpecWriting: true, CapSecurityReview: true,
	CapTesting: true, CapRefactoring: true, CapDocs: true,
	CapDevOps: true, CapNegotiation: true,
}

// Valid reports whether c is one of the 11 recognized capability tags.
func (c Capability) Valid() bool {
	return validCapabilities[c]
}

// ValidCapability validates a raw string and returns the typed Capability.
func ValidCapability(s string) (Capability, bool) {
	c := Capability(s)
	return c, c.Valid()
}

// ---------------------------------------------------------------------------
// Status, Tier, CostProfile (spec §3.1, §10.1)
// ---------------------------------------------------------------------------

// AgentStatus is the lifecycle state of an agent (spec §10.1).
type AgentStatus string

const (
	StatusActive     AgentStatus = "active"
	StatusDeprecated AgentStatus = "deprecated"
	StatusRetired    AgentStatus = "retired"
)

// Valid reports whether s is a recognized lifecycle state.
func (s AgentStatus) Valid() bool {
	switch s {
	case StatusActive, StatusDeprecated, StatusRetired:
		return true
	}
	return false
}

// CostProfile classifies an agent's average task cost (spec §3.1):
// low (<$0.05), medium ($0.05-0.25), high (>$0.25).
type CostProfile string

const (
	CostLow    CostProfile = "low"
	CostMedium CostProfile = "medium"
	CostHigh   CostProfile = "high"
)

// Valid reports whether c is a recognized cost profile.
func (c CostProfile) Valid() bool {
	switch c {
	case CostLow, CostMedium, CostHigh:
		return true
	}
	return false
}

// Tier is the agent's pricing tier (spec §3.1): pro (full-power models) or
// flash (cost-efficient models).
type Tier string

const (
	TierPro   Tier = "pro"
	TierFlash Tier = "flash"
)

// Valid reports whether t is a recognized tier.
func (t Tier) Valid() bool {
	switch t {
	case TierPro, TierFlash:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Agent manifest sub-structures (spec §3.1)
// ---------------------------------------------------------------------------

// ModelPreferences captures the agent's preferred model, provider, and fallback.
type ModelPreferences struct {
	Primary  string `yaml:"primary"  json:"primary"`
	Provider string `yaml:"provider" json:"provider"`
	Fallback string `yaml:"fallback" json:"fallback"`
}

// Budget captures the agent's weekly spending limit and cost profile.
type Budget struct {
	WeeklyLimit     float64     `yaml:"weekly_limit"      json:"weekly_limit"`
	AverageTaskCost float64     `yaml:"average_task_cost" json:"average_task_cost"`
	CostProfile     CostProfile `yaml:"cost_profile"      json:"cost_profile"`
}

// Performance captures the objective metrics that feed trust calculation
// (spec §7.2). These are updated daily by the recalculation cron.
type Performance struct {
	TasksCompleted    int     `yaml:"tasks_completed"     json:"tasks_completed"`
	PrAcceptanceRate  float64 `yaml:"pr_acceptance_rate"  json:"pr_acceptance_rate"`
	ReviewAccuracy    float64 `yaml:"review_accuracy"     json:"review_accuracy"`
	BudgetAdherence   float64 `yaml:"budget_adherence"    json:"budget_adherence"`
	Uptime            float64 `yaml:"uptime"              json:"uptime"`
	AvgResponseTimeMs int     `yaml:"avg_response_time_ms" json:"avg_response_time_ms"`
}

// Ratings aggregates human review data for display and trust scoring.
type Ratings struct {
	Average      float64        `yaml:"average"      json:"average"`
	Count        int            `yaml:"count"        json:"count"`
	Distribution map[string]int `yaml:"distribution" json:"distribution,omitempty"`
}

// Forgejo links the agent to its Forgejo bot account.
type Forgejo struct {
	Username string `yaml:"username" json:"username"`
	UserID   int    `yaml:"user_id"  json:"user_id"`
}

// ---------------------------------------------------------------------------
// Core domain types
// ---------------------------------------------------------------------------

// Agent is an agent known to the marketplace (spec §3.1). Each agent has a
// YAML manifest stored at <marketplace_dir>/agents/<name>.yaml.
type Agent struct {
	Name             string           `yaml:"name"              json:"name"`
	DisplayName      string           `yaml:"display_name"      json:"display_name"`
	Status           AgentStatus      `yaml:"status"            json:"status"`
	Tier             Tier             `yaml:"tier"              json:"tier"`
	TrustScore       int              `yaml:"trust_score"       json:"trust_score"`
	Capabilities     []Capability     `yaml:"capabilities"      json:"capabilities"`
	ModelPreferences ModelPreferences `yaml:"model_preferences" json:"model_preferences"`
	Budget           Budget           `yaml:"budget"            json:"budget"`
	Performance      Performance      `yaml:"performance"       json:"performance"`
	Ratings          Ratings          `yaml:"ratings"           json:"ratings"`
	Reviews          []Review         `yaml:"reviews,omitempty" json:"reviews,omitempty"`
	Forgejo          Forgejo          `yaml:"forgejo"           json:"forgejo"`
	CreatedAt        string           `yaml:"created_at"        json:"created_at"`
	UpdatedAt        string           `yaml:"updated_at"        json:"updated_at"`
	DeprecatedAt     string           `yaml:"deprecated_at,omitempty" json:"deprecated_at,omitempty"`
	History          AgentHistory     `yaml:"history,omitempty" json:"history,omitempty"`
}

// Review is a human rating of an agent (spec §9). Ratings are 1-5 stars,
// immutable once posted (re-rating replaces the previous review by the same
// author), and only humans can submit them.
type Review struct {
	Author  string `yaml:"author"  json:"author"`
	Rating  int    `yaml:"rating"  json:"rating"`
	Comment string `yaml:"comment" json:"comment"`
	Date    string `yaml:"date"    json:"date"`
}

// SearchRequirements is the Axiom query interface (spec §8.2). Axiom uses this
// when decomposing a work item to find agents for swarm assembly.
type SearchRequirements struct {
	RequiredCapabilities  []Capability `json:"required_capabilities"`
	PreferredCapabilities []Capability `json:"preferred_capabilities"`
	MinTrust              int          `json:"min_trust"`
	MaxCost               float64      `json:"max_cost"`
	Tier                  Tier         `json:"tier"`
	ExcludeAgents         []string     `json:"exclude_agents"`
	Limit                 int          `json:"limit"`
}

// SearchQuery is the CLI search parameters (spec §3.3). This is the
// user-facing filter used by the `helix marketplace search` subcommand.
type SearchQuery struct {
	Capabilities []Capability
	MinTrust     int
	MaxCost      float64
	Limit        int
}

// ManifestIndexEntry is one row in _index.yaml (spec §11). The master index
// provides fast lookup for capability/trust filtering without loading full
// manifests.
type ManifestIndexEntry struct {
	Status       AgentStatus  `yaml:"status"       json:"status"`
	TrustScore   int          `yaml:"trust_score"  json:"trust_score"`
	Tier         Tier         `yaml:"tier"         json:"tier"`
	Capabilities []Capability `yaml:"capabilities" json:"capabilities"`
	CostProfile  CostProfile  `yaml:"cost_profile" json:"cost_profile"`
	AvgRating    float64      `yaml:"avg_rating"   json:"avg_rating"`
	ActiveTasks  int          `yaml:"active_tasks" json:"active_tasks"`
	UpdatedAt    string       `yaml:"updated_at"   json:"updated_at"`
}

// AgentListing is a summary entry in search results (spec §8). It contains
// the fields needed for discovery listings without loading the full manifest.
type AgentListing struct {
	Name           string       `json:"name"`
	Description    string       `json:"description"`
	Capabilities   []Capability `json:"capabilities"`
	Reputation     float64      `json:"reputation"`
	Reviews        int          `json:"reviews"`
	ActiveProjects int          `json:"active_projects"`
}

// AgentProfile is the full agent detail returned by GetAgent (spec §8.1).
// It extends the base Agent with reputation history and review summary.
type AgentProfile struct {
	Agent
	ReputationHistory []ReputationPoint `json:"reputation_history"`
	ReviewSummary     ReviewSummary     `json:"review_summary"`
	// SkillsPublished lists IDs of skills this agent published to the
	// Phase 12 skill transfer marketplace (pkg/learning.SkillRegistry).
	SkillsPublished []string `json:"skills_published,omitempty"`
}

// ReputationPoint is a single entry in an agent's reputation history.
type ReputationPoint struct {
	Date  string  `json:"date"`
	Score float64 `json:"score"`
}

// ReviewSummary aggregates review data for the agent profile display.
type ReviewSummary struct {
	Average float64  `json:"average"`
	Count   int      `json:"count"`
	Recent  []Review `json:"recent"`
}

// ---------------------------------------------------------------------------
// Exit codes (spec §12)
// ---------------------------------------------------------------------------

const (
	ExitSuccess           = 0  // Success
	ExitAgentNotFound     = 1  // AGENT_NOT_FOUND
	ExitInvalidRating     = 2  // INVALID_RATING
	ExitUnauthorized      = 3  // UNAUTHORIZED
	ExitInvalidCapability = 4  // INVALID_CAPABILITY
	ExitManifestInvalid   = 5  // MANIFEST_INVALID
	ExitDryRun            = 10 // DRY_RUN
)

// ExitError wraps an error with a marketplace exit code (spec §12). Callers
// can type-assert to extract the code and exit accordingly.
type ExitError struct {
	Code    int
	Message string
}

// Error implements the error interface.
func (e *ExitError) Error() string { return e.Message }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// hasCapability reports whether agent a has capability c in its capability list.
func hasCapability(a *Agent, c Capability) bool {
	for _, cap := range a.Capabilities {
		if cap == c {
			return true
		}
	}
	return false
}

// capabilitiesString joins an agent's capabilities into a comma-separated
// string for display (e.g. "go, typescript, code-review").
func capabilitiesString(caps []Capability) string {
	parts := make([]string, len(caps))
	for i, c := range caps {
		parts[i] = string(c)
	}
	return strings.Join(parts, ", ")
}
