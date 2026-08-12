// Command helix — contract_test.go
//
// Tests for `helix contract` (create/validate/freeze/diff/consumer-check/
// list/show) — API contract generation + breaking change detection CLI
// (Phase 2 §2.4). Covers CLI happy paths AND usage-error paths (COV-003).
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/contract"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// runContractCLI drives runContractWithDryRun and returns the error.
func runContractCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr strings.Builder
	err := runContractWithDryRun(args, &stdout, &stderr, false)
	return stdout.String(), stderr.String(), err
}

// seedContract writes a contract directly into the store (store seeding, not
// under test — the CLI funcs are).
func seedContract(t *testing.T, storeDir string, c *contract.Contract) {
	t.Helper()
	s, err := contract.NewContractStore(storeDir)
	require.NoError(t, err)
	require.NoError(t, s.Save(c))
}

// contractWithSchema builds a Contract with the given raw schema JSON.
func contractWithSchema(id, format string, schema interface{}) *contract.Contract {
	raw, _ := json.Marshal(schema)
	return &contract.Contract{
		ID:        id,
		SpecRef:   "spec-" + id,
		Format:    contract.ContractFormat(format),
		Schema:    raw,
		Version:   1,
		CreatedAt: time.Now().UTC(),
	}
}

func openAPISchema(paths map[string]interface{}) map[string]interface{} {
	if paths == nil {
		paths = map[string]interface{}{}
	}
	return map[string]interface{}{
		"openapi":    "3.0.3",
		"info":       map[string]interface{}{"title": "t", "version": "1.0.0"},
		"paths":      paths,
		"components": map[string]interface{}{},
	}
}

// ---------------------------------------------------------------------------
// parseContractFlags
// ---------------------------------------------------------------------------

func TestParseContractFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantSub  string
		wantID   string
		wantOld  string
		wantRC   int
		wantHelp bool
	}{
		{
			name:    "diff two ids",
			args:    []string{"diff", "old-1", "new-1"},
			wantSub: "diff", wantID: "old-1", wantOld: "new-1",
			wantRC: contractExitOK,
		},
		{
			name:     "help flag",
			args:     []string{"--help"},
			wantHelp: true, wantRC: contractExitOK,
		},
		{
			name:   "missing format value",
			args:   []string{"create", "spec-1", "--format"},
			wantRC: contractExitError,
		},
		{
			name:    "empty defaults to help",
			args:    []string{},
			wantSub: "", wantRC: contractExitOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, help, rc := parseContractFlags(tt.args)
			assert.Equal(t, tt.wantRC, rc)
			if tt.wantRC != contractExitOK {
				return // error returns carry partial struct state
			}
			assert.Equal(t, tt.wantHelp, help)
			assert.Equal(t, tt.wantSub, f.subcommand)
			assert.Equal(t, tt.wantID, f.id)
			assert.Equal(t, tt.wantOld, f.oldID)
		})
	}
}

func TestParseContractFlags_Values(t *testing.T) {
	f, _, rc := parseContractFlags([]string{
		"consumer-check", "spec-1", "--consumer", "webapp",
		"--format", "protobuf", "--store", "/tmp/contracts", "--json", "--dry-run",
	})
	require.Equal(t, contractExitOK, rc)
	assert.Equal(t, "consumer-check", f.subcommand)
	assert.Equal(t, "spec-1", f.id)
	assert.Equal(t, "webapp", f.consumer)
	assert.Equal(t, "protobuf", f.format)
	assert.Equal(t, "/tmp/contracts", f.storePath)
	assert.True(t, f.jsonOut)
	assert.True(t, f.dryRun)
}

func TestParseContractFlags_UnknownFlagSilentlyIgnored(t *testing.T) {
	// Unlike spec/adr, unknown flags do not abort parsing.
	f, _, rc := parseContractFlags([]string{"list", "--bogus"})
	require.Equal(t, contractExitOK, rc)
	assert.Equal(t, "list", f.subcommand)
}

// ---------------------------------------------------------------------------
// runContractWithDryRun dispatch
// ---------------------------------------------------------------------------

