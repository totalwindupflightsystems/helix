package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Attestation Validator — specs/prompt-registry.md §Attestation
//
// Validates that every commit in a PR has a valid prompt attestation link.
// Supports both attestation formats:
//   - Path format:  Prompt: prompts/<name>/v<N>.md  (AGENTS.md convention)
//   - Hash format:  Prompt: sha256:<hex>            (spec §8.1 format)
//
// ValidateCommitMessage checks the trailer format.
// VerifyPromptExists confirms the referenced prompt file exists in the registry.
// VerifyHashMatch confirms the prompt file's hash matches the attested hash.
// ValidatePR scans all commits in a PR and returns an AttestationReport.
// ---------------------------------------------------------------------------

// CommitAttestationStatus is the per-commit validation outcome.
type CommitAttestationStatus string

const (
	// StatusValid: attestation found, prompt exists, hash matches.
	CommitAttestValid CommitAttestationStatus = "VALID"
	// CommitAttestMissing: no Prompt: trailer found in commit message.
	CommitAttestMissing CommitAttestationStatus = "MISSING"
	// CommitAttestMalformed: Prompt: trailer exists but format is invalid.
	CommitAttestMalformed CommitAttestationStatus = "MALFORMED"
	// CommitAttestFileNotFound: referenced prompt file does not exist.
	CommitAttestFileNotFound CommitAttestationStatus = "FILE_NOT_FOUND"
	// CommitAttestHashMismatch: prompt file exists but hash doesn't match.
	CommitAttestHashMismatch CommitAttestationStatus = "HASH_MISMATCH"
)

// CommitAttestationResult holds the validation result for a single commit.
type CommitAttestationResult struct {
	CommitSHA    string                  `json:"commit_sha"`
	Status       CommitAttestationStatus `json:"status"`
	PromptRef    string                  `json:"prompt_ref,omitempty"`    // the parsed prompt reference
	PromptPath   string                  `json:"prompt_path,omitempty"`   // resolved file path
	ComputedHash string                  `json:"computed_hash,omitempty"` // hash of the prompt file content
	ExpectedHash string                  `json:"expected_hash,omitempty"` // hash from attestation (if hash format)
	Detail       string                  `json:"detail,omitempty"`
}

// AttestationReport aggregates per-commit results for an entire PR.
type AttestationReport struct {
	PRRef        string                    `json:"pr_ref,omitempty"`
	TotalCommits int                       `json:"total_commits"`
	Results      []CommitAttestationResult `json:"results"`
	AllValid     bool                      `json:"all_valid"`
}

// HasInvalid returns true if any commit has a non-VALID status.
func (r *AttestationReport) HasInvalid() bool {
	for _, res := range r.Results {
		if res.Status != CommitAttestValid {
			return true
		}
	}
	return false
}

// InvalidCount returns the number of commits with non-VALID status.
func (r *AttestationReport) InvalidCount() int {
	count := 0
	for _, res := range r.Results {
		if res.Status != CommitAttestValid {
			count++
		}
	}
	return count
}

// --- Regex patterns for attestation trailer ---

var (
	// rePromptPath matches "Prompt: prompts/<component>/<version>/prompt.md" or
	// "Prompt: prompts/<component>/v<N>.md" or "Prompt: prompts/<name>/v<N>.md"
	rePromptPath = regexp.MustCompile(`(?m)^Prompt:\s*(prompts/[^\s]+\.md)\s*$`)

	// rePromptHash matches "Prompt: sha256:<hex>" (existing format from attester.go)
	rePromptHash = regexp.MustCompile(`(?m)^Prompt:\s*(sha256:[a-fA-F0-9]+)\s*$`)

	// rePromptGeneric matches any "Prompt: <something>" for detecting malformed entries
	rePromptGeneric = regexp.MustCompile(`(?m)^Prompt:\s*(.+)\s*$`)
)

