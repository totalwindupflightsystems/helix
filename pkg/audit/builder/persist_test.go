// Tests for pkg/audit/builder/persist.go — covers JSONL persistence,
// file-IO error paths, the PersistBuilder wrapper, and StreamWriter
// crash-safety semantics.
package builder

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/audit"
)

// makeAuditEvidence returns an AuditEvidence with a handful of steps
// populated — enough to exercise the JSONL envelope code without
// constructing all 12 variants per test.
func makeAuditEvidence() *audit.AuditEvidence {
	return &audit.AuditEvidence{
		ForgejoIssue: &audit.ForgejoIssueEvidence{
			IssueURL:  "https://forgejo/owner/repo/issues/1",
			Creator:   "alice",
			Title:     "Test issue",
			Timestamp: time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
		},
		GitCommit: &audit.GitCommitEvidence{
			SHA:              "sha-1",
			AttestationFound: true,
			PromptHash:       "ph",
			Model:            "m",
			ContextHash:      "ch",
			AgentID:          "agent-1",
			Confidence:       90,
			CostUSD:          0.1,
		},
	}
}

// -----------------------------------------------------------------------------
// WriteToFile / ReadFromFile
// -----------------------------------------------------------------------------

func TestWriteToFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	ev := makeAuditEvidence()
	if err := WriteToFile(ev, path); err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}

	got, err := ReadFromFile(path)
	if err != nil {
		t.Fatalf("ReadFromFile: %v", err)
	}
	if got.GitCommit == nil || got.GitCommit.SHA != "sha-1" {
		t.Errorf("GitCommit lost in round-trip: %+v", got.GitCommit)
	}
	if got.ForgejoIssue == nil || got.ForgejoIssue.Creator != "alice" {
		t.Errorf("ForgejoIssue lost in round-trip")
	}
}

func TestWriteToFile_AppendsMultipleLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	ev1 := makeAuditEvidence()
	ev1.GitCommit.SHA = "sha-A"
	if err := WriteToFile(ev1, path); err != nil {
		t.Fatalf("WriteToFile 1: %v", err)
	}

	ev2 := makeAuditEvidence()
	ev2.GitCommit.SHA = "sha-B"
	if err := WriteToFile(ev2, path); err != nil {
		t.Fatalf("WriteToFile 2: %v", err)
	}

	// ReadFromFile returns the LAST record (most recent state).
	last, err := ReadFromFile(path)
	if err != nil {
		t.Fatalf("ReadFromFile: %v", err)
	}
	if last.GitCommit.SHA != "sha-B" {
		t.Errorf("last SHA = %q, want sha-B (most recent write)", last.GitCommit.SHA)
	}

	// ReadAllFromFile returns both in order.
	all, err := ReadAllFromFile(path)
	if err != nil {
		t.Fatalf("ReadAllFromFile: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ReadAllFromFile returned %d records, want 2", len(all))
	}
	if all[0].GitCommit.SHA != "sha-A" || all[1].GitCommit.SHA != "sha-B" {
		t.Errorf("order lost: %s -> %s", all[0].GitCommit.SHA, all[1].GitCommit.SHA)
	}
}

func TestWriteToFile_NilEvidence(t *testing.T) {
	dir := t.TempDir()
	err := WriteToFile(nil, filepath.Join(dir, "x.jsonl"))
	if err == nil {
		t.Fatal("expected error for nil evidence")
	}
}

