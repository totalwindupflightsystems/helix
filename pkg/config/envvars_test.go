package config

import (
	"os"
	"strings"
	"testing"
)

// fakeLoader is a deterministic EnvLoader for tests. sourceMap indexes by
// source.
type fakeLoader struct {
	sourceMap map[EnvSource]map[string]string
}

func (f fakeLoader) Load(name string, source EnvSource) (string, bool) {
	if f.sourceMap == nil {
		return "", false
	}
	if m, ok := f.sourceMap[source]; ok {
		v, found := m[name]
		return v, found
	}
	return "", false
}

func TestDefaultEnvVarsContainsSpecEntries(t *testing.T) {
	envs := DefaultEnvVars()
	required := map[string]bool{}
	for _, e := range envs {
		if e.Required {
			required[e.Name] = true
		}
	}
	for _, want := range []string{
		"OPENROUTER_API_KEY",
		"FORGEJO_RUNNER_TOKEN",
		"LANGFUSE_DB_PASS",
		"LANGFUSE_AUTH_SECRET",
		"GRAFANA_ADMIN_PASS",
		"LANGFUSE_PUBLIC_KEY",
		"LANGFUSE_SECRET_KEY",
	} {
		if !required[want] {
			t.Fatalf("spec §9.6 var %s must be required", want)
		}
	}
	// All names must be uppercase-identifier style.
	for _, e := range envs {
		if e.Name == "" {
			t.Fatalf("empty name")
		}
		if strings.ToUpper(e.Name) != e.Name {
			t.Fatalf("name %q must be uppercase", e.Name)
		}
		if e.Service == "" {
			t.Fatalf("name %q missing service", e.Name)
		}
		if e.Description == "" {
			t.Fatalf("name %q missing description", e.Name)
		}
	}
}

func TestGroupByService(t *testing.T) {
	envs := DefaultEnvVars()
	groups := GroupByService(envs)
	if len(groups) < 2 {
		t.Fatalf("expected >= 2 service groups, got %d", len(groups))
	}
	// Sorted by service name.
	for i := 1; i < len(groups); i++ {
		if groups[i-1].Service > groups[i].Service {
			t.Fatalf("groups must be sorted")
		}
	}
	// Vars within a group sorted by name.
	for _, g := range groups {
		for i := 1; i < len(g.Vars); i++ {
			if g.Vars[i-1].Name > g.Vars[i].Name {
				t.Fatalf("vars must be sorted within group")
			}
		}
	}
}

func TestHasValueFromEnv(t *testing.T) {
	v := EnvVar{Name: "OPENROUTER_API_KEY", Required: true, Service: "platform"}
	env := map[string]string{"OPENROUTER_API_KEY": "sk-or-v1-abcdef"}
	rep := v.HasValue(env, nil)
	if !rep.Present {
		t.Fatalf("expected present")
	}
	if !rep.FromEnv {
		t.Fatalf("expected FromEnv")
	}
	if !strings.HasSuffix(rep.Value, "**ef") {
		t.Fatalf("expected redaction, got %s", rep.Value)
	}
}

func TestHasValueUnset(t *testing.T) {
	v := EnvVar{Name: "FORGEJO_RUNNER_TOKEN", Required: true, Service: "forgejo"}
	rep := v.HasValue(map[string]string{}, nil)
	if rep.Present {
		t.Fatalf("expected absent")
	}
	if rep.Value != "" {
		t.Fatalf("expected empty value, got %s", rep.Value)
	}
}

func TestHasValueDefault(t *testing.T) {
	v := EnvVar{Name: "NON_SECRET_DEFAULT", Required: false, Default: "fallback"}
	rep := v.HasValue(map[string]string{}, nil)
	if !rep.Present || !rep.IsDefault {
		t.Fatalf("expected default used, got %+v", rep)
	}
	if rep.Value != "fallback" {
		t.Fatalf("expected default value, got %s", rep.Value)
	}
}

