package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/totalwindupflightsystems/helix/pkg/review"
)

// -----------------------------------------------------------------------------
// strip-bias
// -----------------------------------------------------------------------------

func runReviewStripBias(flags revFlags, stdout, stderr io.Writer) int {
	body, err := readReviewInput(flags.inputPath, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return revExitError
	}

	bs := review.NewBiasStripper()
	stripped := bs.StripPreservingPrefix(body)

	if flags.jsonOut {
		// JSON-shape so callers can compare original vs stripped programmatically.
		out := map[string]any{
			"input":    body,
			"stripped": stripped,
		}
		// If input is JSON, try to extract structured fields for the wrapped bundle.
		b, _ := json.MarshalIndent(out, "", "  ")
		writeReviewOutput(flags.outputPath, string(b)+"\n", stdout)
		return revExitOK
	}

	if flags.outputPath != "" {
		writeReviewOutput(flags.outputPath, stripped+"\n", stdout)
		fmt.Fprintf(stdout, "wrote %d bytes to %s\n", len(stripped)+1, flags.outputPath)
		return revExitOK
	}

	fmt.Fprintln(stdout, stripped)
	return revExitOK
}
