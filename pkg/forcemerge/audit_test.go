package forcemerge

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// Test helpers
// =============================================================================

func sampleAuditEntry() AuditEntry {
	return AuditEntry{
		PRURL:         "https://forgejo.example.com/owner/repo/pulls/123",
		HumanIdentity: "alice",
		Justification: "Critical hotfix for production outage in checkout flow — rollback risk too high to wait for full review.",
		MergeSHA:      "abc123def4567890",
		Timestamp:     "2026-07-04T10:00:00Z",
		Repo:          "owner/repo",
	}
}

func sampleReviewEntry() ReviewEntry {
	return ReviewEntry{
		PRURL:      "https://forgejo.example.com/owner/repo/pulls/123",
		MergeSHA:   "abc123def4567890",
		Reviewer:   "conscientiousness/gpt-5",
		Status:     ReviewPassed,
		Reason:     "The justification references a production outage and the rollback risk; override was appropriate.",
		Confidence: 88,
		Timestamp:  "2026-07-04T10:30:00Z",
	}
}

// =============================================================================
// Constants and labels
// =============================================================================

func TestReviewStatus_IsValid(t *testing.T) {
	for _, s := range []ReviewStatus{ReviewPending, ReviewPassed, ReviewFailed} {
		if !s.IsValid() {
			t.Errorf("expected %q valid", s)
		}
	}
	for _, s := range []ReviewStatus{"", "UNKNOWN", "pending", "passed"} {
		if s.IsValid() {
			t.Errorf("expected %q invalid", s)
		}
	}
}

func TestHasForceMergeLabel(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		want   bool
	}{
		{"empty", nil, false},
		{"direct hit", []string{"force-merge"}, true},
		{"mixed", []string{"bug", "force-merge", "urgent"}, true},
		{"different", []string{"bug", "enhancement"}, false},
		{"case-insensitive", []string{"FORCE-MERGE"}, true},
		{"with-whitespace", []string{" force-merge "}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HasForceMergeLabel(c.labels); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

// =============================================================================
// Validation
// =============================================================================

func TestValidateJustification(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty", "", true},
		{"whitespace", "   \t\n", true},
		{"too-short", "ok", true},
		{"19-chars (one below)", "0123456789012345678", true},
		{"20-chars (boundary)", "01234567890123456789", false},
		{"good", "Critical hotfix — production outage.", false},
		{"too-long", strings.Repeat("x", MaxJustificationLen+1), true},
		{"max-len (boundary)", strings.Repeat("x", MaxJustificationLen), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateJustification(c.in)
			if c.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateAuditEntry(t *testing.T) {
	good := sampleAuditEntry()
	if err := ValidateAuditEntry(good); err != nil {
		t.Errorf("good entry should validate, got %v", err)
	}

	cases := []struct {
		name   string
		mutate func(AuditEntry) AuditEntry
		want   string
	}{
		{"empty PRURL", func(e AuditEntry) AuditEntry { e.PRURL = ""; return e }, "PRURL"},
		{"empty human", func(e AuditEntry) AuditEntry { e.HumanIdentity = ""; return e }, "human_identity"},
		{"empty sha", func(e AuditEntry) AuditEntry { e.MergeSHA = ""; return e }, "merge_sha"},
		{"empty justification", func(e AuditEntry) AuditEntry { e.Justification = ""; return e }, "justification"},
		{"short justification", func(e AuditEntry) AuditEntry { e.Justification = "ok"; return e }, "justification"},
		{"bad timestamp", func(e AuditEntry) AuditEntry { e.Timestamp = "yesterday"; return e }, "timestamp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateAuditEntry(c.mutate(good))
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err.Error(), c.want)
			}
		})
	}
}

func TestValidateReviewEntry(t *testing.T) {
	good := sampleReviewEntry()
	if err := ValidateReviewEntry(good); err != nil {
		t.Errorf("good entry should validate, got %v", err)
	}

	cases := []struct {
		name   string
		mutate func(ReviewEntry) ReviewEntry
		want   string
	}{
		{"empty PRURL", func(e ReviewEntry) ReviewEntry { e.PRURL = ""; return e }, "PRURL"},
		{"empty sha", func(e ReviewEntry) ReviewEntry { e.MergeSHA = ""; return e }, "merge_sha"},
		{"bad status", func(e ReviewEntry) ReviewEntry { e.Status = "WUT"; return e }, "invalid review status"},
		{"negative confidence", func(e ReviewEntry) ReviewEntry { e.Confidence = -1; return e }, "out of [0,100]"},
		{"high confidence", func(e ReviewEntry) ReviewEntry { e.Confidence = 101; return e }, "out of [0,100]"},
		{"bad timestamp", func(e ReviewEntry) ReviewEntry { e.Timestamp = "yesterday"; return e }, "timestamp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateReviewEntry(c.mutate(good))
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err.Error(), c.want)
			}
		})
	}
}

