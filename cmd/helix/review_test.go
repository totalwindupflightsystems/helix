// Tests for cmd/helix/review.go.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/review"
)

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// writeReviewFile writes body to a temp file and returns the path.
func writeReviewFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "review-input.txt")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// generateKeyPair writes a 64-byte raw private key and derives the matching 32-byte public key.
func generateKeyPair(t *testing.T) (privPath, pubPath string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	dir := t.TempDir()
	privPath = filepath.Join(dir, "key.priv")
	pubPath = filepath.Join(dir, "key.pub")
	if err := os.WriteFile(privPath, []byte(priv), 0o600); err != nil {
		t.Fatalf("write priv: %v", err)
	}
	if err := os.WriteFile(pubPath, []byte(pub), 0o644); err != nil {
		t.Fatalf("write pub: %v", err)
	}
	return privPath, pubPath
}

// -----------------------------------------------------------------------------
// parseReviewFlags
// -----------------------------------------------------------------------------

func TestParseReviewFlags_DefaultsToHelp(t *testing.T) {
	f, helpWanted, rc := parseReviewFlags([]string{})
	if rc != revExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if f.subcommand != "help" {
		t.Fatalf("subcommand=%q", f.subcommand)
	}
	if helpWanted {
		t.Fatalf("helpWanted should be false for empty args")
	}
}

func TestParseReviewFlags_StripBias(t *testing.T) {
	f, _, rc := parseReviewFlags([]string{"strip-bias", "--input", "/tmp/x.txt", "--json"})
	if rc != revExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if f.subcommand != "strip-bias" || f.inputPath != "/tmp/x.txt" || !f.jsonOut {
		t.Fatalf("flags=%+v", f)
	}
}

func TestParseReviewFlags_EvidenceSignRequiresRoleAndKey(t *testing.T) {
	// Confirm positional `evidence sign` is captured.
	f, _, rc := parseReviewFlags([]string{
		"evidence", "sign", "--input", "x", "--key-role", "primary", "--key-path", "/tmp/k",
	})
	if rc != revExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if f.subcommand != "evidence" || f.evidenceCmd != "sign" {
		t.Fatalf("evidence=%+v", f)
	}
}

func TestParseReviewFlags_EvidenceInvalidPositional(t *testing.T) {
	// "evidence rotate" is not valid — only sign|verify.
	_, _, rc := parseReviewFlags([]string{"evidence", "rotate"})
	if rc != revExitError {
		t.Fatalf("expected rc=2 for invalid evidence subcommand, got %d", rc)
	}
}

func TestParseReviewFlags_InvalidDismissals(t *testing.T) {
	_, _, rc := parseReviewFlags([]string{"fp-record", "--model", "x", "--dismissals", "five"})
	if rc != revExitError {
		t.Fatalf("expected rc=2 for non-integer --dismissals, got %d", rc)
	}
}

func TestParseReviewFlags_UnknownFlag(t *testing.T) {
	_, _, rc := parseReviewFlags([]string{"strip-bias", "--bogus"})
	if rc != revExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

func TestParseReviewFlags_DefaultsDismissalsToOne(t *testing.T) {
	f, _, rc := parseReviewFlags([]string{"fp-record", "--model", "x"})
	if rc != revExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if f.dismissals != 1 {
		t.Fatalf("expected dismissals=1, got %d", f.dismissals)
	}
}

// -----------------------------------------------------------------------------
// runReview subcommand routing
// -----------------------------------------------------------------------------

func TestRunReview_Help(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"help"}, &out, &errBuf)
	if rc != revExitOK {
		t.Fatalf("sign rc=%d err=%q", rc, errBuf.String())
	}
	for _, want := range []string{"helix review", "strip-bias", "fp-stats", "fp-record", "evidence", "custody", "dashboard"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in help: %q", want, out.String())
		}
	}
}

func TestRunReview_UnknownSubcommand(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"bogus"}, &out, &errBuf)
	if rc != revExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

// -----------------------------------------------------------------------------
// strip-bias
// -----------------------------------------------------------------------------

const biasyCommit = `feat(auth): fix JWT validation — all tests pass

I tested locally and it definitely works on my machine.
This is a simple change, just trust me, LGTM ✅
The CI is green so there are no problems.`

