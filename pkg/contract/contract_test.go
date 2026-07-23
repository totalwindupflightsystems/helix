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

// ---------------------------------------------------------------------------
// COVERAGE-001 — additional tests to lift pkg/contract coverage >= 80%.
// ---------------------------------------------------------------------------

// --- helpers: schemaList, indexByName, fieldName, fieldStr ---

func TestSchemaList_ExtractsItems(t *testing.T) {
	schema := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"name": "User"},
			map[string]interface{}{"name": "Order"},
			map[string]interface{}{"not_a_name": "ignored"},
		},
	}
	items := schemaList(schema, "messages")
	if len(items) != 3 {
		t.Fatalf("expected 3 items (all map entries), got %d", len(items))
	}
}

func TestSchemaList_MissingKey(t *testing.T) {
	schema := map[string]interface{}{}
	if got := schemaList(schema, "types"); got != nil {
		t.Errorf("expected nil for missing key, got %v", got)
	}
}

func TestSchemaList_WrongType(t *testing.T) {
	schema := map[string]interface{}{
		"messages": "not-a-list",
	}
	if got := schemaList(schema, "messages"); got != nil {
		t.Errorf("expected nil for non-list value, got %v", got)
	}
}

func TestSchemaList_ItemWrongType(t *testing.T) {
	schema := map[string]interface{}{
		"messages": []interface{}{"not-a-map", 42},
	}
	if got := schemaList(schema, "messages"); got != nil {
		t.Errorf("expected nil when items aren't maps, got %v", got)
	}
}

func TestIndexByName(t *testing.T) {
	items := []map[string]interface{}{
		{"name": "User", "type": "object"},
		{"name": "Order", "type": "object"},
	}
	idx := indexByName(items)
	if len(idx) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(idx))
	}
	if idx["User"] == nil || idx["Order"] == nil {
		t.Error("missing User or Order in index")
	}
}

func TestIndexByName_NoNameField(t *testing.T) {
	items := []map[string]interface{}{
		{"type": "object"},
	}
	idx := indexByName(items)
	if _, ok := idx[""]; !ok {
		t.Error("expected empty-string key entry")
	}
}

func TestFieldName(t *testing.T) {
	m := map[string]interface{}{"name": "User", "type": "object"}
	if got := fieldName(m); got != "User" {
		t.Errorf("expected User, got %q", got)
	}
	missing := map[string]interface{}{"type": "object"}
	if got := fieldName(missing); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
	wrongType := map[string]interface{}{"name": 42}
	if got := fieldName(wrongType); got != "" {
		t.Errorf("expected empty string for non-string name, got %q", got)
	}
}

func TestFieldStr(t *testing.T) {
	m := map[string]interface{}{"type": "string", "name": "id"}
	if got := fieldStr(m, "type"); got != "string" {
		t.Errorf("expected string, got %q", got)
	}
	if got := fieldStr(m, "missing"); got != "" {
		t.Errorf("expected empty for missing key, got %q", got)
	}
	wrong := map[string]interface{}{"type": 42}
	if got := fieldStr(wrong, "type"); got != "" {
		t.Errorf("expected empty for non-string value, got %q", got)
	}
}

// --- detectSchemaRemovals ---

func TestDetectSchemaRemovals_Protobuf_MessageRemoved(t *testing.T) {
	old := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"name": "User", "fields": []interface{}{}},
			map[string]interface{}{"name": "Order", "fields": []interface{}{}},
		},
	}
	newC := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"name": "User", "fields": []interface{}{}},
		},
	}
	changes := detectSchemaRemovals(newC, old, "messages")
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(changes), changes)
	}
	if changes[0].Field != "messages.Order" || changes[0].ChangeType != "endpoint_removed" {
		t.Errorf("unexpected change: %+v", changes[0])
	}
}

func TestDetectSchemaRemovals_Protobuf_FieldRemoved(t *testing.T) {
	// Code semantics: flags a field as "field_removed" when NEW has a field
	// that OLD does NOT have (new contract has unexpected fields).
	old := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"name": "User",
				"fields": []interface{}{
					map[string]interface{}{"name": "id", "type": "int"},
				},
			},
		},
	}
	newC := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"name": "User",
				"fields": []interface{}{
					map[string]interface{}{"name": "id", "type": "int"},
					map[string]interface{}{"name": "email", "type": "string"},
				},
			},
		},
	}
	changes := detectSchemaRemovals(newC, old, "messages")
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(changes), changes)
	}
	if changes[0].Field != "messages.User.email" || changes[0].ChangeType != "field_removed" {
		t.Errorf("unexpected change: %+v", changes[0])
	}
}

