package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWalkProvenance_EmptyAttestHash verifies that when attestHash is empty,
// WalkProvenance returns a single "missing" commit link.
func TestWalkProvenance_EmptyAttestHash(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	chain, err := WalkProvenance("abc1234", "", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chain.CommitSHA != "abc1234" {
		t.Errorf("CommitSHA = %q, want %q", chain.CommitSHA, "abc1234")
	}
	if chain.Complete {
		t.Error("expected chain to be incomplete with empty attestHash")
	}
	if len(chain.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(chain.Links))
	}
	link := chain.Links[0]
	if link.Stage != "commit" {
		t.Errorf("stage = %q, want %q", link.Stage, "commit")
	}
	if link.Status != "missing" {
		t.Errorf("status = %q, want %q", link.Status, "missing")
	}
	if link.OK {
		t.Error("expected link.OK to be false")
	}
}

// TestWalkProvenance_HashNotFound verifies that when the attestHash is not in
// the registry, WalkProvenance returns a commit link and a not_found prompt link.
func TestWalkProvenance_HashNotFound(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	// Create an empty index so Lookup fails
	idxPath := filepath.Join(dir, "_index.yaml")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(idxPath, []byte("components: {}\n"), 0644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	chain, err := WalkProvenance("abc1234", "sha256:deadbeef", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chain.Complete {
		t.Error("expected chain to be incomplete when hash not found")
	}
	if len(chain.Links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(chain.Links))
	}
	// First link: commit
	if chain.Links[0].Stage != "commit" || !chain.Links[0].OK {
		t.Errorf("first link: stage=%s OK=%v", chain.Links[0].Stage, chain.Links[0].OK)
	}
	// Second link: prompt not_found
	if chain.Links[1].Stage != "prompt" || chain.Links[1].Status != "not_found" || chain.Links[1].OK {
		t.Errorf("second link: stage=%s status=%s OK=%v", chain.Links[1].Stage, chain.Links[1].Status, chain.Links[1].OK)
	}
}

// TestWalkProvenance_FullChain verifies the complete provenance chain when all
// links are present: commit → prompt → spec → work_item → intent.
func TestWalkProvenance_FullChain(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	// Register a prompt with specRef and workItem
	content := "# Agent Identity Prompt\n\nThis is the agent identity prompt."
	component := "agent-identity"
	version := "v1.0.0"

	setupRegisteredPrompt(t, dir, component, version, content, StatusActive)

	// Add specRef, workItem, and changes to the metadata
	hash := Hash(content)
	pv, err := Lookup(hash)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	meta, err := readMetadata(pv.MetadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	meta.SpecRef = "specs/agent-identity.md"
	meta.SpecVersion = "v1.0"
	meta.WorkItem = "WI-001"
	meta.Changes = "Add agent identity prompt"
	if err := writeMetadata(pv.MetadataPath, meta); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	// Create the spec file so os.Stat finds it
	specPath := filepath.Join(dir, "specs/agent-identity.md")
	if err := os.MkdirAll(filepath.Dir(specPath), 0755); err != nil {
		t.Fatalf("mkdir spec: %v", err)
	}
	if err := os.WriteFile(specPath, []byte("# Agent Identity Spec"), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	chain, err := WalkProvenance("abc1234", hash, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !chain.Complete {
		t.Error("expected chain to be complete")
	}
	if len(chain.Links) != 5 {
		t.Fatalf("expected 5 links (commit+prompt+spec+work_item+intent), got %d", len(chain.Links))
	}

	// Verify each link
	stages := []struct {
		stage  string
		status string
		ok     bool
	}{
		{"commit", "parsed", true},
		{"prompt", string(StatusActive), true},
		{"spec", "found", true},
		{"work_item", "referenced", true},
		{"intent", "declared", true},
	}
	for i, want := range stages {
		link := chain.Links[i]
		if link.Stage != want.stage {
			t.Errorf("link[%d]: stage = %q, want %q", i, link.Stage, want.stage)
		}
		if link.Status != want.status {
			t.Errorf("link[%d]: status = %q, want %q", i, link.Status, want.status)
		}
		if link.OK != want.ok {
			t.Errorf("link[%d]: OK = %v, want %v", i, link.OK, want.ok)
		}
	}
}

// TestWalkProvenance_MissingSpecFile verifies that when the metadata references
// a spec file that doesn't exist on disk, the spec link is marked missing.
func TestWalkProvenance_MissingSpecFile(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	content := "# Test Prompt"
	component := "test-comp"
	version := "v1.0.0"

	setupRegisteredPrompt(t, dir, component, version, content, StatusActive)

	hash := Hash(content)
	pv, err := Lookup(hash)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	meta, err := readMetadata(pv.MetadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	// specRef points to a file that does NOT exist
	meta.SpecRef = "specs/nonexistent.md"
	meta.SpecVersion = "v1.0"
	if err := writeMetadata(pv.MetadataPath, meta); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	chain, err := WalkProvenance("abc1234", hash, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chain.Complete {
		t.Error("expected chain to be incomplete when spec file is missing")
	}

	// Find the spec link
	var specLink *ChainLink
	for i := range chain.Links {
		if chain.Links[i].Stage == "spec" {
			specLink = &chain.Links[i]
			break
		}
	}
	if specLink == nil {
		t.Fatal("expected spec link")
	}
	if specLink.Status != "missing" {
		t.Errorf("spec status = %q, want %q", specLink.Status, "missing")
	}
	if specLink.OK {
		t.Error("spec link should not be OK when file is missing")
	}
}

// TestWalkProvenance_NoSpecRefInMetadata verifies that when metadata has no
// specRef, the spec link is marked missing.
func TestWalkProvenance_NoSpecRefInMetadata(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	content := "# No Spec Prompt"
	component := "no-spec"
	version := "v1.0.0"

	setupRegisteredPrompt(t, dir, component, version, content, StatusActive)

	hash := Hash(content)
	pv, err := Lookup(hash)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	meta, err := readMetadata(pv.MetadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	// No specRef
	meta.SpecRef = ""
	if err := writeMetadata(pv.MetadataPath, meta); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	chain, err := WalkProvenance("abc1234", hash, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var specLink *ChainLink
	for i := range chain.Links {
		if chain.Links[i].Stage == "spec" {
			specLink = &chain.Links[i]
			break
		}
	}
	if specLink == nil {
		t.Fatal("expected spec link")
	}
	if specLink.Status != "missing" {
		t.Errorf("spec status = %q, want %q", specLink.Status, "missing")
	}
	if !strings.Contains(specLink.Detail, "no spec_ref") {
		t.Errorf("detail should mention missing spec_ref, got %q", specLink.Detail)
	}
}

// TestWalkProvenance_NoWorkItemInMetadata verifies that when metadata has no
// WorkItem, the work_item and intent links are marked missing.
func TestWalkProvenance_NoWorkItemInMetadata(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	content := "# No WorkItem Prompt"
	component := "no-wi"
	version := "v1.0.0"

	setupRegisteredPrompt(t, dir, component, version, content, StatusActive)

	hash := Hash(content)
	pv, err := Lookup(hash)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	meta, err := readMetadata(pv.MetadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	meta.SpecRef = "specs/some-spec.md"
	meta.SpecVersion = "v1.0"
	meta.WorkItem = "" // no work item
	if err := writeMetadata(pv.MetadataPath, meta); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	// Create the spec file
	specPath := filepath.Join(dir, "specs/some-spec.md")
	if err := os.MkdirAll(filepath.Dir(specPath), 0755); err != nil {
		t.Fatalf("mkdir spec: %v", err)
	}
	if err := os.WriteFile(specPath, []byte("# Spec"), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	chain, err := WalkProvenance("abc1234", hash, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chain.Complete {
		t.Error("expected chain to be incomplete without work_item")
	}

	// Find work_item link
	var wiLink *ChainLink
	for i := range chain.Links {
		if chain.Links[i].Stage == "work_item" {
			wiLink = &chain.Links[i]
			break
		}
	}
	if wiLink == nil {
		t.Fatal("expected work_item link")
	}
	if wiLink.Status != "missing" || wiLink.OK {
		t.Errorf("work_item link: status=%s OK=%v", wiLink.Status, wiLink.OK)
	}
}

// TestWalkProvenance_WorkItemEmptyChanges verifies that when WorkItem is set
// but Changes is empty, the intent link has OK=false.
func TestWalkProvenance_WorkItemEmptyChanges(t *testing.T) {
	dir := t.TempDir()
	setRegistryDir(t, dir)

	content := "# Empty Changes Prompt"
	component := "empty-changes"
	version := "v1.0.0"

	setupRegisteredPrompt(t, dir, component, version, content, StatusActive)

	hash := Hash(content)
	pv, err := Lookup(hash)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	meta, err := readMetadata(pv.MetadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	meta.SpecRef = "specs/some-spec.md"
	meta.SpecVersion = "v1.0"
	meta.WorkItem = "WI-002"
	meta.Changes = "" // empty changes
	if err := writeMetadata(pv.MetadataPath, meta); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	specPath := filepath.Join(dir, "specs/some-spec.md")
	if err := os.MkdirAll(filepath.Dir(specPath), 0755); err != nil {
		t.Fatalf("mkdir spec: %v", err)
	}
	if err := os.WriteFile(specPath, []byte("# Spec"), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	chain, err := WalkProvenance("abc1234", hash, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chain.Complete {
		t.Error("expected chain to be incomplete with empty Changes")
	}

	var intentLink *ChainLink
	for i := range chain.Links {
		if chain.Links[i].Stage == "intent" {
			intentLink = &chain.Links[i]
			break
		}
	}
	if intentLink == nil {
		t.Fatal("expected intent link")
	}
	if intentLink.Status != "declared" {
		t.Errorf("intent status = %q, want %q", intentLink.Status, "declared")
	}
	if intentLink.OK {
		t.Error("intent link should be not OK when Changes is empty")
	}
}

// TestVerifyProvenance_AllOK verifies that VerifyProvenance returns allOK=true
// and no failures when all links are OK.
func TestVerifyProvenance_AllOK(t *testing.T) {
	chain := &ProvenanceChain{
		CommitSHA: "abc1234",
		Links: []ChainLink{
			{Stage: "commit", Status: "parsed", OK: true},
			{Stage: "prompt", Status: "active", OK: true},
			{Stage: "spec", Status: "found", OK: true},
		},
		Complete: true,
	}

	allOK, failures := VerifyProvenance(chain)
	if !allOK {
		t.Error("expected allOK=true")
	}
	if len(failures) != 0 {
		t.Errorf("expected 0 failures, got %d: %v", len(failures), failures)
	}
}

// TestVerifyProvenance_SomeFailures verifies that VerifyProvenance reports
// failures correctly when some links are not OK.
func TestVerifyProvenance_SomeFailures(t *testing.T) {
	chain := &ProvenanceChain{
		CommitSHA: "abc1234",
		Links: []ChainLink{
			{Stage: "commit", Status: "parsed", OK: true},
			{Stage: "prompt", Status: "not_found", Detail: "hash not in registry", OK: false},
			{Stage: "spec", Status: "missing", Detail: "no spec_ref", OK: false},
			{Stage: "intent", Status: "declared", OK: true},
		},
		Complete: false,
	}

	allOK, failures := VerifyProvenance(chain)
	if allOK {
		t.Error("expected allOK=false with failed links")
	}
	if len(failures) != 2 {
		t.Fatalf("expected 2 failures, got %d: %v", len(failures), failures)
	}
	if !strings.Contains(failures[0], "prompt") || !strings.Contains(failures[0], "not_found") {
		t.Errorf("failure[0] should mention prompt/not_found: %q", failures[0])
	}
	if !strings.Contains(failures[1], "spec") || !strings.Contains(failures[1], "missing") {
		t.Errorf("failure[1] should mention spec/missing: %q", failures[1])
	}
}

// TestVerifyProvenance_EmptyChain verifies that VerifyProvenance handles an
// empty chain: allOK=true and no failures.
func TestVerifyProvenance_EmptyChain(t *testing.T) {
	chain := &ProvenanceChain{
		CommitSHA: "abc1234",
		Links:     nil,
	}

	allOK, failures := VerifyProvenance(chain)
	if !allOK {
		t.Error("expected allOK=true for empty chain")
	}
	if len(failures) != 0 {
		t.Errorf("expected 0 failures for empty chain, got %d", len(failures))
	}
}

// TestVerifyProvenance_AllFailures verifies that when every link fails,
// VerifyProvenance returns all failures.
func TestVerifyProvenance_AllFailures(t *testing.T) {
	chain := &ProvenanceChain{
		CommitSHA: "abc1234",
		Links: []ChainLink{
			{Stage: "commit", Status: "missing", Detail: "no attestation", OK: false},
			{Stage: "prompt", Status: "not_found", Detail: "not found", OK: false},
		},
	}

	allOK, failures := VerifyProvenance(chain)
	if allOK {
		t.Error("expected allOK=false")
	}
	if len(failures) != 2 {
		t.Fatalf("expected 2 failures, got %d", len(failures))
	}
}
