// Command helix — incident_test.go
//
// Tests for the `helix incident` CLI subcommand family. Each test uses
// a t.TempDir-backed store so no global state is mutated.

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/security"
)

// splitArgs splits a shell-like command string into tokens, respecting
// single-quoted and double-quoted substrings. Used by tests that want
// to write `helix incident declare --title "Major breach"` inline.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inSingle, inDouble := false, false
	for _, r := range s {
		switch {
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case (r == ' ' || r == '	') && !inSingle && !inDouble:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// withTempStore returns args prepended with --store <tmpfile>.
func withTempStore(t *testing.T, base string) []string {
	t.Helper()
	dir := t.TempDir()
	store := filepath.Join(dir, "incidents.jsonl")
	pre := []string{"--store", store}
	return append(pre, splitArgs(base)...)
}

// runIncidentCLI runs the incident subcommand with args and returns
// stdout, stderr, exit code.
func runIncidentCLI(t *testing.T, args []string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	rc := runIncident(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), rc
}

func TestRunIncident_Help(t *testing.T) {
	stdout, _, rc := runIncidentCLI(t, []string{})
	if rc != 0 {
		t.Errorf("exit code = %d, want 0", rc)
	}
	if !strings.Contains(stdout, "helix incident") {
		t.Errorf("stdout = %q, want usage header", stdout)
	}
	if !strings.Contains(stdout, "declare") || !strings.Contains(stdout, "list") {
		t.Errorf("stdout missing subcommand list: %q", stdout)
	}
}

func TestRunIncident_UnknownSubcommand(t *testing.T) {
	_, stderr, rc := runIncidentCLI(t, []string{"bogus"})
	if rc != 2 {
		t.Errorf("exit code = %d, want 2", rc)
	}
	if !strings.Contains(stderr, "unknown subcommand") {
		t.Errorf("stderr = %q, want 'unknown subcommand'", stderr)
	}
}

func TestRunIncidentDeclare_HappyPath(t *testing.T) {
	args := withTempStore(t, `declare --severity SEV-1 --title "Test incident"`)
	stdout, stderr, rc := runIncidentCLI(t, args)
	if rc != 0 {
		t.Errorf("exit = %d, stderr=%q", rc, stderr)
	}
	if !strings.Contains(stdout, "Declared SEV-1") {
		t.Errorf("stdout = %q, want 'Declared SEV-1'", stdout)
	}
	if !strings.Contains(stdout, "inc-") {
		t.Errorf("stdout = %q, want incident ID 'inc-...'", stdout)
	}
}

func TestRunIncidentDeclare_InvalidSeverity(t *testing.T) {
	args := withTempStore(t, "declare --severity SEV-99 --title Test")
	_, stderr, rc := runIncidentCLI(t, args)
	if rc != 2 {
		t.Errorf("exit = %d, want 2", rc)
	}
	if !strings.Contains(stderr, "invalid severity") {
		t.Errorf("stderr = %q, want 'invalid severity'", stderr)
	}
}

func TestRunIncidentDeclare_MissingRequired(t *testing.T) {
	cases := [][]string{
		{"declare", "--severity", "SEV-1"},
		{"declare", "--title", "Test"},
	}
	for i, args := range cases {
		_, stderr, rc := runIncidentCLI(t, withTempStore(t, strings.Join(args, " ")))
		if rc != 2 {
			t.Errorf("case %d: exit = %d, want 2", i, rc)
		}
		if !strings.Contains(stderr, "required") {
			t.Errorf("case %d: stderr = %q, want 'required'", i, stderr)
		}
	}
}

func TestRunIncidentDeclare_JSON(t *testing.T) {
	args := withTempStore(t, "declare --severity SEV-0 --title 'Major breach' --agent agent-x --json")
	stdout, _, rc := runIncidentCLI(t, args)
	if rc != 0 {
		t.Fatalf("exit = %d", rc)
	}
	var out struct {
		ID, Severity, Status, StoredAt, StorePath string
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %q", err, stdout)
	}
	if out.ID == "" || out.Severity != "SEV-0" || out.Status != "open" {
		t.Errorf("unexpected JSON: %+v", out)
	}
}

func TestRunIncidentList_Empty(t *testing.T) {
	args := withTempStore(t, "list")
	stdout, _, rc := runIncidentCLI(t, args)
	if rc != 0 {
		t.Fatalf("exit = %d", rc)
	}
	if !strings.Contains(stdout, "(no incidents)") {
		t.Errorf("stdout = %q, want '(no incidents)'", stdout)
	}
}

func TestRunIncidentList_AfterDeclare(t *testing.T) {
	store := withTempStore(t, "") // capture the temp store path

	// Declare 3 incidents
	for _, sev := range []string{"SEV-0", "SEV-1", "SEV-2"} {
		args := append([]string{}, store...)
		args = append(args, "declare", "--severity", sev, "--title", "Test "+sev)
		if _, _, rc := runIncidentCLI(t, args); rc != 0 {
			t.Fatalf("declare %s exit = %d", sev, rc)
		}
	}

	// List all
	args := append([]string{}, store...)
	args = append(args, "list")
	stdout, _, rc := runIncidentCLI(t, args)
	if rc != 0 {
		t.Fatalf("list exit = %d", rc)
	}
	if !strings.Contains(stdout, "SEV-0") || !strings.Contains(stdout, "SEV-1") || !strings.Contains(stdout, "SEV-2") {
		t.Errorf("stdout = %q, want all 3 severities", stdout)
	}
	// Should be sorted by severity: SEV-0 first.
	idx0 := strings.Index(stdout, "SEV-0")
	idx1 := strings.Index(stdout, "SEV-1")
	idx2 := strings.Index(stdout, "SEV-2")
	if !(idx0 < idx1 && idx1 < idx2) {
		t.Errorf("list not sorted by severity: SEV-0@%d SEV-1@%d SEV-2@%d", idx0, idx1, idx2)
	}
}

func TestRunIncidentList_JSON(t *testing.T) {
	store := withTempStore(t, "")
	args := append([]string{}, store...)
	args = append(args, "declare", "--severity", "SEV-1", "--title", "First")
	if _, _, rc := runIncidentCLI(t, args); rc != 0 {
		t.Fatalf("declare exit = %d", rc)
	}

	args = append([]string{}, store...)
	args = append(args, "list", "--json")
	stdout, _, rc := runIncidentCLI(t, args)
	if rc != 0 {
		t.Fatalf("list exit = %d", rc)
	}
	var list []*json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &list); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %q", err, stdout)
	}
	if len(list) != 1 {
		t.Errorf("got %d records, want 1", len(list))
	}
}

