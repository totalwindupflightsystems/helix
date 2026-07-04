// Command helix is the unified CLI entry point for the Helix platform.
//
// It wraps all sub-CLIs (helix-identity, helix-estimate, helix-negotiate,
// helix-prompt, helix-marketplace, sandbox) behind a single binary with
// consistent global flags and version reporting.
//
// Subcommands:
//
//	helix identity    → delegates to helix-identity
//	helix estimate    → delegates to helix-estimate
//	helix negotiate   → delegates to helix-negotiate
//	helix prompt      → delegates to helix-prompt
//	helix marketplace → delegates to helix-marketplace
//	helix sandbox     → delegates to sandbox
//	helix version     → prints the current version
//	helix status      → checks all component health
//
// Global flags:
//
//	--verbose   enable verbose output
//	--config    path to config file (default: ~/.helix/config.yaml)
//	--dry-run   preview commands without executing
package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// Version is the current Helix version. Overridden at build time via
	// -ldflags "-X main.Version=$(git describe --tags)".
	Version = "0.1.0-dev"
	// AppName is the canonical CLI name.
	AppName = "helix"
)

// ---------------------------------------------------------------------------
// Global flags
// ---------------------------------------------------------------------------

var (
	verbose bool
	cfgFile string
	dryRun  bool
)

// ---------------------------------------------------------------------------
// Subcommand registry
// ---------------------------------------------------------------------------

// subcommand maps a subcommand name to the binary it delegates to.
var subcommands = map[string]string{
	"identity":    "helix-identity",
	"estimate":    "helix-estimate",
	"negotiate":   "helix-negotiate",
	"prompt":      "helix-prompt",
	"marketplace": "helix-marketplace",
	"sandbox":     "sandbox",
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	_ = cfgFile // reserved for --config flag in Phase 2

	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// rootCmd builds the root cobra-free command dispatcher. We use a simple
// flag-based approach to avoid pulling in cobra as a dependency for this
// thin wrapper.
func rootCmd() *dispatcher {
	d := &dispatcher{
		usage: func() {
			printUsage()
		},
	}
	return d
}

// ---------------------------------------------------------------------------
// Dispatcher
// ---------------------------------------------------------------------------

type dispatcher struct {
	usage func()
}

// Execute parses os.Args and dispatches to the appropriate subcommand.
func (d *dispatcher) Execute() error {
	return d.dispatch(os.Args[1:])
}

// dispatch parses the given args and dispatches to the appropriate subcommand.
// Exported for testing — callers should use Execute() in production.
func (d *dispatcher) dispatch(args []string) error {

	// Parse global flags
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--verbose":
			verbose = true
		case "--config":
			if i+1 < len(args) {
				cfgFile = args[i+1]
				i++
			} else {
				return fmt.Errorf("--config requires a value")
			}
		case "--dry-run":
			dryRun = true
		case "--help", "-h":
			d.usage()
			return nil
		case "--version":
			printVersion()
			return nil
		default:
			filtered = append(filtered, args[i])
		}
	}

	if len(filtered) == 0 {
		d.usage()
		return nil
	}

	name := filtered[0]
	rest := filtered[1:]

	// Handle built-in commands
	switch name {
	case "version":
		printVersion()
		return nil
	case "status":
		return runStatus()
	case "doctor":
		return runDoctorWithConfig(parseDoctorFlags(rest))
	case "dispatch":
		// The global --dry-run flag (parsed in dispatch() above) is
		// honoured by every subcommand. Thread it into the dispatch
		// handler explicitly so dispatch's --dry-run flag isn't shadowed
		// by the global parser.
		return runDispatchWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
	}

	// Delegate to subcommand binary
	binary, ok := subcommands[name]
	if !ok {
		return fmt.Errorf("unknown subcommand %q\n\nAvailable subcommands: %s",
			name, strings.Join(sortedKeys(subcommands), ", "))
	}

	return execSubcommand(binary, rest)
}