// AttestationValidator validates prompt attestations across commits.
type AttestationValidator struct {
	// RegistryRoot is the root directory for prompt storage (default: "prompts").
	// Override for testing with t.TempDir().
	RegistryRoot string
}

// NewAttestationValidator creates a validator with the default registry root.
func NewAttestationValidator() *AttestationValidator {
	return &AttestationValidator{
		RegistryRoot: RegistryDir,
	}
}

// ValidateCommitMessage checks the commit message for a valid Prompt: trailer.
// Supports both path format and hash format.
// Returns the parsed result with status VALID/MISSING/MALFORMED.
// Does NOT check file existence or hash — use ValidateCommit for full validation.
func (av *AttestationValidator) ValidateCommitMessage(commitSHA, message string) CommitAttestationResult {
	result := CommitAttestationResult{
		CommitSHA: commitSHA,
	}

	// Try path format first: "Prompt: prompts/<name>/v<N>.md"
	if m := rePromptPath.FindStringSubmatch(message); m != nil {
		result.PromptRef = m[1]
		result.PromptPath = filepath.Join(av.RegistryRoot, strings.TrimPrefix(m[1], "prompts/"))
		result.Status = CommitAttestValid
		result.Detail = "prompt path reference found"
		return result
	}

	// Try hash format: "Prompt: sha256:<hex>"
	if m := rePromptHash.FindStringSubmatch(message); m != nil {
		result.PromptRef = m[1]
		result.ExpectedHash = m[1]
		result.Status = CommitAttestValid
		result.Detail = "prompt hash reference found"
		return result
	}

	// Check if there's any "Prompt:" line at all → malformed
	if m := rePromptGeneric.FindStringSubmatch(message); m != nil {
		result.PromptRef = m[1]
		result.Status = CommitAttestMalformed
		result.Detail = fmt.Sprintf("Prompt: trailer exists but format is invalid (expected prompts/<name>/v<N>.md or sha256:<hex>), got: %s", m[1])
		return result
	}

	// No Prompt: trailer at all
	result.Status = CommitAttestMissing
	result.Detail = "no 'Prompt:' trailer found in commit message"
	return result
}

// VerifyPromptExists checks whether the referenced prompt file exists on disk.
// For path-format references, checks the file at PromptPath.
// For hash-format references, looks up the prompt via the registry's Lookup function.
// Updates the result status to FILE_NOT_FOUND if the file doesn't exist.
func (av *AttestationValidator) VerifyPromptExists(result *CommitAttestationResult) {
	if result.Status != CommitAttestValid {
		return // Only verify for messages that parsed successfully
	}

	// Path format: check file directly
	if result.PromptPath != "" {
		if _, err := os.Stat(result.PromptPath); os.IsNotExist(err) {
			result.Status = CommitAttestFileNotFound
			result.Detail = fmt.Sprintf("prompt file not found: %s", result.PromptPath)
			return
		} else if err != nil {
			result.Status = CommitAttestFileNotFound
			result.Detail = fmt.Sprintf("error checking prompt file: %v", err)
			return
		}
		// File exists — compute hash for verification
		content, err := os.ReadFile(result.PromptPath)
		if err != nil {
			result.Status = CommitAttestFileNotFound
			result.Detail = fmt.Sprintf("cannot read prompt file: %v", err)
			return
		}
		result.ComputedHash = Hash(string(content))
		return
	}

	// Hash format: use registry lookup
	if result.ExpectedHash != "" {
		pv, err := Lookup(result.ExpectedHash)
		if err != nil {
			result.Status = CommitAttestFileNotFound
			result.Detail = fmt.Sprintf("prompt hash not found in registry: %s", result.ExpectedHash)
			return
		}
		result.PromptPath = pv.PromptPath
		// Read and hash the content
		content, err := os.ReadFile(pv.PromptPath)
		if err != nil {
			result.Status = CommitAttestFileNotFound
			result.Detail = fmt.Sprintf("cannot read prompt file at %s: %v", pv.PromptPath, err)
			return
		}
		result.ComputedHash = Hash(string(content))
		return
	}
}

