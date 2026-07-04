// Command helix — coapproval.go
//
// `helix coapproval` exposes the co-approval gate (specs/SPECIFICATION.md
// §7.8) from the unified CLI. The gate requires 1 human + 1 trusted-agent
// (or 2 untrusted-agent) approvals before a PR can be merged. This file is
// a thin CLI wrapper around pkg/coapproval.CoApprovalGate — it parses
// reviewer evidence from JSON fixtures, builds a gate, runs
// CheckEligibility(), and renders the decision in both human-readable and
// machine-readable forms.
//
// Subcommands:
//
//	helix coapproval check   Evaluate one PR's co-approval state
//	helix coapproval status  Print gate thresholds + config
//	helix coapproval help    Show usage
//
// Reviewer evidence JSON shapes (see sample fixtures in coapproval_test.go):
//
//	--human-approvals  file containing []coapproval.Approval (Type=human)
//	--agent-approvals  file containing []coapproval.Approval (Type=agent)
//
// Each Approval must include reviewer_id, trust_score, timestamp (RFC3339),
// commit_sha, and is_veto (optional bool). The CLI rejects fixtures with
// missing or wrong-typed fields.
//
// Exit codes:
//
//	0 — Decision is ALLOWED (merge permitted)
//	1 — Decision is BLOCKED, NEEDS_HUMAN, or NEEDS_AGENT (gate not satisfied)
//	2 — Bad invocation (parse error, missing flag, malformed JSON, etc.)
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/coapproval"
)

// ============================================================================
// Constants — env-var names for each flag
// ============================================================================

const (
	envCoapprovalPR             = "HELIX_COAPPROVAL_PR"
	envCoapprovalCommitSHA      = "HELIX_COAPPROVAL_COMMIT_SHA"
	envCoapprovalHumanApprovals = "HELIX_COAPPROVAL_HUMAN_APPROVALS"
	envCoapprovalAgentApprovals = "HELIX_COAPPROVAL_AGENT_APPROVALS"
	envCoapprovalFormat         = "HELIX_COAPPROVAL_FORMAT"
	envCoapprovalPRURL          = "HELIX_COAPPROVAL_PR_URL"
)

// coapprovalFlags captures every CLI flag in one struct so Run functions
// can stay pure readers + callers.
type coapprovalFlags struct {
	subcommand     string
	prNumber       int
	commitSHA      string
	humanApprovals string
	agentApprovals string
	format         string
	prURL          string
}

// ============================================================================
// parseCoapprovalFlags — single flag.FS shared by check + status
// ============================================================================