func TestRunReview_StripBias_OK(t *testing.T) {
	in := writeReviewFile(t, biasyCommit)
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"strip-bias", "--input", in}, &out, &errBuf)
	if rc != revExitOK {
		t.Fatalf("sign rc=%d err=%q", rc, errBuf.String())
	}
	got := out.String()
	// evaluative language removed
	lowered := strings.ToLower(got)
	for _, banned := range []string{"definitely", "lgtm", "all tests pass", "tested locally", "works on my machine"} {
		if strings.Contains(lowered, banned) {
			t.Fatalf("bias term %q leaked through stripped output:\n%s", banned, got)
		}
	}
	// factual content preserved
	if !strings.Contains(got, "feat(auth)") {
		t.Fatalf("commit prefix lost: %q", got)
	}
	if !strings.Contains(got, "JWT validation") {
		t.Fatalf("subject lost: %q", got)
	}
}

func TestRunReview_StripBias_JSON(t *testing.T) {
	in := writeReviewFile(t, biasyCommit)
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"strip-bias", "--input", in, "--json"}, &out, &errBuf)
	if rc != revExitOK {
		t.Fatalf("sign rc=%d err=%q", rc, errBuf.String())
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON: %v out=%q", err, out.String())
	}
	input, _ := result["input"].(string)
	stripped, _ := result["stripped"].(string)
	if input == "" || stripped == "" {
		t.Fatalf("missing input/stripped: %+v", result)
	}
	if len(stripped) >= len(input) {
		t.Fatalf("expected stripped to be shorter than input")
	}
}

func TestRunReview_StripBias_OutputFile(t *testing.T) {
	in := writeReviewFile(t, biasyCommit)
	outPath := filepath.Join(t.TempDir(), "stripped.txt")
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"strip-bias", "--input", in, "--output", outPath}, &out, &errBuf)
	if rc != revExitOK {
		t.Fatalf("sign rc=%d err=%q", rc, errBuf.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("output file empty")
	}
	if !strings.Contains(out.String(), "wrote") {
		t.Fatalf("expected 'wrote' confirmation, got %q", out.String())
	}
}

func TestRunReview_StripBias_MissingFile(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"strip-bias", "--input", "/no/such/file"}, &out, &errBuf)
	if rc != revExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

// -----------------------------------------------------------------------------
// fp-stats / fp-record
// -----------------------------------------------------------------------------

func TestRunReview_FPRecord_OK(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runReview([]string{
		"fp-record", "--model", "gpt-4",
	}, &out, &errBuf)
	if rc != revExitOK && rc != revExitBlock {
		t.Fatalf("unexpected rc=%d err=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "gpt-4") {
		t.Fatalf("missing model name: %q", out.String())
	}
}

func TestRunReview_FPRecord_JSON(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runReview([]string{
		"fp-record", "--model", "deepseek-v4-pro", "--dismissals", "3", "--total-evals", "100",
		"--json",
	}, &out, &errBuf)
	// Dismissals=3 from a fresh tracker should not flag (threshold is 10).
	if rc != revExitOK {
		t.Fatalf("rc=%d err=%q out=%q", rc, errBuf.String(), out.String())
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON: %v out=%q", err, out.String())
	}
	if dismissals, _ := result["dismissals"].(float64); int(dismissals) != 3 {
		t.Fatalf("dismissals=%v", result["dismissals"])
	}
}

func TestRunReview_FPRecord_FlagsAtThreshold(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runReview([]string{
		"fp-record", "--model", "noisy-model", "--dismissals", "10",
	}, &out, &errBuf)
	if rc != revExitBlock {
		t.Fatalf("expected rc=1 (model flagged), got %d err=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "flagged") {
		t.Fatalf("expected 'flagged' in output: %q", out.String())
	}
}

func TestRunReview_FPRecord_RemovedAtHigherThreshold(t *testing.T) {
	// 20 dismissals + 100 evals → 20% FP rate, which trips the rotation gate.
	var out, errBuf bytes.Buffer
	rc := runReview([]string{
		"fp-record", "--model", "very-noisy",
		"--dismissals", "20", "--total-evals", "100",
	}, &out, &errBuf)
	if rc != revExitBlock {
		t.Fatalf("expected rc=1 (model removed), got %d err=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "removed") {
		t.Fatalf("expected 'removed' in output: %q", out.String())
	}
}

func TestRunReview_FPRecord_MissingModel(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"fp-record"}, &out, &errBuf)
	if rc != revExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

func TestRunReview_FPStats_EmptyTracker(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "empty.json")
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"fp-stats", "--state", statePath}, &out, &errBuf)
	if rc != revExitOK {
		t.Fatalf("sign rc=%d err=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "FPTracker") || !strings.Contains(out.String(), "dismissals") {
		t.Fatalf("expected summary stats, got %q", out.String())
	}
}