// VerifyHashMatch checks whether the computed hash matches the expected hash.
// For path-format references where no ExpectedHash is set, this is a no-op
// (path references are inherently valid if the file exists).
// For hash-format references, the computed hash must match the expected hash.
// Updates the result status to HASH_MISMATCH if they differ.
func (av *AttestationValidator) VerifyHashMatch(result *CommitAttestationResult) {
	if result.Status != CommitAttestValid {
		return
	}

	// Only check hash match for hash-format references
	if result.ExpectedHash == "" {
		return // path format — no hash to verify against
	}

	if result.ComputedHash != result.ExpectedHash {
		result.Status = CommitAttestHashMismatch
		result.Detail = fmt.Sprintf("hash mismatch: expected %s, computed %s", result.ExpectedHash, result.ComputedHash)
	}
}

// ValidateCommit performs full validation of a single commit:
// 1. Parse and validate the Prompt: trailer format
// 2. Verify the referenced prompt file exists
// 3. Verify the hash matches (for hash-format references)
func (av *AttestationValidator) ValidateCommit(commitSHA, message string) CommitAttestationResult {
	result := av.ValidateCommitMessage(commitSHA, message)
	av.VerifyPromptExists(&result)
	av.VerifyHashMatch(&result)
	return result
}

// ValidateCommits validates multiple commit messages and returns an aggregated report.
// Each entry in messages is (commitSHA, message).
func (av *AttestationValidator) ValidateCommits(prRef string, messages []struct{ SHA, Message string }) *AttestationReport {
	report := &AttestationReport{
		PRRef:        prRef,
		TotalCommits: len(messages),
		Results:      make([]CommitAttestationResult, 0, len(messages)),
	}

	for _, msg := range messages {
		result := av.ValidateCommit(msg.SHA, msg.Message)
		report.Results = append(report.Results, result)
	}

	report.AllValid = !report.HasInvalid()
	return report
}

// ShouldBlockMerge returns true if the attestation report contains any
// commits that should block a merge (MISSING, MALFORMED, FILE_NOT_FOUND, HASH_MISMATCH).
func (r *AttestationReport) ShouldBlockMerge() bool {
	return r.HasInvalid()
}

// Summary returns a human-readable summary of the report.
func (r *AttestationReport) Summary() string {
	if r.AllValid {
		return fmt.Sprintf("all %d commits have valid prompt attestations", r.TotalCommits)
	}
	return fmt.Sprintf("%d/%d commits have invalid prompt attestations", r.InvalidCount(), r.TotalCommits)
}

// --- Convenience functions (no receiver, for simple use) ---

// HasPromptTrailer checks whether a commit message contains any "Prompt:" line.
func HasPromptTrailer(message string) bool {
	return rePromptGeneric.MatchString(message)
}

// HasValidPromptTrailer checks whether a commit message contains a properly
// formatted Prompt: trailer (either path or hash format).
func HasValidPromptTrailer(message string) bool {
	return rePromptPath.MatchString(message) || rePromptHash.MatchString(message)
}

// ExtractPromptRef extracts the prompt reference from a commit message.
// Returns empty string if no valid Prompt: trailer is found.
func ExtractPromptRef(message string) string {
	if m := rePromptPath.FindStringSubmatch(message); m != nil {
		return m[1]
	}
	if m := rePromptHash.FindStringSubmatch(message); m != nil {
		return m[1]
	}
	return ""
}

// IsPathFormat returns true if the reference is in prompts/<name>/v<N>.md format.
func IsPathFormat(ref string) bool {
	return strings.HasPrefix(ref, "prompts/")
}

// IsHashFormat returns true if the reference is in sha256:<hex> format.
func IsHashFormat(ref string) bool {
	return strings.HasPrefix(ref, "sha256:")
}
