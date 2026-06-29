package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

// mustMkdirAll wraps os.MkdirAll with t.Fatal for test setup.
func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
}

// mustWriteFile wraps os.WriteFile with t.Fatal for test setup.
func mustWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
}

// --- ValidateCommitMessage tests ---

func TestValidateCommitMessage_PathFormat(t *testing.T) {
	av := NewAttestationValidator()

	msg := `feat: add rate limiter

Implement rate limiting on /login endpoint per spec §3.

Co-authored-by: wojons <wojonstech@gmail.com>
Prompt: prompts/coding-hermes/v1.md`

	result := av.ValidateCommitMessage("abc123", msg)

	if result.Status != CommitAttestValid {
		t.Errorf("Status = %s, want VALID", result.Status)
	}
	if result.PromptRef != "prompts/coding-hermes/v1.md" {
		t.Errorf("PromptRef = %q, want prompts/coding-hermes/v1.md", result.PromptRef)
	}
}

func TestValidateCommitMessage_HashFormat(t *testing.T) {
	av := NewAttestationValidator()

	msg := `feat: add rate limiter

Co-authored-by: wojons <wojonstech@gmail.com>
Prompt: sha256:a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456`

	result := av.ValidateCommitMessage("abc123", msg)

	if result.Status != CommitAttestValid {
		t.Errorf("Status = %s, want VALID", result.Status)
	}
	if result.ExpectedHash != "sha256:a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456" {
		t.Errorf("ExpectedHash = %q", result.ExpectedHash)
	}
}

func TestValidateCommitMessage_MissingPrompt(t *testing.T) {
	av := NewAttestationValidator()

	msg := `feat: add rate limiter

Co-authored-by: wojons <wojonstech@gmail.com>`

	result := av.ValidateCommitMessage("abc123", msg)

	if result.Status != CommitAttestMissing {
		t.Errorf("Status = %s, want MISSING", result.Status)
	}
}

func TestValidateCommitMessage_Malformed(t *testing.T) {
	av := NewAttestationValidator()

	msg := `feat: add stuff

Prompt: some random text without proper format`

	result := av.ValidateCommitMessage("abc123", msg)

	if result.Status != CommitAttestMalformed {
		t.Errorf("Status = %s, want MALFORMED", result.Status)
	}
}

func TestValidateCommitMessage_EmptyMessage(t *testing.T) {
	av := NewAttestationValidator()

	result := av.ValidateCommitMessage("abc123", "")

	if result.Status != CommitAttestMissing {
		t.Errorf("Status = %s, want MISSING", result.Status)
	}
}

func TestValidateCommitMessage_PathWithSubdirs(t *testing.T) {
	av := NewAttestationValidator()

	msg := `feat: test

Prompt: prompts/agent-identity/v1.0.0/prompt.md`

	result := av.ValidateCommitMessage("abc123", msg)

	if result.Status != CommitAttestValid {
		t.Errorf("Status = %s, want VALID", result.Status)
	}
	if result.PromptRef != "prompts/agent-identity/v1.0.0/prompt.md" {
		t.Errorf("PromptRef = %q", result.PromptRef)
	}
}

func TestValidateCommitMessage_PromptWithSpaces(t *testing.T) {
	av := NewAttestationValidator()

	// "Prompt:  prompts/..." with extra space should still match
	msg := "feat: test\n\nPrompt:  prompts/auth/v1.md"

	result := av.ValidateCommitMessage("abc123", msg)

	if result.Status != CommitAttestValid {
		t.Errorf("Status = %s, want VALID", result.Status)
	}
}

// --- VerifyPromptExists tests ---

func TestVerifyPromptExists_FileExists(t *testing.T) {
	tmpDir := t.TempDir()
	promptDir := filepath.Join(tmpDir, "auth", "v1")
	mustMkdirAll(t, promptDir)
	mustWriteFile(t, filepath.Join(promptDir, "prompt.md"), []byte("# Auth Prompt\n\nDo the thing."))

	av := &AttestationValidator{RegistryRoot: tmpDir}

	result := CommitAttestationResult{
		Status:     CommitAttestValid,
		PromptPath: filepath.Join(tmpDir, "auth", "v1", "prompt.md"),
	}

	av.VerifyPromptExists(&result)

	if result.Status != CommitAttestValid {
		t.Errorf("Status = %s, want VALID (file exists)", result.Status)
	}
	if result.ComputedHash == "" {
		t.Error("ComputedHash should be set after successful file read")
	}
}

