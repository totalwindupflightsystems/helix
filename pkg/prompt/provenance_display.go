package prompt

import (
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Provenance Display — spec §11.2 (Chain Verification display format)
// ---------------------------------------------------------------------------

// FormatProvenanceChain renders the ProvenanceChain as the structured display
// format described in spec §11.2. The output shows each link in the chain with
// its status (✅/❌), making it suitable for CLI output.
//
// Example output:
//
//	COMMIT: abc123def
//	  PROMPT: sha256:a1b2c3d4 (cost-estimator v1.0.0)
//	    STATUS: active ✅
//	    HASH MATCH: verified ✅
//	    SPEC: specs/cost-estimator.md §7 ✅
//	    WORK ITEM: WI-helix-002 (complete) ✅
//	    INTENT: "Build pre-flight cost estimator" ✅
//
//	PROVENANCE CHAIN: COMPLETE ✅
func FormatProvenanceChain(chain *ProvenanceChain) string {
	if chain == nil {
		return "PROVENANCE CHAIN: (nil)\n"
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("COMMIT: %s\n", shortSHA(chain.CommitSHA)))

	for _, link := range chain.Links {
		writeChainLink(&sb, link)
	}

	if chain.Complete {
		sb.WriteString("\nPROVENANCE CHAIN: COMPLETE ✅\n")
	} else {
		sb.WriteString("\nPROVENANCE CHAIN: INCOMPLETE ❌\n")
	}

	return sb.String()
}

// writeChainLink writes a single link in the chain display.
func writeChainLink(sb *strings.Builder, link ChainLink) {
	indent := "  "
	subIndent := "    "

	emoji := "❌"
	if link.OK {
		emoji = "✅"
	}

	stageLabel := stageDisplayLabel(link.Stage)

	sb.WriteString(fmt.Sprintf("%s%s: ", indent, stageLabel))

	switch link.Stage {
	case "commit":
		sb.WriteString(fmt.Sprintf("%s\n", shortSHA(link.Name)))
	case "prompt":
		sb.WriteString(fmt.Sprintf("%s\n", link.Name))
		sb.WriteString(fmt.Sprintf("%s%s: %s %s\n", subIndent, "STATUS", link.Status, emoji))
	case "spec":
		if link.Name != "" {
			sb.WriteString(fmt.Sprintf("%s %s\n", link.Name, emoji))
		} else {
			sb.WriteString(fmt.Sprintf("%s %s\n", link.Detail, emoji))
		}
	case "work_item":
		if link.Name != "" {
			sb.WriteString(fmt.Sprintf("%s (%s) %s\n", link.Name, link.Status, emoji))
		} else {
			sb.WriteString(fmt.Sprintf("%s %s\n", link.Detail, emoji))
		}
	case "intent":
		if link.Detail != "" {
			sb.WriteString(fmt.Sprintf("%q %s\n", link.Detail, emoji))
		} else {
			sb.WriteString(fmt.Sprintf("(empty) %s\n", emoji))
		}
	default:
		sb.WriteString(fmt.Sprintf("%s: %s %s\n", link.Stage, link.Status, emoji))
	}
}

// stageDisplayLabel maps internal stage names to the display labels from
// spec §11.2.
func stageDisplayLabel(stage string) string {
	switch stage {
	case "commit":
		return "COMMIT"
	case "prompt":
		return "PROMPT"
	case "spec":
		return "SPEC"
	case "work_item":
		return "WORK ITEM"
	case "intent":
		return "INTENT"
	default:
		return strings.ToUpper(stage)
	}
}

// FormatTamperReport renders a tamper detection report when a prompt file's
// content hash does not match the attested hash (spec §11.3).
func FormatTamperReport(commitSHA, promptRef, storedHash, computedHash string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("COMMIT: %s\n", shortSHA(commitSHA)))
	sb.WriteString(fmt.Sprintf("  PROMPT: %s\n", promptRef))
	sb.WriteString("    HASH MISMATCH ❌\n")
	sb.WriteString(fmt.Sprintf("    Stored hash:   %s\n", storedHash))
	sb.WriteString(fmt.Sprintf("    Computed hash: %s\n", computedHash))
	sb.WriteString("    File modified after attestation!\n")
	sb.WriteString("\n")
	sb.WriteString("VERDICT: TAMPER DETECTED. Prompt content does not match attested hash.\n")

	return sb.String()
}

// shortSHA returns a 10-character SHA prefix for display.
func shortSHA(sha string) string {
	if len(sha) <= 10 {
		return sha
	}
	return sha[:10]
}

// ProvenanceSummary is a compact summary for audit logs.
type ProvenanceSummary struct {
	CommitSHA string    `json:"commit_sha"`
	Complete  bool      `json:"complete"`
	Stages    []string  `json:"stages"`
	Failures  []string  `json:"failures,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// SummarizeProvenance returns a compact machine-readable summary.
func SummarizeProvenance(chain *ProvenanceChain) ProvenanceSummary {
	summary := ProvenanceSummary{
		CommitSHA: chain.CommitSHA,
		Complete:  chain.Complete,
		CheckedAt: time.Now().UTC(),
	}

	for _, link := range chain.Links {
		summary.Stages = append(summary.Stages, link.Stage)
		if !link.OK {
			summary.Failures = append(summary.Failures,
				fmt.Sprintf("%s: %s", link.Stage, link.Detail))
		}
	}

	return summary
}
