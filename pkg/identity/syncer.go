// Package identity — sync orchestration.
//
// Syncer reads known-friends.json, classifies each agent by status, and
// drives the Provisioner (or Deprovisioner) per agent. It is responsible
// for partial-sync recovery, idempotency reporting, and the final
// SyncReport aggregation that the CLI prints to stdout.
package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ---------------------------------------------------------------------------
// Sync inputs
// ---------------------------------------------------------------------------

// SyncOptions controls a single sync run.
type SyncOptions struct {
	// KnownFriendsPath is the absolute filesystem path to known-friends.json.
	// Default: /opt/hermes-demo/.hermes/h4f/known-friends.json.
	KnownFriendsPath string

	// OnlyActive skips pending/offboarded agents. Default: true.
	OnlyActive *bool

	// AgentFilter, if non-empty, restricts the run to the named agent(s).
	// Used by `helix identity provision <name>` and `deprovision <name>`.
	AgentFilter []string

	// ContinueOnError keeps going after a per-agent failure. Default: true.
	// When false, the first failure aborts the run.
	ContinueOnError *bool
}

// SetDefaults fills in zero-valued fields with sensible defaults.
func (o *SyncOptions) SetDefaults() {
	if o.KnownFriendsPath == "" {
		o.KnownFriendsPath = "/opt/hermes-demo/.hermes/h4f/known-friends.json"
	}
	if o.OnlyActive == nil {
		t := true
		o.OnlyActive = &t
	}
	if o.ContinueOnError == nil {
		t := true
		o.ContinueOnError = &t
	}
}

// SyncReport is the top-level output of Sync(). The CLI prints it as
// structured JSON (one per line for log scrapers) and a human-readable
// summary table.
type SyncReport struct {
	StartedAt  time.Time           `json:"started_at"`
	FinishedAt time.Time           `json:"finished_at"`
	Duration   time.Duration       `json:"duration_ns"`
	DryRun     bool                `json:"dry_run"`
	Source     string              `json:"source_path"`
	Results    []ProvisioningResult `json:"results"`

	// Counts is derived from Results — populated by AggregateCounts.
	Counts SyncCounts `json:"counts"`
}

// SyncCounts is the at-a-glance summary used in CLI exit-code logic and
// the human-readable report.
type SyncCounts struct {
	Total          int `json:"total"`
	Created        int `json:"created"`
	Updated        int `json:"updated"`
	Unchanged      int `json:"unchanged"`
	Deprovisioned  int `json:"deprovisioned"`
	Skipped        int `json:"skipped"`
	Failed         int `json:"failed"`
}

// HasFailures is true if any per-agent result has Action=ActionFailed.
// The CLI exits non-zero in this case.
func (r *SyncReport) HasFailures() bool {
	return r.Counts.Failed > 0
}

// ---------------------------------------------------------------------------
// Syncer
// ---------------------------------------------------------------------------

// Syncer orchestrates a sync run. One Syncer is created per CLI invocation;
// it is not safe for concurrent use (the Provisioner beneath it is, but the
// driver loop is single-threaded by design — see §7.2 of the spec).
type Syncer struct {
	provisioner *Provisioner
	opts        SyncOptions
}

// NewSyncer wires a Syncer to a Provisioner. Returns an error if opts is
// invalid.
func NewSyncer(p *Provisioner, opts SyncOptions) (*Syncer, error) {
	opts.SetDefaults()
	if p == nil {
		return nil, errors.New("identity: nil provisioner")
	}
	return &Syncer{provisioner: p, opts: opts}, nil
}

