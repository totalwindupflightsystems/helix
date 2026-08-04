// Command helix — source.go
//
// `helix source` manages integration sources defined in .helix/sources.yaml
// (SPEC-025 §7): add, list, test, and tools generation via the Muster
// bridge (pkg/source).
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/totalwindupflightsystems/helix/pkg/source"
)

const (
	sourceExitOK    = 0
	sourceExitError = 2

	// envSourceFile overrides the sources file path (default: .helix/sources.yaml).
	envSourceFile = "HELIX_SOURCES_FILE"
	// envMusterURL overrides the Muster base URL (default: http://localhost:9090).
	envMusterURL = "HELIX_MUSTER_URL"
)

// sourceFlags holds parsed flags for helix source subcommands.
type sourceFlags struct {
	subcommand    string
	name          string
	typ           string
	specPath      string
	connection    string
	baseURL       string
	root          string
	rateLimit     string
	tokenEnv      string
	readOnly      bool
	allowedAgents string
	enabled       bool
	dryRun        bool
}

// parseSourceFlags parses `helix source` arguments. It returns the parsed
// flags, whether help was requested, and an exit code (sourceExitOK on
// success, sourceExitError on malformed arguments).
func parseSourceFlags(args []string) (sourceFlags, bool, int) {
	var f sourceFlags
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--enabled":
			f.enabled = true
		case arg == "--read-only":
			f.readOnly = true
		case arg == "--dry-run":
			f.dryRun = true
		case arg == "--name":
			i++
			if i >= len(args) {
				return f, false, sourceExitError
			}
			f.name = args[i]
		case arg == "--type":
			i++
			if i >= len(args) {
				return f, false, sourceExitError
			}
			f.typ = args[i]
		case arg == "--spec":
			i++
			if i >= len(args) {
				return f, false, sourceExitError
			}
			f.specPath = args[i]
		case arg == "--connection":
			i++
			if i >= len(args) {
				return f, false, sourceExitError
			}
			f.connection = args[i]
		case arg == "--base-url":
			i++
			if i >= len(args) {
				return f, false, sourceExitError
			}
			f.baseURL = args[i]
		case arg == "--root":
			i++
			if i >= len(args) {
				return f, false, sourceExitError
			}
			f.root = args[i]
		case arg == "--rate-limit":
			i++
			if i >= len(args) {
				return f, false, sourceExitError
			}
			f.rateLimit = args[i]
		case arg == "--token-env":
			i++
			if i >= len(args) {
				return f, false, sourceExitError
			}
			f.tokenEnv = args[i]
		case arg == "--allowed-agents":
			i++
			if i >= len(args) {
				return f, false, sourceExitError
			}
			f.allowedAgents = args[i]
		default:
			if !strings.HasPrefix(arg, "-") && f.subcommand == "" {
				f.subcommand = arg
			} else {
				return f, false, sourceExitError
			}
		}
		i++
	}
	return f, helpWanted, sourceExitOK
}

const sourceHelp = `helix source — manage integration sources (.helix/sources.yaml)

Usage:
  helix source add    --name STRING --type [postgres|rest|local] --spec PATH
  helix source list   [--enabled]
  helix source test   --name STRING
  helix source tools  --name STRING

Options:
  --name STRING         Source name (key in .helix/sources.yaml)
  --type TYPE           Source type: postgres | rest | local
  --spec PATH           OpenAPI spec file path or http(s) URL
  --connection STRING   Postgres connection string (type=postgres)
  --base-url STRING     REST base URL (type=rest)
  --root PATH           Local filesystem root (type=local)
  --rate-limit SPEC     Rate limit (e.g. 10/s)
  --token-env VAR       Environment variable holding the API token
  --read-only           Mark the source read-only
  --allowed-agents LIST Comma-separated agent IDs allowed to use the source
  --enabled             list: show only enabled sources
  --dry-run             add: show what would be written without writing

Environment:
  HELIX_SOURCES_FILE   Override sources file path (default: .helix/sources.yaml)
  HELIX_MUSTER_URL     Override Muster base URL (default: http://localhost:9090)
`

