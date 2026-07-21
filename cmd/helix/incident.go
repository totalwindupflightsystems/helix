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
//
// Subcommand handlers live in:
//   - incident_crud.go      (declare / list / show / update)
//   - incident_stats.go     (stats / attribute)
//   - incident_attr.go      (git change-path discovery helpers)
//   - incident_patterns.go  (patterns list / show / discover)

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

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
	case "patterns":
		return runIncidentPatterns(common, subArgs, stdout, stderr)
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

func printIncidentPatternsUsage(w io.Writer) {
	fmt.Fprintf(w, `helix incident patterns — discover and inspect systemic incident patterns

Usage:
  helix incident patterns list    [--category CAT] [--min-confidence N]
  helix incident patterns show    <pattern-id>
  helix incident patterns discover

Subcommands:
  list       List discovered patterns, sorted by confidence.
  show       Show full details for a pattern, including review checklist.
  discover   Trigger a manual discovery run across the incident database.

List flags:
  --category CAT          Filter by file category (auth, database, api, etc.)
  --min-confidence N      Minimum confidence score (0.0–1.0, default: 0.4)

Show args:
  <pattern-id>            Pattern ID to display (e.g., pattern-0001)

Examples:
  helix incident patterns list
  helix incident patterns list --category auth --min-confidence 0.7
  helix incident patterns show pattern-0001
  helix incident patterns discover --json
`)
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