func TestRunContract_Help(t *testing.T) {
	stdout, _, err := runContractCLI(t)
	require.NoError(t, err)
	assert.Contains(t, stdout, "helix contract")
	assert.Contains(t, stdout, "consumer-check")

	stdout, _, err = runContractCLI(t, "list", "--help")
	require.NoError(t, err)
	assert.Contains(t, stdout, "Usage:")
}

func TestRunContract_StoreError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	_, _, err := runContractCLI(t, "list", "--store", filepath.Join(blocker, "sub"))
	require.Error(t, err)
}

func TestRunContract_UnknownSubcommand(t *testing.T) {
	_, _, err := runContractCLI(t, "frobnicate", "--store", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown contract subcommand "frobnicate"`)
}

func TestRunContractWithDryRun_Flag(t *testing.T) {
	// dryRun=true threads through: prints intent header before dispatching.
	var stdout, stderr strings.Builder
	err := runContractWithDryRun([]string{"create", "spec-1", "--store", t.TempDir()},
		&stdout, &stderr, true)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "[DRY-RUN] contract create (store:")
	assert.Contains(t, stdout.String(), "[DRY-RUN] would create contract")
}

func TestRunContract_EnvStoreOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envContractStore, dir)
	stdout, _, err := runContractCLI(t, "list")
	require.NoError(t, err)
	assert.Contains(t, stdout, "no contracts found")
}

// ---------------------------------------------------------------------------
// runContractCreate
// ---------------------------------------------------------------------------

func TestRunContractCreate_UsageErrors(t *testing.T) {
	_, _, err := runContractCLI(t, "create", "--store", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contract create requires <spec-id>")

	_, _, err = runContractCLI(t, "create", "spec-1", "--format", "yaml", "--store", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown format "yaml"`)
}

func TestRunContractCreate_HappyPath(t *testing.T) {
	store := t.TempDir()
	stdout, _, err := runContractCLI(t, "create", "SPEC-9", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, "✓ contract SPEC-9-openapi created (format: openapi, version: 1)")

	// Persisted and loadable.
	s, err := contract.NewContractStore(store)
	require.NoError(t, err)
	c, err := s.Load("SPEC-9-openapi")
	require.NoError(t, err)
	assert.Equal(t, "SPEC-9", c.SpecRef)
	assert.Equal(t, contract.FormatOpenAPI, c.Format)
	assert.Equal(t, 1, c.Version)
}

func TestRunContractCreate_NonDefaultFormat(t *testing.T) {
	store := t.TempDir()
	stdout, _, err := runContractCLI(t, "create", "SPEC-9", "--format", "protobuf", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, "✓ contract SPEC-9-protobuf created (format: protobuf, version: 1)")
}

// ---------------------------------------------------------------------------
// DF-014: create→validate round-trip with bare spec ids
// ---------------------------------------------------------------------------

func TestRunContractCreateValidate_BareIDRoundTrip(t *testing.T) {
	// DF-014: create registers <spec-id>-<format>; validate/freeze/diff/show
	// accept the bare spec id as an alias for the same contract.
	store := t.TempDir()
	stdout, _, err := runContractCLI(t, "create", "agent-identity", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, "✓ contract agent-identity-openapi created")

	stdout, _, err = runContractCLI(t, "validate", "agent-identity", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, `✓ contract "agent-identity-openapi" is consistent`)

	stdout, _, err = runContractCLI(t, "show", "agent-identity", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, "ID:       agent-identity-openapi")

	stdout, _, err = runContractCLI(t, "freeze", "agent-identity", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, `✓ contract "agent-identity-openapi" frozen`)

	// diff accepts bare ids on both sides.
	_, _, err = runContractCLI(t, "create", "agent-marketplace", "--store", store)
	require.NoError(t, err)
	stdout, _, err = runContractCLI(t, "diff", "agent-marketplace", "agent-identity", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, `✓ no breaking changes between "agent-identity-openapi" and "agent-marketplace-openapi"`)
}

func TestRunContract_BareIDAliasStillResolvesFullID(t *testing.T) {
	// The full registered id keeps working after the alias is introduced.
	store := t.TempDir()
	_, _, err := runContractCLI(t, "create", "SPEC-9", "--store", store)
	require.NoError(t, err)

	stdout, _, err := runContractCLI(t, "validate", "SPEC-9-openapi", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, `✓ contract "SPEC-9-openapi" is consistent`)

	stdout, _, err = runContractCLI(t, "validate", "SPEC-9", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, `✓ contract "SPEC-9-openapi" is consistent`)
}

