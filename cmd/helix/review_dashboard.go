package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/review"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// -----------------------------------------------------------------------------
// review dashboard — human change management view
// -----------------------------------------------------------------------------

// runReviewDashboard builds the change management dashboard (blast radius,
// risk score, ADR fit, trust context) for human reviewers. Spec §6.1.
func runReviewDashboard(flags revFlags, stdout, stderr io.Writer) int {
	if flags.prURL == "" {
		fmt.Fprintln(stderr, "error: --pr is required for review dashboard")
		return revExitError
	}

	files, err := collectDashboardFiles(flags)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return revExitError
	}
	if len(files) == 0 {
		fmt.Fprintln(stderr, "error: provide --files or --files-from with at least one changed path")
		return revExitError
	}

	in := review.DashboardInput{
		PR:           flags.prURL,
		AgentID:      flags.agentID,
		ChangedFiles: files,
		RepoRoot:     flags.repoRoot,
		LedgerPath:   flags.ledgerPath,
		ADRDir:       flags.adrDir,
	}
	if flags.category != "" {
		cat, ok := parseReviewCategory(flags.category)
		if !ok {
			fmt.Fprintf(stderr, "error: invalid --category %q (contract|behavioral|resilience|cosmetic)\n", flags.category)
			return revExitError
		}
		in.Category = cat
	}
	if flags.tier != "" {
		tier, ok := parseReviewTrustTier(flags.tier)
		if !ok {
			fmt.Fprintf(stderr, "error: invalid --tier %q (provisional|observed|trusted|veteran)\n", flags.tier)
			return revExitError
		}
		in.TrustTier = tier
	}
	if flags.incidents != "" {
		incs, err := loadRelatedIncidents(flags.incidents)
		if err != nil {
			fmt.Fprintf(stderr, "error: --incidents: %v\n", err)
			return revExitError
		}
		in.RelatedIncidents = incs
	}

	dash, err := review.BuildDashboard(in)
	if err != nil {
		fmt.Fprintf(stderr, "error: build dashboard: %v\n", err)
		return revExitError
	}

	if flags.jsonOut {
		raw, err := review.DashboardJSON(dash)
		if err != nil {
			fmt.Fprintf(stderr, "error: marshal dashboard: %v\n", err)
			return revExitError
		}
		fmt.Fprintln(stdout, string(raw))
		return revExitOK
	}

	fmt.Fprint(stdout, review.FormatDashboard(dash))
	return revExitOK
}

func collectDashboardFiles(flags revFlags) ([]string, error) {
	var files []string
	if flags.filesCSV != "" {
		for _, p := range strings.Split(flags.filesCSV, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				files = append(files, p)
			}
		}
	}
	if flags.filesFrom != "" {
		body, err := os.ReadFile(flags.filesFrom)
		if err != nil {
			return nil, fmt.Errorf("read --files-from: %w", err)
		}
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			files = append(files, line)
		}
	}
	return files, nil
}

func parseReviewCategory(s string) (review.ChangeCategory, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "contract":
		return review.CategoryContract, true
	case "behavioral":
		return review.CategoryBehavioral, true
	case "resilience":
		return review.CategoryResilience, true
	case "cosmetic":
		return review.CategoryCosmetic, true
	default:
		return "", false
	}
}

func parseReviewTrustTier(s string) (trust.TrustTier, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "provisional":
		return trust.TierProvisional, true
	case "observed":
		return trust.TierObserved, true
	case "trusted":
		return trust.TierTrusted, true
	case "veteran":
		return trust.TierVeteran, true
	default:
		return "", false
	}
}

func loadRelatedIncidents(path string) ([]review.RelatedIncident, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var incs []review.RelatedIncident
	if err := json.Unmarshal(body, &incs); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return incs, nil
}
