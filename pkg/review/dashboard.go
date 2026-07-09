package review

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// =============================================================================
// Change management dashboard — human review interface
// Spec: specs/plans/phase-5-6-review.md §6.1
//
// Surfaces for humans (not agents):
//   1. Blast radius map — packages / services / teams affected
//   2. Risk score — category + trust tier + incident correlation
//   3. Architectural fit — ADR lineage comparison
//   4. Trust context — agent track record from the ledger
// =============================================================================

// RelatedIncident is a past incident correlated with the change under review.
// Callers populate this from pkg/incident (or any store) to avoid forcing a
// hard dependency edge from review → incident in every call path.
type RelatedIncident struct {
	ID          string  `json:"id"`
	Severity    string  `json:"severity"`
	Description string  `json:"description"`
	Similarity  float64 `json:"similarity,omitempty"` // 0.0–1.0 when known
	AgentID     string  `json:"agent_id,omitempty"`
}

// ADRRef is a matched architecture decision record.
type ADRRef struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Title   string `json:"title,omitempty"`
	Status  string `json:"status,omitempty"`  // proposed|accepted|deprecated|superseded|unknown
	Matched string `json:"matched,omitempty"` // what term matched
}

// DashboardInput is everything needed to build a change management dashboard.
type DashboardInput struct {
	// PR is a PR number or URL (display only).
	PR string `json:"pr"`
	// AgentID is the authoring agent (optional).
	AgentID string `json:"agent_id,omitempty"`
	// Category drives the base risk weight.
	Category ChangeCategory `json:"category"`
	// ChangedFiles are paths relative to the repo root.
	ChangedFiles []string `json:"changed_files"`
	// RepoRoot enables import-graph blast radius (optional).
	RepoRoot string `json:"repo_root,omitempty"`
	// LedgerPath enables trust snapshot lookup (optional).
	LedgerPath string `json:"ledger_path,omitempty"`
	// ADRDir is a directory of ADR markdown files (optional).
	ADRDir string `json:"adr_dir,omitempty"`
	// RelatedIncidents feed the risk correlation component.
	RelatedIncidents []RelatedIncident `json:"related_incidents,omitempty"`
	// TeamMap maps package prefixes to owning teams.
	TeamMap map[string]string `json:"team_map,omitempty"`
	// TrustTier overrides ledger-derived tier when set (for dry-run / tests).
	TrustTier trust.TrustTier `json:"trust_tier,omitempty"`
	// Now overrides wall clock (tests).
	Now time.Time `json:"-"`
}

// RiskAssessment is the composite 0–100 risk score with a transparent breakdown.
type RiskAssessment struct {
	Score            int               `json:"score"` // 0–100
	Level            string            `json:"level"` // low|medium|high|critical
	CategoryWeight   int               `json:"category_weight"`
	TierMultiplier   float64           `json:"tier_multiplier"`
	IncidentBoost    int               `json:"incident_boost"`
	Category         ChangeCategory    `json:"category"`
	Tier             trust.TrustTier   `json:"tier"`
	RelatedIncidents []RelatedIncident `json:"related_incidents,omitempty"`
	Rationale        []string          `json:"rationale"`
}

// ArchitectureFit summarizes ADR lineage alignment for the change.
type ArchitectureFit struct {
	Status      string   `json:"status"` // aligned|partial|unknown|conflict
	MatchedADRs []ADRRef `json:"matched_adrs,omitempty"`
	Gaps        []string `json:"gaps,omitempty"`
	Summary     string   `json:"summary"`
}

// TrustContext is the agent track record surface for human reviewers.
type TrustContext struct {
	AgentID     string          `json:"agent_id"`
	Tier        trust.TrustTier `json:"tier"`
	Score       float64         `json:"score"`
	TotalEvents int             `json:"total_events"`
	Trend       string          `json:"trend,omitempty"`
	RecentCount int             `json:"recent_event_count"`
	LastActive  time.Time       `json:"last_active,omitempty"`
	Source      string          `json:"source"` // ledger|override|unknown
	Notes       []string        `json:"notes,omitempty"`
}

// ChangeDashboard is the full human change management view for one PR.
type ChangeDashboard struct {
	PR           string          `json:"pr"`
	GeneratedAt  time.Time       `json:"generated_at"`
	AgentID      string          `json:"agent_id,omitempty"`
	BlastRadius  *BlastRadiusMap `json:"blast_radius"`
	Risk         RiskAssessment  `json:"risk"`
	Architecture ArchitectureFit `json:"architecture"`
	Trust        *TrustContext   `json:"trust,omitempty"`
	Summary      string          `json:"summary"`
}

