// Command helix — status.go
//
// `helix status` exposes the unified platform health view backed by
// pkg/health.PlatformHealthAggregator. The aggregator collects health
// from every registered Helix subsystem (forgejo, chimera, trust,
// review, negotiate, verify, marketplace, estimate) and produces a
// DashboardReport. This CLI is a thin wrapper that:
//
//  1. Builds an aggregator with a short cache TTL (CLI is one-shot).
//  2. Registers default subsystems backed by the same HTTP probes that
//     cmd/helix/doctor.go uses today (so the migration is invisible to
//     operators — same data, structured output).
//  3. Renders the DashboardReport as either a table or JSON.
//
// Flags:
//
//	--json           Emit machine-readable JSON instead of the table
//	--no-cache       Force a fresh aggregate (bypass the 15s default cache)
//	--timeout        Per-subsystem HTTP timeout (default 5s)
//
// Exit codes:
//
//	0 — Every subsystem is healthy
//	1 — One or more subsystems are degraded (non-critical failures)
//	2 — Critical subsystem is DOWN — platform cannot serve traffic
//
// The legacy `helix doctor` command continues to work — its ad-hoc
// checks are preserved as a fallback (--legacy flag on doctor) but the
// new default `helix status` is the structured source of truth.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/health"
)

// ============================================================================
// Constants
// ============================================================================

const (
	envStatusJSON     = "HELIX_STATUS_JSON"
	envStatusNoCache  = "HELIX_STATUS_NO_CACHE"
	envStatusTimeout  = "HELIX_STATUS_TIMEOUT"
	envStatusCacheTTL = "HELIX_STATUS_CACHE_TTL"

	// defaultCacheTTL is the in-process cache TTL for the aggregator.
	// CLI is one-shot but caching helps when callers wrap the binary.
	defaultStatusCacheTTL = 15 * time.Second
	// defaultSubsystemTimeout is the per-subsystem HTTP probe timeout.
	defaultSubsystemTimeout = 5 * time.Second
)

// statusFlags captures every CLI flag in one struct.
type statusFlags struct {
	jsonOutput bool
	noCache    bool
	timeout    time.Duration
	cacheTTL   time.Duration
}

// ============================================================================
// parseStatusFlags
// ============================================================================

// parseStatusFlags builds a flag.FS, parses the args, and returns the
// populated flags plus a "showHelp" bool. `helix status` accepts no
// positional args — only flags.
func parseStatusFlags(args []string, stdout, stderr io.Writer) (statusFlags, bool, error) {
	fs := flag.NewFlagSet("helix-status", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f statusFlags
	fs.BoolVar(&f.jsonOutput, "json", false,
		"Emit JSON instead of the table (env HELIX_STATUS_JSON)")
	fs.BoolVar(&f.noCache, "no-cache", false,
		"Force a fresh aggregate, bypassing the in-process cache (env HELIX_STATUS_NO_CACHE)")
	fs.DurationVar(&f.timeout, "timeout", defaultSubsystemTimeout,
		"Per-subsystem HTTP probe timeout (env HELIX_STATUS_TIMEOUT)")
	fs.DurationVar(&f.cacheTTL, "cache-ttl", defaultStatusCacheTTL,
		"In-process aggregator cache TTL (env HELIX_STATUS_CACHE_TTL)")

	// Env-var defaults BEFORE parse.
	if os.Getenv(envStatusJSON) == "1" || os.Getenv(envStatusJSON) == "true" {
		f.jsonOutput = true
	}
	if os.Getenv(envStatusNoCache) == "1" || os.Getenv(envStatusNoCache) == "true" {
		f.noCache = true
	}
	if v := os.Getenv(envStatusTimeout); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			f.timeout = d
		}
	}
	if v := os.Getenv(envStatusCacheTTL); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			f.cacheTTL = d
		}
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return f, true, nil
		}
		return f, false, err
	}
	if rest := fs.Args(); len(rest) > 0 {
		return f, false, fmt.Errorf("unexpected positional arguments: %v", rest)
	}
	return f, false, nil
}

// ============================================================================
// Entry points — wired from main.go
// ============================================================================

// runStatusWithDryRun is the variant invoked by the unified `helix`
// CLI when the user passes the GLOBAL --dry-run flag. status has no
// network side-effects of its own (the aggregator probes HTTP), so
// --dry-run is informational only — we surface that in the output and
// skip the actual probes.
func runStatusWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	rc := runStatusNew(args, stdout, stderr, globalDryRun)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}

