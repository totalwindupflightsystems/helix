// Tests for cmd/helix/dispatcher.go.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	disp "github.com/totalwindupflightsystems/helix/pkg/dispatcher"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// -----------------------------------------------------------------------------
// Test fixtures
// -----------------------------------------------------------------------------

// writeTempSpec writes a minimal spec markdown to a temp dir and returns the path.
func writeTempSpec(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test-spec.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return path
}

// minimalSpec with two tasks for decompose + cost guard testing.
const minimalSpec = `## Feature: Decompose this spec

First section body.

## Feature: Build the dispatcher CLI

Second section body.
`

// -----------------------------------------------------------------------------
// parseDispatcherFlags
// -----------------------------------------------------------------------------

func TestParseDispatcherFlags_Help(t *testing.T) {
	f, helpWanted, rc := parseDispatcherFlags([]string{"--help"})
	if rc != dispExitOK || !helpWanted {
		t.Fatalf("expected rc=0 + helpWanted=true, got rc=%d help=%v", rc, helpWanted)
	}
	if f.subcommand != "help" {
		t.Fatalf("expected subcommand=help (default), got %q", f.subcommand)
	}
}

func TestParseDispatcherFlags_Status(t *testing.T) {
	f, helpWanted, rc := parseDispatcherFlags([]string{"status", "--spec", "/tmp/x.md", "--json"})
	if rc != dispExitOK || helpWanted {
		t.Fatalf("rc=%d help=%v", rc, helpWanted)
	}
	if f.subcommand != "status" {
		t.Fatalf("subcommand=%q", f.subcommand)
	}
	if f.specPath != "/tmp/x.md" {
		t.Fatalf("specPath=%q", f.specPath)
	}
	if !f.jsonOut {
		t.Fatalf("jsonOut=false")
	}
}

func TestParseDispatcherFlags_AllFlags(t *testing.T) {
	f, _, rc := parseDispatcherFlags([]string{
		"tick", "--spec", "/tmp/spec.md",
		"--agent", `{"name":"alice","capability":"go","max_load":3,"current_load":0}`,
		"--tier", "Trusted",
	})
	if rc != dispExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if f.subcommand != "tick" || f.specPath != "/tmp/spec.md" ||
		f.agentJSON == "" || f.tier != "Trusted" {
		t.Fatalf("flags not parsed: %+v", f)
	}
}

func TestParseDispatcherFlags_MissingSpecArg(t *testing.T) {
	_, _, rc := parseDispatcherFlags([]string{"status", "--spec"})
	if rc != dispExitError {
		t.Fatalf("expected rc=2 for missing --spec value, got %d", rc)
	}
}

func TestParseDispatcherFlags_MissingAgentArg(t *testing.T) {
	_, _, rc := parseDispatcherFlags([]string{"status", "--agent"})
	if rc != dispExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

func TestParseDispatcherFlags_UnknownFlag(t *testing.T) {
	_, _, rc := parseDispatcherFlags([]string{"status", "--no-such-flag"})
	if rc != dispExitError {
		t.Fatalf("expected rc=2 for unknown flag, got %d", rc)
	}
}

func TestParseDispatcherFlags_MultiplePositional(t *testing.T) {
	_, _, rc := parseDispatcherFlags([]string{"status", "--spec", "x", "extra"})
	if rc != dispExitError {
		t.Fatalf("expected rc=2 for extra positional, got %d", rc)
	}
}

// -----------------------------------------------------------------------------
// parseAgentJSON
// -----------------------------------------------------------------------------

func TestParseAgentJSON_OK(t *testing.T) {
	a, err := parseAgentJSON(`{"name":"alice","capability":"go","max_load":3,"current_load":1}`)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if a.Name != "alice" || a.Capability != "go" || a.MaxLoad != 3 || a.CurrentLoad != 1 {
		t.Fatalf("got %+v", a)
	}
	if !a.CanAcceptLoad() {
		t.Fatalf("expected CanAcceptLoad=true")
	}
}

func TestParseAgentJSON_DefaultMaxLoad(t *testing.T) {
	a, err := parseAgentJSON(`{"name":"alice","capability":"go"}`)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if a.MaxLoad != 3 {
		t.Fatalf("expected default MaxLoad=3, got %d", a.MaxLoad)
	}
}

func TestParseAgentJSON_MissingName(t *testing.T) {
	_, err := parseAgentJSON(`{"capability":"go"}`)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestParseAgentJSON_MissingCapability(t *testing.T) {
	_, err := parseAgentJSON(`{"name":"alice"}`)
	if err == nil {
		t.Fatal("expected error for missing capability")
	}
}

func TestParseAgentJSON_MalformedJSON(t *testing.T) {
	_, err := parseAgentJSON(`{not json`)
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
}

// -----------------------------------------------------------------------------
// parseTrustTier
// -----------------------------------------------------------------------------

func TestParseTrustTier_AllValid(t *testing.T) {
	cases := []struct {
		s    string
		want string
	}{
		{"provisional", "provisional"},
		{"PROVISIONAL", "provisional"},
		{"observed", "observed"},
		{"trusted", "trusted"},
		{"Veteran", "veteran"},
	}
	for _, c := range cases {
		got, err := parseTrustTier(c.s)
		if err != nil {
			t.Fatalf("%q: err=%v", c.s, err)
		}
		if string(got) != c.want {
			t.Fatalf("%q: got %q want %q", c.s, got, c.want)
		}
	}
}

func TestParseTrustTier_Invalid(t *testing.T) {
	_, err := parseTrustTier("legendary")
	if err == nil {
		t.Fatal("expected error for unknown tier")
	}
}

// -----------------------------------------------------------------------------
// runDispatcher — subcommand routing
// -----------------------------------------------------------------------------

func TestRunDispatcher_Help(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{"help"}, &out, &errBuf)
	if rc != dispExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "helix dispatcher") {
		t.Fatalf("help missing — out=%q", out.String())
	}
	if !strings.Contains(out.String(), "status") || !strings.Contains(out.String(), "tick") || !strings.Contains(out.String(), "list-tasks") {
		t.Fatalf("help missing subcommands: %q", out.String())
	}
}

