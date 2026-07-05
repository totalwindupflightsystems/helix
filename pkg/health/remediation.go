// Package health — remediation.go
//
// Operator-facing remediation hints for Helix doctor failures. When
// `helix doctor` flags a failing service without telling the operator
// how to fix it, triage time balloons. RemediationRegistry maps each
// known failure type to a ranked set of next-action steps plus a doc
// reference and severity.
//
// Per docs/specs/SPECIFICATION.md §10.5 (Doctor) and the May/June 2026
// field-feedback sessions (~30% of triage time lost to "no suggestion").
//
// Design rules:
//
//  1. Steps are DESCRIPTIVE — operators run them. helix doctor NEVER
//     auto-fixes. The wrapper is purely advisory.
//  2. Severity is derived from the check (not the operator). Critical
//     → platform down, high → required service down, medium → degraded,
//     low → informational.
//  3. DocURLs reference local specs/ paths so ops work offline.
//  4. The registry is process-global (defaultDrRegistry) but tests can
//     construct isolated instances via NewRemediationRegistry.
//
// Coupling: pkg/health/remediation.go has zero dependencies outside the
// standard library and pkg/log. No HTTP, no db, no agent code. Pure
// data + formatting.
package health

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Severity is the operator-facing severity of a failed check.
type Severity string

const (
	SeverityLow      Severity = "low"      // informational; not blocking
	SeverityMedium   Severity = "medium"   // degraded performance
	SeverityHigh     Severity = "high"     // required service down
	SeverityCritical Severity = "critical" // platform cannot serve traffic
)

// Remediation is the actionable output for a single failed check.
//
// Steps are sorted best-first (Steady-State Operator's Guide pattern —
// the most-likely fix comes first; the operator can stop as soon as
// one works).
type Remediation struct {
	// Check is the doctor check name (e.g., "Forgejo reachable").
	Check string
	// Reason is a one-sentence diagnosis ("Forgejo /api/v1/version returned 503").
	Reason string
	// Severity is the operator-facing severity level.
	Severity Severity
	// Steps is the ordered list of recovery commands. Best-first.
	Steps []Step
	// DocURL points at the local specs/ page that explains the failure.
	DocURL string
	// AutoApplicable indicates whether a programmatic (non-shell) helper
	// could run this step automatically. Always false in this release —
	// safety-first. Surfaced for transparency.
	AutoApplicable bool
}

// Step is a single recovery action.
//
// Cmd is a shell command the operator can paste. Doc is a one-line
// explanation. Optional: programmatic steps (Future) carry a Go function
// pointer; today all steps are shell-only.
type Step struct {
	Cmd string // shell command
	Doc string // one-line explanation
}

// RemediationRegistry is a keyed lookup from "check name" → Remediation.
//
// Use the package-level Default() for production. Construct your own
// for tests with NewRemediationRegistry.
type RemediationRegistry struct {
	entries map[string]Remediation
}

// NewRemediationRegistry returns an empty registry.
func NewRemediationRegistry() *RemediationRegistry {
	return &RemediationRegistry{
		entries: make(map[string]Remediation),
	}
}

// Register adds (or replaces) a Remediation keyed by check name.
//
// Use Add() for fluent chaining on a fresh registry; use Register on
// a registry that already has entries you want to keep.
func (r *RemediationRegistry) Register(check string, rem Remediation) *RemediationRegistry {
	if r.entries == nil {
		r.entries = make(map[string]Remediation)
	}
	r.entries[check] = rem
	return r
}

// Add is a fluent convenience over Register — chains like
//
//	r := NewRemediationRegistry().
//	    Add("forgejo_reachable", Remediation{...}).
//	    Add("chimera_healthy", Remediation{...})
func (r *RemediationRegistry) Add(check string, rem Remediation) *RemediationRegistry {
	return r.Register(check, rem)
}

// Lookup returns the Remediation for a check and whether it exists.
//
// Two return values (vs a single (Remediation, bool)) so callers can do
//
//	rem, ok := reg.Lookup(name)
//	if !ok { /* no suggestion — operator on their own */ }
func (r *RemediationRegistry) Lookup(check string) (Remediation, bool) {
	rem, ok := r.entries[check]
	return rem, ok
}

