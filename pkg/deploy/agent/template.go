// Package agent encodes the Helix per-agent container template generator
// per specs/SPECIFICATION.md §9.5. Each agent runs in a container generated
// by H4F with a fixed set of env vars, security options, and resource limits.
//
// This package produces deterministic docker-compose service fragments that
// can be merged into a top-level compose file.
package agent

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// -----------------------------------------------------------------------------
// Tier
// -----------------------------------------------------------------------------

// Tier enumerates the supported agent tiers per spec §9.5 (AGENT_TIER env).
type Tier string

const (
	TierFlash    Tier = "flash"
	TierStandard Tier = "standard"
	TierPro      Tier = "pro"
	TierVeteran  Tier = "veteran"
)

// IsValid reports whether t is a recognized tier.
func (t Tier) IsValid() bool {
	switch t {
	case TierFlash, TierStandard, TierPro, TierVeteran:
		return true
	default:
		return false
	}
}

// AllTiers returns the supported tiers in canonical order.
func AllTiers() []Tier {
	return []Tier{TierFlash, TierStandard, TierPro, TierVeteran}
}

// -----------------------------------------------------------------------------
// Default endpoint constants (spec §9.5)
// -----------------------------------------------------------------------------

const (
	DefaultImage          = "hermes-agent:latest"
	DefaultForgejoURL     = "http://forgejo:3000"
	DefaultHivemindURL    = "http://hivemind:8003"
	DefaultChimeraURL     = "http://chimera:8001"
	DefaultTmpSize        = "512M"
	DefaultEnvKeyPrefix   = "AGENT_"
	NetworkModeGluetunFmt = "service:gluetun-%s"
	NetworkModeHost       = "host"
	WorktreesMountFmt     = "%s_worktrees:/worktrees"
	CacheMountFmt         = "%s_cache:/home/hermes/.cache"
)

// -----------------------------------------------------------------------------
// Spec
// -----------------------------------------------------------------------------

// Spec describes one per-agent container.
type Spec struct {
	// Name is the agent identifier (e.g. "agent-sandbox-7").
	// Used as container_name and HERMES_PROFILE.
	Name string
	// Tier selects the agent tier.
	Tier Tier
	// BudgetMonthlyUSD is the per-agent monthly budget in USD.
	BudgetMonthlyUSD int
	// MemLimit is the container memory limit (e.g. "8g").
	MemLimit string
	// CPUs is the CPU count for the container (e.g. "4").
	CPUs string
	// VPNRequired routes the container through gluetun when true.
	VPNRequired bool
	// OpenRouterKeyEnv is the env var name holding the OpenRouter API key.
	// Defaults to "AGENT_<N>_OPENROUTER_KEY" where <N> is the agent number.
	OpenRouterKeyEnv string
	// ForgejoTokenEnv is the env var name holding the Forgejo PAT.
	ForgejoTokenEnv string
	// LangFusePublicEnv / LangFuseSecretEnv name the LANGFUSE_* env vars.
	// Defaults to LANGFUSE_PUBLIC_KEY / LANGFUSE_SECRET_KEY.
	LangFusePublicEnv string
	LangFuseSecretEnv string
}

// Validate enforces spec §9.5 invariants.
func (s Spec) Validate() error {
	if err := validateAgentName(s.Name); err != nil {
		return err
	}
	if !s.Tier.IsValid() {
		return fmt.Errorf("agent %q: invalid tier %q (must be one of flash/standard/pro/veteran)", s.Name, s.Tier)
	}
	if s.BudgetMonthlyUSD <= 0 {
		return fmt.Errorf("agent %q: BudgetMonthlyUSD must be > 0, got %d", s.Name, s.BudgetMonthlyUSD)
	}
	if s.MemLimit == "" {
		return fmt.Errorf("agent %q: MemLimit is required (e.g. \"8g\")", s.Name)
	}
	if s.CPUs == "" {
		return fmt.Errorf("agent %q: CPUs is required (e.g. \"4\")", s.Name)
	}
	return nil
}

