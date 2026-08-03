// Command helix-identity provisions Helix agent accounts in a self-hosted
// Forgejo instance, keyed off known-friends.json.
//
// Subcommands:
//
//	helix identity sync         Provision all active agents
//	helix identity provision N  Provision a single named agent
//	helix identity deprovision N  Revoke an agent's PAT, archive keys
//	helix identity status       Show provisioned agent status table
//	helix identity keygen N     Generate a fresh ED25519 keypair (no Forgejo)
//	helix identity create       Create a portable agent identity (HID)
//	helix identity register     Register an HID as a Forgejo OAuth2 app
//	helix identity verify       Verify an HID file's signature
//	helix identity export       Export an HID as JSON or a Nostr kind-0 event
//	helix identity import       Import an HID file and print its identity
//	helix identity list         List OAuth2 apps (agents) registered at a forge
//
// Every flag has a matching environment variable (documented below). No
// credentials are ever read from or written to disk by this binary — the
// only persisted secrets are the per-agent private keys, written by the
// syncer with mode 0600 under ~/.helix/keys/<agent>/.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/totalwindupflightsystems/helix/internal/observability"
	"github.com/totalwindupflightsystems/helix/pkg/identity"
)

// envVar documentation: every flag below resolves from CLI > env > default.
//
//	FORGEJO_URL              → --forgejo-url
//	FORGEJO_ADMIN_TOKEN      → --admin-token
//	FORGEJO_ADMIN_USER       → --admin-user        (BasicAuth for token create/revoke)
//	FORGEJO_ADMIN_PASSWORD   → --admin-password    (BasicAuth for token create/revoke)
//	HELIX_KNOWN_FRIENDS      → --known-friends
//	HELIX_SSH_KEY_DIR        → --ssh-key-dir
//	HELIX_STATE_PATH         → --state-path

const (
	envForgejoURL    = "FORGEJO_URL"
	envAdminToken    = "FORGEJO_ADMIN_TOKEN"
	envAdminUser     = "FORGEJO_ADMIN_USER"
	envAdminPassword = "FORGEJO_ADMIN_PASSWORD"
	envKnownFriends  = "HELIX_KNOWN_FRIENDS"
	envSSHKeyDir     = "HELIX_SSH_KEY_DIR"
	envStatePath     = "HELIX_STATE_PATH"
)

// flagHolder keeps every CLI flag in one struct so subcommand funcs can
// build a ProvisionerConfig without juggling 10 closure variables. Cobra
// binds flags into these fields via cmd.Flags().*Var.
type flagHolder struct {
	forgejoURL    string
	adminToken    string
	adminUser     string
	adminPassword string
	knownFriends  string
	sshKeyDir     string
	statePath     string
	dryRun        bool
	verbose       bool
}

// rootFlags is the singleton flag holder populated by Cobra. Subcommands
// read from it via buildConfig().
var rootFlags = &flagHolder{}

// ---------------------------------------------------------------------------
// Cobra command tree
// ---------------------------------------------------------------------------