func TestRunIncidentShow_HappyPath(t *testing.T) {
	store := withTempStore(t, "")
	args := append([]string{}, store...)
	args = append(args, "declare", "--severity", "SEV-2", "--title", "Show me", "--description", "long form")
	if _, _, rc := runIncidentCLI(t, args); rc != 0 {
		t.Fatalf("declare exit = %d", rc)
	}

	args = append([]string{}, store...)
	args = append(args, "list")
	stdout, _, _ := runIncidentCLI(t, args)
	// Extract first ID from the list table.
	var id string
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 1 && strings.HasPrefix(fields[0], "inc-") {
			id = fields[0]
			break
		}
	}
	if id == "" {
		t.Fatalf("could not extract incident ID from list output: %q", stdout)
	}

	args = append([]string{}, store...)
	args = append(args, "show", id)
	stdout, _, rc := runIncidentCLI(t, args)
	if rc != 0 {
		t.Errorf("show exit = %d", rc)
	}
	if !strings.Contains(stdout, "SEV-2") {
		t.Errorf("stdout = %q, want 'SEV-2'", stdout)
	}
	if !strings.Contains(stdout, "Show me") {
		t.Errorf("stdout = %q, want title", stdout)
	}
	if !strings.Contains(stdout, "Response procedure") {
		t.Errorf("stdout = %q, want response procedure", stdout)
	}
}

func TestRunIncidentShow_NotFound(t *testing.T) {
	args := withTempStore(t, "show inc-doesnotexist")
	_, stderr, rc := runIncidentCLI(t, args)
	if rc != 1 {
		t.Errorf("exit = %d, want 1", rc)
	}
	if !strings.Contains(stderr, "no incident") {
		t.Errorf("stderr = %q, want 'no incident'", stderr)
	}
}

func TestRunIncidentUpdate_ByID(t *testing.T) {
	store := withTempStore(t, "")
	// Declare
	args := append([]string{}, store...)
	args = append(args, "declare", "--severity", "SEV-2", "--title", "To resolve")
	_, _, rc := runIncidentCLI(t, args)
	if rc != 0 {
		t.Fatalf("declare exit = %d", rc)
	}
	// List to get ID
	args = append([]string{}, store...)
	args = append(args, "list")
	stdout, _, _ := runIncidentCLI(t, args)
	var id string
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 1 && strings.HasPrefix(fields[0], "inc-") {
			id = fields[0]
			break
		}
	}
	if id == "" {
		t.Fatalf("no ID found")
	}
	// Update by --id
	args = append([]string{}, store...)
	args = append(args, "update", "--id", id, "--status", "resolved")
	_, stderr, rc := runIncidentCLI(t, args)
	if rc != 0 {
		t.Errorf("update exit = %d, stderr=%q", rc, stderr)
	}
	// Re-list to confirm
	args = append([]string{}, store...)
	args = append(args, "list", "--all")
	stdout, _, _ = runIncidentCLI(t, args)
	if !strings.Contains(stdout, "resolved") {
		t.Errorf("after update, list should show 'resolved': %q", stdout)
	}
}