// agentNameRe permits lowercase letters, digits, hyphens, underscores.
// Per spec §9.5 example: "agent-sandbox-7".
var agentNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func validateAgentName(name string) error {
	if name == "" {
		return fmt.Errorf("agent Name is required")
	}
	if !agentNameRe.MatchString(name) {
		return fmt.Errorf("agent Name %q is invalid (must match %s)", name, agentNameRe.String())
	}
	return nil
}

// agentNumberRe extracts the trailing number from names like "agent-sandbox-7".
var agentNumberRe = regexp.MustCompile(`-(\d+)$`)

func agentNumber(name string) string {
	m := agentNumberRe.FindStringSubmatch(name)
	if len(m) != 2 {
		return "0"
	}
	return m[1]
}

// keyEnv returns the env var name for OpenRouter key, defaulting per spec.
func (s Spec) keyEnv() string {
	if s.OpenRouterKeyEnv != "" {
		return s.OpenRouterKeyEnv
	}
	return fmt.Sprintf("AGENT_%s_OPENROUTER_KEY", agentNumber(s.Name))
}

// tokenEnv returns the env var name for the Forgejo token.
func (s Spec) tokenEnv() string {
	if s.ForgejoTokenEnv != "" {
		return s.ForgejoTokenEnv
	}
	return fmt.Sprintf("AGENT_%s_FORGEJO_TOKEN", agentNumber(s.Name))
}

func (s Spec) publicEnv() string {
	if s.LangFusePublicEnv != "" {
		return s.LangFusePublicEnv
	}
	return "LANGFUSE_PUBLIC_KEY"
}

func (s Spec) secretEnv() string {
	if s.LangFuseSecretEnv != "" {
		return s.LangFuseSecretEnv
	}
	return "LANGFUSE_SECRET_KEY"
}

// slug converts agent name "agent-sandbox-7" → "agent_7" for volume names.
func slug(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

// -----------------------------------------------------------------------------
// ComposeService — the rendered output struct
// -----------------------------------------------------------------------------

// ComposeService is the rendered per-agent docker-compose service fragment.
// Fields are kept in canonical order matching the spec §9.5 example.
type ComposeService struct {
	Image         string
	ContainerName string
	Environment   []string
	Volumes       []string
	NetworkMode   string
	SecurityOpt   []string
	ReadOnly      bool
	Tmpfs         []string
	MemLimit      string
	CPUs          string
}

// ToYAML emits a YAML service fragment with stable key ordering suitable for
// merging into a top-level docker-compose.yaml.
func (c ComposeService) ToYAML() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  image: %s\n", c.Image)
	fmt.Fprintf(&b, "  container_name: %s\n", c.ContainerName)
	b.WriteString("  environment:\n")
	for _, e := range c.Environment {
		fmt.Fprintf(&b, "    %s\n", e)
	}
	if len(c.Volumes) > 0 {
		b.WriteString("  volumes:\n")
		for _, v := range c.Volumes {
			fmt.Fprintf(&b, "    - %s\n", v)
		}
	}
	if c.NetworkMode != "" {
		fmt.Fprintf(&b, "  network_mode: %s\n", c.NetworkMode)
	}
	if len(c.SecurityOpt) > 0 {
		b.WriteString("  security_opt:\n")
		for _, s := range c.SecurityOpt {
			fmt.Fprintf(&b, "    - %s\n", s)
		}
	}
	if c.ReadOnly {
		b.WriteString("  read_only: true\n")
	}
	if len(c.Tmpfs) > 0 {
		b.WriteString("  tmpfs:\n")
		for _, t := range c.Tmpfs {
			fmt.Fprintf(&b, "    - %s\n", t)
		}
	}
	if c.MemLimit != "" {
		fmt.Fprintf(&b, "  mem_limit: %s\n", c.MemLimit)
	}
	if c.CPUs != "" {
		fmt.Fprintf(&b, "  cpus: %s\n", c.CPUs)
	}
	return b.String()
}