func main() {
	// Initialise structured logging as early as possible so any error
	// surfaced during cobra arg parsing shows up in the operator log
	// pipeline. Config is driven by env vars (HELIX_LOG_FORMAT,
	// HELIX_LOG_FILE, HELIX_AGENT_ID, HELIX_LOG).
	if _, err := observability.Init(observability.Options{App: "helix-identity"}); err != nil {
		fmt.Fprintf(os.Stderr, "helix-identity: failed to initialise logger: %v\n", err)
		os.Exit(identity.ExitGeneral)
	}

	rootCmd := &cobra.Command{
		Use:   "helix-identity",
		Short: "Provision Helix agent identities in Forgejo",
		Long: `helix-identity provisions and manages Helix agent accounts in a
self-hosted Forgejo instance. Agents are sourced from known-friends.json;
each active agent gets a Forgejo account, an ED25519 SSH keypair, and a
scoped personal access token (PAT).

All credentials are read from environment variables or flags — none are
ever written to disk beyond the per-agent private keys under
~/.helix/keys/<agent>/ (mode 0600).

Supports Forgejo v1.21+. Uses admin BasicAuth for user creation and key
registration; uses admin-list for idempotency probes (handles
visibility:limited users). Dry-run mode simulates the full flow without
touching Forgejo.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.PersistentFlags().StringVar(&rootFlags.forgejoURL, "forgejo-url",
		envOr(envForgejoURL, ""),
		"Forgejo base URL (env: "+envForgejoURL+")")
	rootCmd.PersistentFlags().StringVar(&rootFlags.adminToken, "admin-token",
		envOr(envAdminToken, ""),
		"Forgejo admin token (env: "+envAdminToken+")")
	rootCmd.PersistentFlags().StringVar(&rootFlags.adminUser, "admin-user",
		envOr(envAdminUser, ""),
		"Forgejo admin username for BasicAuth token ops (env: "+envAdminUser+")")
	rootCmd.PersistentFlags().StringVar(&rootFlags.adminPassword, "admin-password",
		envOr(envAdminPassword, ""),
		"Forgejo admin password for BasicAuth token ops (env: "+envAdminPassword+")")
	rootCmd.PersistentFlags().StringVar(&rootFlags.knownFriends, "known-friends",
		envOr(envKnownFriends, identity.DefaultKnownFriendsPath),
		"path to known-friends.json (env: "+envKnownFriends+")")
	rootCmd.PersistentFlags().StringVar(&rootFlags.sshKeyDir, "ssh-key-dir",
		envOr(envSSHKeyDir, identity.DefaultSSHKeyDir),
		"root directory for per-agent SSH keys (env: "+envSSHKeyDir+")")
	rootCmd.PersistentFlags().StringVar(&rootFlags.statePath, "state-path",
		envOr(envStatePath, identity.DefaultStatePath),
		"path to idempotency state file (env: "+envStatePath+")")
	rootCmd.PersistentFlags().BoolVar(&rootFlags.dryRun, "dry-run", false,
		"simulate all operations without touching Forgejo or the filesystem")
	rootCmd.PersistentFlags().BoolVarP(&rootFlags.verbose, "verbose", "v", false,
		"log every API call with timing")

	rootCmd.AddCommand(
		newSyncCmd(),
		newProvisionCmd(),
		newDeprovisionCmd(),
		newStatusCmd(),
		newKeygenCmd(),
		newCreateCmd(),
		newRegisterCmd(),
		newVerifyCmd(),
		newExportCmd(),
		newImportCmd(),
		newListCmd(),
	)

	rc := executeRoot(rootCmd)
	os.Exit(rc)
}

// executeRoot wraps rootCmd.Execute() with the observability wrapper so
// every helix-identity invocation emits a "subcommand_complete" log
// line. The subcommand name is captured from cobra's first positional
// arg (defaults to "helix-identity" for bare invocations).
//
// Returns the process exit code (0 success, non-zero error).
//
// The wrapped function may return identity-typed errors which the
// wrapper extracts via the ExitCode() interface method. Cobra's
// own errors (parse errors, etc.) are mapped to ExitConfigError so
// they propagate correctly through os.Exit.
func executeRoot(rootCmd *cobra.Command) int {
	sub := "helix-identity"
	if args := rootCmd.Flags().Args(); len(args) > 0 {
		sub = "helix-identity:" + args[0]
	}
	err := observability.Run(sub, func() error {
		return rootCmd.Execute()
	})
	if err == nil {
		return 0
	}
	// Map typed errors to documented exit codes; unknown → ExitGeneral.
	if te, ok := err.(*identity.TypedError); ok {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", te.Error())
		return te.ExitCode()
	}
	fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
	return identity.ExitGeneral
}

// ---------------------------------------------------------------------------
// sync
// ---------------------------------------------------------------------------

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Provision all active agents from known-friends.json",
		Long: `Reads known-friends.json and provisions every active agent:
creates the Forgejo account, generates an ED25519 keypair, registers the
public key, and mints a scoped PAT. Offboarded agents have their PATs
revoked and keys archived.

Safe to re-run: existing accounts are detected and skipped (idempotent).`,
		RunE: runSync,
	}
}

func runSync(cmd *cobra.Command, args []string) error {
	cfg, err := buildConfig()
	if err != nil {
		return err
	}
	kf, err := identity.LoadKnownFriends(cfg.KnownFriendsPath)
	if err != nil {
		return err
	}
	if len(kf.Agents) == 0 {
		fmt.Println("NO_AGENTS: known-friends.json contains no agents")
		return nil
	}
	syncer, err := identity.NewSyncer(cfg, nil)
	if err != nil {
		return err
	}
	defer syncer.Provisioner().Close()

	results, err := syncer.Sync(kf, identity.SyncOptions{
		AdminUser:     rootFlags.adminUser,
		AdminPassword: rootFlags.adminPassword,
	})

	if cfg.DryRun {
		renderDryRun(kf)
	}
	renderResultsTable(results)

	// Even on partial failure we printed the table; surface the error so
	// main() maps it to an exit code.
	return err
}

// ---------------------------------------------------------------------------
// provision <name>
// ---------------------------------------------------------------------------

func newProvisionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "provision <name>",
		Short: "Provision a single named agent",
		Args:  cobra.ExactArgs(1),
		RunE:  runProvision,
	}
}

func runProvision(cmd *cobra.Command, args []string) error {
	cfg, err := buildConfig()
	if err != nil {
		return err
	}
	name := args[0]
	kf, err := identity.LoadKnownFriends(cfg.KnownFriendsPath)
	if err != nil {
		return err
	}
	agent, ok := kf.Agents[name]
	if !ok || agent == nil {
		return identity.NewConfigError(
			fmt.Sprintf("agent %q not found in %s", name, cfg.KnownFriendsPath), nil)
	}
	if agent.Name == "" {
		agent.Name = name
	}
	if err := agent.Validate(); err != nil {
		return err
	}
	syncer, err := identity.NewSyncer(cfg, nil)
	if err != nil {
		return err
	}
	defer syncer.Provisioner().Close()

	r, err := syncer.ProvisionOne(agent, identity.SyncOptions{
		AdminUser:     rootFlags.adminUser,
		AdminPassword: rootFlags.adminPassword,
	})
	renderSingleResult(r)
	return err
}

// ---------------------------------------------------------------------------
// deprovision <name>
// ---------------------------------------------------------------------------

func newDeprovisionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deprovision <name>",
		Short: "Revoke an agent's PAT and archive their keys",
		Long: `Deprovisioning revokes the agent's personal access token and
archives their SSH key material under a dated subdirectory. The Forgejo
account itself is preserved so historical git attribution remains intact
(see §3.2 of the spec: DELETE /admin/users is intentionally never used).`,
		Args: cobra.ExactArgs(1),
		RunE: runDeprovision,
	}
}

func runDeprovision(cmd *cobra.Command, args []string) error {
	cfg, err := buildConfig()
	if err != nil {
		return err
	}
	name := args[0]
	kf, err := identity.LoadKnownFriends(cfg.KnownFriendsPath)
	if err != nil {
		return err
	}
	agent, ok := kf.Agents[name]
	if !ok || agent == nil {
		// Allow deprovisioning an agent that's already gone from
		// known-friends.json (state file is the source of truth here).
		agent = &identity.Agent{Name: name, Status: identity.StatusOffboarded}
	}
	if agent.Name == "" {
		agent.Name = name
	}
	syncer, err := identity.NewSyncer(cfg, nil)
	if err != nil {
		return err
	}
	defer syncer.Provisioner().Close()

	r, err := syncer.DeprovisionOne(agent, identity.SyncOptions{
		AdminUser:     rootFlags.adminUser,
		AdminPassword: rootFlags.adminPassword,
	})
	renderSingleResult(r)
	return err
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show provisioned agent status (from the local state file)",
		Long: `Renders a table of every agent tracked in the local state file
(~/.helix/state.json). Does not contact Forgejo — this is a fast local
view. Use --verbose + sync to reconcile against the server.`,
		RunE: runStatus,
	}
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := buildConfig()
	if err != nil {
		return err
	}
	syncer, err := identity.NewSyncer(cfg, nil)
	if err != nil {
		return err
	}
	defer syncer.Provisioner().Close()
	renderStateTable(syncer.State())
	return nil
}

// ---------------------------------------------------------------------------
// keygen <name>
// ---------------------------------------------------------------------------

func newKeygenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "keygen <name>",
		Short: "Generate a fresh ED25519 keypair for an agent (no Forgejo)",
		Long: `Generates a new ED25519 keypair and writes the three files
under ~/.helix/keys/<name>/. Does NOT contact Forgejo — use this to rotate
a key independently of the provisioning flow. The public key still needs
to be registered manually (or via a subsequent provision run).`,
		Args: cobra.ExactArgs(1),
		RunE: runKeygen,
	}
}

func runKeygen(cmd *cobra.Command, args []string) error {
	cfg, err := buildConfig()
	if err != nil {
		return err
	}
	name := args[0]
	// keygen doesn't require known-friends.json — it just needs a name.
	agent := &identity.Agent{
		Name:        name,
		DisplayName: name,
		Status:      identity.StatusActive,
		Tier:        identity.TierPro,
	}
	syncer, err := identity.NewSyncer(cfg, nil)
	if err != nil {
		return err
	}
	defer syncer.Provisioner().Close()

	r, err := syncer.KeyGenOnly(agent)
	renderSingleResult(r)
	return err
}

// ---------------------------------------------------------------------------
// identity create — SPEC-022 portable agent identity
// ---------------------------------------------------------------------------

// createFlags holds the per-command flags for `helix identity create`.
type createFlags struct {
	name   string
	output string
}

func newCreateCmd() *cobra.Command {
	var f createFlags
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new portable agent identity (HID) with a fresh Ed25519 keypair",
		Long: `Creates a self-signed Helix Identity Document (SPEC-022 §2): a fresh
Ed25519 keypair, a UUIDv7 agent_id, and a signed HID JSON file. The private
key is written beside the HID as <output>.key (PKCS#8 PEM, mode 0600) so
later commands (export --format nostr) can sign on the agent's behalf.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(f)
		},
	}
	cmd.Flags().StringVar(&f.name, "name", "", "agent name (used as the default output filename)")
	cmd.Flags().StringVar(&f.output, "output", "", "output path for the HID file (default: <name>.hid)")
	return cmd
}

