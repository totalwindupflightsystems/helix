package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/totalwindupflightsystems/helix/pkg/review"
)

// -----------------------------------------------------------------------------
// evidence sign / verify
// -----------------------------------------------------------------------------

func runReviewEvidence(flags revFlags, stdout, stderr io.Writer) int {
	if flags.evidenceCmd == "" {
		fmt.Fprintln(stderr, "error: evidence requires a subcommand: sign | verify")
		return revExitError
	}
	switch flags.evidenceCmd {
	case "sign":
		return runReviewEvidenceSign(flags, stdout, stderr)
	case "verify":
		return runReviewEvidenceVerify(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown evidence subcommand %q\n", flags.evidenceCmd)
		return revExitError
	}
}

func runReviewEvidenceSign(flags revFlags, stdout, stderr io.Writer) int {
	if flags.inputPath == "" || flags.keyPath == "" {
		fmt.Fprintln(stderr, "error: evidence sign requires --input, --key-path")
		return revExitError
	}
	// Default keyRole to "primary" if empty (signer role inferred from flag)
	// BEFORE validation, otherwise the validation rejects empty role.
	if flags.keyRole == "" {
		flags.keyRole = "primary"
	}
	if !isValidSignerRole(flags.keyRole) {
		fmt.Fprintf(stderr, "error: --key-role must be one of primary|adversarial|audit (got %q)\n", flags.keyRole)
		return revExitError
	}
	priv, err := readPrivateKeyFile(flags.keyPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: read key: %v\n", err)
		return revExitError
	}

	inputBytes, err := readInputFile(flags.inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: read input: %v\n", err)
		return revExitError
	}

	// Accept either a plain string OR a JSON-shaped EvidenceBundle.
	var bundle *review.EvidenceBundle
	if json.Valid(inputBytes) {
		var existing review.EvidenceBundle
		if err := json.Unmarshal(inputBytes, &existing); err == nil && existing.PRURL != "" {
			bundle = &existing
		}
	}
	if bundle == nil {
		// Build a minimal bundle from the raw text.
		formation := review.Formation{
			Primary:     review.ModelInfo{Model: "primary", Provider: "local"},
			Adversarial: review.ModelInfo{Model: "adversarial", Provider: "local"},
			Audit:       review.ModelInfo{Model: "audit", Provider: "local"},
		}
		bundle = review.NewEvidenceBundle("local://input", "review-"+tsSuffix(), formation, "", "")
	}
	sig, err := bundle.SignBundle(flags.keyRole, priv)
	if err != nil {
		fmt.Fprintf(stderr, "error: sign: %v\n", err)
		return revExitError
	}

	// Write the bundle itself (with the embedded signature) so verify can
	// re-read it as a full EvidenceBundle. The CLI wraps extra metadata in
	// the stdout summary but always persists the canonical bundle JSON to
	// --output (or stdout if --output is empty).
	bundleJSON, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: marshal bundle: %v\n", err)
		return revExitError
	}
	bundleJSON = append(bundleJSON, '\n')
	if flags.outputPath != "" {
		writeReviewOutput(flags.outputPath, string(bundleJSON), stdout)
		fmt.Fprintf(stdout, "Signed bundle written to %s (role=%s, sig=%s...)\n", flags.outputPath, flags.keyRole, sig[:min(16, len(sig))])
	} else {
		fmt.Fprintln(stdout, string(bundleJSON))
	}
	return revExitOK
}

func runReviewEvidenceVerify(flags revFlags, stdout, stderr io.Writer) int {
	if flags.inputPath == "" || flags.keyPath == "" {
		fmt.Fprintln(stderr, "error: evidence verify requires --input, --key-path")
		return revExitError
	}
	if flags.keyRole == "" {
		flags.keyRole = "primary"
	}
	if !isValidSignerRole(flags.keyRole) {
		fmt.Fprintf(stderr, "error: --key-role must be one of primary|adversarial|audit (got %q)\n", flags.keyRole)
		return revExitError
	}
	pub, err := readPublicKeyFile(flags.keyPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: read key: %v\n", err)
		return revExitError
	}
	inputBytes, err := readInputFile(flags.inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: read input: %v\n", err)
		return revExitError
	}
	var bundle review.EvidenceBundle
	if err := json.Unmarshal(inputBytes, &bundle); err != nil {
		fmt.Fprintf(stderr, "error: parse bundle: %v\n", err)
		return revExitError
	}
	ok, err := bundle.VerifySignature(flags.keyRole, pub)
	if err != nil {
		fmt.Fprintf(stderr, "error: verify: %v\n", err)
		return revExitError
	}

	out := map[string]any{
		"pr_url": bundle.PRURL,
		"role":   flags.keyRole,
		"valid":  ok,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Fprintln(stdout, string(b))
	if !ok {
		return revExitBlock
	}
	return revExitOK
}

// -----------------------------------------------------------------------------
// custody
// -----------------------------------------------------------------------------

func runReviewCustody(flags revFlags, stdout, stderr io.Writer) int {
	if flags.inputPath == "" {
		fmt.Fprintln(stderr, "error: custody requires --input")
		return revExitError
	}
	inputBytes, err := readInputFile(flags.inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: read input: %v\n", err)
		return revExitError
	}
	var bundle review.EvidenceBundle
	if err := json.Unmarshal(inputBytes, &bundle); err != nil {
		fmt.Fprintf(stderr, "error: parse bundle: %v\n", err)
		return revExitError
	}

	coc := review.NewChainOfCustody(&bundle)
	sigCount := signatureCount(bundle)
	if flags.jsonOut {
		type sealedFor struct {
			PRURL         string `json:"pr_url"`
			ReviewID      string `json:"review_id"`
			Signed        bool   `json:"signed"`
			Signatures    int    `json:"signatures"`
			CustodySealed bool   `json:"custody_sealed"`
		}
		out := sealedFor{
			PRURL:         bundle.PRURL,
			ReviewID:      bundle.ReviewID,
			Signed:        sigCount > 0,
			Signatures:    sigCount,
			CustodySealed: coc != nil,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return revExitOK
	}

	fmt.Fprintf(stdout, "Custody summary for PR %q (review %q)\n", bundle.PRURL, bundle.ReviewID)
	fmt.Fprintf(stdout, "  Signed: %v (signatures=%d)\n", sigCount > 0, sigCount)
	if coc != nil {
		fmt.Fprintf(stdout, "  Custody chain-of-custody sealed: true")
	} else {
		fmt.Fprintf(stdout, "  Custody chain-of-custody sealed: false")
	}
	return revExitOK
}
