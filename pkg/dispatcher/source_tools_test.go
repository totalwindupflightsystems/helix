package dispatcher

// source_tools_test.go — SRC-005 dispatcher wiring tests (SPEC-025 §5/§6).
//
// Covers: capability gating tiers (read-write / read-only / none),
// AllowedAgents + ReadOnly policy through the dispatcher path, per-source
// rate limiting before tool execution, clean failure when the tools provider
// (Muster) is unreachable, nil-safety, and empty-sources handling.
// No test in this file touches a live Muster: all providers are static.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/identity"
	"github.com/totalwindupflightsystems/helix/pkg/integration"
	"github.com/totalwindupflightsystems/helix/pkg/source"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// testTool builds a minimal MCP tool descriptor.
func testTool(name, method string) integration.MCPTool {
	return integration.MCPTool{
		Name:        name,
		Description: "tool " + name,
		Method:      method,
		Path:        "/api/" + name,
	}
}

// testToolSet builds a ToolSet with the given tools for source name.
func testToolSet(name string, tools ...integration.MCPTool) source.ToolSet {
	return source.ToolSet{
		SourceName:  name,
		Tools:       tools,
		ToolCount:   len(tools),
		GeneratedAt: time.Now(),
	}
}

// dbTools is a mixed read/write tool set for the "db" source.
func dbTools() []integration.MCPTool {
	return []integration.MCPTool{
		testTool("db_query", "GET"),
		testTool("db_export", "GET"),
		testTool("db_insert", "POST"),
		testTool("db_delete", "DELETE"),
	}
}

// testSources returns a small source configuration used across tests.
// "db" carries a rate limit so the Wait path is exercisable.
func testSources() map[string]source.Source {
	return map[string]source.Source{
		"db": {
			Name:       "db",
			Type:       source.SourceTypePostgres,
			Connection: "postgres://localhost/db",
			OpenAPI:    "db.yaml",
			RateLimit:  "2/s",
		},
		"crm": {
			Name:    "crm",
			Type:    source.SourceTypeREST,
			BaseURL: "http://crm.internal",
			OpenAPI: "crm.yaml",
		},
	}
}

// claim builds a capability claim.
func claim(domain string, strength int) identity.CapabilityClaim {
	return identity.CapabilityClaim{Domain: domain, Strength: strength, Evidence: "test"}
}

// staticToolsProvider returns a ToolsProvider that serves pre-built ToolSets
// by source name — the Muster-free stand-in used by every test here. Sources
// without a stubbed set yield an empty ToolSet (as generation would for any
// configured source); the gateway drops sets the agent cannot use.
func staticToolsProvider(sets map[string]source.ToolSet) ToolsProvider {
	return func(_ context.Context, src *source.Source) (*source.ToolSet, error) {
		ts, ok := sets[src.Name]
		if !ok {
			return &source.ToolSet{SourceName: src.Name}, nil
		}
		cp := ts
		cp.SourceName = src.Name
		return &cp, nil
	}
}

// errToolsProvider simulates an unreachable Muster: every generation fails.
func errToolsProvider(err error) ToolsProvider {
	return func(context.Context, *source.Source) (*source.ToolSet, error) {
		return nil, err
	}
}

// mustInjector constructs an injector for testSources, failing the test on
// construction error.
func mustInjector(t *testing.T, sources map[string]source.Source, provider ToolsProvider) *SourceToolInjector {
	t.Helper()
	in, err := NewSourceToolInjector(sources, WithToolsProvider(provider))
	if err != nil {
		t.Fatalf("NewSourceToolInjector() error: %v", err)
	}
	return in
}

// collectMethods returns the HTTP methods of every tool in the sets, in order.
func collectMethods(sets []source.ToolSet) []string {
	var methods []string
	for _, ts := range sets {
		for _, tool := range ts.Tools {
			methods = append(methods, tool.Method)
		}
	}
	return methods
}

// ---------------------------------------------------------------------------
// AC1 — capability gating tiers
// ---------------------------------------------------------------------------