func TestRunDispatcher_UnknownSubcommand(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{"bogus"}, &out, &errBuf)
	if rc != dispExitError {
		t.Fatalf("expected rc=2, got %d", rc)
	}
	if !strings.Contains(errBuf.String(), "unknown subcommand") {
		t.Fatalf("expected error message, got %q", errBuf.String())
	}
}

func TestRunDispatcher_DefaultIsHelp(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{}, &out, &errBuf)
	if rc != dispExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(out.String(), "helix dispatcher") {
		t.Fatalf("expected help text, got %q", out.String())
	}
}

// -----------------------------------------------------------------------------
// runDispatcher — list-tasks
// -----------------------------------------------------------------------------

func TestRunDispatcher_ListTasks_OK(t *testing.T) {
	spec := writeTempSpec(t, minimalSpec)
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{"list-tasks", "--spec", spec}, &out, &errBuf)
	if rc != dispExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	outStr := out.String()
	if !strings.Contains(outStr, "task-001") || !strings.Contains(outStr, "task-002") {
		t.Fatalf("expected task IDs, got %q", outStr)
	}
	if !strings.Contains(outStr, "Decompose this spec") || !strings.Contains(outStr, "Build the dispatcher CLI") {
		t.Fatalf("missing task descriptions: %q", outStr)
	}
}

func TestRunDispatcher_ListTasks_JSON(t *testing.T) {
	spec := writeTempSpec(t, minimalSpec)
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{"list-tasks", "--spec", spec, "--json"}, &out, &errBuf)
	if rc != dispExitOK {
		t.Fatalf("rc=%d", rc)
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v out=%q", err, out.String())
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(got))
	}
	if got[0]["id"] != "task-001" {
		t.Fatalf("first task id=%v", got[0]["id"])
	}
}

func TestRunDispatcher_ListTasks_MissingSpec(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{"list-tasks"}, &out, &errBuf)
	if rc != dispExitError {
		t.Fatalf("rc=%d", rc)
	}
}

func TestRunDispatcher_ListTasks_SpecNotFound(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{"list-tasks", "--spec", "/nonexistent/spec.md"}, &out, &errBuf)
	if rc != dispExitError {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(errBuf.String(), "not found") {
		t.Fatalf("expected 'not found', got %q", errBuf.String())
	}
}

// -----------------------------------------------------------------------------
// runDispatcher — status
// -----------------------------------------------------------------------------

func TestRunDispatcher_Status_OK(t *testing.T) {
	spec := writeTempSpec(t, minimalSpec)
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{"status", "--spec", spec}, &out, &errBuf)
	if rc != dispExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	outStr := out.String()
	if !strings.Contains(outStr, "Spec:") {
		t.Fatalf("missing Spec line: %q", outStr)
	}
	if !strings.Contains(outStr, "Tasks: 2") {
		t.Fatalf("expected 2 tasks: %q", outStr)
	}
	if !strings.Contains(outStr, "ID") || !strings.Contains(outStr, "DESCRIPTION") {
		t.Fatalf("expected table header: %q", outStr)
	}
}

