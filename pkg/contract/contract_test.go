package contract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidFormat(t *testing.T) {
	if !ValidFormat(FormatOpenAPI) {
		t.Error("expected FormatOpenAPI to be valid")
	}
	if !ValidFormat(FormatProtobuf) {
		t.Error("expected FormatProtobuf to be valid")
	}
	if !ValidFormat(FormatGraphQL) {
		t.Error("expected FormatGraphQL to be valid")
	}
	if ValidFormat("invalid") {
		t.Error("expected 'invalid' format to be invalid")
	}
	if ValidFormat("") {
		t.Error("expected empty format to be invalid")
	}
}

func TestContractIsFrozen(t *testing.T) {
	c := &Contract{ID: "test"}
	if c.IsFrozen() {
		t.Error("new contract should not be frozen")
	}
}

func TestFreeze(t *testing.T) {
	c := &Contract{
		ID:      "test-openapi",
		Format:  FormatOpenAPI,
		Schema:  json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test"},"paths":{}}`),
		Version: 1,
	}
	if err := Freeze(c); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if !c.IsFrozen() {
		t.Error("expected frozen contract")
	}
	if c.Hash == "" {
		t.Error("expected hash to be set")
	}
	if err := Freeze(c); err == nil {
		t.Error("expected double-freeze to fail")
	}
}