// TestSourceToolInjector_Inject_ReadWriteStrength: strength 7–10 keeps the
// full tool set, write methods included (SPEC-025 §5 "database: 98%").
func TestSourceToolInjector_Inject_ReadWriteStrength(t *testing.T) {
	in := mustInjector(t, testSources(), staticToolsProvider(map[string]source.ToolSet{
		"db": testToolSet("db", dbTools()...),
	}))
	agent := AgentProfile{Name: "alice", Capabilities: []identity.CapabilityClaim{claim("db", 9)}}

	sets, err := in.Inject(context.Background(), agent)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("Inject() = %d sets, want 1", len(sets))
	}
	if got := sets[0].SourceName; got != "db" {
		t.Fatalf("set source = %q, want %q", got, "db")
	}
	if got, want := len(sets[0].Tools), 4; got != want {
		t.Fatalf("tool count = %d, want %d (full set preserved)", got, want)
	}
	methods := collectMethods(sets)
	for _, m := range []string{"GET", "GET", "POST", "DELETE"} {
		if !containsStr(methods, m) {
			t.Fatalf("methods %v: missing %s (read-write access must keep all methods)", methods, m)
		}
	}
}

// TestSourceToolInjector_Inject_ReadOnlyStrength: strength 1–6 keeps only
// GET/HEAD/OPTIONS; write methods are stripped (SPEC-025 §5 "database: 82%").
func TestSourceToolInjector_Inject_ReadOnlyStrength(t *testing.T) {
	in := mustInjector(t, testSources(), staticToolsProvider(map[string]source.ToolSet{
		"db": testToolSet("db", dbTools()...),
	}))
	agent := AgentProfile{Name: "alice", Capabilities: []identity.CapabilityClaim{claim("db", 4)}}

	sets, err := in.Inject(context.Background(), agent)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("Inject() = %d sets, want 1", len(sets))
	}
	methods := collectMethods(sets)
	if len(methods) != 2 {
		t.Fatalf("methods = %v, want exactly 2 read-only methods", methods)
	}
	for _, m := range methods {
		if m != "GET" && m != "HEAD" && m != "OPTIONS" {
			t.Fatalf("methods = %v, read-only strength leaked method %q", methods, m)
		}
	}
}

// TestSourceToolInjector_Inject_NoMatchingClaim: an agent whose claims do not
// match the source gets zero source tools (SPEC-025 §5 "no database
// capability → zero database tools").
func TestSourceToolInjector_Inject_NoMatchingClaim(t *testing.T) {
	in := mustInjector(t, testSources(), staticToolsProvider(map[string]source.ToolSet{
		"db": testToolSet("db", dbTools()...),
	}))
	agent := AgentProfile{Name: "alice", Capabilities: []identity.CapabilityClaim{claim("web", 10)}}

	sets, err := in.Inject(context.Background(), agent)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if len(sets) != 0 {
		t.Fatalf("Inject() = %d sets, want 0 (unmatched capability must grant nothing)", len(sets))
	}
}

// TestSourceToolInjector_Inject_NoClaims: an agent with no capability claims
// at all receives zero source tools.
func TestSourceToolInjector_Inject_NoClaims(t *testing.T) {
	in := mustInjector(t, testSources(), staticToolsProvider(map[string]source.ToolSet{
		"db": testToolSet("db", dbTools()...),
	}))
	agent := AgentProfile{Name: "alice"}

	sets, err := in.Inject(context.Background(), agent)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if len(sets) != 0 {
		t.Fatalf("Inject() = %d sets, want 0 (no claims must grant nothing)", len(sets))
	}
}

// ---------------------------------------------------------------------------
// AC2 — AllowedAgents and ReadOnly source policy through the dispatcher path
// ---------------------------------------------------------------------------