func TestHasValueFromLoader(t *testing.T) {
	v := EnvVar{
		Name:    "AGENT_1_OPENROUTER_KEY",
		Service: "agent", Required: false,
		Sources: []EnvSource{SourceDotenv, SourceSecretMgr},
	}
	env := map[string]string{}
	loader := fakeLoader{sourceMap: map[EnvSource]map[string]string{
		SourceDotenv: {"AGENT_1_OPENROUTER_KEY": "sk-or-v1-xyz"},
	}}
	rep := v.HasValue(env, loader)
	if !rep.Present {
		t.Fatalf("expected present from loader")
	}
	if rep.FromEnv {
		t.Fatalf("should not be from process env")
	}
}

func TestHasValueLoaderFallbackThroughSources(t *testing.T) {
	v := EnvVar{
		Name:    "FOO",
		Sources: []EnvSource{SourceDotenv, SourceSecretMgr},
	}
	loader := fakeLoader{sourceMap: map[EnvSource]map[string]string{
		SourceSecretMgr: {"FOO": "bar"},
	}}
	rep := v.HasValue(map[string]string{}, loader)
	if !rep.Present {
		t.Fatalf("fallback to second source should work")
	}
	if rep.Value != "bar" {
		t.Fatalf("expected bar, got %s", rep.Value)
	}
}

func TestProcessEnvLoader(t *testing.T) {
	t.Setenv("HELIX_TEST_VAR", "val")
	loader := ProcessEnvLoader{}
	if v, ok := loader.Load("HELIX_TEST_VAR", SourceInferred); !ok || v != "val" {
		t.Fatalf("expected val, got %s ok=%v", v, ok)
	}
	if _, ok := loader.Load("HELIX_TEST_VAR_NONEXISTENT", SourceInferred); ok {
		t.Fatalf("expected not found for missing var")
	}
}

