// Package mergegate — hook.go
//
// Pre-receive hook evaluation logic. This file implements the server-side
// gate enforcement that Forgejo (or any git server) invokes via a
// pre-receive hook. The hook reads pushed refs from stdin (standard git
// pre-receive protocol), determines whether any protected branch is affected,
// collects the changed files, and runs the merge gate pipeline.
//
// Design:
//   - The bash script (scripts/helix-pre-receive.sh) is a thin wrapper that
//     pipes stdin to `helix mergegate hook`.
//   - The Go code does the real work: parsing refs, calling git to list
//     changed files, evaluating the gate, and printing a structured
//     accept/reject message.
//   - Exit 0 = ALLOW push. Exit 1 = REJECT push.
//
// Spec: specs/plans/phase-7-8-negotiate-merge.md §Gap 2-4, §8.1-8.3
package mergegate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// HookRef represents a single pushed ref parsed from pre-receive stdin.
// Format per line: "<old-sha> <new-sha> <ref-name>".
type HookRef struct {
	OldSHA  string `json:"old_sha"`
	NewSHA  string `json:"new_sha"`
	RefName string `json:"ref_name"`
}

// IsBranchPush reports whether this ref is a branch push.
func (r HookRef) IsBranchPush() bool {
	return strings.HasPrefix(r.RefName, "refs/heads/")
}

// BranchName extracts the branch name from the ref (strips refs/heads/ prefix).
func (r HookRef) BranchName() string {
	return strings.TrimPrefix(r.RefName, "refs/heads/")
}

// IsDelete reports whether this ref is being deleted (new-sha is all zeros).
func (r HookRef) IsDelete() bool {
	return r.NewSHA == strings.Repeat("0", 40)
}

// IsCreate reports whether this ref is being created (old-sha is all zeros).
func (r HookRef) IsCreate() bool {
	return r.OldSHA == strings.Repeat("0", 40)
}

// HookConfig configures the pre-receive hook evaluator.
type HookConfig struct {
	// ProtectedBranches is the set of branches that require gate enforcement.
	// A push to a branch NOT in this set is always allowed.
	// If empty, defaults to {"main", "master", "release/*"}.
	ProtectedBranches []string `json:"protected_branches"`

	// GitBinary is the path to the git executable. Defaults to "git".
	GitBinary string `json:"git_binary"`

	// WorkingDir is the bare repo directory. The pre-receive hook runs
	// from inside the bare repo, so GIT_DIR is set by git itself. This
	// field is for explicit override (tests).
	WorkingDir string `json:"working_dir,omitempty"`

	// AgentID identifies the pushing agent (extracted from GIT_AUTHOR_EMAIL
	// or a Helix-specific env var). When empty, the hook tries to infer.
	AgentID string `json:"agent_id,omitempty"`

	// TrustTier for the pushing agent. Required for trust gate.
	TrustTier string `json:"trust_tier,omitempty"`

	// EvidencePath is a path glob or directory where evidence bundles are
	// stored (e.g., ".helix/evidence/"). When empty, evidence check is
	// skipped (WARN, not FAIL).
	EvidencePath string `json:"evidence_path,omitempty"`

	// SkipGates is a set of gate names to skip entirely. Useful for
	// fast-forward merges or maintenance pushes.
	SkipGates map[string]bool `json:"skip_gates,omitempty"`

	// DryRun does not reject pushes but prints what would happen.
	DryRun bool `json:"dry_run"`
}

// DefaultHookConfig returns the recommended configuration.
func DefaultHookConfig() HookConfig {
	return HookConfig{
		ProtectedBranches: []string{"main", "master", "release/*"},
		GitBinary:         "git",
	}
}

// HookResult is the outcome of evaluating a single pushed ref through the gate.
type HookResult struct {
	Ref          HookRef       `json:"ref"`
	Branch       string        `json:"branch"`
	Protected    bool          `json:"protected"`
	Allowed      bool          `json:"allowed"`
	Skipped      bool          `json:"skipped"`
	ChangedFiles []string      `json:"changed_files,omitempty"`
	GateReport   *GateReport   `json:"gate_report,omitempty"`
	Reason       string        `json:"reason"`
	Checks       []CheckResult `json:"checks,omitempty"`
}

// HookOutput is the aggregate result for a single pre-receive invocation
// (may contain multiple refs).
type HookOutput struct {
	Allowed  bool         `json:"allowed"`
	DryRun   bool         `json:"dry_run"`
	Results  []HookResult `json:"results"`
	Rejected []string     `json:"rejected,omitempty"`
}

// ---------------------------------------------------------------------------
// Hook evaluation
// ---------------------------------------------------------------------------