// parseCoapprovalFlags builds a flag.FS, parses the args, and returns the
// populated flags plus a "showHelp" bool. showHelp=true means the user
// passed --help or "help" as the subcommand — the caller should print
// usage and exit 0.
//
// Args shape: [subcommand] [flags...]. If no subcommand is present we
// default to "check" (the most common use case).
func parseCoapprovalFlags(args []string, stdout, stderr io.Writer) (coapprovalFlags, string, bool, error) {
	// Subcommand is the first positional arg (defaults to "check").
	subcommand := "check"
	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		subcommand = rest[0]
		rest = rest[1:]
	}

	if subcommand == "help" || subcommand == "-h" || subcommand == "--help" {
		return coapprovalFlags{}, subcommand, true, nil
	}

	if subcommand != "check" && subcommand != "status" {
		return coapprovalFlags{}, subcommand, false, fmt.Errorf("unknown subcommand %q (expected: check | status | help)", subcommand)
	}

	fs := flag.NewFlagSet("helix-coapproval-"+subcommand, flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f coapprovalFlags
	f.subcommand = subcommand

	fs.IntVar(&f.prNumber, "pr", 0,
		"PR number being evaluated (env HELIX_COAPPROVAL_PR). Required for 'check'.")
	fs.StringVar(&f.commitSHA, "commit-sha", "",
		"Commit SHA the PR currently points at (env HELIX_COAPPROVAL_COMMIT_SHA). Required for 'check'.")
	fs.StringVar(&f.humanApprovals, "human-approvals", "",
		"Path to JSON file with []coapproval.Approval (humans) (env HELIX_COAPPROVAL_HUMAN_APPROVALS).")
	fs.StringVar(&f.agentApprovals, "agent-approvals", "",
		"Path to JSON file with []coapproval.Approval (agents) (env HELIX_COAPPROVAL_AGENT_APPROVALS).")
	fs.StringVar(&f.format, "format", "text",
		"Output format: text | json | markdown (env HELIX_COAPPROVAL_FORMAT). Default: text.")
	fs.StringVar(&f.prURL, "pr-url", "",
		"Optional human-readable PR URL for markdown output (env HELIX_COAPPROVAL_PR_URL).")

	// Apply env-var defaults BEFORE parsing so flag.Parse sees them.
	if f.prNumber == 0 {
		if v := os.Getenv(envCoapprovalPR); v != "" {
			if _, err := fmt.Sscanf(v, "%d", &f.prNumber); err != nil {
				return f, subcommand, false, fmt.Errorf("invalid %s=%q: %w", envCoapprovalPR, v, err)
			}
		}
	}
	if f.commitSHA == "" {
		f.commitSHA = os.Getenv(envCoapprovalCommitSHA)
	}
	if f.humanApprovals == "" {
		f.humanApprovals = os.Getenv(envCoapprovalHumanApprovals)
	}
	if f.agentApprovals == "" {
		f.agentApprovals = os.Getenv(envCoapprovalAgentApprovals)
	}
	if f.format == "text" {
		if v := os.Getenv(envCoapprovalFormat); v != "" {
			f.format = v
		}
	}
	if f.prURL == "" {
		f.prURL = os.Getenv(envCoapprovalPRURL)
	}

	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return f, subcommand, true, nil
		}
		return f, subcommand, false, err
	}

	return f, subcommand, false, nil
}

// ============================================================================
// runCoapprovalWithDryRun — invoked from main.go's dispatcher.
// Threads the global --dry-run flag through (currently a no-op since
// coapproval is a pure local computation, but the indirection matches
// the dispatch pattern so future network calls can honour it).
// ============================================================================

// runCoapprovalWithDryRun is the variant invoked by the unified `helix`
// CLI when the user passes the GLOBAL --dry-run flag (parsed in main.go
// before the coapproval sub-handler sees it).
func runCoapprovalWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	rc := runCoapproval(args, stdout, stderr)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}

// runCoapproval is the cmd/helix coapproval subcommand entry point.
// It returns an int (process exit code) rather than error so tests can
// inspect stdout/stderr without coupling to error wrapping.
func runCoapproval(args []string, stdout, stderr io.Writer) int {
	flags, subcommand, showHelp, err := parseCoapprovalFlags(args, stdout, stderr)
	if showHelp {
		printCoapprovalUsage(stdout, subcommand)
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "helix coapproval: parse:", err)
		printCoapprovalUsage(stderr, "")
		return 2
	}

	switch subcommand {
	case "check":
		return runCoapprovalCheck(flags, stdout, stderr)
	case "status":
		return runCoapprovalStatus(flags, stdout, stderr)
	}
	// Unreachable: parseCoapprovalFlags validates the subcommand.
	return 2
}

// ============================================================================
// check — evaluate a PR's co-approval state
// ============================================================================

