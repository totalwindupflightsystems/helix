// Command helix — incident_patterns.go
//
// `helix incident patterns` subcommands: list / show / discover, plus the
// pattern miner construction and the display helpers they share.
// Mechanical extraction from the original incident.go; no behavior change.

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/incident"
	"github.com/totalwindupflightsystems/helix/pkg/learning"
	"github.com/totalwindupflightsystems/helix/pkg/security"
)

// =============================================================================
// patterns
// =============================================================================

// runIncidentPatterns dispatches to the `helix incident patterns` subcommands.
//
// Subcommands:
//
//	helix incident patterns list    List discovered patterns, sorted by confidence.
//	helix incident patterns show ID Show details for a specific pattern.
//	helix incident patterns discover Trigger a manual discovery run.
func runIncidentPatterns(common incidentCommonFlags, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "helix incident patterns: expected subcommand: list | show | discover")
		return 2
	}
	subCmd := args[0]
	subArgs := args[1:]

	switch subCmd {
	case "list":
		return runIncidentPatternsList(common, subArgs, stdout, stderr)
	case "show":
		return runIncidentPatternsShow(common, subArgs, stdout, stderr)
	case "discover":
		return runIncidentPatternsDiscover(common, subArgs, stdout, stderr)
	case "--help", "-h", "help":
		printIncidentPatternsUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "helix incident patterns: unknown subcommand %q\n\n", subCmd)
		printIncidentPatternsUsage(stderr)
		return 2
	}
}

func runIncidentPatternsList(common incidentCommonFlags, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("helix-incident-patterns-list", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var category string
	var minConfidence float64
	fs.StringVar(&category, "category", "", "Filter by file category (auth, database, api, etc.)")
	fs.Float64Var(&minConfidence, "min-confidence", 0.4, "Minimum confidence score (0.0–1.0)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printIncidentPatternsUsage(stdout)
			return 0
		}
		return 2
	}

	// Build miner from existing incident data.
	miner := buildPatternMiner()
	results := miner.Discover()

	// Filter.
	filtered := filterPatterns(results, category, minConfidence)

	if common.asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(filtered); err != nil {
			fmt.Fprintf(stderr, "helix incident patterns list: encode: %v\n", err)
			return 1
		}
		return 0
	}

	if len(filtered) == 0 {
		fmt.Fprintln(stdout, "No patterns discovered matching criteria.")
		return 0
	}

	printPatternsTable(stdout, filtered)
	return 0
}

func runIncidentPatternsShow(common incidentCommonFlags, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "helix incident patterns show: requires a pattern ID")
		return 2
	}
	patternID := args[0]

	miner := buildPatternMiner()
	miner.Discover() // ensure patterns are populated

	p := miner.Get(patternID)
	if p == nil {
		fmt.Fprintf(stderr, "helix incident patterns show: pattern %q not found\n", patternID)
		return 1
	}

	if common.asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(p); err != nil {
			fmt.Fprintf(stderr, "helix incident patterns show: encode: %v\n", err)
			return 1
		}
		return 0
	}

	printPatternDetail(stdout, p)
	return 0
}

