package mergegate

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/dispatcher"
	"github.com/totalwindupflightsystems/helix/pkg/review"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
	"github.com/totalwindupflightsystems/helix/pkg/verify"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func validBundle() *review.EvidenceBundle {
	b := review.NewEvidenceBundle(
		"https://forgejo/repo/pulls/42",
		"review-uuid-001",
		review.Formation{
			Primary:     review.ModelInfo{Model: "gpt-5.5", Provider: "openai"},
			Adversarial: review.ModelInfo{Model: "deepseek-v4-pro", Provider: "deepseek"},
			Audit:       review.ModelInfo{Model: "owl-alpha", Provider: "openrouter"},
		},
		"sha256-stripped",
		"sha256-original",
	)
	b.SetConsensus(review.VerdictApproved, review.VerdictPassWithNotes, review.VerdictApproved)
	return b
}

func validContract() *verify.BehaviorContract {
	return &verify.BehaviorContract{
		Contract: verify.ContractBody{
			Name:        "auth-session-v2",
			Agent:       "agent-001",
			MergeCommit: "abc1234",
			Assertions: []verify.Assertion{
				{Metric: "success_rate", Op: "gte", Value: 0.999, Window: "1h"},
				{Metric: "p99_latency_ms", Op: "lte", Value: 200, Window: "1h"},
			},
			BreachAction: "rollback_and_notify",
		},
	}
}

func approvedCostResult() *dispatcher.CostGuardResult {
	return &dispatcher.CostGuardResult{
		Decision:      dispatcher.CostGuardApproved,
		AgentID:       "agent-001",
		Tier:          trust.TierTrusted,
		CostCapPerJob: 100.0,
		EstimatedCost: 2.50,
		Reason:        "cost within cap",
	}
}

func fullRequest() MergeRequest {
	return MergeRequest{
		AgentID:      "agent-001",
		AgentTier:    trust.TierTrusted,
		ChangedFiles: []string{"pkg/auth/session.go"},
		Bundle:       validBundle(),
		Contract:     validContract(),
		CostResult:   approvedCostResult(),
	}
}

// ---------------------------------------------------------------------------
// NewMergeGate
// ---------------------------------------------------------------------------

func TestNewMergeGate_Defaults(t *testing.T) {
	g := NewMergeGate()
	if g == nil {
		t.Fatal("NewMergeGate() = nil")
	}
	if g.skipContract {
		t.Error("default skipContract should be false")
	}
	if g.skipCost {
		t.Error("default skipCost should be false")
	}
	if g.requireSignedBundle {
		t.Error("default requireSignedBundle should be false")
	}
}

func TestNewMergeGate_Options(t *testing.T) {
	g := NewMergeGate(WithContractSkipped(), WithCostSkipped(), WithSignedBundleRequired())
	if !g.skipContract {
		t.Error("WithContractSkipped should set skipContract")
	}
	if !g.skipCost {
		t.Error("WithCostSkipped should set skipCost")
	}
	if !g.requireSignedBundle {
		t.Error("WithSignedBundleRequired should set requireSignedBundle")
	}
}

// ---------------------------------------------------------------------------
// Evaluate — full pipeline
// ---------------------------------------------------------------------------

func TestEvaluate_AllPass(t *testing.T) {
	g := NewMergeGate()
	report := g.Evaluate(fullRequest())

	if report.Decision != DecisionAllowed {
		t.Fatalf("Decision = %s, want ALLOWED", report.Decision)
	}
	if len(report.Blockers) != 0 {
		t.Errorf("Blockers = %v, want empty", report.Blockers)
	}
	for _, c := range report.Checks {
		if c.Status != CheckPass {
			t.Errorf("check %s status = %s, want PASS", c.Name, c.Status)
		}
	}
}

func TestEvaluate_NoBundle(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.Bundle = nil

	report := g.Evaluate(req)

	if report.Decision != DecisionBlocked {
		t.Fatalf("Decision = %s, want BLOCKED", report.Decision)
	}
	if len(report.Blockers) == 0 {
		t.Error("Blockers should not be empty for blocked merge")
	}
}

func TestEvaluate_NoContract(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.Contract = nil

	report := g.Evaluate(req)

	if report.Decision != DecisionBlocked {
		t.Fatalf("Decision = %s, want BLOCKED", report.Decision)
	}
}

func TestEvaluate_NoContract_WithSkip(t *testing.T) {
	g := NewMergeGate(WithContractSkipped())
	req := fullRequest()
	req.Contract = nil

	report := g.Evaluate(req)

	if report.Decision != DecisionAllowed {
		t.Fatalf("Decision = %s, want ALLOWED (contract skipped)", report.Decision)
	}
}

