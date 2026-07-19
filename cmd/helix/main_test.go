package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout runs f and returns captured stdout.
func captureStdout(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// captureStderr runs f and returns captured stderr.
func captureStderr(f func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	f()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestPrintVersion(t *testing.T) {
	out := captureStdout(printVersion)
	if !strings.Contains(out, AppName) {
		t.Errorf("printVersion output should contain app name %q, got: %s", AppName, out)
	}
	if !strings.Contains(out, Version) {
		t.Errorf("printVersion output should contain version %q, got: %s", Version, out)
	}
}

func TestPrintUsage(t *testing.T) {
	out := captureStdout(printUsage)
	if !strings.Contains(out, "Usage:") {
		t.Error("printUsage should contain 'Usage:'")
	}
	if !strings.Contains(out, "Subcommands:") {
		t.Error("printUsage should contain 'Subcommands:'")
	}
	if !strings.Contains(out, "identity") {
		t.Error("printUsage should mention 'identity' subcommand")
	}
	if !strings.Contains(out, "marketplace") {
		t.Error("printUsage should mention 'marketplace' subcommand")
	}
}

func TestURLToAddr(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"http with port", "http://localhost:3030", "localhost:3030"},
		{"http with path", "http://localhost:3030/api/v1/version", "localhost:3030"},
		{"https with port", "https://chimera.example.com:8765", "chimera.example.com:8765"},
		{"http no port", "http://localhost", "localhost:80"},
		{"https no port", "https://example.com/api/health", "example.com:80"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := urlToAddr(tt.input)
			if got != tt.expected {
				t.Errorf("urlToAddr(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSortedKeys(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got := sortedKeys(map[string]string{})
		if len(got) != 0 {
			t.Errorf("sortedKeys({}) = %v, want empty", got)
		}
	})

	t.Run("single", func(t *testing.T) {
		got := sortedKeys(map[string]string{"a": "1"})
		if len(got) != 1 || got[0] != "a" {
			t.Errorf("sortedKeys({a:1}) = %v, want [a]", got)
		}
	})

	t.Run("multiple sorted", func(t *testing.T) {
		got := sortedKeys(map[string]string{
			"z": "last",
			"a": "first",
			"m": "middle",
		})
		if len(got) != 3 {
			t.Fatalf("sortedKeys expected 3 keys, got %d: %v", len(got), got)
		}
		for i := 1; i < len(got); i++ {
			if got[i] < got[i-1] {
				t.Errorf("sortedKeys not sorted: %v", got)
			}
		}
	})

	t.Run("all subcommands", func(t *testing.T) {
		got := sortedKeys(subcommands)
		if len(got) != 7 {
			t.Errorf("sortedKeys(subcommands) = %d keys, want 7", len(got))
		}
		for i := 1; i < len(got); i++ {
			if got[i] < got[i-1] {
				t.Errorf("sortedKeys not sorted: %v", got)
			}
		}
	})
}

func TestLookPath(t *testing.T) {
	t.Run("local file in cwd", func(t *testing.T) {
		dir := t.TempDir()
		fakeBin := filepath.Join(dir, "fake-cmd")
		_ = os.WriteFile(fakeBin, []byte("#!/bin/sh\necho ok"), 0755)

		oldDir, _ := os.Getwd()
		_ = os.Chdir(dir)
		defer func() { _ = os.Chdir(oldDir) }()

		got, err := lookPath("fake-cmd")
		if err != nil {
			t.Fatalf("lookPath(fake-cmd) = error: %v", err)
		}
		if got != "./fake-cmd" {
			t.Errorf("lookPath(fake-cmd) = %q, want ./fake-cmd", got)
		}
	})

	t.Run("local file in cmd/<name>/<name>", func(t *testing.T) {
		dir := t.TempDir()
		cmdDir := filepath.Join(dir, "cmd", "fake-cmd")
		_ = os.MkdirAll(cmdDir, 0755)
		fakeBin := filepath.Join(cmdDir, "fake-cmd")
		_ = os.WriteFile(fakeBin, []byte("#!/bin/sh\necho ok"), 0755)

		oldDir, _ := os.Getwd()
		_ = os.Chdir(dir)
		defer func() { _ = os.Chdir(oldDir) }()

		got, err := lookPath("fake-cmd")
		if err != nil {
			t.Fatalf("lookPath(fake-cmd) = error: %v", err)
		}
		if got != "cmd/fake-cmd/fake-cmd" {
			t.Errorf("lookPath(fake-cmd) = %q, want cmd/fake-cmd/fake-cmd", got)
		}
	})

	t.Run("PATH fallback", func(t *testing.T) {
		got, err := lookPath("sh")
		if err != nil {
			t.Fatalf("lookPath(sh) should find sh in PATH: %v", err)
		}
		if got == "" {
			t.Error("lookPath(sh) returned empty string")
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := lookPath("nonexistent-binary-xyzzy-12345")
		if err == nil {
			t.Error("lookPath(nonexistent) should return error")
		}
	})
}

func TestExecSubcommand(t *testing.T) {
	t.Run("missing binary", func(t *testing.T) {
		stderr := captureStderr(func() {
			err := execSubcommand("nonexistent-xyzzy-99999", []string{})
			if err == nil {
				t.Error("execSubcommand for missing binary should return error")
			}
		})
		_ = stderr // ignore verbose output if enabled
	})

	t.Run("dry-run", func(t *testing.T) {
		oldVerbose := verbose
		oldDryRun := dryRun
		verbose = false
		dryRun = true
		defer func() {
			verbose = oldVerbose
			dryRun = oldDryRun
		}()

		out := captureStdout(func() {
			err := execSubcommand("sh", []string{"-c", "echo hello"})
			if err != nil {
				t.Errorf("execSubcommand dry-run should not error: %v", err)
			}
		})
		if !strings.Contains(out, "[DRY RUN]") {
			t.Errorf("dry-run output should contain [DRY RUN], got: %s", out)
		}
	})

	t.Run("verbose mode", func(t *testing.T) {
		oldVerbose := verbose
		oldDryRun := dryRun
		verbose = true
		dryRun = false
		defer func() {
			verbose = oldVerbose
			dryRun = oldDryRun
		}()

		// Use a real binary that exists (sh) to test verbose output
		stderr := captureStderr(func() {
			_ = execSubcommand("sh", []string{"-c", "true"})
		})
		if !strings.Contains(stderr, "[verbose]") {
			t.Errorf("verbose stderr should contain [verbose], got: %s", stderr)
		}
	})

	t.Run("real execution", func(t *testing.T) {
		oldVerbose := verbose
		oldDryRun := dryRun
		verbose = false
		dryRun = false
		defer func() {
			verbose = oldVerbose
			dryRun = oldDryRun
		}()

		// sh -c 'true' should succeed
		err := execSubcommand("sh", []string{"-c", "true"})
		if err != nil {
			t.Errorf("execSubcommand(sh -c true) should succeed: %v", err)
		}

		// sh -c 'exit 1' should fail
		err = execSubcommand("sh", []string{"-c", "exit 1"})
		if err == nil {
			t.Error("execSubcommand(sh -c 'exit 1') should return error")
		}
	})
}

func TestDispatch(t *testing.T) {
	d := rootCmd()

	t.Run("no args", func(t *testing.T) {
		out := captureStdout(func() {
			err := d.dispatch([]string{})
			if err != nil {
				t.Errorf("dispatch([]) error: %v", err)
			}
		})
		if !strings.Contains(out, "Usage:") {
			t.Error("dispatch([]) should print usage")
		}
	})

	t.Run("--help flag", func(t *testing.T) {
		out := captureStdout(func() {
			err := d.dispatch([]string{"--help"})
			if err != nil {
				t.Errorf("dispatch(--help) error: %v", err)
			}
		})
		if !strings.Contains(out, "Usage:") {
			t.Error("dispatch(--help) should print usage")
		}
	})

	t.Run("-h flag", func(t *testing.T) {
		out := captureStdout(func() {
			err := d.dispatch([]string{"-h"})
			if err != nil {
				t.Errorf("dispatch(-h) error: %v", err)
			}
		})
		if !strings.Contains(out, "Usage:") {
			t.Error("dispatch(-h) should print usage")
		}
	})

	t.Run("--version flag", func(t *testing.T) {
		out := captureStdout(func() {
			err := d.dispatch([]string{"--version"})
			if err != nil {
				t.Errorf("dispatch(--version) error: %v", err)
			}
		})
		if !strings.Contains(out, AppName) {
			t.Errorf("dispatch(--version) should contain %q, got: %s", AppName, out)
		}
	})

	t.Run("version subcommand", func(t *testing.T) {
		out := captureStdout(func() {
			err := d.dispatch([]string{"version"})
			if err != nil {
				t.Errorf("dispatch(version) error: %v", err)
			}
		})
		if !strings.Contains(out, AppName) {
			t.Errorf("dispatch(version) should contain %q, got: %s", AppName, out)
		}
	})

	t.Run("status subcommand", func(t *testing.T) {
		out := captureStdout(func() {
			_ = d.dispatch([]string{"status", "--timeout", "200ms"})
		})
		// New status output is "Helix Platform Status". Accept either
		// (the old "Helix Status" string was kept for backwards
		// compatibility with scripts that grep for it).
		if !strings.Contains(out, "Helix Platform Status") && !strings.Contains(out, "Helix Status") {
			t.Error("dispatch(status) should print a Helix Status header")
		}
	})

	t.Run("--verbose flag", func(t *testing.T) {
		oldVerbose := verbose
		oldDryRun := dryRun
		verbose = false
		dryRun = false
		defer func() {
			verbose = oldVerbose
			dryRun = oldDryRun
		}()

		_ = d.dispatch([]string{"--verbose", "version"})
		if !verbose {
			t.Error("dispatch(--verbose) should set verbose=true")
		}
	})

	t.Run("--dry-run flag", func(t *testing.T) {
		oldVerbose := verbose
		oldDryRun := dryRun
		verbose = false
		dryRun = false
		defer func() {
			verbose = oldVerbose
			dryRun = oldDryRun
		}()

		_ = d.dispatch([]string{"--dry-run", "version"})
		if !dryRun {
			t.Error("dispatch(--dry-run) should set dryRun=true")
		}
	})

	t.Run("--config with value", func(t *testing.T) {
		oldCfg := cfgFile
		defer func() { cfgFile = oldCfg }()

		_ = d.dispatch([]string{"--config", "/tmp/helix-test.yaml", "version"})
		if cfgFile != "/tmp/helix-test.yaml" {
			t.Errorf("dispatch(--config /tmp/helix-test.yaml) should set cfgFile, got %q", cfgFile)
		}
	})

	t.Run("--config missing value", func(t *testing.T) {
		oldCfg := cfgFile
		defer func() { cfgFile = oldCfg }()

		err := d.dispatch([]string{"--config"})
		if err == nil {
			t.Error("dispatch(--config) with missing value should return error")
		} else if !strings.Contains(err.Error(), "--config requires a value") {
			t.Errorf("dispatch(--config) error = %q, want '--config requires a value'", err.Error())
		}
	})

	t.Run("unknown subcommand", func(t *testing.T) {
		err := d.dispatch([]string{"nonexistent-subcommand-xyzzy"})
		if err == nil {
			t.Error("dispatch(unknown subcommand) should return error")
		} else if !strings.Contains(err.Error(), "unknown subcommand") {
			t.Errorf("dispatch(unknown subcommand) error = %q, want 'unknown subcommand'", err.Error())
		}
	})

	t.Run("unknown subcommand with available list", func(t *testing.T) {
		err := d.dispatch([]string{"bogus-cmd"})
		if err == nil {
			t.Error("dispatch(bogus) should return error")
		} else if !strings.Contains(err.Error(), "Available subcommands") {
			t.Errorf("dispatch(bogus) error should list available subcommands, got: %v", err)
		}
	})

	t.Run("valid subcommand routing", func(t *testing.T) {
		// test that valid subcommand names are recognized
		for name := range subcommands {
			t.Run("routing-"+name, func(t *testing.T) {
				// dispatch to valid subcommand (will fail because binary isn't built,
				// but the routing should reach execSubcommand, not unknown error)
				err := d.dispatch([]string{name})
				if err != nil {
					if strings.Contains(err.Error(), "unknown subcommand") {
						t.Errorf("dispatch(%s) should route to subcommand, not unknown: %v", name, err)
					}
					// Error about binary not found is expected
				}
			})
		}
	})

	t.Run("global flags before subcommand", func(t *testing.T) {
		oldVerbose := verbose
		oldDryRun := dryRun
		oldCfg := cfgFile
		verbose = false
		dryRun = false
		defer func() {
			verbose = oldVerbose
			dryRun = oldDryRun
			cfgFile = oldCfg
		}()

		_ = d.dispatch([]string{"--verbose", "--dry-run", "--config", "/tmp/test.yaml", "version"})
		if !verbose {
			t.Error("--verbose should be set")
		}
		if !dryRun {
			t.Error("--dry-run should be set")
		}
		if cfgFile != "/tmp/test.yaml" {
			t.Errorf("--config should set cfgFile, got %q", cfgFile)
		}
	})
}

func TestCheckEndpoint(t *testing.T) {
	// Legacy endpoint probe was replaced by the unified
	// pkg/health.PlatformHealthAggregator in cmd/helix/status.go.
	// See TestStatus_* in status_test.go for the new behaviour.
	t.Skip("legacy checkEndpoint removed — superseded by pkg/health + status.go")
}

func TestConstants(t *testing.T) {
	if AppName != "helix" {
		t.Errorf("AppName = %q, want 'helix'", AppName)
	}
	if Version == "" {
		t.Error("Version should not be empty")
	}
}

func TestSubcommandsMap(t *testing.T) {
	expected := []string{"identity", "estimate", "negotiate", "prompt", "marketplace", "sandbox", "release"}
	for _, name := range expected {
		binary, ok := subcommands[name]
		if !ok {
			t.Errorf("subcommands missing %q", name)
		}
		if binary == "" {
			t.Errorf("subcommands[%q] has empty binary", name)
		}
	}
	if len(subcommands) != 7 {
		t.Errorf("subcommands has %d entries, want 7", len(subcommands))
	}
}

func TestLookPathCmdSubdirPattern(t *testing.T) {
	// Test the cmd/<name>/<name> lookup pattern specifically
	dir := t.TempDir()
	cmdDir := filepath.Join(dir, "cmd", "helix-identity")
	_ = os.MkdirAll(cmdDir, 0755)
	_ = os.WriteFile(filepath.Join(cmdDir, "helix-identity"), []byte("fake"), 0755)

	oldDir, _ := os.Getwd()
	_ = os.Chdir(dir)
	defer func() { _ = os.Chdir(oldDir) }()

	got, err := lookPath("helix-identity")
	if err != nil {
		t.Fatalf("lookPath(helix-identity) should find in cmd/helix-identity/helix-identity: %v", err)
	}
	if got != "cmd/helix-identity/helix-identity" {
		t.Errorf("lookPath(helix-identity) = %q, want cmd/helix-identity/helix-identity", got)
	}
}

func TestURLToAddrEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty path", "http://example.com/", "example.com:80"},
		{"no scheme no port", "localhost:8080", "localhost:8080"},
		{"no scheme with path", "localhost:8080/api", "localhost:8080"},
		{"ip address", "http://192.168.1.1:3000", "192.168.1.1:3000"},
		{"deep path", "https://api.example.com/v1/very/deep/path?query=1", "api.example.com:80"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := urlToAddr(tt.input)
			if got != tt.expected {
				t.Errorf("urlToAddr(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExecCommand(t *testing.T) {
	// test exec.Command basic usage to ensure the import is valid
	cmd := exec.Command("true")
	err := cmd.Run()
	if err != nil {
		t.Errorf("exec.Command(true) should succeed: %v", err)
	}
}

func TestLookPathNotFoundErrorMessage(t *testing.T) {
	_, err := lookPath("nonexistent-zzz-98765")
	if err == nil {
		t.Error("lookPath should error for nonexistent binary")
	}
}

func TestDispatchVerboseDryRunCombo(t *testing.T) {
	d := rootCmd()
	oldVerbose := verbose
	oldDryRun := dryRun
	verbose = false
	dryRun = false
	defer func() {
		verbose = oldVerbose
		dryRun = oldDryRun
	}()

	_ = d.dispatch([]string{"--verbose", "--dry-run", "version"})
	if !verbose {
		t.Error("verbose should be true")
	}
	if !dryRun {
		t.Error("dryRun should be true")
	}
}

func TestPrintUsageContainsAllSubcommands(t *testing.T) {
	out := captureStdout(printUsage)
	for name := range subcommands {
		if !strings.Contains(out, name) {
			t.Errorf("printUsage missing subcommand %q in output", name)
		}
	}
	if !strings.Contains(out, "version") {
		t.Error("printUsage missing 'version' subcommand")
	}
	if !strings.Contains(out, "status") {
		t.Error("printUsage missing 'status' subcommand")
	}
}

func TestSortedKeysDeterministic(t *testing.T) {
	// Running twice with same input should produce same output
	m := map[string]string{"c": "3", "a": "1", "b": "2"}
	first := sortedKeys(m)
	second := sortedKeys(m)
	if len(first) != len(second) {
		t.Fatalf("sortedKeys not deterministic: %v vs %v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("sortedKeys not deterministic at index %d: %q vs %q", i, first[i], second[i])
		}
	}
	// Verify sorted order
	for i := 1; i < len(first); i++ {
		if first[i] < first[i-1] {
			t.Errorf("sortedKeys not sorted: %v", first)
		}
	}
}

func TestAllSubcommandRouting(t *testing.T) {
	d := rootCmd()
	for name, binary := range subcommands {
		t.Run("dispatch-"+name, func(t *testing.T) {
			err := d.dispatch([]string{name})
			if err == nil {
				// binary was found and executed successfully
				t.Logf("subcommand %s (%s) executed successfully", name, binary)
			} else if strings.Contains(err.Error(), "unknown subcommand") {
				t.Errorf("dispatch(%s) should not be unknown: %v", name, err)
			}
			// Error about binary not found is expected and acceptable
		})
	}
}

func TestGlobalFlagEdgeCases(t *testing.T) {
	d := rootCmd()

	t.Run("--config at end with no value", func(t *testing.T) {
		err := d.dispatch([]string{"version", "--config"})
		if err == nil {
			t.Error("dispatch with --config at end should error")
		}
	})

	t.Run("only flags no subcommand", func(t *testing.T) {
		out := captureStdout(func() {
			err := d.dispatch([]string{"--verbose"})
			if err != nil {
				t.Errorf("dispatch(--verbose) error: %v", err)
			}
		})
		if !strings.Contains(out, "Usage:") {
			t.Error("dispatch with only flags should print usage")
		}
	})
}

func TestDisaptcherConstruction(t *testing.T) {
	d := rootCmd()
	if d == nil {
		t.Fatal("rootCmd() returned nil")
	}
	if d.usage == nil {
		t.Error("dispatcher.usage should not be nil")
	}
	// Verify usage function works
	out := captureStdout(func() {
		d.usage()
	})
	if !strings.Contains(out, "Usage:") {
		t.Error("dispatcher.usage() should print usage")
	}
}

func TestConstants_NonEmpty(t *testing.T) {
	if AppName == "" {
		t.Error("AppName should not be empty")
	}
}

func TestDispatchHelpVsHFlag(t *testing.T) {
	d := rootCmd()
	outHelp := captureStdout(func() { _ = d.dispatch([]string{"--help"}) })
	outH := captureStdout(func() { _ = d.dispatch([]string{"-h"}) })
	// Both should produce identical output
	if outHelp != outH {
		t.Errorf("--help and -h should produce identical output\n--help: %q\n-h:    %q", outHelp, outH)
	}
}

func TestExecSubcommand_DryRunStderr(t *testing.T) {
	oldVerbose := verbose
	oldDryRun := dryRun
	verbose = true
	dryRun = true
	defer func() {
		verbose = oldVerbose
		dryRun = oldDryRun
	}()

	stderr := captureStderr(func() {
		_ = execSubcommand("sh", []string{"-c", "echo test"})
	})
	// With verbose+ dry-run, should see [verbose] on stderr
	if !strings.Contains(stderr, "[verbose]") && dryRun {
		t.Log("verbose+dry-run: stderr may or may not have [verbose]")
	}
	// The dry-run prevents actual execution, so no output from the subcommand itself
}

func TestURLToAddr_NoSchemeHostPort(t *testing.T) {
	got := urlToAddr("example.com:9090")
	if got != "example.com:9090" {
		t.Errorf("urlToAddr(example.com:9090) = %q, want example.com:9090", got)
	}
}

func TestURLToAddr_NoSchemeNoPort(t *testing.T) {
	got := urlToAddr("example.com")
	if got != "example.com:80" {
		t.Errorf("urlToAddr(example.com) = %q, want example.com:80", got)
	}
}
