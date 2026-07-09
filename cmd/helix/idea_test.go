package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/ideation"
)

func TestRunIdea_Help(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIdea([]string{"help"}, &stdout, &stderr)
	if rc != ideaExitOK {
		t.Fatalf("rc=%d stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "helix idea") {
		t.Fatalf("expected help text, got %q", stdout.String())
	}
}

func TestRunIdea_CaptureMissingTitle(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runIdea([]string{"capture", "--body", "no title"}, &stdout, &stderr)
	if rc != ideaExitError {
		t.Fatalf("rc=%d want error", rc)
	}
	if !strings.Contains(stderr.String(), "title") {
		t.Fatalf("expected title error, got %q", stderr.String())
	}
}

func TestRunIdea_E2E_CaptureValidatePrioritizePromote(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "ideas.jsonl")
	specDir := filepath.Join(dir, "specs")

	var stdout, stderr strings.Builder

	// capture
	rc := runIdea([]string{
		"capture",
		"--title", "Add rate limiting to auth",
		"--body", "Protect login endpoints from brute force with token bucket and audit logging.",
		"--tags", "auth,security",
		"--evidence", "incident:inc-42:login spam",
		"--store", storePath,
		"--json",
	}, &stdout, &stderr)
	if rc != ideaExitOK {
		t.Fatalf("capture rc=%d stderr=%s", rc, stderr.String())
	}
	var captured ideation.Idea
	if err := json.Unmarshal([]byte(stdout.String()), &captured); err != nil {
		t.Fatalf("capture json: %v out=%s", err, stdout.String())
	}
	if captured.ID == "" {
		t.Fatal("expected id")
	}
	id := captured.ID

	// list
	stdout.Reset()
	stderr.Reset()
	rc = runIdea([]string{"list", "--store", storePath, "--json"}, &stdout, &stderr)
	if rc != ideaExitOK {
		t.Fatalf("list rc=%d stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), id) {
		t.Fatalf("list missing id: %s", stdout.String())
	}

	// show
	stdout.Reset()
	stderr.Reset()
	rc = runIdea([]string{"show", id, "--store", storePath}, &stdout, &stderr)
	if rc != ideaExitOK {
		t.Fatalf("show rc=%d stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Add rate limiting") {
		t.Fatalf("show missing title: %s", stdout.String())
	}

	// validate
	stdout.Reset()
	stderr.Reset()
	rc = runIdea([]string{"validate", id, "--store", storePath, "--json"}, &stdout, &stderr)
	if rc != ideaExitOK {
		t.Fatalf("validate rc=%d stderr=%s", rc, stderr.String())
	}
	var report ideation.ValidationReport
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("validate json: %v out=%s", err, stdout.String())
	}
	if len(report.AgentsRun) < 2 {
		t.Fatalf("agents_run=%v", report.AgentsRun)
	}

	// prioritize
	stdout.Reset()
	stderr.Reset()
	rc = runIdea([]string{"prioritize", "--store", storePath, "--json"}, &stdout, &stderr)
	if rc != ideaExitOK {
		t.Fatalf("prioritize rc=%d stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"rank"`) {
		t.Fatalf("prioritize missing rank: %s", stdout.String())
	}

	// promote
	stdout.Reset()
	stderr.Reset()
	rc = runIdea([]string{
		"promote", id, "--to", "spec",
		"--store", storePath,
		"--spec-dir", specDir,
		"--json",
	}, &stdout, &stderr)
	if rc != ideaExitOK {
		t.Fatalf("promote rc=%d stderr=%s out=%s", rc, stderr.String(), stdout.String())
	}
	var promo map[string]interface{}
	if err := json.Unmarshal([]byte(stdout.String()), &promo); err != nil {
		t.Fatalf("promote json: %v out=%s", err, stdout.String())
	}
	specPath, _ := promo["spec_path"].(string)
	if specPath == "" {
		t.Fatalf("missing spec_path: %v", promo)
	}
	if _, err := os.Stat(specPath); err != nil {
		t.Fatalf("spec file missing: %v", err)
	}
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if !strings.Contains(string(data), "idea_ref:") {
		t.Fatalf("spec missing frontmatter: %s", data)
	}
}

func TestRunIdea_PromoteBlockedHighRisk(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "ideas.jsonl")
	store, err := ideation.NewStore(storePath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	idea := &ideation.Idea{
		Title:     "big bang rewrite everything",
		Body:      "just rewrite everything with a big bang migration simply and easily",
		RiskScore: 80,
		Status:    ideation.StatusValidated,
	}
	if err := store.Capture(idea); err != nil {
		t.Fatalf("capture: %v", err)
	}

	var stdout, stderr strings.Builder
	rc := runIdea([]string{
		"promote", idea.ID, "--to", "spec",
		"--store", storePath,
		"--spec-dir", filepath.Join(dir, "specs"),
	}, &stdout, &stderr)
	if rc != ideaExitError {
		t.Fatalf("rc=%d want error, out=%s err=%s", rc, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "risk_score") {
		t.Fatalf("expected risk block message, got %q", stderr.String())
	}
}

func TestParseIdeaFlags_Evidence(t *testing.T) {
	f, help, rc := parseIdeaFlags([]string{
		"capture", "--title", "t", "--body", "b",
		"--evidence", "file:pkg/auth/rate.go:rate limiter",
	})
	if help || rc != ideaExitOK {
		t.Fatalf("parse failed help=%v rc=%d", help, rc)
	}
	if len(f.evidence) != 1 || f.evidence[0].Type != "file" {
		t.Fatalf("evidence=%+v", f.evidence)
	}
}

func TestSlugify(t *testing.T) {
	if got := slugify("Add Rate Limiting!"); got != "add-rate-limiting" {
		t.Fatalf("slugify = %q", got)
	}
	if got := slugify("   "); got != "idea" {
		t.Fatalf("empty slug = %q", got)
	}
}

func TestRunIdeaWithDryRun_Capture(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "ideas.jsonl")
	var stdout, stderr strings.Builder
	err := runIdeaWithDryRun([]string{
		"capture", "--title", "Dry", "--body", "Would not write",
		"--store", storePath,
	}, &stdout, &stderr, true)
	if err != nil {
		t.Fatalf("dry-run: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "DRY RUN") {
		t.Fatalf("expected dry run marker: %s", stdout.String())
	}
	// store should not exist or be empty
	if _, err := os.Stat(storePath); err == nil {
		data, _ := os.ReadFile(storePath)
		if len(strings.TrimSpace(string(data))) > 0 {
			t.Fatalf("store should be empty on dry-run, got %q", data)
		}
	}
}