func TestDetectSchemaRemovals_Protobuf_TypeChanged(t *testing.T) {
	old := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"name": "User",
				"fields": []interface{}{
					map[string]interface{}{"name": "age", "type": "string"},
				},
			},
		},
	}
	newC := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"name": "User",
				"fields": []interface{}{
					map[string]interface{}{"name": "age", "type": "int"},
				},
			},
		},
	}
	changes := detectSchemaRemovals(newC, old, "messages")
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(changes), changes)
	}
	if changes[0].ChangeType != "type_changed" {
		t.Errorf("expected type_changed, got %q", changes[0].ChangeType)
	}
	if changes[0].OldType != "string" || changes[0].NewType != "int" {
		t.Errorf("expected string->int, got %s->%s", changes[0].OldType, changes[0].NewType)
	}
}

func TestDetectSchemaRemovals_Protobuf_ServicesAndMessages(t *testing.T) {
	old := map[string]interface{}{
		"services": []interface{}{
			map[string]interface{}{"name": "Auth", "fields": []interface{}{}},
		},
		"messages": []interface{}{
			map[string]interface{}{"name": "User", "fields": []interface{}{}},
		},
	}
	newC := map[string]interface{}{
		"services": []interface{}{},
		"messages": []interface{}{},
	}
	changes := detectSchemaRemovals(newC, old, "services", "messages")
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes (services+messages), got %d: %+v", len(changes), changes)
	}
}

func TestDetectSchemaRemovals_GraphQL_TypeRemoved(t *testing.T) {
	old := map[string]interface{}{
		"types": []interface{}{
			map[string]interface{}{"name": "Query", "fields": []interface{}{}},
			map[string]interface{}{"name": "Mutation", "fields": []interface{}{}},
		},
	}
	newC := map[string]interface{}{
		"types": []interface{}{
			map[string]interface{}{"name": "Query", "fields": []interface{}{}},
		},
	}
	changes := detectSchemaRemovals(newC, old, "types")
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(changes), changes)
	}
	if changes[0].Field != "types.Mutation" {
		t.Errorf("unexpected field: %q", changes[0].Field)
	}
}

func TestDetectSchemaRemovals_GraphQL_TypeFieldRemoved(t *testing.T) {
	// Code semantics: flags "field_removed" when NEW has a field OLD does NOT.
	old := map[string]interface{}{
		"types": []interface{}{
			map[string]interface{}{
				"name": "User",
				"fields": []interface{}{
					map[string]interface{}{"name": "id", "type": "ID"},
				},
			},
		},
	}
	newC := map[string]interface{}{
		"types": []interface{}{
			map[string]interface{}{
				"name": "User",
				"fields": []interface{}{
					map[string]interface{}{"name": "id", "type": "ID"},
					map[string]interface{}{"name": "name", "type": "String"},
				},
			},
		},
	}
	changes := detectSchemaRemovals(newC, old, "types")
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(changes), changes)
	}
	if changes[0].Field != "types.User.name" || changes[0].ChangeType != "field_removed" {
		t.Errorf("unexpected change: %+v", changes[0])
	}
}

func TestDetectSchemaRemovals_IdenticalSchemas(t *testing.T) {
	schema := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"name": "User", "fields": []interface{}{}},
		},
	}
	changes := detectSchemaRemovals(schema, schema, "messages")
	if len(changes) != 0 {
		t.Errorf("expected no changes for identical schemas, got %d: %+v", len(changes), changes)
	}
}

func TestDetectSchemaRemovals_TypeChangedEmptyType(t *testing.T) {
	// Old has type, new doesn't — should NOT register as type_changed.
	old := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"name": "User",
				"fields": []interface{}{
					map[string]interface{}{"name": "x", "type": "string"},
				},
			},
		},
	}
	newC := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"name": "User",
				"fields": []interface{}{
					map[string]interface{}{"name": "x"}, // no type
				},
			},
		},
	}
	changes := detectSchemaRemovals(newC, old, "messages")
	if len(changes) != 0 {
		t.Errorf("expected 0 changes when new type is missing, got %d: %+v", len(changes), changes)
	}
}

// --- detectChangesByFormat routing ---