// BuildDashboard computes the full change management dashboard.
func BuildDashboard(in DashboardInput) (*ChangeDashboard, error) {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if in.Category == "" {
		in.Category = InferChangeCategory(in.ChangedFiles)
	}

	blast, err := BuildBlastRadius(in.ChangedFiles, BlastRadiusOptions{
		RepoRoot: in.RepoRoot,
		TeamMap:  in.TeamMap,
	})
	if err != nil {
		return nil, fmt.Errorf("blast radius: %w", err)
	}

	trustCtx := resolveTrustContext(in)
	tier := trust.TierProvisional
	if trustCtx != nil && trustCtx.Tier != "" {
		tier = trustCtx.Tier
	} else if in.TrustTier != "" {
		tier = in.TrustTier
	}

	risk := ComputeRiskScore(in.Category, tier, in.RelatedIncidents)
	arch := AssessArchitectureFit(in.ADRDir, in.ChangedFiles, blast)

	d := &ChangeDashboard{
		PR:           in.PR,
		GeneratedAt:  now,
		AgentID:      in.AgentID,
		BlastRadius:  blast,
		Risk:         risk,
		Architecture: arch,
		Trust:        trustCtx,
	}
	d.Summary = summarizeDashboard(d)
	return d, nil
}

// InferChangeCategory guesses a ChangeCategory from file paths.
// Auth/crypto/api signatures → contract; deploy/retry → resilience;
// docs/format → cosmetic; else behavioral.
func InferChangeCategory(files []string) ChangeCategory {
	if len(files) == 0 {
		return CategoryBehavioral
	}
	contractHints := []string{"/auth", "auth/", "crypto", "oauth", "openid", "schema", "openapi", "proto", "contract", "identity", "secret"}
	resilienceHints := []string{"retry", "circuit", "timeout", "backoff", "health", "degradation", "recovery"}
	cosmeticHints := []string{".md", "docs/", "comment", "readme", "license", ".txt"}

	hasContract, hasResilience, allCosmetic := false, false, true
	for _, f := range files {
		low := strings.ToLower(filepath.ToSlash(f))
		cosmetic := false
		for _, h := range cosmeticHints {
			if strings.Contains(low, h) {
				cosmetic = true
				break
			}
		}
		if !cosmetic {
			allCosmetic = false
		}
		for _, h := range contractHints {
			if strings.Contains(low, h) {
				hasContract = true
			}
		}
		for _, h := range resilienceHints {
			if strings.Contains(low, h) {
				hasResilience = true
			}
		}
	}
	if hasContract {
		return CategoryContract
	}
	if hasResilience {
		return CategoryResilience
	}
	if allCosmetic {
		return CategoryCosmetic
	}
	return CategoryBehavioral
}

// CategoryRiskWeight returns the base risk points for a change category
// (spec phase-5-6-review.md §6.1: contract=40, behavioral=25, resilience=15, cosmetic=5).
func CategoryRiskWeight(cat ChangeCategory) int {
	switch cat {
	case CategoryContract:
		return 40
	case CategoryBehavioral:
		return 25
	case CategoryResilience:
		return 15
	case CategoryCosmetic:
		return 5
	default:
		return 25
	}
}

// TierRiskMultiplier returns the agent trust multiplier
// (provisional×2.0, observed×1.5, trusted×1.0, veteran×0.5).
func TierRiskMultiplier(tier trust.TrustTier) float64 {
	switch tier {
	case trust.TierProvisional:
		return 2.0
	case trust.TierObserved:
		return 1.5
	case trust.TierTrusted:
		return 1.0
	case trust.TierVeteran:
		return 0.5
	default:
		return 2.0 // unknown → treat as provisional
	}
}