func TestEvaluate_BlockedConsensus(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.Bundle.SetConsensus(review.VerdictApproved, review.VerdictBlock, review.VerdictConfirmAdversarial)

	report := g.Evaluate(req)

	if report.Decision != DecisionBlocked {
		t.Fatalf("Decision = %s, want BLOCKED", report.Decision)
	}
}

func TestEvaluate_TieBreakerConsensus_Escalated(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.Bundle.SetConsensus(review.VerdictApproved, review.VerdictBlock, "")

	report := g.Evaluate(req)

	if report.Decision != DecisionEscalated {
		t.Fatalf("Decision = %s, want ESCALATED (tie-breaker needed)", report.Decision)
	}
}

func TestEvaluate_TrustTierInsufficient(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.AgentTier = trust.TierProvisional
	req.ChangedFiles = []string{"deploy/terraform/main.tf"}

	report := g.Evaluate(req)

	if report.Decision != DecisionBlocked {
		t.Fatalf("Decision = %s, want BLOCKED (tier insufficient for IaC)", report.Decision)
	}
}

func TestEvaluate_CostBlocked(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.CostResult = &dispatcher.CostGuardResult{
		Decision:      dispatcher.CostGuardBlocked,
		AgentID:       "agent-001",
		Tier:          trust.TierProvisional,
		CostCapPerJob: 5.0,
		EstimatedCost: 50.0,
		Reason:        "cost exceeds cap",
	}

	report := g.Evaluate(req)

	if report.Decision != DecisionBlocked {
		t.Fatalf("Decision = %s, want BLOCKED (cost exceeded)", report.Decision)
	}
}

func TestEvaluate_CostEscalated(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.CostResult = &dispatcher.CostGuardResult{
		Decision:      dispatcher.CostGuardEscalated,
		AgentID:       "agent-001",
		Tier:          trust.TierTrusted,
		CostCapPerJob: 100.0,
		EstimatedCost: 50.0,
		Reason:        "estimation failed",
	}

	report := g.Evaluate(req)

	if report.Decision != DecisionEscalated {
		t.Fatalf("Decision = %s, want ESCALATED (cost estimation failed)", report.Decision)
	}
}

func TestEvaluate_CostSkipped(t *testing.T) {
	g := NewMergeGate(WithCostSkipped())
	req := fullRequest()
	req.CostResult = nil

	report := g.Evaluate(req)

	if report.Decision != DecisionAllowed {
		t.Fatalf("Decision = %s, want ALLOWED (cost skipped)", report.Decision)
	}
}

func TestEvaluate_NoChangedFiles(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.ChangedFiles = nil

	report := g.Evaluate(req)

	if report.Decision != DecisionAllowed {
		t.Fatalf("Decision = %s, want ALLOWED", report.Decision)
	}
}

// ---------------------------------------------------------------------------
// Evidence bundle check
// ---------------------------------------------------------------------------

func TestCheckEvidenceBundle_Present(t *testing.T) {
	g := NewMergeGate()
	result := g.checkEvidenceBundle(fullRequest())
	if result.Status != CheckPass {
		t.Errorf("status = %s, want PASS", result.Status)
	}
}

func TestCheckEvidenceBundle_Nil(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.Bundle = nil
	result := g.checkEvidenceBundle(req)
	if result.Status != CheckFail {
		t.Errorf("status = %s, want FAIL", result.Status)
	}
}

func TestCheckEvidenceBundle_EmptyReviewID(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.Bundle.ReviewID = ""
	result := g.checkEvidenceBundle(req)
	if result.Status != CheckFail {
		t.Errorf("status = %s, want FAIL (empty review_id)", result.Status)
	}
}

func TestCheckEvidenceBundle_EmptyPRURL(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.Bundle.PRURL = ""
	result := g.checkEvidenceBundle(req)
	if result.Status != CheckFail {
		t.Errorf("status = %s, want FAIL (empty pr_url)", result.Status)
	}
}

func TestCheckEvidenceBundle_SignatureRequired_NoKeys(t *testing.T) {
	g := NewMergeGate(WithSignedBundleRequired())
	result := g.checkEvidenceBundle(fullRequest())
	// No keys provided → should pass (skipped verification)
	if result.Status != CheckPass {
		t.Errorf("status = %s, want PASS (no keys to verify against)", result.Status)
	}
}