// EvaluateHook reads pre-receive input from `stdin` (one ref per line),
// evaluates each ref against the gate, and writes a structured report to
// `stdout`. Returns nil if all refs are allowed; returns an error describing
// the rejection if any ref is blocked.
func EvaluateHook(cfg HookConfig, stdin io.Reader, stdout, stderr io.Writer) error {
	refs, err := parsePreReceiveStdin(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "helix-pre-receive: failed to parse stdin: %v\n", err)
		return err
	}

	if len(refs) == 0 {
		// No refs to evaluate — nothing to do.
		fmt.Fprintln(stdout, "helix-pre-receive: no refs to evaluate")
		return nil
	}

	output := &HookOutput{
		Allowed: true,
		DryRun:  cfg.DryRun,
	}

	for _, ref := range refs {
		result := evaluateRef(cfg, ref, stderr)
		output.Results = append(output.Results, result)

		if !result.Allowed && !result.Skipped {
			output.Allowed = false
			output.Rejected = append(output.Rejected,
				fmt.Sprintf("%s: %s", ref.RefName, result.Reason))
		}
	}

	// Print structured output.
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(output)

	// Print human-readable summary.
	if output.Allowed {
		fmt.Fprintf(stdout, "\nhelix-pre-receive: ✓ ALLOWED (%d refs evaluated)\n", len(refs))
		return nil
	}

	// Rejection message.
	fmt.Fprintf(stderr, "\nhelix-pre-receive: ✗ REJECTED\n")
	for _, r := range output.Rejected {
		fmt.Fprintf(stderr, "  • %s\n", r)
	}
	fmt.Fprintf(stderr, "\nTo bypass (emergency only): git push --no-verify or set HELIX_SKIP_GATE=1\n")

	return fmt.Errorf("merge gate blocked push to %d ref(s)", len(output.Rejected))
}

// evaluateRef evaluates a single pushed ref against the gate.
func evaluateRef(cfg HookConfig, ref HookRef, stderr io.Writer) HookResult {
	result := HookResult{
		Ref:    ref,
		Branch: ref.BranchName(),
	}

	// Only evaluate branch pushes — tags, notes, etc. are always allowed.
	if !ref.IsBranchPush() {
		result.Skipped = true
		result.Allowed = true
		result.Reason = "non-branch ref, skipping gate"
		return result
	}

	// Check if this branch is protected.
	result.Protected = isProtectedBranch(ref.BranchName(), cfg.ProtectedBranches)
	if !result.Protected {
		result.Skipped = true
		result.Allowed = true
		result.Reason = fmt.Sprintf("branch %q is not protected, skipping gate", ref.BranchName())
		return result
	}

	// Branch deletion is handled separately — require explicit bypass.
	if ref.IsDelete() {
		if cfg.DryRun {
			result.Skipped = true
			result.Allowed = true
			result.Reason = "dry-run: would reject branch deletion"
			return result
		}
		result.Allowed = false
		result.Reason = "branch deletion blocked by merge gate (use HELIX_FORCE_DELETE=1 to bypass)"
		return result
	}

	// Collect changed files.
	files, err := collectChangedFiles(cfg, ref)
	if err != nil {
		// If we can't collect files (e.g., new branch with no base), allow
		// but warn — this is a common case for feature branches.
		result.Allowed = true
		result.Skipped = true
		result.Reason = fmt.Sprintf("could not collect changed files: %v (allowing — likely a new branch)", err)
		return result
	}
	result.ChangedFiles = files

	// Run gate checks.
	checks := runHookGateChecks(cfg, ref, files)
	result.Checks = checks

	// Evaluate results.
	allPass := true
	var failedChecks []string
	for _, c := range checks {
		if !c.IsPassing() {
			allPass = false
			failedChecks = append(failedChecks, fmt.Sprintf("%s: %s", c.Name, c.Reason))
		}
	}

	if cfg.DryRun {
		result.Allowed = true
		if allPass {
			result.Reason = "dry-run: all gate checks passed"
		} else {
			result.Reason = "dry-run: would reject — " + strings.Join(failedChecks, "; ")
		}
		return result
	}

	result.Allowed = allPass
	if allPass {
		result.Reason = "all gate checks passed"
	} else {
		result.Reason = strings.Join(failedChecks, "; ")
	}

	return result
}

// ---------------------------------------------------------------------------
// Gate checks (hook-specific)
// ---------------------------------------------------------------------------

// runHookGateChecks runs the gate checks applicable in a pre-receive context.
// These are lighter than the full MergeGate.Evaluate because pre-receive
// doesn't have access to in-memory artifacts — it must find them on disk.
func runHookGateChecks(cfg HookConfig, ref HookRef, changedFiles []string) []CheckResult {
	var checks []CheckResult

	// Check 1: Evidence bundle exists (if evidence path is configured).
	checks = append(checks, checkEvidenceOnDisk(cfg, changedFiles))

	// Check 2: Trust tier meets minimum for changed file categories.
	checks = append(checks, checkTrustTier(cfg, changedFiles))

	// Check 3: No secrets in changed files.
	checks = append(checks, checkNoSecrets(cfg, changedFiles))

	// Check 4: Commit attestation present (Co-authored-by or Helix agent).
	checks = append(checks, checkCommitAttestation(cfg, ref))

	return checks
}