// ComputeRiskScore builds the composite 0–100 risk assessment.
func ComputeRiskScore(cat ChangeCategory, tier trust.TrustTier, incidents []RelatedIncident) RiskAssessment {
	base := CategoryRiskWeight(cat)
	mult := TierRiskMultiplier(tier)
	raw := float64(base) * mult

	boost := 0
	var rationale []string
	rationale = append(rationale, fmt.Sprintf("category %s base weight %d", cat, base))
	rationale = append(rationale, fmt.Sprintf("trust tier %s multiplier ×%.1f", tier, mult))

	for _, inc := range incidents {
		points := severityPoints(inc.Severity)
		if inc.Similarity > 0 {
			points = int(float64(points) * clamp01(inc.Similarity))
		}
		boost += points
		desc := inc.ID
		if inc.Description != "" {
			desc = inc.ID + ": " + truncate(inc.Description, 60)
		}
		rationale = append(rationale, fmt.Sprintf("incident +%d (%s)", points, desc))
	}
	// Cap incident boost so a flood of low-sev incidents can't dominate.
	if boost > 40 {
		rationale = append(rationale, fmt.Sprintf("incident boost capped from %d to 40", boost))
		boost = 40
	}

	score := int(raw) + boost
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return RiskAssessment{
		Score:            score,
		Level:            riskLevel(score),
		CategoryWeight:   base,
		TierMultiplier:   mult,
		IncidentBoost:    boost,
		Category:         cat,
		Tier:             tier,
		RelatedIncidents: incidents,
		Rationale:        rationale,
	}
}

func severityPoints(sev string) int {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "critical":
		return 25
	case "high":
		return 15
	case "medium":
		return 8
	case "low":
		return 3
	default:
		return 5
	}
}

func riskLevel(score int) string {
	switch {
	case score >= 75:
		return "critical"
	case score >= 50:
		return "high"
	case score >= 25:
		return "medium"
	default:
		return "low"
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// AssessArchitectureFit scans ADRDir for markdown ADRs that mention packages
// touched by the change. Missing ADRDir → status "unknown".
func AssessArchitectureFit(adrDir string, changedFiles []string, blast *BlastRadiusMap) ArchitectureFit {
	if adrDir == "" {
		return ArchitectureFit{
			Status:  "unknown",
			Summary: "No ADR directory provided — architectural lineage not checked",
			Gaps:    []string{"pass --adr-dir to enable ADR lineage comparison"},
		}
	}
	info, err := os.Stat(adrDir)
	if err != nil || !info.IsDir() {
		return ArchitectureFit{
			Status:  "unknown",
			Summary: fmt.Sprintf("ADR directory %q not readable", adrDir),
			Gaps:    []string{"ADR directory missing or unreadable"},
		}
	}

	terms := collectSearchTerms(changedFiles, blast)
	var matched []ADRRef
	_ = filepath.WalkDir(adrDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		low := strings.ToLower(d.Name())
		if !strings.HasSuffix(low, ".md") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		text := string(body)
		textLow := strings.ToLower(text)
		for _, term := range terms {
			if term == "" || len(term) < 3 {
				continue
			}
			if strings.Contains(textLow, strings.ToLower(term)) {
				matched = append(matched, ADRRef{
					ID:      deriveADRID(d.Name()),
					Path:    path,
					Title:   firstHeading(text),
					Status:  extractADRStatus(text),
					Matched: term,
				})
				break
			}
		}
		return nil
	})

	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })

	if len(matched) == 0 {
		return ArchitectureFit{
			Status:  "unknown",
			Summary: "No ADRs reference the packages/files in this change",
			Gaps:    []string{"consider authoring an ADR if this change introduces architectural decisions"},
		}
	}

	// If any matched ADR is deprecated/superseded → conflict signal.
	status := "aligned"
	var gaps []string
	for _, a := range matched {
		switch strings.ToLower(a.Status) {
		case "deprecated", "superseded":
			status = "conflict"
			gaps = append(gaps, fmt.Sprintf("ADR %s is %s", a.ID, a.Status))
		case "proposed":
			if status == "aligned" {
				status = "partial"
			}
			gaps = append(gaps, fmt.Sprintf("ADR %s is still proposed", a.ID))
		}
	}
	return ArchitectureFit{
		Status:      status,
		MatchedADRs: matched,
		Gaps:        uniqueSorted(gaps),
		Summary:     fmt.Sprintf("%d ADR(s) reference this change surface; fit=%s", len(matched), status),
	}
}

func collectSearchTerms(files []string, blast *BlastRadiusMap) []string {
	set := map[string]bool{}
	for _, f := range files {
		f = filepath.ToSlash(f)
		set[f] = true
		base := filepath.Base(f)
		if base != "" && base != "." {
			set[base] = true
		}
		// Package-ish segments: pkg/trust → trust
		parts := strings.Split(f, "/")
		for _, p := range parts {
			if p == "" || p == "pkg" || p == "cmd" || p == "internal" || strings.HasSuffix(p, ".go") {
				continue
			}
			set[p] = true
		}
	}
	if blast != nil {
		for _, p := range blast.Packages {
			if p.Depth > 0 {
				continue // only seed from changed packages
			}
			if p.Dir != "" {
				set[p.Dir] = true
				set[filepath.Base(p.Dir)] = true
			}
			if i := strings.LastIndex(p.ImportPath, "/"); i >= 0 {
				set[p.ImportPath[i+1:]] = true
			}
		}
	}
	return sortedKeysBool(set)
}

