// Command helix — secrets_crud.go
//
// Implements the CRUD + rotate + init subcommands for `helix secrets`
// per specs/secret-management.md §4.1. These subcommands back onto
// pkg/security/store.SOPSSecretStore when `secrets.provider: sops` and
// fall through to os.Getenv when `secrets.provider: env`.
//
// Subcommands:
//
//	helix secrets set <key> <value>       Create/update a secret
//	helix secrets get <key>               Retrieve and print a secret
//	helix secrets delete <key>            Remove a secret (idempotent)
//	helix secrets list                    List all secret keys
//	helix secrets rotate <new-key-path>   Re-encrypt with new identity
//	helix secrets init                    Create ~/.helix/secrets.enc.yaml + age key
//
// This file deliberately does NOT touch the scan/list-rules/help flow
// defined in secrets.go. It plugs into runSecrets via a new dispatch
// case for each CRUD subcommand; parseSecretFlags is unchanged.
//
// Exit codes:
//
//	0 — success
//	1 — operational error (store missing/corrupt, key not found for get)
//	2 — invocation error (bad args, bad flags)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/config"
	"github.com/totalwindupflightsystems/helix/pkg/security/store"
)

// crudSubcommands is the set of subcommands handled here. Kept as a
// package-level set so parseSecretFlags / runSecrets can recognise
// them without scattering string literals through the dispatch.
var crudSubcommands = map[string]bool{
	"set":    true,
	"get":    true,
	"delete": true,
	"list":   true,
	"rotate": true,
	"init":   true,
}

// crudFlags holds the parsed flags + positional args for a CRUD
// subcommand. The fields used depend on the subcommand:
//
//	set:    key, value
//	get:    key
//	delete: key
//	list:   (none)
//	rotate: newKeyPath
//	init:   (none)
type crudFlags struct {
	subcommand  string
	key         string // set/get/delete
	value       string // set
	newKeyPath  string // rotate
	storePath   string // --store override
	keyPath     string // --key-path override
	provider    string // --provider override (env|sops)
	showHelp    bool
}

// env-var overrides for the store/key paths. These are intentionally
// separate from the HELIX_SECRETS_* env vars in secrets.go (which are
// scan-specific) so operators can configure the CRUD store without
// affecting scan defaults.
const (
	envSecretsStorePath = "HELIX_SECRETS_STORE_PATH"
	envSecretsKeyPath   = "HELIX_SECRETS_KEY_PATH"
	envSecretsProvider  = "HELIX_SECRETS_PROVIDER"
)