func TestRunContract_BareIDAmbiguous(t *testing.T) {
	// A bare id matching contracts in multiple formats is rejected with the
	// matches listed, instead of silently picking one.
	store := t.TempDir()
	_, _, err := runContractCLI(t, "create", "SPEC-9", "--store", store)
	require.NoError(t, err)
	_, _, err = runContractCLI(t, "create", "SPEC-9", "--format", "protobuf", "--store", store)
	require.NoError(t, err)

	_, _, err = runContractCLI(t, "validate", "SPEC-9", "--store", store)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
	assert.Contains(t, err.Error(), "SPEC-9-openapi")
	assert.Contains(t, err.Error(), "SPEC-9-protobuf")
}

func TestRunContractCreate_ScaffoldLabeled(t *testing.T) {
	// DF-014: the generated OpenAPI is explicitly labeled a scaffold instead
	// of being a silently empty contract.
	store := t.TempDir()
	stdout, _, err := runContractCLI(t, "create", "SPEC-9", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, "scaffold")

	s, err := contract.NewContractStore(store)
	require.NoError(t, err)
	c, err := s.Load("SPEC-9-openapi")
	require.NoError(t, err)
	var schema map[string]interface{}
	require.NoError(t, json.Unmarshal(c.Schema, &schema))
	assert.Equal(t, true, schema["x-helix-scaffold"])
}

// ---------------------------------------------------------------------------
// runContractValidate
// ---------------------------------------------------------------------------