// runStatusNew is the replacement for the legacy `runStatus` in main.go.
// When globalDryRun is true, the aggregator is skipped and a stub
// "all healthy" report is rendered so scripts that test --dry-run see
// the same shape as a real run.
func runStatusNew(args []string, stdout, stderr io.Writer, globalDryRun bool) int {
	flags, showHelp, err := parseStatusFlags(args, stdout, stderr)
	if showHelp {
		printStatusUsage(stdout)
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "helix status: parse:", err)
		printStatusUsage(stderr)
		return 2
	}

	if globalDryRun {
		// Dry-run: render the placeholder report without probing.
		return renderDryRunStatus(flags, stdout)
	}

	// Build the aggregator with default subsystems (matching what
	// cmd/helix/doctor.go probes today).
	agg := health.NewPlatformHealthAggregator(flags.cacheTTL)
	registerDefaultSubsystems(agg, flags.timeout)

	ctx, cancel := context.WithTimeout(context.Background(), flags.timeout*2)
	defer cancel()

	report := agg.Aggregate(ctx)
	if flags.noCache {
		agg.Invalidate()
		report = agg.Aggregate(ctx)
	}

	if flags.jsonOutput {
		return renderStatusJSON(report, stdout, stderr)
	}
	return renderStatusTable(report, stdout)
}

// ============================================================================
// Default subsystems
// ============================================================================

// registerDefaultSubsystems populates the aggregator with the same
// probes cmd/helix/doctor.go runs today. Every subsystem is a
// SubsystemHealth implementation backed by a single HTTP GET.
func registerDefaultSubsystems(agg *health.PlatformHealthAggregator, timeout time.Duration) {
	defaults := []struct {
		name string
		url  string
	}{
		{"forgejo", "http://localhost:3000/api/v1/version"},
		{"chimera", "http://localhost:8765/v1/health"},
		{"negotiate", "http://localhost:8765/v1/health"},
		{"trust", "http://localhost:8765/v1/health"},
		{"review", "http://localhost:8765/v1/health"},
		{"verify", "http://localhost:8765/v1/health"},
		{"marketplace", "http://localhost:8765/v1/health"},
		{"estimate", "http://localhost:8765/v1/health"},
	}
	for _, d := range defaults {
		agg.Register(d.name, httpSubsystemHealth{name: d.name, url: d.url, timeout: timeout})
	}
}

// httpSubsystemHealth implements health.SubsystemHealth by issuing a
// single GET request with a bounded timeout. It classifies the response
// as healthy / degraded / down based on the HTTP status code and any
// network error.
type httpSubsystemHealth struct {
	name    string
	url     string
	timeout time.Duration
}

// HealthCheck performs the HTTP probe and converts the result into a
// SubsystemStatus. Returns "degraded" for 4xx (subsystem is up but
// rejecting requests) and "down" for 5xx or network errors.
func (h httpSubsystemHealth) HealthCheck(ctx context.Context) health.SubsystemStatus {
	probeCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, h.url, nil)
	if err != nil {
		return health.SubsystemStatus{
			Name:      h.name,
			State:     health.StateDown,
			Message:   fmt.Sprintf("invalid request: %v", err),
			UpdatedAt: time.Now().UTC(),
		}
	}

	client := &http.Client{
		Timeout: h.timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return health.SubsystemStatus{
			Name:      h.name,
			State:     health.StateDown,
			Message:   fmt.Sprintf("probe failed: %v", err),
			UpdatedAt: time.Now().UTC(),
		}
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return health.SubsystemStatus{
			Name:      h.name,
			State:     health.StateHealthy,
			Message:   fmt.Sprintf("HTTP %d", resp.StatusCode),
			UpdatedAt: time.Now().UTC(),
		}
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return health.SubsystemStatus{
			Name:      h.name,
			State:     health.StateDegraded,
			Message:   fmt.Sprintf("HTTP %d (rejecting requests)", resp.StatusCode),
			UpdatedAt: time.Now().UTC(),
		}
	default:
		return health.SubsystemStatus{
			Name:      h.name,
			State:     health.StateDown,
			Message:   fmt.Sprintf("HTTP %d", resp.StatusCode),
			UpdatedAt: time.Now().UTC(),
		}
	}
}

// ============================================================================
// Renderers
// ============================================================================