// printSourceHelp renders the source subcommand help text.
func printSourceHelp(w io.Writer) {
	fmt.Fprint(w, sourceHelp)
}

// sourcesFilePath resolves the sources YAML path: HELIX_SOURCES_FILE when
// set, otherwise .helix/sources.yaml relative to the current directory.
func sourcesFilePath() string {
	if p := os.Getenv(envSourceFile); p != "" {
		return p
	}
	return filepath.Join(".helix", "sources.yaml")
}

// musterURL resolves the Muster base URL: HELIX_MUSTER_URL when set,
// otherwise the MusterBridge default.
func musterURL() string {
	if u := os.Getenv(envMusterURL); u != "" {
		return u
	}
	return "http://localhost:9090"
}

// runSource dispatches to the requested source subcommand.
func runSource(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, exitCode := parseSourceFlags(args)
	if exitCode != sourceExitOK {
		fmt.Fprintln(stderr, "source: invalid arguments")
		printSourceHelp(stderr)
		return sourceExitError
	}
	if helpWanted || flags.subcommand == "" {
		printSourceHelp(stdout)
		return sourceExitOK
	}

	switch flags.subcommand {
	case "add":
		return runSourceAdd(flags, stdout, stderr)
	case "list":
		return runSourceList(flags, stdout, stderr)
	case "test":
		return runSourceTest(flags, stdout, stderr)
	case "tools":
		return runSourceTools(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "source: unknown subcommand %q\n\n", flags.subcommand)
		printSourceHelp(stderr)
		return sourceExitError
	}
}

// runSourceWithDryRun threads the global --dry-run flag through to the
// source subcommands and maps non-zero exit codes to errExit so main.go
// propagates the documented exit-code contract.
func runSourceWithDryRun(args []string, stdout, stderr io.Writer, dryRun bool) error {
	if dryRun {
		args = append(append([]string{}, args...), "--dry-run")
	}
	rc := runSource(args, stdout, stderr)
	if rc != sourceExitOK {
		return errExit{code: rc}
	}
	return nil
}

// runSourceAdd creates or upserts a source in .helix/sources.yaml. The new
// source is validated before anything is written; on validation failure the
// error is reported and the file is left untouched.
func runSourceAdd(f sourceFlags, stdout, stderr io.Writer) int {
	if f.name == "" {
		fmt.Fprintln(stderr, "source add: --name is required")
		printSourceHelp(stderr)
		return sourceExitError
	}
	if f.typ == "" {
		fmt.Fprintln(stderr, "source add: --type is required (postgres, rest, local)")
		printSourceHelp(stderr)
		return sourceExitError
	}

	// Type-specific field requirements (connection/base_url/root/openapi)
	// are enforced by source.Validate below — a local source, for example,
	// needs no OpenAPI spec.

	src := source.Source{
		Name:       f.name,
		Type:       source.SourceType(f.typ),
		Connection: f.connection,
		OpenAPI:    f.specPath,
		BaseURL:    f.baseURL,
		Root:       f.root,
		RateLimit:  f.rateLimit,
		TokenEnv:   f.tokenEnv,
		ReadOnly:   f.readOnly,
	}
	for _, a := range strings.Split(f.allowedAgents, ",") {
		if a = strings.TrimSpace(a); a != "" {
			src.AllowedAgents = append(src.AllowedAgents, a)
		}
	}

	if err := src.Validate(); err != nil {
		fmt.Fprintf(stderr, "source add: %v\n", err)
		return sourceExitError
	}

	path := sourcesFilePath()
	if f.dryRun {
		fmt.Fprintf(stdout, "[DRY-RUN] would add source %q (type: %s, spec: %s) to %s\n",
			src.Name, src.Type, src.OpenAPI, path)
		return sourceExitOK
	}

	file, err := source.ParseSourcesYAML(path)
	if err != nil {
		fmt.Fprintf(stderr, "source add: %v\n", err)
		return sourceExitError
	}
	if file.Sources == nil {
		file.Sources = make(map[string]source.Source)
	}
	file.Sources[src.Name] = src

	data, err := yaml.Marshal(file)
	if err != nil {
		fmt.Fprintf(stderr, "source add: marshal: %v\n", err)
		return sourceExitError
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(stderr, "source add: %v\n", err)
		return sourceExitError
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintf(stderr, "source add: %v\n", err)
		return sourceExitError
	}

	fmt.Fprintf(stdout, "✓ source %q added (type: %s, spec: %s) to %s\n",
		src.Name, src.Type, src.OpenAPI, path)
	return sourceExitOK
}