// ---------------------------------------------------------------------------
// Built-in commands
// ---------------------------------------------------------------------------

func printVersion() {
	fmt.Printf("%s %s (%s/%s)\n", AppName, Version, runtime.GOOS, runtime.GOARCH)
}

func runStatus() error {
	fmt.Printf("Helix Status (version %s)\n\n", Version)
	fmt.Println("Checking components...")

	components := []struct {
		name string
		url  string
	}{
		{"Forgejo", "http://localhost:3000/api/v1/version"},
		{"Chimera", "http://localhost:8765/v1/health"},
	}

	allOK := true
	for _, c := range components {
		ok := checkEndpoint(c.name, c.url)
		if !ok {
			allOK = false
		}
	}

	fmt.Println()
	if allOK {
		fmt.Println("[OK] All components healthy")
	} else {
		fmt.Println("[WARN] Some components are unreachable (see above)")
	}

	// Check subcommand binaries
	fmt.Println()
	fmt.Println("Subcommand binaries:")
	for name, binary := range subcommands {
		_, err := lookPath(binary)
		if err != nil {
			fmt.Printf("  [MISSING] %s (%s) — not in PATH\n", name, binary)
		} else {
			fmt.Printf("  [OK]      %s (%s)\n", name, binary)
		}
	}

	if !allOK {
		return fmt.Errorf("some components are unreachable")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func execSubcommand(binary string, args []string) error {
	// Find binary in PATH
	binPath, err := lookPath(binary)
	if err != nil {
		return fmt.Errorf("subcommand %q not found (%s).\n\nInstall it with:\n  cd cmd/%s && go build -o %s\n  sudo mv %s /usr/local/bin/\n",
			binary, binary, binary, binary, binary)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] executing: %s %s\n", binPath, strings.Join(args, " "))
	}
	if dryRun {
		fmt.Printf("[DRY RUN] %s %s\n", binPath, strings.Join(args, " "))
		return nil
	}

	cmd := exec.Command(binPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func checkEndpoint(name, url string) bool {
	// Simple TCP connect check
	addr := urlToAddr(url)
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		fmt.Printf("  [DOWN]  %s — cannot reach %s (%v)\n", name, url, err)
		return false
	}
	conn.Close()
	fmt.Printf("  [OK]    %s — reachable at %s\n", name, url)
	return true
}

// urlToAddr extracts the host:port from a URL for TCP dialing.
func urlToAddr(rawURL string) string {
	// Minimal URL parsing without net/url to avoid import cycles
	rawURL = strings.TrimPrefix(rawURL, "http://")
	rawURL = strings.TrimPrefix(rawURL, "https://")
	// Strip path
	if idx := strings.Index(rawURL, "/"); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	// Add default port if missing
	if !strings.Contains(rawURL, ":") {
		rawURL = rawURL + ":80"
	}
	return rawURL
}

func lookPath(name string) (string, error) {
	// Prefer project-local binaries
	localPaths := []string{
		filepath.Join(".", name),
		filepath.Join("cmd", name, name),
	}
	for _, p := range localPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	// Fall back to PATH
	return exec.LookPath(name)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple sort
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func printUsage() {
	fmt.Printf(`%s — Helix Agent-First Software Development Platform

Usage: %s [global-flags] <subcommand> [subcommand-args]

Subcommands:
  identity    Provision agent accounts in Forgejo
  estimate    Estimate task cost before execution
  negotiate   Negotiate task contracts between agents
  prompt      Manage and attest prompt files
  marketplace Search and discover agents
  sandbox     Run commands in a sandboxed environment
  version     Print version information
  status      Check all component health

Global Flags:
  --verbose   Enable verbose output
  --config    Path to config file (default: ~/.helix/config.yaml)
  --dry-run   Preview commands without executing
  --help      Show this help message
  --version   Show version

Examples:
  %s identity sync
  %s estimate --task "Write a Go HTTP server"
  %s marketplace search --capability go
  %s status
`, AppName, AppName, AppName, AppName, AppName, AppName)
}
