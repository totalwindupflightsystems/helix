package recovery

import "time"

// Disaster recovery scenarios per spec §10.3.

// DRScenario represents a disaster recovery scenario from spec §10.3.
type DRScenario struct {
	ID        string
	Scenario  string
	Detection string
	Response  string
	RTO       string // Recovery Time Objective
	RPO       string // Recovery Point Objective
	Severity  Severity
}

// DRScenarioID constants.
const (
	DRHardwareFailure    = "dr-hardware-failure"
	DRDiskFailure        = "dr-disk-failure"
	DRAccidentalDeletion = "dr-accidental-deletion"
	DRSecurityBreach     = "dr-security-breach"
	DRForgejoCorruption  = "dr-forgejo-corruption"
)

// DefaultDRScenarios returns the 5 spec §10.3 DR scenarios.
func DefaultDRScenarios() []DRScenario {
	return []DRScenario{
		{
			ID:        DRHardwareFailure,
			Scenario:  "Host hardware failure",
			Detection: "Hetzner monitoring",
			Response:  "Provision new server, restore from latest backup",
			RTO:       "4 hours",
			RPO:       "24 hours",
			Severity:  SEV1,
		},
		{
			ID:        DRDiskFailure,
			Scenario:  "Disk failure",
			Detection: "SMART alerts, filesystem errors",
			Response:  "Replace disk, restore from backup",
			RTO:       "2 hours",
			RPO:       "24 hours",
			Severity:  SEV1,
		},
		{
			ID:        DRAccidentalDeletion,
			Scenario:  "Accidental deletion",
			Detection: "Manual report or audit",
			Response:  "Restore specific repo/DB from backup",
			RTO:       "30 minutes",
			RPO:       "24 hours",
			Severity:  SEV2,
		},
		{
			ID:        DRSecurityBreach,
			Scenario:  "Security breach (agent container)",
			Detection: "Intrusion detection, anomaly alerts",
			Response:  "Kill container, rotate all keys, audit logs",
			RTO:       "1 hour",
			RPO:       "0 (agents can be re-provisioned)",
			Severity:  SEV1,
		},
		{
			ID:        DRForgejoCorruption,
			Scenario:  "Forgejo corruption",
			Detection: "Health check failure",
			Response:  "Restore Forgejo from backup, replay git reflog for recent pushes",
			RTO:       "1 hour",
			RPO:       "0 (git is distributed)",
			Severity:  SEV1,
		},
	}
}

// DRRegistry holds all DR scenarios for lookup.
type DRRegistry struct {
	scenarios map[string]DRScenario
}

// NewDRRegistry creates a registry populated with default spec §10.3 scenarios.
func NewDRRegistry() *DRRegistry {
	r := &DRRegistry{scenarios: make(map[string]DRScenario)}
	for _, s := range DefaultDRScenarios() {
		r.scenarios[s.ID] = s
	}
	return r
}

// Get retrieves a DR scenario by ID.
func (r *DRRegistry) Get(id string) (DRScenario, bool) {
	s, ok := r.scenarios[id]
	return s, ok
}

// All returns all registered DR scenarios.
func (r *DRRegistry) All() []DRScenario {
	out := make([]DRScenario, 0, len(r.scenarios))
	for _, s := range r.scenarios {
		out = append(out, s)
	}
	return out
}

// BySeverity returns scenarios filtered by severity.
func (r *DRRegistry) BySeverity(sev Severity) []DRScenario {
	var out []DRScenario
	for _, s := range r.scenarios {
		if s.Severity == sev {
			out = append(out, s)
		}
	}
	return out
}

// KeyRotationSteps returns the spec §10.3 security incident key rotation procedure.
func KeyRotationSteps() []string {
	return []string{
		"Pause all agents: hivemind-cli agents pause --all",
		"Rotate platform master keys: hermes config rotate-keys --platform",
		"Rotate per-agent keys: h4f-cli rotate-all-agent-keys",
		"Rotate Forgejo admin token: forgejo-cli admin token-revoke --all && forgejo-cli admin token-create --name admin --scopes all > /opt/helix/.env.new",
		"Resume agents: hivemind-cli agents resume --all",
	}
}

// FormatDRScenario renders a single DR scenario for CLI output.
func FormatDRScenario(s DRScenario) string {
	return s.Scenario + " (RTO: " + s.RTO + ", RPO: " + s.RPO + ")\n" +
		"  Detection: " + s.Detection + "\n" +
		"  Response:  " + s.Response + "\n" +
		"  Severity:  " + string(s.Severity)
}

// ScalingModel encodes spec §10.4 scaling model.
type ScalingModel struct {
	MaxConcurrentAgents      int           // 20
	CoresPerAgent            float64       // 0.8
	HostCores                int           // 16
	GitCloneLatencyThreshold time.Duration // 2s — when to add a second host
	PrometheusStorageLimit   float64       // 500GB — when to add a second host
}

// DefaultScalingModel returns the spec §10.4 scaling model.
func DefaultScalingModel() ScalingModel {
	return ScalingModel{
		MaxConcurrentAgents:      20,
		CoresPerAgent:            0.8,
		HostCores:                16,
		GitCloneLatencyThreshold: 2_000_000_000, // 2s in nanoseconds
		PrometheusStorageLimit:   500.0,         // GB
	}
}

// ShouldAddHost returns true if any spec §10.4 threshold is exceeded.
func (sm ScalingModel) ShouldAddHost(currentAgents int, gitCloneLatency time.Duration, promStorageGB float64) bool {
	if currentAgents > sm.MaxConcurrentAgents {
		return true
	}
	if gitCloneLatency > sm.GitCloneLatencyThreshold {
		return true
	}
	if promStorageGB > sm.PrometheusStorageLimit {
		return true
	}
	return false
}

// MaxAgentsForCores computes the maximum concurrent agents given a host's core count.
func MaxAgentsForCores(cores int, coresPerAgent float64) int {
	if coresPerAgent <= 0 {
		return 0
	}
	return int(float64(cores) / coresPerAgent)
}