func TestRunDispatcher_Status_WithCostGuard_JSON(t *testing.T) {
	spec := writeTempSpec(t, minimalSpec)
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{
		"status", "--spec", spec, "--json",
		"--agent", `{"name":"alice","capability":"decompose","max_load":3,"current_load":0}`,
		"--tier", "trusted",
	}, &out, &errBuf)
	if rc != dispExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	var report dispStatusReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON: %v out=%q", err, out.String())
	}
	if report.TaskCount != 2 {
		t.Fatalf("task_count=%d", report.TaskCount)
	}
	if report.CostGuard == nil {
		t.Fatalf("cost_guard absent")
	}
	if report.AgentProfile == nil || report.AgentProfile.Name != "alice" {
		t.Fatalf("agent_profile=%+v", report.AgentProfile)
	}
}

func TestRunDispatcher_Status_HumanReadableWithCostGuard(t *testing.T) {
	spec := writeTempSpec(t, minimalSpec)
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{
		"status", "--spec", spec,
		"--agent", `{"name":"bob","capability":"x","max_load":3,"current_load":0}`,
		"--tier", "observed",
	}, &out, &errBuf)
	if rc != dispExitOK && rc != dispExitBlock {
		t.Fatalf("unexpected rc=%d err=%q", rc, errBuf.String())
	}
	outStr := out.String()
	if !strings.Contains(outStr, "Cost guard") {
		t.Fatalf("missing cost-guard block: %q", outStr)
	}
	if !strings.Contains(outStr, "Decision:") {
		t.Fatalf("missing Decision line: %q", outStr)
	}
}

func TestRunDispatcher_Status_OnlyOneOfAgentTierProvided(t *testing.T) {
	spec := writeTempSpec(t, minimalSpec)
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{"status", "--spec", spec, "--agent", `{"name":"a","capability":"b"}`}, &out, &errBuf)
	if rc != dispExitError {
		t.Fatalf("expected rc=2 when only --agent given, got %d", rc)
	}
	if !strings.Contains(errBuf.String(), "must be provided together") {
		t.Fatalf("expected 'must be provided together', got %q", errBuf.String())
	}
}

func TestRunDispatcher_Status_InvalidTier(t *testing.T) {
	spec := writeTempSpec(t, minimalSpec)
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{
		"status", "--spec", spec,
		"--agent", `{"name":"a","capability":"b"}`,
		"--tier", "mythical",
	}, &out, &errBuf)
	if rc != dispExitError {
		t.Fatalf("expected rc=2 for invalid tier, got %d", rc)
	}
}

func TestRunDispatcher_Status_InvalidAgentJSON(t *testing.T) {
	spec := writeTempSpec(t, minimalSpec)
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{
		"status", "--spec", spec,
		"--agent", `{not json`,
		"--tier", "trusted",
	}, &out, &errBuf)
	if rc != dispExitError {
		t.Fatalf("expected rc=2 for invalid JSON, got %d", rc)
	}
}

// -----------------------------------------------------------------------------
// runDispatcher — tick
// -----------------------------------------------------------------------------

func TestRunDispatcher_Tick_OK(t *testing.T) {
	spec := writeTempSpec(t, minimalSpec)
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{
		"tick", "--spec", spec,
		"--agent", `{"name":"alice","capability":"decompose","max_load":3,"current_load":0}`,
	}, &out, &errBuf)
	if rc != dispExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	outStr := out.String()
	if !strings.Contains(outStr, "dispatched") {
		t.Fatalf("missing dispatched line: %q", outStr)
	}
	if !strings.Contains(outStr, "agent") && !strings.Contains(outStr, "alice") {
		t.Fatalf("missing agent info: %q", outStr)
	}
}

func TestRunDispatcher_Tick_JSON(t *testing.T) {
	spec := writeTempSpec(t, minimalSpec)
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{
		"tick", "--spec", spec, "--json",
		"--agent", `{"name":"alice","capability":"decompose","max_load":3,"current_load":0}`,
	}, &out, &errBuf)
	if rc != dispExitOK {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON: %v out=%q", err, out.String())
	}
	if result["task_id"] != "task-001" {
		t.Fatalf("task_id=%v", result["task_id"])
	}
	if result["agent_name"] != "alice" {
		t.Fatalf("agent_name=%v", result["agent_name"])
	}
	if _, ok := result["steps"]; !ok {
		t.Fatalf("missing steps in JSON: %v", result)
	}
}

