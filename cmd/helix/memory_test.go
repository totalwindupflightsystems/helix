package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunMemory_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMemory([]string{"--help"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("help exit code: got %d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "Hivemind memory bank lifecycle") {
		t.Errorf("expected help header in stdout, got: %q", stdout.String())
	}
}

func TestRunMemory_InboxStatus_Empty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMemory([]string{"inbox-status"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("inbox-status exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Inbox: 0 events") {
		t.Errorf("expected 'Inbox: 0 events' for empty inbox; got:\n%s", out)
	}
	if !strings.Contains(out, "batch_window") {
		t.Errorf("expected batch_window in output; got:\n%s", out)
	}
}

func TestRunMemory_InboxStatus_WithEvent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMemory([]string{
		"inbox-status",
		"--agent", "kara",
		"--repo", "org/repo",
		"--event-type", "merge",
		"--summary", "merged PR #123",
	}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("inbox-status with event exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Inbox: 1 events") {
		t.Errorf("expected 'Inbox: 1 events' after append; got:\n%s", out)
	}
}

func TestRunMemory_InboxStatusJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMemory([]string{"inbox-status", "--json"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("inbox-status --json exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(out, "{") {
		t.Errorf("expected JSON object, got: %s", out)
	}
	if !strings.Contains(out, `"event_count":0`) {
		t.Errorf("expected event_count:0 in JSON: %s", out)
	}
	if !strings.Contains(out, `"batch_window":`) {
		t.Errorf("expected batch_window in JSON: %s", out)
	}
}

func TestRunMemory_Append_OK(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMemory([]string{
		"append",
		"--agent", "kara",
		"--repo", "org/repo",
		"--event-type", "merge",
		"--summary", "merged PR #123",
		"--file", "src/main.go",
		"--operation", "write",
		"--tags", "merge,pr-123",
	}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("append exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "APPENDED") {
		t.Errorf("expected APPENDED in output; got:\n%s", out)
	}
	if !strings.Contains(out, "kara") {
		t.Errorf("expected agent name in output; got:\n%s", out)
	}
}

func TestRunMemory_AppendJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMemory([]string{
		"append",
		"--json",
		"--agent", "kara",
		"--repo", "org/repo",
		"--event-type", "gate_failure",
		"--summary", "merge gate blocked PR",
	}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("append --json exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(out, "{") {
		t.Errorf("expected JSON object, got: %s", out)
	}
	if !strings.Contains(out, `"id":"evt-`) {
		t.Errorf("expected id with evt- prefix; got: %s", out)
	}
	if !strings.Contains(out, `"agent":"kara"`) {
		t.Errorf("expected agent kara in JSON: %s", out)
	}
	if !strings.Contains(out, `"event_type":"gate_failure"`) {
		t.Errorf("expected event_type gate_failure in JSON: %s", out)
	}
}

func TestRunMemory_Append_MissingFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no agent", []string{"append", "--repo", "x", "--event-type", "y", "--summary", "z"}},
		{"no repo", []string{"append", "--agent", "x", "--event-type", "y", "--summary", "z"}},
		{"no event-type", []string{"append", "--agent", "x", "--repo", "y", "--summary", "z"}},
		{"no summary", []string{"append", "--agent", "x", "--repo", "y", "--event-type", "z"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			rc := runMemory(tc.args, &stdout, &stderr)
			if rc == 0 {
				t.Fatalf("expected non-zero exit for %s", tc.name)
			}
			if !strings.Contains(stderr.String(), "requires") {
				t.Errorf("expected 'requires' error message; got: %s", stderr.String())
			}
		})
	}
}

func TestRunMemory_Compile_Empty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMemory([]string{"compile"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("compile empty exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "0 compiled entries") {
		t.Errorf("expected '0 compiled entries' for empty inbox; got: %s", stdout.String())
	}
}

func TestRunMemory_Compile_WithEvent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMemory([]string{
		"compile",
		"--agent", "kara",
		"--repo", "org/repo",
		"--event-type", "merge",
		"--summary", "merged PR",
	}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("compile with event exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "compiled entries") {
		t.Errorf("expected compile output; got: %s", stdout.String())
	}
}

func TestRunMemory_Run_OK(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMemory([]string{
		"run",
		"--agent", "kara",
		"--repo", "org/repo",
		"--event-type", "merge",
		"--summary", "merged PR",
	}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("run exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Lifecycle run") {
		t.Errorf("expected lifecycle run header; got: %s", stdout.String())
	}
}

func TestRunMemory_RunJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMemory([]string{
		"run", "--json",
		"--agent", "kara",
		"--repo", "org/repo",
		"--event-type", "merge",
		"--summary", "merged PR",
	}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("run --json exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(out, "{") {
		t.Errorf("expected JSON object, got: %s", out)
	}
	if !strings.Contains(out, `"persisted_count":`) {
		t.Errorf("expected persisted_count in JSON: %s", out)
	}
}

func TestRunMemory_List_OK(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMemory([]string{
		"list",
		"--agent", "kara",
		"--repo", "org/repo",
		"--event-type", "merge",
		"--summary", "merged PR",
	}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("list exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "persisted entries") {
		t.Errorf("expected 'persisted entries' header; got: %s", stdout.String())
	}
}

func TestRunMemory_Index_WithAgent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMemory([]string{
		"index",
		"--agent", "kara",
	}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("index exit code: got %d, want 0; stderr=%s", rc, stderr.String())
	}
	// Either we got a namespace index output, or a "Provide --agent" hint
	out := stdout.String()
	if !strings.Contains(out, "agents/kara") && !strings.Contains(out, "Provide --agent") {
		t.Errorf("expected namespace or hint in output; got: %s", out)
	}
}

func TestParseMemoryFlags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantSub   string
		wantAgent string
		wantRepo  string
		wantType  string
		wantSum   string
		wantJSON  bool
		wantRC    int
	}{
		{"default", []string{}, "inbox-status", "", "", "", "", false, 0},
		{"append sub", []string{"append"}, "append", "", "", "", "", false, 0},
		{"compile sub", []string{"compile"}, "compile", "", "", "", "", false, 0},
		{"run sub", []string{"run"}, "run", "", "", "", "", false, 0},
		{"list sub", []string{"list"}, "list", "", "", "", "", false, 0},
		{"index sub", []string{"index"}, "index", "", "", "", "", false, 0},
		{"inbox-status sub", []string{"inbox-status"}, "inbox-status", "", "", "", "", false, 0},
		{"with flags", []string{"append", "--agent", "a", "--repo", "r", "--event-type", "t", "--summary", "s"},
			"append", "a", "r", "t", "s", false, 0},
		{"with tags", []string{"append", "--tags", "a,b,c", "--agent", "a", "--repo", "r", "--event-type", "t", "--summary", "s"},
			"append", "a", "r", "t", "s", false, 0},
		{"with = form", []string{"append", "--agent=foo"}, "append", "foo", "", "", "", false, 0},
		{"json flag", []string{"list", "--json"}, "list", "", "", "", "", true, 0},
		{"unknown sub", []string{"badname"}, "", "", "", "", "", false, 2},
		{"unknown flag", []string{"list", "--bad"}, "", "", "", "", "", false, 2},
		{"help", []string{"--help"}, "inbox-status", "", "", "", "", false, 0},
		{"agent missing value", []string{"append", "--agent"}, "append", "", "", "", "", false, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, rc := parseMemoryFlags(tc.args, &bytes.Buffer{}, &bytes.Buffer{})
			if rc != tc.wantRC {
				t.Fatalf("rc: got %d, want %d", rc, tc.wantRC)
			}
			if rc != 0 {
				return
			}
			if f.subcommand != tc.wantSub {
				t.Errorf("subcommand: got %q, want %q", f.subcommand, tc.wantSub)
			}
			if f.agent != tc.wantAgent {
				t.Errorf("agent: got %q, want %q", f.agent, tc.wantAgent)
			}
			if f.repo != tc.wantRepo {
				t.Errorf("repo: got %q, want %q", f.repo, tc.wantRepo)
			}
			if f.eventType != tc.wantType {
				t.Errorf("eventType: got %q, want %q", f.eventType, tc.wantType)
			}
			if f.summary != tc.wantSum {
				t.Errorf("summary: got %q, want %q", f.summary, tc.wantSum)
			}
			if f.jsonOut != tc.wantJSON {
				t.Errorf("jsonOut: got %v, want %v", f.jsonOut, tc.wantJSON)
			}
		})
	}
}

func TestRunMemoryWithDryRun_PassThrough(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runMemoryWithDryRun([]string{"inbox-status"}, &stdout, &stderr, true)
	if err != nil {
		t.Fatalf("expected nil error from runMemoryWithDryRun on inbox-status; got %v", err)
	}
	if !strings.Contains(stdout.String(), "Inbox: 0 events") {
		t.Errorf("expected inbox-status output via WithDryRun; got: %s", stdout.String())
	}
}