// TestSourceToolInjector_Inject_AllowedAgents: a source with an allow-list
// drops agents not on it, even when their capability claim matches.
func TestSourceToolInjector_Inject_AllowedAgents(t *testing.T) {
	sources := testSources()
	crm := sources["crm"]
	crm.AllowedAgents = []string{"carol"}
	sources["crm"] = crm

	in := mustInjector(t, sources, staticToolsProvider(map[string]source.ToolSet{
		"db":  testToolSet("db", dbTools()...),
		"crm": testToolSet("crm", testTool("crm_get_contact", "GET"), testTool("crm_create_contact", "POST")),
	}))

	// alice has strong claims on both sources but is not on crm's allow-list.
	alice := AgentProfile{Name: "alice", Capabilities: []identity.CapabilityClaim{
		claim("db", 9), claim("crm", 9),
	}}
	sets, err := in.Inject(context.Background(), alice)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("alice: Inject() = %d sets, want 1 (crm must be dropped by allow-list)", len(sets))
	}
	if sets[0].SourceName != "db" {
		t.Fatalf("alice: set source = %q, want %q", sets[0].SourceName, "db")
	}

	// carol is on the allow-list and gets both sources.
	carol := AgentProfile{Name: "carol", Capabilities: []identity.CapabilityClaim{
		claim("db", 9), claim("crm", 9),
	}}
	sets, err = in.Inject(context.Background(), carol)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if len(sets) != 2 {
		t.Fatalf("carol: Inject() = %d sets, want 2 (allow-listed agent gets both sources)", len(sets))
	}
}

// TestSourceToolInjector_Inject_ReadOnlySource: a source configured
// read_only strips write methods even from a strength-9 agent.
func TestSourceToolInjector_Inject_ReadOnlySource(t *testing.T) {
	sources := testSources()
	sources["fs"] = source.Source{
		Name:     "fs",
		Type:     source.SourceTypeLocal,
		Root:     "/data/shared",
		ReadOnly: true,
	}
	in := mustInjector(t, sources, staticToolsProvider(map[string]source.ToolSet{
		"fs": testToolSet("fs", testTool("fs_read", "GET"), testTool("fs_write", "PUT")),
	}))
	agent := AgentProfile{Name: "alice", Capabilities: []identity.CapabilityClaim{claim("fs", 9)}}

	sets, err := in.Inject(context.Background(), agent)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("Inject() = %d sets, want 1", len(sets))
	}
	methods := collectMethods(sets)
	if len(methods) != 1 || methods[0] != "GET" {
		t.Fatalf("methods = %v, want [GET] (read_only source must strip PUT even at strength 9)", methods)
	}
}

// ---------------------------------------------------------------------------
// AC5 — provider failure is a clean error; partial success keeps winners
// ---------------------------------------------------------------------------

// TestSourceToolInjector_Inject_ProviderError: an unreachable Muster (every
// generation failing) yields a clean error and zero sets — no panic, no hang.
func TestSourceToolInjector_Inject_ProviderError(t *testing.T) {
	in := mustInjector(t, testSources(), errToolsProvider(errors.New("connection refused")))
	agent := AgentProfile{Name: "alice", Capabilities: []identity.CapabilityClaim{claim("db", 9)}}

	sets, err := in.Inject(context.Background(), agent)
	if err == nil {
		t.Fatal("Inject() error = nil, want provider error")
	}
	if len(sets) != 0 {
		t.Fatalf("Inject() = %d sets, want 0 when every source failed", len(sets))
	}
	if !strings.Contains(err.Error(), "db") {
		t.Fatalf("error %q should name the failing source", err)
	}
}

// TestSourceToolInjector_Inject_PartialFailure: one source failing does not
// starve the others; the error is joined and successes are still gated.
func TestSourceToolInjector_Inject_PartialFailure(t *testing.T) {
	sets := map[string]source.ToolSet{
		"db":  testToolSet("db", dbTools()...),
		"crm": testToolSet("crm", testTool("crm_get", "GET")),
	}
	in := mustInjector(t, testSources(), staticToolsProvider(sets))
	// Break only the crm provider entry.
	in.provider = func(ctx context.Context, src *source.Source) (*source.ToolSet, error) {
		if src.Name == "crm" {
			return nil, errors.New("muster timeout")
		}
		return staticToolsProvider(sets)(ctx, src)
	}
	agent := AgentProfile{Name: "alice", Capabilities: []identity.CapabilityClaim{claim("db", 9), claim("crm", 9)}}

	got, err := in.Inject(context.Background(), agent)
	if err == nil {
		t.Fatal("Inject() error = nil, want joined error naming crm")
	}
	if !strings.Contains(err.Error(), "crm") {
		t.Fatalf("error %q should name the failed source", err)
	}
	if len(got) != 1 || got[0].SourceName != "db" {
		t.Fatalf("Inject() = %v, want surviving db set only", got)
	}
	if methods := collectMethods(got); len(methods) != 4 {
		t.Fatalf("db methods = %v, want full 4-tool set preserved", methods)
	}
}

