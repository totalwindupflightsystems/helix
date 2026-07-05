// Command helix — webhook.go
//
// `helix webhook serve` starts the Forgejo webhook ingestion server
// (see pkg/webhook). It verifies HMAC-SHA256 signatures against a
// shared secret loaded from a file, then prints each parsed
// PullRequestEvent as one JSON object per line on stdout.
//
// This is the ingestion half of the Forgejo → Chimera/Axiom wiring
// (specs/cross-component-wiring.md §2.1). The action half — firing
// review / merge-gate based on parsed events — lives in a separate task.
//
// Modes:
//
//	helix webhook serve --addr :9090 --secret-file ~/.helix/webhook-secret
//	  Starts the HTTP server. Blocks until SIGINT/SIGTERM.
//
// Flags:
//
//	--addr <listen-addr>    default :9090
//	--secret-file <path>    path to file containing the HMAC secret
//	--path <url-path>       URL path the handler responds to (default /)
//	--verbose               verbose stderr logging
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/totalwindupflightsystems/helix/pkg/webhook"
)

// envWebhookSecretFile is the env var the operator can use to avoid
// hardcoding the secret-file path in CI / crontabs.
const envWebhookSecretFile = "HELIX_WEBHOOK_SECRET_FILE"

// webhookServeFlags captures every CLI flag in one struct so the Run
// function can stay a pure reader + caller.
type webhookServeFlags struct {
	addr       string
	secretFile string
	path       string
	verbose    bool
}

// parseWebhookServeFlags builds a flag.FS, parses args, and returns
// populated flags + showHelp + error.
//
// showHelp=true means the user passed --help — caller prints help and
// exits 0.
func parseWebhookServeFlags(args []string, stdout, stderr io.Writer) (webhookServeFlags, bool, error) {
	fs := flag.NewFlagSet("helix-webhook-serve", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f webhookServeFlags

	fs.StringVar(&f.addr, "addr", envOr("HELIX_WEBHOOK_ADDR", ":9090"),
		"HTTP listen address (env HELIX_WEBHOOK_ADDR)")
	fs.StringVar(&f.secretFile, "secret-file", envOr(envWebhookSecretFile, ""),
		"Path to file containing the HMAC-SHA256 shared secret (env HELIX_WEBHOOK_SECRET_FILE)")
	fs.StringVar(&f.path, "path", "/",
		"URL path the handler responds to (default /)")
	fs.BoolVar(&f.verbose, "verbose", false, "Verbose stderr logging")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printWebhookUsage(stdout)
			return f, true, nil
		}
		return f, false, err
	}
	if rest := fs.Args(); len(rest) > 0 {
		return f, false, fmt.Errorf("unexpected positional arguments: %v", rest)
	}
	return f, false, nil
}

// runWebhookServe is the cmd/helix webhook serve entry point.
func runWebhookServe(args []string, stdout, stderr io.Writer) int {
	flags, showHelp, err := parseWebhookServeFlags(args, stdout, stderr)
	if showHelp {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "helix webhook serve: parse:", err)
		return 2
	}
	if flags.secretFile == "" {
		fmt.Fprintln(stderr, "helix webhook serve: --secret-file is required")
		printWebhookUsage(stderr)
		return 2
	}

	// Read the secret from disk.
	secret, err := os.ReadFile(flags.secretFile)
	if err != nil {
		fmt.Fprintln(stderr, "helix webhook serve: read secret:", err)
		return 1
	}
	if len(secret) == 0 {
		fmt.Fprintln(stderr, "helix webhook serve: secret file is empty")
		return 1
	}

	if flags.verbose {
		fmt.Fprintf(stderr, "[webhook serve] addr=%s secret-file=%s path=%s\n",
			flags.addr, flags.secretFile, flags.path)
	}

	handler := webhook.NewHandler(secret)
	handler.Path = flags.path
	handler.Output = stdout

	// Wire SIGINT/SIGTERM for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- webhook.ListenAndServe(flags.addr, handler)
	}()

	// Wait for signal or fatal server error.
	select {
	case sig := <-sigCh:
		if flags.verbose {
			fmt.Fprintf(stderr, "[webhook serve] received signal %s, shutting down\n", sig)
		}
		// http.Server has no graceful Shutdown method in this scope —
		// the ListenAndServe goroutine will exit on the next accepted
		// request's connection close. For now, just exit.
		return 0
	case err := <-errCh:
		if err != nil {
			fmt.Fprintln(stderr, "helix webhook serve: server error:", err)
			return 1
		}
		return 0
	}
}

// runWebhook is the cmd/helix webhook subcommand entry point. Routes to
// runWebhookServe when called with "serve", otherwise prints help.
func runWebhook(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printWebhookUsage(stdout)
		return 0
	}
	switch args[0] {
	case "serve":
		return runWebhookServe(args[1:], stdout, stderr)
	case "--help", "-h", "help":
		printWebhookUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "helix webhook: unknown subcommand %q\n\n", args[0])
		printWebhookUsage(stderr)
		return 2
	}
}

// printWebhookUsage renders the webhook subcommand's help text.
func printWebhookUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage: helix webhook <subcommand> [flags]

Forgejo webhook ingestion server.

Subcommands:
  serve    Start the HTTP server that ingests Forgejo webhook events

serve flags:
  --addr <listen-addr>     HTTP listen address (default :9090)
  --secret-file <path>     Path to file containing the HMAC-SHA256 secret (required)
  --path <url-path>        URL path the handler responds to (default /)
  --verbose                Verbose stderr logging

Environment:
  HELIX_WEBHOOK_ADDR             default listen address
  HELIX_WEBHOOK_SECRET_FILE      default secret file path

Exit codes:
  0  server stopped cleanly
  1  server failed to start or read secret
  2  invalid arguments

Examples:
  # Generate a random secret
  openssl rand -hex 32 > ~/.helix/webhook-secret

  # Start the server
  helix webhook serve --addr :9090 --secret-file ~/.helix/webhook-secret

  # Configure Forgejo to send webhooks to http://your-host:9090/
  # with the secret from ~/.helix/webhook-secret`)
}