func TestVerifyPromptExists_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	av := &AttestationValidator{RegistryRoot: tmpDir}

	result := CommitAttestationResult{
		Status:     CommitAttestValid,
		PromptPath: filepath.Join(tmpDir, "nonexistent", "v1", "prompt.md"),
	}

	av.VerifyPromptExists(&result)

	if result.Status != CommitAttestFileNotFound {
		t.Errorf("Status = %s, want FILE_NOT_FOUND", result.Status)
	}
}

func TestVerifyPromptExists_AlreadyInvalid(t *testing.T) {
	av := NewAttestationValidator()

	result := CommitAttestationResult{
		Status: CommitAttestMissing,
	}

	av.VerifyPromptExists(&result)

	// Should be a no-op for non-VALID results
	if result.Status != CommitAttestMissing {
		t.Errorf("Status = %s, should remain MISSING", result.Status)
	}
}

// --- VerifyHashMatch tests ---

func TestVerifyHashMatch_PathFormat_NoCheck(t *testing.T) {
	av := NewAttestationValidator()

	result := CommitAttestationResult{
		Status:       CommitAttestValid,
		PromptRef:    "prompts/auth/v1.md",
		ComputedHash: "sha256:abc123",
		ExpectedHash: "", // no expected hash for path format
	}

	av.VerifyHashMatch(&result)

	if result.Status != CommitAttestValid {
		t.Errorf("Path format should not fail hash check, Status = %s", result.Status)
	}
}

func TestVerifyHashMatch_HashFormat_Match(t *testing.T) {
	av := NewAttestationValidator()

	result := CommitAttestationResult{
		Status:       CommitAttestValid,
		ComputedHash: "sha256:abc123",
		ExpectedHash: "sha256:abc123",
	}

	av.VerifyHashMatch(&result)

	if result.Status != CommitAttestValid {
		t.Errorf("Status = %s, want VALID (hashes match)", result.Status)
	}
}

func TestVerifyHashMatch_HashFormat_Mismatch(t *testing.T) {
	av := NewAttestationValidator()

	result := CommitAttestationResult{
		Status:       CommitAttestValid,
		ComputedHash: "sha256:actual",
		ExpectedHash: "sha256:expected",
	}

	av.VerifyHashMatch(&result)

	if result.Status != CommitAttestHashMismatch {
		t.Errorf("Status = %s, want HASH_MISMATCH", result.Status)
	}
}

// --- ValidateCommit (full flow) tests ---

func TestValidateCommit_PathFormat_FileExists(t *testing.T) {
	tmpDir := t.TempDir()
	promptDir := filepath.Join(tmpDir, "auth", "v1")
	mustMkdirAll(t, promptDir)
	mustWriteFile(t, filepath.Join(promptDir, "prompt.md"), []byte("# Auth"))

	av := &AttestationValidator{RegistryRoot: tmpDir}

	msg := `feat: auth

Prompt: prompts/auth/v1/prompt.md`

	result := av.ValidateCommit("sha123", msg)

	if result.Status != CommitAttestValid {
		t.Errorf("Status = %s, want VALID", result.Status)
	}
}

func TestValidateCommit_PathFormat_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	av := &AttestationValidator{RegistryRoot: tmpDir}

	msg := `feat: auth

Prompt: prompts/nonexistent/v1/prompt.md`

	result := av.ValidateCommit("sha123", msg)

	if result.Status != CommitAttestFileNotFound {
		t.Errorf("Status = %s, want FILE_NOT_FOUND", result.Status)
	}
}

func TestValidateCommit_MissingTrailer(t *testing.T) {
	av := NewAttestationValidator()

	msg := `feat: stuff

Just some code.`

	result := av.ValidateCommit("sha123", msg)

	if result.Status != CommitAttestMissing {
		t.Errorf("Status = %s, want MISSING", result.Status)
	}
}

// --- AttestationReport tests ---

func TestAttestationReport_AllValid(t *testing.T) {
	report := &AttestationReport{
		TotalCommits: 3,
		Results: []CommitAttestationResult{
			{CommitSHA: "a1", Status: CommitAttestValid},
			{CommitSHA: "b2", Status: CommitAttestValid},
			{CommitSHA: "c3", Status: CommitAttestValid},
		},
	}
	report.AllValid = !report.HasInvalid()

	if !report.AllValid {
		t.Error("AllValid should be true when all results are VALID")
	}
	if report.InvalidCount() != 0 {
		t.Errorf("InvalidCount = %d, want 0", report.InvalidCount())
	}
	if report.ShouldBlockMerge() {
		t.Error("ShouldBlockMerge should be false when all valid")
	}
}

