// Package config — envvars.go implements the spec §9.6 Env Var Inventory.
// It encodes every documented platform environment variable as a typed
// record, validates that all required vars are present in a process's
// environment (or .env file), and produces human-readable inventory reports
// suitable for CLI output.

package config

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

// EnvSource enumerates where an environment variable is typically set. The
// spec groups env vars by service; this classification helps operators locate
// the right .env file when something is missing.
type EnvSource string

const (
	SourceDotenv        EnvSource = ".env"
	SourceDockerCompose EnvSource = "docker-compose.yml"
	SourceSystemd       EnvSource = "systemd unit"
	SourceSecretMgr     EnvSource = "secret manager"
	SourceFlag          EnvSource = "cli flag"
	SourceInferred      EnvSource = "process env"
)

// EnvVar is a single documented environment variable.
type EnvVar struct {
	// Name is the canonical variable name (uppercase + underscores).
	Name string
	// Service is the Helix component or external service this var belongs to.
	Service string
	// Description is a one-line explanation shown in reports.
	Description string
	// Required indicates whether the var must be present for the platform
	// to start. Optional vars are nice-to-have (e.g. observability exports).
	Required bool
	// Default is the value used if the variable is unset. Empty means there
	// is no safe default — the operator MUST supply it.
	Default string
	// Sources is an ordered list of where this variable may be defined.
	Sources []EnvSource
}

// DefaultEnvVars returns the canonical inventory declared by spec §9.6.
// The list is in source-document order so reports match the spec text.
func DefaultEnvVars() []EnvVar {
	return []EnvVar{
		{
			Name:        "OPENROUTER_API_KEY",
			Service:     "platform",
			Description: "Master OpenRouter key used by Chimera and other LLM-routed components",
			Required:    true,
			Sources:     []EnvSource{SourceDotenv, SourceSecretMgr},
		},
		{
			Name:        "FORGEJO_RUNNER_TOKEN",
			Service:     "forgejo",
			Description: "Registration token used by the Forgejo CI runner on first connect",
			Required:    true,
			Sources:     []EnvSource{SourceDotenv, SourceDockerCompose},
		},
		{
			Name:        "LANGFUSE_DB_PASS",
			Service:     "langfuse",
			Description: "PostgreSQL password for the LangFuse traces database",
			Required:    true,
			Sources:     []EnvSource{SourceDotenv, SourceDockerCompose, SourceSystemd},
		},
		{
			Name:        "LANGFUSE_AUTH_SECRET",
			Service:     "langfuse",
			Description: "NextAuth secret used to sign LangFuse session tokens",
			Required:    true,
			Sources:     []EnvSource{SourceDotenv, SourceSecretMgr},
		},
		{
			Name:        "GRAFANA_ADMIN_PASS",
			Service:     "grafana",
			Description: "Grafana admin user password for first-login bootstrap",
			Required:    true,
			Sources:     []EnvSource{SourceDotenv, SourceSecretMgr},
		},
		{
			Name:        "AGENT_N_OPENROUTER_KEY",
			Service:     "agent",
			Description: "Per-agent OpenRouter key issued by the marketplace (N is the agent index)",
			Required:    false,
			Sources:     []EnvSource{SourceDotenv, SourceSecretMgr},
		},
		{
			Name:        "AGENT_N_FORGEJO_TOKEN",
			Service:     "agent",
			Description: "Per-agent Forgejo PAT scoped to the agent's namespace",
			Required:    false,
			Sources:     []EnvSource{SourceDotenv, SourceSecretMgr},
		},
		{
			Name:        "LANGFUSE_PUBLIC_KEY",
			Service:     "langfuse",
			Description: "LangFuse public key used by clients (read-only, safe to expose)",
			Required:    true,
			Sources:     []EnvSource{SourceDotenv},
		},
		{
			Name:        "LANGFUSE_SECRET_KEY",
			Service:     "langfuse",
			Description: "LangFuse secret key used by ingestion clients",
			Required:    true,
			Sources:     []EnvSource{SourceDotenv, SourceSecretMgr},
		},
		{
			Name:        "GITHUB_TOKEN",
			Service:     "platform",
			Description: "GitHub PAT used for repo mirroring and Pages deployment",
			Required:    false,
			Sources:     []EnvSource{SourceDotenv},
		},
	}
}

