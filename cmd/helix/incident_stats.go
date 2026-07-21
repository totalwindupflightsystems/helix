// Command helix — incident_stats.go
//
// Stats + attribute subcommands for `helix incident`. Mechanical extraction
// from the original incident.go; no behavior change.

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/incident"
	"github.com/totalwindupflightsystems/helix/pkg/security"
)

// =============================================================================
// stats
// =============================================================================

func runIncidentStats(common incidentCommonFlags, args []string, stdout, stderr io.Writer) int {
	// stats has no subcommand-specific flags but accept --help gracefully.
	fs := flag.NewFlagSet("helix-incident-stats", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printIncidentUsage(stdout)
			return 0
		}
		return 2
	}

	store, err := security.NewIncidentFileStore(common.resolvedStorePath())
	if err != nil {
		fmt.Fprintln(stderr, "helix incident stats: open store:", err)
		return 1
	}
	defer store.Close()

	recs, err := store.LoadAll()
	if err != nil {
		fmt.Fprintln(stderr, "helix incident stats: load:", err)
		return 1
	}

	engine := security.NewIncidentResponseEngine()
	for _, r := range recs {
		engine.RegisterIncident(*r)
	}
	stats := engine.ComputeStats()

	if common.asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(stats)
	} else {
		fmt.Fprint(stdout, security.FormatStats(stats))
	}
	return 0
}

// =============================================================================
// attribute
// =============================================================================

func runIncidentAttribute(common incidentCommonFlags, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("helix-incident-attribute", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var since string
	var limit int
	fs.StringVar(&since, "since", "24h", "Time window for change discovery (e.g., 24h, 7d, 30m)")
	fs.IntVar(&limit, "limit", 100, "Max commits to examine")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printIncidentUsage(stdout)
			return 0
		}
		return 2
	}

	// Discover change paths from git history within the time window.
	paths, err := discoverChangePaths(since, limit)
	if err != nil {
		fmt.Fprintf(stderr, "helix incident attribute: %v\n", err)
		return 1
	}

	if len(paths) == 0 {
		if common.asJSON {
			fmt.Fprintln(stdout, `{"attributions":[],"message":"no changes found in window"}`)
		} else {
			fmt.Fprintf(stdout, "No changes found in the last %s.\n", since)
		}
		return 0
	}

	store := NewIncidentAttrStore()
	engine := NewIncidentAttrEngine(store)

	result, err := engine.Attribute("attr-"+randomHex(4), paths, nil)
	if err != nil {
		fmt.Fprintf(stderr, "helix incident attribute: %v\n", err)
		return 1
	}

	if common.asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		type attrEntry struct {
			AgentID        string  `json:"agent_id"`
			Responsibility float64 `json:"responsibility"`
			TrustPenalty   float64 `json:"trust_penalty"`
		}
		summaries := result.Summarize(SeverityMediumStr)
		var entries []attrEntry
		for _, s := range summaries {
			entries = append(entries, attrEntry{
				AgentID:        s.AgentID,
				Responsibility: s.Responsibility,
				TrustPenalty:   s.TrustPenalty,
			})
		}
		out := struct {
			Attributions  []attrEntry `json:"attributions"`
			ChangePaths   int         `json:"change_paths"`
			Since         string      `json:"since"`
			Normalization string      `json:"normalization"`
		}{
			Attributions:  entries,
			ChangePaths:   len(paths),
			Since:         since,
			Normalization: "author 70%%, reviewer(s) 20%, approver 10%",
		}
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(stderr, "helix incident attribute: encode: %v\n", err)
			return 1
		}
	} else {
		printAttributionTable(stdout, result, paths, since)
	}

	if common.verbose {
		fmt.Fprintf(stderr, "[incident attribute] since=%s paths=%d agents=%d\n",
			since, len(paths), len(result.Responsibility))
	}
	return 0
}

func printAttributionTable(w io.Writer, result *IncidentAttrResult, paths []IncidentAttrChangePath, since string) {
	fmt.Fprintf(w, "Incident attribution — %d change paths in last %s\n", len(paths), since)
	fmt.Fprintf(w, "Attribution model: author 70%%, reviewer(s) 20%% (shared), approver 10%%\n\n")
	fmt.Fprintf(w, "%-20s %-16s %-14s\n", "AGENT", "RESPONSIBILITY", "TRUST PENALTY")
	fmt.Fprintln(w, strings.Repeat("-", 56))
	summaries := result.Summarize(SeverityMediumStr)
	for _, s := range summaries {
		fmt.Fprintf(w, "%-20s %15.1f%% %14.1f%%\n",
			s.AgentID, s.Responsibility*100, s.TrustPenalty*100)
	}
	fmt.Fprintln(w, "\nChange paths:")
	for _, p := range paths {
		fmt.Fprintf(w, "  %s (author: %s)\n", p.FilePath, p.AuthorID)
	}
}

// IncidentAttrStore is a thin alias so the CLI doesn't import pkg/incident directly.
type IncidentAttrStore = incident.Store

// IncidentAttrEngine is a thin alias.
type IncidentAttrEngine = incident.AttributionEngine

// IncidentAttrResult is a thin alias.
type IncidentAttrResult = incident.AttributionResult

// IncidentAttrChangePath is a thin alias.
type IncidentAttrChangePath = incident.ChangePath

// NewIncidentAttrStore creates the store via the pkg/incident package.
var NewIncidentAttrStore = incident.NewStore

// NewIncidentAttrEngine creates the engine via the pkg/incident package.
var NewIncidentAttrEngine = incident.NewAttributionEngine