func runCreate(f createFlags) error {
	if strings.TrimSpace(f.name) == "" {
		return identity.NewConfigError("--name is required (e.g. --name builder-01)", nil)
	}
	out := f.output
	if out == "" {
		out = f.name + ".hid"
	}
	agent, privKey, err := identity.NewAgentIdentity(f.name)
	if err != nil {
		return err
	}
	if err := agent.Export(out, privKey); err != nil {
		return err
	}
	if err := writePrivateKeyPEM(out+".key", privKey); err != nil {
		return err
	}
	fmt.Printf("✅ created HID: %s\n", out)
	fmt.Printf("   agent_id:    %s\n", agent.ID)
	fmt.Printf("   fingerprint: %s\n", agent.Fingerprint())
	fmt.Printf("   private key: %s (mode 0600 — keep secret)\n", out+".key")
	return nil
}

// ---------------------------------------------------------------------------
// identity register — Forgejo OAuth2 registration
// ---------------------------------------------------------------------------

// registerFlags holds the per-command flags for `helix identity register`.
type registerFlags struct {
	forge       string
	agent       string
	redirectURI string
}

func newRegisterCmd() *cobra.Command {
	var f registerFlags
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register an HID as a Forgejo OAuth2 application",
		Long: `Registers the agent's HID with a Forgejo instance by creating an
OAuth2 application named helix-agent-<fingerprint-prefix> (SPEC-022 §5).
Uses FORGEJO_ADMIN_USER / FORGEJO_ADMIN_PASSWORD (or --admin-user /
--admin-password) for BasicAuth, like sync. The HID signature is verified
before registration; a tampered document is refused.

The client_secret is printed once — Forgejo never exposes it again.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRegister(f)
		},
	}
	cmd.Flags().StringVar(&f.forge, "forge", envOr(envForgejoURL, ""),
		"Forgejo base URL (env: "+envForgejoURL+")")
	cmd.Flags().StringVar(&f.agent, "agent", "", "path to the agent's HID file")
	cmd.Flags().StringVar(&f.redirectURI, "redirect-uri",
		"http://127.0.0.1:3000/oauth/callback",
		"OAuth2 redirect URI for the registered application")
	return cmd
}

func runRegister(f registerFlags) error {
	forge := strings.TrimRight(f.forge, "/")
	if forge == "" {
		return identity.NewConfigError("--forge URL is required (or set FORGEJO_URL)", nil)
	}
	if f.agent == "" {
		return identity.NewConfigError("--agent HID_PATH is required", nil)
	}
	hid, err := loadHID(f.agent)
	if err != nil {
		return err
	}
	if ok, verr := hid.Identity.Verify(hid); !ok || verr != nil {
		return identity.NewConfigError(
			fmt.Sprintf("refusing to register invalid HID %q: %v", f.agent, verr), nil)
	}
	if rootFlags.adminUser == "" {
		return identity.NewConfigError(
			"register needs Forgejo credentials: set FORGEJO_ADMIN_USER and FORGEJO_ADMIN_PASSWORD (or --admin-user/--admin-password)", nil)
	}
	registrar := identity.NewForgejoOAuthRegistrar(forge, rootFlags.adminUser, rootFlags.adminPassword)
	app, err := registrar.RegisterOAuthApp(context.Background(), hid, f.redirectURI)
	if err != nil {
		return err
	}
	fmt.Printf("✅ registered agent at %s\n", forge)
	fmt.Printf("   app:           %s\n", app.Name)
	fmt.Printf("   client_id:     %s\n", app.ClientID)
	fmt.Printf("   client_secret: %s\n", app.ClientSecret)
	fmt.Printf("   redirect_uris: %v\n", app.RedirectURIs)
	fmt.Println("   note: client_secret is shown once — capture it now; Forgejo never returns it again.")
	return nil
}

// ---------------------------------------------------------------------------
// identity verify
// ---------------------------------------------------------------------------

// verifyFlags holds the per-command flags for `helix identity verify`.
type verifyFlags struct {
	hid string
}

func newVerifyCmd() *cobra.Command {
	var f verifyFlags
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify an HID file's Ed25519 signature",
		Long: `Checks that the signature embedded in an HID file validates against
the identity's own public key. Exits 0 on a valid document, non-zero with
an actionable message on a tampered or malformed one.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerify(f)
		},
	}
	cmd.Flags().StringVar(&f.hid, "hid", "", "path to the HID file to verify")
	return cmd
}