// parseCrudFlags parses the args for a CRUD subcommand. The first
// positional arg is the subcommand; remaining positionals and flags
// are validated per-subcommand. Returns an invocation error (caller
// maps to exit code 2) on bad input.
func parseCrudFlags(args []string, stdout, stderr io.Writer) (crudFlags, error) {
	if len(args) == 0 {
		return crudFlags{}, fmt.Errorf("missing secrets subcommand")
	}
	sub := args[0]
	rest := args[1:]

	f := crudFlags{subcommand: sub}

	fs := flag.NewFlagSet("helix-secrets-"+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.storePath, "store", "",
		"Encrypted store file path (env HELIX_SECRETS_STORE_PATH, default from config)")
	fs.StringVar(&f.keyPath, "key-path", "",
		"Age identity file path (env HELIX_SECRETS_KEY_PATH, default from config)")
	fs.StringVar(&f.provider, "provider", "",
		"Secret provider: env | sops (env HELIX_SECRETS_PROVIDER, default from config)")

	// Apply env-var defaults before parsing so explicit flags still win.
	if v := os.Getenv(envSecretsStorePath); v != "" {
		f.storePath = v
	}
	if v := os.Getenv(envSecretsKeyPath); v != "" {
		f.keyPath = v
	}
	if v := os.Getenv(envSecretsProvider); v != "" {
		f.provider = v
	}

	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return crudFlags{subcommand: sub, showHelp: true}, nil
		}
		return crudFlags{}, err
	}

	pos := fs.Args()
	switch sub {
	case "set":
		if len(pos) < 2 {
			return crudFlags{}, fmt.Errorf("helix secrets set: requires <key> <value>")
		}
		if len(pos) > 2 {
			return crudFlags{}, fmt.Errorf("helix secrets set: unexpected extra arguments: %v", pos[2:])
		}
		f.key, f.value = pos[0], pos[1]
	case "get":
		if len(pos) < 1 {
			return crudFlags{}, fmt.Errorf("helix secrets get: requires <key>")
		}
		if len(pos) > 1 {
			return crudFlags{}, fmt.Errorf("helix secrets get: unexpected extra arguments: %v", pos[1:])
		}
		f.key = pos[0]
	case "delete":
		if len(pos) < 1 {
			return crudFlags{}, fmt.Errorf("helix secrets delete: requires <key>")
		}
		if len(pos) > 1 {
			return crudFlags{}, fmt.Errorf("helix secrets delete: unexpected extra arguments: %v", pos[1:])
		}
		f.key = pos[0]
	case "list":
		if len(pos) > 0 {
			return crudFlags{}, fmt.Errorf("helix secrets list: unexpected arguments: %v", pos)
		}
	case "rotate":
		if len(pos) < 1 {
			return crudFlags{}, fmt.Errorf("helix secrets rotate: requires <new-key-path>")
		}
		if len(pos) > 1 {
			return crudFlags{}, fmt.Errorf("helix secrets rotate: unexpected extra arguments: %v", pos[1:])
		}
		f.newKeyPath = pos[0]
	case "init":
		if len(pos) > 0 {
			return crudFlags{}, fmt.Errorf("helix secrets init: unexpected arguments: %v", pos)
		}
	default:
		return crudFlags{}, fmt.Errorf("unknown secrets subcommand %q", sub)
	}

	// --provider validation is deferred to the run layer so it can
	// fall back to the config default when unset.
	return f, nil
}

// resolveSecretsConfig picks the effective store/key/provider values
// using flag > env > config > defaults precedence. The cfg pointer is
// optional — when nil, defaults matching an unset config section are
// applied (provider=env, no paths).
func resolveSecretsConfig(f crudFlags, cfg *config.Config) (provider, storePath, keyPath string) {
	// Defaults mirror pkg/config.Defaults() so the CLI works the same
	// without a loaded config file.
	provider = "env"
	storePath = "~/.helix/secrets.enc.yaml"
	keyPath = "~/.helix/keys/age.txt"
	if cfg != nil {
		if cfg.Secrets.Provider != "" {
			provider = cfg.Secrets.Provider
		}
		if cfg.Secrets.StorePath != "" {
			storePath = cfg.Secrets.StorePath
		}
		if cfg.Secrets.SOPSKeyPath != "" {
			keyPath = cfg.Secrets.SOPSKeyPath
		}
	}
	if f.provider != "" {
		provider = f.provider
	}
	if f.storePath != "" {
		storePath = f.storePath
	}
	if f.keyPath != "" {
		keyPath = f.keyPath
	}
	return provider, storePath, keyPath
}

// runCrud is the entry point dispatched from runSecrets for every
// CRUD subcommand. It returns 0 on success, 1 on operational error,
// 2 on invocation error — matching the convention used by the scan
// subcommand.
func runCrud(f crudFlags, stdout, stderr io.Writer) int {
	if f.showHelp {
		printCrudUsage(stdout)
		return 0
	}

	// Effective config. We don't load ~/.helix/config.yaml here yet —
	// the spec's 5-tier loader lives in pkg/config but the cmd/helix
	// dispatcher doesn't currently thread a loaded Config into the
	// secrets handler. Operators can override paths via --store /
	// --key-path / env vars; defaults match the spec's documented
	// paths.
	provider, storePath, keyPath := resolveSecretsConfig(f, nil)

	switch provider {
	case "env":
		return runCrudEnv(f, stdout, stderr)
	case "sops":
		return runCrudSOPS(f, storePath, keyPath, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "helix secrets: unsupported provider %q (expected: env | sops)\n", provider)
		return 2
	}
}

