// Command helix-sandbox is the CLI entry point for Bubblewrap-based agent
// isolation. Every Helix platform action — agent spawn, code execution, tool
// invocation — flows through this binary.
//
// Usage:
//
//	helix-sandbox run [flags] -- <command...>
//
// Everything after the "--" separator is treated as the command to execute
// inside the sandbox.
//
// Examples:
//
//	helix-sandbox run -- echo hello
//	helix-sandbox run --isolation full --time-limit 30 -- python3 /workspace/script.py
//	helix-sandbox run --dry-run -- /bin/bash -c 'env | sort'
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/sandbox"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run parses arguments, builds the config, and executes the sandbox. It returns
// the process exit code, never panicking.
func run(args []string) int {
	// --- Subcommand dispatch ---

	if len(args) == 0 {
		printUsage(os.Stderr)
		return sandbox.ExitConfigError
	}

	switch args[0] {
	case "run":
		return runCommand(args[1:])
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return sandbox.ExitOK
	case "version", "-v", "--version":
		printVersion(os.Stdout)
		return sandbox.ExitOK
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n\n", args[0])
		printUsage(os.Stderr)
		return sandbox.ExitConfigError
	}
}

// ---------------------------------------------------------------------------
// run subcommand
// ---------------------------------------------------------------------------

func runCommand(args []string) int {
	fs := flag.NewFlagSet("helix-sandbox run", flag.ExitOnError)

	sessionID := fs.String("session-id", "", "Session identifier (default: random UUID)")
	isolation := fs.String("isolation", string(sandbox.IsolationWorkspace),
		"Isolation level: none|workspace|full")
	workdir := fs.String("workdir", "/workspace", "Working directory inside sandbox")
	timeLimit := fs.Int("time-limit", 600, "Max execution time in seconds (0 = unlimited)")
	memoryLimit := fs.Int("memory-limit", 2048, "Max memory in MB (0 = unlimited)")
	network := fs.String("network", string(sandbox.NetworkNone), "Network mode: none|restricted")
	dryRun := fs.Bool("dry-run", false, "Print bwrap command without executing")
	verbose := fs.Bool("verbose", false, "Log all mount operations")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: helix-sandbox run [flags] -- <command...>\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEverything after '--' is the command to run inside the sandbox.\n")
	}

	if err := fs.Parse(args); err != nil {
		return sandbox.ExitConfigError
	}

	// Extract the command after "--".
	command := fs.Args()
	if len(command) == 0 {
		fmt.Fprintln(os.Stderr, "error: no command specified after '--'")
		fs.Usage()
		return sandbox.ExitConfigError
	}

	// --- Build config ---

	cfg := sandbox.DefaultConfig()
	if *sessionID != "" {
		cfg.SessionID = *sessionID
	} else {
		cfg.SessionID = generateSessionID()
	}
	cfg.Isolation = sandbox.IsolationLevel(*isolation)
	cfg.Workdir = *workdir
	cfg.TimeLimit = *timeLimit
	cfg.MemoryLimit = *memoryLimit
	cfg.Network = sandbox.NetworkMode(*network)
	cfg.DryRun = *dryRun
	cfg.Verbose = *verbose
	cfg.Command = command

	// --- Validate ---

	if err := cfg.Validate(); err != nil {
		printError(err)
		return sandbox.ExitConfigError
	}

	// --- Execute ---

	executor, err := sandbox.NewExecutor(cfg)
	if err != nil {
		printError(err)
		return sandbox.ExitConfigError
	}

	if err := executor.RunWithTimeout(); err != nil {
		return handleError(err)
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[sandbox] session %s completed\n", cfg.SessionID)
	}

	return sandbox.ExitOK
}

// ---------------------------------------------------------------------------
// Error handling
// ---------------------------------------------------------------------------

// handleError maps a sandbox error to the appropriate exit code and prints a
// human-readable message to stderr.
func handleError(err error) int {
	switch {
	case errors.Is(err, sandbox.ErrBwrapNotFound):
		fmt.Fprintf(os.Stderr, "error: bubblewrap not found — %v\n", err)
		return sandbox.ExitBwrapNotFound

	case errors.Is(err, sandbox.ErrConfigInvalid):
		fmt.Fprintf(os.Stderr, "error: invalid configuration — %v\n", err)
		return sandbox.ExitConfigError

	case errors.Is(err, sandbox.ErrSetupFailed):
		fmt.Fprintf(os.Stderr, "error: setup failed — %v\n", err)
		return sandbox.ExitSetupError

	case errors.Is(err, sandbox.ErrTimeoutExceeded):
		fmt.Fprintf(os.Stderr, "error: time limit exceeded — %v\n", err)
		return sandbox.ExitTimeout

	case errors.Is(err, sandbox.ErrNotImplemented):
		// Dry-run succeeds even though real exec is stubbed.
		if isDryRunOnly(err) {
			return sandbox.ExitOK
		}
		fmt.Fprintf(os.Stderr, "error: not implemented — %v\n", err)
		return sandbox.ExitInternalError

	case errors.Is(err, sandbox.ErrExecutionFailed):
		fmt.Fprintf(os.Stderr, "error: execution failed — %v\n", err)
		return sandbox.ExitExecutionError

	default:
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return sandbox.ExitInternalError
	}
}

// isDryRunOnly checks if the error is purely from a dry-run path (which is
// expected to succeed).
func isDryRunOnly(err error) bool {
	return strings.Contains(err.Error(), "bwrap execution") &&
		errors.Is(err, sandbox.ErrNotImplemented)
}

// printError writes an error to stderr with a standard prefix.
func printError(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// generateSessionID produces a random 16-byte hex session ID (32 chars).
func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to a timestamp-based ID if crypto/rand fails.
		return fmt.Sprintf("session-%d", os.Getpid())
	}
	return hex.EncodeToString(b)
}

// printUsage writes the CLI usage to w.
func printUsage(w io.Writer) {
	fmt.Fprintf(w, `helix-sandbox — Bubblewrap-based agent isolation

Usage:
  helix-sandbox run [flags] -- <command...>
  helix-sandbox help
  helix-sandbox version

Subcommands:
  run       Execute a command inside a sandboxed environment
  help      Show this help message
  version   Print version information

Isolation levels:
  none      No sandboxing (debug only)
  workspace Isolated workspace, no network, private PID namespace
  full      Maximum isolation: no GPU, minimal environment

Examples:
  helix-sandbox run -- echo hello
  helix-sandbox run --isolation full --time-limit 30 -- python3 script.py
  helix-sandbox run --dry-run -- /bin/bash -c 'env | sort'
`)
}

// printVersion writes version information to w.
func printVersion(w io.Writer) {
	fmt.Fprintf(w, "helix-sandbox 0.1.0 (spec: bubblewrap %s)\n", sandbox.BwrapVersion)
}
