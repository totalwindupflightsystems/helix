package agent

import (
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// Tier
// -----------------------------------------------------------------------------

func TestTier_IsValid(t *testing.T) {
	tests := []struct {
		tier Tier
		want bool
	}{
		{TierFlash, true},
		{TierStandard, true},
		{TierPro, true},
		{TierVeteran, true},
		{Tier(""), false},
		{Tier("ultra"), false},
		{Tier("FLASH"), false},
	}
	for _, tt := range tests {
		if got := tt.tier.IsValid(); got != tt.want {
			t.Errorf("Tier(%q).IsValid() = %v, want %v", tt.tier, got, tt.want)
		}
	}
}

func TestAllTiers(t *testing.T) {
	got := AllTiers()
	if len(got) != 4 {
		t.Errorf("AllTiers() returned %d entries, want 4", len(got))
	}
	want := []Tier{TierFlash, TierStandard, TierPro, TierVeteran}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("AllTiers()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// -----------------------------------------------------------------------------
// Spec.Validate
// -----------------------------------------------------------------------------

func validSpec() Spec {
	return Spec{
		Name:             "agent-sandbox-7",
		Tier:             TierFlash,
		BudgetMonthlyUSD: 150,
		MemLimit:         "8g",
		CPUs:             "4",
		VPNRequired:      true,
	}
}

func TestSpec_Validate_Valid(t *testing.T) {
	if err := validSpec().Validate(); err != nil {
		t.Errorf("expected valid: %v", err)
	}
}

func TestSpec_Validate_MissingName(t *testing.T) {
	s := validSpec()
	s.Name = ""
	if err := s.Validate(); err == nil {
		t.Error("expected error for missing Name")
	}
}

func TestSpec_Validate_BadNameChars(t *testing.T) {
	for _, name := range []string{"Agent-Sandbox", "agent sandbox 7", "agent!", "7-agent"} {
		s := validSpec()
		s.Name = name
		if err := s.Validate(); err == nil {
			t.Errorf("expected error for invalid name %q", name)
		}
	}
}

func TestSpec_Validate_BadTier(t *testing.T) {
	s := validSpec()
	s.Tier = Tier("ultra")
	if err := s.Validate(); err == nil {
		t.Error("expected error for invalid tier")
	}
}

func TestSpec_Validate_BudgetZero(t *testing.T) {
	s := validSpec()
	s.BudgetMonthlyUSD = 0
	if err := s.Validate(); err == nil {
		t.Error("expected error for zero budget")
	}
}

func TestSpec_Validate_BudgetNegative(t *testing.T) {
	s := validSpec()
	s.BudgetMonthlyUSD = -50
	if err := s.Validate(); err == nil {
		t.Error("expected error for negative budget")
	}
}

func TestSpec_Validate_MissingMemLimit(t *testing.T) {
	s := validSpec()
	s.MemLimit = ""
	if err := s.Validate(); err == nil {
		t.Error("expected error for missing MemLimit")
	}
}

func TestSpec_Validate_MissingCPUs(t *testing.T) {
	s := validSpec()
	s.CPUs = ""
	if err := s.Validate(); err == nil {
		t.Error("expected error for missing CPUs")
	}
}

// -----------------------------------------------------------------------------
// Spec.Render
// -----------------------------------------------------------------------------

func TestSpec_Render_SpecExample(t *testing.T) {
	s := validSpec()
	cs, err := s.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if cs.Image != "hermes-agent:latest" {
		t.Errorf("Image=%q", cs.Image)
	}
	if cs.ContainerName != "agent-sandbox-7" {
		t.Errorf("ContainerName=%q", cs.ContainerName)
	}
	// env vars in order
	wants := []string{
		"HERMES_PROFILE: agent-sandbox-7",
		"OPENROUTER_API_KEY: ${AGENT_7_OPENROUTER_KEY}",
		"FORGEJO_URL: http://forgejo:3000",
		"FORGEJO_TOKEN: ${AGENT_7_FORGEJO_TOKEN}",
		"HIVEMIND_URL: http://hivemind:8003",
		"CHIMERA_URL: http://chimera:8001",
		"LANGFUSE_PUBLIC_KEY: ${LANGFUSE_PUBLIC_KEY}",
		"LANGFUSE_SECRET_KEY: ${LANGFUSE_SECRET_KEY}",
		"AGENT_UUID: agent-sandbox-7",
		"AGENT_TIER: flash",
		"BUDGET_MONTHLY_USD: 150",
	}
	if len(cs.Environment) != len(wants) {
		t.Errorf("Environment length = %d, want %d", len(cs.Environment), len(wants))
	}
	for i, w := range wants {
		if i >= len(cs.Environment) {
			continue
		}
		if cs.Environment[i] != w {
			t.Errorf("Environment[%d] = %q, want %q", i, cs.Environment[i], w)
		}
	}
	// volumes
	wantVols := []string{
		"agent_sandbox_7_worktrees:/worktrees",
		"agent_sandbox_7_cache:/home/hermes/.cache",
	}
	for i, w := range wantVols {
		if i >= len(cs.Volumes) {
			continue
		}
		if cs.Volumes[i] != w {
			t.Errorf("Volumes[%d] = %q, want %q", i, cs.Volumes[i], w)
		}
	}
	if cs.NetworkMode != "service:gluetun-agent-sandbox-7" {
		t.Errorf("NetworkMode=%q", cs.NetworkMode)
	}
	if !cs.ReadOnly {
		t.Error("ReadOnly should be true")
	}
	if cs.MemLimit != "8g" {
		t.Errorf("MemLimit=%q", cs.MemLimit)
	}
	if cs.CPUs != "4" {
		t.Errorf("CPUs=%q", cs.CPUs)
	}
}

func TestSpec_Render_NoVPN(t *testing.T) {
	s := validSpec()
	s.VPNRequired = false
	cs, err := s.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if cs.NetworkMode != "" {
		t.Errorf("NetworkMode should be empty without VPN, got %q", cs.NetworkMode)
	}
}

func TestSpec_Render_NoAgentNumber_DefaultZero(t *testing.T) {
	s := Spec{
		Name:             "agent-alpha",
		Tier:             TierPro,
		BudgetMonthlyUSD: 200,
		MemLimit:         "4g",
		CPUs:             "2",
	}
	cs, err := s.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	// No trailing number → defaults to AGENT_0_*
	if cs.Environment[1] != "OPENROUTER_API_KEY: ${AGENT_0_OPENROUTER_KEY}" {
		t.Errorf("default agent number not used: %q", cs.Environment[1])
	}
	if cs.Environment[3] != "FORGEJO_TOKEN: ${AGENT_0_FORGEJO_TOKEN}" {
		t.Errorf("default agent number not used for token: %q", cs.Environment[3])
	}
}

func TestSpec_Render_CustomEnvVarNames(t *testing.T) {
	s := validSpec()
	s.OpenRouterKeyEnv = "MY_OR_KEY"
	s.ForgejoTokenEnv = "MY_FG_TOKEN"
	s.LangFusePublicEnv = "LF_PUB"
	s.LangFuseSecretEnv = "LF_SEC"
	cs, err := s.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if cs.Environment[1] != "OPENROUTER_API_KEY: ${MY_OR_KEY}" {
		t.Errorf("custom OpenRouter env not honored: %q", cs.Environment[1])
	}
	if cs.Environment[3] != "FORGEJO_TOKEN: ${MY_FG_TOKEN}" {
		t.Errorf("custom Forgejo env not honored: %q", cs.Environment[3])
	}
	if cs.Environment[6] != "LANGFUSE_PUBLIC_KEY: ${LF_PUB}" {
		t.Errorf("custom LangFuse public not honored: %q", cs.Environment[6])
	}
	if cs.Environment[7] != "LANGFUSE_SECRET_KEY: ${LF_SEC}" {
		t.Errorf("custom LangFuse secret not honored: %q", cs.Environment[7])
	}
}

func TestSpec_Render_AllTiers(t *testing.T) {
	for _, tier := range AllTiers() {
		s := validSpec()
		s.Tier = tier
		cs, err := s.Render()
		if err != nil {
			t.Errorf("tier %q: %v", tier, err)
			continue
		}
		// AGENT_TIER env should equal the tier string
		idx := -1
		for i, e := range cs.Environment {
			if strings.HasPrefix(e, "AGENT_TIER: ") {
				idx = i
				break
			}
		}
		if idx == -1 {
			t.Errorf("tier %q: AGENT_TIER env missing", tier)
			continue
		}
		if cs.Environment[idx] != "AGENT_TIER: "+string(tier) {
			t.Errorf("tier %q: env = %q", tier, cs.Environment[idx])
		}
	}
}

func TestSpec_Render_SecurityOpt(t *testing.T) {
	s := validSpec()
	cs, err := s.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if len(cs.SecurityOpt) != 1 || cs.SecurityOpt[0] != "no-new-privileges:true" {
		t.Errorf("SecurityOpt=%v", cs.SecurityOpt)
	}
}

func TestSpec_Render_Tmpfs(t *testing.T) {
	s := validSpec()
	cs, err := s.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if len(cs.Tmpfs) != 1 || cs.Tmpfs[0] != "/tmp:size=512M" {
		t.Errorf("Tmpfs=%v", cs.Tmpfs)
	}
}

// -----------------------------------------------------------------------------
// ComposeService.ToYAML
// -----------------------------------------------------------------------------

func TestComposeService_ToYAML_Structure(t *testing.T) {
	cs := ComposeService{
		Image:         "hermes-agent:latest",
		ContainerName: "agent-x",
		Environment:   []string{"HERMES_PROFILE: agent-x"},
		Volumes:       []string{"agent_x_worktrees:/worktrees"},
		NetworkMode:   "service:gluetun-agent-x",
		SecurityOpt:   []string{"no-new-privileges:true"},
		ReadOnly:      true,
		Tmpfs:         []string{"/tmp:size=512M"},
		MemLimit:      "8g",
		CPUs:          "4",
	}
	got := cs.ToYAML()
	wants := []string{
		"  image: hermes-agent:latest",
		"  container_name: agent-x",
		"  environment:",
		"    HERMES_PROFILE: agent-x",
		"  volumes:",
		"    - agent_x_worktrees:/worktrees",
		"  network_mode: service:gluetun-agent-x",
		"  security_opt:",
		"    - no-new-privileges:true",
		"  read_only: true",
		"  tmpfs:",
		"    - /tmp:size=512M",
		"  mem_limit: 8g",
		"  cpus: 4",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in YAML\n%s", w, got)
		}
	}
}

func TestComposeService_ToYAML_OmitsEmpty(t *testing.T) {
	cs := ComposeService{
		Image:         "hermes-agent:latest",
		ContainerName: "agent-x",
		Environment:   []string{"HERMES_PROFILE: agent-x"},
		// no volumes, network_mode, security_opt, tmpfs, mem_limit, cpus
	}
	got := cs.ToYAML()
	notWants := []string{"volumes:", "network_mode:", "security_opt:", "tmpfs:", "mem_limit:", "cpus:", "read_only:"}
	for _, nw := range notWants {
		if strings.Contains(got, nw) {
			t.Errorf("unexpected %q in YAML\n%s", nw, got)
		}
	}
}

func TestComposeService_ToYAML_NotReadOnly(t *testing.T) {
	cs := ComposeService{
		Image:         "hermes-agent:latest",
		ContainerName: "agent-x",
		Environment:   []string{"X: y"},
		ReadOnly:      false,
	}
	got := cs.ToYAML()
	if strings.Contains(got, "read_only") {
		t.Errorf("read_only should be omitted when false: %s", got)
	}
}

// -----------------------------------------------------------------------------
// FormatService
// -----------------------------------------------------------------------------

func TestFormatService(t *testing.T) {
	got, err := FormatService(validSpec())
	if err != nil {
		t.Fatalf("FormatService: %v", err)
	}
	if !strings.HasPrefix(got, "agent-sandbox-7:\n") {
		t.Errorf("missing service key header: %s", got[:50])
	}
	if !strings.Contains(got, "  image: hermes-agent:latest") {
		t.Errorf("missing image: %s", got)
	}
}

func TestFormatService_InvalidSpec(t *testing.T) {
	s := validSpec()
	s.Name = ""
	if _, err := FormatService(s); err == nil {
		t.Error("expected error for invalid spec")
	}
}

// -----------------------------------------------------------------------------
// Registry
// -----------------------------------------------------------------------------

func TestRegistry_Register_Get_List_All(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(validSpec())
	s2 := validSpec()
	s2.Name = "agent-sandbox-8"
	r.MustRegister(s2)

	if got := r.List(); len(got) != 2 {
		t.Errorf("List() length = %d, want 2", len(got))
	}
	// sorted
	want := []string{"agent-sandbox-7", "agent-sandbox-8"}
	for i, w := range want {
		if r.List()[i] != w {
			t.Errorf("List()[%d] = %q, want %q", i, r.List()[i], w)
		}
	}

	got, ok := r.Get("agent-sandbox-7")
	if !ok {
		t.Fatal("Get(agent-sandbox-7) not found")
	}
	if got.Tier != TierFlash {
		t.Errorf("got tier %q", got.Tier)
	}
	if _, ok := r.Get("nonexistent"); ok {
		t.Error("Get should miss on nonexistent")
	}

	all := r.All()
	if len(all) != 2 {
		t.Errorf("All() length = %d, want 2", len(all))
	}
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(validSpec())
	err := r.Register(validSpec())
	if err == nil {
		t.Error("expected duplicate-registration error")
	}
}

func TestRegistry_Register_InvalidSpec(t *testing.T) {
	r := NewRegistry()
	s := validSpec()
	s.Name = "Bad Name"
	err := r.Register(s)
	if err == nil {
		t.Error("expected validation error for invalid name")
	}
}

// -----------------------------------------------------------------------------
// Spec verbatim — render and assert spec example phrases
// -----------------------------------------------------------------------------

func TestSpecVerbatim_AllKeyFragments(t *testing.T) {
	got, err := FormatService(validSpec())
	if err != nil {
		t.Fatalf("FormatService: %v", err)
	}
	// Spec §9.5 verbatim phrases
	mustContain := []string{
		"image: hermes-agent:latest",
		"container_name: agent-sandbox-7",
		"HERMES_PROFILE: agent-sandbox-7",
		"OPENROUTER_API_KEY: ${AGENT_7_OPENROUTER_KEY}",
		"FORGEJO_URL: http://forgejo:3000",
		"HIVEMIND_URL: http://hivemind:8003",
		"CHIMERA_URL: http://chimera:8001",
		"LANGFUSE_PUBLIC_KEY: ${LANGFUSE_PUBLIC_KEY}",
		"LANGFUSE_SECRET_KEY: ${LANGFUSE_SECRET_KEY}",
		"AGENT_UUID: agent-sandbox-7",
		"AGENT_TIER: flash",
		"BUDGET_MONTHLY_USD: 150",
		"agent_sandbox_7_worktrees:/worktrees",
		"agent_sandbox_7_cache:/home/hermes/.cache",
		"network_mode: service:gluetun-agent-sandbox-7",
		"- no-new-privileges:true",
		"read_only: true",
		"- /tmp:size=512M",
		"mem_limit: 8g",
		"cpus: 4",
	}
	for _, frag := range mustContain {
		if !strings.Contains(got, frag) {
			t.Errorf("spec phrase missing: %s", frag)
		}
	}
}
