// Command helix — design.go
//
// `helix design` exposes pkg/design: automated design review via adversarial
// agents (Phase 2 §2.3). Primary subcommand: review.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/adr"
	"github.com/totalwindupflightsystems/helix/pkg/design"
	"github.com/totalwindupflightsystems/helix/pkg/spec"
)

const (
	designExitOK    = 0
	designExitError = 2

	envDesignSpecStore = "HELIX_SPEC_STORE"
	envDesignADRStore  = "HELIX_ADR_STORE"
)

// designFlags holds parsed flags for helix design.
type designFlags struct {
	subcommand string
	id         string // spec-id
	fixID      string
	storePath  string
	adrStore   string
	summary    bool
	jsonOut    bool
	dryRun     bool
	budget     float64
	capacity   float64
	timeline   int
}

func parseDesignFlags(args []string) (designFlags, bool, int) {
	var f designFlags
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--dry-run":
			f.dryRun = true
		case arg == "--summary":
			f.summary = true
		case arg == "--fix":
			if i+1 >= len(args) {
				return f, false, designExitError
			}
			i++
			f.fixID = args[i]
		case arg == "--store":
			if i+1 >= len(args) {
				return f, false, designExitError
			}
			i++
			f.storePath = args[i]
		case arg == "--adr-store":
			if i+1 >= len(args) {
				return f, false, designExitError
			}
			i++
			f.adrStore = args[i]
		case arg == "--budget":
			if i+1 >= len(args) {
				return f, false, designExitError
			}
			i++
			var v float64
			if _, err := fmt.Sscanf(args[i], "%f", &v); err != nil {
				return f, false, designExitError
			}
			f.budget = v
		case arg == "--capacity":
			if i+1 >= len(args) {
				return f, false, designExitError
			}
			i++
			var v float64
			if _, err := fmt.Sscanf(args[i], "%f", &v); err != nil {
				return f, false, designExitError
			}
			f.capacity = v
		case arg == "--timeline":
			if i+1 >= len(args) {
				return f, false, designExitError
			}
			i++
			var v int
			if _, err := fmt.Sscanf(args[i], "%d", &v); err != nil {
				return f, false, designExitError
			}
			f.timeline = v
		case strings.HasPrefix(arg, "--"):
			return f, false, designExitError
		default:
			if f.subcommand == "" {
				f.subcommand = arg
			} else if f.id == "" {
				f.id = arg
			} else {
				return f, false, designExitError
			}
		}
		i++
	}

	if f.subcommand == "" {
		f.subcommand = "help"
	}
	return f, helpWanted, designExitOK
}

func printDesignHelp(w io.Writer) {
	fmt.Fprintln(w, `helix design — design review with adversarial agents

Usage:
  helix design review <spec-id> [--summary] [--fix <finding-id>] [--json]
  helix design help

Flags:
  --summary         Exec-level summary: risk, cost, key blockers
  --fix ID          Focus a single finding (re-print + remediation hint)
  --store PATH      Spec store directory (or $HELIX_SPEC_STORE)
  --adr-store PATH  ADR store directory (or $HELIX_ADR_STORE)
  --budget USD      Remaining budget injected into DesignContext
  --capacity N      Team capacity (person-weeks) for DesignContext
  --timeline DAYS   Timeline days for DesignContext
  --json            Machine-readable DesignReviewReport
  --dry-run         Preview without side effects (review is read-only)

Change Management View sections:
  (a) Assumption Risk     — @assumption-buster, ranked by risk
  (b) Threat Surface      — @redteam attack vectors + services
  (c) Cost Budget         — @cost-auditor projection
  (d) Completeness Gaps   — missing design elements
  (e) Consensus Verdict   — PASS / WARN / FAIL

Exit codes:
  0  Success
  2  Error`)
}

func resolveDesignSpecStore(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv(envDesignSpecStore); v != "" {
		return v
	}
	return ""
}

func resolveDesignADRStore(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv(envDesignADRStore); v != "" {
		return v
	}
	return ""
}

