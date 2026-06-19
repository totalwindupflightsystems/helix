// Command helix-identity provisions and manages Forgejo accounts for Helix
// agents. See specs/agent-identity.md for the full specification.
//
// Usage:
//
//	helix identity sync                          # sync all active agents
//	helix identity provision <name>              # provision one agent
//	helix identity deprovision <name>            # offboard one agent
//	helix identity status                        # show current state
//	helix identity keygen <name>                 # mint a fresh SSH keypair
//
// All commands accept the flags documented in newRootCmd. Secrets are read
// exclusively from environment variables — never CLI args.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/helixloop/helix/pkg/identity"
)

// Version is set at build time via -ldflags "-X main.Version=...". Default
// for `go run` is "dev".
var Version = "dev"

// Global flags shared by every subcommand. Bound to env vars in PersistentPreRun.
var (
	flagForgejoURL    string
	flagAdminToken    string
	flagKnownFriends  string
	flagSSHKeyDir     string
	flagDryRun        bool
	flagVerbose       bool
	flagForgejoInsecure bool
)

// Exit codes — chosen to be stable across releases so cron jobs and other
// orchestrators can rely on them.
const (
	exitOK           = 0
	exitUsage        = 2 // cobra arg/flag errors
	exitConfig       = 3 // env var missing or invalid
	exitPartialSync  = 4 // one or more agents failed during sync
	exitNotImpl      = 5 // stub code path hit; CLI is not yet wired to real API
	exitInternal     = 1 // unexpected panic or programmer error
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(exitInternal)
	}
}

// ---------------------------------------------------------------------------
// Root command
// ---------------------------------------------------------------------------

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "helix",
		Short: "Helix — agent-first code platform",
		Long: `helix is the CLI entry point for the Helix platform.

The identity subcommand provisions and manages Forgejo accounts for
Helix agents. All other Helix subsystems (cost estimator, PR negotiation,
prompt registry, agent marketplace) will land as additional subcommands
in future releases.

Version: ` + Version,
		SilenceUsage:  true, // don't dump usage on every error
		SilenceErrors: true, // we print errors ourselves for stable formatting
	}

	root.PersistentFlags().StringVar(&flagForgejoURL, "forgejo-url",
		envOr("HELIX_FORGEJO_URL", ""),
		"Forgejo base URL (e.g. https://forge.helixloop.dev). "+
			"Env: HELIX_FORGEJO_URL")
	root.PersistentFlags().StringVar(&flagAdminToken, "admin-token",
		envOr("HELIX_FORGEJO_ADMIN_TOKEN", ""),
		"Admin personal access token for /api/v1/admin/* endpoints. "+
			"Env: HELIX_FORGEJO_ADMIN_TOKEN. NEVER pass on the CLI in shared envs.")
	root.PersistentFlags().StringVar(&flagKnownFriends, "known-friends",
		envOr("HELIX_KNOWN_FRIENDS_PATH", "/opt/hermes-demo/.hermes/h4f/known-friends.json"),
		"Path to H4F known-friends.json. "+
			"Env: HELIX_KNOWN_FRIENDS_PATH")
	root.PersistentFlags().StringVar(&flagSSHKeyDir, "ssh-key-dir",
		envOr("HELIX_SSH_KEY_DIR", "/var/lib/helix/ssh-keys"),
		"Directory where SSH keypairs are persisted. "+
			"Env: HELIX_SSH_KEY_DIR")
	root.PersistentFlags().BoolVar(&flagDryRun, "dry-run", false,
		"Print intended actions; make no Forgejo API calls.")
	root.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false,
		"Verbose logging.")
	root.PersistentFlags().BoolVar(&flagForgejoInsecure, "forgejo-insecure", false,
		"Skip TLS verification on Forgejo calls. DEV ONLY. "+
			"Env: HELIX_FORGEJO_INSECURE")

	root.AddCommand(newIdentityCmd())
	return root
}

// envOr returns os.Getenv(key) if set and non-empty, else fallback.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ---------------------------------------------------------------------------
// helix identity <subcommand>
// ---------------------------------------------------------------------------

func newIdentityCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "identity",
		Short: "Manage Forgejo identities for Helix agents",
		Long: `Provision, deprovision, and inspect Forgejo accounts backed by the
H4F known-friends.json roster.

The five subcommands map to the lifecycle stages:
  sync        — full reconciliation (one-shot, cron-friendly)
  provision   — add a single agent
  deprovision — offboard a single agent
  status      — show current roster + Forgejo state
  keygen      — mint a fresh SSH keypair (no Forgejo side effects)

All commands are safe to re-run; see specs/agent-identity.md §7.3
(Idempotent Provisioning).`,
	}

	c.AddCommand(newSyncCmd())
	c.AddCommand(newProvisionCmd())
	c.AddCommand(newDeprovisionCmd())
	c.AddCommand(newStatusCmd())
	c.AddCommand(newKeygenCmd())
	return c
}

// ---------------------------------------------------------------------------
// helix identity sync
// ---------------------------------------------------------------------------

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Reconcile known-friends.json with Forgejo state",
		Long: `Read known-friends.json and provision (or deprovision) every agent
to match the roster. Exits 0 on full success, 4 if any agent fails
(see "Exit Codes" in the root help).

Designed to be cron-driven:
    0 */6 * * * helix identity sync --dry-run=false >> /var/log/helix.log

The sync is idempotent — running it twice in a row is safe and the
second run reports Action=Unchanged for every agent.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := buildProvisionerConfig()
			if err != nil {
				return configError(err)
			}
			prov, err := identity.NewProvisioner(cfg)
			if err != nil {
				return configError(err)
			}
			syncer, err := identity.NewSyncer(prov, identity.SyncOptions{
				KnownFriendsPath: flagKnownFriends,
			})
			if err != nil {
				return configError(err)
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			report, err := syncer.Sync(ctx)
			printSyncReport(report)

			// Surface ErrNotImplemented as a distinct exit code so the
			// build pipeline can gate "stubbed" → "wired" transitions.
			if errors.Is(err, identity.ErrNotImplemented) {
				os.Exit(exitNotImpl)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "sync: %v\n", err)
				os.Exit(exitInternal)
			}
			if report.HasFailures() {
				os.Exit(exitPartialSync)
			}
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// helix identity provision <name>
// ---------------------------------------------------------------------------

func newProvisionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "provision <name>",
		Short: "Provision a single agent's Forgejo account",
		Long: `Provision one agent by name (e.g. "wojons"). Reads the agent
record from known-friends.json and runs the same flow as ` + "`sync`" + `
for that agent only. Errors from a single agent are reported and
cause exit code 4.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSingleAgent(args[0], syncModeProvision)
		},
	}
}

// ---------------------------------------------------------------------------
// helix identity deprovision <name>
// ---------------------------------------------------------------------------

func newDeprovisionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deprovision <name>",
		Short: "Offboard an agent (revoke tokens, archive keys)",
		Long: `Revoke the agent's personal access token and rename its SSH
keypair to *.offboarded. The Forgejo user account is NOT deleted —
historical PRs and comments remain attributable. See
specs/agent-identity.md §6.5 for the deprovisioning lifecycle.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSingleAgent(args[0], syncModeDeprovision)
		},
	}
}

// syncMode selects which provisioner path to drive for single-agent commands.
type syncMode int

const (
	syncModeProvision syncMode = iota
	syncModeDeprovision
)

func runSingleAgent(name string, mode syncMode) error {
	cfg, err := buildProvisionerConfig()
	if err != nil {
		return configError(err)
	}
	prov, err := identity.NewProvisioner(cfg)
	if err != nil {
		return configError(err)
	}
	onlyFalse := false
	syncer, err := identity.NewSyncer(prov, identity.SyncOptions{
		KnownFriendsPath: flagKnownFriends,
		OnlyActive:       &onlyFalse, // single-agent runs respect the agent's actual status
		AgentFilter:      []string{name},
	})
	if err != nil {
		return configError(err)
	}

	// Pre-load to give a friendly error when the agent doesn't exist in
	// the roster (instead of a silent no-op from the filter step).
	kf, err := loadKnownFriendsOrExit(flagKnownFriends)
	if err != nil {
		return err
	}
	if _, ok := kf.Agents[name]; !ok {
		fmt.Fprintf(os.Stderr, "agent %q not found in %s\n", name, flagKnownFriends)
		os.Exit(exitUsage)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	report, err := syncer.Sync(ctx)
	// Restrict output to the requested agent — the report still contains
	// only that one result because of AgentFilter, but be defensive.
	filtered := &identity.SyncReport{
		StartedAt: report.StartedAt, FinishedAt: report.FinishedAt,
		Duration: report.Duration, DryRun: report.DryRun, Source: report.Source,
	}
	for _, r := range report.Results {
		if r.Agent == name {
			filtered.Results = append(filtered.Results, r)
		}
	}
	filtered.Counts = aggregateFiltered(filtered.Results)
	printSyncReport(filtered)

	if mode == syncModeDeprovision {
		// Override the action for display purposes — the syncer will have
		// picked the right Provision/Deprovision path based on status,
		// but for offboarded agents we want to make this unambiguous.
		_ = mode
	}

	if errors.Is(err, identity.ErrNotImplemented) {
		os.Exit(exitNotImpl)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(exitInternal)
	}
	if filtered.HasFailures() {
		os.Exit(exitPartialSync)
	}
	return nil
}

// ---------------------------------------------------------------------------
// helix identity status
// ---------------------------------------------------------------------------

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show roster and Forgejo account state",
		Long: `Print a table of every agent in known-friends.json with:
  - status (active/pending/offboarded)
  - tier (pro/flash)
  - Forgejo account existence (live GET against /api/v1/users/{name})
  - PAT last-eight (if known)

This command does NOT mutate state. Safe to run as a dashboard.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			kf, err := loadKnownFriendsOrExit(flagKnownFriends)
			if err != nil {
				return err
			}
			cfg, err := buildProvisionerConfig()
			if err != nil {
				return configError(err)
			}
			prov, err := identity.NewProvisioner(cfg)
			if err != nil {
				return configError(err)
			}
			_ = prov // the stub uses prov.userExists; status command will
			// light up once userExists is implemented. Until then it
			// prints a placeholder column.

			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "AGENT\tSTATUS\tTIER\tFORGEJO\tPAT")
			fmt.Fprintln(tw, "-----\t------\t----\t-------\t---")
			for _, name := range sortedAgentNames(kf) {
				a := kf.Agents[name]
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					a.Name, a.Status, a.Tier,
					"(stub)", "(stub)")
			}
			return tw.Flush()
		},
	}
}

// ---------------------------------------------------------------------------
// helix identity keygen <name>
// ---------------------------------------------------------------------------

func newKeygenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "keygen <name>",
		Short: "Generate a fresh ED25519 SSH keypair for an agent",
		Long: `Mint a new keypair, write <name>.pub / <name>.key.pem /
<name>.fingerprint under --ssh-key-dir, and print the public key line
on stdout (suitable for piping into authorized_keys or
`+"`helix identity provision`"+`).

Does NOT register the key with Forgejo — use ` + "`helix identity provision`" + `
afterwards for that step.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := buildProvisionerConfig()
			if err != nil {
				return configError(err)
			}
			dir, err := identity.EnsureSSHKeyDir(flagSSHKeyDir)
			if err != nil {
				return configError(err)
			}
			prov, err := identity.NewProvisioner(cfg)
			if err != nil {
				return configError(err)
			}
			_ = prov

			kf, err := loadKnownFriendsOrExit(flagKnownFriends)
			if err != nil {
				return err
			}
			a, ok := kf.Agents[args[0]]
			if !ok {
				fmt.Fprintf(os.Stderr, "agent %q not found in %s\n", args[0], flagKnownFriends)
				os.Exit(exitUsage)
			}

			// Stub — real impl in provisioner.go::Keygen. Returns
			// identity.ErrNotImplemented so the CLI exits 5.
			_, err = prov.Keygen(a)
			_ = dir
			if errors.Is(err, identity.ErrNotImplemented) {
				fmt.Fprintln(os.Stderr,
					"keygen: stub — real implementation pending spec approval")
				os.Exit(exitNotImpl)
			}
			if err != nil {
				return err
			}
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func buildProvisionerConfig() (identity.ProvisionerConfig, error) {
	if flagForgejoURL == "" {
		return identity.ProvisionerConfig{}, errors.New(
			"HELIX_FORGEJO_URL (or --forgejo-url) is required")
	}
	if flagAdminToken == "" {
		return identity.ProvisionerConfig{}, errors.New(
			"HELIX_FORGEJO_ADMIN_TOKEN (or --admin-token) is required")
	}
	return identity.ProvisionerConfig{
		ForgejoBaseURL: flagForgejoURL,
		AdminToken:     flagAdminToken,
		SSHKeyDir:      flagSSHKeyDir,
		DryRun:         flagDryRun,
	}, nil
}

// configError prints a friendly config message and returns the sentinel.
// The caller decides the exit code.
func configError(err error) error {
	fmt.Fprintf(os.Stderr, "config: %v\n", err)
	return err
}

// printSyncReport writes the human-readable summary + machine-readable
// JSON to stdout. JSON goes to stderr-friendly channels in cron mode;
// we send both to stdout and let the operator redirect.
func printSyncReport(r *identity.SyncReport) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "AGENT\tACTION\tFORGEJO_ID\tPAT\tDURATION\tERROR\n")
	fmt.Fprintf(tw, "-----\t------\t----------\t---\t--------\t-----\n")
	for _, res := range r.Results {
		errStr := res.Error
		if errStr == "" {
			errStr = "-"
		}
		patStr := res.PATLastEight
		if patStr == "" {
			patStr = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n",
			res.Agent, res.Action, res.ForgejoAccountID,
			patStr, res.Duration.Round(time.Millisecond), errStr)
	}
	tw.Flush()

	fmt.Fprintf(os.Stdout,
		"\nSummary: total=%d created=%d updated=%d unchanged=%d "+
			"deprovisioned=%d skipped=%d failed=%d  (dry_run=%t, took=%s)\n",
		r.Counts.Total, r.Counts.Created, r.Counts.Updated, r.Counts.Unchanged,
		r.Counts.Deprovisioned, r.Counts.Skipped, r.Counts.Failed,
		r.DryRun, r.Duration.Round(time.Millisecond))

	// Also dump structured JSON for log shippers.
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(r)
}

// aggregateFiltered mirrors identity.aggregateCounts but only over the
// filtered slice (defensive — the filtered report should already be
// single-agent in practice).
func aggregateFiltered(results []identity.ProvisioningResult) identity.SyncCounts {
	var c identity.SyncCounts
	c.Total = len(results)
	for _, r := range results {
		switch r.Action {
		case identity.ActionCreated:
			c.Created++
		case identity.ActionUpdated:
			c.Updated++
		case identity.ActionUnchanged:
			c.Unchanged++
		case identity.ActionDeprovisioned:
			c.Deprovisioned++
		case identity.ActionSkipped:
			c.Skipped++
		case identity.ActionFailed:
			c.Failed++
		}
	}
	return c
}

// sortedAgentNames returns the keys of kf.Agents sorted ascending.
func sortedAgentNames(kf *identity.KnownFriends) []string {
	out := make([]string, 0, len(kf.Agents))
	for name := range kf.Agents {
		out = append(out, name)
	}
	// insertion sort — small N, no need for sort.Strings import.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// loadKnownFriendsOrExit is a CLI-friendly wrapper around the package's
// loadKnownFriends. On failure, prints a clear message and exits 3.
func loadKnownFriendsOrExit(path string) (*identity.KnownFriends, error) {
	// The package's loader is unexported; here we re-implement the read
	// inline because the cmd package must not depend on unexported
	// internals. Behavior must match loadKnownFriends exactly; the
	// identity package's contract test pins it.
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
		os.Exit(exitConfig)
	}
	var kf identity.KnownFriends
	if err := json.Unmarshal(data, &kf); err != nil {
		fmt.Fprintf(os.Stderr, "parse %s: %v\n", path, err)
		os.Exit(exitConfig)
	}
	return &kf, nil
}