// =============================================================================
// env provider — passthrough to os.Getenv
// =============================================================================

// runCrudEnv handles the env-provider case. Secret lookups fall
// through to the process environment. set/delete/rotate/init are
// no-ops or surface a clear error so operators don't think they've
// mutated the environment.
func runCrudEnv(f crudFlags, stdout, stderr io.Writer) int {
	switch f.subcommand {
	case "get":
		v, ok := os.LookupEnv(f.key)
		if !ok {
			fmt.Fprintf(stderr, "helix secrets get: %q not set in environment\n", f.key)
			return 1
		}
		fmt.Fprintln(stdout, v)
		return 0
	case "set":
		// We cannot mutate the parent process's environment usefully —
		// the change would die with this invocation. Surface it as an
		// operational error so operators know to use the SOPS store
		// for persistent secrets.
		fmt.Fprintln(stderr, "helix secrets set: provider=env cannot persist values;",
			"set the variable in your shell or .env file, or switch to provider=sops")
		return 1
	case "delete":
		fmt.Fprintln(stderr, "helix secrets delete: provider=env cannot unset values;",
			"unset the variable in your shell or .env file")
		return 1
	case "list":
		// Env iteration is non-deterministic by default; sort for stable output.
		keys := make([]string, 0)
		for _, kv := range os.Environ() {
			if i := strings.IndexByte(kv, '='); i >= 0 {
				keys = append(keys, kv[:i])
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintln(stdout, k)
		}
		return 0
	case "rotate":
		fmt.Fprintln(stderr, "helix secrets rotate: provider=env has no key to rotate")
		return 1
	case "init":
		fmt.Fprintln(stderr, "helix secrets init: provider=env has nothing to initialise;",
			"switch to provider=sops to create an encrypted store")
		return 1
	}
	return 2
}

// =============================================================================
// sops provider — pkg/security/store backed CRUD
// =============================================================================

// runCrudSOPS handles the sops-provider case. It opens (or initialises)
// the SOPSSecretStore and routes the subcommand to the matching
// SecretStore method. Auto-init (spec §4.3) kicks in when the age key
// file is absent: a new key is generated via the external `age-keygen`
// binary and an empty encrypted store is written.
func runCrudSOPS(f crudFlags, storePath, keyPath string, stdout, stderr io.Writer) int {
	ctx := context.Background()

	// `init` is special: it explicitly creates the key + store rather
	// than auto-initialising in passing.
	if f.subcommand == "init" {
		return runCrudInitSOPS(storePath, keyPath, stdout, stderr)
	}

	// Ensure the age key exists — auto-init per spec §4.3. We do NOT
	// auto-create the encrypted store file here; NewSOPSStore handles
	// a missing file as an empty store.
	if err := ensureAgeKey(keyPath, stdout); err != nil {
		fmt.Fprintln(stderr, "helix secrets:", err)
		return 1
	}

	// SOPS keyservice.NewLocalClient reads the identity from
	// SOPS_AGE_KEY_FILE / SOPS_AGE_KEY. Set the file-based env var so
	// the in-process key service uses our key for decrypt.
	os.Setenv("SOPS_AGE_KEY_FILE", expandCLIHome(keyPath))

	s, err := store.NewSOPSStore(storePath, keyPath)
	if err != nil {
		fmt.Fprintln(stderr, "helix secrets: open store:", err)
		return 1
	}

	switch f.subcommand {
	case "get":
		v, err := s.Get(ctx, f.key)
		if err != nil {
			if errors.Is(err, store.ErrSecretNotFound) {
				fmt.Fprintf(stderr, "helix secrets get: %q not found\n", f.key)
				return 1
			}
			fmt.Fprintln(stderr, "helix secrets get:", err)
			return 1
		}
		fmt.Fprintln(stdout, v)
		return 0
	case "set":
		if err := s.Set(ctx, f.key, f.value); err != nil {
			fmt.Fprintln(stderr, "helix secrets set:", err)
			return 1
		}
		fmt.Fprintf(stdout, "set %s\n", f.key)
		return 0
	case "delete":
		if err := s.Delete(ctx, f.key); err != nil {
			fmt.Fprintln(stderr, "helix secrets delete:", err)
			return 1
		}
		fmt.Fprintf(stdout, "deleted %s\n", f.key)
		return 0
	case "list":
		keys, err := s.List(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "helix secrets list:", err)
			return 1
		}
		for _, k := range keys {
			fmt.Fprintln(stdout, k)
		}
		return 0
	case "rotate":
		if err := s.Rotate(ctx, f.newKeyPath); err != nil {
			fmt.Fprintln(stderr, "helix secrets rotate:", err)
			return 1
		}
		fmt.Fprintf(stdout, "rotated to %s\n", f.newKeyPath)
		return 0
	}
	return 2
}