func TestRunIncidentUpdate_ByPositional(t *testing.T) {
	store := withTempStore(t, "")
	args := append([]string{}, store...)
	args = append(args, "declare", "--severity", "SEV-1", "--title", "Escalate me")
	_, _, rc := runIncidentCLI(t, args)
	if rc != 0 {
		t.Fatalf("declare exit = %d", rc)
	}
	args = append([]string{}, store...)
	args = append(args, "list")
	stdout, _, _ := runIncidentCLI(t, args)
	var id string
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 1 && strings.HasPrefix(fields[0], "inc-") {
			id = fields[0]
			break
		}
	}
	if id == "" {
		t.Fatalf("no ID found")
	}

	// Update by positional ID + --status
	args = append([]string{}, store...)
	args = append(args, "update", id, "--status", "escalated")
	_, stderr, rc := runIncidentCLI(t, args)
	if rc != 0 {
		t.Errorf("update exit = %d, stderr=%q", rc, stderr)
	}
}

func TestRunIncidentUpdate_InvalidStatus(t *testing.T) {
	store := withTempStore(t, "")
	args := append([]string{}, store...)
	args = append(args, "declare", "--severity", "SEV-1", "--title", "x")
	_, _, _ = runIncidentCLI(t, args)

	args = append([]string{}, store...)
	args = append(args, "update", "--status", "BOGUS")
	_, stderr, rc := runIncidentCLI(t, args)
	if rc != 2 {
		t.Errorf("exit = %d, want 2", rc)
	}
	if !strings.Contains(stderr, "invalid status") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRunIncidentUpdate_MissingID(t *testing.T) {
	args := withTempStore(t, "update --status resolved")
	_, stderr, rc := runIncidentCLI(t, args)
	if rc != 2 {
		t.Errorf("exit = %d, want 2", rc)
	}
	if !strings.Contains(stderr, "ID is required") {
		t.Errorf("stderr = %q, want 'ID is required'", stderr)
	}
}

func TestRunIncidentStats(t *testing.T) {
	store := withTempStore(t, "")
	for _, sev := range []string{"SEV-1", "SEV-2"} {
		args := append([]string{}, store...)
		args = append(args, "declare", "--severity", sev, "--title", "x")
		_, _, _ = runIncidentCLI(t, args)
	}
	args := append([]string{}, store...)
	args = append(args, "stats")
	stdout, _, rc := runIncidentCLI(t, args)
	if rc != 0 {
		t.Fatalf("stats exit = %d", rc)
	}
	if !strings.Contains(stdout, "Total: 2") {
		t.Errorf("stdout = %q, want 'Total: 2'", stdout)
	}
	if !strings.Contains(stdout, "SEV-1") || !strings.Contains(stdout, "SEV-2") {
		t.Errorf("stdout = %q, want both severities", stdout)
	}
}

func TestRunIncidentStats_JSON(t *testing.T) {
	store := withTempStore(t, "")
	args := append([]string{}, store...)
	args = append(args, "declare", "--severity", "SEV-1", "--title", "x")
	_, _, _ = runIncidentCLI(t, args)
	args = append([]string{}, store...)
	args = append(args, "stats", "--json")
	stdout, _, rc := runIncidentCLI(t, args)
	if rc != 0 {
		t.Fatalf("exit = %d", rc)
	}
	var stats struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(stdout), &stats); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if stats.Total != 1 {
		t.Errorf("total = %d, want 1", stats.Total)
	}
}

func TestFilterIncidents(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	mkRec := func(id string, sev security.Severity, st security.IncidentStatus) *security.IncidentRecord {
		return &security.IncidentRecord{
			ID: id, Severity: sev, Status: st, CreatedAt: now,
		}
	}
	recs := []*security.IncidentRecord{
		mkRec("a", security.SeveritySEV0, security.IncidentOpen),
		mkRec("b", security.SeveritySEV1, security.IncidentResolved),
		mkRec("c", security.SeveritySEV2, security.IncidentEscalated),
		mkRec("d", security.SeveritySEV1, security.IncidentOpen),
	}

	cases := []struct {
		name              string
		sev, statusFilter string
		all               bool
		want              []string
	}{
		{"active only", "", "", false, []string{"a", "c", "d"}},
		{"all", "", "", true, []string{"a", "b", "c", "d"}},
		{"filter sev-1", "SEV-1", "", true, []string{"b", "d"}},
		{"filter open", "", "open", true, []string{"a", "d"}},
		{"both filters", "SEV-1", "open", true, []string{"d"}},
		{"no match", "SEV-0", "resolved", true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterIncidents(recs, tc.sev, tc.statusFilter, tc.all)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d records, want %d", len(got), len(tc.want))
			}
			for i, w := range tc.want {
				if got[i].ID != w {
					t.Errorf("record %d: ID = %q, want %q", i, got[i].ID, w)
				}
			}
		})
	}
}