func TestComputeHash(t *testing.T) {
	c1 := &Contract{Schema: json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test"}}`)}
	c2 := &Contract{Schema: json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test"}}`)}
	h1 := ComputeHash(c1)
	h2 := ComputeHash(c2)
	if h1 != h2 {
		t.Errorf("hashes should match: %s != %s", h1, h2)
	}
	c3 := &Contract{Schema: json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Other"}}`)}
	h3 := ComputeHash(c3)
	if h1 == h3 {
		t.Error("different schemas should produce different hashes")
	}
}

func TestDetectChanges(t *testing.T) {
	old := &Contract{
		Format: FormatOpenAPI,
		Schema: json.RawMessage(`{"openapi":"3.0.3","info":{"title":"API","version":"1.0.0"},"paths":{"/users":{"get":{"responses":{"200":{"description":"OK"}}}}}}`),
	}
	new := &Contract{
		Format: FormatOpenAPI,
		Schema: json.RawMessage(`{"openapi":"3.0.3","info":{"title":"API","version":"1.0.0"},"paths":{"/users":{"get":{"responses":{"200":{"description":"OK"}}}}}}`),
	}
	changes := DetectChanges(new, old)
	if len(changes) != 0 {
		t.Errorf("expected 0 changes, got %d", len(changes))
	}

	new2 := &Contract{
		Format: FormatOpenAPI,
		Schema: json.RawMessage(`{"openapi":"3.0.3","info":{"title":"API","version":"2.0.0"},"paths":{}}`),
	}
	changes = DetectChanges(new2, old)
	if len(changes) == 0 {
		t.Error("expected breaking changes for endpoint removal")
	}
}

func TestDetectChanges_Nil(t *testing.T) {
	c := &Contract{Format: FormatOpenAPI, Schema: json.RawMessage(`{}`)}
	changes := DetectChanges(c, nil)
	if len(changes) != 0 {
		t.Error("expected 0 changes when old is nil")
	}
	changes = DetectChanges(nil, c)
	if len(changes) != 0 {
		t.Error("expected 0 changes when new is nil")
	}
}

func TestConsumerImpactReport(t *testing.T) {
	changes := []BreakingChange{
		{Field: "paths./users.get", ChangeType: "endpoint_removed"},
		{Field: "paths./users.post.requestBody.name", ChangeType: "field_removed"},
	}
	catalog := map[string][]string{
		"web-app":    {"paths./users.get", "paths./users.post"},
		"mobile-app": {"paths./users.get"},
	}

	impacts := ConsumerImpactReport(changes, catalog)
	if len(impacts) != 2 {
		t.Fatalf("expected 2 impacts, got %d", len(impacts))
	}
	for _, imp := range impacts {
		if imp.ConsumerName == "web-app" {
			if len(imp.BreakingChanges) != 2 {
				t.Errorf("web-app: expected 2 breaking changes, got %d: %v", len(imp.BreakingChanges), imp.BreakingChanges)
			}
		}
	}
}

func TestContractStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewContractStore(dir)
	if err != nil {
		t.Fatalf("NewContractStore: %v", err)
	}

	c := &Contract{
		ID:      "test-openapi",
		SpecRef: "test-spec",
		Format:  FormatOpenAPI,
		Schema:  json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test"},"paths":{}}`),
		Version: 1,
	}

	if err := store.Save(c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load("test-openapi")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ID != c.ID {
		t.Errorf("expected ID %q, got %q", c.ID, loaded.ID)
	}

	ids, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 1 {
		t.Errorf("expected 1 contract, got %d", len(ids))
	}

	// Freeze and test mutation rejection.
	if err := Freeze(c); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if err := store.Save(c); err != nil {
		t.Fatalf("Save frozen: %v", err)
	}

	// Try to save with different version — should be rejected.
	c2 := *c
	c2.Version = 2
	c2.Hash = "" // different hash
	if err := store.Save(&c2); err == nil {
		t.Error("expected frozen contract mutation to be rejected")
	}
}

func TestContractStore_ConsumerCatalog(t *testing.T) {
	dir := t.TempDir()
	store, err := NewContractStore(dir)
	if err != nil {
		t.Fatalf("NewContractStore: %v", err)
	}

	catalog := map[string][]string{
		"web-app": {"paths./users", "paths./items"},
	}
	if err := store.SaveConsumerCatalog(catalog); err != nil {
		t.Fatalf("SaveConsumerCatalog: %v", err)
	}

	loaded, err := store.LoadConsumerCatalog()
	if err != nil {
		t.Fatalf("LoadConsumerCatalog: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("expected 1 entry, got %d", len(loaded))
	}
}

func TestGenerate(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "test-spec.md")
	if err := os.WriteFile(specPath, []byte("---\ntitle: Test API\nstatus: draft\n---\n"), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	author, err := NewContractAuthor(dir)
	if err != nil {
		t.Fatalf("NewContractAuthor: %v", err)
	}

	c, err := author.Generate(context.Background(), "test-spec", FormatOpenAPI)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if c.ID != "test-spec-openapi" {
		t.Errorf("expected ID test-spec-openapi, got %s", c.ID)
	}
	if c.Format != FormatOpenAPI {
		t.Errorf("expected format openapi, got %s", c.Format)
	}
	if c.Version != 1 {
		t.Errorf("expected version 1, got %d", c.Version)
	}
	if c.IsFrozen() {
		t.Error("generated contract should not be frozen")
	}
}

func TestValidate(t *testing.T) {
	c := &Contract{
		ID:     "test-openapi",
		Format: FormatOpenAPI,
		Schema: json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test","version":"1.0.0"},"paths":{"/users":{"get":{"responses":{"200":{"description":"OK"},"400":{"description":"Bad Request"}}}}}}`),
	}

	validator := NewContractValidator()
	report, err := validator.Validate(context.Background(), c, nil, nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !report.Consistent {
		t.Errorf("expected consistent, got warnings: %v", report.Warnings)
	}
}

func TestValidate_MissingPaths(t *testing.T) {
	c := &Contract{
		ID:     "test-openapi",
		Format: FormatOpenAPI,
		Schema: json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test"},"paths":{}}`),
	}

	validator := NewContractValidator()
	report, err := validator.Validate(context.Background(), c, nil, nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	found := false
	for _, w := range report.Warnings {
		if containsStr(w, "no endpoints") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning about no endpoints, got: %v", report.Warnings)
	}
}

func TestValidate_InvalidJSON(t *testing.T) {
	c := &Contract{
		ID:     "test-openapi",
		Format: FormatOpenAPI,
		Schema: json.RawMessage(`not json`),
	}

	validator := NewContractValidator()
	report, err := validator.Validate(context.Background(), c, nil, nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.Consistent {
		t.Error("expected inconsistent for invalid JSON")
	}
}

func TestValidateProtobuf(t *testing.T) {
	c := &Contract{
		ID:     "test-protobuf",
		Format: FormatProtobuf,
		Schema: json.RawMessage(`{"syntax":"proto3","package":"test","services":[],"messages":[]}`),
	}

	validator := NewContractValidator()
	report, err := validator.Validate(context.Background(), c, nil, nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !report.Consistent {
		t.Errorf("expected consistent protobuf, got warnings: %v", report.Warnings)
	}
}

func TestValidateGraphQL(t *testing.T) {
	c := &Contract{
		ID:     "test-graphql",
		Format: FormatGraphQL,
		Schema: json.RawMessage(`{"schema":{"query":"Query"},"types":[{"name":"Query","fields":[]}]}`),
	}

	validator := NewContractValidator()
	report, err := validator.Validate(context.Background(), c, nil, nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !report.Consistent {
		t.Errorf("expected consistent graphql, got warnings: %v", report.Warnings)
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