func TestAttestationReport_HasInvalid(t *testing.T) {
	report := &AttestationReport{
		TotalCommits: 3,
		Results: []CommitAttestationResult{
			{CommitSHA: "a1", Status: CommitAttestValid},
			{CommitSHA: "b2", Status: CommitAttestMissing},
			{CommitSHA: "c3", Status: CommitAttestValid},
		},
	}

	if !report.HasInvalid() {
		t.Error("HasInvalid should be true")
	}
	if report.InvalidCount() != 1 {
		t.Errorf("InvalidCount = %d, want 1", report.InvalidCount())
	}
	if !report.ShouldBlockMerge() {
		t.Error("ShouldBlockMerge should be true with invalid commits")
	}
}

func TestAttestationReport_Empty(t *testing.T) {
	report := &AttestationReport{
		TotalCommits: 0,
		Results:      []CommitAttestationResult{},
	}

	if report.HasInvalid() {
		t.Error("Empty report should not have invalid")
	}
	if report.TotalCommits != 0 {
		t.Errorf("TotalCommits = %d, want 0", report.TotalCommits)
	}
}

func TestAttestationReport_Summary_AllValid(t *testing.T) {
	report := &AttestationReport{
		TotalCommits: 5,
		Results: []CommitAttestationResult{
			{Status: CommitAttestValid}, {Status: CommitAttestValid},
			{Status: CommitAttestValid}, {Status: CommitAttestValid},
			{Status: CommitAttestValid},
		},
	}
	report.AllValid = true

	summary := report.Summary()
	if summary == "" {
		t.Error("Summary should not be empty")
	}
}

func TestAttestationReport_Summary_WithInvalid(t *testing.T) {
	report := &AttestationReport{
		TotalCommits: 3,
		Results: []CommitAttestationResult{
			{Status: CommitAttestValid},
			{Status: CommitAttestMissing},
			{Status: CommitAttestMalformed},
		},
	}

	summary := report.Summary()
	if summary == "" {
		t.Error("Summary should not be empty")
	}
}

// --- Convenience function tests ---