func TestRunReview_FPStats_JSON(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	// Pre-populate state file.
	_ = os.WriteFile(statePath, []byte(`{"dismissals":{"gpt-4":5}}`), 0o644)
	// Run fp-record once to make the tracker load + persist the state, so the
	// FPTracker has flagged/removed entries that fp-stats can enumerate.
	var so, se bytes.Buffer
	if rc := runReview([]string{
		"fp-record", "--model", "gpt-4", "--dismissals", "20", "--total-evals", "100",
		"--state", statePath, "--json",
	}, &so, &se); rc == 0 {
		t.Fatalf("expected model to be flagged/removed, got rc=0")
	}

	var out, errBuf bytes.Buffer
	rc := runReview([]string{"fp-stats", "--state", statePath, "--json"}, &out, &errBuf)
	if rc != revExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("JSON: %v out=%q", err, out.String())
	}
	models, _ := report["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("expected 1 model entry, got %d: %+v", len(models), models)
	}
	entry := models[0].(map[string]any)
	if entry["model"] != "gpt-4" {
		t.Fatalf("entry=%+v", entry)
	}
}

func TestRunReview_FPRecord_PersistsState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	// Run multiple fp-record calls so the model is "known" to the tracker
	// (becomes flagged at threshold). Without flagged status, persist won't
	// include it in the union walk.
	var out, errBuf bytes.Buffer
	rc := runReview([]string{
		"fp-record", "--model", "model-x", "--dismissals", "11",
		"--state", statePath,
	}, &out, &errBuf)
	if rc != revExitBlock && rc != revExitOK {
		t.Fatalf("unexpected rc=%d err=%q", rc, errBuf.String())
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("state file not created: %v", err)
	}
	var persisted struct {
		Dismissals map[string]int `json:"dismissals"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if persisted.Dismissals["model-x"] < 11 {
		t.Fatalf("persisted=%+v", persisted)
	}
}

// -----------------------------------------------------------------------------
// evidence sign / verify round-trip
// -----------------------------------------------------------------------------

func TestRunReview_EvidenceSignAndVerify_RoundTrip(t *testing.T) {
	privPath, pubPath := generateKeyPair(t)
	in := writeReviewFile(t, `{"pr_url":"http://example/PR/1","review_id":"r1"}`)
	outPath := filepath.Join(t.TempDir(), "signed.json")

	// Sign.
	var so, se bytes.Buffer
	rc := runReview([]string{
		"evidence", "sign", "--input", in, "--key-role", "primary",
		"--key-path", privPath, "--output", outPath,
	}, &so, &se)
	if rc != revExitOK {
		t.Fatalf("sign rc=%d err=%q", rc, se.String())
	}

	// Verify.
	var vo, ve bytes.Buffer
	rc = runReview([]string{
		"evidence", "verify", "--input", outPath, "--key-role", "primary",
		"--key-path", pubPath,
	}, &vo, &ve)
	if rc != revExitOK {
		t.Fatalf("verify rc=%d err=%q out=%q", rc, ve.String(), vo.String())
	}
	var result map[string]any
	if err := json.Unmarshal(vo.Bytes(), &result); err != nil {
		t.Fatalf("JSON: %v out=%q", err, vo.String())
	}
	if valid, _ := result["valid"].(bool); !valid {
		t.Fatalf("expected valid=true, got %+v", result)
	}
}

func TestRunReview_EvidenceVerify_WrongKey(t *testing.T) {
	// Sign with one key, verify with a different public key.
	privPath, _ := generateKeyPair(t)
	_, wrongPub := generateKeyPair(t)

	in := writeReviewFile(t, `{"pr_url":"x", "review_id":"y"}`)
	outPath := filepath.Join(t.TempDir(), "signed.json")
	var so, se bytes.Buffer
	if rc := runReview([]string{
		"evidence", "sign", "--input", in, "--key-role", "primary",
		"--key-path", privPath, "--output", outPath,
	}, &so, &se); rc != revExitOK {
		t.Fatalf("sign rc=%d err=%q", rc, se.String())
	}
	var vo, ve bytes.Buffer
	rc := runReview([]string{
		"evidence", "verify", "--input", outPath, "--key-role", "primary",
		"--key-path", wrongPub,
	}, &vo, &ve)
	if rc != revExitBlock {
		t.Fatalf("expected rc=1 for invalid signature, got %d", rc)
	}
}

func TestRunReview_EvidenceSign_MissingFlags(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"evidence", "sign"}, &out, &errBuf)
	if rc != revExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

func TestRunReview_EvidenceVerify_MissingFlags(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"evidence", "verify"}, &out, &errBuf)
	if rc != revExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

func TestRunReview_EvidenceSign_InvalidRole(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"evidence", "sign", "--input", "x", "--key-role", "judge", "--key-path", "/no/path"},
		&out, &errBuf)
	if rc != revExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
	if !strings.Contains(errBuf.String(), "primary|adversarial|audit") {
		t.Fatalf("expected role list in error, got %q", errBuf.String())
	}
}

func TestRunReview_EvidenceSign_NoRoleDefaultsToPrimary(t *testing.T) {
	privPath, pubPath := generateKeyPair(t)
	in := writeReviewFile(t, `{"pr_url":"x"}`)
	outPath := filepath.Join(t.TempDir(), "signed.json")
	var so, se bytes.Buffer
	// Pass no --key-role; the CLI should default to "primary".
	rc := runReview([]string{
		"evidence", "sign", "--input", in,
		"--key-path", privPath, "--output", outPath,
	}, &so, &se)
	if rc != revExitOK {
		t.Fatalf("sign rc=%d err=%q", rc, se.String())
	}
	var vo, ve bytes.Buffer
	rc = runReview([]string{
		"evidence", "verify", "--input", outPath, "--key-role", "primary", "--key-path", pubPath,
	}, &vo, &ve)
	if rc != revExitOK {
		t.Fatalf("sign rc=%d err=%q", rc, se.String())
	}
}

// -----------------------------------------------------------------------------
// custody
// -----------------------------------------------------------------------------

func TestRunReview_Custody_OK(t *testing.T) {
	privPath, pubPath := generateKeyPair(t)
	// Build a signed bundle then custody-analyze it.
	in := writeReviewFile(t, `{"pr_url":"x", "review_id":"y"}`)
	outPath := filepath.Join(t.TempDir(), "signed.json")
	var so, se bytes.Buffer
	if rc := runReview([]string{
		"evidence", "sign", "--input", in, "--key-role", "primary",
		"--key-path", privPath, "--output", outPath,
	}, &so, &se); rc != revExitOK {
		t.Fatalf("sign rc=%d err=%q", rc, se.String())
	}
	_ = pubPath

	var co, ce bytes.Buffer
	rc := runReview([]string{"custody", "--input", outPath}, &co, &ce)
	if rc != revExitOK {
		t.Fatalf("sign rc=%d err=%q", rc, se.String())
	}
	if !strings.Contains(co.String(), "Custody summary") {
		t.Fatalf("expected summary, got %q", co.String())
	}
	if !strings.Contains(co.String(), "Signed: true") {
		t.Fatalf("expected Signed: true, got %q", co.String())
	}
}

func TestRunReview_Custody_JSON(t *testing.T) {
	in := writeReviewFile(t, `{"pr_url":"local://1","review_id":"r1"}`)
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"custody", "--input", in, "--json"}, &out, &errBuf)
	if rc != revExitOK {
		t.Fatalf("sign rc=%d err=%q", rc, errBuf.String())
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("JSON: %v out=%q", err, out.String())
	}
	if report["pr_url"] != "local://1" {
		t.Fatalf("pr_url=%v", report["pr_url"])
	}
	if _, ok := report["signed"]; !ok {
		t.Fatalf("missing 'signed' field: %+v", report)
	}
}

func TestRunReview_Custody_MissingInput(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"custody"}, &out, &errBuf)
	if rc != revExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

func TestRunReview_Custody_InvalidJSON(t *testing.T) {
	in := writeReviewFile(t, `{bogus`)
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"custody", "--input", in}, &out, &errBuf)
	if rc != revExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

// -----------------------------------------------------------------------------
// runReviewWithDryRun plumbing
// -----------------------------------------------------------------------------

func TestRunReviewWithDryRun_VerifyFailureWrappedCorrectly(t *testing.T) {
	privPath, _ := generateKeyPair(t)
	_, wrongPub := generateKeyPair(t)
	in := writeReviewFile(t, `{"pr_url":"x"}`)
	signedPath := filepath.Join(t.TempDir(), "s.json")
	var so, se bytes.Buffer
	if rc := runReview([]string{
		"evidence", "sign", "--input", in, "--key-role", "primary",
		"--key-path", privPath, "--output", signedPath,
	}, &so, &se); rc != revExitOK {
		t.Fatalf("sign rc=%d", rc)
	}

	var vo, ve bytes.Buffer
	err := runReviewWithDryRun([]string{
		"evidence", "verify", "--input", signedPath, "--key-role", "primary",
		"--key-path", wrongPub,
	}, &vo, &ve, false)
	// revExitBlock (1) is the "expected failure" code and MUST NOT be wrapped.
	if err != nil {
		t.Fatalf("revExitBlock should NOT be wrapped, got %v", err)
	}
}

func TestRunReviewWithDryRun_InvocationErrorWrapped(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := runReviewWithDryRun([]string{"bogus"}, &out, &errBuf, false)
	if err == nil {
		t.Fatal("expected errExit wrapping")
	}
	if !strings.Contains(err.Error(), "exit") {
		t.Fatalf("expected exit in error, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// Helpers (signatureCount, isValidSignerRole)
// -----------------------------------------------------------------------------

func TestSignatureCount_AllEmpty(t *testing.T) {
	b := review.EvidenceBundle{}
	if got := signatureCount(b); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestSignatureCount_OneSet(t *testing.T) {
	b := review.EvidenceBundle{
		Signatures: review.Signatures{Primary: "abc"},
	}
	if got := signatureCount(b); got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
}

func TestSignatureCount_AllSet(t *testing.T) {
	b := review.EvidenceBundle{
		Signatures: review.Signatures{Primary: "p", Adversarial: "a", Audit: "au"},
	}
	if got := signatureCount(b); got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
}

func TestIsValidSignerRole(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"primary", true},
		{"adversarial", true},
		{"audit", true},
		{"reviewer", false},
		{"judge", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isValidSignerRole(c.in); got != c.want {
			t.Fatalf("isValidSignerRole(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

// -----------------------------------------------------------------------------
// Key reader error paths
// -----------------------------------------------------------------------------

func TestReadPrivateKeyFile_BadLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.key")
	_ = os.WriteFile(path, []byte("short"), 0o600)
	_, err := readPrivateKeyFile(path)
	if err == nil {
		t.Fatal("expected error for short key file")
	}
}

func TestReadPublicKeyFile_BadLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.pub")
	_ = os.WriteFile(path, []byte("xx"), 0o644)
	_, err := readPublicKeyFile(path)
	if err == nil {
		t.Fatal("expected error for short pub key file")
	}
}

func TestReadPrivateKeyFile_OK(t *testing.T) {
	privPath, _ := generateKeyPair(t)
	priv, err := readPrivateKeyFile(privPath)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Fatalf("expected %d bytes, got %d", ed25519.PrivateKeySize, len(priv))
	}
}

func TestReadPublicKeyFile_OK(t *testing.T) {
	_, pubPath := generateKeyPair(t)
	pub, err := readPublicKeyFile(pubPath)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		t.Fatalf("expected %d bytes, got %d", ed25519.PublicKeySize, len(pub))
	}
}

func TestParseIntInRange(t *testing.T) {
	if _, err := parseIntInRange("5", 1, 10); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, err := parseIntInRange("11", 1, 10); err == nil {
		t.Fatal("expected range error")
	}
	if _, err := parseIntInRange("abc", 1, 10); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestRunReview_Dashboard_TextAndJSON(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runReview([]string{
		"dashboard",
		"--pr", "42",
		"--files", "pkg/trust/ledger.go,pkg/review/dashboard.go",
		"--tier", "observed",
		"--category", "contract",
		"--agent", "agent-alpha",
	}, &out, &errBuf)
	if rc != revExitOK {
		t.Fatalf("dashboard rc=%d err=%q out=%q", rc, errBuf.String(), out.String())
	}
	text := out.String()
	for _, want := range []string{"Change Management Dashboard", "Risk Assessment", "Blast Radius", "Architectural Fit", "Trust Context"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in dashboard output:\n%s", want, text)
		}
	}

	out.Reset()
	errBuf.Reset()
	rc = runReview([]string{
		"dashboard",
		"--pr", "42",
		"--files", "pkg/auth/session.go",
		"--tier", "provisional",
		"--json",
	}, &out, &errBuf)
	if rc != revExitOK {
		t.Fatalf("dashboard json rc=%d err=%q", rc, errBuf.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if payload["pr"] != "42" {
		t.Errorf("pr = %v", payload["pr"])
	}
	risk, ok := payload["risk"].(map[string]any)
	if !ok {
		t.Fatalf("risk missing: %v", payload)
	}
	if risk["level"] == nil {
		t.Errorf("risk.level missing: %v", risk)
	}
}

func TestRunReview_Dashboard_MissingFlags(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runReview([]string{"dashboard"}, &out, &errBuf)
	if rc != revExitError {
		t.Fatalf("expected error without --pr, got %d", rc)
	}
	rc = runReview([]string{"dashboard", "--pr", "1"}, &out, &errBuf)
	if rc != revExitError {
		t.Fatalf("expected error without --files, got %d", rc)
	}
}