// =============================================================================
// Path expansion
// =============================================================================

func TestExpandPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want func(string) bool
	}{
		{"absolute", "/var/log/x.jsonl", func(s string) bool { return s == "/var/log/x.jsonl" }},
		{"relative", "x.jsonl", func(s string) bool { return s == "x.jsonl" }},
		{"home-only", "~", func(s string) bool {
			home, _ := os.UserHomeDir()
			return s == home
		}},
		{"home-prefix", "~/forcemerge.jsonl", func(s string) bool {
			home, _ := os.UserHomeDir()
			return s == filepath.Join(home, "forcemerge.jsonl")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ExpandPath(c.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !c.want(got) {
				t.Errorf("got %q, want matches predicate", got)
			}
		})
	}
}

// =============================================================================
// AuditStore
// =============================================================================

func TestAuditStore_RecordAudit_WritesJSONL(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewWriterStore(buf)

	if err := s.RecordAudit(sampleAuditEntry()); err != nil {
		t.Fatalf("RecordAudit: %v", err)
	}

	line := strings.TrimSpace(buf.String())
	if !strings.HasSuffix(line, "}") {
		t.Fatalf("expected JSON object, got %q", line)
	}
	// Round-trip: parse back.
	var got AuditEntry
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("unmarshal: %v\nline=%q", err, line)
	}
	if got.PRURL != sampleAuditEntry().PRURL {
		t.Errorf("PRURL round-trip mismatch")
	}
}

func TestAuditStore_RecordAudit_RejectsInvalid(t *testing.T) {
	s := NewWriterStore(&bytes.Buffer{})
	err := s.RecordAudit(AuditEntry{}) // empty
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestAuditStore_RecordReview_WritesJSONL(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewWriterStore(buf)

	if err := s.RecordReview(sampleReviewEntry()); err != nil {
		t.Fatalf("RecordReview: %v", err)
	}
	line := strings.TrimSpace(buf.String())
	var got ReviewEntry
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Status != ReviewPassed {
		t.Errorf("status round-trip mismatch")
	}
}

func TestAuditStore_ConcurrentWrites(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewWriterStore(buf)

	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			e := sampleAuditEntry()
			e.HumanIdentity = "human-" + itoaSafe(i)
			// Use unique timestamps for each goroutine by varying the second.
			sec := i % 60
			e.Timestamp = "2026-07-04T10:00:" + pad2(sec) + "Z"
			_ = s.RecordAudit(e)
		}(i)
	}
	wg.Wait()

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != N {
		t.Errorf("expected %d lines, got %d", N, len(lines))
	}
}