func TestCheckEvidenceBundle_SignatureRequired_ValidSig(t *testing.T) {
	g := NewMergeGate(WithSignedBundleRequired())

	// Generate key pair and sign the bundle.
	pub, priv, err := review.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	req := fullRequest()
	if _, err := req.Bundle.SignBundle("primary", priv); err != nil {
		t.Fatalf("SignBundle: %v", err)
	}
	if _, err := req.Bundle.SignBundle("adversarial", priv); err != nil {
		t.Fatalf("SignBundle: %v", err)
	}
	if _, err := req.Bundle.SignBundle("audit", priv); err != nil {
		t.Fatalf("SignBundle: %v", err)
	}

	req.SignKeys = map[string]ed25519.PublicKey{
		"primary":     pub,
		"adversarial": pub,
		"audit":       pub,
	}

	result := g.checkEvidenceBundle(req)
	if result.Status != CheckPass {
		t.Errorf("status = %s, want PASS (valid signatures)", result.Status)
	}
}

func TestCheckEvidenceBundle_SignatureRequired_InvalidSig(t *testing.T) {
	g := NewMergeGate(WithSignedBundleRequired())

	// Generate two key pairs — sign with one, verify with the other.
	pub1, _, _ := review.GenerateKeyPair()
	_, priv2, _ := review.GenerateKeyPair()

	req := fullRequest()
	_, _ = req.Bundle.SignBundle("primary", priv2)

	req.SignKeys = map[string]ed25519.PublicKey{
		"primary": pub1, // mismatched key
	}

	result := g.checkEvidenceBundle(req)
	if result.Status != CheckFail {
		t.Errorf("status = %s, want FAIL (invalid signature)", result.Status)
	}
}

// ---------------------------------------------------------------------------
// Consensus check
// ---------------------------------------------------------------------------

func TestCheckConsensus_Approved(t *testing.T) {
	g := NewMergeGate()
	result := g.checkConsensus(fullRequest())
	if result.Status != CheckPass {
		t.Errorf("status = %s, want PASS", result.Status)
	}
}

func TestCheckConsensus_Blocked(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.Bundle.SetConsensus(review.VerdictBlock, review.VerdictBlock, review.VerdictConfirmAdversarial)
	result := g.checkConsensus(req)
	if result.Status != CheckFail {
		t.Errorf("status = %s, want FAIL", result.Status)
	}
}

func TestCheckConsensus_TieBreaker(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.Bundle.SetConsensus(review.VerdictApproved, review.VerdictBlock, "")
	result := g.checkConsensus(req)
	if result.Status != CheckWarning {
		t.Errorf("status = %s, want WARN", result.Status)
	}
}

func TestCheckConsensus_NoBundle(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.Bundle = nil
	result := g.checkConsensus(req)
	if result.Status != CheckFail {
		t.Errorf("status = %s, want FAIL", result.Status)
	}
}

// ---------------------------------------------------------------------------
// Behavior contract check
// ---------------------------------------------------------------------------

func TestCheckBehaviorContract_Present(t *testing.T) {
	g := NewMergeGate()
	result := g.checkBehaviorContract(fullRequest())
	if result.Status != CheckPass {
		t.Errorf("status = %s, want PASS", result.Status)
	}
}

func TestCheckBehaviorContract_Nil(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.Contract = nil
	result := g.checkBehaviorContract(req)
	if result.Status != CheckFail {
		t.Errorf("status = %s, want FAIL", result.Status)
	}
}

func TestCheckBehaviorContract_InvalidAssertion(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.Contract.Contract.Assertions = []verify.Assertion{
		{Metric: "", Op: "gte", Value: 1, Window: "1h"},
	}
	result := g.checkBehaviorContract(req)
	if result.Status != CheckFail {
		t.Errorf("status = %s, want FAIL (empty metric)", result.Status)
	}
}

// ---------------------------------------------------------------------------
// Trust tier check
// ---------------------------------------------------------------------------

func TestCheckTrustTier_ProvisionalCodeOnly(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.AgentTier = trust.TierProvisional
	req.ChangedFiles = []string{"pkg/util/util.go"} // non-auth code
	result := g.checkTrustTier(req)
	if result.Status != CheckPass {
		t.Errorf("status = %s, want PASS (code is provisional+)", result.Status)
	}
}

func TestCheckTrustTier_ProvisionalIaC(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.AgentTier = trust.TierProvisional
	req.ChangedFiles = []string{"deploy/main.tf"}
	result := g.checkTrustTier(req)
	if result.Status != CheckFail {
		t.Errorf("status = %s, want FAIL (IaC requires observed+)", result.Status)
	}
}

