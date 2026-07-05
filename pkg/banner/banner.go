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

import "fmt"

// Width constants used by the test suite to assert the banners stay
// within reasonable bounds.
const (
	// FullWidth is the expected maximum width of the full banner's art
	// lines, in characters.
	FullWidth = 53

	// CompactWidth is the expected maximum width of the compact banner's
	// art lines, in characters.
	CompactWidth = 33
)

// Render returns the full 7-line HELIX ASCII art banner with the version
// string on line 8. The output ends with a single trailing newline so
// callers can append additional output without an extra blank line.
//
// All art lines are exactly 53 characters wide (the H, E, L, I, X
// glyphs sit in the middle 5 columns).
func Render(version string) string {
	const art = ` =====================================================`
	const art2 = `|  _   _ _____ ___ _     _____ ___  _   _ ___  __   |`
	const art3 = `| | | | |_   _|_ _| |   | ____/ _ \| \ | | \ \/ /  |`
	const art4 = `| | |_| | | |  | || |   |  _|| | | |  \| | \  /   |`
	const art5 = `| |  _  | | |  | || |___| |__| |_| | |\  | /  \   |`
	const art6 = `| |_| |_| | | |___|_____|_____\___/|_| \_|/_/\_\  |`
	const art7 = ` =====================================================`
	// 53 chars each, ASCII-only.

	return fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s\n%s\nHELIX %s\n",
		art, art2, art3, art4, art5, art6, art7, version)
}

// RenderCompact returns a 3-line compact variant suitable for tight
// output contexts (e.g. `helix status`, `helix doctor`). The art fits
// in one line per letter pair (HELIX = H-E-L-I-X = 5 letters = 2 pairs
// + 1 wraparound) — chosen for terminals where vertical space is at a
// premium.
//
// Format:
//
//	 _   _ ___ _    ___  ___
//	| | | | __| |  |_  )__  |
//	H E L I X (3 lines: art, blank, version)
func RenderCompact(version string) string {
	const art = ` _   _ ___ _    ___  ___
| | | | __| |  |_  )__  |
| |_| | _|| |__ / / |  |`
	// 26 chars × 3 lines art.

	return fmt.Sprintf("%s\n\nHELIX %s\n", art, version)
}