func TestAuditStore_FileStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forcemerge.jsonl")

	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	defer s.Close()

	if s.Path() != path {
		t.Errorf("Path()=%q want %q", s.Path(), path)
	}

	if err := s.RecordAudit(sampleAuditEntry()); err != nil {
		t.Fatalf("RecordAudit: %v", err)
	}
	if err := s.RecordReview(sampleReviewEntry()); err != nil {
		t.Fatalf("RecordReview: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open and read.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

func TestAuditStore_FileStore_AppendsAcrossOpens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	s1, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s1.RecordAudit(sampleAuditEntry()); err != nil {
		t.Fatalf("RecordAudit: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}

	s2, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore 2: %v", err)
	}
	e2 := sampleAuditEntry()
	e2.MergeSHA = "different-sha"
	if err := s2.RecordAudit(e2); err != nil {
		t.Fatalf("RecordAudit 2: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("Close 2: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 2 {
		t.Errorf("expected 2 appended lines, got %d", len(lines))
	}
}

func TestAuditStore_FileStore_InvalidPath(t *testing.T) {
	// Should error if we can't create the parent dir.
	_, err := NewFileStore("/nonexistent_root_zzzzz/x.jsonl")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestAuditStore_Close_Idempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(filepath.Join(dir, "x.jsonl"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close (idempotent): %v", err)
	}
}

// =============================================================================
// BuildAuditReport
// =============================================================================

func TestBuildAuditReport_Empty(t *testing.T) {
	rep, err := BuildAuditReport(strings.NewReader(""), time.Now())
	if err != nil {
		t.Fatalf("BuildAuditReport: %v", err)
	}
	if rep.TotalMerges != 0 {
		t.Errorf("TotalMerges=%d want 0", rep.TotalMerges)
	}
	if rep.ByMonth == nil {
		t.Error("ByMonth should be initialised (empty), not nil")
	}
}

func TestBuildAuditReport_SingleAudit(t *testing.T) {
	e := sampleAuditEntry()
	data, _ := json.Marshal(e)
	input := string(data) + "\n"

	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	rep, err := BuildAuditReport(strings.NewReader(input), now)
	if err != nil {
		t.Fatalf("BuildAuditReport: %v", err)
	}
	if rep.TotalMerges != 1 {
		t.Errorf("TotalMerges=%d", rep.TotalMerges)
	}
	if rep.PendingReviewCount != 1 {
		t.Errorf("PendingReviewCount=%d want 1", rep.PendingReviewCount)
	}
	ms, ok := rep.ByMonth["2026-07"]
	if !ok {
		t.Fatalf("missing 2026-07 stats: %+v", rep.ByMonth)
	}
	if ms.Merges != 1 {
		t.Errorf("ms.Merges=%d", ms.Merges)
	}
	usages := rep.HumansByMonth["2026-07"]
	if len(usages) != 1 || usages[0].Human != "alice" || usages[0].Count != 1 {
		t.Errorf("usages=%+v", usages)
	}
}

func TestBuildAuditReport_AuditPlusReview_Passed(t *testing.T) {
	e := sampleAuditEntry()
	r := sampleReviewEntry()
	auditLine, _ := json.Marshal(e)
	reviewLine, _ := json.Marshal(r)
	input := string(auditLine) + "\n" + string(reviewLine) + "\n"

	rep, err := BuildAuditReport(strings.NewReader(input), time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildAuditReport: %v", err)
	}
	if rep.PendingReviewCount != 0 {
		t.Errorf("PendingReviewCount=%d want 0", rep.PendingReviewCount)
	}
	if rep.FailedReviewCount != 0 {
		t.Errorf("FailedReviewCount=%d want 0", rep.FailedReviewCount)
	}
	ms := rep.ByMonth["2026-07"]
	if ms.PassedReviews != 1 {
		t.Errorf("PassedReviews=%d want 1", ms.PassedReviews)
	}
}

func TestBuildAuditReport_AuditPlusReview_Failed(t *testing.T) {
	e := sampleAuditEntry()
	r := sampleReviewEntry()
	r.Status = ReviewFailed
	auditLine, _ := json.Marshal(e)
	reviewLine, _ := json.Marshal(r)
	input := string(auditLine) + "\n" + string(reviewLine) + "\n"

	rep, err := BuildAuditReport(strings.NewReader(input), time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildAuditReport: %v", err)
	}
	if rep.FailedReviewCount != 1 {
		t.Errorf("FailedReviewCount=%d want 1", rep.FailedReviewCount)
	}
}

func TestBuildAuditReport_MultipleHumans(t *testing.T) {
	var lines []string
	for i, h := range []string{"alice", "bob", "alice", "carol", "alice"} {
		e := sampleAuditEntry()
		e.HumanIdentity = h
		e.MergeSHA = "sha-" + itoaSafe(i)
		e.Timestamp = "2026-07-04T10:00:00Z"
		data, _ := json.Marshal(e)
		lines = append(lines, string(data))
	}
	input := strings.Join(lines, "\n") + "\n"

	rep, err := BuildAuditReport(strings.NewReader(input), time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildAuditReport: %v", err)
	}
	if rep.TotalMerges != 5 {
		t.Errorf("TotalMerges=%d", rep.TotalMerges)
	}
	usages := rep.HumansByMonth["2026-07"]
	if len(usages) != 3 {
		t.Fatalf("usages=%+v", usages)
	}
	// Sorted by count desc, then name asc.
	if usages[0].Human != "alice" || usages[0].Count != 3 {
		t.Errorf("usages[0]=%+v", usages[0])
	}
	if usages[1].Human != "bob" || usages[1].Count != 1 {
		t.Errorf("usages[1]=%+v", usages[1])
	}
	if usages[2].Human != "carol" || usages[2].Count != 1 {
		t.Errorf("usages[2]=%+v", usages[2])
	}
}

func TestBuildAuditReport_MalformedLineSkipped(t *testing.T) {
	e := sampleAuditEntry()
	good, _ := json.Marshal(e)
	input := "this is not json\n" + string(good) + "\n" + "{}\n" + "{\"unknown\":\"shape\"}\n"

	rep, err := BuildAuditReport(strings.NewReader(input), time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildAuditReport: %v", err)
	}
	if rep.TotalMerges != 1 {
		t.Errorf("TotalMerges=%d want 1", rep.TotalMerges)
	}
}

func TestBuildAuditReport_MultipleMonths(t *testing.T) {
	jul := sampleAuditEntry()
	jun := sampleAuditEntry()
	jun.HumanIdentity = "bob"
	jun.Timestamp = "2026-06-15T10:00:00Z"
	jun.MergeSHA = "sha-jun"

	julLine, _ := json.Marshal(jul)
	junLine, _ := json.Marshal(jun)
	input := string(julLine) + "\n" + string(junLine) + "\n"

	rep, err := BuildAuditReport(strings.NewReader(input), time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildAuditReport: %v", err)
	}
	if rep.TotalMerges != 2 {
		t.Errorf("TotalMerges=%d", rep.TotalMerges)
	}
	if len(rep.ByMonth) != 2 {
		t.Errorf("ByMonth len=%d", len(rep.ByMonth))
	}
	if rep.ByMonth["2026-07"].Merges != 1 {
		t.Errorf("Jul Merges=%d", rep.ByMonth["2026-07"].Merges)
	}
	if rep.ByMonth["2026-06"].Merges != 1 {
		t.Errorf("Jun Merges=%d", rep.ByMonth["2026-06"].Merges)
	}
}

func TestBuildAuditReport_TimestampFallbackMonth(t *testing.T) {
	// If a record has a bad timestamp, the audit entry's month is
	// bucketed into the report's "now" month (operational fallback).
	e := sampleAuditEntry()
	e.Timestamp = "" // will fail Validate but BuildAuditReport is forgiving
	data, _ := json.Marshal(e)
	input := string(data) + "\n"

	now := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	rep, err := BuildAuditReport(strings.NewReader(input), now)
	if err != nil {
		t.Fatalf("BuildAuditReport: %v", err)
	}
	if rep.ByMonth["2026-07"].Merges != 1 {
		t.Errorf("expected fallback to current month, got %+v", rep.ByMonth)
	}
}

func TestBuildAuditReport_LongLines(t *testing.T) {
	// Verify the scanner's 1 MiB buffer is honoured.
	e := sampleAuditEntry()
	e.Justification = strings.Repeat("x", 100_000) // 100 KiB
	data, _ := json.Marshal(e)
	input := string(data) + "\n"

	_, err := BuildAuditReport(strings.NewReader(input), time.Now())
	if err != nil {
		t.Fatalf("BuildAuditReport (long line): %v", err)
	}
}

// =============================================================================
// FormatReport
// =============================================================================

func TestFormatReport_Empty(t *testing.T) {
	rep := AuditReport{ByMonth: map[string]MonthlyStats{}, HumansByMonth: map[string][]HumanUsage{}}
	out := FormatReport(rep)
	if !strings.Contains(out, "total merges: 0") {
		t.Errorf("expected 'total merges: 0' in:\n%s", out)
	}
	if !strings.Contains(out, "(no records)") {
		t.Errorf("expected '(no records)' in:\n%s", out)
	}
}

func TestFormatReport_NonEmpty(t *testing.T) {
	rep := AuditReport{
		TotalMerges:        3,
		PendingReviewCount: 1,
		FailedReviewCount:  1,
		ByMonth: map[string]MonthlyStats{
			"2026-07": {Merges: 3, PassedReviews: 1, FailedReviews: 1, PendingReviews: 1},
			"2026-06": {Merges: 0},
		},
		HumansByMonth: map[string][]HumanUsage{
			"2026-07": {{Human: "alice", Count: 3}},
		},
	}
	out := FormatReport(rep)
	for _, want := range []string{
		"total merges: 3",
		"Pending reviews: 1",
		"Failed reviews: 1",
		"2026-06", "2026-07",
		"alice × 3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
}

// =============================================================================
// End-to-end smoke
// =============================================================================

func TestEndToEnd_WriteReadReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	// 3 audit entries across 2 humans, 2 reviews.
	entries := []AuditEntry{
		{
			PRURL:         "https://forgejo.example.com/owner/repo/pulls/1",
			HumanIdentity: "alice", Justification: "Production outage in checkout — emergency fix.",
			MergeSHA: "sha-1", Timestamp: "2026-07-01T10:00:00Z",
		},
		{
			PRURL:         "https://forgejo.example.com/owner/repo/pulls/2",
			HumanIdentity: "alice", Justification: "Security advisory CVE-2026-X — embargoed hotfix.",
			MergeSHA: "sha-2", Timestamp: "2026-07-02T10:00:00Z",
		},
		{
			PRURL:         "https://forgejo.example.com/owner/repo/pulls/3",
			HumanIdentity: "bob", Justification: "Marketing launch coordination — deadline-driven.",
			MergeSHA: "sha-3", Timestamp: "2026-07-03T10:00:00Z",
		},
	}
	for _, e := range entries {
		if err := s.RecordAudit(e); err != nil {
			t.Fatalf("RecordAudit: %v", err)
		}
	}
	reviews := []ReviewEntry{
		{
			PRURL: entries[0].PRURL, MergeSHA: "sha-1",
			Reviewer: "conscientiousness/gpt-5", Status: ReviewPassed,
			Reason: "Production outage justification was valid.", Confidence: 90,
			Timestamp: "2026-07-01T11:00:00Z",
		},
		{
			PRURL: entries[2].PRURL, MergeSHA: "sha-3",
			Reviewer: "conscientiousness/gpt-5", Status: ReviewFailed,
			Reason: "Marketing launches should not bypass co-approval.", Confidence: 85,
			Timestamp: "2026-07-03T11:00:00Z",
		},
	}
	for _, r := range reviews {
		if err := s.RecordReview(r); err != nil {
			t.Fatalf("RecordReview: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open, read, build report.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	rep, err := BuildAuditReport(f, time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildAuditReport: %v", err)
	}

	if rep.TotalMerges != 3 {
		t.Errorf("TotalMerges=%d want 3", rep.TotalMerges)
	}
	if rep.FailedReviewCount != 1 {
		t.Errorf("FailedReviewCount=%d want 1", rep.FailedReviewCount)
	}
	if rep.PendingReviewCount != 1 {
		t.Errorf("PendingReviewCount=%d want 1 (entry 2 has no review)", rep.PendingReviewCount)
	}
	usages := rep.HumansByMonth["2026-07"]
	if len(usages) != 2 {
		t.Fatalf("expected 2 humans, got %d", len(usages))
	}
	if usages[0].Human != "alice" || usages[0].Count != 2 {
		t.Errorf("usages[0]=%+v", usages[0])
	}
}

// =============================================================================
// Helpers (test-only)
// =============================================================================

// itoaSafe returns i as a decimal string, supporting any non-negative int.
func itoaSafe(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// pad2 returns i as a 2-digit zero-padded string ("00".."99"). Panics if
// i is out of [0,99].
func pad2(i int) string {
	if i < 0 || i > 99 {
		panic("pad2: out of range")
	}
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string([]byte{byte('0' + i/10), byte('0' + i%10)})
}

// unused imports — keep for future use without breaking builds.
var _ = bufio.NewScanner
var _ = io.EOF
var _ = errors.New
