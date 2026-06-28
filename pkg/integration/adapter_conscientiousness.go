package integration

// ---------------------------------------------------------------------------
// Conscientiousness Adapter — specs/integrations.md §3
// ---------------------------------------------------------------------------
//
// Conscientiousness provides agentic self-evaluation: adversarial review of
// PRs with verification loops. Runs after Chimera review. Asks "are you sure?"
// and feeds findings back to Axiom for work-item confidence scoring.

// ConscientiousnessAdapter defines the contract for adversarial evaluation.
type ConscientiousnessAdapter interface {
	// Evaluate runs adversarial self-evaluation on a completed PR.
	Evaluate(pr ConscientiousnessPR, opts EvalOpts) (*ConscientiousnessVerdict, error)

	// Health returns service health.
	Health() (*ConscientiousnessHealth, error)
}

// ConscientiousnessPR bundles the information needed for adversarial review.
type ConscientiousnessPR struct {
	RepoOwner       string
	RepoName        string
	PRNumber        int
	Diff            string
	ChimeraVerdict  *ChimeraVerdict      // Chimera's review (input to adversarial eval)
	GitReinsEval    *EvalResult          // GitReins Tier 2 result
	EvidenceBundle  string               // Path to verification.md
	ACs             []AcceptanceCriterion
}

// AcceptanceCriterion represents a single acceptance criterion check.
type AcceptanceCriterion struct {
	ID     string
	Text   string
	Status string // "pass", "fail", "untested"
}

// ConscientiousnessVerdict is the outcome of adversarial evaluation.
type ConscientiousnessVerdict struct {
	Status        string  // "DEFENSIBLE", "VULNERABLE", "INDEFENSIBLE"
	Confidence    float64
	AttackVectors []AttackVector
	Mitigations   []Mitigation
	Cost          float64
}

// AttackVector describes a discovered attack surface.
type AttackVector struct {
	Description   string
	Severity      string
	Exploitability string // "trivial", "moderate", "difficult", "theoretical"
}

// Mitigation describes a countermeasure for an attack vector.
type Mitigation struct {
	AttackVector string
	Mitigation   string
	Sufficient   bool
}

// ConscientiousnessHealth reports the service's operational status.
type ConscientiousnessHealth struct {
	Status  string
	Version string
	Uptime  float64
}