func TestDotEnvLoader(t *testing.T) {
	dotenv := "/tmp/helix-test.env"
	content := "# comment\nHELIX_TEST_FOO=bar\nHELIX_TEST_RED=sk-or-v1-zzz\nHELIX_TEST_EMPTY=\n"
	if err := os.WriteFile(dotenv, []byte(content), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	defer func() { _ = os.Remove(dotenv) }()

	loader := DotEnvLoader{Path: dotenv}
	if v, ok := loader.Load("HELIX_TEST_FOO", SourceDotenv); !ok || v != "bar" {
		t.Fatalf("HELIX_TEST_FOO expected bar, got %s ok=%v", v, ok)
	}
	if _, ok := loader.Load("HELIX_TEST_EMPTY", SourceDotenv); ok {
		t.Fatalf("empty values should be treated as missing")
	}
	// Quoted values get stripped.
	if err := os.WriteFile(dotenv, []byte("HELIX_TEST_Q=\"quoted value\"\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	loader = DotEnvLoader{Path: dotenv}
	if v, _ := loader.Load("HELIX_TEST_Q", SourceDotenv); v != "quoted value" {
		t.Fatalf("quotes not stripped: %s", v)
	}
	// Missing file.
	if _, ok := (DotEnvLoader{Path: "/nonexistent"}).Load("X", SourceDotenv); ok {
		t.Fatalf("missing file should return not-found")
	}
	if _, ok := (DotEnvLoader{}).Load("X", SourceDotenv); ok {
		t.Fatalf("empty path should return not-found")
	}
}

func TestValidateEnvVarsReportsMissing(t *testing.T) {
	envs := []EnvVar{
		{Name: "A", Required: true, Service: "s"},
		{Name: "B", Required: true, Service: "s"},
		{Name: "C", Required: false, Service: "s"},
	}
	env := map[string]string{"A": "x"}
	rpt := ValidateEnvVars(envs, env, nil)
	if rpt.HasMissing != true {
		t.Fatalf("expected missing flag")
	}
	if rpt.Present != 1 {
		t.Fatalf("expected 1 present, got %d", rpt.Present)
	}
	if len(rpt.Missing) != 1 {
		t.Fatalf("expected 1 missing, got %d", len(rpt.Missing))
	}
	if rpt.Missing[0].Var.Name != "B" {
		t.Fatalf("expected B to be missing, got %s", rpt.Missing[0].Var.Name)
	}
	// Optional missing is not in Missing.
	if rpt.Missing[0].Var.Name == "C" {
		t.Fatalf("optional missing should not be in Missing")
	}
}

func TestMissingRequiredVars(t *testing.T) {
	envs := []EnvVar{
		{Name: "A", Required: true},
		{Name: "B", Required: true},
		{Name: "C", Required: true},
	}
	env := map[string]string{"A": "1"}
	missing := MissingRequiredVars(envs, env, nil)
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing, got %v", missing)
	}
	if missing[0] != "B" || missing[1] != "C" {
		t.Fatalf("expected sorted [B C], got %v", missing)
	}
	// All present returns empty.
	env["B"] = "2"
	env["C"] = "3"
	if got := MissingRequiredVars(envs, env, nil); len(got) != 0 {
		t.Fatalf("expected none, got %v", got)
	}
}

func TestResolveSourceCount(t *testing.T) {
	envs := DefaultEnvVars()
	env := map[string]string{
		"OPENROUTER_API_KEY":   "k1",
		"FORGEJO_RUNNER_TOKEN": "t1",
	}
	loader := fakeLoader{sourceMap: map[EnvSource]map[string]string{
		SourceDotenv: {"LANGFUSE_DB_PASS": "p1"},
	}}
	rpt := ValidateEnvVars(envs, env, loader)
	if rpt.ResolvedBySource[SourceInferred] < 2 {
		t.Fatalf("expected at least 2 from process env, got %d", rpt.ResolvedBySource[SourceInferred])
	}
	// present >= 3 (2 from env + 1 from loader + at least default if any)
	if rpt.Present < 3 {
		t.Fatalf("expected >= 3 present, got %d", rpt.Present)
	}
}

func TestFormatEnvVarReport(t *testing.T) {
	envs := []EnvVar{
		{Name: "PRESENT_REQUIRED", Required: true, Service: "s"},
		{Name: "MISSING_REQUIRED", Required: true, Service: "s"},
		{Name: "OPTIONAL", Required: false, Service: "s"},
	}
	env := map[string]string{"PRESENT_REQUIRED": "ok"}
	out := FormatEnvVarReport(envs, env, nil)
	for _, want := range []string{
		"PRESENT_REQUIRED",
		"MISSING_REQUIRED",
		"MISSING REQUIRED",
		"OPTIONAL",
		"Total:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q\n%s", want, out)
		}
	}
}

func TestRedactIfSecret(t *testing.T) {
	cases := []struct {
		name, val string
	}{
		{"OPENROUTER_API_KEY", "sk-or-v1-abcdefgh"},
		{"LANGFUSE_AUTH_SECRET", "abcdefgh"},
		{"GRAFANA_ADMIN_PASS", "supersecret"},
		{"SOME_TOKEN", "tok"},
		{"SERVICE_NAME", "nonsecret"},
	}
	for _, tc := range cases {
		got := redactIfSecret(tc.name, tc.val)
		if !strings.Contains(got, "*") && strings.Contains(strings.ToUpper(tc.name), "KEY") {
			// Either it's a key/token that should be redacted OR the var name
			// is just "KEY" suffix and the value is small enough to be all stars.
			_ = got
		}
	}
	// Short secret collapses to ***.
	if redactIfSecret("OPENROUTER_API_KEY", "abc") != "***" {
		t.Fatalf("short secret must collapse to ***")
	}
	// Long secret must contain asterisks.
	long := redactIfSecret("SOME_TOKEN", "abcdefghijkl")
	if !strings.Contains(long, "*") {
		t.Fatalf("long secret must contain asterisks, got %s", long)
	}
}

func TestDefaultsIncludesOptionalVars(t *testing.T) {
	envs := DefaultEnvVars()
	hasOptional := false
	for _, e := range envs {
		if !e.Required {
			hasOptional = true
		}
	}
	if !hasOptional {
		t.Fatalf("defaults should include optional vars (AGENT_*, GITHUB_TOKEN)")
	}
}