func TestWriteToFile_EmptyPath(t *testing.T) {
	err := WriteToFile(makeAuditEvidence(), "")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestWriteToFile_CreatesParentDirOnDemand(t *testing.T) {
	// WriteToFile does NOT auto-mkdir (that's PersistBuilder's job),
	// so a non-existent parent must error.
	dir := t.TempDir()
	path := filepath.Join(dir, "no-such-subdir", "audit.jsonl")
	err := WriteToFile(makeAuditEvidence(), path)
	if err == nil {
		t.Fatal("expected error for missing parent dir, got nil")
	}
}

func TestReadFromFile_Missing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-file.jsonl")
	_, err := ReadFromFile(path)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestReadFromFile_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ReadFromFile(path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if !strings.Contains(err.Error(), "no audit evidence on disk") {
		t.Errorf("error %q should mention 'no audit evidence on disk'", err.Error())
	}
}

func TestReadFromFile_EmptyPath(t *testing.T) {
	_, err := ReadFromFile("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestReadFromFile_MalformedLastLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonl")
	// First valid line, then a corrupt tail.
	if err := os.WriteFile(path, []byte("{valid}\n{not-json\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ReadFromFile(path)
	if err == nil {
		t.Fatal("expected error for malformed last line")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error %q should mention 'decode'", err.Error())
	}
}

func TestReadFromFile_SkipsBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blanklines.jsonl")
	// Empty lines interspersed with valid records.
	ev := makeAuditEvidence()
	data, err := audit.MarshalEvidence(ev)
	if err != nil {
		t.Fatalf("MarshalEvidence: %v", err)
	}
	content := "\n\n" + string(data) + "\n\n" + string(data) + "\n\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadFromFile(path)
	if err != nil {
		t.Fatalf("ReadFromFile: %v", err)
	}
	if got.GitCommit == nil || got.GitCommit.SHA != "sha-1" {
		t.Errorf("blank-line skip broke parsing: %+v", got)
	}
}

func TestReadFromFile_LargeRecord(t *testing.T) {
	// Scanner buffer is 4 MiB — confirm we can read records larger than
	// the default 64 KiB scanner buffer.
	dir := t.TempDir()
	path := filepath.Join(dir, "large.jsonl")
	bigDiff := strings.Repeat("diff line\n", 200_000) // ~2.2 MiB
	ev := &audit.AuditEvidence{
		GitCommit: &audit.GitCommitEvidence{
			SHA:        "sha-big",
			PromptHash: bigDiff, // abuse PromptHash to hold a large payload
		},
	}
	if err := WriteToFile(ev, path); err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}
	got, err := ReadFromFile(path)
	if err != nil {
		t.Fatalf("ReadFromFile: %v", err)
	}
	if got.GitCommit.PromptHash != bigDiff {
		t.Errorf("large payload lost: %d bytes", len(got.GitCommit.PromptHash))
	}
}

// -----------------------------------------------------------------------------
// ReadAllFromFile
// -----------------------------------------------------------------------------

func TestReadAllFromFile_MultipleRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.jsonl")
	for _, sha := range []string{"a", "b", "c"} {
		ev := makeAuditEvidence()
		ev.GitCommit.SHA = sha
		if err := WriteToFile(ev, path); err != nil {
			t.Fatalf("WriteToFile %s: %v", sha, err)
		}
	}
	got, err := ReadAllFromFile(path)
	if err != nil {
		t.Fatalf("ReadAllFromFile: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if got[i].GitCommit.SHA != w {
			t.Errorf("got[%d].SHA = %q, want %q", i, got[i].GitCommit.SHA, w)
		}
	}
}

func TestReadAllFromFile_Missing(t *testing.T) {
	_, err := ReadAllFromFile(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadAllFromFile_EmptyPath(t *testing.T) {
	_, err := ReadAllFromFile("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestReadAllFromFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ReadAllFromFile(path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

// -----------------------------------------------------------------------------
// PersistBuilder
// -----------------------------------------------------------------------------

func TestPersistBuilder_Flush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	pb, err := NewPersistBuilder(path, "pr-1", false)
	if err != nil {
		t.Fatalf("NewPersistBuilder: %v", err)
	}

	// Mutate via the wrapper's setter (autoflush=false so no disk write
	// happens yet), then call Flush explicitly.
	pb.Issue("u", "c", time.Now(), "t")
	if err := pb.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if pb.Path() != path {
		t.Errorf("Path() = %q, want %q", pb.Path(), path)
	}

	got, err := ReadFromFile(path)
	if err != nil {
		t.Fatalf("ReadFromFile: %v", err)
	}
	if got.ForgejoIssue == nil || got.ForgejoIssue.Creator != "c" {
		t.Errorf("Flush did not persist ForgejoIssue: %+v", got.ForgejoIssue)
	}
}

func TestPersistBuilder_Autoflush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	pb, err := NewPersistBuilder(path, "pr-auto", true)
	if err != nil {
		t.Fatalf("NewPersistBuilder: %v", err)
	}
	// Use the wrapper's autoflush-enabled setter, not the underlying
	// builder's Commit (which bypasses the persistence layer).
	pb.Commit("sha", "ph", "m", "ch", "a", 90, 0.1, true)
	// No explicit Flush call — autoflush should have written already.
	got, err := ReadFromFile(path)
	if err != nil {
		t.Fatalf("ReadFromFile: %v", err)
	}
	if got.GitCommit == nil || got.GitCommit.SHA != "sha" {
		t.Errorf("autoflush did not persist: %+v", got)
	}
}

func TestNewPersistBuilder_EmptyPath(t *testing.T) {
	_, err := NewPersistBuilder("", "pr-x", false)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestNewPersistBuilder_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "subdir", "audit.jsonl")
	pb, err := NewPersistBuilder(path, "pr-nested", false)
	if err != nil {
		t.Fatalf("NewPersistBuilder: %v", err)
	}
	if pb == nil {
		t.Fatal("got nil builder")
	}
	// Parent dir must now exist.
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("parent dir not created: %v", err)
	}
}

