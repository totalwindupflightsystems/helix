package dispatcher

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// repoRoot walks up from this test file to the module root (the directory that
// holds go.mod) so the corpus test never hardcodes a checkout path.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed: cannot locate the test file")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod in any parent of %s", filepath.Dir(file))
		}
		dir = parent
	}
}

// writeTempSpec writes body to a temp file named name and returns its path.
func writeTempSpec(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", name, err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Every spec in the repository's own corpus must decompose
// ---------------------------------------------------------------------------

// TestDecomposeSpec_RepoSpecsAllDecompose is the DF-HELIX-7 regression: the
// repo's own specs/*.md must all decompose without error, produce at least one
// task each, keep every Description non-empty, and carry a section body.
func TestDecomposeSpec_RepoSpecsAllDecompose(t *testing.T) {
	specsDir := filepath.Join(repoRoot(t), "specs")
	paths, err := filepath.Glob(filepath.Join(specsDir, "*.md"))
	if err != nil {
		t.Fatalf("glob %s: %v", specsDir, err)
	}
	if len(paths) == 0 {
		if _, statErr := os.Stat(specsDir); statErr != nil {
			t.Skipf("specs/ absent at %s: nothing to check", specsDir)
		}
		t.Fatalf("specs/ exists at %s but holds no markdown specs: the corpus check would be vacuous", specsDir)
	}

	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			tasks, err := DecomposeSpec(path)
			if err != nil {
				t.Fatalf("DecomposeSpec(%s): %v", filepath.Base(path), err)
			}
			if len(tasks) == 0 {
				t.Fatalf("DecomposeSpec(%s) = 0 tasks, want >= 1", filepath.Base(path))
			}

			seenIDs := make(map[string]bool, len(tasks))
			withBody := 0
			for i, task := range tasks {
				if strings.TrimSpace(task.Description) == "" {
					t.Errorf("task[%d] (%s) Description is empty", i, task.ID)
				}
				if task.ID == "" {
					t.Errorf("task[%d] ID is empty", i)
				}
				if seenIDs[task.ID] {
					t.Errorf("task[%d] ID %q is duplicated", i, task.ID)
				}
				seenIDs[task.ID] = true
				if task.Status != StatusPending {
					t.Errorf("task[%d] Status = %q, want %q", i, task.Status, StatusPending)
				}
				if task.SpecRef != path {
					t.Errorf("task[%d] SpecRef = %q, want %q", i, task.SpecRef, path)
				}
				if strings.TrimSpace(task.Body) != "" {
					withBody++
				}
			}
			if withBody == 0 {
				t.Errorf("no task carries a body: the spec section text was dropped")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Fallback ladder
// ---------------------------------------------------------------------------

// TestDecomposeSpec_TitledH1NumberedSections is the focused regression for the
// 15-failure shape: a titled H1 plus numbered H2 sections that carry no
// Phase/Feature keyword.
func TestDecomposeSpec_TitledH1NumberedSections(t *testing.T) {
	specPath := writeTempSpec(t, "deployment.md", `# Helix Platform — Deployment Specification

This document describes how the platform is deployed.

## 1. Mission

Deploy the platform reliably. Operators must verify every container starts.

## 2. Topology

Run each service on its own host. Configure the reverse proxy per service.

## 3. Rollback

Revert the previous release and restart the affected containers.
`)

	tasks, err := DecomposeSpec(specPath)
	if err != nil {
		t.Fatalf("DecomposeSpec() error: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("DecomposeSpec() returned %d tasks, want 3 (one per H2)", len(tasks))
	}

	wantDescs := []string{"1. Mission", "2. Topology", "3. Rollback"}
	for i, want := range wantDescs {
		if tasks[i].Description != want {
			t.Errorf("task[%d].Description = %q, want %q", i, tasks[i].Description, want)
		}
		if tasks[i].Priority != i+1 {
			t.Errorf("task[%d].Priority = %d, want %d", i, tasks[i].Priority, i+1)
		}
		if strings.TrimSpace(tasks[i].Body) == "" {
			t.Errorf("task[%d].Body is empty", i)
		}
	}

	if !strings.Contains(tasks[0].Body, "reliably") {
		t.Errorf("task[0].Body should hold the section prose, got %q", tasks[0].Body)
	}
	if strings.Contains(tasks[0].Body, "Rollback") {
		t.Errorf("task[0].Body leaked the next section: %q", tasks[0].Body)
	}
}

// TestDecomposeSpec_HelixFeatureH1StillOneTask locks the legacy H1 rule: a
// "# Helix Feature 1 — X" document yields a single task for that H1, with its
// H2s captured as the body.
func TestDecomposeSpec_HelixFeatureH1StillOneTask(t *testing.T) {
	specPath := writeTempSpec(t, "agent-identity.md", `# Helix Feature 1 — Agent Identity in Forgejo

## 1. Mission

Provide an identity for every agent. Create a Forgejo user per agent.

## 2. Scope

In scope for v1: provisioning and listing.
`)

	tasks, err := DecomposeSpec(specPath)
	if err != nil {
		t.Fatalf("DecomposeSpec() error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("DecomposeSpec() returned %d tasks, want 1", len(tasks))
	}
	if tasks[0].Description != "Helix Feature 1 — Agent Identity in Forgejo" {
		t.Errorf("task[0].Description = %q", tasks[0].Description)
	}
	if !strings.Contains(tasks[0].Body, "Mission") || !strings.Contains(tasks[0].Body, "Scope") {
		t.Errorf("task[0].Body should span both H2 sections, got %q", tasks[0].Body)
	}
}

// TestDecomposeSpec_PhaseHeadingsPreserved locks the rest of the legacy rule:
// H2 headings containing PHASE keep their task shape.
func TestDecomposeSpec_PhaseHeadingsPreserved(t *testing.T) {
	specPath := writeTempSpec(t, "phases.md", `# Build Order

## 1. Dependency Graph

Lay out the graph first.

## 2. Phase 0: Infrastructure Prerequisites

Provision the hosts.

## 3. Phase 1: Forgejo

Deploy Forgejo as the foundation.
`)

	tasks, err := DecomposeSpec(specPath)
	if err != nil {
		t.Fatalf("DecomposeSpec() error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("DecomposeSpec() returned %d tasks, want 2 (only PHASE H2s)", len(tasks))
	}
	if tasks[0].Description != "2. Phase 0: Infrastructure Prerequisites" {
		t.Errorf("task[0].Description = %q", tasks[0].Description)
	}
	if tasks[0].Priority != 1 || tasks[1].Priority != 2 {
		t.Errorf("priorities = %d,%d; want 1,2", tasks[0].Priority, tasks[1].Priority)
	}
}

// TestDecomposeSpec_WholeFileWithoutH2 covers ladder step 3: no H2 sections
// means one task titled from the H1 whose body is the rest of the document.
func TestDecomposeSpec_WholeFileWithoutH2(t *testing.T) {
	specPath := writeTempSpec(t, "notes.md", `# Sandbox Threat Model

An agent can reach the network unless the sandbox denies egress.

The sandbox must isolate the filesystem per session.
`)

	tasks, err := DecomposeSpec(specPath)
	if err != nil {
		t.Fatalf("DecomposeSpec() error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("DecomposeSpec() returned %d tasks, want 1", len(tasks))
	}
	if tasks[0].Description != "Sandbox Threat Model" {
		t.Errorf("task[0].Description = %q, want the H1 text", tasks[0].Description)
	}
	if !strings.Contains(tasks[0].Body, "denies egress") {
		t.Errorf("task[0].Body = %q, want the document text", tasks[0].Body)
	}
}

// TestDecomposeSpec_NoHeadingsUsesFilename covers the no-H1 tail of ladder
// step 3.
func TestDecomposeSpec_NoHeadingsUsesFilename(t *testing.T) {
	specPath := writeTempSpec(t, "runbook-notes.md", `Operators must restart the daemon after a config change.
`)

	tasks, err := DecomposeSpec(specPath)
	if err != nil {
		t.Fatalf("DecomposeSpec() error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("DecomposeSpec() returned %d tasks, want 1", len(tasks))
	}
	if tasks[0].Description != "runbook notes" {
		t.Errorf("task[0].Description = %q, want the filename-derived title", tasks[0].Description)
	}
}

// TestDecomposeSpec_EmptyInputStillFails keeps ErrDecomposeFailed for the only
// input class that may still fail: a document with no content.
func TestDecomposeSpec_EmptyInputStillFails(t *testing.T) {
	specPath := writeTempSpec(t, "blank.md", "  \n\t\n\n")

	_, err := DecomposeSpec(specPath)
	if err == nil {
		t.Fatal("DecomposeSpec() on a blank file = nil error, want ErrDecomposeFailed")
	}
	if !errors.Is(err, ErrDecomposeFailed) {
		t.Fatalf("DecomposeSpec() error = %v, want ErrDecomposeFailed", err)
	}
}

// TestDecomposeSpec_FencedHeadingsAreNotBoundaries proves fenced code comment
// lines ("# restart the container") neither create tasks nor truncate a body.
func TestDecomposeSpec_FencedHeadingsAreNotBoundaries(t *testing.T) {
	specPath := writeTempSpec(t, "recovery.md", `# Error Recovery

## 1. Forgejo Recovery

Restart the container when it stops.

`+"```bash"+`
# restart forgejo
docker compose restart forgejo
`+"```"+`

## 2. Disk Recovery

Clean Docker images when the disk fills.
`)

	tasks, err := DecomposeSpec(specPath)
	if err != nil {
		t.Fatalf("DecomposeSpec() error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("DecomposeSpec() returned %d tasks, want 2 (fenced '# ' lines ignored)", len(tasks))
	}
	if !strings.Contains(tasks[0].Body, "docker compose restart forgejo") {
		t.Errorf("task[0].Body lost the fenced block: %q", tasks[0].Body)
	}
}

// ---------------------------------------------------------------------------
// Body → steps
// ---------------------------------------------------------------------------

// TestDecomposeTaskWithBody_PopulatesSteps proves the dispatched work item gets
// real steps from the section body rather than a single heading-shaped step
// with an empty expected output.
func TestDecomposeTaskWithBody_PopulatesSteps(t *testing.T) {
	desc := "1. Mission"
	body := `Deploy the platform reliably. Operators must verify every container starts.

- Run the smoke test after each deploy.
- Configure the reverse proxy per service.

| Component | Requirement |
|---|---|
| Forgejo | Create the admin user |`

	steps, err := DecomposeTaskWithBody(desc, body)
	if err != nil {
		t.Fatalf("DecomposeTaskWithBody() error: %v", err)
	}
	if len(steps) < 3 {
		t.Fatalf("got %d steps, want >= 3 from the body; steps=%+v", len(steps), steps)
	}
	if len(steps) > maxBodySteps {
		t.Errorf("got %d steps, want <= %d", len(steps), maxBodySteps)
	}
	for i, step := range steps {
		if strings.TrimSpace(step.Action) == "" {
			t.Errorf("step[%d].Action is empty", i)
		}
		if strings.TrimSpace(step.ExpectedOutput) == "" {
			t.Errorf("step[%d].ExpectedOutput is empty: %+v", i, step)
		}
		if step.Status != StepPending {
			t.Errorf("step[%d].Status = %q, want %q", i, step.Status, StepPending)
		}
		// Markdown scaffolding must not survive as a step.
		if strings.HasPrefix(step.Action, "#") || isTableSeparator(step.Action) {
			t.Errorf("step[%d].Action is markdown scaffolding: %q", i, step.Action)
		}
	}
	if !strings.Contains(strings.ToLower(steps[0].Action), "deploy") {
		t.Errorf("step[0].Action = %q, want the first body sentence", steps[0].Action)
	}

	// An empty body must fall back to the description without failing.
	fallback, err := DecomposeTaskWithBody(desc, "")
	if err != nil {
		t.Fatalf("DecomposeTaskWithBody(empty body) error: %v", err)
	}
	if len(fallback) == 0 {
		t.Fatal("DecomposeTaskWithBody(empty body) returned no steps")
	}
	for i, step := range fallback {
		if strings.TrimSpace(step.ExpectedOutput) == "" {
			t.Errorf("fallback step[%d].ExpectedOutput is empty", i)
		}
	}
}

// TestDecomposeTask_LegacyOutputUnchanged guards the contract existing callers
// rely on: DecomposeTask still leaves ExpectedOutput empty and keeps its
// sentence-splitting behaviour.
func TestDecomposeTask_LegacyOutputUnchanged(t *testing.T) {
	steps, err := DecomposeTask("Create the types file. Write the implementation. Add tests.")
	if err != nil {
		t.Fatalf("DecomposeTask() error: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("DecomposeTask() returned %d steps, want 3", len(steps))
	}
	for i, step := range steps {
		if step.ExpectedOutput != "" {
			t.Errorf("step[%d].ExpectedOutput = %q, want empty (legacy contract)", i, step.ExpectedOutput)
		}
	}
}

// TestForgejoLoop_Plan_UsesSectionBody proves the dispatch path (not just the
// helper) turns a spec section body into steps.
func TestForgejoLoop_Plan_UsesSectionBody(t *testing.T) {
	specPath := writeTempSpec(t, "spec.md", `# Helix Platform — Deployment

## 1. Topology

Deploy each service on its own host. Configure the reverse proxy per service.

## 2. Rollback

Revert the previous release. Restart the affected containers.
`)

	f := &ForgejoLoop{Owner: "org", Repo: "repo", DryRun: true}
	task, steps, err := f.Plan(specPath, "agent-x")
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if task.Description != "1. Topology" {
		t.Errorf("task.Description = %q, want the first H2 heading", task.Description)
	}
	if strings.TrimSpace(task.Body) == "" {
		t.Fatal("task.Body is empty: the spec body was dropped before dispatch")
	}
	if len(steps) < 2 {
		t.Fatalf("Plan() produced %d steps, want >= 2 from the body: %+v", len(steps), steps)
	}
	for i, step := range steps {
		if strings.TrimSpace(step.ExpectedOutput) == "" {
			t.Errorf("step[%d].ExpectedOutput is empty", i)
		}
	}
}
