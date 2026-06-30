package forgejo

// pr_status.go implements Forgejo PR status integration per
// specs/cross-component-wiring.md §2.1 (Forgejo → Chimera PR review) and
// specs/deployment.md §5.
//
// PRStatusManager posts review verdicts and deployment status as Forgejo
// PR comments and commit statuses. It renders Chimera verdicts as structured
// markdown, sets CI-style status checks on commits, and shows canary/shadow
// deployment progress inline.

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ============================================================================
// Commit Status Types
// ============================================================================

// CommitStatus represents a CI-style status check on a git commit.
// Per Forgejo API: POST /repos/{owner}/{repo}/statuses/{sha}
type CommitStatusState string

const (
	StatusStatePending CommitStatusState = "pending"
	StatusStateSuccess CommitStatusState = "success"
	StatusStateFailure CommitStatusState = "failure"
	StatusStateError   CommitStatusState = "error"
	StatusStateWarning CommitStatusState = "warning"
)

// CommitStatus is the body for posting a commit status check.
type CommitStatus struct {
	State       CommitStatusState `json:"state"`
	TargetURL   string            `json:"target_url,omitempty"`
	Description string            `json:"description,omitempty"`
	Context     string            `json:"context"`
}

// ============================================================================
// PR Comment Types
// ============================================================================

// ReviewVerdict captures the output of a Chimera PR review for rendering.
type ReviewVerdict struct {
	Decision     string    // "APPROVE", "REJECT", "REQUEST_CHANGES"
	Confidence   float64   // 0.0-1.0
	ModelsUsed   []string  // model IDs that participated
	Findings     []string  // issue descriptions
	Consensus    string    // "unanimous", "majority", "divergent"
	BiasStripped bool      // whether bias-stripping was applied
	Timestamp    time.Time `json:"timestamp"`
}

// DeploymentStatus captures canary/shadow deployment progress for display.
type DeploymentStatus struct {
	Phase        string  // "shadow", "canary", "promoted", "rolled_back"
	TrafficPct   float64 // percentage of traffic routed
	AgentID      string
	ContractName string
	StartTime    time.Time
	Progress     float64 // 0.0-1.0
	Breaches     int
	LastError    string
}

// ============================================================================
// PRStatusManager
// ============================================================================

// PRStatusManager posts review verdicts and deployment status as Forgejo
// PR comments and commit statuses.
type PRStatusManager struct {
	client *Client
}

// NewPRStatusManager creates a PRStatusManager wrapping the given client.
func NewPRStatusManager(client *Client) *PRStatusManager {
	return &PRStatusManager{client: client}
}

// ============================================================================
// PostReviewComment — renders Chimera verdict as structured markdown
// ============================================================================

// PostReviewComment renders a ReviewVerdict as a structured markdown comment
// and posts it to the PR.
func (m *PRStatusManager) PostReviewComment(ctx context.Context, owner, repo string, prNumber int64, verdict *ReviewVerdict) error {
	body := FormatReviewComment(verdict)
	return m.client.CreatePRReview(ctx, owner, repo, prNumber, CreatePRReviewRequest{
		Body:  body,
		Event: verdictToReviewEvent(verdict.Decision),
	})
}

// FormatReviewComment renders a ReviewVerdict as structured markdown.
func FormatReviewComment(v *ReviewVerdict) string {
	var sb strings.Builder

	icon := verdictIcon(v.Decision)
	sb.WriteString(fmt.Sprintf("### %s Helix Review: `%s`\n\n", icon, v.Decision))

	sb.WriteString(fmt.Sprintf("**Confidence:** %.1f%%  \n", v.Confidence*100))
	sb.WriteString(fmt.Sprintf("**Consensus:** %s  \n", v.Consensus))
	sb.WriteString(fmt.Sprintf("**Models:** %s  \n", strings.Join(v.ModelsUsed, ", ")))
	if v.BiasStripped {
		sb.WriteString("**Bias Stripping:** ✅ Applied\n")
	}
	sb.WriteString(fmt.Sprintf("**Timestamp:** %s\n\n", v.Timestamp.Format(time.RFC3339)))

	if len(v.Findings) > 0 {
		sb.WriteString("#### Findings\n\n")
		for i, f := range v.Findings {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, f))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("---\n*Posted by Helix Adversarial Review Pipeline*\n")
	return sb.String()
}

