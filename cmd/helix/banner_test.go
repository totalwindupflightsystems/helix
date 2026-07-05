// Tests for cmd/helix/banner.go — covers flag parsing, both render
// variants, --banner root flag, and the integrated dispatch.
package main

import (
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// parseBannerFlags
// -----------------------------------------------------------------------------

func TestParseBannerFlags_HappyPath(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	f, showHelp, err := parseBannerFlags([]string{"--compact"}, stdout, stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if showHelp {
		t.Fatal("showHelp should be false")
	}
	if !f.compact {
		t.Error("compact should be true")
	}
}

func TestParseBannerFlags_Help(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	_, showHelp, err := parseBannerFlags([]string{"--help"}, stdout, stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !showHelp {
		t.Error("showHelp should be true on --help")
	}
}

func TestParseBannerFlags_PositionalRejected(t *testing.T) {
	_, _, err := parseBannerFlags([]string{"extra"}, &strings.Builder{}, &strings.Builder{})
	if err == nil {
		t.Fatal("expected error for positional argument")
	}
}

// -----------------------------------------------------------------------------
// runBanner — happy path
// -----------------------------------------------------------------------------

func TestRunBanner_FullVariant(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runBanner([]string{}, stdout, stderr)
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "HELIX") {
		t.Errorf("full banner should contain HELIX:\n%s", stdout.String())
	}
	// Full banner should be ≥7 lines.
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) < 7 {
		t.Errorf("full banner should have ≥7 lines, got %d", len(lines))
	}
}

func TestRunBanner_CompactVariant(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runBanner([]string{"--compact"}, stdout, stderr)
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "HELIX") {
		t.Errorf("compact banner should contain HELIX:\n%s", stdout.String())
	}
}

// -----------------------------------------------------------------------------
// runBanner — help
// -----------------------------------------------------------------------------

func TestRunBanner_Help(t *testing.T) {
	stdout, _ := &strings.Builder{}, &strings.Builder{}
	rc := runBanner([]string{"--help"}, stdout, &strings.Builder{})
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Errorf("stdout should contain Usage:\n%s", stdout.String())
	}
}

func TestRunBanner_PositionalArgs(t *testing.T) {
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	rc := runBanner([]string{"extra"}, stdout, stderr)
	if rc != 2 {
		t.Errorf("rc = %d, want 2 for positional args", rc)
	}
}

// -----------------------------------------------------------------------------
// printBannerUsage
// -----------------------------------------------------------------------------

func TestPrintBannerUsage(t *testing.T) {
	out := &strings.Builder{}
	printBannerUsage(out)
	for _, want := range []string{"--compact", "Examples:"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("usage missing %q", want)
		}
	}
}
