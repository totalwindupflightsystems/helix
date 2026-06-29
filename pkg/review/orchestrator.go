package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// =============================================================================
// ReviewOrchestrator — Multi-Model Adversarial Review
//
// Per spec §Multi-Model Adversarial Review and §Three-Layer Review Pipeline:
//
//   1. Strip bias from commit message (BiasStripper)
//   2. Dispatch review to N models from different providers
//   3. Collect independent findings
//   4. Reconcile consensus: all agree → PASS, N-1 agree → WARN, divergence → FLAG
//   5. Build evidence bundle with model diversity score
//
// Provider diversity requirement: at least 2 different provider families
// in every review panel (spec §Model Formation Strategy).
// =============================================================================

// ModelClient is the interface each review model must implement.
// A model receives the stripped diff + neutral context and returns
// independent findings plus its verdict.
type ModelClient interface {
	// Review submits the code for adversarial review.
	Review(ctx context.Context, req ReviewRequest) (*ModelReviewResult, error)
	// Info returns model identity for formation and diversity checks.
	Info() ModelInfo
}

// ReviewRequest is the input sent to each model after bias stripping.
type ReviewRequest struct {
	// Diff is the code changes to review.
	Diff string
	// NeutralCommitMsg is the bias-stripped commit message.
	NeutralCommitMsg string
	// Role tells the model what adversarial posture to take.
	Role ReviewRole
	// Context provides additional review parameters.
	Context ReviewContext
}

// ReviewRole defines the model's position in the adversarial formation.
type ReviewRole string

const (
	// RolePrimary — structural correctness review.
	RolePrimary ReviewRole = "primary"
	// RoleAdversarial — find what primary missed.
	RoleAdversarial ReviewRole = "adversarial"
	// RoleAudit — verify adversarial findings are real.
	RoleAudit ReviewRole = "audit"
)

// ReviewContext carries change-category-specific review parameters.
type ReviewContext struct {
	// ChangeCategory determines consensus thresholds (per spec §Review Criteria).
	Category ChangeCategory
	// PRURL is the PR under review.
	PRURL string
	// ReviewID is the unique identifier for this review session.
	ReviewID string
}

// ChangeCategory classifies the type of code change to determine
// the required review formation depth (spec §Review Criteria by Change Category).
type ChangeCategory string

const (
	// CategoryContract — API signatures, data schemas, auth.
	// Requires full 3-model formation, 3/3 consensus.
	CategoryContract ChangeCategory = "contract"
	// CategoryBehavioral — business logic, algorithms, state machines.
	// Requires 2-model formation, 2/2 consensus.
	CategoryBehavioral ChangeCategory = "behavioral"
	// CategoryResilience — error handling, retry, circuit breakers.
	// Requires primary review + property testing.
	CategoryResilience ChangeCategory = "resilience"
	// CategoryCosmetic — formatting, comments, variable names.
	// Single-model review, auto-merge if Tier 1 passes.
	CategoryCosmetic ChangeCategory = "cosmetic"
)

// ModelReviewResult is what a single model returns after reviewing code.
type ModelReviewResult struct {
	Verdict  string    `json:"verdict"`
	Findings []Finding `json:"findings"`
	// TokenUsed is the token count consumed (for cost tracking).
	TokenUsed int `json:"token_used,omitempty"`
	// Latency is how long the review took.
	Latency time.Duration `json:"latency,omitempty"`
}

// ReviewPanel is the set of models assigned to a review.
type ReviewPanel struct {
	Primary     ModelClient `json:"-"`
	Adversarial ModelClient `json:"-"`
	Audit       ModelClient `json:"-"` // nil for 2-model formations
}

// Roles returns the non-nil roles in the panel.
func (p *ReviewPanel) Roles() []ReviewRole {
	roles := []ReviewRole{RolePrimary}
	if p.Adversarial != nil {
		roles = append(roles, RoleAdversarial)
	}
	if p.Audit != nil {
		roles = append(roles, RoleAudit)
	}
	return roles
}

// Formation converts the panel to the Formation struct used in evidence bundles.
func (p *ReviewPanel) Formation() Formation {
	f := Formation{Primary: p.Primary.Info()}
	if p.Adversarial != nil {
		f.Adversarial = p.Adversarial.Info()
	}
	if p.Audit != nil {
		f.Audit = p.Audit.Info()
	}
	return f
}

// ReviewOrchestrator coordinates multi-model adversarial review.
type ReviewOrchestrator struct {
	stripper  *BiasStripper
	fpTracker *FPTracker
	// minProviderDiversity is the minimum number of distinct provider families.
	// Default 2 per spec: "at least 2 different provider families."
	minProviderDiversity int
}