func deriveADRID(name string) string {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	return name
}

func firstHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	return ""
}

func extractADRStatus(body string) string {
	// Look for common frontmatter / Status: lines.
	low := strings.ToLower(body)
	for _, st := range []string{"accepted", "proposed", "deprecated", "superseded"} {
		if strings.Contains(low, "status: "+st) || strings.Contains(low, "status: **"+st) {
			return st
		}
	}
	return "unknown"
}

func resolveTrustContext(in DashboardInput) *TrustContext {
	if in.AgentID == "" && in.TrustTier == "" && in.LedgerPath == "" {
		return nil
	}
	ctx := &TrustContext{
		AgentID: in.AgentID,
		Source:  "unknown",
		Tier:    in.TrustTier,
	}
	if in.TrustTier != "" {
		ctx.Source = "override"
	}
	if in.LedgerPath == "" || in.AgentID == "" {
		if ctx.Tier == "" {
			ctx.Tier = trust.TierProvisional
			ctx.Notes = append(ctx.Notes, "no ledger/agent — defaulting to provisional")
		}
		return ctx
	}
	snap, err := trust.GetSnapshot(in.LedgerPath, in.AgentID)
	if err != nil {
		ctx.Notes = append(ctx.Notes, fmt.Sprintf("ledger read failed: %v", err))
		if ctx.Tier == "" {
			ctx.Tier = trust.TierProvisional
		}
		return ctx
	}
	ctx.Source = "ledger"
	ctx.Tier = snap.Tier
	ctx.Score = float64(snap.Score)
	ctx.TotalEvents = snap.TotalEvents
	ctx.Trend = snap.ScoreTrend.Direction
	ctx.RecentCount = len(snap.RecentEvents)
	ctx.LastActive = snap.LastActive
	if in.TrustTier != "" && in.TrustTier != snap.Tier {
		ctx.Notes = append(ctx.Notes, fmt.Sprintf("override tier %s differs from ledger tier %s", in.TrustTier, snap.Tier))
		ctx.Tier = in.TrustTier
		ctx.Source = "override"
	}
	return ctx
}