// renderStatusTable prints the DashboardReport as a structured table:
// an overall line, per-subsystem status with icon + message, and a
// footer with critical/degraded summaries.
func renderStatusTable(report *health.DashboardReport, stdout io.Writer) int {
	fmt.Fprintf(stdout, "Helix Platform Status\n")
	fmt.Fprintf(stdout, "=====================\n\n")
	fmt.Fprintf(stdout, "Overall: %s    Generated: %s    Latency: %dms\n\n",
		report.Overall, report.GeneratedAt.Format(time.RFC3339), report.Latency.Milliseconds())

	// Column header
	fmt.Fprintf(stdout, "%-16s %-12s %s\n", "SUBSYSTEM", "STATE", "MESSAGE")
	fmt.Fprintf(stdout, "%s\n", strings.Repeat("-", 70))

	// Subsystems in stable order (PlatformHealthAggregator already
	// preserves registration order, but we sort for determinism).
	subs := make([]health.SubsystemStatus, len(report.Subsystems))
	copy(subs, report.Subsystems)
	sort.Slice(subs, func(i, j int) bool {
		return subs[i].Name < subs[j].Name
	})

	for _, s := range subs {
		fmt.Fprintf(stdout, "%-16s %-12s %s\n", s.Name, s.State, s.Message)
	}

	if len(report.Critical) > 0 {
		fmt.Fprintf(stdout, "\nCritical failures: %s\n", strings.Join(report.Critical, ", "))
	}
	if len(report.Degraded) > 0 {
		fmt.Fprintf(stdout, "Degraded:         %s\n", strings.Join(report.Degraded, ", "))
	}

	fmt.Fprintln(stdout)
	switch {
	case report.HasCriticalFailure():
		fmt.Fprintln(stdout, "Result: CRITICAL — one or more core subsystems are DOWN.")
		return 2
	case !report.AllHealthy():
		fmt.Fprintln(stdout, "Result: DEGRADED — some subsystems are not fully healthy.")
		return 1
	default:
		fmt.Fprintln(stdout, "Result: HEALTHY — all subsystems reporting nominal.")
		return 0
	}
}

// renderStatusJSON emits the DashboardReport as JSON. Uses the
// DashboardReport's MarshalJSON which formats latency in milliseconds
// and cache_ttl in seconds.
func renderStatusJSON(report *health.DashboardReport, stdout, stderr io.Writer) int {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "helix status: marshal: %v\n", err)
		return 2
	}
	fmt.Fprintln(stdout, string(data))

	switch {
	case report.HasCriticalFailure():
		return 2
	case !report.AllHealthy():
		return 1
	default:
		return 0
	}
}

// renderDryRunStatus prints a stub "all healthy" report without
// probing any subsystems. Honours --json when set.
func renderDryRunStatus(flags statusFlags, stdout io.Writer) int {
	if flags.jsonOutput {
		stub := &health.DashboardReport{
			Overall:     health.StateHealthy,
			Subsystems:  []health.SubsystemStatus{},
			GeneratedAt: time.Now().UTC(),
			CacheTTL:    flags.cacheTTL,
		}
		data, _ := json.MarshalIndent(stub, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintln(stdout, "Helix Platform Status — DRY RUN")
	fmt.Fprintln(stdout, "================================")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Overall: healthy (dry-run: no probes issued)")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Result: HEALTHY (dry-run).")
	return 0
}

// ============================================================================
// Usage
// ============================================================================

// printStatusUsage prints `helix status` help. Same shape as the
// other subcommands — short, scannable, examples.
func printStatusUsage(w io.Writer) {
	fmt.Fprintf(w, `helix status — unified platform health view

USAGE:
  helix status [flags...]

FLAGS:
  --json         Emit machine-readable JSON instead of the table
  --no-cache     Force a fresh aggregate, bypassing the in-process cache
  --timeout      Per-subsystem HTTP probe timeout (default 5s)
  --cache-ttl    In-process aggregator cache TTL (default 15s)

ENV VARS:
  HELIX_STATUS_JSON
  HELIX_STATUS_NO_CACHE
  HELIX_STATUS_TIMEOUT
  HELIX_STATUS_CACHE_TTL

EXIT CODES:
  0 — Every subsystem is healthy
  1 — One or more subsystems are degraded (non-critical)
  2 — Critical subsystem is DOWN — platform cannot serve traffic

EXAMPLES:
  helix status
  helix status --json
  helix status --no-cache --timeout 10s
  helix status --json | jq '.critical_failures'
`)
}