// verdictIcon returns the emoji for a verdict decision.
func verdictIcon(decision string) string {
	switch strings.ToUpper(decision) {
	case "APPROVE":
		return "✅"
	case "REJECT":
		return "❌"
	case "REQUEST_CHANGES":
		return "⚠️"
	default:
		return "ℹ️"
	}
}

// verdictToReviewEvent maps a verdict decision to a Forgejo review event type.
func verdictToReviewEvent(decision string) string {
	switch strings.ToUpper(decision) {
	case "APPROVE":
		return "APPROVED"
	case "REJECT":
		return "REQUEST_CHANGES"
	default:
		return "COMMENT"
	}
}

// ============================================================================
// PostCommitStatus — sets CI-style status check on commits
// ============================================================================

// PostCommitStatus sets a CI-style status check on a commit.
// Per Forgejo API: POST /repos/{owner}/{repo}/statuses/{sha}
func (m *PRStatusManager) PostCommitStatus(ctx context.Context, owner, repo, sha string, status CommitStatus) error {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/statuses/%s",
		escapePath(owner), escapePath(repo), escapePath(sha))
	return m.client.doRequest(ctx, "POST", path, status, nil)
}

// PostReviewStatus sets a commit status reflecting the review verdict.
func (m *PRStatusManager) PostReviewStatus(ctx context.Context, owner, repo, sha string, verdict *ReviewVerdict) error {
	state := StatusStateSuccess
	desc := fmt.Sprintf("Review: %s (%.0f%% confidence)", verdict.Decision, verdict.Confidence*100)

	switch strings.ToUpper(verdict.Decision) {
	case "REJECT":
		state = StatusStateFailure
	case "REQUEST_CHANGES":
		state = StatusStateWarning
	}

	return m.PostCommitStatus(ctx, owner, repo, sha, CommitStatus{
		State:       state,
		Description: desc,
		Context:     "helix/review",
	})
}

// PostDeploymentStatus sets a commit status showing canary/shadow progress.
func (m *PRStatusManager) PostDeploymentStatus(ctx context.Context, owner, repo, sha string, dep *DeploymentStatus) error {
	state := StatusStatePending
	desc := fmt.Sprintf("Deployment: %s (%.0f%% traffic)", dep.Phase, dep.TrafficPct)

	switch dep.Phase {
	case "promoted":
		state = StatusStateSuccess
		desc = fmt.Sprintf("Deployment: promoted (100%% traffic)")
	case "rolled_back":
		state = StatusStateError
		desc = fmt.Sprintf("Deployment: rolled back (%d breaches)", dep.Breaches)
	case "canary":
		if dep.Breaches > 0 {
			state = StatusStateWarning
			desc = fmt.Sprintf("Deployment: canary %d%% traffic — %d breach(es)", int(dep.TrafficPct), dep.Breaches)
		} else {
			state = StatusStatePending
			desc = fmt.Sprintf("Deployment: canary %d%% traffic", int(dep.TrafficPct))
		}
	}

	return m.PostCommitStatus(ctx, owner, repo, sha, CommitStatus{
		State:       state,
		Description: desc,
		Context:     "helix/deployment",
	})
}

// ============================================================================
// ParsePRReviews — reads existing review comments
// ============================================================================

// ParsedReview extracts structured information from a raw PR review comment.
type ParsedReview struct {
	Decision   string  // extracted verdict or empty
	Confidence float64 // 0.0-1.0 or 0 if not found
	IsHelix    bool    // true if this is a Helix-generated review
	Body       string
}

// ParsePRReviews reads existing PR review comments and extracts Helix review data.
func ParsePRReviews(reviews []PRReview) []ParsedReview {
	var parsed []ParsedReview
	for _, r := range reviews {
		p := ParseReviewBody(r.Body)
		if p != nil {
			parsed = append(parsed, *p)
		}
	}
	return parsed
}