func runIncidentPatternsDiscover(common incidentCommonFlags, args []string, stdout, stderr io.Writer) int {
	miner := buildPatternMiner()
	results := miner.Discover()

	if common.asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			fmt.Fprintf(stderr, "helix incident patterns discover: encode: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintf(stdout, "Pattern discovery complete — %d patterns found.\n\n", len(results))
	if len(results) > 0 {
		printPatternsTable(stdout, results)
	}

	if common.verbose {
		fmt.Fprintf(stderr, "[incident patterns] discovered %d patterns, last run: %s\n",
			len(results), miner.LastDiscovery().Format(time.RFC3339))
	}
	return 0
}

// buildPatternMiner constructs a PatternMiner from the incident store on disk.
func buildPatternMiner() *learning.PatternMiner {
	// Build an in-memory incident data source from the incident file store.
	store, err := security.NewIncidentFileStore(defaultIncidentStorePath())
	if err != nil {
		// Fallback: return miner with empty data sources.
		return learning.NewPatternMiner(
			learning.NewIncidentSliceSource(nil),
			learning.NewPatternSliceSource(nil),
		)
	}
	defer store.Close()

	records, err := store.LoadAll()
	if err != nil {
		return learning.NewPatternMiner(
			learning.NewIncidentSliceSource(nil),
			learning.NewPatternSliceSource(nil),
		)
	}

	// Convert security.IncidentRecord → incident.Incident
	incidents := make([]*incident.Incident, 0, len(records))
	for _, rec := range records {
		incidents = append(incidents, &incident.Incident{
			ID:          rec.ID,
			AgentID:     rec.AgentID,
			Severity:    string(rec.Severity),
			Timestamp:   rec.CreatedAt,
			Description: rec.Description,
		})
	}
	incSource := learning.NewIncidentSliceSource(incidents)

	// Build pattern data source from the incident learning DB.
	ldb := incident.NewLearningDatabase()
	// Populate patterns from incidents if available.
	for _, inc := range incidents {
		categories := incident.CategorizeFiles(inc.CausalChain)
		var changeType incident.ChangeType
		if len(inc.CausalChain) > 0 {
			changeType = incident.ChangeModify
		}
		ldb.StoreFromIncident(inc, categories, changeType, nil, "", nil)
	}
	patSource := learning.NewPatternSliceSource(ldb.All())

	return learning.NewPatternMiner(incSource, patSource)
}

// filterPatterns filters discovered patterns by category and minimum confidence.
func filterPatterns(patterns []learning.DiscoveredPattern, category string, minConfidence float64) []learning.DiscoveredPattern {
	var filtered []learning.DiscoveredPattern
	for _, p := range patterns {
		if p.Confidence < minConfidence {
			continue
		}
		if category != "" {
			catMatch := false
			for _, c := range p.Categories {
				if strings.EqualFold(string(c), category) {
					catMatch = true
					break
				}
			}
			if !catMatch {
				continue
			}
		}
		filtered = append(filtered, p)
	}
	return filtered
}

func printPatternsTable(w io.Writer, patterns []learning.DiscoveredPattern) {
	fmt.Fprintf(w, "%-14s %-6s %-40s %s\n", "PATTERN ID", "CONF", "TITLE", "TYPE")
	fmt.Fprintln(w, strings.Repeat("-", 90))
	for _, p := range patterns {
		status := ""
		if p.IsEstablished() {
			status = " ★"
		} else if p.IsHypothesis() {
			status = " ?"
		}
		fmt.Fprintf(w, "%-14s %5.0f%%  %-40s %s%s\n",
			p.ID, p.Confidence*100, patternTruncate(p.Title, 40), p.PatternType, status)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "★ = established (≥80% confidence)   ? = hypothesis (<80%)")
}

func printPatternDetail(w io.Writer, p *learning.DiscoveredPattern) {
	fmt.Fprintf(w, "PATTERN: %s\n", p.ID)
	fmt.Fprintf(w, "  Type:        %s\n", p.PatternType)
	fmt.Fprintf(w, "  Title:       %s\n", p.Title)
	fmt.Fprintf(w, "  Description: %s\n", p.Description)
	fmt.Fprintf(w, "  Confidence:  %.0f%%", p.Confidence*100)
	if p.IsEstablished() {
		fmt.Fprintf(w, " (established)\n")
	} else if p.IsHypothesis() {
		fmt.Fprintf(w, " (hypothesis)\n")
	} else {
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "  Sample size: %d\n", p.SampleSize)
	fmt.Fprintf(w, "  Statistical basis: %s\n", p.StatisticalBasis)
	fmt.Fprintf(w, "  Discovered:  %s\n", p.DiscoveredAt.Format(time.RFC3339))
	if len(p.Categories) > 0 {
		catStrs := make([]string, len(p.Categories))
		for i, c := range p.Categories {
			catStrs[i] = string(c)
		}
		fmt.Fprintf(w, "  Categories:  %s\n", strings.Join(catStrs, ", "))
	}
	if len(p.Keywords) > 0 {
		fmt.Fprintf(w, "  Keywords:    %s\n", strings.Join(p.Keywords, ", "))
	}
	if len(p.ReviewChecklist) > 0 {
		fmt.Fprintf(w, "\nReview Checklist:\n")
		for _, item := range p.ReviewChecklist {
			fmt.Fprintf(w, "  • %s\n", item)
		}
	}
}

func patternTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