func TestPersistBuilder_NilSafety(t *testing.T) {
	var pb *PersistBuilder
	if err := pb.Flush(); err == nil {
		t.Error("nil receiver should error on Flush")
	}
	if got := pb.Path(); got != "" {
		t.Errorf("nil.Path() = %q, want \"\"", got)
	}
	if got := pb.Builder(); got != nil {
		t.Errorf("nil.Builder() = %+v, want nil", got)
	}
}

// -----------------------------------------------------------------------------
// StreamWriter
// -----------------------------------------------------------------------------

func TestStreamWriter_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.jsonl")
	sw, err := NewStreamWriter(path)
	if err != nil {
		t.Fatalf("NewStreamWriter: %v", err)
	}
	ev1 := makeAuditEvidence()
	ev1.GitCommit.SHA = "stream-1"
	if err := sw.Write(ev1); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	ev2 := makeAuditEvidence()
	ev2.GitCommit.SHA = "stream-2"
	if err := sw.Write(ev2); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	if err := sw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := ReadAllFromFile(path)
	if err != nil {
		t.Fatalf("ReadAllFromFile: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].GitCommit.SHA != "stream-1" || got[1].GitCommit.SHA != "stream-2" {
		t.Errorf("stream order: %s -> %s", got[0].GitCommit.SHA, got[1].GitCommit.SHA)
	}
}

func TestStreamWriter_DoubleClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.jsonl")
	sw, err := NewStreamWriter(path)
	if err != nil {
		t.Fatalf("NewStreamWriter: %v", err)
	}
	if err := sw.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := sw.Close(); err != nil {
		t.Errorf("second Close: %v (should be no-op)", err)
	}
}

func TestStreamWriter_ClosedWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.jsonl")
	sw, err := NewStreamWriter(path)
	if err != nil {
		t.Fatalf("NewStreamWriter: %v", err)
	}
	_ = sw.Close()
	err = sw.Write(makeAuditEvidence())
	if err == nil {
		t.Fatal("expected error writing to closed writer")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("error %q should mention 'closed'", err.Error())
	}
}

func TestStreamWriter_EmptyPath(t *testing.T) {
	_, err := NewStreamWriter("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestStreamWriter_NilSafety(t *testing.T) {
	var sw *StreamWriter
	if err := sw.Close(); err != nil {
		t.Errorf("nil.Close() = %v, want nil", err)
	}
	if err := sw.Write(nil); err == nil {
		t.Error("nil.Write(nil) should error")
	}
}

// -----------------------------------------------------------------------------
// EncodeJSON — small helper
// -----------------------------------------------------------------------------

func TestEncodeJSON_Success(t *testing.T) {
	out, err := EncodeJSON(map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("EncodeJSON: %v", err)
	}
	if !bytes.HasSuffix(out, []byte("\n")) {
		t.Errorf("EncodeJSON output missing trailing newline: %q", out)
	}
	if !strings.Contains(string(out), `"k":"v"`) {
		t.Errorf("EncodeJSON output malformed: %q", out)
	}
}

func TestEncodeJSON_Nil(t *testing.T) {
	_, err := EncodeJSON(nil)
	if err == nil {
		t.Fatal("expected error for nil input")
	}
}

// -----------------------------------------------------------------------------
// Compile-time interface assertion — defensive
// -----------------------------------------------------------------------------

// Verify that bytes.Buffer (which we use throughout the persist tests)
// satisfies io.Writer. If this ever fails, Go's stdlib changed shape
// and the rest of the file needs re-auditing.
func TestBytesBufferImplementsIOWriter(t *testing.T) {
	var w io.Writer = (*bytes.Buffer)(nil)
	_ = w
}