// LookupOrDefault returns the Remediation for a check or a generic
// fallback if the check is unknown.
func (r *RemediationRegistry) LookupOrDefault(check, fallbackReason string) Remediation {
	rem, ok := r.entries[check]
	if ok {
		return rem
	}
	return Remediation{
		Check:    check,
		Reason:   fallbackReason,
		Severity: SeverityMedium,
		Steps: []Step{{
			Cmd: "helix doctor --suggest --json",
			Doc: "Re-run with --json for machine-readable output",
		}},
		DocURL:         "specs/SPECIFICATION.md §10.5",
		AutoApplicable: false,
	}
}

// Len returns the number of registered remediations.
func (r *RemediationRegistry) Len() int {
	return len(r.entries)
}

// Keys returns the registered check names in sorted order.
//
// Returned slice is a defensive copy — callers may sort/mutate freely.
func (r *RemediationRegistry) Keys() []string {
	keys := make([]string, 0, len(r.entries))
	for k := range r.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ============================================================================
// Default registry — covers every doctor check known to cmd/helix/doctor.go
// ============================================================================

// defaultRegistry is the process-global registry. Constructed lazily.
var (
	defaultRegistry     *RemediationRegistry
	defaultRegistryOnce bool
)

// Default returns the process-global registry.
//
// The registry is constructed on first call and then cached. It is
// shared across all callers — DO NOT mutate it from tests; use
// NewRemediationRegistry() to construct your own.
func Default() *RemediationRegistry {
	if !defaultRegistryOnce {
		defaultRegistry = NewRemediationRegistry()
		defaultRegistry.populate()
		defaultRegistryOnce = true
	}
	return defaultRegistry
}

// ResetDefaultRegistry clears the cached global registry.
//
// Test-only. Allows tests that mutate Default() to start from scratch.
// Not safe to call concurrently with Default() in production code.
func ResetDefaultRegistry() {
	defaultRegistry = nil
	defaultRegistryOnce = false
}

// populate fills the registry with defaults matching cmd/helix/doctor.go.
//
// Check names match the literal strings used by runAllChecks(). Keep
// in sync when doctor.go adds a check.
func (r *RemediationRegistry) populate() *RemediationRegistry {
	return r.
		// Forgejo — required for identity, PRs, OAuth.
		Add("Forgejo reachable", Remediation{
			Reason:   "Forgejo HTTP endpoint did not respond or returned an error",
			Severity: SeverityHigh,
			Steps: []Step{
				{Cmd: "docker compose ps forgejo", Doc: "Check if Forgejo container is running"},
				{Cmd: "docker compose up -d forgejo", Doc: "Start Forgejo container"},
				{Cmd: "docker logs forgejo --tail 50", Doc: "Inspect recent logs"},
				{Cmd: "curl -v http://localhost:3030/api/v1/version", Doc: "Verify endpoint reachable"},
			},
			DocURL:         "specs/deployment.md §2",
			AutoApplicable: false,
		}).

		// Chimera — required for multi-model review.
		Add("Chimera healthy", Remediation{
			Reason:   "Chimera multi-model review API did not respond",
			Severity: SeverityHigh,
			Steps: []Step{
				{Cmd: "systemctl status chimera", Doc: "Check Chimera daemon status"},
				{Cmd: "systemctl restart chimera", Doc: "Restart Chimera"},
				{Cmd: "journalctl -u chimera -n 50", Doc: "Inspect recent Chimera logs"},
				{Cmd: "curl -v http://localhost:8765/health", Doc: "Verify health endpoint"},
			},
			DocURL:         "specs/integrations.md §Chimera",
			AutoApplicable: false,
		}).

		// Conscientiousness — agent sandbox policy daemon.
		Add("Conscientiousness healthy", Remediation{
			Reason:   "Conscientiousness policy daemon did not respond",
			Severity: SeverityMedium,
			Steps: []Step{
				{Cmd: "systemctl status conscientiousness", Doc: "Check daemon status"},
				{Cmd: "systemctl restart conscientiousness", Doc: "Restart daemon"},
				{Cmd: "journalctl -u conscientiousness -n 50", Doc: "Inspect logs"},
			},
			DocURL:         "specs/integrations.md §Conscientiousness",
			AutoApplicable: false,
		}).

		// Hivemind — agent memory bank.
		Add("Hivemind healthy", Remediation{
			Reason:   "Hivemind memory bank did not respond",
			Severity: SeverityMedium,
			Steps: []Step{
				{Cmd: "systemctl status hivemind", Doc: "Check memory bank daemon"},
				{Cmd: "systemctl restart hivemind", Doc: "Restart memory bank"},
				{Cmd: "hivemind status", Doc: "Show memory bank contents"},
				{Cmd: "journalctl -u hivemind -n 50", Doc: "Inspect logs"},
			},
			DocURL:         "specs/integrations.md §Hivemind",
			AutoApplicable: false,
		}).

		// LangFuse — prompt telemetry.
		Add("LangFuse reachable", Remediation{
			Reason:   "LangFuse telemetry endpoint did not respond",
			Severity: SeverityMedium,
			Steps: []Step{
				{Cmd: "docker compose ps langfuse", Doc: "Check LangFuse container"},
				{Cmd: "docker compose up -d langfuse", Doc: "Start LangFuse"},
				{Cmd: "docker logs langfuse --tail 50", Doc: "Inspect logs"},
			},
			DocURL:         "specs/integrations.md §LangFuse",
			AutoApplicable: false,
		}).

		// Prometheus — required for metrics + alerts.
		Add("Prometheus scraping", Remediation{
			Reason:   "Prometheus scrape endpoint did not respond healthy",
			Severity: SeverityHigh,
			Steps: []Step{
				{Cmd: "systemctl status prometheus", Doc: "Check Prometheus status"},
				{Cmd: "systemctl restart prometheus", Doc: "Restart Prometheus"},
				{Cmd: "curl -v http://localhost:9090/-/healthy", Doc: "Verify healthy endpoint"},
				{Cmd: "journalctl -u prometheus -n 30", Doc: "Inspect logs"},
			},
			DocURL:         "specs/deployment.md §3",
			AutoApplicable: false,
		}).

		// Disk usage — high disk can break Forgejo/CI/etc.
		Add("Disk usage", Remediation{
			Reason:   "Disk usage exceeded threshold (default 90%)",
			Severity: SeverityHigh,
			Steps: []Step{
				{Cmd: "df -h", Doc: "Show disk usage by mount"},
				{Cmd: "du -sh /var/log/* | sort -h | tail -20", Doc: "Largest log directories"},
				{Cmd: "docker system prune -af", Doc: "Remove unused Docker artifacts"},
				{Cmd: "journalctl --vacuum-size=200M", Doc: "Trim systemd journal"},
			},
			DocURL:         "specs/SPECIFICATION.md §10.3",
			AutoApplicable: false,
		}).

		// Memory.
		Add("Memory", Remediation{
			Reason:   "System memory usage exceeded threshold (default 90%)",
			Severity: SeverityHigh,
			Steps: []Step{
				{Cmd: "free -h", Doc: "Show memory usage"},
				{Cmd: "ps aux --sort=-%mem | head -20", Doc: "Top memory consumers"},
				{Cmd: "docker stats --no-stream", Doc: "Container memory usage"},
				{Cmd: "systemctl restart helix-agent", Doc: "Restart agent (if leaking)"},
			},
			DocURL:         "specs/SPECIFICATION.md §10.4",
			AutoApplicable: false,
		}).

		// Backup freshness — required for DR.
		Add("Backup freshness", Remediation{
			Reason:   "Backups are stale or missing (default threshold: 24h)",
			Severity: SeverityCritical,
			Steps: []Step{
				{Cmd: "ls -la /mnt/backups/helix | tail -20", Doc: "List recent backups"},
				{Cmd: "helix backup run", Doc: "Trigger a manual backup"},
				{Cmd: "systemctl status helix-backup", Doc: "Check backup scheduler"},
				{Cmd: "journalctl -u helix-backup -n 30", Doc: "Inspect backup logs"},
			},
			DocURL:         "specs/SPECIFICATION.md §10.3",
			AutoApplicable: false,
		})
}

// ============================================================================
// Formatting — operator-facing output
// ============================================================================

// FormatRemediation renders one remediation as a human-readable block.
//
// Output is plain text with no ANSI — works in plain terminals, log
// aggregators, and CI captures. Tabular alignment, no colour codes.
//
// Example output:
//
//	✗ Forgejo reachable — high
//
//	Reason:  Forgejo HTTP endpoint did not respond or returned an error
//	Doc:     specs/deployment.md §2
//
//	Try:
//
//	  1. Check if Forgejo container is running
//	     $ docker compose ps forgejo
//
//	  2. Start Forgejo container
//	     $ docker compose up -d forgejo
//
//	  3. Inspect recent logs
//	     $ docker logs forgejo --tail 50
func FormatRemediation(rem Remediation, w io.Writer) {
	fmt.Fprintf(w, "  ✗ %s — %s\n", rem.Check, rem.Severity)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Reason:  %s\n", rem.Reason)
	fmt.Fprintf(w, "  Doc:     %s\n", rem.DocURL)
	fmt.Fprintln(w)
	if len(rem.Steps) == 0 {
		fmt.Fprintln(w, "  (no automatic remediation known — see Doc for guidance)")
		return
	}
	fmt.Fprintln(w, "  Try:")
	fmt.Fprintln(w)
	for i, step := range rem.Steps {
		fmt.Fprintf(w, "    %d. %s\n", i+1, step.Doc)
		fmt.Fprintf(w, "       $ %s\n", step.Cmd)
		fmt.Fprintln(w)
	}
}

// FormatRemediationJSON renders a Remediation as compact JSON.
//
// One-line JSON per remediation, NDJSON-friendly (newline-delimited).
// Suitable for piping to `jq` or shipping to a triage dashboard.
func FormatRemediationJSON(rem Remediation) (string, error) {
	// Lightweight string-build for stable JSON without pulling encoding/json.
	var b strings.Builder
	b.WriteString("{")
	b.WriteString(`"check":` + jsonString(rem.Check) + ",")
	b.WriteString(`"reason":` + jsonString(rem.Reason) + ",")
	b.WriteString(`"severity":` + jsonString(string(rem.Severity)) + ",")
	b.WriteString(`"doc_url":` + jsonString(rem.DocURL) + ",")
	b.WriteString(`"auto_applicable":` + boolString(rem.AutoApplicable) + ",")
	b.WriteString(`"steps":[`)
	for i, s := range rem.Steps {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{`)
		b.WriteString(`"cmd":` + jsonString(s.Cmd) + ",")
		b.WriteString(`"doc":` + jsonString(s.Doc))
		b.WriteString(`}`)
	}
	b.WriteString("]}")
	return b.String(), nil
}

// RemediationReport groups the remediations produced for a single
// doctor run. Used by cmd/helix to decide exit code + format.
type RemediationReport struct {
	Remediations []Remediation // one per failing check
	Unknown      []string      // failing checks with no remediation known
	HasAny       bool          // len(Remediations) > 0
}

// BuildRemediationReport walks a DoctorReport and looks up remediations
// for every failing check. Failing checks without an entry appear in
// Unknown (exit code 1 per spec).
//
// DoctorReport is defined in cmd/helix/doctor.go and uses string status
// values "PASS"/"FAIL"/"WARN". Only FAIL and WARN produce remediations;
// PASS checks are skipped (no operator action needed).
//
// The function also overrides rem.Check with the actual check name
// from the lookup key. This guards against registry entries that
// forget to set Check explicitly — the remediation block always
// identifies the failure by its doctor name, never blank.
func BuildRemediationReport(reg *RemediationRegistry, checks []CheckOutcome) RemediationReport {
	rep := RemediationReport{}
	for _, c := range checks {
		if !c.IsFailing() {
			continue
		}
		rem, ok := reg.Lookup(c.Name)
		if !ok {
			rep.Unknown = append(rep.Unknown, c.Name)
			continue
		}
		// Force rem.Check to match the lookup key. The registry
		// stores each Remediation under its check name, so the
		// canonical source of truth is c.Name — not whatever
		// rem.Check was set to when the entry was registered.
		rem.Check = c.Name
		// Override reason with the actual detail if available.
		if c.Detail != "" {
			rem.Reason = c.Detail + " — " + rem.Reason
		}
		rep.Remediations = append(rep.Remediations, rem)
	}
	rep.HasAny = len(rep.Remediations) > 0
	return rep
}

// CheckOutcome is the minimal view BuildRemediationReport needs from a
// doctor check result. Defined here (rather than importing doctor.go)
// so the remediation package has zero coupling to cmd/. cmd/helix
// constructs a []CheckOutcome from its DoctorReport.
type CheckOutcome struct {
	Name   string
	Status string // "PASS", "FAIL", "WARN"
	Detail string
}

// IsFailing returns true for FAIL or WARN. PASS is not failing.
func (c CheckOutcome) IsFailing() bool {
	return c.Status == "FAIL" || c.Status == "WARN"
}

// jsonString renders a string as a JSON string literal with escaping.
// Avoids importing encoding/json for the simple case.
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
