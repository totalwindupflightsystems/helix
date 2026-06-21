package prompt

import (
	"fmt"
	"os"
	"strings"
)

// RunCommitMsgHook implements the 7-step GitReins commit-msg hook from
// spec §8.2:
//  1. Parse commit message for "Prompt: sha256:<hash>" line
//  2. If missing → REJECT
//  3. Look up hash in prompts/_index.yaml
//  4. If not found → REJECT
//  5. Check prompt status (active/attested → PASS, deprecated → WARN,
//     others → REJECT)
//  6. Verify hash matches stored prompt content (mismatch → REJECT: tamper)
//  7. Verify PromptFoo last_run status is "pass" (fail → REJECT)
//
// Prints [PASS]/[WARN]/[FAIL] to stderr. Returns non-nil error on REJECT.
func RunCommitMsgHook(commitMsgFile string) error {
	// Step 1: Parse commit message
	msg, err := ParseCommitMsgFromFile(commitMsgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FAIL] Cannot read commit message file: %v\n", err)
		return fmt.Errorf("cannot read commit message: %w", err)
	}

	att, parseErr := ParseCommitMessage(msg)

	// Step 2: Missing attestation → REJECT
	if parseErr != nil || att.Hash == "" {
		fmt.Fprintf(os.Stderr, "[FAIL] Attestation: missing 'Prompt: sha256:<hash>' in commit message\n")
		fmt.Fprintf(os.Stderr, "\nCommit rejected. To override: git commit --no-verify\n")
		return fmt.Errorf("ATTESTATION_MISSING: commit message must include 'Prompt: sha256:<hash>'")
	}

	// Step 3: Lookup hash in registry
	pv, err := Lookup(att.Hash)
	if err != nil {
		// Step 4: Not found → REJECT
		fmt.Fprintf(os.Stderr, "[FAIL] Prompt hash not in registry: %s\n", att.Hash)
		fmt.Fprintf(os.Stderr, "\nCommit rejected. To override: git commit --no-verify\n")
		return fmt.Errorf("PROMPT_NOT_FOUND: %s not in registry", att.Hash)
	}

	// Step 5: Status check
	allowed, warn := AllowedForAttestation(pv.Status)
	if !allowed {
		fmt.Fprintf(os.Stderr, "[FAIL] Lifecycle: prompt status is %s, must be active or attested\n", pv.Status)
		fmt.Fprintf(os.Stderr, "\nCommit rejected. To override: git commit --no-verify\n")
		return fmt.Errorf("LIFECYCLE_VIOLATION: prompt status is %s, must be active or attested", pv.Status)
	}
	if warn {
		fmt.Fprintf(os.Stderr, "[WARN] Attestation: %s (%s %s, %s) — deprecated, 30-day grace\n",
			shortHash(att.Hash), pv.Component, pv.Version, pv.Status)
	} else {
		fmt.Fprintf(os.Stderr, "[PASS] Attestation: %s (%s %s, %s)\n",
			shortHash(att.Hash), pv.Component, pv.Version, pv.Status)
	}

	// Step 6: Hash verification
	content, err := os.ReadFile(pv.PromptPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FAIL] Cannot read prompt file: %v\n", err)
		return fmt.Errorf("cannot read prompt file: %w", err)
	}
	computed := Hash(string(content))
	if computed != att.Hash {
		fmt.Fprintf(os.Stderr, "[FAIL] Hash mismatch: tamper detected\n")
		fmt.Fprintf(os.Stderr, "  Stored hash:   %s\n", att.Hash)
		fmt.Fprintf(os.Stderr, "  Computed hash: %s\n", computed)
		fmt.Fprintf(os.Stderr, "  File modified after attestation!\n")
		fmt.Fprintf(os.Stderr, "\nVERDICT: TAMPER DETECTED. Prompt content does not match attested hash.\n")
		return fmt.Errorf("TAMPER_DETECTED: stored hash != computed hash for %s/%s", pv.Component, pv.Version)
	}
	fmt.Fprintf(os.Stderr, "[PASS] Hash match: verified\n")

	// Step 7: PromptFoo check
	meta, err := readMetadata(pv.MetadataPath)
	if err == nil && meta.Promptfoo.Status != "" {
		if meta.Promptfoo.Status != "pass" {
			fmt.Fprintf(os.Stderr, "[FAIL] PromptFoo: status is %s\n", meta.Promptfoo.Status)
			return fmt.Errorf("PROMPTFOO_FAILED: status is %s", meta.Promptfoo.Status)
		}
		fmt.Fprintf(os.Stderr, "[PASS] PromptFoo: %s\n", meta.Promptfoo.Status)
	} else {
		fmt.Fprintf(os.Stderr, "[WARN] PromptFoo: no test results on record\n")
	}

	fmt.Fprintf(os.Stderr, "[PASS] All checks passed. Commit allowed.\n")
	return nil
}

// ParseCommitMsgFromFile reads a commit message from a file path (the file
// path passed to the commit-msg hook by git).
func ParseCommitMsgFromFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// shortHash truncates a hash for display purposes.
func shortHash(hash string) string {
	if i := strings.Index(hash, ":"); i >= 0 {
		hash = hash[i+1:]
	}
	if len(hash) > 12 {
		return "sha256:" + hash[:12]
	}
	return "sha256:" + hash
}