// runCoapprovalCheck validates required flags, loads reviewer evidence,
// builds the gate, runs CheckEligibility, and renders the result.
//
// Required flags: --pr, --commit-sha, --human-approvals, --agent-approvals.
//
// Exit codes:
//
//	0 — Decision is ALLOWED
//	1 — Decision is BLOCKED, NEEDS_HUMAN, or NEEDS_AGENT
//	2 — Bad invocation (missing flag, malformed JSON, etc.)
func runCoapprovalCheck(f coapprovalFlags, stdout, stderr io.Writer) int {
	if f.prNumber == 0 {
		fmt.Fprintln(stderr, "helix coapproval check: --pr is required")
		printCoapprovalUsage(stderr, "check")
		return 2
	}
	if f.commitSHA == "" {
		fmt.Fprintln(stderr, "helix coapproval check: --commit-sha is required")
		printCoapprovalUsage(stderr, "check")
		return 2
	}
	if f.humanApprovals == "" {
		fmt.Fprintln(stderr, "helix coapproval check: --human-approvals is required (path to JSON fixture)")
		printCoapprovalUsage(stderr, "check")
		return 2
	}
	if f.agentApprovals == "" {
		fmt.Fprintln(stderr, "helix coapproval check: --agent-approvals is required (path to JSON fixture)")
		printCoapprovalUsage(stderr, "check")
		return 2
	}

	// Load both reviewer fixtures. Tolerate an empty agent fixture by
	// substituting an empty array — most PRs start with zero agent
	// approvals and we shouldn't require a stub file for that.
	humanApprovals, err := loadApprovals(f.humanApprovals)
	if err != nil {
		fmt.Fprintf(stderr, "helix coapproval check: load human approvals: %v\n", err)
		return 2
	}
	agentApprovals, err := loadApprovals(f.agentApprovals)
	if err != nil {
		fmt.Fprintf(stderr, "helix coapproval check: load agent approvals: %v\n", err)
		return 2
	}

	// Build the gate with a fresh PR URL (CLI doesn't talk to Forgejo).
	prURL := f.prURL
	if prURL == "" {
		prURL = fmt.Sprintf("local://pr/%d", f.prNumber)
	}
	gate := coapproval.NewCoApprovalGate(prURL, f.commitSHA)

	// Record every human approval. coapproval.NewCoApprovalGate stores by
	// reviewer_id — duplicate IDs overwrite silently, which matches the
	// library's behaviour (a reviewer updating their approval replaces the
	// old record).
	for _, a := range humanApprovals {
		a := a
		a.Type = coapproval.ReviewerHuman
		if err := gate.RecordApproval(a); err != nil {
			fmt.Fprintf(stderr, "helix coapproval check: record human approval %q: %v\n", a.ReviewerID, err)
			return 2
		}
	}
	for _, a := range agentApprovals {
		a := a
		a.Type = coapproval.ReviewerAgent
		if err := gate.RecordApproval(a); err != nil {
			fmt.Fprintf(stderr, "helix coapproval check: record agent approval %q: %v\n", a.ReviewerID, err)
			return 2
		}
	}

	result := gate.CheckEligibility()

	// Render in the requested format. Default "text" prints a short
	// summary + exit code; "json" prints the full structured result;
	// "markdown" prints a Forgejo-comment-style block ready to paste
	// into a PR comment.
	switch strings.ToLower(f.format) {
	case "json":
		return renderCoapprovalJSON(gate, result, stdout, stderr)
	case "markdown", "md":
		return renderCoapprovalMarkdown(f, result, stdout)
	default:
		return renderCoapprovalText(f, result, stdout)
	}
}

// ============================================================================
// status — print thresholds + config
// ============================================================================

// runCoapprovalStatus prints the gate's thresholds (approval expiry,
// trust thresholds, etc.) in either text or json form. The flags
// --pr / --commit-sha / --human-approvals / --agent-approvals are NOT
// required for status.
func runCoapprovalStatus(f coapprovalFlags, stdout, stderr io.Writer) int {
	cfg := struct {
		ApprovalExpiry            string `json:"approval_expiry"`
		TrustedAgentThreshold     int    `json:"trusted_agent_threshold"`
		UntrustedAgentRequiredCnt int    `json:"untrusted_agent_required_count"`
		VetoAgentThreshold        int    `json:"veto_agent_threshold"`
	}{
		ApprovalExpiry:            coapproval.ApprovalExpiry.String(),
		TrustedAgentThreshold:     coapproval.TrustedAgentThreshold,
		UntrustedAgentRequiredCnt: coapproval.UntrustedAgentRequiredCount,
		VetoAgentThreshold:        coapproval.VetoAgentThreshold,
	}
	switch strings.ToLower(f.format) {
	case "json":
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "helix coapproval status: marshal: %v\n", err)
			return 2
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	default:
		fmt.Fprintf(stdout, "Helix Co-Approval Gate — Configuration\n\n")
		fmt.Fprintf(stdout, "  Approval expiry:                  %s\n", cfg.ApprovalExpiry)
		fmt.Fprintf(stdout, "  Trusted agent threshold:          %d (>= this satisfies agent side alone)\n", cfg.TrustedAgentThreshold)
		fmt.Fprintf(stdout, "  Untrusted agent required count:   %d (collective when none hit trusted threshold)\n", cfg.UntrustedAgentRequiredCnt)
		fmt.Fprintf(stdout, "  Veto agent threshold:             %d (can override a single dissent)\n", cfg.VetoAgentThreshold)
		return 0
	}
}