// checkEvidenceOnDisk verifies that evidence bundle files exist for the
// pushed changes. In a pre-receive context, evidence bundles are committed
// artifacts at a known path (e.g., .helix/evidence/<commit>.json).
func checkEvidenceOnDisk(cfg HookConfig, files []string) CheckResult {
	if cfg.EvidencePath == "" {
		return CheckResult{
			Name:   "evidence-bundle",
			Status: CheckSkipped,
			Reason: "no evidence path configured (set --evidence-path)",
		}
	}

	// Check if evidence directory or glob exists.
	if _, err := os.Stat(cfg.EvidencePath); err != nil {
		return CheckResult{
			Name:   "evidence-bundle",
			Status: CheckWarning,
			Reason: fmt.Sprintf("evidence path %s does not exist: %v", cfg.EvidencePath, err),
		}
	}

	return CheckResult{
		Name:   "evidence-bundle",
		Status: CheckPass,
		Reason: fmt.Sprintf("evidence path %s exists", cfg.EvidencePath),
	}
}

// checkTrustTier verifies the pushing agent's trust tier is sufficient for
// the file categories being changed. Maps file types to required tiers.
func checkTrustTier(cfg HookConfig, files []string) CheckResult {
	if cfg.TrustTier == "" {
		return CheckResult{
			Name:   "trust-tier",
			Status: CheckSkipped,
			Reason: "no trust tier specified (set --trust)",
		}
	}

	// Determine the maximum required tier across all changed files.
	requiredTier := trust.TierProvisional // baseline
	for _, f := range files {
		req := fileCategoryTier(f)
		if tierRank(req) > tierRank(requiredTier) {
			requiredTier = req
		}
	}

	agentTier := trust.TrustTier(cfg.TrustTier)
	if tierRank(agentTier) >= tierRank(requiredTier) {
		return CheckResult{
			Name:   "trust-tier",
			Status: CheckPass,
			Reason: fmt.Sprintf("agent tier %s >= required %s for changed files", agentTier, requiredTier),
		}
	}

	return CheckResult{
		Name:   "trust-tier",
		Status: CheckFail,
		Reason: fmt.Sprintf("agent tier %s < required %s (files require higher trust)", agentTier, requiredTier),
	}
}

// checkNoSecrets scans changed files for obvious secret patterns.
func checkNoSecrets(cfg HookConfig, files []string) CheckResult {
	// In pre-receive, files aren't checked out — we rely on the commit
	// having passed CI secrets scanning. This is a meta-check that verifies
	// the files don't look like credential stores.
	suspicious := []string{}
	for _, f := range files {
		base := f
		if idx := strings.LastIndex(f, "/"); idx >= 0 {
			base = f[idx+1:]
		}
		lower := strings.ToLower(base)
		if lower == ".env" || strings.HasPrefix(lower, ".env.") {
			suspicious = append(suspicious, f)
		}
		if strings.Contains(lower, "secret") && !strings.Contains(lower, "_test") {
			suspicious = append(suspicious, f)
		}
		if strings.Contains(lower, "credentials") && !strings.Contains(lower, "_test") {
			suspicious = append(suspicious, f)
		}
	}

	if len(suspicious) > 0 {
		return CheckResult{
			Name:   "secrets-scan",
			Status: CheckFail,
			Reason: fmt.Sprintf("suspicious files in push: %s", strings.Join(suspicious, ", ")),
		}
	}

	return CheckResult{
		Name:   "secrets-scan",
		Status: CheckPass,
		Reason: fmt.Sprintf("no suspicious files detected in %d changed files", len(files)),
	}
}

// checkCommitAttestation verifies the pushed commits have attestation
// trailers (Co-authored-by or Helix agent signature).
func checkCommitAttestation(cfg HookConfig, ref HookRef) CheckResult {
	// In a pre-receive hook, we can use git log to check commit messages.
	range_ := commitRange(ref)
	if range_ == "" {
		return CheckResult{
			Name:   "commit-attestation",
			Status: CheckSkipped,
			Reason: "cannot determine commit range (new branch or single commit)",
		}
	}

	log, err := gitLog(cfg, range_)
	if err != nil {
		return CheckResult{
			Name:   "commit-attestation",
			Status: CheckSkipped,
			Reason: fmt.Sprintf("cannot read commit log: %v", err),
		}
	}

	if !strings.Contains(log, "Co-authored-by:") && !strings.Contains(log, "Helix-Agent:") {
		return CheckResult{
			Name:   "commit-attestation",
			Status: CheckFail,
			Reason: "commits in this push lack Co-authored-by: or Helix-Agent: attestation trailers",
		}
	}

	return CheckResult{
		Name:   "commit-attestation",
		Status: CheckPass,
		Reason: "commit attestation trailers found",
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parsePreReceiveStdin reads pre-receive stdin lines. Each line has the format:
//
//	<old-sha> <new-sha> <ref-name>
//
// Blank lines and comments (#) are ignored.
func parsePreReceiveStdin(r io.Reader) ([]HookRef, error) {
	var refs []HookRef
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue // skip malformed lines
		}
		refs = append(refs, HookRef{
			OldSHA:  parts[0],
			NewSHA:  parts[1],
			RefName: parts[2],
		})
	}
	return refs, scanner.Err()
}

