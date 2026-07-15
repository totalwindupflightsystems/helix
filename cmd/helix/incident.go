// Command helix — incident.go
//
// `helix incident` exposes the pkg/security.IncidentResponseEngine
// to operators (spec §6.7 + §10.5). Subcommands:
//
//	helix incident declare --severity SEV-1 --title "..." [--description ...] [--agent ...]
//	    Creates a new IncidentRecord, appends it to the JSONL log, prints the ID.
//
//	helix incident list [--severity SEV-1] [--status open|in_progress|escalated|resolved] [--all]
//	    Lists incidents sorted by severity desc, time desc. Default: active only.
//
//	helix incident show <id>
//	    Prints the full record + the spec response procedure.
//
//	helix incident update <id> --status <open|in_progress|escalated|resolved>
//	    Transitions status; appends a new record (JSONL is append-only).
//
//	helix incident stats
//	    Prints IncidentStats: totals, by severity, by status, mean resolve time.
//
// All subcommands accept --store PATH (defaults to ~/.helix/incidents.jsonl)
// and --json for machine-readable output.

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/incident"
	"github.com/totalwindupflightsystems/helix/pkg/security"
)

// envIncidentStoreFile is the env var the operator can use to avoid
// hardcoding the --store path in CI / crontabs.
const envIncidentStoreFile = "HELIX_INCIDENT_STORE"

// =============================================================================
// Common-flag parsing (single source of truth)
// =============================================================================

// incidentCommonFlags carries flags every subcommand accepts.
// Populated by runIncident from leading common-flag args, then passed
// down to each subcommand handler.
type incidentCommonFlags struct {
	storePath string
	asJSON    bool
	verbose   bool
}

// resolvedStorePath returns the store path the caller should use,
// applying the env → default fallback when --store was not specified.
func (c incidentCommonFlags) resolvedStorePath() string {
	if c.storePath != "" {
		return c.storePath
	}
	return defaultIncidentStorePath()
}

// defaultIncidentStorePath returns the default store path when --store
// was not specified: env var (HELIX_INCIDENT_STORE) takes precedence
// over the package default (security.DefaultIncidentPath).
func defaultIncidentStorePath() string {
	if v := envOr(envIncidentStoreFile, ""); v != "" {
		return v
	}
	return security.DefaultIncidentPath
}

// =============================================================================
// Dispatch
// =============================================================================

// runIncident is the cmd/helix incident entry point. It parses common
// flags anywhere in args (both before and after the subcommand keyword),
// dispatches to the subcommand, and returns the exit code.
func runIncident(args []string, stdout, stderr io.Writer) int {
	common, rest := extractIncidentCommonFlags(args)
	if len(rest) == 0 {
		printIncidentUsage(stdout)
		return 0
	}
	subCmd := rest[0]
	subArgs := rest[1:]
	switch subCmd {
	case "declare":
		return runIncidentDeclare(common, subArgs, stdout, stderr)
	case "list":
		return runIncidentList(common, subArgs, stdout, stderr)
	case "show":
		return runIncidentShow(common, subArgs, stdout, stderr)
	case "update":
		return runIncidentUpdate(common, subArgs, stdout, stderr)
	case "stats":
		return runIncidentStats(common, subArgs, stdout, stderr)
	case "attribute":
		return runIncidentAttribute(common, subArgs, stdout, stderr)
	case "--help", "-h", "help":
		printIncidentUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "helix incident: unknown subcommand %q\n\n", subCmd)
		printIncidentUsage(stderr)
		return 2
	}
}

// extractIncidentCommonFlags walks args and pulls out --store PATH,
// --json, --verbose from ANYWHERE in the arg list. This lets users
// pass them before or after the subcommand keyword uniformly.
func extractIncidentCommonFlags(args []string) (incidentCommonFlags, []string) {
	var c incidentCommonFlags
	rest := make([]string, 0, len(args))
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--store" && i+1 < len(args):
			c.storePath = args[i+1]
			i += 2
		case strings.HasPrefix(a, "--store="):
			c.storePath = strings.TrimPrefix(a, "--store=")
			i++
		case a == "--json":
			c.asJSON = true
			i++
		case a == "--verbose":
			c.verbose = true
			i++
		default:
			rest = append(rest, a)
			i++
		}
	}
	return c, rest
}

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

// SeverityMediumStr is the medium severity string for trust penalty calculation.
const SeverityMediumStr = "medium"