// Sync is the main entry point. Steps:
//
//  1. Load known-friends.json from opts.KnownFriendsPath.
//  2. Sort agents by name for deterministic output (and so logs read
//     alphabetically).
//  3. Apply opts.AgentFilter (if set).
//  4. For each surviving agent:
//       - active     → Provisioner.Provision
//       - offboarded → Provisioner.Deprovision
//       - pending    → skip (or provision if !opts.OnlyActive)
//  5. Aggregate counts, build SyncReport, return.
//
// On a per-agent failure with opts.ContinueOnError=true, the error is
// recorded in the result and the loop continues. With
// opts.ContinueOnError=false, the loop aborts at the first failure.
func (s *Syncer) Sync(ctx context.Context) (*SyncReport, error) {
	report := &SyncReport{
		StartedAt: time.Now(),
		DryRun:    s.provisioner.cfg.DryRun,
		Source:    s.opts.KnownFriendsPath,
	}

	// Step 1: load.
	kf, err := loadKnownFriends(s.opts.KnownFriendsPath)
	if err != nil {
		report.FinishedAt = time.Now()
		report.Duration = report.FinishedAt.Sub(report.StartedAt)
		return report, fmt.Errorf("identity: load known-friends.json: %w", err)
	}

	// Step 2: sort.
	agents := sortedAgents(kf)

	// Step 3: filter.
	agents = s.applyFilter(agents)

	// Step 4: drive.
	for _, a := range agents {
		if err := ctx.Err(); err != nil {
			report.Results = append(report.Results, ProvisioningResult{
				Agent:  a.Name,
				Action: ActionFailed,
				Error:  "context cancelled: " + err.Error(),
			})
			break
		}

		var (
			res *ProvisioningResult
			err error
		)
		switch {
		case a.Status == StatusOffboarded:
			res, err = s.provisioner.Deprovision(ctx, a)
		case a.Status == StatusActive ||
			(a.Status == StatusPending && !*s.opts.OnlyActive):
			res, err = s.provisioner.Provision(ctx, a)
		default:
			// Pending with OnlyActive=true — skip silently.
			res = &ProvisioningResult{
				Agent:  a.Name,
				Action: ActionSkipped,
				Skipped: true,
			}
		}

		if err != nil {
			res.Action = ActionFailed
			res.Error = err.Error()
			if !*s.opts.ContinueOnError {
				report.Results = append(report.Results, *res)
				break
			}
		}
		report.Results = append(report.Results, *res)
	}

	// Step 5: aggregate.
	report.FinishedAt = time.Now()
	report.Duration = report.FinishedAt.Sub(report.StartedAt)
	report.Counts = aggregateCounts(report.Results)
	return report, nil
}

// applyFilter restricts agents to the names in opts.AgentFilter.
// If AgentFilter is empty, returns agents unchanged.
func (s *Syncer) applyFilter(agents []*Agent) []*Agent {
	if len(s.opts.AgentFilter) == 0 {
		return agents
	}
	wanted := make(map[string]struct{}, len(s.opts.AgentFilter))
	for _, n := range s.opts.AgentFilter {
		wanted[n] = struct{}{}
	}
	out := make([]*Agent, 0, len(wanted))
	for _, a := range agents {
		if _, ok := wanted[a.Name]; ok {
			out = append(out, a)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Helpers (pure functions, exposed for testing)
// ---------------------------------------------------------------------------

// loadKnownFriends reads and parses the file at path. The stub uses os.ReadFile
// only — the real impl may also support HTTP fetch (e.g. from the Hetzner
// box over HTTPS) via the H4F_READ_URL env var. See specs/agent-identity.md
// §5.2 (Integration Points).
func loadKnownFriends(path string) (*KnownFriends, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var kf KnownFriends
	if err := json.Unmarshal(data, &kf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &kf, nil
}

// sortedAgents returns kf.Agents as a slice sorted by Name ascending.
// Pure function — easy to unit test.
func sortedAgents(kf *KnownFriends) []*Agent {
	out := make([]*Agent, 0, len(kf.Agents))
	for _, a := range kf.Agents {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// aggregateCounts tallies Actions across all results.
func aggregateCounts(results []ProvisioningResult) SyncCounts {
	var c SyncCounts
	c.Total = len(results)
	for _, r := range results {
		switch r.Action {
		case ActionCreated:
			c.Created++
		case ActionUpdated:
			c.Updated++
		case ActionUnchanged:
			c.Unchanged++
		case ActionDeprovisioned:
			c.Deprovisioned++
		case ActionSkipped:
			c.Skipped++
		case ActionFailed:
			c.Failed++
		}
	}
	return c
}

// ---------------------------------------------------------------------------
// Pure helpers used by tests and (eventually) the provisioner
// ---------------------------------------------------------------------------

// EmailFor returns the canonical agent email. Exported so tests can pin it.
func EmailFor(name string) string {
	return name + "@helix-agents.local"
}

// BioFor returns the canonical agent bio per the task brief.
func BioFor(tier AgentTier) string {
	return fmt.Sprintf("Helix Agent — %s tier. Part of the Helix agent fleet.", tier)
}

// WebsiteFor is the placeholder until helixloop.dev is live.
func WebsiteFor() string {
	return "https://helixloop.dev"
}

// EnsureSSHKeyDir creates the directory if missing and returns its absolute
// path. Permission 0700 — keys must not be world-readable.
func EnsureSSHKeyDir(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("identity: empty SSHKeyDir")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return "", err
	}
	return abs, nil
}

// DisplayNameFor returns the agent's display name with a fallback to the
// short name. Mirrors the policy used when building CreateUserRequest.
func DisplayNameFor(a *Agent) string {
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return a.Name
}