// TestSourceToolInjector_Inject_NilReceiver: nil injector fails cleanly.
func TestSourceToolInjector_Inject_NilReceiver(t *testing.T) {
	var in *SourceToolInjector
	sets, err := in.Inject(context.Background(), AgentProfile{Name: "alice"})
	if err == nil {
		t.Fatal("Inject() on nil receiver: error = nil, want error")
	}
	if sets != nil {
		t.Fatalf("Inject() on nil receiver = %d sets, want nil", len(sets))
	}
}

// TestSourceToolInjector_Inject_NoProvider: constructed without a provider,
// Inject fails with a descriptive error instead of panicking.
func TestSourceToolInjector_Inject_NoProvider(t *testing.T) {
	in, err := NewSourceToolInjector(testSources())
	if err != nil {
		t.Fatalf("NewSourceToolInjector() error: %v", err)
	}
	agent := AgentProfile{Name: "alice", Capabilities: []identity.CapabilityClaim{claim("db", 9)}}
	if _, err := in.Inject(context.Background(), agent); err == nil {
		t.Fatal("Inject() error = nil, want no-provider error")
	}
}

// TestSourceToolInjector_Inject_EmptySources: no configured sources yields
// an empty (non-nil) result and no error, even with a provider attached.
func TestSourceToolInjector_Inject_EmptySources(t *testing.T) {
	in := mustInjector(t, map[string]source.Source{}, staticToolsProvider(nil))
	agent := AgentProfile{Name: "alice", Capabilities: []identity.CapabilityClaim{claim("db", 9)}}

	sets, err := in.Inject(context.Background(), agent)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if sets == nil || len(sets) != 0 {
		t.Fatalf("Inject() = %v, want empty non-nil slice", sets)
	}
}

// ---------------------------------------------------------------------------
// AC4 — rate limiting: Wait/WaitForTool gate execution, unknown names error
// ---------------------------------------------------------------------------

// TestSourceToolInjector_WaitForTool_GatesExecution: WaitForTool resolves the
// tool's source and blocks once the source's token bucket is exhausted,
// proving the wait is invoked before (further) execution would proceed.
func TestSourceToolInjector_WaitForTool_GatesExecution(t *testing.T) {
	in := mustInjector(t, testSources(), staticToolsProvider(map[string]source.ToolSet{
		"db": testToolSet("db", dbTools()...),
	}))
	agent := AgentProfile{Name: "alice", Capabilities: []identity.CapabilityClaim{claim("db", 9)}}
	sets, err := in.Inject(context.Background(), agent)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}

	ctx := context.Background()
	// Burst is 2 ("2/s"): the first two waits pass immediately.
	for i := 0; i < 2; i++ {
		if err := in.WaitForTool(ctx, "db_query", sets); err != nil {
			t.Fatalf("WaitForTool() #%d error: %v", i+1, err)
		}
	}
	// The third wait needs a refill (>500ms away): a short-deadline context
	// must be cut off — execution would have proceeded without the gate.
	// (x/time/rate reports this as a plain "would exceed context deadline"
	// error rather than wrapping context.DeadlineExceeded.)
	shortCtx, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
	defer cancel()
	err = in.WaitForTool(shortCtx, "db_query", sets)
	if err == nil {
		t.Fatal("WaitForTool() after burst exhaustion = nil, want deadline error (gate did not block)")
	}
	if !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("WaitForTool() after burst exhaustion = %v, want deadline error", err)
	}
}

// TestSourceToolInjector_Wait_UnknownSource: an unknown source name is an
// error, so a typo or dropped source cannot silently bypass rate limiting.
func TestSourceToolInjector_Wait_UnknownSource(t *testing.T) {
	in := mustInjector(t, testSources(), staticToolsProvider(nil))
	if err := in.Wait(context.Background(), "ghost"); err == nil {
		t.Fatal("Wait() error = nil, want unknown-source error")
	}
}