// runSourceList prints configured sources as a table sorted by name.
func runSourceList(f sourceFlags, stdout, stderr io.Writer) int {
	file, err := source.ParseSourcesYAML(sourcesFilePath())
	if err != nil {
		fmt.Fprintf(stderr, "source list: %v\n", err)
		return sourceExitError
	}

	names := make([]string, 0, len(file.Sources))
	for name := range file.Sources {
		names = append(names, name)
	}
	sort.Strings(names)

	if len(names) == 0 {
		fmt.Fprintln(stdout, "no sources configured")
		return sourceExitOK
	}

	fmt.Fprintf(stdout, "%-20s %-10s %-9s %-10s %s\n", "NAME", "TYPE", "READ_ONLY", "RATE_LIMIT", "ALLOWED_AGENTS")
	printed := 0
	for _, name := range names {
		s := file.Sources[name]
		if f.enabled && !s.IsEnabled() {
			continue
		}
		rateLimit := s.RateLimit
		if rateLimit == "" {
			rateLimit = "-"
		}
		fmt.Fprintf(stdout, "%-20s %-10s %-9t %-10s %s\n",
			name, s.Type, s.ReadOnly, rateLimit, strings.Join(s.AllowedAgents, ","))
		printed++
	}
	if printed == 0 {
		fmt.Fprintln(stdout, "no enabled sources configured")
	}
	return sourceExitOK
}

// runSourceTest runs connectivity checks against a configured source:
// spec presence, type-specific probe, and Muster health. A Muster outage is
// reported as a warning and does not fail the command when all source
// probes passed.
func runSourceTest(f sourceFlags, stdout, stderr io.Writer) int {
	if f.name == "" {
		fmt.Fprintln(stderr, "source test: --name is required")
		printSourceHelp(stderr)
		return sourceExitError
	}

	file, err := source.ParseSourcesYAML(sourcesFilePath())
	if err != nil {
		fmt.Fprintf(stderr, "source test: %v\n", err)
		return sourceExitError
	}
	src, ok := file.Sources[f.name]
	if !ok {
		fmt.Fprintf(stderr, "source test: source %q not found\n", f.name)
		return sourceExitError
	}
	src.Name = f.name
	if err := src.Validate(); err != nil {
		fmt.Fprintf(stderr, "source test: %v\n", err)
		return sourceExitError
	}

	failed := 0

	// OpenAPI spec presence (remote URLs are not file-checked).
	if src.OpenAPI != "" {
		if isHTTPURL(src.OpenAPI) {
			fmt.Fprintf(stdout, "✓ openapi spec %s (remote URL — skipping file check)\n", src.OpenAPI)
		} else if _, err := os.Stat(src.OpenAPI); err != nil {
			fmt.Fprintf(stdout, "✗ openapi spec %s: %v\n", src.OpenAPI, err)
			failed++
		} else {
			fmt.Fprintf(stdout, "✓ openapi spec %s exists\n", src.OpenAPI)
		}
	}

	// Type-specific reachability probe.
	switch src.Type {
	case source.SourceTypeLocal:
		info, err := os.Stat(src.Root)
		switch {
		case err != nil:
			fmt.Fprintf(stdout, "✗ local root %s: %v\n", src.Root, err)
			failed++
		case !info.IsDir():
			fmt.Fprintf(stdout, "✗ local root %s: not a directory\n", src.Root)
			failed++
		default:
			fmt.Fprintf(stdout, "✓ local root %s is a directory\n", src.Root)
		}
	case source.SourceTypeREST:
		if err := probeREST(src.BaseURL); err != nil {
			fmt.Fprintf(stdout, "✗ rest base_url %s: %v\n", src.BaseURL, err)
			failed++
		} else {
			fmt.Fprintf(stdout, "✓ rest base_url %s reachable\n", src.BaseURL)
		}
	case source.SourceTypePostgres:
		host, port, err := parsePostgresAddr(src.Connection)
		if err != nil {
			fmt.Fprintf(stdout, "⚠ postgres connection not parseable — skipping probe: %v\n", err)
		} else if err := probeTCP(host, port); err != nil {
			fmt.Fprintf(stdout, "✗ postgres %s:%s: %v\n", host, port, err)
			failed++
		} else {
			fmt.Fprintf(stdout, "✓ postgres %s:%s reachable\n", host, port)
		}
	}

	// Muster health — a warning only; tools generation can be retried later.
	bridge := source.NewMusterBridge(source.BridgeConfig{BaseURL: musterURL()})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := bridge.HealthWithCtx(ctx); err != nil {
		fmt.Fprintln(stdout, "⚠ muster unreachable — tools generation unavailable")
	} else {
		fmt.Fprintln(stdout, "✓ muster reachable")
	}

	if failed > 0 {
		fmt.Fprintf(stdout, "✗ %d check(s) failed for source %q\n", failed, src.Name)
		return sourceExitError
	}
	fmt.Fprintf(stdout, "✓ source %q checks passed\n", src.Name)
	return sourceExitOK
}