// ============================================================================
// Renderers
// ============================================================================

// renderCoapprovalText prints a short text summary and returns the
// process exit code (0=ALLOWED, 1=blocked/needs-*).
func renderCoapprovalText(f coapprovalFlags, r *coapproval.EligibilityResult, stdout io.Writer) int {
	fmt.Fprintf(stdout, "Co-Approval Gate — PR #%d\n", f.prNumber)
	fmt.Fprintf(stdout, "  Decision:     %s\n", r.Decision)
	fmt.Fprintf(stdout, "  Reason:       %s\n", r.Reason)
	fmt.Fprintf(stdout, "  Human side:   %s (count=%d)\n", sideLabel(r.HumanOK), r.HumanCount)
	fmt.Fprintf(stdout, "  Agent side:   %s (count=%d)\n", sideLabel(r.AgentOK), r.AgentCount)
	if r.Vetoed {
		fmt.Fprintf(stdout, "  Vetoed by:    %s\n", r.VetoBy)
	}
	if n := len(r.Approvals); n > 0 {
		fmt.Fprintf(stdout, "  Approvals (%d):\n", n)
		for _, a := range r.Approvals {
			fmt.Fprintf(stdout, "    - %s [%s] trust=%d at %s\n",
				a.ReviewerID, a.Type, a.TrustScore, a.Timestamp.Format(time.RFC3339))
		}
	} else {
		fmt.Fprintf(stdout, "  Approvals:    none\n")
	}
	if r.IsMergeable() {
		fmt.Fprintln(stdout, "\nResult: ALLOWED — merge permitted.")
		return 0
	}
	fmt.Fprintln(stdout, "\nResult: BLOCKED — merge not permitted.")
	return 1
}

// renderCoapprovalJSON prints the full EligibilityResult as JSON, plus
// the gate's PR URL + commit SHA + approval counts. Always exits 0
// (the JSON consumer decides what to do with the decision).
func renderCoapprovalJSON(gate *coapproval.CoApprovalGate, r *coapproval.EligibilityResult, stdout, stderr io.Writer) int {
	out := struct {
		PRURL         string                        `json:"pr_url"`
		CommitSHA     string                        `json:"commit_sha"`
		ApprovalCount int                           `json:"approval_count"`
		VetoCount     int                           `json:"veto_count"`
		Eligibility   *coapproval.EligibilityResult `json:"eligibility"`
	}{
		PRURL:         gate.PRURL(),
		CommitSHA:     gate.CommitSHA(),
		ApprovalCount: gate.ApprovalCount(),
		VetoCount:     gate.VetoCount(),
		Eligibility:   r,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "helix coapproval check: marshal: %v\n", err)
		return 2
	}
	fmt.Fprintln(stdout, string(data))
	if r.IsMergeable() {
		return 0
	}
	return 1
}