// runCrudInitSOPS implements `helix secrets init` for provider=sops.
// It generates a fresh age key (unless one already exists), creates
// an empty encrypted store (unless one already exists), and prints
// the public key for backup.
func runCrudInitSOPS(storePath, keyPath string, stdout, stderr io.Writer) int {
	storePath = expandCLIHome(storePath)
	keyPath = expandCLIHome(keyPath)

	keyExisted := fileExists(keyPath)
	if !keyExisted {
		if err := generateAgeKey(keyPath); err != nil {
			fmt.Fprintln(stderr, "helix secrets init: generate age key:", err)
			return 1
		}
		fmt.Fprintf(stdout, "generated age key at %s\n", keyPath)
	}

	// Surface the public key regardless — operators need it for
	// backups and for enrolling additional recipients.
	pub, _ := readAgePublicKey(keyPath)
	if pub != "" {
		fmt.Fprintf(stdout, "public key: %s\n", pub)
	}

	storeExisted := fileExists(storePath)
	if !storeExisted {
		// NewSOPSStore + Set(empty) writes an encrypted file with no
		// secrets — the canonical "initialised empty store" shape.
		os.Setenv("SOPS_AGE_KEY_FILE", keyPath)
		s, err := store.NewSOPSStore(storePath, keyPath)
		if err != nil {
			fmt.Fprintln(stderr, "helix secrets init: open store:", err)
			return 1
		}
		// Writing an empty marker secret then deleting it would leave
		// behind a real file with SOPS metadata and no leaves. We use
		// the same trick the test helper uses: set + delete to force
		// the initial file write, then the file is the empty store.
		if err := s.Set(context.Background(), "__helix_init_marker__", ""); err != nil {
			fmt.Fprintln(stderr, "helix secrets init: write store:", err)
			return 1
		}
		_ = s.Delete(context.Background(), "__helix_init_marker__")
		fmt.Fprintf(stdout, "created encrypted store at %s\n", storePath)
	}

	if keyExisted && storeExisted {
		fmt.Fprintln(stdout, "already initialised")
	}
	return 0
}

// =============================================================================
// Helpers
// =============================================================================

// ensureAgeKey makes sure an age identity file exists at keyPath. If
// it doesn't, generate one via age-keygen (spec §4.3). If age-keygen
// isn't on PATH, return a clear error so the operator knows to
// install age or pre-create the key.
func ensureAgeKey(keyPath string, stdout io.Writer) error {
	keyPath = expandCLIHome(keyPath)
	if fileExists(keyPath) {
		return nil
	}
	if err := generateAgeKey(keyPath); err != nil {
		return fmt.Errorf("auto-init age key: %w", err)
	}
	fmt.Fprintf(stdout, "generated age key at %s\n", keyPath)
	return nil
}