// runDesign is the entry point for `helix design`.
func runDesign(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseDesignFlags(args)
	if rc != designExitOK {
		fmt.Fprintln(stderr, "error: invalid arguments")
		printDesignHelp(stderr)
		return designExitError
	}
	if helpWanted || flags.subcommand == "help" {
		printDesignHelp(stdout)
		return designExitOK
	}

	switch flags.subcommand {
	case "review":
		return runDesignReview(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		printDesignHelp(stderr)
		return designExitError
	}
}

// runDesignWithDryRun threads the global --dry-run flag.
func runDesignWithDryRun(args []string, stdout, stderr io.Writer, dryRun bool) error {
	if dryRun {
		args = append(append([]string{}, args...), "--dry-run")
	}
	rc := runDesign(args, stdout, stderr)
	if rc != designExitOK {
		return errExit{code: rc}
	}
	return nil
}

func runDesignReview(f designFlags, stdout, stderr io.Writer) int {
	specID := strings.TrimSpace(f.id)
	if specID == "" {
		fmt.Fprintln(stderr, "error: review requires <spec-id>")
		return designExitError
	}

	specStore, err := spec.NewSpecStore(resolveDesignSpecStore(f.storePath))
	if err != nil {
		fmt.Fprintf(stderr, "error: open spec store: %v\n", err)
		return designExitError
	}
	s, err := specStore.Load(specID)
	if err != nil {
		fmt.Fprintf(stderr, "error: load spec %s: %v\n", specID, err)
		return designExitError
	}

	adrRefs := append([]string(nil), s.ADRRefs...)
	adrTexts := make([]string, 0, len(adrRefs))
	if len(adrRefs) > 0 {
		adrStore, err := adr.NewADRStore(resolveDesignADRStore(f.adrStore))
		if err != nil {
			fmt.Fprintf(stderr, "error: open adr store: %v\n", err)
			return designExitError
		}
		for _, ref := range adrRefs {
			a, err := adrStore.Load(ref)
			if err != nil {
				// Non-fatal: include a stub so consistency-checker can flag it.
				adrTexts = append(adrTexts, fmt.Sprintf("(failed to load ADR %s: %v)", ref, err))
				continue
			}
			adrTexts = append(adrTexts, renderADRForDesign(a))
		}
	}

	req := &design.DesignReviewRequest{
		SpecRef:   s.ID,
		ADRRefs:   adrRefs,
		SpecTitle: s.Title,
		SpecText:  renderSpecForDesign(s),
		ADRTexts:  adrTexts,
		Context: design.DesignContext{
			TeamCapacity:    f.capacity,
			BudgetRemaining: f.budget,
			TimelineDays:    f.timeline,
		},
	}

	if f.dryRun {
		fmt.Fprintf(stdout, "dry-run: would review design for spec %s (%d ADRs)\n", s.ID, len(adrRefs))
		if f.jsonOut {
			_ = json.NewEncoder(stdout).Encode(map[string]any{
				"dry_run":  true,
				"spec_ref": s.ID,
				"adr_refs": adrRefs,
			})
		}
		return designExitOK
	}

	dispatcher := design.NewDesignReviewDispatcher()
	report, err := dispatcher.Review(context.Background(), req)
	if err != nil {
		fmt.Fprintf(stderr, "error: design review: %v\n", err)
		return designExitError
	}

	if f.fixID != "" {
		matched := design.FilterFindingsByID(report.Findings, f.fixID)
		if len(matched) == 0 {
			fmt.Fprintf(stderr, "error: finding %q not found\n", f.fixID)
			return designExitError
		}
		report.Findings = matched
	}

	if f.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(stderr, "error: encode json: %v\n", err)
			return designExitError
		}
		return designExitOK
	}

	if f.summary {
		printDesignSummary(stdout, report)
		return designExitOK
	}

	printChangeManagementView(stdout, report)
	return designExitOK
}