// TestSourceToolInjector_Wait_NilReceiver: nil injector fails cleanly.
func TestSourceToolInjector_Wait_NilReceiver(t *testing.T) {
	var in *SourceToolInjector
	if err := in.Wait(context.Background(), "db"); err == nil {
		t.Fatal("Wait() on nil receiver: error = nil, want error")
	}
}

// TestSourceToolInjector_WaitForTool_UnknownTool: an unresolvable tool name
// errors before any wait happens.
func TestSourceToolInjector_WaitForTool_UnknownTool(t *testing.T) {
	in := mustInjector(t, testSources(), staticToolsProvider(nil))
	if err := in.WaitForTool(context.Background(), "no_such_tool", nil); err == nil {
		t.Fatal("WaitForTool() error = nil, want unknown-tool error")
	}
}

// TestSourceToolInjector_WaitForTool_UnknownSource: a tool whose owning
// source is not in the rate-limit manager errors (source dropped from config
// but still attached to the work item).
func TestSourceToolInjector_WaitForTool_UnknownSource(t *testing.T) {
	in := mustInjector(t, testSources(), staticToolsProvider(nil))
	sets := []source.ToolSet{testToolSet("ghost", testTool("ghost_read", "GET"))}
	if err := in.WaitForTool(context.Background(), "ghost_read", sets); err == nil {
		t.Fatal("WaitForTool() error = nil, want unknown-source error for ghost")
	}
}

// TestSourceToolInjector_SourceForTool: source attribution resolution —
// the carrier keeps each tool's source name so rate limiting can key on it.
func TestSourceToolInjector_SourceForTool(t *testing.T) {
	in := mustInjector(t, testSources(), staticToolsProvider(nil))
	sets := []source.ToolSet{
		testToolSet("db", testTool("db_query", "GET")),
		testToolSet("crm", testTool("crm_get", "GET")),
	}

	if src, ok := in.SourceForTool("db_query", sets); !ok || src != "db" {
		t.Fatalf("SourceForTool(db_query) = %q, %v; want %q, true", src, ok, "db")
	}
	if src, ok := in.SourceForTool("crm_get", sets); !ok || src != "crm" {
		t.Fatalf("SourceForTool(crm_get) = %q, %v; want %q, true", src, ok, "crm")
	}
	if src, ok := in.SourceForTool("nope", sets); ok || src != "" {
		t.Fatalf("SourceForTool(nope) = %q, %v; want %q, false", src, ok, "")
	}
	if src, ok := in.SourceForTool("", sets); ok || src != "" {
		t.Fatalf("SourceForTool(empty) = %q, %v; want %q, false", src, ok, "")
	}
}

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

// TestNewSourceToolInjector_InvalidRateLimit: a malformed rate_limit is an
// error at construction, not a silent misconfiguration at dispatch.
func TestNewSourceToolInjector_InvalidRateLimit(t *testing.T) {
	sources := map[string]source.Source{
		"db": {Name: "db", Type: source.SourceTypePostgres, Connection: "x", OpenAPI: "db.yaml", RateLimit: "bogus"},
	}
	if _, err := NewSourceToolInjector(sources); err == nil {
		t.Fatal("NewSourceToolInjector() error = nil, want invalid rate_limit error")
	}
}

// TestNewSourceToolInjector_NilSources: a nil sources map behaves as empty.
func TestNewSourceToolInjector_NilSources(t *testing.T) {
	in, err := NewSourceToolInjector(nil, WithToolsProvider(staticToolsProvider(nil)))
	if err != nil {
		t.Fatalf("NewSourceToolInjector(nil) error: %v", err)
	}
	sets, err := in.Inject(context.Background(), AgentProfile{Name: "alice"})
	if err != nil || len(sets) != 0 {
		t.Fatalf("Inject() = %v, %v; want empty, nil", sets, err)
	}
}

// TestNewSourceToolInjector_WithMusterBridge: the bridge option wires
// GenerateToolsFromSource as the provider without touching the network.
func TestNewSourceToolInjector_WithMusterBridge(t *testing.T) {
	bridge := source.NewMusterBridge(source.BridgeConfig{})
	in, err := NewSourceToolInjector(map[string]source.Source{}, WithMusterBridge(bridge))
	if err != nil {
		t.Fatalf("NewSourceToolInjector() error: %v", err)
	}
	// Empty sources: Inject returns before any provider call, so this never
	// contacts localhost:9090.
	sets, err := in.Inject(context.Background(), AgentProfile{Name: "alice"})
	if err != nil || len(sets) != 0 {
		t.Fatalf("Inject() = %v, %v; want empty, nil", sets, err)
	}
}