func summarizeDashboard(d *ChangeDashboard) string {
	parts := []string{
		fmt.Sprintf("PR %s risk=%d (%s)", emptyDash(d.PR), d.Risk.Score, d.Risk.Level),
	}
	if d.BlastRadius != nil {
		parts = append(parts, fmt.Sprintf("blast packages=%d direct=%d services=%d",
			len(d.BlastRadius.Packages),
			len(d.BlastRadius.DirectDependents),
			len(d.BlastRadius.Services)))
	}
	parts = append(parts, "architecture="+d.Architecture.Status)
	if d.Trust != nil {
		parts = append(parts, fmt.Sprintf("agent=%s tier=%s", emptyDash(d.Trust.AgentID), d.Trust.Tier))
	}
	return strings.Join(parts, " | ")
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// FormatDashboard renders a human-readable terminal report.
func FormatDashboard(d *ChangeDashboard) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Helix Change Management Dashboard\n")
	fmt.Fprintf(&b, "=================================\n")
	fmt.Fprintf(&b, "PR:          %s\n", emptyDash(d.PR))
	fmt.Fprintf(&b, "Generated:   %s\n", d.GeneratedAt.Format(time.RFC3339))
	if d.AgentID != "" {
		fmt.Fprintf(&b, "Agent:       %s\n", d.AgentID)
	}
	fmt.Fprintf(&b, "Summary:     %s\n\n", d.Summary)

	// Risk
	fmt.Fprintf(&b, "1. Risk Assessment\n")
	fmt.Fprintf(&b, "------------------\n")
	fmt.Fprintf(&b, "  Score:      %d / 100 (%s)\n", d.Risk.Score, d.Risk.Level)
	fmt.Fprintf(&b, "  Category:   %s (weight %d)\n", d.Risk.Category, d.Risk.CategoryWeight)
	fmt.Fprintf(&b, "  Tier:       %s (×%.1f)\n", d.Risk.Tier, d.Risk.TierMultiplier)
	fmt.Fprintf(&b, "  Incidents:  +%d from %d related\n", d.Risk.IncidentBoost, len(d.Risk.RelatedIncidents))
	for _, r := range d.Risk.Rationale {
		fmt.Fprintf(&b, "    • %s\n", r)
	}
	if len(d.Risk.RelatedIncidents) > 0 {
		fmt.Fprintf(&b, "  Related incidents:\n")
		for _, inc := range d.Risk.RelatedIncidents {
			fmt.Fprintf(&b, "    - [%s] %s — %s\n", inc.Severity, inc.ID, truncate(inc.Description, 80))
		}
	}
	fmt.Fprintln(&b)

	// Blast radius
	fmt.Fprintf(&b, "2. Blast Radius\n")
	fmt.Fprintf(&b, "---------------\n")
	if d.BlastRadius == nil {
		fmt.Fprintf(&b, "  (unavailable)\n\n")
	} else {
		fmt.Fprintf(&b, "  Changed files: %d\n", len(d.BlastRadius.ChangedFiles))
		for _, f := range d.BlastRadius.ChangedFiles {
			fmt.Fprintf(&b, "    • %s\n", f)
		}
		fmt.Fprintf(&b, "  Packages (%d):\n", len(d.BlastRadius.Packages))
		for _, p := range d.BlastRadius.Packages {
			fmt.Fprintf(&b, "    [%s d=%d] %s\n", p.Role, p.Depth, p.ImportPath)
		}
		if len(d.BlastRadius.DirectDependents) > 0 {
			fmt.Fprintf(&b, "  Direct dependents: %s\n", strings.Join(d.BlastRadius.DirectDependents, ", "))
		}
		if len(d.BlastRadius.TransitiveDependents) > 0 {
			fmt.Fprintf(&b, "  Transitive:        %s\n", strings.Join(d.BlastRadius.TransitiveDependents, ", "))
		}
		if len(d.BlastRadius.Services) > 0 {
			fmt.Fprintf(&b, "  Services:          %s\n", strings.Join(d.BlastRadius.Services, ", "))
		}
		if len(d.BlastRadius.Teams) > 0 {
			fmt.Fprintf(&b, "  Teams:             %s\n", strings.Join(d.BlastRadius.Teams, ", "))
		}
		fmt.Fprintf(&b, "  Max depth:         %d | files scanned: %d\n\n", d.BlastRadius.MaxDepth, d.BlastRadius.FilesScanned)
	}

	// Architecture
	fmt.Fprintf(&b, "3. Architectural Fit\n")
	fmt.Fprintf(&b, "--------------------\n")
	fmt.Fprintf(&b, "  Status:  %s\n", d.Architecture.Status)
	fmt.Fprintf(&b, "  Summary: %s\n", d.Architecture.Summary)
	for _, a := range d.Architecture.MatchedADRs {
		fmt.Fprintf(&b, "    • %s (%s) — %s [matched %q]\n", a.ID, a.Status, a.Title, a.Matched)
	}
	for _, g := range d.Architecture.Gaps {
		fmt.Fprintf(&b, "    gap: %s\n", g)
	}
	fmt.Fprintln(&b)

	// Trust
	fmt.Fprintf(&b, "4. Trust Context\n")
	fmt.Fprintf(&b, "----------------\n")
	if d.Trust == nil {
		fmt.Fprintf(&b, "  (no agent / ledger provided)\n")
	} else {
		fmt.Fprintf(&b, "  Agent:   %s\n", emptyDash(d.Trust.AgentID))
		fmt.Fprintf(&b, "  Tier:    %s (source=%s)\n", d.Trust.Tier, d.Trust.Source)
		fmt.Fprintf(&b, "  Score:   %.2f | events=%d | recent=%d | trend=%s\n",
			d.Trust.Score, d.Trust.TotalEvents, d.Trust.RecentCount, emptyDash(d.Trust.Trend))
		if !d.Trust.LastActive.IsZero() {
			fmt.Fprintf(&b, "  Active:  %s\n", d.Trust.LastActive.Format(time.RFC3339))
		}
		for _, n := range d.Trust.Notes {
			fmt.Fprintf(&b, "    note: %s\n", n)
		}
	}
	return b.String()
}

// DashboardJSON returns indented JSON for CI pipelines.
func DashboardJSON(d *ChangeDashboard) ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}