// -----------------------------------------------------------------------------
// Render
// -----------------------------------------------------------------------------

// Render produces the docker-compose service fragment for the agent.
func (s Spec) Render() (ComposeService, error) {
	if err := s.Validate(); err != nil {
		return ComposeService{}, err
	}

	netMode := ""
	if s.VPNRequired {
		netMode = fmt.Sprintf(NetworkModeGluetunFmt, s.Name)
	}

	return ComposeService{
		Image:         DefaultImage,
		ContainerName: s.Name,
		Environment: []string{
			fmt.Sprintf("HERMES_PROFILE: %s", s.Name),
			fmt.Sprintf("OPENROUTER_API_KEY: ${%s}", s.keyEnv()),
			fmt.Sprintf("FORGEJO_URL: %s", DefaultForgejoURL),
			fmt.Sprintf("FORGEJO_TOKEN: ${%s}", s.tokenEnv()),
			fmt.Sprintf("HIVEMIND_URL: %s", DefaultHivemindURL),
			fmt.Sprintf("CHIMERA_URL: %s", DefaultChimeraURL),
			fmt.Sprintf("LANGFUSE_PUBLIC_KEY: ${%s}", s.publicEnv()),
			fmt.Sprintf("LANGFUSE_SECRET_KEY: ${%s}", s.secretEnv()),
			fmt.Sprintf("AGENT_UUID: %s", s.Name),
			fmt.Sprintf("AGENT_TIER: %s", s.Tier),
			fmt.Sprintf("BUDGET_MONTHLY_USD: %d", s.BudgetMonthlyUSD),
		},
		Volumes: []string{
			fmt.Sprintf(WorktreesMountFmt, slug(s.Name)),
			fmt.Sprintf(CacheMountFmt, slug(s.Name)),
		},
		NetworkMode: netMode,
		SecurityOpt: []string{"no-new-privileges:true"},
		ReadOnly:    true,
		Tmpfs:       []string{fmt.Sprintf("/tmp:size=%s", DefaultTmpSize)},
		MemLimit:    s.MemLimit,
		CPUs:        s.CPUs,
	}, nil
}

// FormatService is a convenience wrapper returning the rendered YAML for s.
func FormatService(s Spec) (string, error) {
	svc, err := s.Render()
	if err != nil {
		return "", err
	}
	// Wrap under the agent's service key.
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", s.Name)
	b.WriteString(svc.ToYAML())
	return b.String(), nil
}

// -----------------------------------------------------------------------------
// Registry
// -----------------------------------------------------------------------------

// Registry holds a collection of agent specs.
type Registry struct {
	specs map[string]Spec
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{specs: make(map[string]Spec)}
}

// Register adds a spec. Returns an error if a spec with the same Name
// already exists or the spec fails validation.
func (r *Registry) Register(s Spec) error {
	if _, exists := r.specs[s.Name]; exists {
		return fmt.Errorf("agent %q already registered", s.Name)
	}
	if err := s.Validate(); err != nil {
		return err
	}
	r.specs[s.Name] = s
	return nil
}

// MustRegister adds a spec and panics on error.
func (r *Registry) MustRegister(s Spec) {
	if err := r.Register(s); err != nil {
		panic(err)
	}
}

// Get returns the spec with the given name and whether it was found.
func (r *Registry) Get(name string) (Spec, bool) {
	s, ok := r.specs[name]
	return s, ok
}

// List returns all agent names in sorted order.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.specs))
	for name := range r.specs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// All returns a snapshot of all specs keyed by name.
func (r *Registry) All() map[string]Spec {
	out := make(map[string]Spec, len(r.specs))
	for k, v := range r.specs {
		out[k] = v
	}
	return out
}