func TestDetectChangesByFormat_OpenAPI(t *testing.T) {
	old := map[string]interface{}{
		"paths": map[string]interface{}{
			"/users": map[string]interface{}{"get": map[string]interface{}{}},
		},
	}
	newC := map[string]interface{}{
		"paths": map[string]interface{}{},
	}
	changes := detectChangesByFormat(FormatOpenAPI, newC, old)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].ChangeType != "endpoint_removed" {
		t.Errorf("expected endpoint_removed, got %q", changes[0].ChangeType)
	}
}

func TestDetectChangesByFormat_Protobuf(t *testing.T) {
	old := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"name": "Old", "fields": []interface{}{}},
		},
	}
	newC := map[string]interface{}{
		"messages": []interface{}{},
	}
	changes := detectChangesByFormat(FormatProtobuf, newC, old)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Field != "messages.Old" {
		t.Errorf("unexpected field: %q", changes[0].Field)
	}
}

func TestDetectChangesByFormat_GraphQL(t *testing.T) {
	old := map[string]interface{}{
		"types": []interface{}{
			map[string]interface{}{"name": "Legacy", "fields": []interface{}{}},
		},
	}
	newC := map[string]interface{}{
		"types": []interface{}{},
	}
	changes := detectChangesByFormat(FormatGraphQL, newC, old)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Field != "types.Legacy" {
		t.Errorf("unexpected field: %q", changes[0].Field)
	}
}

func TestDetectChangesByFormat_Unknown(t *testing.T) {
	old := map[string]interface{}{"x": "y"}
	newC := map[string]interface{}{"x": "z"}
	changes := detectChangesByFormat(ContractFormat("bogus"), newC, old)
	if changes != nil {
		t.Errorf("expected nil for unknown format, got %v", changes)
	}
}

// --- Root() accessor ---

func TestRoot_Accessor(t *testing.T) {
	dir := t.TempDir()
	store, err := NewContractStore(dir)
	if err != nil {
		t.Fatalf("NewContractStore: %v", err)
	}
	if got := store.Root(); got != dir {
		t.Errorf("expected Root()=%q, got %q", dir, got)
	}
}

// --- LoadPrevious ---

func TestLoadPrevious_VersionOneReturnsError(t *testing.T) {
	dir := t.TempDir()
	store, err := NewContractStore(dir)
	if err != nil {
		t.Fatalf("NewContractStore: %v", err)
	}
	c := &Contract{
		ID:      "test-openapi",
		Format:  FormatOpenAPI,
		Schema:  json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test"},"paths":{}}`),
		Version: 1,
	}
	if err := store.Save(c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	_, err = store.LoadPrevious("test-openapi")
	if err == nil {
		t.Fatal("expected error for version 1, got nil")
	}
	if !containsStr(err.Error(), "no previous version") {
		t.Errorf("expected 'no previous version' error, got %v", err)
	}
}

func TestLoadPrevious_VersionTwoWithNumberedFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewContractStore(dir)
	if err != nil {
		t.Fatalf("NewContractStore: %v", err)
	}

	// Save v1 under a suffixed ID.
	prev := &Contract{
		ID:      "test-openapi-v1",
		Format:  FormatOpenAPI,
		Schema:  json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test v1"},"paths":{}}`),
		Version: 1,
	}
	if err := store.Save(prev); err != nil {
		t.Fatalf("Save v1: %v", err)
	}

	// Save current at v2.
	curr := &Contract{
		ID:      "test-openapi",
		Format:  FormatOpenAPI,
		Schema:  json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test v2"},"paths":{}}`),
		Version: 2,
	}
	if err := store.Save(curr); err != nil {
		t.Fatalf("Save v2: %v", err)
	}

	loaded, err := store.LoadPrevious("test-openapi")
	if err != nil {
		t.Fatalf("LoadPrevious: %v", err)
	}
	if loaded.Version != 1 {
		t.Errorf("expected version 1, got %d", loaded.Version)
	}
	if loaded.ID != "test-openapi-v1" {
		t.Errorf("expected ID test-openapi-v1, got %q", loaded.ID)
	}
}

func TestLoadPrevious_VersionTwoWithoutNumberedFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	store, err := NewContractStore(dir)
	if err != nil {
		t.Fatalf("NewContractStore: %v", err)
	}
	curr := &Contract{
		ID:      "test-openapi",
		Format:  FormatOpenAPI,
		Schema:  json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test"},"paths":{}}`),
		Version: 2,
	}
	if err := store.Save(curr); err != nil {
		t.Fatalf("Save: %v", err)
	}
	_, err = store.LoadPrevious("test-openapi")
	if err == nil {
		t.Fatal("expected error when no previous version file exists")
	}
}