// TestNewSourceToolInjectorFromFile_MissingFile: a missing sources file is
// not an error; the injector simply has no sources.
func TestNewSourceToolInjectorFromFile_MissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	in, err := NewSourceToolInjectorFromFile(missing, WithToolsProvider(staticToolsProvider(nil)))
	if err != nil {
		t.Fatalf("NewSourceToolInjectorFromFile(missing) error: %v", err)
	}
	sets, err := in.Inject(context.Background(), AgentProfile{Name: "alice"})
	if err != nil || len(sets) != 0 {
		t.Fatalf("Inject() = %v, %v; want empty, nil", sets, err)
	}
}

// TestNewSourceToolInjectorFromFile_Parses: a real sources file is parsed
// and its sources are gated exactly like programmatic ones.
func TestNewSourceToolInjectorFromFile_Parses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.yaml")
	content := "sources:\n" +
		"  db:\n" +
		"    type: postgres\n" +
		"    connection: postgres://localhost/db\n" +
		"    openapi: db.yaml\n" +
		"    rate_limit: 2/s\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	in, err := NewSourceToolInjectorFromFile(path, WithToolsProvider(staticToolsProvider(map[string]source.ToolSet{
		"db": testToolSet("db", dbTools()...),
	})))
	if err != nil {
		t.Fatalf("NewSourceToolInjectorFromFile() error: %v", err)
	}
	agent := AgentProfile{Name: "alice", Capabilities: []identity.CapabilityClaim{claim("db", 9)}}
	sets, err := in.Inject(context.Background(), agent)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if len(sets) != 1 || sets[0].SourceName != "db" {
		t.Fatalf("Inject() = %v, want single db set", sets)
	}
	if methods := collectMethods(sets); len(methods) != 4 {
		t.Fatalf("db methods = %v, want full set from parsed config", methods)
	}
}

// ---------------------------------------------------------------------------
// AC1/AC3 — end-to-end through Dispatcher.Dispatch
// ---------------------------------------------------------------------------

// TestDispatcher_Dispatch_AttachesGatedTools: Dispatch attaches the
// capability-gated sets to the assigned agent's WorkItem; an agent without a
// matching claim gets zero source tools. Existing assignment semantics are
// unchanged.
func TestDispatcher_Dispatch_AttachesGatedTools(t *testing.T) {
	in := mustInjector(t, testSources(), staticToolsProvider(map[string]source.ToolSet{
		"db": testToolSet("db", dbTools()...),
	}))
	d := NewDispatcher(nil).WithSourceTools(in)

	alice := AgentProfile{
		Name:         "alice",
		Capability:   "code",
		MaxLoad:      1,
		Capabilities: []identity.CapabilityClaim{claim("db", 9)},
	}
	bob := AgentProfile{
		Name:       "bob",
		Capability: "code",
		MaxLoad:    1,
	}
	tasks := []Task{
		{ID: "t1", Description: "write code", Priority: 1},
		{ID: "t2", Description: "write code", Priority: 2},
	}

	results, err := d.Dispatch(tasks, []AgentProfile{alice, bob})
	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Dispatch() = %d results, want 2", len(results))
	}

	for _, r := range results {
		if r.Error != "" {
			t.Fatalf("result for %s has error: %s", r.WorkItem.Agent.Name, r.Error)
		}
		if r.WorkItem.Agent.Name == "alice" {
			if len(r.WorkItem.SourceTools) != 1 || r.WorkItem.SourceTools[0].SourceName != "db" {
				t.Fatalf("alice SourceTools = %v, want db set", r.WorkItem.SourceTools)
			}
			if methods := collectMethods(r.WorkItem.SourceTools); len(methods) != 4 {
				t.Fatalf("alice methods = %v, want full 4-tool set", methods)
			}
			if r.WorkItem.SourceToolsError != "" {
				t.Fatalf("alice SourceToolsError = %q, want empty", r.WorkItem.SourceToolsError)
			}
		}
		if r.WorkItem.Agent.Name == "bob" {
			if len(r.WorkItem.SourceTools) != 0 {
				t.Fatalf("bob SourceTools = %v, want zero tools (no matching claim)", r.WorkItem.SourceTools)
			}
		}
	}
}