func TestRunDispatcher_Tick_MissingFlags(t *testing.T) {
	spec := writeTempSpec(t, minimalSpec)
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{"tick", "--spec", spec}, &out, &errBuf)
	if rc != dispExitError {
		t.Fatalf("rc=%d", rc)
	}
}

func TestRunDispatcher_Tick_MissingSpec(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{"tick", "--agent", `{"name":"a","capability":"b"}`}, &out, &errBuf)
	if rc != dispExitError {
		t.Fatalf("rc=%d", rc)
	}
}

func TestRunDispatcher_Tick_BadAgentJSON(t *testing.T) {
	spec := writeTempSpec(t, minimalSpec)
	var out, errBuf bytes.Buffer
	rc := runDispatcher([]string{"tick", "--spec", spec, "--agent", "{bad"}, &out, &errBuf)
	if rc != dispExitError {
		t.Fatalf("rc=%d", rc)
	}
}

// -----------------------------------------------------------------------------
// runDispatcherWithDryRun — global --dry-run plumbing
// -----------------------------------------------------------------------------

func TestRunDispatcherWithDryRun_HelpReturnsNil(t *testing.T) {
	var out, errBuf bytes.Buffer
	if err := runDispatcherWithDryRun([]string{"help"}, &out, &errBuf, true); err != nil {
		t.Fatalf("help with --dry-run should be nil error, got %v", err)
	}
}

func TestRunDispatcherWithDryRun_UnknownReturnsError(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := runDispatcherWithDryRun([]string{"bogus"}, &out, &errBuf, false)
	if err == nil {
		t.Fatal("expected errExit wrapping unknown subcommand rc=2")
	}
	if !strings.Contains(err.Error(), "exit") {
		t.Fatalf("expected exit code wrapper, got %v", err)
	}
}

func TestRunDispatcherWithDryRun_ListTasksReturnsNil(t *testing.T) {
	spec := writeTempSpec(t, minimalSpec)
	var out, errBuf bytes.Buffer
	if err := runDispatcherWithDryRun([]string{"list-tasks", "--spec", spec}, &out, &errBuf, true); err != nil {
		t.Fatalf("dry-run should be safe (no side-effects), got %v", err)
	}
}

// -----------------------------------------------------------------------------
// oneLine helper
// -----------------------------------------------------------------------------

func TestOneLine_CollapsesWhitespace(t *testing.T) {
	got := oneLine("hello\n\n  world\n\t\tfoo")
	if got != "hello world foo" {
		t.Fatalf("got %q", got)
	}
}

func TestOneLine_TruncatesLong(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := oneLine(long)
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
	if len(got) > 85 {
		t.Fatalf("expected truncation around 80 chars, got len=%d", len(got))
	}
}

func TestOneLine_Empty(t *testing.T) {
	if got := oneLine(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

// -----------------------------------------------------------------------------
// Table printer
// -----------------------------------------------------------------------------

func TestPrintTaskTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	printTaskTable(&buf, nil)
	if !strings.Contains(buf.String(), "no tasks") {
		t.Fatalf("expected 'no tasks' for empty input, got %q", buf.String())
	}
}

func TestPrintTaskTable_WithRows(t *testing.T) {
	var buf bytes.Buffer
	printTaskTable(&buf, []dispTaskSummary{
		{ID: "task-001", Description: "First", Priority: 1, Status: "pending"},
		{ID: "task-002", Description: "Second", Priority: 2, Status: "pending"},
	})
	out := buf.String()
	for _, want := range []string{"task-001", "task-002", "First", "Second", "ID", "DESCRIPTION", "STATUS"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// -----------------------------------------------------------------------------
// Cost-guard printer
// -----------------------------------------------------------------------------

func TestPrintCostGuardReport_OK(t *testing.T) {
	var buf bytes.Buffer
	printCostGuardReport(&buf, disp.CostGuardResult{
		Decision:      disp.CostGuardApproved,
		AgentID:       "alice",
		Tier:          trust.TierTrusted,
		CostCapPerJob: 100,
		EstimatedCost: 0.5,
		Reason:        "within cap",
	}, disp.AgentProfile{Name: "alice"})
	out := buf.String()
	for _, want := range []string{"APPROVED", "alice", "trusted", "$100.00", "$0.5000", "within cap"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
