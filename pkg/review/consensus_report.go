package review

import (
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Consensus report formatter (spec §Evidence Bundles — consensus display)
// ---------------------------------------------------------------------------

// FormatConsensusReport renders an EvidenceBundle as a structured markdown
// report suitable for posting as a Forgejo PR comment. The report contains:
//   - Header: PR URL, review ID, timestamp
//   - Formation summary: models + providers used, diversity score
//   - Findings table: per-finding model, severity, type, file:line, description, evidence
//   - Consensus block: per-model verdicts + resolution
//   - Bias-stripped commit hash + original commit hash
//   - Evidence bundle link
func FormatConsensusReport(bundle EvidenceBundle) string {
	var b strings.Builder

	// Header
	b.WriteString("## 🔍 Helix Adversarial Review Report\n\n")
	b.WriteString("| Field | Value |\n|-------|-------|\n")
	b.WriteString(fmt.Sprintf("| **PR** | %s |\n", bundle.PRURL))
	b.WriteString(fmt.Sprintf("| **Review ID** | %s |\n", bundle.ReviewID))
	b.WriteString(fmt.Sprintf("| **Timestamp** | %s |\n\n", bundle.Timestamp.UTC().Format(time.RFC3339)))

	// Formation summary
	b.WriteString("### Formation\n\n")
	b.WriteString("| Role | Model | Provider |\n|------|-------|----------|\n")
	b.WriteString(fmt.Sprintf("| Primary | %s | %s |\n", bundle.Formation.Primary.Model, bundle.Formation.Primary.Provider))
	if bundle.Formation.Adversarial.Model != "" {
		b.WriteString(fmt.Sprintf("| Adversarial | %s | %s |\n", bundle.Formation.Adversarial.Model, bundle.Formation.Adversarial.Provider))
	}
	if bundle.Formation.Audit.Model != "" {
		b.WriteString(fmt.Sprintf("| Audit | %s | %s |\n", bundle.Formation.Audit.Model, bundle.Formation.Audit.Provider))
	}
	diversity := DiversityScore(bundle.Formation)
	b.WriteString(fmt.Sprintf("\n**Provider diversity:** %d distinct providers\n\n", diversity))

	// Findings table
	b.WriteString(RenderFindingsTable(bundle.Findings))

	// Consensus block
	b.WriteString(RenderConsensusBlock(bundle.Consensus))

	// Commit hashes
	b.WriteString("### Commit Verification\n\n")
	b.WriteString("| Type | SHA |\n|------|-----|\n")
	b.WriteString(fmt.Sprintf("| Bias-stripped | `%s` |\n", shortSHA(bundle.BiasStrippedSHA)))
	b.WriteString(fmt.Sprintf("| Original | `%s` |\n\n", shortSHA(bundle.OriginalCommit)))

	// Evidence bundle link
	if bundle.ReviewID != "" {
		b.WriteString(fmt.Sprintf("📎 **Evidence bundle:** `%s.json`\n", bundle.ReviewID))
	}

	return b.String()
}

// RenderFindingsTable renders findings as a markdown table. Each row has:
// model, severity, type, file:line, description, and evidence.
func RenderFindingsTable(findings []Finding) string {
	var b strings.Builder

	if len(findings) == 0 {
		b.WriteString("### Findings\n\n✅ No findings reported — all models approved.\n\n")
		return b.String()
	}

	b.WriteString("### Findings\n\n")
	b.WriteString("| Model | Severity | Type | Location | Description |\n")
	b.WriteString("|-------|----------|------|----------|-------------|\n")

	for _, f := range findings {
		location := f.File
		if f.Line > 0 {
			location = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
			f.Model, f.Severity, f.Type, location, f.Description))
	}
	b.WriteString("\n")

	return b.String()
}

// RenderConsensusBlock renders the consensus verdicts as a structured block.
func RenderConsensusBlock(consensus Consensus) string {
	var b strings.Builder

	b.WriteString("### Consensus\n\n")
	b.WriteString("| Model Role | Verdict |\n|------------|---------|\n")
	b.WriteString(fmt.Sprintf("| Primary | %s |\n", formatVerdict(consensus.PrimaryVerdict)))
	if consensus.AdversarialVerdict != "" {
		b.WriteString(fmt.Sprintf("| Adversarial | %s |\n", formatVerdict(consensus.AdversarialVerdict)))
	}
	if consensus.AuditVerdict != "" {
		b.WriteString(fmt.Sprintf("| Audit | %s |\n", formatVerdict(consensus.AuditVerdict)))
	}

	b.WriteString(fmt.Sprintf("\n**Resolution:** %s\n", formatResolution(consensus.Resolution)))
	if consensus.TieBreaker != "" {
		b.WriteString(fmt.Sprintf("**Tie-breaker:** %s\n", consensus.TieBreaker))
	}
	b.WriteString("\n")

	return b.String()
}

// formatVerdict converts an internal verdict string to a human-readable
// label with emoji.
func formatVerdict(verdict string) string {
	switch verdict {
	case VerdictApproved:
		return "✅ Approved"
	case VerdictPassWithNotes:
		return "⚠️ Pass with notes"
	case VerdictBlock:
		return "❌ Block"
	case VerdictConfirmAdversarial:
		return "🔴 Confirmed adversarial"
	case VerdictOverrule:
		return "↩️ Overruled"
	default:
		if verdict == "" {
			return "—"
		}
		return verdict
	}
}

// formatResolution converts a resolution string to a human-readable label.
func formatResolution(resolution string) string {
	switch resolution {
	case ResolutionApproved:
		return "✅ Approved — merge allowed"
	case ResolutionBlocked:
		return "❌ Blocked — merge denied"
	case ResolutionTieBreak:
		return "⚖️ Tie-breaker required — escalated to Chimera arbiter"
	default:
		if resolution == "" {
			return "—"
		}
		return resolution
	}
}

// shortSHA returns the first 12 characters of a hash/SHA string for display.
func shortSHA(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}
