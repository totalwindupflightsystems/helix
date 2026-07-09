package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

func TestInferChangeCategory(t *testing.T) {
	cases := []struct {
		files []string
		want  ChangeCategory
	}{
		{[]string{"pkg/identity/oauth.go"}, CategoryContract},
		{[]string{"pkg/retry/backoff.go"}, CategoryResilience},
		{[]string{"README.md", "docs/guide.md"}, CategoryCosmetic},
		{[]string{"pkg/dispatcher/loop.go"}, CategoryBehavioral},
	}
	for _, tc := range cases {
		got := InferChangeCategory(tc.files)
		if got != tc.want {
			t.Errorf("InferChangeCategory(%v) = %s, want %s", tc.files, got, tc.want)
		}
	}
}

func TestComputeRiskScore_BaseAndTier(t *testing.T) {
	// contract(40) × provisional(2.0) = 80
	r := ComputeRiskScore(CategoryContract, trust.TierProvisional, nil)
	if r.Score != 80 {
		t.Errorf("score = %d, want 80", r.Score)
	}
	if r.Level != "critical" {
		t.Errorf("level = %s, want critical", r.Level)
	}

	// cosmetic(5) × veteran(0.5) = 2 → low
	r = ComputeRiskScore(CategoryCosmetic, trust.TierVeteran, nil)
	if r.Score != 2 {
		t.Errorf("cosmetic/veteran score = %d, want 2", r.Score)
	}
	if r.Level != "low" {
		t.Errorf("level = %s, want low", r.Level)
	}
}

func TestComputeRiskScore_IncidentBoost(t *testing.T) {
	incs := []RelatedIncident{
		{ID: "inc-1", Severity: "high", Description: "auth session bug", Similarity: 1.0},
		{ID: "inc-2", Severity: "low", Description: "typo", Similarity: 0.5},
	}
	// behavioral(25)×trusted(1.0)=25 + high15 + low*0.5≈1 → ~41
	r := ComputeRiskScore(CategoryBehavioral, trust.TierTrusted, incs)
	if r.IncidentBoost < 15 {
		t.Errorf("incident boost = %d, want ≥15", r.IncidentBoost)
	}
	if r.Score < 40 || r.Score > 50 {
		t.Errorf("score = %d, want ~40-50", r.Score)
	}
	if len(r.RelatedIncidents) != 2 {
		t.Errorf("related = %d", len(r.RelatedIncidents))
	}
}

func TestComputeRiskScore_IncidentCap(t *testing.T) {
	var incs []RelatedIncident
	for i := 0; i < 10; i++ {
		incs = append(incs, RelatedIncident{ID: "x", Severity: "critical"})
	}
	r := ComputeRiskScore(CategoryCosmetic, trust.TierVeteran, incs)
	if r.IncidentBoost != 40 {
		t.Errorf("boost = %d, want capped 40", r.IncidentBoost)
	}
	if r.Score > 100 {
		t.Errorf("score = %d > 100", r.Score)
	}
}

func TestAssessArchitectureFit_NoDir(t *testing.T) {
	fit := AssessArchitectureFit("", []string{"pkg/trust/x.go"}, nil)
	if fit.Status != "unknown" {
		t.Errorf("status = %s", fit.Status)
	}
}