func runVerify(f verifyFlags) error {
	if f.hid == "" {
		return identity.NewConfigError("--hid PATH is required", nil)
	}
	hid, err := loadHID(f.hid)
	if err != nil {
		return err
	}
	if ok, verr := hid.Identity.Verify(hid); !ok || verr != nil {
		return identity.NewConfigError(
			fmt.Sprintf("HID %q failed verification: %v", f.hid, verr), nil)
	}
	fmt.Printf("✅ HID %q verified — signature valid\n", f.hid)
	fmt.Printf("   agent_id:    %s\n", hid.Identity.ID)
	fmt.Printf("   fingerprint: %s\n", hid.Identity.Fingerprint())
	return nil
}

// ---------------------------------------------------------------------------
// identity export — portable representation (JSON or Nostr kind-0)
// ---------------------------------------------------------------------------

// exportFlags holds the per-command flags for `helix identity export`.
type exportFlags struct {
	hid    string
	format string
	key    string
}

func newExportCmd() *cobra.Command {
	var f exportFlags
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export an HID as JSON or a signed Nostr kind-0 event",
		Long: `Prints a portable representation of an HID. --format json (default)
prints the signed HID document itself. --format nostr converts the identity
to a NIP-01 kind-0 metadata event signed with the agent's private key
(SPEC-022 §6); that key is NOT inside the HID file, so --key must point at
the PKCS#8 PEM private key written by "helix identity create" (<output>.key)
or at an agent's ~/.helix/keys/<agent>/id_ed25519.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(f)
		},
	}
	cmd.Flags().StringVar(&f.hid, "hid", "", "path to the HID file to export")
	cmd.Flags().StringVar(&f.format, "format", "json", "export format: json or nostr")
	cmd.Flags().StringVar(&f.key, "key", "", "path to the PKCS#8 PEM private key (required for --format nostr)")
	return cmd
}

func runExport(f exportFlags) error {
	if f.hid == "" {
		return identity.NewConfigError("--hid PATH is required", nil)
	}
	format := strings.ToLower(strings.TrimSpace(f.format))
	if format == "" {
		format = "json"
	}
	switch format {
	case "json":
		data, err := os.ReadFile(f.hid)
		if err != nil {
			return identity.NewConfigError(
				fmt.Sprintf("failed to read HID file %q", f.hid), err)
		}
		fmt.Println(strings.TrimRight(string(data), "\n"))
	case "nostr":
		agent, err := identity.ImportHID(f.hid)
		if err != nil {
			return err
		}
		if f.key == "" {
			return identity.NewConfigError(
				"--key PATH is required for --format nostr (the HID file does not contain the private key)", nil)
		}
		privKey, err := loadPrivateKeyPEM(f.key)
		if err != nil {
			return err
		}
		event, err := identity.NewNostrEventFromHID(agent)
		if err != nil {
			return err
		}
		if err := event.Sign(privKey); err != nil {
			return err
		}
		data, err := event.Marshal()
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		return identity.NewConfigError(
			fmt.Sprintf("unsupported --format %q (want json or nostr)", f.format), nil)
	}
	return nil
}

// ---------------------------------------------------------------------------
// identity import
// ---------------------------------------------------------------------------

// importFlags holds the per-command flags for `helix identity import`.
type importFlags struct {
	path string
}

func newImportCmd() *cobra.Command {
	var f importFlags
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import an HID file and print its identity",
		Long: `Loads a signed HID document from disk and prints its agent_id,
fingerprint, public key, and creation time. Does not modify anything.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(f)
		},
	}
	cmd.Flags().StringVar(&f.path, "path", "", "path to the HID file to import")
	return cmd
}