func TestHasPromptTrailer(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"Prompt: prompts/auth/v1.md", true},
		{"Prompt: sha256:abc123", true},
		{"Prompt: malformed garbage", true},
		{"No prompt trailer here", false},
		{"", false},
	}
	for _, tt := range tests {
		got := HasPromptTrailer(tt.msg)
		if got != tt.want {
			t.Errorf("HasPromptTrailer(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestHasValidPromptTrailer(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"Prompt: prompts/auth/v1.md", true},
		{"Prompt: sha256:abc123def456", true},
		{"Prompt: malformed garbage", false},
		{"No prompt trailer here", false},
		{"", false},
		{"Prompt: prompts/auth/v1.md\nMore text", true},
	}
	for _, tt := range tests {
		got := HasValidPromptTrailer(tt.msg)
		if got != tt.want {
			t.Errorf("HasValidPromptTrailer(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestExtractPromptRef(t *testing.T) {
	tests := []struct {
		msg  string
		want string
	}{
		{"Prompt: prompts/auth/v1.md", "prompts/auth/v1.md"},
		{"Prompt: sha256:abc123", "sha256:abc123"},
		{"Prompt: garbage", ""},
		{"no prompt", ""},
	}
	for _, tt := range tests {
		got := ExtractPromptRef(tt.msg)
		if got != tt.want {
			t.Errorf("ExtractPromptRef(%q) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}

func TestIsPathFormat(t *testing.T) {
	if !IsPathFormat("prompts/auth/v1.md") {
		t.Error("prompts/ prefix should be path format")
	}
	if IsPathFormat("sha256:abc123") {
		t.Error("sha256: prefix should not be path format")
	}
}

func TestIsHashFormat(t *testing.T) {
	if !IsHashFormat("sha256:abc123") {
		t.Error("sha256: prefix should be hash format")
	}
	if IsHashFormat("prompts/auth/v1.md") {
		t.Error("prompts/ prefix should not be hash format")
	}
}

// --- ValidateCommits (batch) tests ---

func TestValidateCommits_AllValid(t *testing.T) {
	tmpDir := t.TempDir()
	promptDir := filepath.Join(tmpDir, "auth", "v1")
	mustMkdirAll(t, promptDir)
	mustWriteFile(t, filepath.Join(promptDir, "prompt.md"), []byte("# Auth"))

	av := &AttestationValidator{RegistryRoot: tmpDir}

	messages := []struct{ SHA, Message string }{
		{"sha1", "feat: a\n\nPrompt: prompts/auth/v1/prompt.md"},
		{"sha2", "feat: b\n\nPrompt: prompts/auth/v1/prompt.md"},
		{"sha3", "feat: c\n\nPrompt: prompts/auth/v1/prompt.md"},
	}

	report := av.ValidateCommits("PR-42", messages)

	if !report.AllValid {
		t.Errorf("AllValid = false, want true. Results: %+v", report.Results)
	}
	if report.TotalCommits != 3 {
		t.Errorf("TotalCommits = %d, want 3", report.TotalCommits)
	}
}

func TestValidateCommits_MixedValidity(t *testing.T) {
	tmpDir := t.TempDir()
	promptDir := filepath.Join(tmpDir, "auth", "v1")
	mustMkdirAll(t, promptDir)
	mustWriteFile(t, filepath.Join(promptDir, "prompt.md"), []byte("# Auth"))

	av := &AttestationValidator{RegistryRoot: tmpDir}

	messages := []struct{ SHA, Message string }{
		{"sha1", "feat: a\n\nPrompt: prompts/auth/v1/prompt.md"},    // VALID
		{"sha2", "feat: b\n\nNo prompt here"},                       // MISSING
		{"sha3", "feat: c\n\nPrompt: prompts/missing/v1/prompt.md"}, // FILE_NOT_FOUND
	}

	report := av.ValidateCommits("PR-42", messages)

	if report.AllValid {
		t.Error("AllValid should be false with mixed results")
	}
	if report.InvalidCount() != 2 {
		t.Errorf("InvalidCount = %d, want 2", report.InvalidCount())
	}
	if !report.ShouldBlockMerge() {
		t.Error("ShouldBlockMerge should be true")
	}

	// Verify individual statuses
	if report.Results[0].Status != CommitAttestValid {
		t.Errorf("Result[0].Status = %s, want VALID", report.Results[0].Status)
	}
	if report.Results[1].Status != CommitAttestMissing {
		t.Errorf("Result[1].Status = %s, want MISSING", report.Results[1].Status)
	}
	if report.Results[2].Status != CommitAttestFileNotFound {
		t.Errorf("Result[2].Status = %s, want FILE_NOT_FOUND", report.Results[2].Status)
	}
}

func TestValidateCommits_Empty(t *testing.T) {
	av := NewAttestationValidator()

	report := av.ValidateCommits("PR-0", nil)

	if !report.AllValid {
		t.Error("Empty report should be AllValid (trivially)")
	}
	if report.TotalCommits != 0 {
		t.Errorf("TotalCommits = %d, want 0", report.TotalCommits)
	}
}

// --- Integration test with real hashing ---

func TestValidateCommit_HashFormat_FullFlow(t *testing.T) {
	tmpDir := t.TempDir()

	content := "# Test Prompt\n\nDo the thing."
	promptDir := filepath.Join(tmpDir, "test-comp", "v1")
	mustMkdirAll(t, promptDir)
	promptPath := filepath.Join(promptDir, "prompt.md")
	mustWriteFile(t, promptPath, []byte(content))

	oldDir := RegistryDir
	RegistryDir = tmpDir
	defer func() { RegistryDir = oldDir }()

	pv, err := Register("test-comp", "v1", promptPath, "test-model", "test-provider", "specs/test.md", nil)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	av := &AttestationValidator{RegistryRoot: tmpDir}

	msg := "feat: test\n\nPrompt: " + pv.Hash

	result := av.ValidateCommit("sha123", msg)

	if result.Status != CommitAttestValid {
		t.Errorf("Status = %s, want VALID. Detail: %s", result.Status, result.Detail)
	}
}

func TestValidateCommit_HashFormat_TamperedContent(t *testing.T) {
	tmpDir := t.TempDir()

	content := "# Original Prompt"
	promptDir := filepath.Join(tmpDir, "test-comp", "v1")
	mustMkdirAll(t, promptDir)
	promptPath := filepath.Join(promptDir, "prompt.md")
	mustWriteFile(t, promptPath, []byte(content))

	oldDir := RegistryDir
	RegistryDir = tmpDir
	defer func() { RegistryDir = oldDir }()

	pv, err := Register("test-comp", "v1", promptPath, "test-model", "test-provider", "specs/test.md", nil)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	mustWriteFile(t, promptPath, []byte("# TAMPERED PROMPT"))

	av := &AttestationValidator{RegistryRoot: tmpDir}

	msg := "feat: test\n\nPrompt: " + pv.Hash

	result := av.ValidateCommit("sha123", msg)

	if result.Status != CommitAttestHashMismatch {
		t.Errorf("Status = %s, want HASH_MISMATCH (tampered). Detail: %s", result.Status, result.Detail)
	}
}