// renderCoapprovalMarkdown prints a Forgejo-comment-style markdown block
// that's ready to paste into a PR comment. Always exits 0 (the markdown
// is informational — consumers decide the merge based on the underlying
// decision).
func renderCoapprovalMarkdown(f coapprovalFlags, r *coapproval.EligibilityResult, stdout io.Writer) int {
	var b strings.Builder
	fmt.Fprintf(&b, "## Co-Approval Gate — PR #%d\n\n", f.prNumber)
	fmt.Fprintf(&b, "**Decision:** `%s`\n\n", r.Decision)
	fmt.Fprintf(&b, "%s\n\n", r.Reason)
	fmt.Fprintf(&b, "| Side | Status | Count |\n")
	fmt.Fprintf(&b, "|------|--------|-------|\n")
	fmt.Fprintf(&b, "| Human   | %s | %d |\n", sideLabel(r.HumanOK), r.HumanCount)
	fmt.Fprintf(&b, "| Agent   | %s | %d |\n", sideLabel(r.AgentOK), r.AgentCount)
	if r.Vetoed {
		fmt.Fprintf(&b, "\n**Vetoed by:** %s\n", r.VetoBy)
	}
	if len(r.Approvals) > 0 {
		fmt.Fprintf(&b, "\n### Approvals\n\n")
		for _, a := range r.Approvals {
			fmt.Fprintf(&b, "- `%s` (%s, trust=%d) at %s\n",
				a.ReviewerID, a.Type, a.TrustScore, a.Timestamp.Format(time.RFC3339))
		}
	}
	fmt.Fprintln(stdout, b.String())
	return 0
}

// ============================================================================
// Helpers
// ============================================================================

// loadApprovals reads a JSON file containing []coapproval.Approval and
// returns the parsed slice. The empty-array case ("[]" or "") is treated
// as a successful load with zero approvals.
func loadApprovals(path string) ([]coapproval.Approval, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "[]" || trimmed == "null" {
		return nil, nil
	}
	var out []coapproval.Approval
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return out, nil
}

// sideLabel renders the human/agent OK flag as a short status string.
func sideLabel(ok bool) string {
	if ok {
		return "satisfied"
	}
	return "pending"
}

// printCoapprovalUsage prints the top-level (subcommand="") usage when
// the user runs `helix coapproval` with no args or asks for help. When
// subcommand is set ("check" / "status") it prints the subcommand's
// specific flags.
func printCoapprovalUsage(w io.Writer, subcommand string) {
	switch subcommand {
	case "check":
		fmt.Fprintf(w, `helix coapproval check — evaluate one PR's co-approval state

USAGE:
  helix coapproval check --pr <n> --commit-sha <sha> \
    --human-approvals <file.json> --agent-approvals <file.json> \
    [--format text|json|markdown] [--pr-url <url>]

FLAGS:
  --pr                 PR number being evaluated (required)
  --commit-sha         Commit SHA the PR currently points at (required)
  --human-approvals    Path to JSON file with []coapproval.Approval (humans) (required)
  --agent-approvals    Path to JSON file with []coapproval.Approval (agents) (required)
  --format             Output format: text | json | markdown (default text)
  --pr-url             Optional human-readable PR URL for markdown output

ENV VARS (override defaults):
  HELIX_COAPPROVAL_PR
  HELIX_COAPPROVAL_COMMIT_SHA
  HELIX_COAPPROVAL_HUMAN_APPROVALS
  HELIX_COAPPROVAL_AGENT_APPROVALS
  HELIX_COAPPROVAL_FORMAT
  HELIX_COAPPROVAL_PR_URL

EXIT CODES:
  0 — Decision is ALLOWED (merge permitted)
  1 — Decision is BLOCKED / NEEDS_HUMAN / NEEDS_AGENT (gate not satisfied)
  2 — Bad invocation (missing flag, malformed JSON, etc.)

EXAMPLE JSON FIXTURE (human-approvals.json):
  [
    {
      "reviewer_id": "alice",
      "trust_score": 100,
      "timestamp": "2026-07-04T10:00:00Z",
      "commit_sha": "abc123"
    }
  ]
`)
	case "status":
		fmt.Fprintf(w, `helix coapproval status — print gate thresholds + config

USAGE:
  helix coapproval status [--format text|json]

Prints the approval expiry, trusted-agent threshold, untrusted-agent
required count, and veto threshold for the co-approval gate.
`)
	default:
		fmt.Fprintf(w, `helix coapproval — co-approval gate CLI (specs/SPECIFICATION.md §7.8)

USAGE:
  helix coapproval <subcommand> [flags...]

SUBCOMMANDS:
  check    Evaluate one PR's co-approval state
  status   Print gate thresholds + config
  help     Show this message

Run 'helix coapproval <subcommand> --help' for subcommand-specific flags.
`)
	}
}
