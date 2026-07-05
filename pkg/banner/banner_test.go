// Package banner implements ASCII art startup banners for the Helix CLI.
//
// The banners are intentionally ASCII-only (no box-drawing characters) so
// they survive copy-paste into chat / tickets / logs without rendering
// artifacts. The full banner is 7 lines; the compact variant is 3 lines
// for tight output contexts (`helix status`, `helix doctor`, etc.).
//
// Both Render and RenderCompact return the version string on the last
// line so operators immediately know which build they're on.
package banner

import (
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// Render — full 7-line banner
// -----------------------------------------------------------------------------

func TestRender_ContainsHELIX(t *testing.T) {
	got := Render("1.0.0")
	if !strings.Contains(got, "HELIX") {
		t.Errorf("Render output should contain HELIX:\n%s", got)
	}
}

func TestRender_ContainsVersion(t *testing.T) {
	got := Render("2.3.4-test")
	if !strings.Contains(got, "2.3.4-test") {
		t.Errorf("Render output should contain the version string:\n%s", got)
	}
}

func TestRender_AsciiOnly(t *testing.T) {
	got := Render("1.0.0")
	for _, r := range got {
		// Allow letters, digits, space, common punctuation, newlines.
		// Reject anything > 127 (non-ASCII).
		if r > 127 {
			t.Errorf("Render contains non-ASCII character %q (U+%04X)", r, r)
		}
	}
}

func TestRender_MultipleLines(t *testing.T) {
	got := Render("1.0.0")
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	// The full banner has 7 art lines + 1 version line = 8 lines.
	if len(lines) < 7 {
		t.Errorf("Render should produce ≥7 lines, got %d:\n%s", len(lines), got)
	}
}

func TestRender_Deterministic(t *testing.T) {
	// Same version → same output. Important for snapshot tests.
	a := Render("1.0.0")
	b := Render("1.0.0")
	if a != b {
		t.Errorf("Render not deterministic\nfirst:\n%s\nsecond:\n%s", a, b)
	}
}

func TestRender_EmptyVersion(t *testing.T) {
	// Empty version is a degenerate but valid input.
	got := Render("")
	if got == "" {
		t.Error("Render with empty version should still produce art")
	}
}

// -----------------------------------------------------------------------------
// RenderCompact — 3-line variant
// -----------------------------------------------------------------------------

func TestRenderCompact_ContainsHELIX(t *testing.T) {
	got := RenderCompact("1.0.0")
	if !strings.Contains(got, "HELIX") {
		t.Errorf("RenderCompact output should contain HELIX:\n%s", got)
	}
}

func TestRenderCompact_ContainsVersion(t *testing.T) {
	got := RenderCompact("v9.9.9")
	if !strings.Contains(got, "v9.9.9") {
		t.Errorf("RenderCompact should contain version:\n%s", got)
	}
}

func TestRenderCompact_LineCount(t *testing.T) {
	got := RenderCompact("1.0.0")
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	// Compact = 3 art lines + 1 blank + 1 version line = 5 lines total.
	if len(lines) != 5 {
		t.Errorf("RenderCompact should produce 5 lines, got %d:\n%s", len(lines), got)
	}
}

func TestRenderCompact_AsciiOnly(t *testing.T) {
	got := RenderCompact("1.0.0")
	for _, r := range got {
		if r > 127 {
			t.Errorf("RenderCompact contains non-ASCII %q", r)
		}
	}
}

func TestRenderCompact_ShorterThanFull(t *testing.T) {
	full := Render("1.0.0")
	compact := RenderCompact("1.0.0")
	if len(compact) >= len(full) {
		t.Errorf("compact (%d chars) should be shorter than full (%d chars)",
			len(compact), len(full))
	}
}

// -----------------------------------------------------------------------------
// Version constants — defensive
// -----------------------------------------------------------------------------

func TestVersionConstants(t *testing.T) {
	if FullWidth < 40 {
		t.Errorf("FullWidth = %d, expected ≥40 chars for the art", FullWidth)
	}
	if CompactWidth < 20 {
		t.Errorf("CompactWidth = %d, expected ≥20 chars", CompactWidth)
	}
}