// ParseReviewBody extracts structured data from a single review body.
func ParseReviewBody(body string) *ParsedReview {
	p := &ParsedReview{
		Body: body,
	}

	// Check if this is a Helix-generated review.
	if strings.Contains(body, "Helix") {
		p.IsHelix = true
	}

	// Extract decision from header (e.g., "### ✅ Helix Review: `APPROVE`").
	if idx := strings.Index(body, "Helix Review: `"); idx >= 0 {
		rest := body[idx+len("Helix Review: `"):]
		end := strings.Index(rest, "`")
		if end >= 0 {
			p.Decision = rest[:end]
		}
	}

	// Extract confidence (e.g., "**Confidence:** 95.0%").
	if idx := strings.Index(body, "**Confidence:**"); idx >= 0 {
		rest := body[idx+len("**Confidence:**"):]
		// Find the percentage.
		pctIdx := strings.Index(rest, "%")
		if pctIdx >= 0 {
			numStr := strings.TrimSpace(rest[:pctIdx])
			// Try to parse as float.
			var pct float64
			if _, err := fmt.Sscanf(numStr, "%f", &pct); err == nil {
				p.Confidence = pct / 100.0
			}
		}
	}

	// If no structured data was found and it's not Helix, skip it.
	if !p.IsHelix && p.Decision == "" {
		return nil
	}

	return p
}

// ============================================================================
// Deployment Comment Formatting
// ============================================================================

// FormatDeploymentComment renders a DeploymentStatus as structured markdown.
func FormatDeploymentComment(dep *DeploymentStatus) string {
	var sb strings.Builder

	phaseIcon := deploymentPhaseIcon(dep.Phase)
	sb.WriteString(fmt.Sprintf("### %s Deployment: `%s`\n\n", phaseIcon, dep.Phase))

	sb.WriteString(fmt.Sprintf("**Agent:** `%s`  \n", dep.AgentID))
	sb.WriteString(fmt.Sprintf("**Contract:** `%s`  \n", dep.ContractName))
	sb.WriteString(fmt.Sprintf("**Traffic:** %.0f%%\n", dep.TrafficPct))

	progressBar := renderProgressBar(dep.Progress)
	sb.WriteString(fmt.Sprintf("**Progress:** %s (%.0f%%)\n", progressBar, dep.Progress*100))
	sb.WriteString(fmt.Sprintf("**Started:** %s\n", dep.StartTime.Format(time.RFC3339)))

	if dep.Breaches > 0 {
		sb.WriteString(fmt.Sprintf("\n⚠️ **Breaches:** %d\n", dep.Breaches))
	}
	if dep.LastError != "" {
		sb.WriteString(fmt.Sprintf("\n❌ **Last Error:** %s\n", dep.LastError))
	}

	sb.WriteString("\n---\n*Posted by Helix Production Verification*\n")
	return sb.String()
}

// PostDeploymentComment posts a deployment status as a PR comment.
func (m *PRStatusManager) PostDeploymentComment(ctx context.Context, owner, repo string, prNumber int64, dep *DeploymentStatus) error {
	body := FormatDeploymentComment(dep)
	return m.client.CreatePRReview(ctx, owner, repo, prNumber, CreatePRReviewRequest{
		Body:  body,
		Event: "COMMENT",
	})
}

// ============================================================================
// Helpers
// ============================================================================

// deploymentPhaseIcon returns the emoji for a deployment phase.
func deploymentPhaseIcon(phase string) string {
	switch strings.ToLower(phase) {
	case "shadow":
		return "🌑"
	case "canary":
		return "🐤"
	case "promoted":
		return "✅"
	case "rolled_back":
		return "⏪"
	default:
		return "ℹ️"
	}
}

// renderProgressBar creates a simple text progress bar.
func renderProgressBar(progress float64) string {
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	filled := int(progress * 10)
	if filled > 10 {
		filled = 10
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
}

// escapePath URL-escapes a path segment for use in API paths.
func escapePath(s string) string {
	// Simple implementation: replace / and spaces.
	s = strings.ReplaceAll(s, "/", "%2F")
	s = strings.ReplaceAll(s, " ", "%20")
	return s
}