// --- generateSchema ---

func TestGenerateSchema_OpenAPI(t *testing.T) {
	schema := generateSchema(FormatOpenAPI, "myapi")
	m, ok := schema.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", schema)
	}
	if m["openapi"] != "3.0.3" {
		t.Errorf("expected openapi=3.0.3, got %v", m["openapi"])
	}
	if _, ok := m["paths"]; !ok {
		t.Error("expected paths key")
	}
	info, ok := m["info"].(map[string]interface{})
	if !ok {
		t.Fatal("expected info to be a map")
	}
	if info["title"] != "myapi API" {
		t.Errorf("expected title=myapi API, got %v", info["title"])
	}
}

func TestGenerateSchema_Protobuf(t *testing.T) {
	schema := generateSchema(FormatProtobuf, "mypkg")
	m, ok := schema.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", schema)
	}
	if m["syntax"] != "proto3" {
		t.Errorf("expected syntax=proto3, got %v", m["syntax"])
	}
	if m["package"] != "mypkg" {
		t.Errorf("expected package=mypkg, got %v", m["package"])
	}
	if _, ok := m["services"]; !ok {
		t.Error("expected services key")
	}
	if _, ok := m["messages"]; !ok {
		t.Error("expected messages key")
	}
}

func TestGenerateSchema_GraphQL(t *testing.T) {
	schema := generateSchema(FormatGraphQL, "myschema")
	m, ok := schema.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", schema)
	}
	if _, ok := m["schema"]; !ok {
		t.Error("expected schema key")
	}
	if _, ok := m["types"]; !ok {
		t.Error("expected types key")
	}
}

func TestGenerateSchema_Unknown(t *testing.T) {
	schema := generateSchema(ContractFormat("bogus"), "x")
	if schema != nil {
		t.Errorf("expected nil for unknown format, got %v", schema)
	}
}

// --- resolveSpecDir / resolveStoreRoot ---

func TestResolveSpecDir_Explicit(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveSpecDir(dir)
	if err != nil {
		t.Fatalf("resolveSpecDir: %v", err)
	}
	if got != dir {
		t.Errorf("expected %q, got %q", dir, got)
	}
}