// TestDispatcher_Dispatch_ReadOnlyStrengthEndToEnd: the read-only tier is
// honored through the full Dispatch path, not just Inject.
func TestDispatcher_Dispatch_ReadOnlyStrengthEndToEnd(t *testing.T) {
	in := mustInjector(t, testSources(), staticToolsProvider(map[string]source.ToolSet{
		"db": testToolSet("db", dbTools()...),
	}))
	d := NewDispatcher(nil).WithSourceTools(in)
	agent := AgentProfile{
		Name:         "alice",
		Capability:   "code",
		MaxLoad:      1,
		Capabilities: []identity.CapabilityClaim{claim("db", 3)},
	}
	results, err := d.Dispatch([]Task{{ID: "t1", Description: "write code", Priority: 1}}, []AgentProfile{agent})
	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Dispatch() = %d results, want 1", len(results))
	}
	methods := collectMethods(results[0].WorkItem.SourceTools)
	if len(methods) != 2 {
		t.Fatalf("methods = %v, want exactly 2 read-only methods", methods)
	}
}

// TestDispatcher_Dispatch_ProviderFailureIsRecorded: Muster unreachable at
// dispatch time records the error on the work item without failing the
// assignment (AC5 through the production path).
func TestDispatcher_Dispatch_ProviderFailureIsRecorded(t *testing.T) {
	in := mustInjector(t, testSources(), errToolsProvider(errors.New("connection refused")))
	d := NewDispatcher(nil).WithSourceTools(in)
	agent := AgentProfile{
		Name:         "alice",
		Capability:   "code",
		MaxLoad:      1,
		Capabilities: []identity.CapabilityClaim{claim("db", 9)},
	}
	results, err := d.Dispatch([]Task{{ID: "t1", Description: "write code", Priority: 1}}, []AgentProfile{agent})
	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Dispatch() = %d results, want 1", len(results))
	}
	r := results[0]
	if r.Error != "" {
		t.Fatalf("assignment error = %q, want empty (assignment must stand)", r.Error)
	}
	if len(r.WorkItem.SourceTools) != 0 {
		t.Fatalf("SourceTools = %v, want empty when generation failed", r.WorkItem.SourceTools)
	}
	if r.WorkItem.SourceToolsError == "" {
		t.Fatal("SourceToolsError = \"\", want recorded provider error")
	}
}

// TestDispatcher_Dispatch_NoInjector: without an injector, Dispatch behaves
// exactly as before — no source tools, no errors.
func TestDispatcher_Dispatch_NoInjector(t *testing.T) {
	d := NewDispatcher(nil)
	agent := AgentProfile{Name: "alice", Capability: "code", MaxLoad: 1}
	results, err := d.Dispatch([]Task{{ID: "t1", Description: "write code", Priority: 1}}, []AgentProfile{agent})
	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if len(results) != 1 || results[0].Error != "" {
		t.Fatalf("results = %+v, want single clean result", results)
	}
	if results[0].WorkItem.SourceTools != nil || results[0].WorkItem.SourceToolsError != "" {
		t.Fatalf("work item unexpectedly carries source tools: %+v", results[0].WorkItem)
	}
}

// TestDispatcher_WithSourceTools: the setter is additive and nil-safe.
func TestDispatcher_WithSourceTools(t *testing.T) {
	d := NewDispatcher(nil)
	in := mustInjector(t, testSources(), staticToolsProvider(nil))
	if got := d.WithSourceTools(in); got != d {
		t.Fatal("WithSourceTools() did not return the same dispatcher")
	}
	if d.SourceTools != in {
		t.Fatal("WithSourceTools() did not attach the injector")
	}
	var nilD *Dispatcher
	if got := nilD.WithSourceTools(in); got != nil {
		t.Fatal("WithSourceTools() on nil receiver should return nil")
	}
}

// containsStr reports whether s appears in xs.
func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