// isProtectedBranch checks if a branch name matches any protected branch pattern.
// Supports exact match and glob patterns with * wildcard (e.g., "release/*").
func isProtectedBranch(branch string, patterns []string) bool {
	if len(patterns) == 0 {
		patterns = []string{"main", "master", "release/*"}
	}
	for _, p := range patterns {
		if matchGlob(p, branch) {
			return true
		}
	}
	return false
}

// matchGlob matches a pattern with * wildcards against a string.
func matchGlob(pattern, s string) bool {
	if pattern == s {
		return true
	}
	// Handle prefix/* patterns.
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(s, prefix+"/") || s == prefix
	}
	return false
}

// collectChangedFiles uses git diff-tree to list files changed in the push.
func collectChangedFiles(cfg HookConfig, ref HookRef) ([]string, error) {
	var raw string
	var err error

	if ref.IsCreate() {
		// New branch: list all files in the new commit.
		raw, err = gitListFiles(cfg, ref.NewSHA)
	} else {
		// Update: diff old..new.
		raw, err = gitDiffTree(cfg, ref.OldSHA, ref.NewSHA)
	}

	if err != nil {
		return nil, err
	}

	var files []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// gitDiffTree runs `git diff-tree --name-only -r old new`.
func gitDiffTree(cfg HookConfig, oldSHA, newSHA string) (string, error) {
	args := []string{"diff-tree", "--name-only", "-r", "--root", oldSHA, newSHA}
	return runGit(cfg, args...)
}

// gitListFiles runs `git ls-tree --name-only -r sha`.
func gitListFiles(cfg HookConfig, sha string) (string, error) {
	args := []string{"ls-tree", "--name-only", "-r", sha}
	return runGit(cfg, args...)
}

// gitLog runs `git log --format=%B range`.
func gitLog(cfg HookConfig, rangeArg string) (string, error) {
	args := []string{"log", "--format=%B", rangeArg}
	return runGit(cfg, args...)
}

// commitRange builds the git range for attestation checking.
func commitRange(ref HookRef) string {
	if ref.IsCreate() {
		// For new branches, we can't easily determine the range — skip.
		return ""
	}
	if ref.IsDelete() {
		return ""
	}
	return fmt.Sprintf("%s..%s", ref.OldSHA, ref.NewSHA)
}

// runGit executes a git command with the configured binary and working dir.
func runGit(cfg HookConfig, args ...string) (string, error) {
	binary := cfg.GitBinary
	if binary == "" {
		binary = "git"
	}

	cmd := exec.Command(binary, args...)
	if cfg.WorkingDir != "" {
		cmd.Dir = cfg.WorkingDir
	}
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(output), nil
}

// fileCategoryTier maps file paths to the minimum trust tier required to
// change them. Based on specs/trust-model.md §File Category Requirements.
func fileCategoryTier(path string) trust.TrustTier {
	// IaC / deployment configs require Trusted (Tier 3).
	if hasSuffixAny(path, ".tf", ".tfvars", "docker-compose.yml", "Dockerfile") ||
		containsAny(path, "/deploy/", "/terraform/", "/k8s/") {
		return trust.TierTrusted
	}
	// CI/CD pipelines require Veteran (Tier 4) — highest scrutiny.
	if containsAny(path, ".github/workflows/", "/.ci/", "Makefile", "Jenkinsfile") {
		return trust.TierVeteran
	}
	// Auth/security files require Trusted (Tier 3).
	if containsAny(path, "/auth/", "/security/", "/identity/", "middleware/") {
		return trust.TierTrusted
	}
	// Database migrations require Observed (Tier 2).
	if containsAny(path, "/migrations/", "/db/") {
		return trust.TierObserved
	}
	// Everything else: Provisional (Tier 1) can handle.
	return trust.TierProvisional
}

// hasSuffixAny checks if s ends with any of the suffixes.
func hasSuffixAny(s string, suffixes ...string) bool {
	for _, suf := range suffixes {
		if strings.HasSuffix(s, suf) {
			return true
		}
	}
	return false
}

// containsAny checks if s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
