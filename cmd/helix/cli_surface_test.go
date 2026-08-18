package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIHelp is the CLI-surface regression test: every documented
// subcommand must appear in the `helix --help` output rendered by
// printUsage(). A subcommand silently disappearing from the help text is
// a CLI-contract regression that previously only manual gap sweeps could
// catch.
//
// The expected surface is derived from three independent sources:
//  1. The code-side registry (allSubcommandNames()) — help must not lag
//     the subcommands the dispatcher actually accepts.
//  2. The README component table — every `helix <word>` the README
//     documents as a dispatcher subcommand must be listed in help.
//  3. A curated hardcoded backstop of the core subcommand surface, so a
//     subcommand dropped from both README and the registry still fails
//     the test if it disappears from --help.
func TestCLIHelp(t *testing.T) {
	out := captureStdout(printUsage)

	// Layer 1: code-side registry. builtinSubcommands carries a
	// "KEEP IN SYNC with the switch cases" contract; help output must
	// stay in sync with the registry too.
	registered := map[string]bool{}
	for _, name := range allSubcommandNames() {
		registered[name] = true
		if !usageListsSubcommand(out, name) {
			t.Errorf("printUsage() does not list subcommand %q, but allSubcommandNames() registers it", name)
		}
	}

	// Layer 2: README component table. Intersect with the registry so
	// stale README rows that name legacy aliases rather than real
	// dispatcher subcommands ("helix coordinator" -> `helix pipeline`,
	// "helix health" -> `helix status`) do not fail the check; every
	// README-documented subcommand the CLI actually accepts must appear
	// in help.
	for name := range documentedSubcommandsFromREADME(t) {
		if !registered[name] {
			continue // legacy alias in README, not a dispatcher subcommand
		}
		if !usageListsSubcommand(out, name) {
			t.Errorf("printUsage() does not list %q, which the README component table documents as a subcommand", name)
		}
	}

	// Layer 3: curated hardcoded backstop of the core subcommand surface.
	core := []string{
		"identity", "estimate", "negotiate", "prompt", "marketplace",
		"sandbox", "version", "banner", "status", "doctor", "dispatch",
		"dispatcher", "review", "verify", "release", "trust", "mergegate",
		"security", "forgejo", "coapproval", "adversarial", "secrets",
		"pipeline", "lifecycle", "webhook", "incident", "config", "alerts",
		"retry", "backup", "degradation", "audit", "api", "integration",
		"forcemerge", "vuln", "deploy", "ci", "recovery", "memory", "idea",
		"adr", "spec", "source", "channel", "design", "contract", "notify",
		"models", "learn",
	}
	for _, name := range core {
		if !usageListsSubcommand(out, name) {
			t.Errorf("printUsage() does not list core subcommand %q: a documented CLI contract is missing from --help", name)
		}
	}
}

// documentedSubcommandsFromREADME returns the set of `helix <word>`
// subcommand references found in the README component table. Only table
// rows (lines containing "|") are scanned; standalone-binary entries such
// as `helix-identity` or `helix-release` use a hyphen after "helix" and
// are not dispatcher subcommands, so the "helix " prefix requirement
// excludes them. Multi-word references ("helix incident patterns",
// "helix dispatcher clarify") contribute their first token only.
func documentedSubcommandsFromREADME(t *testing.T) map[string]bool {
	t.Helper()
	readmePath := filepath.Join("..", "..", "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("cannot read %s: %v", readmePath, err)
	}
	names := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "|") {
			continue
		}
		for _, tok := range strings.Split(line, "`") {
			rest, ok := strings.CutPrefix(tok, "helix ")
			if !ok {
				continue
			}
			word := strings.Fields(rest)[0]
			if isSubcommandWord(word) {
				names[word] = true
			}
		}
	}
	return names
}

// isSubcommandWord reports whether s is a bare subcommand word (lowercase
// letters, digits, hyphens).
func isSubcommandWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

// usageListsSubcommand reports whether out contains a subcommand entry
// line for name: two-space indent, the bare name, then a space. The
// line-anchored check ensures description prose (e.g. "(spec §6.7)" in
// the incident line) can never satisfy the assertion.
func usageListsSubcommand(out, name string) bool {
	prefix := "  " + name + " "
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}
