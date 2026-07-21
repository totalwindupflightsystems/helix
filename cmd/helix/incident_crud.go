// Command helix — incident_crud.go
//
// CRUD subcommands for `helix incident`: declare, list, show, update.
// Mechanical extraction from the original incident.go; no behavior change.

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/security"
)

// =============================================================================
// declare
// =============================================================================

func runIncidentDeclare(common incidentCommonFlags, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("helix-incident-declare", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var severity, title, description, agentID, id string
	fs.StringVar(&severity, "severity", "", "Severity: SEV-0|SEV-1|SEV-2|SEV-3 (required)")
	fs.StringVar(&title, "title", "", "Short incident title (required)")
	fs.StringVar(&description, "description", "", "Detailed description")
	fs.StringVar(&agentID, "agent", "", "Agent ID involved (if applicable)")
	fs.StringVar(&id, "id", "", "Explicit incident ID (default: auto-generated)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printIncidentUsage(stdout)
			return 0
		}
		return 2
	}
	if severity == "" || title == "" {
		fmt.Fprintln(stderr, "helix incident declare: --severity and --title are required")
		return 2
	}

	sev := security.Severity(strings.ToUpper(severity))
	switch sev {
	case security.SeveritySEV0, security.SeveritySEV1,
		security.SeveritySEV2, security.SeveritySEV3:
		// ok
	default:
		fmt.Fprintf(stderr, "helix incident declare: invalid severity %q (want SEV-0|SEV-1|SEV-2|SEV-3)\n", severity)
		return 2
	}

	store, err := security.NewIncidentFileStore(common.resolvedStorePath())
	if err != nil {
		fmt.Fprintln(stderr, "helix incident declare: open store:", err)
		return 1
	}
	defer store.Close()

	if id == "" {
		id = "inc-" + randomHex(6)
	}

	rec := security.IncidentRecord{
		ID:          id,
		Severity:    sev,
		Title:       title,
		Description: description,
		Status:      security.IncidentOpen,
		CreatedAt:   time.Now().UTC(),
		AgentID:     agentID,
	}
	if err := store.Record(rec); err != nil {
		fmt.Fprintln(stderr, "helix incident declare: record:", err)
		return 1
	}

	if common.asJSON {
		out := struct {
			ID        string `json:"id"`
			Severity  string `json:"severity"`
			Status    string `json:"status"`
			Stored    string `json:"stored_at"`
			StorePath string `json:"store_path"`
		}{id, string(sev), string(security.IncidentOpen), security.FormatTimestamp(rec.CreatedAt), store.Path()}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(stderr, "helix incident declare: encode:", err)
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "Declared %s incident %q (ID=%s) at %s\n",
			sev, title, id, security.FormatTimestamp(rec.CreatedAt))
		fmt.Fprintf(stdout, "Store: %s\n", store.Path())
	}

	if common.verbose {
		fmt.Fprintf(stderr, "[incident declare] severity=%s id=%s stored=%s\n",
			sev, id, store.Path())
	}
	return 0
}

// =============================================================================
// list
// =============================================================================