func renderSpecForDesign(s *spec.Spec) string {
	var b strings.Builder
	b.WriteString("Title: ")
	b.WriteString(s.Title)
	b.WriteString("\n")
	b.WriteString("Status: ")
	b.WriteString(s.Status)
	b.WriteString("\n")
	for _, sec := range s.Sections {
		b.WriteString("\n## ")
		b.WriteString(sec.Title)
		b.WriteString("\n")
		b.WriteString(sec.Content)
		b.WriteString("\n")
	}
	if len(s.Annotations) > 0 {
		b.WriteString("\n## Annotations\n")
		for _, a := range s.Annotations {
			b.WriteString(fmt.Sprintf("- [%s/%s] %s\n", a.AgentType, a.AnnotationType, a.Content))
		}
	}
	return b.String()
}

func renderADRForDesign(a *adr.ADR) string {
	var b strings.Builder
	b.WriteString("Title: ")
	b.WriteString(a.Title)
	b.WriteString("\n")
	b.WriteString("Status: ")
	b.WriteString(a.Status)
	b.WriteString("\n")
	b.WriteString("Context: ")
	b.WriteString(a.Context)
	b.WriteString("\n")
	b.WriteString("Decision: ")
	b.WriteString(a.Decision)
	b.WriteString("\n")
	b.WriteString("Consequences: ")
	b.WriteString(a.Consequences)
	b.WriteString("\n")
	for _, alt := range a.Alternatives {
		b.WriteString(fmt.Sprintf("Alternative: %s | tradeoffs: %s", alt.Description, alt.Tradeoffs))
		if alt.RejectedBecause != "" {
			b.WriteString(" | rejected because: ")
			b.WriteString(alt.RejectedBecause)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func printDesignSummary(w io.Writer, report *design.DesignReviewReport) {
	fmt.Fprintln(w, "Design Review Summary")
	fmt.Fprintln(w, "─────────────────────")
	fmt.Fprintf(w, "Spec:      %s\n", report.SpecRef)
	fmt.Fprintf(w, "Risk:      %.0f/100\n", report.RiskScore)
	fmt.Fprintf(w, "Cost:      $%.2f (%s)\n", report.CostProjection.CostTotal, report.CostProjection.Model)
	fmt.Fprintf(w, "Verdict:   %s\n", report.Consensus.Verdict)
	fmt.Fprintf(w, "Agents:    %s\n", strings.Join(report.AgentsRun, ", "))
	blockers := 0
	for _, f := range report.Findings {
		if design.FindRiskLevel(f.Severity) == "high" {
			blockers++
		}
	}
	fmt.Fprintf(w, "Blockers:  %d high-risk findings\n", blockers)
	if len(report.ThreatSurface.AttackVectors) > 0 {
		fmt.Fprintf(w, "Threats:   %d attack vectors\n", len(report.ThreatSurface.AttackVectors))
	}
}

func printChangeManagementView(w io.Writer, report *design.DesignReviewReport) {
	fmt.Fprintln(w, "Change Management View — Design Review")
	fmt.Fprintln(w, "══════════════════════════════════════")
	fmt.Fprintf(w, "Spec: %s\n", report.SpecRef)
	if len(report.ADRRefs) > 0 {
		fmt.Fprintf(w, "ADRs: %s\n", strings.Join(report.ADRRefs, ", "))
	}
	fmt.Fprintf(w, "Reviewed: %s\n", report.ReviewedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "Agents: %s\n", strings.Join(report.AgentsRun, ", "))
	fmt.Fprintln(w)

	// (a) Assumption Risk
	fmt.Fprintln(w, "(a) Assumption Risk")
	fmt.Fprintln(w, "───────────────────")
	assumptions := design.AssumptionsByRisk(report.Findings)
	if len(assumptions) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		for _, f := range assumptions {
			fmt.Fprintf(w, "  [%s] %s  risk=%s\n", f.ID, f.Description, f.RiskLevel)
			if f.Evidence != "" {
				fmt.Fprintf(w, "         challenge: %s\n", f.Evidence)
			}
		}
	}
	fmt.Fprintln(w)

	// (b) Threat Surface
	fmt.Fprintln(w, "(b) Threat Surface")
	fmt.Fprintln(w, "──────────────────")
	tm := report.ThreatSurface
	if len(tm.Services) > 0 {
		fmt.Fprintln(w, "  Services:")
		for _, s := range tm.Services {
			fmt.Fprintf(w, "    - %s (risk=%.0f)\n", s.Name, s.RiskScore)
		}
	}
	if len(tm.AttackVectors) == 0 {
		fmt.Fprintln(w, "  Attack vectors: (none)")
	} else {
		fmt.Fprintln(w, "  Attack vectors:")
		for _, av := range tm.AttackVectors {
			fmt.Fprintf(w, "    - [%s] %s  service=%s\n", strings.ToUpper(av.Severity), av.Description, av.AffectedService)
			if av.Mitigation != "" {
				fmt.Fprintf(w, "      mitigation: %s\n", av.Mitigation)
			}
		}
	}
	if len(tm.TrustBoundaries) > 0 {
		fmt.Fprintln(w, "  Trust boundaries:")
		for _, tb := range tm.TrustBoundaries {
			fmt.Fprintf(w, "    - %s: %s\n", tb.Name, tb.Description)
		}
	}
	fmt.Fprintln(w)

	// (c) Cost Budget
	fmt.Fprintln(w, "(c) Cost Budget")
	fmt.Fprintln(w, "───────────────")
	fmt.Fprintf(w, "  Projected: $%.2f  (model=%s provider=%s)\n",
		report.CostProjection.CostTotal, report.CostProjection.Model, report.CostProjection.Provider)
	if report.CostProjection.Tokens.TotalInput > 0 || report.CostProjection.Tokens.Output > 0 {
		fmt.Fprintf(w, "  Tokens:    input≈%d output≈%d\n",
			report.CostProjection.Tokens.TotalInput, report.CostProjection.Tokens.Output)
	}
	costFindings := design.FindingsByAspect(report.Findings, design.AspectCost)
	for _, f := range costFindings {
		if strings.EqualFold(f.Severity, "info") {
			continue
		}
		fmt.Fprintf(w, "  [%s] %s\n", f.Severity, f.Description)
	}
	fmt.Fprintln(w)

	// (d) Completeness Gaps
	fmt.Fprintln(w, "(d) Completeness Gaps")
	fmt.Fprintln(w, "─────────────────────")
	gaps := design.FindingsByAspect(report.Findings, design.AspectCompleteness)
	// Also include consistency issues that are completeness-adjacent.
	consistency := design.FindingsByAspect(report.Findings, design.AspectConsistency)
	if len(gaps) == 0 && len(consistency) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, f := range gaps {
		fmt.Fprintf(w, "  [%s] %s  %s\n", f.ID, f.Severity, f.Description)
	}
	for _, f := range consistency {
		if strings.EqualFold(f.Severity, "info") {
			continue
		}
		fmt.Fprintf(w, "  [%s] %s  %s\n", f.ID, f.Severity, f.Description)
	}
	fmt.Fprintln(w)

	// (e) Consensus Verdict
	fmt.Fprintln(w, "(e) Consensus Verdict")
	fmt.Fprintln(w, "────────────────────")
	fmt.Fprintf(w, "  Verdict:    %s\n", report.Consensus.Verdict)
	fmt.Fprintf(w, "  Score:      %.2f\n", report.Consensus.Score)
	fmt.Fprintf(w, "  RiskScore:  %.0f/100\n", report.RiskScore)
	fmt.Fprintf(w, "  CleanBill:  %v\n", report.Consensus.CleanBill)
	fmt.Fprintf(w, "  Summary:    %s\n", report.Consensus.Summary)
	if report.DispatchSummary != "" {
		fmt.Fprintf(w, "  Dispatch:   %s\n", report.DispatchSummary)
	}
}