// runSourceTools generates MCP tools for a source via Muster and prints
// them sorted by name.
func runSourceTools(f sourceFlags, stdout, stderr io.Writer) int {
	if f.name == "" {
		fmt.Fprintln(stderr, "source tools: --name is required")
		printSourceHelp(stderr)
		return sourceExitError
	}

	file, err := source.ParseSourcesYAML(sourcesFilePath())
	if err != nil {
		fmt.Fprintf(stderr, "source tools: %v\n", err)
		return sourceExitError
	}
	src, ok := file.Sources[f.name]
	if !ok {
		fmt.Fprintf(stderr, "source tools: source %q not found\n", f.name)
		return sourceExitError
	}
	src.Name = f.name

	bridge := source.NewMusterBridge(source.BridgeConfig{BaseURL: musterURL()})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ts, err := bridge.GenerateToolsFromSource(ctx, &src)
	if err != nil {
		fmt.Fprintf(stderr, "source tools: %v\n", err)
		return sourceExitError
	}

	tools := ts.Tools
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	for _, tool := range tools {
		fmt.Fprintf(stdout, "%s  %s %s  auth=%t\n", tool.Name, tool.Method, tool.Path, tool.AuthRequired)
	}
	fmt.Fprintf(stdout, "%d tool(s) from %s\n", len(tools), src.Name)
	return sourceExitOK
}

// ---------------------------------------------------------------------------
// Probes
// ---------------------------------------------------------------------------

// probeREST checks that baseURL is reachable via HTTP. 2xx/3xx responses and
// auth-wrapped responses (401/403) count as reachable; network errors fail.
func probeREST(baseURL string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Head(baseURL)
	if err != nil {
		// Some servers reject HEAD; fall back to GET.
		resp, err = client.Get(baseURL)
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 400:
		return nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil
	default:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
}

// probeTCP dials host:port with a 5s timeout.
func probeTCP(host, port string) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 5*time.Second)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// parsePostgresAddr extracts host and port from a postgres connection URL.
// The port defaults to 5432 when omitted.
func parsePostgresAddr(connStr string) (string, string, error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return "", "", err
	}
	host := u.Hostname()
	if host == "" {
		return "", "", fmt.Errorf("no host in %q", connStr)
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	return host, port, nil
}

// isHTTPURL reports whether s is an http(s) URL (used to skip local-file
// existence checks for remote OpenAPI specs).
func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