// discoverChangePaths queries git log for changed files within the given
// time window, then runs git blame on each to identify author/reviewer/approver.
func discoverChangePaths(since string, limit int) ([]incident.ChangePath, error) {
	// Convert shorthand formats to git-compatible relative dates.
	gitSince := sinceToGitSince(since)

	// Get commits in the time window.
	output, err := gitLog(gitSince, limit)
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	if output == "" {
		return nil, nil
	}

	// Collect unique files from commit output.
	files := uniqueFilesFromGitLog(output)

	// For each file, run git blame to find the merge commit + author.
	var paths []incident.ChangePath
	for _, f := range files {
		blameOut, err := gitBlame(f)
		if err != nil {
			// Skip files that can't be blamed (deleted, binary, etc.)
			continue
		}
		path := parseBlameOutput(blameOut, f)
		if path.AuthorID != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// sinceToGitSince converts shorthand duration formats (24h, 7d, 30m, 2w)
// to git-compatible relative date strings. Unknown formats pass through.
func sinceToGitSince(since string) string {
	// If it already looks git-compatible, pass through.
	if strings.Contains(since, ".") || strings.Contains(since, " ") || strings.Contains(since, "-") {
		return since
	}
	// Parse shorthand: <number><unit>
	for i, c := range since {
		if c >= '0' && c <= '9' {
			continue
		}
		num := since[:i]
		unit := since[i:]
		switch unit {
		case "h", "hr", "hrs":
			return num + ".hours"
		case "d":
			return num + ".days"
		case "w", "wk", "wks":
			return num + ".weeks"
		case "m":
			return num + ".minutes"
		case "s":
			return num + ".seconds"
		}
		break
	}
	return since
}

// gitLog runs `git log` to get changed files since the given duration.
func gitLog(since string, limit int) (string, error) {
	cmd := exec.Command("git", "log", "--since="+since,
		"--name-only", "--pretty=format:%H %an %s",
		"-n", fmt.Sprintf("%d", limit))
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// uniqueFilesFromGitLog parses git log output and returns unique file paths.
func uniqueFilesFromGitLog(output string) []string {
	seen := make(map[string]bool)
	var files []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "commit ") {
			continue
		}
		// Skip lines that look like commit hashes + metadata (don't contain "/.")
		if !strings.Contains(line, "/") && !strings.HasSuffix(line, ".go") &&
			!strings.HasSuffix(line, ".py") && !strings.HasSuffix(line, ".ts") &&
			!strings.HasSuffix(line, ".rs") && !strings.HasSuffix(line, ".js") {
			continue
		}
		if !seen[line] {
			seen[line] = true
			files = append(files, line)
		}
	}
	return files
}

// gitBlame runs `git blame` on a file to find the commit hash and author.
func gitBlame(file string) (string, error) {
	cmd := exec.Command("git", "blame", "--porcelain", "-w", file)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// parseBlameOutput extracts ChangePath from git blame porcelain output.
func parseBlameOutput(blameOutput, filePath string) incident.ChangePath {
	lines := strings.Split(blameOutput, "\n")
	if len(lines) < 2 {
		return incident.ChangePath{FilePath: filePath}
	}

	// First line: <commit-hash> <orig-line> <final-line> <line-count>
	firstFields := strings.Fields(lines[0])
	if len(firstFields) < 1 || len(firstFields[0]) < 7 {
		return incident.ChangePath{FilePath: filePath}
	}
	commitHash := firstFields[0]

	// Parse porcelain fields: author <name>, committer <name>, summary <text>
	var author string
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "author ") {
			author = strings.TrimPrefix(line, "author ")
			break
		}
		if strings.HasPrefix(line, "author-mail ") {
			// Extract identity from email: agent-X@... → agent-X
			// But prefer the author name field.
		}
	}

	return incident.ChangePath{
		FilePath: filePath,
		MergeSHA: commitHash,
		AuthorID: author,
	}
}

// =============================================================================
// Usage
// =============================================================================

func printIncidentUsage(w io.Writer) {
	fmt.Fprintf(w, `helix incident — declare, list, and resolve incidents (spec §6.7)

Usage:
  helix incident [--store PATH] [--json] [--verbose] <subcommand> [flags]

Subcommands:
  declare    Create a new incident and append to the JSONL log.
  list       List incidents (default: active only).
  show       Show details for one incident ID.
  update     Update an incident's status.
  stats      Print aggregate statistics.
  attribute  Trace causal chain from recent deploys to responsible agents.

Common flags (before subcommand):
  --store PATH    JSONL store path (default %s, env %s)
  --json          Emit machine-readable JSON
  --verbose       Verbose stderr logging

Declare flags:
  --severity SEV     SEV-0|SEV-1|SEV-2|SEV-3 (required)
  --title TITLE      Short incident title (required)
  --description D    Detailed description
  --agent ID         Agent ID involved
  --id ID            Explicit incident ID (default: auto-generated)

List flags:
  --severity SEV     Filter by severity
  --status STATUS    Filter by status (open|in_progress|escalated|resolved)
  --all              Include resolved incidents

Update flags:
  --status STATUS    New status (required)
  <id>               Incident ID (first positional or --id)

Attribute flags:
  --since DURATION   Time window for change discovery (default: 24h)
  --limit N          Max commits to examine (default: 100)

Examples:
  helix incident declare --severity SEV-1 --title "Chimera 503 spike"
  helix incident list
  helix incident show inc-a1b2c3
  helix incident update inc-a1b2c3 --status resolved
  helix incident stats --json
  helix incident attribute --since 24h
  helix incident attribute --since 7d --limit 50 --json
`, security.DefaultIncidentPath, envIncidentStoreFile)
}

// =============================================================================
// Helpers
// =============================================================================

// randomHex returns n random bytes as a hex string. Used to mint
// short, human-friendly incident IDs like "inc-a1b2c3".
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp suffix — never collide on a single host.
		return fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