func TestAssessArchitectureFit_MatchAndConflict(t *testing.T) {
	dir := t.TempDir()
	adr := filepath.Join(dir, "ADR-001-trust-tiers.md")
	body := "# Trust Tiers\n\nStatus: accepted\n\nWe use pkg/trust for graduated tiers.\n"
	if err := os.WriteFile(adr, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	fit := AssessArchitectureFit(dir, []string{"pkg/trust/ledger.go"}, nil)
	if fit.Status != "aligned" {
		t.Errorf("status = %s, want aligned; summary=%s", fit.Status, fit.Summary)
	}
	if len(fit.MatchedADRs) != 1 {
		t.Fatalf("matched = %d", len(fit.MatchedADRs))
	}
	if fit.MatchedADRs[0].Title != "Trust Tiers" {
		t.Errorf("title = %q", fit.MatchedADRs[0].Title)
	}

	// Superseded ADR → conflict
	adr2 := filepath.Join(dir, "ADR-002-old.md")
	if err := os.WriteFile(adr2, []byte("# Old\n\nStatus: superseded\n\ntrust ledger v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fit = AssessArchitectureFit(dir, []string{"pkg/trust/ledger.go"}, nil)
	if fit.Status != "conflict" {
		t.Errorf("status = %s, want conflict", fit.Status)
	}
}

func TestBuildDashboard_EndToEnd(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/demo\n\ngo 1.22\n")
	write("pkg/auth/session.go", "package auth\n\nfunc Session() {}\n")
	write("pkg/api/handler.go", "package api\n\nimport \"example.com/demo/pkg/auth\"\n\nfunc H() { auth.Session() }\n")
	write("docs/adr/ADR-010-auth.md", "# Auth\n\nStatus: accepted\n\nUses pkg/auth session model.\n")

	d, err := BuildDashboard(DashboardInput{
		PR:       "42",
		AgentID:  "agent-alpha",
		Category: CategoryContract,
		ChangedFiles: []string{
			"pkg/auth/session.go",
		},
		RepoRoot:   root,
		ADRDir:     filepath.Join(root, "docs/adr"),
		TrustTier:  trust.TierObserved,
		RelatedIncidents: []RelatedIncident{
			{ID: "inc-9", Severity: "medium", Description: "session refresh race", Similarity: 0.8},
		},
		TeamMap: map[string]string{"pkg/auth": "security"},
		Now:     time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildDashboard: %v", err)
	}
	if d.PR != "42" {
		t.Errorf("PR = %s", d.PR)
	}
	if d.Risk.Score < 50 {
		t.Errorf("expected elevated risk for contract×observed + incident, got %d", d.Risk.Score)
	}
	if d.BlastRadius == nil || len(d.BlastRadius.Packages) == 0 {
		t.Fatal("missing blast radius packages")
	}
	// api should be a direct dependent of auth
	foundAPI := false
	for _, p := range d.BlastRadius.Packages {
		if strings.HasSuffix(p.ImportPath, "/pkg/api") && p.Role == "direct" {
			foundAPI = true
		}
	}
	if !foundAPI {
		t.Errorf("expected pkg/api as direct dependent; packages=%+v", d.BlastRadius.Packages)
	}
	if d.Architecture.Status != "aligned" {
		t.Errorf("architecture = %s (%s)", d.Architecture.Status, d.Architecture.Summary)
	}
	if d.Trust == nil || d.Trust.Tier != trust.TierObserved {
		t.Errorf("trust = %+v", d.Trust)
	}
	if !strings.Contains(d.Summary, "risk=") {
		t.Errorf("summary = %q", d.Summary)
	}

	// Format + JSON smoke
	text := FormatDashboard(d)
	if !strings.Contains(text, "Risk Assessment") || !strings.Contains(text, "Blast Radius") {
		t.Errorf("format missing sections:\n%s", text)
	}
	raw, err := DashboardJSON(d)
	if err != nil {
		t.Fatal(err)
	}
	var round ChangeDashboard
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatal(err)
	}
	if round.Risk.Score != d.Risk.Score {
		t.Errorf("json round-trip score %d != %d", round.Risk.Score, d.Risk.Score)
	}
}

func TestBuildDashboard_InferCategory(t *testing.T) {
	d, err := BuildDashboard(DashboardInput{
		PR:           "7",
		ChangedFiles: []string{"pkg/identity/keys.go"},
		TrustTier:   trust.TierTrusted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Risk.Category != CategoryContract {
		t.Errorf("category = %s, want contract", d.Risk.Category)
	}
}