// NewReviewOrchestrator creates an orchestrator with default config.
func NewReviewOrchestrator(opts ...OrchestratorOption) *ReviewOrchestrator {
	o := &ReviewOrchestrator{
		stripper:             NewBiasStripper(),
		fpTracker:            NewFPTracker(),
		minProviderDiversity: 2,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// OrchestratorOption configures a ReviewOrchestrator.
type OrchestratorOption func(*ReviewOrchestrator)

// WithBiasStripper sets a custom bias stripper.
func WithBiasStripper(bs *BiasStripper) OrchestratorOption {
	return func(o *ReviewOrchestrator) { o.stripper = bs }
}

// WithFPTracker sets a custom false positive tracker.
func WithFPTracker(fp *FPTracker) OrchestratorOption {
	return func(o *ReviewOrchestrator) { o.fpTracker = fp }
}

// WithMinProviderDiversity sets the minimum number of distinct providers.
func WithMinProviderDiversity(n int) OrchestratorOption {
	return func(o *ReviewOrchestrator) { o.minProviderDiversity = n }
}

// FPTracker exposes the internal false positive tracker for inspection.
func (o *ReviewOrchestrator) FPTracker() *FPTracker { return o.fpTracker }

// ValidatePanel checks that a review panel meets diversity requirements.
// Returns an error if the panel has fewer than minProviderDiversity distinct
// providers, or if any model has been removed from rotation.
func (o *ReviewOrchestrator) ValidatePanel(panel *ReviewPanel) error {
	providers := make(map[string]bool)
	models := []ModelClient{panel.Primary}
	if panel.Adversarial != nil {
		models = append(models, panel.Adversarial)
	}
	if panel.Audit != nil {
		models = append(models, panel.Audit)
	}

	for _, m := range models {
		info := m.Info()
		// Check removal
		if o.fpTracker.IsRemoved(info.Model) {
			return fmt.Errorf("model %q has been removed from rotation (high false positive rate)", info.Model)
		}
		providers[info.Provider] = true
	}

	if len(providers) < o.minProviderDiversity {
		return fmt.Errorf("provider diversity violation: need %d distinct providers, got %d",
			o.minProviderDiversity, len(providers))
	}

	return nil
}

// DetermineFormation returns the recommended panel size for a change category.
func DetermineFormation(cat ChangeCategory) int {
	switch cat {
	case CategoryContract:
		return 3 // full adversarial formation
	case CategoryBehavioral:
		return 2 // primary + adversarial
	case CategoryResilience:
		return 1 // primary only (property testing handles the rest)
	case CategoryCosmetic:
		return 1 // single-model review
	default:
		return 3 // default to full formation for unknown categories
	}
}

// ConsensusThreshold returns the minimum number of models that must agree
// for approval, based on the change category.
func ConsensusThreshold(cat ChangeCategory) int {
	switch cat {
	case CategoryContract:
		return 3 // 3/3 required
	case CategoryBehavioral:
		return 2 // 2/2 required
	case CategoryResilience:
		return 1
	case CategoryCosmetic:
		return 1
	default:
		return 3
	}
}

// DiversityScore computes the provider diversity of a formation.
// Returns the count of distinct providers.
func DiversityScore(f Formation) int {
	providers := map[string]bool{
		f.Primary.Provider: true,
	}
	if f.Adversarial.Provider != "" {
		providers[f.Adversarial.Provider] = true
	}
	if f.Audit.Provider != "" {
		providers[f.Audit.Provider] = true
	}
	return len(providers)
}

// ReviewResult is the complete output of a multi-model review session.
type ReviewResult struct {
	Bundle         *EvidenceBundle `json:"bundle"`
	DiversityScore int             `json:"diversity_score"`
	// ModelsAgree is how many models returned an approving verdict.
	ModelsAgree int `json:"models_agree"`
	// TotalModels is the number of models that participated.
	TotalModels int `json:"total_models"`
	// ConsensusLevel is: "unanimous", "majority", or "divergent".
	ConsensusLevel string `json:"consensus_level"`
}

// Review orchestrates a full multi-model adversarial review.
//
// The pipeline:
//  1. Strip bias from commit message
//  2. Validate panel diversity
//  3. Dispatch to each model concurrently
//  4. Collect results, reconcile consensus
//  5. Build and sign evidence bundle
func (o *ReviewOrchestrator) Review(ctx context.Context, panel *ReviewPanel,
	diff, originalCommitMsg string, cat ChangeCategory, prURL string,
) (*ReviewResult, error) {

	if panel == nil || panel.Primary == nil {
		return nil, fmt.Errorf("panel must have at least a primary model")
	}

	// Step 1: Strip bias from commit message.
	neutralMsg := o.stripper.StripPreservingPrefix(originalCommitMsg)
	biasStrippedSHA := hashSHA256(neutralMsg)
	originalSHA := hashSHA256(originalCommitMsg)

	// Step 2: Validate panel.
	if err := o.ValidatePanel(panel); err != nil {
		return nil, fmt.Errorf("panel validation: %w", err)
	}

	reviewID := generateReviewID(prURL)
	rctx := ReviewContext{
		Category: cat,
		PRURL:    prURL,
		ReviewID: reviewID,
	}

	// Step 3: Dispatch to each model concurrently.
	type modelOutput struct {
		role   ReviewRole
		result *ModelReviewResult
		err    error
	}

	roles := panel.Roles()
	results := make(chan modelOutput, len(roles))
	var wg sync.WaitGroup

	dispatch := func(role ReviewRole, client ModelClient) {
		defer wg.Done()
		req := ReviewRequest{
			Diff:             diff,
			NeutralCommitMsg: neutralMsg,
			Role:             role,
			Context:          rctx,
		}
		start := time.Now()
		res, err := client.Review(ctx, req)
		if err == nil && res != nil {
			res.Latency = time.Since(start)
		}
		results <- modelOutput{role: role, result: res, err: err}
	}

	for _, role := range roles {
		wg.Add(1)
		switch role {
		case RolePrimary:
			go dispatch(role, panel.Primary)
		case RoleAdversarial:
			go dispatch(role, panel.Adversarial)
		case RoleAudit:
			go dispatch(role, panel.Audit)
		}
	}

	wg.Wait()
	close(results)

	// Step 4: Collect results.
	verdicts := map[ReviewRole]string{}
	var allFindings []Finding
	totalTokens := 0
	totalModels := 0
	var firstErr error

	for out := range results {
		if out.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("model %s: %w", out.role, out.err)
			}
			// Model failed — record as a block for consensus purposes.
			verdicts[out.role] = VerdictBlock
			totalModels++
			continue
		}
		verdicts[out.role] = out.result.Verdict
		for _, f := range out.result.Findings {
			f.Model = string(out.role)
			allFindings = append(allFindings, f)
		}
		totalTokens += out.result.TokenUsed
		totalModels++
	}

	// Step 5: Reconcile consensus.
	primaryV := verdicts[RolePrimary]
	adversarialV := verdicts[RoleAdversarial]
	auditV := ""
	if _, ok := verdicts[RoleAudit]; ok {
		auditV = verdicts[RoleAudit]
	}

	modelsAgree := countApproving(primaryV, adversarialV, auditV)

	// If every model failed and none approved, return the error.
	if firstErr != nil && modelsAgree == 0 && len(allFindings) == 0 {
		return nil, fmt.Errorf("all models failed: %w", firstErr)
	}

	consensusLevel := classifyConsensus(modelsAgree, totalModels)

	// Step 6: Build evidence bundle.
	formation := panel.Formation()
	bundle := NewEvidenceBundle(prURL, reviewID, formation, biasStrippedSHA, originalSHA)
	for _, f := range allFindings {
		bundle.AddFinding(f)
	}
	bundle.SetConsensus(primaryV, adversarialV, auditV)

	divScore := DiversityScore(formation)

	return &ReviewResult{
		Bundle:         bundle,
		DiversityScore: divScore,
		ModelsAgree:    modelsAgree,
		TotalModels:    totalModels,
		ConsensusLevel: consensusLevel,
	}, nil
}

// countApproving counts how many verdicts are approving.
func countApproving(verdicts ...string) int {
	count := 0
	for _, v := range verdicts {
		if isApproving(v) {
			count++
		}
	}
	return count
}

// classifyConsensus returns the consensus level label.
func classifyConsensus(agree, total int) string {
	if total == 0 {
		return "none"
	}
	if agree == total {
		return "unanimous"
	}
	// Majority means strictly more than half.
	if agree > total/2 {
		return "majority"
	}
	return "divergent"
}

// hashSHA256 returns the hex-encoded SHA-256 of a string.
func hashSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// generateReviewID creates a deterministic review ID from the PR URL and timestamp.
func generateReviewID(prURL string) string {
	ts := time.Now().UTC().UnixNano()
	input := fmt.Sprintf("%s-%d", prURL, ts)
	return hashSHA256(input)[:16]
}