func TestCheckTrustTier_ObservedIaC(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.AgentTier = trust.TierObserved
	req.ChangedFiles = []string{"deploy/main.tf"}
	result := g.checkTrustTier(req)
	if result.Status != CheckPass {
		t.Errorf("status = %s, want PASS (observed can do IaC)", result.Status)
	}
}

func TestCheckTrustTier_TrustedAuth(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.AgentTier = trust.TierTrusted
	req.ChangedFiles = []string{"pkg/auth/session.go", "internal/security/policy.go"}
	result := g.checkTrustTier(req)
	if result.Status != CheckPass {
		t.Errorf("status = %s, want PASS (trusted can do auth)", result.Status)
	}
}

func TestCheckTrustTier_ObservedAuth(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.AgentTier = trust.TierObserved
	req.ChangedFiles = []string{"pkg/auth/session.go"}
	result := g.checkTrustTier(req)
	if result.Status != CheckFail {
		t.Errorf("status = %s, want FAIL (auth requires trusted+)", result.Status)
	}
}

func TestCheckTrustTier_VeteranCICD(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.AgentTier = trust.TierVeteran
	req.ChangedFiles = []string{".forgejo/workflows/build.yml", "Dockerfile"}
	result := g.checkTrustTier(req)
	if result.Status != CheckPass {
		t.Errorf("status = %s, want PASS (veteran can do CI/CD)", result.Status)
	}
}

func TestCheckTrustTier_TrustedCICD(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.AgentTier = trust.TierTrusted
	req.ChangedFiles = []string{".forgejo/workflows/build.yml"}
	result := g.checkTrustTier(req)
	if result.Status != CheckFail {
		t.Errorf("status = %s, want FAIL (CI/CD requires veteran+)", result.Status)
	}
}

func TestCheckTrustTier_NoFiles(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.ChangedFiles = nil
	result := g.checkTrustTier(req)
	if result.Status != CheckPass {
		t.Errorf("status = %s, want PASS (no files)", result.Status)
	}
}

func TestCheckTrustTier_MixedFiles(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.AgentTier = trust.TierProvisional
	req.ChangedFiles = []string{"main.go", "README.md", "test_test.go"}
	result := g.checkTrustTier(req)
	if result.Status != CheckPass {
		t.Errorf("status = %s, want PASS (all provisional+ files)", result.Status)
	}
}

func TestCheckTrustTier_Schema(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.AgentTier = trust.TierProvisional
	req.ChangedFiles = []string{"proto/service.proto"}
	result := g.checkTrustTier(req)
	if result.Status != CheckFail {
		t.Errorf("status = %s, want FAIL (schema requires observed+)", result.Status)
	}
}

// ---------------------------------------------------------------------------
// Cost guard check
// ---------------------------------------------------------------------------

func TestCheckCostGuard_Approved(t *testing.T) {
	g := NewMergeGate()
	result := g.checkCostGuard(fullRequest())
	if result.Status != CheckPass {
		t.Errorf("status = %s, want PASS", result.Status)
	}
}

func TestCheckCostGuard_Blocked(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.CostResult.Decision = dispatcher.CostGuardBlocked
	result := g.checkCostGuard(req)
	if result.Status != CheckFail {
		t.Errorf("status = %s, want FAIL", result.Status)
	}
}

func TestCheckCostGuard_Escalated(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.CostResult.Decision = dispatcher.CostGuardEscalated
	result := g.checkCostGuard(req)
	if result.Status != CheckWarning {
		t.Errorf("status = %s, want WARN", result.Status)
	}
}

func TestCheckCostGuard_Nil(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.CostResult = nil
	result := g.checkCostGuard(req)
	if result.Status != CheckSkipped {
		t.Errorf("status = %s, want SKIPPED", result.Status)
	}
}

// ---------------------------------------------------------------------------
// GateReport
// ---------------------------------------------------------------------------

func TestGateReport_IsAllowed(t *testing.T) {
	g := NewMergeGate()
	report := g.Evaluate(fullRequest())
	if !report.IsAllowed() {
		t.Error("full valid request should be allowed")
	}
}

func TestGateReport_IsBlocked(t *testing.T) {
	g := NewMergeGate()
	req := fullRequest()
	req.Bundle = nil
	report := g.Evaluate(req)
	if !report.IsBlocked() {
		t.Error("missing bundle should be blocked")
	}
}