// EnvVarGroup organizes vars by their Service field. The slice returned
// from Group is sorted by service name for stable output.
type EnvVarGroup struct {
	Service string
	Vars    []EnvVar
}

// GroupByService returns the inventory grouped by service and sorted.
func GroupByService(envvars []EnvVar) []EnvVarGroup {
	bucket := make(map[string][]EnvVar)
	for _, v := range envvars {
		bucket[v.Service] = append(bucket[v.Service], v)
	}
	out := make([]EnvVarGroup, 0, len(bucket))
	for svc, vs := range bucket {
		sort.Slice(vs, func(i, j int) bool { return vs[i].Name < vs[j].Name })
		out = append(out, EnvVarGroup{Service: svc, Vars: vs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out
}

// EnvVarReport contains the validation result for a single variable.
type EnvVarReport struct {
	Var       EnvVar
	Present   bool
	FromEnv   bool
	Value     string // redacted if it looks like a secret
	IsDefault bool   // true when value came from the var's Default
}

// HasValue inspects the env or the supplied loader and reports present-state
// for each entry in the inventory. Values that look like secrets are
// redacted in the Value field.
//
// loader supplies values for sources other than the current process
// environment; pass nil to use only os.Getenv. The loader is consulted for
// every var in order — useful when reading from a .env file or vault.
func (e EnvVar) HasValue(env map[string]string, loader EnvLoader) EnvVarReport {
	r := EnvVarReport{Var: e}
	lookup := e.Name
	if v, ok := env[lookup]; ok && v != "" {
		r.Present = true
		r.FromEnv = true
		r.Value = redactIfSecret(e.Name, v)
		return r
	}
	if loader != nil {
		// Try each declared source in order.
		for _, src := range e.Sources {
			if v, ok := loader.Load(e.Name, src); ok && v != "" {
				r.Present = true
				r.Value = redactIfSecret(e.Name, v)
				return r
			}
		}
	}
	if e.Default != "" {
		r.Present = true
		r.IsDefault = true
		r.Value = e.Default
	}
	return r
}

// EnvLoader abstracts how non-process sources supply environment values.
type EnvLoader interface {
	// Load returns (value, true) if the variable was found in the given source.
	Load(name string, source EnvSource) (string, bool)
}

// ProcessEnvLoader reads only from the process environment. Used as the
// default when no custom loader is supplied.
type ProcessEnvLoader struct{}

func (ProcessEnvLoader) Load(name string, _ EnvSource) (string, bool) {
	v := os.Getenv(name)
	if v == "" {
		return "", false
	}
	return v, true
}

// DotEnvLoader is a tiny .env-file reader. It does NOT override process env
// unless the variable is unset — process env wins, matching conventional
// 12-factor behavior.
type DotEnvLoader struct {
	Path string
}

// Load implements EnvLoader. Returns ok=true when a value is found in the
// .env file. Empty values and malformed lines are skipped silently.
func (d DotEnvLoader) Load(name string, _ EnvSource) (string, bool) {
	if d.Path == "" {
		return "", false
	}
	f, err := os.Open(d.Path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if key != name {
			continue
		}
		val := strings.TrimSpace(line[eq+1:])
		val = strings.Trim(val, `"'`)
		if val == "" {
			return "", false
		}
		return val, true
	}
	return "", false
}

// ─────────────────────────────────────────────────────────────────────────────
// Inventory validation
// ─────────────────────────────────────────────────────────────────────────────

// InventoryReport summarizes the validation outcome for the entire inventory.
type InventoryReport struct {
	Total      int
	Present    int
	Missing    []EnvVarReport
	HasMissing bool
	// ResolvedBySource is the count of variables resolved from each source.
	ResolvedBySource map[EnvSource]int
}

// ValidateEnvVars checks every variable in the supplied inventory against
// env + loader. Required vars that aren't found become entries in Missing.
func ValidateEnvVars(envvars []EnvVar, env map[string]string, loader EnvLoader) InventoryReport {
	rpt := InventoryReport{
		Total:            len(envvars),
		ResolvedBySource: make(map[EnvSource]int),
	}
	for _, v := range envvars {
		rr := v.HasValue(env, loader)
		if rr.Present {
			rpt.Present++
			if rr.FromEnv {
				rpt.ResolvedBySource[SourceInferred]++
			} else if loader != nil {
				// We don't know which loader source provided it. HasValue
				// already tried them in declared order; record the first.
				if len(v.Sources) > 0 {
					rpt.ResolvedBySource[v.Sources[0]]++
				}
			}
		} else if v.Required {
			rpt.Missing = append(rpt.Missing, rr)
		}
	}
	rpt.HasMissing = len(rpt.Missing) > 0
	return rpt
}

// MissingRequiredVars returns only the names of required vars that are not
// present in env/loader. Useful for "preflight check" style integrations.
func MissingRequiredVars(envvars []EnvVar, env map[string]string, loader EnvLoader) []string {
	rpt := ValidateEnvVars(envvars, env, loader)
	out := make([]string, 0, len(rpt.Missing))
	for _, r := range rpt.Missing {
		out = append(out, r.Var.Name)
	}
	sort.Strings(out)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Reporting
// ─────────────────────────────────────────────────────────────────────────────

// FormatEnvVarReport renders the entire inventory in a CLI-friendly layout.
// Present vars are listed with their (redacted) value; missing required vars
// are flagged with ✗. Output is grouped by service.
//
// The output is deterministic: groups sorted by service, vars within each
// group sorted by name.
func FormatEnvVarReport(envvars []EnvVar, env map[string]string, loader EnvLoader) string {
	groups := GroupByService(envvars)
	rpt := ValidateEnvVars(envvars, env, loader)
	var b strings.Builder
	fmt.Fprintf(&b, "Helix Environment Variable Inventory\n")
	fmt.Fprintf(&b, "===================================\n\n")
	fmt.Fprintf(&b, "Total: %d    Present: %d    Missing required: %d\n\n", rpt.Total, rpt.Present, len(rpt.Missing))

	for _, g := range groups {
		fmt.Fprintf(&b, "── %s ──\n", g.Service)
		for _, v := range g.Vars {
			rr := v.HasValue(env, loader)
			marker := "✓"
			detail := rr.Value
			if detail == "" {
				detail = "(unset)"
			}
			if !rr.Present {
				if v.Required {
					marker = "✗"
					detail = "MISSING REQUIRED"
				} else {
					marker = "·"
					detail = "(optional, not set)"
				}
			}
			if rr.IsDefault {
				detail = fmt.Sprintf("%s (default)", detail)
			}
			fmt.Fprintf(&b, "  %s  %-30s  %s\n", marker, v.Name, detail)
		}
		fmt.Fprintln(&b)
	}
	if rpt.HasMissing {
		fmt.Fprintf(&b, "Missing required variables:\n")
		for _, m := range rpt.Missing {
			fmt.Fprintf(&b, "  - %s (%s)\n", m.Var.Name, m.Var.Description)
		}
	}
	return b.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// Secret redaction
// ─────────────────────────────────────────────────────────────────────────────

// redactIfSecret masks values for variables whose names suggest they hold
// secrets. We keep a conservative list to avoid masking non-secret values
// (e.g. SERVICE_PORT).
func redactIfSecret(name, value string) string {
	upper := strings.ToUpper(name)
	for _, marker := range []string{"KEY", "TOKEN", "PASS", "SECRET"} {
		if strings.Contains(upper, marker) {
			if len(value) <= 4 {
				return "***"
			}
			return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
		}
	}
	return value
}