func runImport(f importFlags) error {
	if f.path == "" {
		return identity.NewConfigError("--path PATH is required", nil)
	}
	agent, err := identity.ImportHID(f.path)
	if err != nil {
		return err
	}
	fmt.Printf("✅ imported HID %s\n", f.path)
	fmt.Printf("   agent_id:    %s\n", agent.ID)
	fmt.Printf("   fingerprint: %s\n", agent.Fingerprint())
	fmt.Printf("   pubkey:      %s\n", hex.EncodeToString(agent.PubKey))
	fmt.Printf("   created_at:  %s\n", agent.CreatedAt.UTC().Format(time.RFC3339))
	return nil
}

// ---------------------------------------------------------------------------
// identity list — agents registered at a forge
// ---------------------------------------------------------------------------

// listFlags holds the per-command flags for `helix identity list`.
type listFlags struct {
	forge string
}

func newListCmd() *cobra.Command {
	var f listFlags
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List OAuth2 applications (agents) registered at a Forgejo instance",
		Long: `Lists every OAuth2 application registered by the authenticated user
at the given forge — i.e. every agent that ran "helix identity register"
there. Uses FORGEJO_ADMIN_USER / FORGEJO_ADMIN_PASSWORD (or
--admin-user/--admin-password) for BasicAuth, like sync and register.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(f)
		},
	}
	cmd.Flags().StringVar(&f.forge, "forge", envOr(envForgejoURL, ""),
		"Forgejo base URL (env: "+envForgejoURL+")")
	return cmd
}

func runList(f listFlags) error {
	forge := strings.TrimRight(f.forge, "/")
	if forge == "" {
		return identity.NewConfigError("--forge URL is required (or set FORGEJO_URL)", nil)
	}
	if rootFlags.adminUser == "" {
		return identity.NewConfigError(
			"list needs Forgejo credentials: set FORGEJO_ADMIN_USER and FORGEJO_ADMIN_PASSWORD (or --admin-user/--admin-password)", nil)
	}
	registrar := identity.NewForgejoOAuthRegistrar(forge, rootFlags.adminUser, rootFlags.adminPassword)
	apps, err := registrar.ListOAuthApps(context.Background())
	if err != nil {
		return err
	}
	if len(apps) == 0 {
		fmt.Printf("NO_REGISTERED_AGENTS: no OAuth2 applications registered at %s\n", forge)
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "APP	CLIENT ID	REDIRECT URIS	CREATED")
	for _, app := range apps {
		fmt.Fprintf(w, "%s	%s	%s	%s\n",
			app.Name, app.ClientID, strings.Join(app.RedirectURIs, ","), app.Created)
	}
	w.Flush()
	return nil
}

// ---------------------------------------------------------------------------
// HID + private key helpers
// ---------------------------------------------------------------------------

// loadHID reads a HID file from disk and returns the full signed document
// (identity + signature bytes).
func loadHID(path string) (*identity.HID, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, identity.NewConfigError(
			fmt.Sprintf("failed to read HID file %q", path), err)
	}
	var hid identity.HID
	if err := json.Unmarshal(data, &hid); err != nil {
		return nil, identity.NewConfigError(
			fmt.Sprintf("failed to parse HID JSON from %q", path), err)
	}
	return &hid, nil
}

// writePrivateKeyPEM persists an Ed25519 private key as a PKCS#8 PEM block
// with mode 0600 — the same on-disk format the syncer uses for
// ~/.helix/keys/<agent>/id_ed25519.
func writePrivateKeyPEM(path string, privKey ed25519.PrivateKey) error {
	pkcs8, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return identity.NewInternalError("marshal private key to PKCS#8", err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return identity.NewInternalError(
			fmt.Sprintf("failed to write private key to %q", path), err)
	}
	return nil
}

// loadPrivateKeyPEM reads an Ed25519 private key from a PKCS#8 PEM file.
func loadPrivateKeyPEM(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, identity.NewConfigError(
			fmt.Sprintf("failed to read private key %q", path), err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, identity.NewConfigError(
			fmt.Sprintf("no PEM block found in %q", path), nil)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, identity.NewConfigError(
			fmt.Sprintf("failed to parse PKCS#8 private key in %q", path), err)
	}
	privKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, identity.NewConfigError(
			fmt.Sprintf("%q is not an Ed25519 private key", path), nil)
	}
	return privKey, nil
}

// ---------------------------------------------------------------------------
// Config builder + env helper
// ---------------------------------------------------------------------------

// buildConfig assembles a ProvisionerConfig from the flag holder, applying
// defaults. It does NOT validate — that happens in NewProvisioner so the
// error type is consistent.
func buildConfig() (identity.ProvisionerConfig, error) {
	cfg := identity.DefaultProvisionerConfig()
	cfg.ForgejoURL = rootFlags.forgejoURL
	cfg.AdminToken = rootFlags.adminToken
	cfg.AdminUser = rootFlags.adminUser
	cfg.AdminPassword = rootFlags.adminPassword
	cfg.KnownFriendsPath = rootFlags.knownFriends
	cfg.SSHKeyDir = rootFlags.sshKeyDir
	cfg.StatePath = rootFlags.statePath
	cfg.DryRun = rootFlags.dryRun
	cfg.Verbose = rootFlags.verbose
	return cfg, nil
}

// envOr returns the env var's value if set and non-empty, else fallback.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// ---------------------------------------------------------------------------
// Output rendering
// ---------------------------------------------------------------------------

// renderDryRun prints the §19 dry-run preview: would-be API calls per agent
// plus a summary table. The actual network stubs returned dummy objects;
// this output is what an operator reviews before flipping --dry-run off.
func renderDryRun(kf *identity.KnownFriends) {
	for _, a := range kf.ActiveAgents() {
		tempPW := "<redacted>"
		req := identity.NewCreateUserRequest(a, tempPW)
		fmt.Printf("[DRY RUN] POST /api/v1/admin/users %s\n", mustJSON(req))
		fmt.Printf("[DRY RUN] POST /api/v1/user/keys {\"key\":\"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGx... helix-identity\",\"title\":%q}\n",
			a.KeyTitle())
		fmt.Printf("[DRY RUN] POST /api/v1/users/%s/tokens %s\n",
			a.Name, mustJSON(identity.NewCreateTokenRequest(a)))
	}
	for _, a := range kf.OffboardedAgents() {
		fmt.Printf("[DRY RUN] DELETE /api/v1/users/%s/tokens/{id}  (revoke PAT)\n", a.Name)
	}
	sep := strings.Repeat("─", 66)
	fmt.Println(sep)
	fmt.Printf("%-10s %-12s %s\n", "AGENT", "STATUS", "ACTION")
	for _, a := range kf.ActiveAgents() {
		fmt.Printf("%-10s %-12s %s\n", a.Name, a.Status, "would create")
	}
	for _, a := range kf.OffboardedAgents() {
		fmt.Printf("%-10s %-12s %s\n", a.Name, a.Status, "would deprovision")
	}
	fmt.Println(sep)
	ops := len(kf.ActiveAgents()) + len(kf.OffboardedAgents())
	fmt.Printf("DRY RUN COMPLETE — %d operations simulated, 0 executed\n", ops)
}

// renderResultsTable prints the post-sync result table (one row per agent).
func renderResultsTable(results []identity.ProvisioningResult) {
	if len(results) == 0 {
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tSTATUS\tACTION\tSSH KEY\tPAT\tDURATION")
	for _, r := range results {
		marker := "✅"
		if !r.Succeeded() {
			marker = "❌"
		}
		ssh := r.SSHFingerprint
		if ssh == "" {
			ssh = "—"
		} else if len(ssh) > 24 {
			ssh = ssh[:24] + "..."
		}
		pat := r.PATLastEight
		if pat == "" {
			pat = "—"
		}
		fmt.Fprintf(w, "%s %s\t%s\t%s\t%s\t%s\t%s\n",
			marker, r.AgentName, r.Status, r.Action, ssh, pat,
			r.Duration.Round(time.Millisecond))
	}
	w.Flush()
}

// renderSingleResult prints one result row (for provision/deprovision/keygen).
func renderSingleResult(r identity.ProvisioningResult) {
	marker := "✅"
	if !r.Succeeded() {
		marker = "❌"
	}
	fmt.Printf("%s agent=%s action=%s duration=%s\n",
		marker, r.AgentName, r.Action, r.Duration.Round(time.Millisecond))
	if r.SSHFingerprint != "" {
		fmt.Printf("   ssh fingerprint: %s\n", r.SSHFingerprint)
	}
	if r.PATLastEight != "" {
		fmt.Printf("   pat:             %s\n", r.PATLastEight)
	}
	if r.Error != "" {
		fmt.Printf("   error:           %s\n", r.Error)
	}
}

// renderStateTable prints the ~/.helix/state.json contents as a table.
func renderStateTable(state *identity.StateFile) {
	if state == nil || len(state.Agents) == 0 {
		fmt.Println("(no agents provisioned — run `helix-identity sync`)")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tFORGEJO ID\tSSH KEY\tPAT\tLAST SYNC")
	// Sort by name for stable output.
	names := make([]string, 0, len(state.Agents))
	for n := range state.Agents {
		names = append(names, n)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j-1] > names[j]; j-- {
			names[j-1], names[j] = names[j], names[j-1]
		}
	}
	for _, name := range names {
		st := state.Agents[name]
		ssh := st.SSHFingerprint
		if len(ssh) > 24 {
			ssh = ssh[:24] + "..."
		}
		pat := st.PATLastEight
		if pat == "" {
			pat = "—"
		}
		last := st.LastProvisioned.Format(time.RFC3339)
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n",
			name, st.ForgejoAccountID, ssh, pat, last)
	}
	w.Flush()
}

// mustJSON marshals v to a compact JSON string. Panics on failure — used only
// for static render of request bodies in dry-run output where the input
// shape is known to be marshalable (it's all our own structs with json tags).
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Should never happen for our own request structs; if it does, surface
		// a placeholder rather than crash the dry-run.
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	return string(b)
}