// generateAgeKey invokes the external `age-keygen` binary to produce
// a new identity at keyPath with file mode 0600. The key file's parent
// directory is created with mode 0700 to match the spec's key-dir
// permissions (§9.1).
func generateAgeKey(keyPath string) error {
	keyPath = expandCLIHome(keyPath)
	dir := filepath.Dir(keyPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	// age-keygen writes to stdout; redirect into the target file.
	// We use a temp file + rename to keep the write atomic.
	tmp := keyPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open tmp %s: %w", tmp, err)
	}

	cmd := exec.Command("age-keygen")
	cmd.Stdout = f
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		// Distinguish "binary not found" from a real age-keygen failure.
		if errors.Is(err, exec.ErrNotFound) || isNotFound(cmd.Err) {
			return fmt.Errorf("age-keygen not found on PATH: install age (https://age-encryption.org) and retry: %w", err)
		}
		return fmt.Errorf("age-keygen: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, keyPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, keyPath, err)
	}
	// Belt-and-braces: enforce 0600 even if age-keygen wrote group-readable.
	_ = os.Chmod(keyPath, 0o600)
	return nil
}

// isNotFound reports whether err is the exec.ErrNotFound sentinel or
// a "command not found" error from the platform.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	// Some platforms return *exec.Error whose Err field is ErrNotFound.
	var e *exec.Error
	if errors.As(err, &e) {
		return errors.Is(e.Err, exec.ErrNotFound)
	}
	return false
}

// readAgePublicKey extracts the `# public key:` recipient from an
// age identity file. Returns ("", nil) if the file is missing the
// comment.
func readAgePublicKey(keyPath string) (string, error) {
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# public key:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# public key:")), nil
		}
	}
	return "", nil
}

// fileExists is a tiny existence probe used by init / auto-init. We
// don't care about the file's contents — only whether some file is
// there to decide whether to (re)generate.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// expandCLIHome mirrors pkg/security/store.expandHome but is local to
// the cmd/helix package so we don't reach into the store package's
// internal helper. Expands a leading `~/` to $HOME.
func expandCLIHome(p string) string {
	if p == "" {
		return p
	}
	if !strings.HasPrefix(p, "~") {
		return p
	}
	if len(p) == 1 || p[1] == '/' {
		home := os.Getenv("HOME")
		if home == "" {
			return p
		}
		return filepath.Join(home, p[1:])
	}
	return p
}

// printCrudUsage writes the help text for the CRUD subcommands to w.
// Kept separate from printSecretsUsage in secrets.go so the scan
// surface stays untouched; runSecrets stitches them together.
func printCrudUsage(w io.Writer) {
	fmt.Fprint(w, `helix secrets — CRUD operations on the Helix SecretStore

Usage:
  helix secrets set <key> <value>        Create or update a secret
  helix secrets get <key>                Retrieve and print a secret value
  helix secrets delete <key>             Remove a secret (idempotent)
  helix secrets list                     List all secret keys (sorted)
  helix secrets rotate <new-key-path>    Re-encrypt the store with a new age key
  helix secrets init                     Create ~/.helix/secrets.enc.yaml + age key

Provider selection (in precedence order):
  --provider env|sops                    flag
  HELIX_SECRETS_PROVIDER                 environment
  secrets.provider in ~/.helix/config.yaml  config file
  default: env (passthrough to os.Getenv)

When provider=sops, the store/key paths default to:
  ~/.helix/secrets.enc.yaml              encrypted store
  ~/.helix/keys/age.txt                  age identity (auto-generated if missing)

Flags:
  --store PATH        Override the encrypted store file path
  --key-path PATH     Override the age identity file path
  --provider NAME     Override the secret provider (env|sops)

Exit codes:
  0 — success
  1 — operational error (store missing/corrupt, key not found)
  2 — invocation error (bad args, unknown subcommand)
`)
	fmt.Fprintln(w)
}