func TestRunContractValidate_UsageErrors(t *testing.T) {
	_, _, err := runContractCLI(t, "validate", "--store", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contract validate requires <contract-id>")

	_, _, err = runContractCLI(t, "validate", "spec-nope", "--store", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `load contract "spec-nope"`)
}

func TestRunContractValidate_HappyPath(t *testing.T) {
	store := t.TempDir()
	_, _, err := runContractCLI(t, "create", "SPEC-9", "--store", store)
	require.NoError(t, err)

	stdout, _, err := runContractCLI(t, "validate", "SPEC-9-openapi", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, `✓ contract "SPEC-9-openapi" is consistent`)
	assert.Contains(t, stdout, "⚠ OpenAPI: no endpoints defined in paths")
}

func TestRunContractValidate_JSONOutput(t *testing.T) {
	store := t.TempDir()
	_, _, err := runContractCLI(t, "create", "SPEC-9", "--store", store)
	require.NoError(t, err)

	stdout, _, err := runContractCLI(t, "validate", "SPEC-9-openapi", "--store", store, "--json")
	require.NoError(t, err)
	var report struct {
		Consistent bool `json:"consistent"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &report))
	assert.True(t, report.Consistent)
}

// ---------------------------------------------------------------------------
// runContractFreeze
// ---------------------------------------------------------------------------

func TestRunContractFreeze_UsageErrors(t *testing.T) {
	_, _, err := runContractCLI(t, "freeze", "--store", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contract freeze requires <contract-id>")

	_, _, err = runContractCLI(t, "freeze", "spec-nope", "--store", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `load contract "spec-nope"`)
}

func TestRunContractFreeze_HappyPath(t *testing.T) {
	store := t.TempDir()
	_, _, err := runContractCLI(t, "create", "SPEC-9", "--store", store)
	require.NoError(t, err)

	stdout, _, err := runContractCLI(t, "freeze", "SPEC-9-openapi", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, `✓ contract "SPEC-9-openapi" frozen (hash: `)

	// Second freeze reports already-frozen.
	stdout, _, err = runContractCLI(t, "freeze", "SPEC-9-openapi", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, `contract "SPEC-9-openapi" is already frozen (hash: `)

	// Persisted state is frozen.
	s, err := contract.NewContractStore(store)
	require.NoError(t, err)
	c, err := s.Load("SPEC-9-openapi")
	require.NoError(t, err)
	assert.True(t, c.IsFrozen())
	assert.NotEmpty(t, c.Hash)
}

func TestRunContractFreeze_DryRun(t *testing.T) {
	store := t.TempDir()
	_, _, err := runContractCLI(t, "create", "SPEC-9", "--store", store)
	require.NoError(t, err)

	stdout, _, err := runContractCLI(t, "freeze", "SPEC-9-openapi", "--store", store, "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, stdout, "[DRY-RUN] would freeze contract")

	// Dry run must not persist the frozen state.
	s, err := contract.NewContractStore(store)
	require.NoError(t, err)
	c, err := s.Load("SPEC-9-openapi")
	require.NoError(t, err)
	assert.False(t, c.IsFrozen())
}

// ---------------------------------------------------------------------------
// runContractDiff
// ---------------------------------------------------------------------------

func TestRunContractDiff_UsageErrors(t *testing.T) {
	_, _, err := runContractCLI(t, "diff", "only-one", "--store", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contract diff requires <new-id> <old-id>")

	store := t.TempDir()
	_, _, err = runContractCLI(t, "diff", "old-1", "new-1", "--store", store)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `load new contract "old-1"`)

	// Contract: diff <new> <old> — first positional is new, second is old.
	seedContract(t, store, contractWithSchema("old-1", "openapi", openAPISchema(nil)))
	_, _, err = runContractCLI(t, "diff", "old-1", "new-1", "--store", store)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `load old contract "new-1"`)
}

func TestRunContractDiff_NoChanges(t *testing.T) {
	store := t.TempDir()
	seedContract(t, store, contractWithSchema("old-1", "openapi", openAPISchema(nil)))
	seedContract(t, store, contractWithSchema("new-1", "openapi", openAPISchema(nil)))

	stdout, _, err := runContractCLI(t, "diff", "new-1", "old-1", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, `✓ no breaking changes between "old-1" and "new-1"`)
}

func TestRunContractDiff_WithBreakingChanges(t *testing.T) {
	store := t.TempDir()
	// Old contract exposes /users; new contract drops it.
	seedContract(t, store, contractWithSchema("old-1", "openapi", openAPISchema(map[string]interface{}{
		"/users": map[string]interface{}{"get": map[string]interface{}{}},
	})))
	seedContract(t, store, contractWithSchema("new-1", "openapi", openAPISchema(nil)))

	stdout, _, err := runContractCLI(t, "diff", "new-1", "old-1", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, `1 breaking change(s) between "old-1" and "new-1":`)
	assert.Contains(t, stdout, "paths./users")
	assert.Contains(t, stdout, "endpoint_removed")

	// --json form
	stdout, _, err = runContractCLI(t, "diff", "new-1", "old-1", "--store", store, "--json")
	require.NoError(t, err)
	var changes []struct {
		Field      string `json:"field"`
		ChangeType string `json:"change_type"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &changes))
	require.Len(t, changes, 1)
	assert.Equal(t, "paths./users", changes[0].Field)
}

// ---------------------------------------------------------------------------
// runContractConsumerCheck
// ---------------------------------------------------------------------------

func TestRunContractConsumerCheck_UsageErrors(t *testing.T) {
	_, _, err := runContractCLI(t, "consumer-check", "--store", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contract consumer-check requires <contract-id>")

	_, _, err = runContractCLI(t, "consumer-check", "spec-1", "--store", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contract consumer-check requires --consumer <name>")
}

func TestRunContractConsumerCheck_ConsumerNotFound(t *testing.T) {
	store := t.TempDir()
	_, _, err := runContractCLI(t, "create", "SPEC-9", "--store", store)
	require.NoError(t, err)

	stdout, _, err := runContractCLI(t, "consumer-check", "SPEC-9-openapi", "--consumer", "webapp", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, `✗ consumer "webapp" not found in catalog`)
	assert.Contains(t, stdout, "known consumers: ")
}

func TestRunContractConsumerCheck_KnownConsumer(t *testing.T) {
	store := t.TempDir()
	_, _, err := runContractCLI(t, "create", "SPEC-9", "--store", store)
	require.NoError(t, err)

	// Seed a consumer catalog entry.
	s, err := contract.NewContractStore(store)
	require.NoError(t, err)
	require.NoError(t, s.SaveConsumerCatalog(map[string][]string{
		"webapp": {"*"},
	}))

	stdout, _, err := runContractCLI(t, "consumer-check", "SPEC-9-openapi", "--consumer", "webapp", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, `✓ no breaking changes affecting consumer "webapp"`)
}

// ---------------------------------------------------------------------------
// runContractList
// ---------------------------------------------------------------------------

func TestRunContractList_EmptyStore(t *testing.T) {
	stdout, _, err := runContractCLI(t, "list", "--store", t.TempDir())
	require.NoError(t, err)
	assert.Contains(t, stdout, "no contracts found")
}

func TestRunContractList_WithContracts(t *testing.T) {
	store := t.TempDir()
	_, _, err := runContractCLI(t, "create", "SPEC-9", "--store", store)
	require.NoError(t, err)
	_, _, err = runContractCLI(t, "create", "SPEC-10", "--format", "graphql", "--store", store)
	require.NoError(t, err)

	stdout, _, err := runContractCLI(t, "list", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, "SPEC-9-openapi  v1  openapi  unfrozen")
	assert.Contains(t, stdout, "SPEC-10-graphql  v1  graphql  unfrozen")
}

func TestRunContractList_CorruptFileShowsError(t *testing.T) {
	store := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(store, "bad.json"), []byte("{not json"), 0o644))

	stdout, _, err := runContractCLI(t, "list", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, "bad (error: ")
}

// ---------------------------------------------------------------------------
// runContractShow
// ---------------------------------------------------------------------------

func TestRunContractShow_UsageErrors(t *testing.T) {
	_, _, err := runContractCLI(t, "show", "--store", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contract show requires <contract-id>")

	_, _, err = runContractCLI(t, "show", "spec-nope", "--store", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `load contract "spec-nope"`)
}

func TestRunContractShow_HappyPath(t *testing.T) {
	store := t.TempDir()
	_, _, err := runContractCLI(t, "create", "SPEC-9", "--store", store)
	require.NoError(t, err)

	stdout, _, err := runContractCLI(t, "show", "SPEC-9-openapi", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, "ID:       SPEC-9-openapi")
	assert.Contains(t, stdout, "SpecRef:  SPEC-9")
	assert.Contains(t, stdout, "Format:   openapi")
	assert.Contains(t, stdout, "Version:  1")
	assert.Contains(t, stdout, "Created:  ")
	assert.Contains(t, stdout, "Frozen:   no")
}

func TestRunContractShow_FrozenAndADRRefs(t *testing.T) {
	store := t.TempDir()

	// Seed a frozen contract with ADR linkage (create + freeze via CLI).
	_, _, err := runContractCLI(t, "create", "SPEC-9", "--store", store)
	require.NoError(t, err)
	_, _, err = runContractCLI(t, "freeze", "SPEC-9-openapi", "--store", store)
	require.NoError(t, err)

	s, err := contract.NewContractStore(store)
	require.NoError(t, err)
	c, err := s.Load("SPEC-9-openapi")
	require.NoError(t, err)
	c.ADRRefs = []string{"adr-1"}
	require.NoError(t, s.Save(c))

	stdout, _, err := runContractCLI(t, "show", "SPEC-9-openapi", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, stdout, "Frozen:   ")
	assert.Contains(t, stdout, "(hash: ")
	assert.Contains(t, stdout, "ADRs:     adr-1")

	// --json form
	stdout, _, err = runContractCLI(t, "show", "SPEC-9-openapi", "--store", store, "--json")
	require.NoError(t, err)
	var got struct {
		ID      string   `json:"id"`
		ADRRefs []string `json:"adr_refs,omitempty"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.Equal(t, "SPEC-9-openapi", got.ID)
	assert.Equal(t, []string{"adr-1"}, got.ADRRefs)
}

// ---------------------------------------------------------------------------
// consumerNames
// ---------------------------------------------------------------------------

func TestConsumerNames(t *testing.T) {
	assert.Empty(t, consumerNames(map[string][]string{}))

	names := consumerNames(map[string][]string{
		"webapp":   {"*"},
		"cli-tool": {"users.*"},
	})
	assert.ElementsMatch(t, []string{"webapp", "cli-tool"}, names)
}