func TestGateReport_Summary(t *testing.T) {
	g := NewMergeGate()
	report := g.Evaluate(fullRequest())
	s := report.Summary()
	if s == "" {
		t.Error("Summary should not be empty")
	}
	if !contains(s, "ALLOWED") {
		t.Errorf("Summary should contain decision: %s", s)
	}
}

// ---------------------------------------------------------------------------
// File classification
// ---------------------------------------------------------------------------

func TestClassifyFile(t *testing.T) {
	cases := []struct {
		path string
		want FileCategory
	}{
		{"deploy/main.tf", CatIaC},
		{"infra/variables.tfvars", CatIaC},
		{"terraform/modules/vpc.tf", CatIaC},
		{".forgejo/workflows/build.yml", CatCICD},
		{"ci/cd-pipeline.yaml", CatCICD},
		{"Dockerfile", CatCICD},
		{"docker-compose.yaml", CatCICD},
		{"pkg/auth/session.go", CatAuth},
		{"internal/security/policy.go", CatAuth},
		// YAML files with auth keywords are still caught by the YAML branch
		// (matching the shell script ordering: .yaml is checked before auth)
		{"config/secrets.yaml", CatCode}, // .yaml → no CI/CD keywords → code
		{"pkg/oauth/handler.go", CatAuth},
		{"main.go", CatCode},
		{"src/lib.rs", CatCode},
		{"api/server.py", CatCode},
		{"README.md", CatDocs},
		{"docs/guide.md", CatDocs},
		{"CHANGELOG.txt", CatDocs},
		// _test patterns with auth keyword are classified as auth (auth check runs first)
		{"pkg/auth/handler_test.go", CatAuth}, // auth keyword → auth
		{"spec/auth_spec.rb", CatAuth},        // auth keyword → auth
		// openapi.yaml is caught by the .yaml branch before openapi check
		{"api/openapi.yaml", CatCode}, // .yaml → no CI/CD keywords → code
		{"proto/service.proto", CatSchema},
		{"queries/users.sql", CatSchema},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			got := classifyFile(tc.path)
			if got != tc.want {
				t.Errorf("classifyFile(%q) = %s, want %s", tc.path, got, tc.want)
			}
		})
	}
}

func TestMinTierForCategory(t *testing.T) {
	cases := []struct {
		cat  FileCategory
		want trust.TrustTier
	}{
		{CatIaC, trust.TierObserved},
		{CatCICD, trust.TierVeteran},
		{CatAuth, trust.TierTrusted},
		{CatSchema, trust.TierObserved},
		{CatDocs, trust.TierProvisional},
		{CatTests, trust.TierProvisional},
		{CatCode, trust.TierProvisional},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.cat), func(t *testing.T) {
			got := minTierForCategory(tc.cat)
			if got != tc.want {
				t.Errorf("minTierForCategory(%s) = %s, want %s", tc.cat, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TierRank
// ---------------------------------------------------------------------------

func TestTierRank(t *testing.T) {
	cases := []struct {
		tier trust.TrustTier
		want int
	}{
		{trust.TierProvisional, 0},
		{trust.TierObserved, 1},
		{trust.TierTrusted, 2},
		{trust.TierVeteran, 3},
		{trust.TrustTier("bogus"), -1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.tier), func(t *testing.T) {
			got := tierRank(tc.tier)
			if got != tc.want {
				t.Errorf("tierRank(%s) = %d, want %d", tc.tier, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CheckResult
// ---------------------------------------------------------------------------

func TestCheckResult_IsPassing(t *testing.T) {
	cases := []struct {
		status CheckStatus
		want   bool
	}{
		{CheckPass, true},
		{CheckSkipped, true},
		{CheckFail, false},
		{CheckWarning, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.status), func(t *testing.T) {
			r := CheckResult{Status: tc.status}
			if r.IsPassing() != tc.want {
				t.Errorf("IsPassing() = %v, want %v", r.IsPassing(), tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Timestamp sanity
// ---------------------------------------------------------------------------

func TestBundleTimestamp(t *testing.T) {
	b := validBundle()
	if b.Timestamp.IsZero() {
		t.Error("bundle timestamp should not be zero")
	}
	// Should be within the last few seconds.
	if time.Since(b.Timestamp) > 5*time.Second {
		t.Errorf("bundle timestamp is too old: %v", b.Timestamp)
	}
}

// ---------------------------------------------------------------------------
// contains helper
// ---------------------------------------------------------------------------

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(s) > 0 && containsStr(s, sub)))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
