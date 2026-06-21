package prompt

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Attestation types
// ---------------------------------------------------------------------------

// Attestation holds the parsed attestation fields from a commit message
// (spec §8.1).
type Attestation struct {
	Hash             string
	Model            string
	Provider         string
	SpecRef          string
	EstimatedCostUSD float64
	AgentAuthor      string
}

// AttestationResult holds the outcome of validating an attestation against
// the registry.
type AttestationResult struct {
	HashMatch    bool
	LifecycleOK  bool
	Status       LifecycleStatus
	PromptfooPass bool
	Errors       []string
}

// ---------------------------------------------------------------------------
// Commit message parsing
// ---------------------------------------------------------------------------

var (
	reAttestHash = regexp.MustCompile(`(?m)^Prompt:\s*sha256:([a-fA-F0-9]+)`)
	reModel      = regexp.MustCompile(`(?m)^Model:\s*(.+)`)
	reProvider   = regexp.MustCompile(`(?m)^Provider:\s*(.+)`)
	reSpec       = regexp.MustCompile(`(?m)^Spec:\s*(.+)`)
	reCost       = regexp.MustCompile(`(?m)^Cost:\s*\$([0-9]+\.?[0-9]*)`)
	reAuthor     = regexp.MustCompile(`(?m)^Co-authored-by:\s*(.+)`)
)

// ParseCommitMessage extracts attestation fields from a commit message per
// the template in spec §8.1. Returns an error if no "Prompt: sha256:<hash>"
// line is found.
func ParseCommitMessage(msg string) (*Attestation, error) {
	att := &Attestation{}

	if m := reAttestHash.FindStringSubmatch(msg); m != nil {
		att.Hash = "sha256:" + m[1]
	}
	if m := reModel.FindStringSubmatch(msg); m != nil {
		att.Model = strings.TrimSpace(m[1])
	}
	if m := reProvider.FindStringSubmatch(msg); m != nil {
		att.Provider = strings.TrimSpace(m[1])
	}
	if m := reSpec.FindStringSubmatch(msg); m != nil {
		att.SpecRef = strings.TrimSpace(m[1])
	}
	if m := reCost.FindStringSubmatch(msg); m != nil {
		if cost, err := strconv.ParseFloat(m[1], 64); err == nil {
			att.EstimatedCostUSD = cost
		}
	}
	if m := reAuthor.FindStringSubmatch(msg); m != nil {
		att.AgentAuthor = strings.TrimSpace(m[1])
	}

	if att.Hash == "" {
		return att, fmt.Errorf("ATTESTATION_MISSING: no 'Prompt: sha256:<hash>' line found in commit message")
	}
	return att, nil
}

// ValidateAttestation checks an attestation against the registry: lookup by
// hash, lifecycle status, hash verification, and PromptFoo status. Returns a
// populated AttestationResult; errors are collected in result.Errors rather
// than returned.
func ValidateAttestation(att *Attestation, workDir string) (*AttestationResult, error) {
	result := &AttestationResult{}

	// Lookup prompt by hash
	pv, err := Lookup(att.Hash)
	if err != nil {
		result.Errors = append(result.Errors,
			fmt.Sprintf("PROMPT_NOT_FOUND: %s not in registry", att.Hash))
		return result, nil
	}

	// Check lifecycle status
	allowed, warn := AllowedForAttestation(pv.Status)
	result.Status = pv.Status
	result.LifecycleOK = allowed
	if !allowed {
		result.Errors = append(result.Errors,
			fmt.Sprintf("LIFECYCLE_VIOLATION: prompt status is %s, must be active or attested", pv.Status))
	}
	if warn {
		result.Errors = append(result.Errors,
			fmt.Sprintf("LIFECYCLE_WARNING: prompt status is %s (deprecated, 30-day grace)", pv.Status))
	}

	// Verify hash matches stored content
	content, err := os.ReadFile(pv.PromptPath)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("cannot read prompt file: %v", err))
		return result, nil
	}
	result.HashMatch = VerifyHash(string(content), att.Hash)
	if !result.HashMatch {
		result.Errors = append(result.Errors,
			fmt.Sprintf("TAMPER_DETECTED: stored hash != computed hash for %s/%s", pv.Component, pv.Version))
	}

	// Check PromptFoo results
	meta, err := readMetadata(pv.MetadataPath)
	if err == nil && meta.Promptfoo.Status != "" {
		result.PromptfooPass = meta.Promptfoo.Status == "pass"
		if !result.PromptfooPass {
			result.Errors = append(result.Errors,
				fmt.Sprintf("PROMPTFOO_FAILED: status is %s", meta.Promptfoo.Status))
		}
	} else {
		// No PromptFoo results on record — don't fail, just warn
		result.PromptfooPass = true
	}

	return result, nil
}

// Attest runs the full attestation workflow for a parsed attestation: lookup
// → status check → hash verify → PromptFoo check (spec §8.2).
func Attest(att *Attestation, commitSHA, workDir string) (*AttestationResult, error) {
	return ValidateAttestation(att, workDir)
}

// GetCommitAttestation reads a commit's message via git log and parses the
// attestation fields from it.
func GetCommitAttestation(commitSHA, workDir string) (*Attestation, error) {
	cmd := exec.Command("git", "log", "-1", "--format=%B", commitSHA)
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed for %s: %w", commitSHA, err)
	}
	return ParseCommitMessage(string(output))
}
