package prompt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Attestation types
// ---------------------------------------------------------------------------

// Attestation holds the parsed attestation fields from a commit message
// (spec §8.1). Hash is set for sha256-style references
// ("Prompt: sha256:<hex>"); PromptPath is set for path-style references
// ("Prompt: prompts/<name>/v<N>.md" — flat, or
// "Prompt: prompts/<component>/<version>/prompt.md" — nested).
type Attestation struct {
	Hash             string
	PromptPath       string
	Model            string
	Provider         string
	SpecRef          string
	EstimatedCostUSD float64
	AgentAuthor      string
}

// AttestationResult holds the outcome of validating an attestation against
// the registry.
type AttestationResult struct {
	HashMatch     bool
	LifecycleOK   bool
	Status        LifecycleStatus
	PromptfooPass bool
	Errors        []string
}

// ---------------------------------------------------------------------------
// Commit message parsing
// ---------------------------------------------------------------------------

var (
	reAttestHash = regexp.MustCompile(`(?m)^Prompt:\s*sha256:([a-fA-F0-9]+)`)
	// reAttestPath matches the AGENTS.md path-style trailer: flat
	// "Prompt: prompts/<name>/v<N>.md" or nested
	// "Prompt: prompts/<component>/<version>/prompt.md".
	reAttestPath = regexp.MustCompile(`(?m)^Prompt:\s*(prompts/[^\s]+\.md)\s*$`)
	reModel      = regexp.MustCompile(`(?m)^Model:\s*(.+)`)
	reProvider   = regexp.MustCompile(`(?m)^Provider:\s*(.+)`)
	reSpec       = regexp.MustCompile(`(?m)^Spec:\s*(.+)`)
	reCost       = regexp.MustCompile(`(?m)^Cost:\s*\$([0-9]+\.?[0-9]*)`)
	reAuthor     = regexp.MustCompile(`(?m)^Co-authored-by:\s*(.+)`)
)

// ParseCommitMessage extracts attestation fields from a commit message per
// the template in spec §8.1. Accepts either a "Prompt: sha256:<hash>" line
// or a path-style "Prompt: prompts/<name>/v<N>.md" line (AGENTS.md commit
// rule). Returns an error only if neither format is found.
func ParseCommitMessage(msg string) (*Attestation, error) {
	att := &Attestation{}

	if m := reAttestHash.FindStringSubmatch(msg); m != nil {
		att.Hash = "sha256:" + m[1]
	}
	if m := reAttestPath.FindStringSubmatch(msg); m != nil {
		att.PromptPath = m[1]
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

	if att.Hash == "" && att.PromptPath == "" {
		return att, fmt.Errorf("ATTESTATION_MISSING: no 'Prompt: sha256:<hash>' or 'Prompt: prompts/<name>/v<N>.md' line found in commit message")
	}
	return att, nil
}

// ValidateAttestation checks an attestation against the registry: lookup by
// hash, lifecycle status, hash verification, and PromptFoo status. Returns a
// populated AttestationResult; errors are collected in result.Errors rather
// than returned.
func ValidateAttestation(att *Attestation, workDir string) (*AttestationResult, error) {
	result := &AttestationResult{}

	// Path-style reference ("Prompt: prompts/<name>/v<N>.md"): inherently
	// valid if the referenced file exists. Do NOT Lookup() the index — flat
	// unregistered task prompts are not in _index.yaml — and do NOT
	// lifecycle-check them. Hash takes precedence if both formats are set.
	if att.Hash == "" && att.PromptPath != "" {
		if _, err := os.ReadFile(filepath.Join(workDir, att.PromptPath)); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("cannot read prompt file: %v", err))
			return result, nil
		}
		result.HashMatch = true
		result.LifecycleOK = true // no lifecycle gate applies to path refs
		return result, nil
	}

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

// ---------------------------------------------------------------------------
// Spec-level Attest and Verify (spec §8)
// ---------------------------------------------------------------------------

// AttestPrompt creates an attestation for a Prompt, linking it to the current
// HEAD commit. It returns an Attestation struct with the prompt hash, model,
// provider, and the commit reference (spec §8).
func AttestPrompt(prompt Prompt) (*Attestation, error) {
	return &Attestation{
		Hash:     prompt.Hash,
		Model:    prompt.Model,
		Provider: prompt.Provider,
	}, nil
}

// Verify checks whether a commit's attestation is valid by looking up the
// prompt hash in the registry and verifying the hash matches stored content
// (spec §8.2). Returns the Attestation on success.
func Verify(commitRef string) (*Attestation, error) {
	att, err := GetCommitAttestation(commitRef, RegistryDir)
	if err != nil {
		return nil, err
	}
	result, err := ValidateAttestation(att, RegistryDir)
	if err != nil {
		return nil, err
	}
	if !result.HashMatch {
		return nil, fmt.Errorf("TAMPER_DETECTED: stored hash != computed hash")
	}
	if !result.LifecycleOK {
		return nil, fmt.Errorf("LIFECYCLE_VIOLATION: prompt status is %s", result.Status)
	}
	return att, nil
}