func runIncidentList(common incidentCommonFlags, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("helix-incident-list", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var severity, statusFilter string
	var all bool
	fs.StringVar(&severity, "severity", "", "Filter by severity (SEV-0..SEV-3)")
	fs.StringVar(&statusFilter, "status", "", "Filter by status (open|in_progress|escalated|resolved)")
	fs.BoolVar(&all, "all", false, "Include resolved incidents (default: active only)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printIncidentUsage(stdout)
			return 0
		}
		return 2
	}

	store, err := security.NewIncidentFileStore(common.resolvedStorePath())
	if err != nil {
		fmt.Fprintln(stderr, "helix incident list: open store:", err)
		return 1
	}
	defer store.Close()

	recs, err := store.LoadAll()
	if err != nil {
		fmt.Fprintln(stderr, "helix incident list: load:", err)
		return 1
	}

	filtered := filterIncidents(recs, severity, statusFilter, all)
	sort.SliceStable(filtered, func(i, j int) bool {
		si := security.SeverityOrder(filtered[i].Severity)
		sj := security.SeverityOrder(filtered[j].Severity)
		if si != sj {
			return si < sj
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	if common.asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(filtered); err != nil {
			fmt.Fprintln(stderr, "helix incident list: encode:", err)
			return 1
		}
		return 0
	}

	if len(filtered) == 0 {
		fmt.Fprintln(stdout, "(no incidents)")
		return 0
	}
	printIncidentTable(stdout, filtered)
	return 0
}

func filterIncidents(recs []*security.IncidentRecord, severity, statusFilter string, includeAll bool) []*security.IncidentRecord {
	out := make([]*security.IncidentRecord, 0, len(recs))
	for _, r := range recs {
		if !includeAll && r.Status == security.IncidentResolved {
			continue
		}
		if severity != "" && string(r.Severity) != strings.ToUpper(severity) {
			continue
		}
		if statusFilter != "" && string(r.Status) != statusFilter {
			continue
		}
		out = append(out, r)
	}
	return out
}

func printIncidentTable(w io.Writer, recs []*security.IncidentRecord) {
	fmt.Fprintf(w, "%-14s %-7s %-12s %-20s %s\n", "ID", "SEV", "STATUS", "CREATED", "TITLE")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	for _, r := range recs {
		title := r.Title
		if len(title) > 30 {
			title = title[:27] + "..."
		}
		fmt.Fprintf(w, "%-14s %-7s %-12s %-20s %s\n",
			r.ID, r.Severity, r.Status,
			security.FormatTimestamp(r.CreatedAt), title)
	}
}

// =============================================================================
// show
// =============================================================================

func runIncidentShow(common incidentCommonFlags, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "helix incident show: requires exactly one incident ID")
		return 2
	}
	id := args[0]

	store, err := security.NewIncidentFileStore(common.resolvedStorePath())
	if err != nil {
		fmt.Fprintln(stderr, "helix incident show: open store:", err)
		return 1
	}
	defer store.Close()

	recs, err := store.LoadAll()
	if err != nil {
		fmt.Fprintln(stderr, "helix incident show: load:", err)
		return 1
	}

	var match *security.IncidentRecord
	for _, r := range recs {
		if r.ID == id {
			match = r
			break
		}
	}
	if match == nil {
		fmt.Fprintf(stderr, "helix incident show: no incident with ID %q\n", id)
		return 1
	}

	if common.asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(match); err != nil {
			fmt.Fprintln(stderr, "helix incident show: encode:", err)
			return 1
		}
		return 0
	}

	fmt.Fprintf(stdout, "Incident %s\n", match.ID)
	fmt.Fprintf(stdout, "  Severity:    %s\n", match.Severity)
	fmt.Fprintf(stdout, "  Status:      %s\n", match.Status)
	fmt.Fprintf(stdout, "  Created:     %s\n", security.FormatTimestamp(match.CreatedAt))
	if !match.ResolvedAt.IsZero() {
		fmt.Fprintf(stdout, "  Resolved:    %s\n", security.FormatTimestamp(match.ResolvedAt))
	}
	if match.AgentID != "" {
		fmt.Fprintf(stdout, "  Agent:       %s\n", match.AgentID)
	}
	fmt.Fprintf(stdout, "  Title:       %s\n", match.Title)
	if match.Description != "" {
		fmt.Fprintf(stdout, "  Description: %s\n", match.Description)
	}

	engine := security.NewIncidentResponseEngine()
	engine.RegisterIncident(*match)
	if proc, ok := engine.GetProcedure(match.Severity); ok {
		fmt.Fprintf(stdout, "\nResponse procedure (%s):\n", match.Severity)
		fmt.Fprint(stdout, security.FormatProcedure(proc))
	}
	return 0
}

// =============================================================================
// update
// =============================================================================

func runIncidentUpdate(common incidentCommonFlags, args []string, stdout, stderr io.Writer) int {
	// The Go flag package stops parsing at the first positional. So if
	// the user invokes `helix incident update inc-001 --status resolved`,
	// `inc-001` becomes positional and `--status resolved` is also
	// positional (not parsed as a flag). We work around this by
	// extracting the LEADING positional ID (only when it's the first
	// token before any flag) and feeding the rest to flag.FS.
	var id string
	var rest []string
	for i, a := range args {
		if i == 0 && !strings.HasPrefix(a, "-") {
			id = a
			continue
		}
		rest = append(rest, a)
	}

	fs := flag.NewFlagSet("helix-incident-update", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var status string
	fs.StringVar(&status, "status", "", "New status: open|in_progress|escalated|resolved (required)")
	// Also accept --id in case the user prefers the flag form. The
	// default carries forward the positional id we extracted above.
	fs.StringVar(&id, "id", id, "Incident ID (also accepted as first positional)")

	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printIncidentUsage(stdout)
			return 0
		}
		return 2
	}
	// Validate status value first so typos surface even if the user
	// forgot the ID. The status is the operator's intent — if it's
	// wrong we want to know before we complain about missing args.
	if status != "" {
		switch security.IncidentStatus(status) {
		case security.IncidentOpen, security.IncidentInProgress,
			security.IncidentEscalated, security.IncidentResolved:
			// ok
		default:
			fmt.Fprintf(stderr, "helix incident update: invalid status %q\n", status)
			return 2
		}
	}
	if id == "" {
		fmt.Fprintln(stderr, "helix incident update: incident ID is required (first positional or --id)")
		return 2
	}
	if status == "" {
		fmt.Fprintln(stderr, "helix incident update: --status is required")
		return 2
	}

	store, err := security.NewIncidentFileStore(common.resolvedStorePath())
	if err != nil {
		fmt.Fprintln(stderr, "helix incident update: open store:", err)
		return 1
	}
	defer store.Close()

	recs, err := store.LoadAll()
	if err != nil {
		fmt.Fprintln(stderr, "helix incident update: load:", err)
		return 1
	}

	var match *security.IncidentRecord
	for _, r := range recs {
		if r.ID == id {
			match = r
			break
		}
	}
	if match == nil {
		fmt.Fprintf(stderr, "helix incident update: no incident with ID %q\n", id)
		return 1
	}

	newStatus := security.IncidentStatus(status)
	updated := *match
	updated.Status = newStatus
	if newStatus == security.IncidentResolved && updated.ResolvedAt.IsZero() {
		updated.ResolvedAt = time.Now().UTC()
	}
	if err := store.Record(updated); err != nil {
		fmt.Fprintln(stderr, "helix incident update: record:", err)
		return 1
	}

	if common.asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(updated)
	} else {
		fmt.Fprintf(stdout, "Updated incident %s to status %s\n", updated.ID, updated.Status)
	}
	return 0
}