func TestResolveSpecDir_Empty(t *testing.T) {
	got, err := resolveSpecDir("")
	if err != nil {
		t.Fatalf("resolveSpecDir: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".helix", "specs")
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestResolveStoreRoot_Explicit(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveStoreRoot(dir)
	if err != nil {
		t.Fatalf("resolveStoreRoot: %v", err)
	}
	if got != dir {
		t.Errorf("expected %q, got %q", dir, got)
	}
}

func TestResolveStoreRoot_Empty(t *testing.T) {
	got, err := resolveStoreRoot("")
	if err != nil {
		t.Fatalf("resolveStoreRoot: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, DefaultContractsDir)
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// --- Validate with previous + consumers ---

func TestValidate_WithPreviousDetectsBreakingChanges(t *testing.T) {
	prev := &Contract{
		ID:     "test-openapi",
		Format: FormatOpenAPI,
		Schema: json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test","version":"1.0.0"},"paths":{"/users":{"get":{"responses":{"200":{"description":"OK"}}}}}}`),
	}
	curr := &Contract{
		ID:     "test-openapi",
		Format: FormatOpenAPI,
		Schema: json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test","version":"2.0.0"},"paths":{}}`),
	}

	validator := NewContractValidator()
	report, err := validator.Validate(context.Background(), curr, prev, nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.Consistent {
		t.Error("expected inconsistent due to breaking changes")
	}
	if len(report.BreakingChanges) == 0 {
		t.Error("expected breaking changes to be populated")
	}
}

func TestValidate_WithPreviousNoChanges(t *testing.T) {
	schema := json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test","version":"1.0.0"},"paths":{"/users":{"get":{"responses":{"200":{"description":"OK"}}}}}}`)
	prev := &Contract{ID: "test-openapi", Format: FormatOpenAPI, Schema: schema}
	curr := &Contract{ID: "test-openapi", Format: FormatOpenAPI, Schema: schema}

	validator := NewContractValidator()
	report, err := validator.Validate(context.Background(), curr, prev, nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !report.Consistent {
		t.Errorf("expected consistent when no changes, got warnings: %v", report.Warnings)
	}
	if len(report.BreakingChanges) != 0 {
		t.Errorf("expected 0 breaking changes, got %d", len(report.BreakingChanges))
	}
}

func TestValidate_WithConsumersTriggersImpactReport(t *testing.T) {
	prev := &Contract{
		ID:     "test-openapi",
		Format: FormatOpenAPI,
		Schema: json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test","version":"1.0.0"},"paths":{"/users":{"get":{"responses":{"200":{"description":"OK"}}}}}}`),
	}
	curr := &Contract{
		ID:     "test-openapi",
		Format: FormatOpenAPI,
		Schema: json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test","version":"2.0.0"},"paths":{}}`),
	}

	validator := NewContractValidator()
	report, err := validator.Validate(context.Background(), curr, prev, []string{"web-app", "mobile-app"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(report.ConsumerImpacts) != 2 {
		t.Fatalf("expected 2 consumer impacts, got %d", len(report.ConsumerImpacts))
	}
	for _, imp := range report.ConsumerImpacts {
		if imp.ConsumerName != "web-app" && imp.ConsumerName != "mobile-app" {
			t.Errorf("unexpected consumer: %s", imp.ConsumerName)
		}
		if len(imp.BreakingChanges) == 0 {
			t.Errorf("consumer %s: expected breaking changes, got 0", imp.ConsumerName)
		}
	}
}

func TestValidate_WithPreviousNoChangesWithConsumers(t *testing.T) {
	schema := json.RawMessage(`{"openapi":"3.0.3","info":{"title":"Test","version":"1.0.0"},"paths":{"/users":{"get":{"responses":{"200":{"description":"OK"}}}}}}`)
	prev := &Contract{ID: "test-openapi", Format: FormatOpenAPI, Schema: schema}
	curr := &Contract{ID: "test-openapi", Format: FormatOpenAPI, Schema: schema}

	validator := NewContractValidator()
	report, err := validator.Validate(context.Background(), curr, prev, []string{"web-app"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(report.ConsumerImpacts) != 0 {
		t.Errorf("expected 0 consumer impacts when no changes, got %d", len(report.ConsumerImpacts))
	}
}

// --- impactSeverity edge cases (lifting from 60% → 100%) ---

func TestImpactSeverity(t *testing.T) {
	cases := []struct {
		count int
		want  string
	}{
		{1, "low"},
		{2, "medium"},
		{3, "high"},
		{5, "critical"},
		{10, "critical"},
	}
	for _, tc := range cases {
		fields := make([]string, tc.count)
		got := impactSeverity(fields)
		if got != tc.want {
			t.Errorf("impactSeverity with %d: expected %q, got %q", tc.count, tc.want, got)
		}
	}
}

// --- matchesConsumer prefix matching ---

func TestMatchesConsumer_PrefixMatch(t *testing.T) {
	if !matchesConsumer("paths./users.get", []string{"paths./users."}) {
		t.Error("expected prefix match for paths./users.")
	}
	if !matchesConsumer("paths./users.get", []string{"paths./users.get"}) {
		t.Error("expected exact match")
	}
	if !matchesConsumer("paths./users.get", []string{"*"}) {
		t.Error("expected wildcard match")
	}
	if matchesConsumer("paths./users.get", []string{"paths./orders."}) {
		t.Error("expected no match for different prefix")
	}
}

// --- NewContractStore + NewContractAuthor with empty paths (covers default paths) ---

func TestNewContractStore_EmptyRootUsesDefault(t *testing.T) {
	// Use HOME=tempdir to avoid writing to the real $HOME.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	store, err := NewContractStore("")
	if err != nil {
		t.Fatalf("NewContractStore(empty): %v", err)
	}
	want := filepath.Join(tmpHome, DefaultContractsDir)
	if store.Root() != want {
		t.Errorf("expected root=%q, got %q", want, store.Root())
	}
}

func TestNewContractAuthor_EmptySpecDirUsesDefault(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	author, err := NewContractAuthor("")
	if err != nil {
		t.Fatalf("NewContractAuthor(empty): %v", err)
	}
	if author.specDir != filepath.Join(tmpHome, ".helix", "specs") {
		t.Errorf("expected specDir=%q, got %q", filepath.Join(tmpHome, ".helix", "specs"), author.specDir)
	}
}
