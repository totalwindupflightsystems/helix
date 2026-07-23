package adr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/review"
)

func TestNewADRID_UUIDV4Format(t *testing.T) {
	id := NewADRID()
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("expected 5 UUID segments, got %d: %s", len(parts), id)
	}
	if len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		t.Fatalf("unexpected UUID segment lengths: %s", id)
	}
	// Version nibble should be 4.
	if parts[2][0] != '4' {
		t.Fatalf("expected version 4 UUID, got segment %s", parts[2])
	}
	// IDs should be unique.
	id2 := NewADRID()
	if id == id2 {
		t.Fatalf("expected unique IDs, got collision %s", id)
	}
}

func TestADRStatusTransitions(t *testing.T) {
	if !ValidStatus(StatusProposed) || !ValidStatus(StatusAccepted) {
		t.Fatal("expected proposed/accepted to be valid")
	}
	if ValidStatus("bogus") {
		t.Fatal("bogus status should be invalid")
	}
	if !ValidTransition(StatusProposed, StatusAccepted) {
		t.Fatal("proposed → accepted should be valid")
	}
	if !ValidTransition(StatusAccepted, StatusSuperseded) {
		t.Fatal("accepted → superseded should be valid")
	}
	if ValidTransition(StatusSuperseded, StatusAccepted) {
		t.Fatal("superseded → accepted should be invalid")
	}
	if StatusDisplay(StatusProposed) != "Proposed" {
		t.Fatalf("display: got %q", StatusDisplay(StatusProposed))
	}
	if StatusDisplay(StatusSuperseded) != "Superseded" {
		t.Fatalf("display: got %q", StatusDisplay(StatusSuperseded))
	}
}

func TestADRTypesAndEvidence(t *testing.T) {
	a := &ADR{
		ID:       NewADRID(),
		Title:    "Use event sourcing for audit log",
		Status:   StatusProposed,
		Context:  "Need immutable audit trail",
		Decision: "Use event sourcing",
		Alternatives: []Alternative{
			{Description: "Event sourcing", Tradeoffs: "Complex but auditable"},
			{Description: "CRUD audit table", Tradeoffs: "Simple", RejectedBecause: "Weak causality"},
		},
		Consequences: "Strong audit; higher ops cost",
		EvidenceLinks: []EvidenceLink{
			{Type: EvidenceSpecRef, SpecRef: "spec-abc", Description: "Audit spec"},
			{Type: EvidenceMarketplacePattern, MarketplacePattern: "event-sourcing-audit-pattern"},
		},
		Authors:   []string{"operator", "adr-coauthor"},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if !a.HasEvidence() {
		t.Fatal("expected evidence")
	}
	if a.Filename() == "" {
		t.Fatal("expected filename")
	}
	if !strings.Contains(a.Filename(), ".md") {
		t.Fatalf("filename should end with .md: %s", a.Filename())
	}
	slug := Slugify(a.Title)
	if slug == "" || strings.Contains(slug, " ") {
		t.Fatalf("bad slug: %q", slug)
	}

	req := ADRReviewRequest{
		ADR:                *a,
		Models:             []string{"architecture-consistency", "security-surface"},
		ConsensusThreshold: 0.5,
	}
	if req.ADR.Title == "" || len(req.Models) != 2 {
		t.Fatal("review request not populated")
	}
	res := ADRReviewResult{
		ADRID:          a.ID,
		ConsensusScore: 0.8,
		Passed:         true,
		ModelVerdicts: []ModelVerdict{
			{Model: "architecture-consistency", Verdict: "approve", Score: 0.9},
		},
	}
	if !res.Passed || res.ConsensusScore < 0.5 {
		t.Fatal("unexpected review result defaults")
	}
}

func TestADRStore_SaveLoadList(t *testing.T) {
	dir := t.TempDir()
	store, err := NewADRStore(dir)
	if err != nil {
		t.Fatalf("NewADRStore: %v", err)
	}
	if store.Root() != dir {
		// Root may be cleaned abs path.
		if filepath.Clean(store.Root()) != filepath.Clean(dir) {
			t.Fatalf("root: got %s want %s", store.Root(), dir)
		}
	}

	a := &ADR{
		ID:       NewADRID(),
		Title:    "Use event sourcing for audit log",
		Status:   StatusProposed,
		Context:  "Compliance requires reconstructable history",
		Decision: "Adopt event sourcing for the audit log",
		Alternatives: []Alternative{
			{Description: "Event sourcing", Tradeoffs: "Complex"},
			{Description: "CRUD snapshots", Tradeoffs: "Simple", RejectedBecause: "No event stream"},
		},
		Consequences: "Immutable events; projection lag",
		EvidenceLinks: []EvidenceLink{
			{Type: EvidenceMarketplacePattern, MarketplacePattern: "event-sourcing-audit-pattern"},
		},
		Authors: []string{"tester"},
	}
	if err := store.Save(a); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if a.Number != 1 {
		t.Fatalf("expected number 1, got %d", a.Number)
	}
	if a.Slug == "" {
		t.Fatal("expected slug to be set")
	}

	// File should exist on disk.
	path := filepath.Join(dir, a.Filename())
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}

	loaded, err := store.Load(a.ID)
	if err != nil {
		t.Fatalf("Load by id: %v", err)
	}
	if loaded.Title != a.Title {
		t.Fatalf("title: got %q want %q", loaded.Title, a.Title)
	}
	if loaded.Decision == "" {
		t.Fatal("decision not round-tripped")
	}
	if len(loaded.EvidenceLinks) == 0 {
		t.Fatal("evidence not round-tripped")
	}
	if len(loaded.Alternatives) < 1 {
		t.Fatalf("alternatives not round-tripped: %+v", loaded.Alternatives)
	}

	// Load by number.
	byNum, err := store.Load("1")
	if err != nil {
		t.Fatalf("Load by number: %v", err)
	}
	if byNum.ID != a.ID {
		t.Fatalf("number load id mismatch: %s vs %s", byNum.ID, a.ID)
	}

	// Load by stem.
	stem := strings.TrimSuffix(a.Filename(), ".md")
	byStem, err := store.Load(stem)
	if err != nil {
		t.Fatalf("Load by stem: %v", err)
	}
	if byStem.ID != a.ID {
		t.Fatalf("stem load id mismatch")
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len: got %d", len(list))
	}
}

func TestADRStore_SupersedeLinking(t *testing.T) {
	dir := t.TempDir()
	store, err := NewADRStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	old := &ADR{
		ID:       NewADRID(),
		Title:    "Use CRUD audit table",
		Status:   StatusAccepted,
		Context:  "Original approach",
		Decision: "CRUD snapshots",
		EvidenceLinks: []EvidenceLink{
			{Type: EvidenceSpecRef, SpecRef: "spec-1"},
		},
	}
	if err := store.Save(old); err != nil {
		t.Fatalf("save old: %v", err)
	}

	newADR := &ADR{
		ID:       NewADRID(),
		Title:    "Use event sourcing for audit log",
		Status:   StatusProposed,
		Context:  "Superseding prior approach",
		Decision: "Event sourcing",
		EvidenceLinks: []EvidenceLink{
			{Type: EvidenceMarketplacePattern, MarketplacePattern: "event-sourcing-audit-pattern"},
		},
	}
	saved, err := store.Supersede(old.ID, newADR)
	if err != nil {
		t.Fatalf("Supersede: %v", err)
	}
	if saved.Supersedes != old.ID {
		t.Fatalf("new.Supersedes: got %q want %q", saved.Supersedes, old.ID)
	}

	reloadedOld, err := store.Load(old.ID)
	if err != nil {
		t.Fatalf("reload old: %v", err)
	}
	if reloadedOld.Status != StatusSuperseded {
		t.Fatalf("old status: got %q want %q", reloadedOld.Status, StatusSuperseded)
	}
	if reloadedOld.SupersededBy != saved.ID {
		t.Fatalf("old.SupersededBy: got %q want %q", reloadedOld.SupersededBy, saved.ID)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 ADRs, got %d", len(list))
	}
}

func TestADRCoAuthor_ProposesDecisionAndEvidence(t *testing.T) {
	co := NewADRCoAuthor()
	a, err := co.CoAuthor("spec-audit-1", "Use event sourcing for audit log to meet compliance")
	if err != nil {
		t.Fatalf("CoAuthor: %v", err)
	}
	if a.ID == "" {
		t.Fatal("expected id")
	}
	if a.Decision == "" {
		t.Fatal("expected decision")
	}
	if len(a.Alternatives) < 2 {
		t.Fatalf("expected alternatives, got %d", len(a.Alternatives))
	}
	if !a.HasEvidence() {
		t.Fatal("expected evidence links")
	}
	foundSpec := false
	for _, e := range a.EvidenceLinks {
		if e.SpecRef == "spec-audit-1" {
			foundSpec = true
		}
	}
	if !foundSpec {
		t.Fatalf("expected spec ref in evidence: %+v", a.EvidenceLinks)
	}
	if a.Status != StatusProposed {
		t.Fatalf("status: %s", a.Status)
	}
	if a.Consequences == "" {
		t.Fatal("expected consequences")
	}
}

func TestADRReviewer_MultiModelConsensus(t *testing.T) {
	co := NewADRCoAuthor()
	a, err := co.CoAuthor("spec-x", "Use event sourcing for audit log with retention and consumer lag budgets")
	if err != nil {
		t.Fatalf("coauthor: %v", err)
	}

	reviewer := NewADRReviewer()
	res, err := reviewer.Review(context.Background(), &ADRReviewRequest{
		ADR:                *a,
		Models:             []string{"architecture-consistency", "security-surface", "operational-performance"},
		ConsensusThreshold: 0.5,
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if res.ADRID != a.ID {
		t.Fatalf("adr id mismatch")
	}
	if len(res.ModelVerdicts) != 3 {
		t.Fatalf("expected 3 verdicts, got %d", len(res.ModelVerdicts))
	}
	if res.ConsensusScore <= 0 || res.ConsensusScore > 1 {
		t.Fatalf("consensus out of range: %f", res.ConsensusScore)
	}
	if res.Summary == "" {
		t.Fatal("expected summary")
	}

	// Empty ADR decision should lower scores / raise concerns.
	bad := *a
	bad.Decision = ""
	bad.EvidenceLinks = nil
	bad.Alternatives = nil
	res2, err := reviewer.Review(context.Background(), &ADRReviewRequest{
		ADR:                bad,
		ConsensusThreshold: 0.9,
	})
	if err != nil {
		t.Fatalf("Review bad: %v", err)
	}
	if res2.ConsensusScore >= res.ConsensusScore {
		t.Fatalf("expected worse consensus for incomplete ADR: good=%f bad=%f", res.ConsensusScore, res2.ConsensusScore)
	}
}

func TestADRCoAuthor_RequiresInput(t *testing.T) {
	co := NewADRCoAuthor()
	if _, err := co.CoAuthor("", ""); err == nil {
		t.Fatal("expected error for empty input")
	}
}

// ---------------------------------------------------------------------------
// CoAuthorFromDraft + mergeEvidence + firstSentence + truncateRunes + containsAny
// ---------------------------------------------------------------------------

func TestCoAuthorFromDraft_EnrichesPartialADR(t *testing.T) {
	co := NewADRCoAuthor()
	draft := &ADR{
		Title:   "Use PostgreSQL for identity store",
		Context: "Need durable transactional store with ACID guarantees",
	}
	result, err := co.CoAuthorFromDraft(draft, "spec-db-1")
	if err != nil {
		t.Fatalf("CoAuthorFromDraft: %v", err)
	}
	if result.Title != draft.Title {
		t.Fatalf("title: got %q want %q", result.Title, draft.Title)
	}
	if result.Context != draft.Context {
		t.Fatalf("context: got %q want %q", result.Context, draft.Context)
	}
	if len(result.Alternatives) < 2 {
		t.Fatalf("expected alternatives, got %d", len(result.Alternatives))
	}
	if !result.HasEvidence() {
		t.Fatal("expected evidence links from enrichment")
	}
	foundSpec := false
	for _, e := range result.EvidenceLinks {
		if e.SpecRef == "spec-db-1" {
			foundSpec = true
		}
	}
	if !foundSpec {
		t.Fatalf("expected spec ref in evidence: %+v", result.EvidenceLinks)
	}
}

func TestCoAuthorFromDraft_PreservesDraftFields(t *testing.T) {
	co := NewADRCoAuthor()
	draftID := NewADRID()
	draft := &ADR{
		ID:            draftID,
		Title:         "Use Redis for caching",
		Context:       "Latency budget requires cache",
		Decision:      "Adopt Redis as cache-aside",
		Consequences:  "Must manage TTLs and cache invalidation",
		Status:        StatusAccepted,
		Number:        7,
		Authors:       []string{"operator"},
		Alternatives:  []Alternative{{Description: "Memcached", Tradeoffs: "Simpler", RejectedBecause: "Less featureful"}},
		EvidenceLinks: []EvidenceLink{{Type: EvidenceSpecRef, SpecRef: "spec-cache"}},
	}
	result, err := co.CoAuthorFromDraft(draft, "spec-cache")
	if err != nil {
		t.Fatalf("CoAuthorFromDraft: %v", err)
	}
	if result.ID != draftID {
		t.Fatalf("id: got %q want %q", result.ID, draftID)
	}
	if result.Decision != draft.Decision {
		t.Fatalf("decision: got %q want %q", result.Decision, draft.Decision)
	}
	if result.Consequences != draft.Consequences {
		t.Fatalf("consequences: got %q want %q", result.Consequences, draft.Consequences)
	}
	if result.Status != StatusAccepted {
		t.Fatalf("status: got %q", result.Status)
	}
	if result.Number != 7 {
		t.Fatalf("number: got %d", result.Number)
	}
	if len(result.Authors) != 1 || result.Authors[0] != "operator" {
		t.Fatalf("authors: got %v", result.Authors)
	}
	if len(result.Alternatives) != 1 {
		t.Fatalf("expected draft alternatives preserved, got %d", len(result.Alternatives))
	}
	// Merged evidence: draft spec-cache evidence dedups with co-author spec-cache evidence
	// → 1 entry. Verify it's present.
	foundMergedSpec := false
	for _, e := range result.EvidenceLinks {
		if e.SpecRef == "spec-cache" {
			foundMergedSpec = true
		}
	}
	if !foundMergedSpec {
		t.Fatalf("expected spec-cache in merged evidence: %+v", result.EvidenceLinks)
	}
}

func TestCoAuthorFromDraft_NilDraft(t *testing.T) {
	co := NewADRCoAuthor()
	_, err := co.CoAuthorFromDraft(nil, "spec-x")
	if err == nil {
		t.Fatal("expected error for nil draft")
	}
}

func TestCoAuthorFromDraft_EmptyTitle(t *testing.T) {
	co := NewADRCoAuthor()
	_, err := co.CoAuthorFromDraft(&ADR{Title: "  "}, "spec-x")
	if err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestCoAuthorFromDraft_EmptyContextUsesDecision(t *testing.T) {
	co := NewADRCoAuthor()
	draft := &ADR{
		Title:    "Use event sourcing for audit log",
		Decision: "We need an event-sourced audit trail",
	}
	result, err := co.CoAuthorFromDraft(draft, "")
	if err != nil {
		t.Fatalf("CoAuthorFromDraft: %v", err)
	}
	if result.Title != draft.Title {
		t.Fatalf("title: got %q", result.Title)
	}
	if len(result.Alternatives) < 2 {
		t.Fatalf("expected alternatives for event sourcing, got %d", len(result.Alternatives))
	}
}

func TestCoAuthorFromDraft_NoAuthorsFallsBack(t *testing.T) {
	co := NewADRCoAuthor()
	draft := &ADR{
		Title:  "Use Kafka for event bus",
		Status: StatusProposed,
	}
	result, err := co.CoAuthorFromDraft(draft, "spec-kafka")
	if err != nil {
		t.Fatalf("CoAuthorFromDraft: %v", err)
	}
	if len(result.Authors) < 2 {
		t.Fatalf("expected fallback authors, got %v", result.Authors)
	}
}

func TestMergeEvidence_DeduplicatesByKey(t *testing.T) {
	primary := []EvidenceLink{
		{Type: EvidenceSpecRef, SpecRef: "spec-a", Description: "primary spec a"},
		{Type: EvidenceMarketplacePattern, MarketplacePattern: "pattern-x"},
	}
	secondary := []EvidenceLink{
		{Type: EvidenceSpecRef, SpecRef: "spec-a", Description: "dup spec a"},           // dup
		{Type: EvidenceMarketplacePattern, MarketplacePattern: "pattern-x"},             // dup
		{Type: EvidenceIncidentRef, IncidentRef: "inc-1", Description: "unique incident"}, // unique
	}
	merged := mergeEvidence(primary, secondary)
	if len(merged) != 3 {
		t.Fatalf("expected 3 after dedup, got %d: %+v", len(merged), merged)
	}
	// Primary entries should come first.
	if merged[0].SpecRef != "spec-a" || merged[0].Description != "primary spec a" {
		t.Fatalf("first merged should be primary spec-a: %+v", merged[0])
	}
}

func TestMergeEvidence_EmptySlices(t *testing.T) {
	merged := mergeEvidence(nil, nil)
	if len(merged) != 0 {
		t.Fatalf("expected 0, got %d", len(merged))
	}
	merged2 := mergeEvidence(nil, []EvidenceLink{{Type: EvidenceSpecRef, SpecRef: "x"}})
	if len(merged2) != 1 {
		t.Fatalf("expected 1, got %d", len(merged2))
	}
}

func TestFirstSentence(t *testing.T) {
	tests := []struct {
		name string
		input string
		want  string
	}{
		{"dot-space split", "First sentence. Second sentence.", "First sentence."},
		{"dot-newline split", "First sentence.\nSecond sentence.", "First sentence."},
		{"newline split", "First line\nSecond line", "First line"},
		{"no separator returns truncated", strings.Repeat("a", 300), strings.Repeat("a", 239) + "…"},
		{"whitespace trim", "  Hello world.  Done.", "Hello world."},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstSentence(tt.input)
			if got != tt.want {
				t.Errorf("firstSentence(%q): got %q want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("hello", 10); got != "hello" {
		t.Fatalf("short string should be unchanged: got %q", got)
	}
	long := strings.Repeat("x", 100)
	got := truncateRunes(long, 10)
	if len([]rune(got)) != 10 {
		t.Fatalf("expected 10 runes, got %d: %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix: %q", got)
	}
	// Unicode: multi-byte runes
	unicodeStr := strings.Repeat("é", 50)
	got2 := truncateRunes(unicodeStr, 10)
	if len([]rune(got2)) != 10 {
		t.Fatalf("expected 10 runes for unicode, got %d", len([]rune(got2)))
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny("hello world", "hello", "xyz") {
		t.Fatal("expected match for hello")
	}
	if containsAny("hello world", "xyz", "abc") {
		t.Fatal("should not match")
	}
	if containsAny("hello") {
		t.Fatal("no needles should return false")
	}
	if !containsAny("hello", "hello") {
		t.Fatal("exact match should return true")
	}
}

// ---------------------------------------------------------------------------
// deriveTitle, deriveDecision, deriveContext, deriveAlternatives, deriveEvidence,
// deriveConsequences, estimateRisk, estimateBlastRadius
// ---------------------------------------------------------------------------

func TestDeriveTitle_UsesContextLine(t *testing.T) {
	// Long line without keyword prefix — uses first substantial line
	got := deriveTitle("", "Some long architecture decision title here\nsecond line")
	if got != "Some long architecture decision title here" {
		t.Fatalf("deriveTitle: got %q", got)
	}
}

func TestDeriveTitle_KeywordPrefix(t *testing.T) {
	got := deriveTitle("", "Use Redis for caching\nother")
	if got != "Use Redis for caching" {
		t.Fatalf("deriveTitle: got %q", got)
	}
	got2 := deriveTitle("", "Adopt event sourcing\nother")
	if got2 != "Adopt event sourcing" {
		t.Fatalf("deriveTitle: got %q", got2)
	}
}

func TestDeriveTitle_FallsBackToSpecRef(t *testing.T) {
	got := deriveTitle("spec-foo", "")
	if !strings.Contains(got, "spec-foo") {
		t.Fatalf("expected spec ref in title: %q", got)
	}
	got2 := deriveTitle("", "")
	if got2 != "Architecture decision" {
		t.Fatalf("expected default title, got %q", got2)
	}
}

func TestDeriveDecision_Variants(t *testing.T) {
	tests := []struct {
		name   string
		title  string
		ctx    string
		needle string
	}{
		{"event sourcing", "Audit log", "Use event sourcing for the audit log", "event sourcing"},
		{"postgres", "Data store", "Use PostgreSQL", "PostgreSQL"},
		{"redis cache", "Caching", "Use Redis as cache", "Redis"},
		{"kafka", "Event bus", "Use Kafka message queue", "message bus"},
		{"monolith", "Architecture", "Keep the monolith", "modular monolith"},
		{"microservice", "Service", "Extract microservice", "dedicated service"},
		{"auth", "Identity", "Use OAuth for auth", "OIDC"},
		{"default with context", "Some title", "Some random architecture context", "adopt the architecture"},
		{"default no context", "Some title", "", "proceed with"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveDecision(tt.title, tt.ctx)
			if !strings.Contains(strings.ToLower(got), strings.ToLower(tt.needle)) {
				t.Errorf("deriveDecision(%q, %q): got %q, want needle %q", tt.title, tt.ctx, got, tt.needle)
			}
		})
	}
}

func TestDeriveContext(t *testing.T) {
	// With specRef and context
	got := deriveContext("spec-x", "Some context here")
	if !strings.Contains(got, "spec-x") {
		t.Fatalf("expected spec ref: %q", got)
	}
	if !strings.Contains(got, "Some context here") {
		t.Fatalf("expected context: %q", got)
	}
	// Without context
	got2 := deriveContext("spec-x", "")
	if strings.Contains(got2, "Architecture context provided") {
		t.Fatalf("should not have context label: %q", got2)
	}
	if !strings.Contains(got2, "No additional architecture context") {
		t.Fatalf("expected fallback message: %q", got2)
	}
}

func TestDeriveAlternatives_DomainSpecific(t *testing.T) {
	// Event sourcing → domain-specific alternatives
	alts := deriveAlternatives("Audit log", "event sourc")
	if len(alts) < 2 {
		t.Fatalf("expected >= 2 alternatives for event sourcing, got %d", len(alts))
	}
	// PostgreSQL → domain-specific
	alts2 := deriveAlternatives("Database", "PostgreSQL")
	if len(alts2) < 2 {
		t.Fatalf("expected >= 2 alternatives for postgres, got %d", len(alts2))
	}
	// Auth → domain-specific
	alts3 := deriveAlternatives("Identity", "OAuth")
	if len(alts3) < 2 {
		t.Fatalf("expected >= 2 alternatives for auth, got %d", len(alts3))
	}
	// Generic fallback
	alts4 := deriveAlternatives("Something", "random context")
	if len(alts4) != 3 {
		t.Fatalf("expected 3 generic alternatives, got %d", len(alts4))
	}
}

func TestDeriveEvidence_Patterns(t *testing.T) {
	// With specRef
	ev := deriveEvidence("spec-x", "auth oauth")
	if len(ev) < 2 {
		t.Fatalf("expected >= 2 evidence links, got %d", len(ev))
	}
	// Without specRef → marketplace fallback
	ev2 := deriveEvidence("", "audit event sourc")
	if len(ev2) < 2 {
		t.Fatalf("expected >= 2 evidence links without specRef, got %d", len(ev2))
	}
	foundMarketplace := false
	for _, e := range ev2 {
		if e.Type == EvidenceMarketplacePattern {
			foundMarketplace = true
		}
	}
	if !foundMarketplace {
		t.Fatal("expected marketplace pattern in evidence")
	}
	// Incident evidence
	ev3 := deriveEvidence("spec-x", "incident postmortem")
	foundIncident := false
	for _, e := range ev3 {
		if e.Type == EvidenceIncidentRef {
			foundIncident = true
		}
	}
	if !foundIncident {
		t.Fatal("expected incident ref in evidence")
	}
}

func TestDeriveConsequences(t *testing.T) {
	alts := []Alternative{
		{Description: "A", RejectedBecause: "no"},
		{Description: "B", RejectedBecause: ""},
	}
	got := deriveConsequences("Test title", alts)
	if !strings.Contains(got, "Positive") {
		t.Fatalf("expected Positive section: %q", got)
	}
	if !strings.Contains(got, "Negative") {
		t.Fatalf("expected Negative section: %q", got)
	}
	if !strings.Contains(got, "Rejected alternatives (1)") {
		t.Fatalf("expected rejected count: %q", got)
	}
}

func TestEstimateRisk(t *testing.T) {
	// Auth + migration + event sourcing + < 2 alts → 35+25+15+10+10 = 95
	risk := estimateRisk("auth security", "migration event sourc", []Alternative{{Description: "A"}})
	if risk != 95 {
		t.Fatalf("expected risk 95, got %f", risk)
	}
	// Capped at 100: auth + security + crypto + secret + payment = all keywords
	risk3 := estimateRisk("auth security crypto secret payment migration rewrite breaking event sourc distributed multi-region", "", []Alternative{})
	if risk3 != 95 {
		t.Fatalf("expected risk 95, got %f", risk3)
	}
	// Low risk scenario
	risk2 := estimateRisk("Generic title", "some context", []Alternative{
		{Description: "A"}, {Description: "B"}, {Description: "C"},
	})
	if risk2 != 35 {
		t.Fatalf("expected base risk 35, got %f", risk2)
	}
}

func TestEstimateBlastRadius(t *testing.T) {
	// Multiple layers
	br := estimateBlastRadius("API auth database deploy platform", "schema token network global")
	layers := strings.Split(br, " > ")
	if len(layers) < 5 {
		t.Fatalf("expected >= 5 layers, got %d: %q", len(layers), br)
	}
	// Minimal blast radius
	br2 := estimateBlastRadius("Small decision", "")
	if br2 != "decision-local" {
		t.Fatalf("expected decision-local, got %q", br2)
	}
}

// ---------------------------------------------------------------------------
// StatusDisplay, ValidTransition edge cases
// ---------------------------------------------------------------------------

func TestStatusDisplay_EdgeCases(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{StatusProposed, "Proposed"},
		{StatusAccepted, "Accepted"},
		{StatusDeprecated, "Deprecated"},
		{StatusSuperseded, "Superseded"},
		{"", "Unknown"},
		{"custom", "Custom"},
		{"draft", "Draft"},
	}
	for _, tt := range tests {
		got := StatusDisplay(tt.status)
		if got != tt.want {
			t.Errorf("StatusDisplay(%q): got %q want %q", tt.status, got, tt.want)
		}
	}
}

func TestValidTransition_EdgeCases(t *testing.T) {
	// Same → same is always valid for known statuses
	if !ValidTransition(StatusProposed, StatusProposed) {
		t.Fatal("proposed → proposed should be valid")
	}
	if !ValidTransition(StatusAccepted, StatusAccepted) {
		t.Fatal("accepted → accepted should be valid")
	}
	if !ValidTransition(StatusDeprecated, StatusSuperseded) {
		t.Fatal("deprecated → superseded should be valid")
	}
	// Invalid transitions
	if ValidTransition("bogus", StatusAccepted) {
		t.Fatal("invalid from status should return false")
	}
	if ValidTransition(StatusAccepted, "bogus") {
		t.Fatal("invalid to status should return false")
	}
	if ValidTransition(StatusDeprecated, StatusProposed) {
		t.Fatal("deprecated → proposed should be invalid")
	}
	if ValidTransition(StatusDeprecated, StatusAccepted) {
		t.Fatal("deprecated → accepted should be invalid")
	}
	if ValidTransition(StatusSuperseded, StatusProposed) {
		t.Fatal("superseded → proposed should be invalid")
	}
	if ValidTransition(StatusSuperseded, StatusDeprecated) {
		t.Fatal("superseded → deprecated should be invalid")
	}
	if ValidTransition(StatusSuperseded, StatusAccepted) {
		t.Fatal("superseded → accepted should be invalid")
	}
	if ValidTransition(StatusAccepted, StatusProposed) {
		t.Fatal("accepted → proposed should be invalid")
	}
	// Proposed can go anywhere
	if !ValidTransition(StatusProposed, StatusDeprecated) {
		t.Fatal("proposed → deprecated should be valid")
	}
	if !ValidTransition(StatusProposed, StatusSuperseded) {
		t.Fatal("proposed → superseded should be valid")
	}
}

// ---------------------------------------------------------------------------
// ADRStore: Save error paths, Load error paths, List edge cases, resolvePath,
// findPathByID, resolveStoreRoot
// ---------------------------------------------------------------------------

func TestADRStore_SaveErrors(t *testing.T) {
	dir := t.TempDir()
	store, err := NewADRStore(dir)
	if err != nil {
		t.Fatalf("NewADRStore: %v", err)
	}
	// Nil ADR
	if err := store.Save(nil); err == nil {
		t.Fatal("expected error for nil ADR")
	}
	// Empty ID
	if err := store.Save(&ADR{Title: "test"}); err == nil {
		t.Fatal("expected error for empty ID")
	}
	// Empty title
	if err := store.Save(&ADR{ID: NewADRID()}); err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestADRStore_SaveExistingIDReplaces(t *testing.T) {
	dir := t.TempDir()
	store, err := NewADRStore(dir)
	if err != nil {
		t.Fatalf("NewADRStore: %v", err)
	}
	a := &ADR{
		ID:    NewADRID(),
		Title: "Original title",
	}
	if err := store.Save(a); err != nil {
		t.Fatalf("Save: %v", err)
	}
	list, _ := store.List()
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	// Save again with same ID — should replace, not duplicate
	a.Title = "Updated title"
	if err := store.Save(a); err != nil {
		t.Fatalf("Save update: %v", err)
	}
	list2, _ := store.List()
	if len(list2) != 1 {
		t.Fatalf("expected 1 after update, got %d", len(list2))
	}
}

func TestADRStore_LoadErrors(t *testing.T) {
	dir := t.TempDir()
	store, err := NewADRStore(dir)
	if err != nil {
		t.Fatalf("NewADRStore: %v", err)
	}
	// Empty ref
	if _, err := store.Load(""); err == nil {
		t.Fatal("expected error for empty ref")
	}
	// Not found
	if _, err := store.Load("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent ref")
	}
}

func TestADRStore_LoadByFilename(t *testing.T) {
	dir := t.TempDir()
	store, err := NewADRStore(dir)
	if err != nil {
		t.Fatalf("NewADRStore: %v", err)
	}
	a := &ADR{
		ID:    NewADRID(),
		Title: "Test decision",
	}
	if err := store.Save(a); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Load by .md filename
	loaded, err := store.Load(a.Filename())
	if err != nil {
		t.Fatalf("Load by filename: %v", err)
	}
	if loaded.ID != a.ID {
		t.Fatalf("id mismatch: got %s want %s", loaded.ID, a.ID)
	}
}

func TestADRStore_ListHandlesCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	store, err := NewADRStore(dir)
	if err != nil {
		t.Fatalf("NewADRStore: %v", err)
	}
	// Save a valid ADR
	a := &ADR{ID: NewADRID(), Title: "Valid decision"}
	if err := store.Save(a); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Write a corrupt .md file
	corruptPath := filepath.Join(dir, "9999-corrupt.md")
	if err := os.WriteFile(corruptPath, []byte("not valid frontmatter"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	// Write a non-md file (should be ignored)
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	// Create a subdirectory (should be ignored)
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 valid ADR (corrupt skipped), got %d", len(list))
	}
}

func TestADRStore_LoadByNumberPrefix(t *testing.T) {
	dir := t.TempDir()
	store, err := NewADRStore(dir)
	if err != nil {
		t.Fatalf("NewADRStore: %v", err)
	}
	a := &ADR{ID: NewADRID(), Title: "First decision"}
	if err := store.Save(a); err != nil {
		t.Fatalf("Save: %v", err)
	}
	b := &ADR{ID: NewADRID(), Title: "Second decision"}
	if err := store.Save(b); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Load by padded number "0002"
	loaded, err := store.Load("0002")
	if err != nil {
		t.Fatalf("Load by padded number: %v", err)
	}
	if loaded.ID != b.ID {
		t.Fatalf("expected second ADR, got %s", loaded.ID)
	}
}

func TestADRStore_SupersedeErrors(t *testing.T) {
	dir := t.TempDir()
	store, err := NewADRStore(dir)
	if err != nil {
		t.Fatalf("NewADRStore: %v", err)
	}
	// Nonexistent old ID
	if _, err := store.Supersede("nonexistent", &ADR{Title: "test"}); err == nil {
		t.Fatal("expected error for nonexistent old ID")
	}
	// Save an old one, then test nil new ADR
	old := &ADR{ID: NewADRID(), Title: "Old", Status: StatusAccepted}
	if err := store.Save(old); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := store.Supersede(old.ID, nil); err == nil {
		t.Fatal("expected error for nil new ADR")
	}
	// Empty title
	if _, err := store.Supersede(old.ID, &ADR{Title: ""}); err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestADRStore_SupersedeExistingEvidenceLink(t *testing.T) {
	dir := t.TempDir()
	store, err := NewADRStore(dir)
	if err != nil {
		t.Fatalf("NewADRStore: %v", err)
	}
	old := &ADR{ID: NewADRID(), Title: "Old", Status: StatusAccepted}
	if err := store.Save(old); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// New ADR already has evidence link referencing old.ID
	newADR := &ADR{
		ID:            NewADRID(),
		Title:         "New approach",
		Status:        StatusProposed,
		EvidenceLinks: []EvidenceLink{{Type: EvidenceSpecRef, SpecRef: "adr:" + old.ID, Description: "supersedes " + old.ID}},
	}
	saved, err := store.Supersede(old.ID, newADR)
	if err != nil {
		t.Fatalf("Supersede: %v", err)
	}
	// Should not add a duplicate evidence link
	hasLinkCount := 0
	for _, e := range saved.EvidenceLinks {
		if e.Description == "supersedes "+old.ID {
			hasLinkCount++
		}
	}
	if hasLinkCount != 1 {
		t.Fatalf("expected exactly 1 existing link, got %d", hasLinkCount)
	}
}

func TestResolveStoreRoot(t *testing.T) {
	// Empty root → ~/.helix/adrs
	got, err := resolveStoreRoot("")
	if err != nil {
		t.Fatalf("resolveStoreRoot empty: %v", err)
	}
	if !strings.HasSuffix(got, DefaultADRsDir) {
		t.Fatalf("expected default dir suffix, got %q", got)
	}
	// ~/ path
	got2, err := resolveStoreRoot("~/custom/adrs")
	if err != nil {
		t.Fatalf("resolveStoreRoot ~/: %v", err)
	}
	if !strings.HasSuffix(got2, "custom/adrs") {
		t.Fatalf("expected custom path, got %q", got2)
	}
	// Absolute path
	got3, err := resolveStoreRoot("/tmp/adrs-test")
	if err != nil {
		t.Fatalf("resolveStoreRoot absolute: %v", err)
	}
	if got3 != "/tmp/adrs-test" {
		t.Fatalf("expected /tmp/adrs-test, got %q", got3)
	}
}

// ---------------------------------------------------------------------------
// parseRFC3339, markdownToADR
// ---------------------------------------------------------------------------

func TestParseRFC3339(t *testing.T) {
	valid := "2024-01-15T10:30:00Z"
	got := parseRFC3339(valid)
	if got.IsZero() {
		t.Fatal("expected valid time")
	}
	if got.Year() != 2024 {
		t.Fatalf("expected 2024, got %d", got.Year())
	}
	// Empty string
	if !parseRFC3339("").IsZero() {
		t.Fatal("expected zero time for empty")
	}
	// Invalid string
	if !parseRFC3339("invalid").IsZero() {
		t.Fatal("expected zero time for invalid")
	}
}

func TestMarkdownToADR_Malformed(t *testing.T) {
	// No frontmatter
	_, err := markdownToADR([]byte("no frontmatter here"))
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
	// Unterminated frontmatter
	_, err = markdownToADR([]byte("---\nid: test\nstatus: proposed\n"))
	if err == nil {
		t.Fatal("expected error for unterminated frontmatter")
	}
}

// ---------------------------------------------------------------------------
// Review: ADRReviewer, WithReviewModelClients, offlineADRClient,
// AdaptReviewModelClient, reviewModelAdapter, formatADRAsReviewPayload,
// verdictFromScore, computeConsensus, detectConflicts, collectSuggestions,
// normalizeVerdictClass, review functions
// ---------------------------------------------------------------------------

func TestADRReviewer_NilRequest(t *testing.T) {
	reviewer := NewADRReviewer()
	_, err := reviewer.Review(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestADRReviewer_EmptyADRID(t *testing.T) {
	reviewer := NewADRReviewer()
	_, err := reviewer.Review(context.Background(), &ADRReviewRequest{ADR: ADR{Title: "test"}})
	if err == nil {
		t.Fatal("expected error for empty ADR ID")
	}
}

func TestADRReviewer_AllModelsFail(t *testing.T) {
	// Use injected clients that all return errors
	failing := &mockADRClient{name: "failing-model", err: fmt.Errorf("model unavailable")}
	reviewer := NewADRReviewer(failing)
	_, err := reviewer.Review(context.Background(), &ADRReviewRequest{
		ADR: ADR{ID: NewADRID(), Title: "test"},
	})
	if err == nil {
		t.Fatal("expected error when all models fail")
	}
	if !strings.Contains(err.Error(), "all models failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestADRReviewer_NilVerdict(t *testing.T) {
	nilVerdict := &mockADRClient{name: "nil-verdict-model", nilVerdict: true}
	reviewer := NewADRReviewer(nilVerdict)
	_, err := reviewer.Review(context.Background(), &ADRReviewRequest{
		ADR: ADR{ID: NewADRID(), Title: "test"},
	})
	if err == nil {
		t.Fatal("expected error when model returns nil verdict")
	}
}

func TestADRReviewer_NilContext(t *testing.T) {
	reviewer := NewADRReviewer()
	_, err := reviewer.Review(nil, &ADRReviewRequest{
		ADR: ADR{ID: NewADRID(), Title: "test", Decision: "Some decision"},
	})
	if err != nil {
		t.Fatalf("expected nil context to be handled, got: %v", err)
	}
}

func TestADRReviewer_ResolveClients_FilterByModel(t *testing.T) {
	c1 := &mockADRClient{name: "model-a", verdict: &ModelVerdict{Model: "model-a", Verdict: "approve", Score: 0.9}}
	c2 := &mockADRClient{name: "model-b", verdict: &ModelVerdict{Model: "model-b", Verdict: "warn", Score: 0.6}}
	reviewer := NewADRReviewer(c1, c2)
	res, err := reviewer.Review(context.Background(), &ADRReviewRequest{
		ADR:    ADR{ID: NewADRID(), Title: "test", Decision: "decision"},
		Models: []string{"model-a"},
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(res.ModelVerdicts) != 1 {
		t.Fatalf("expected 1 verdict (filtered), got %d", len(res.ModelVerdicts))
	}
	if res.ModelVerdicts[0].Model != "model-a" {
		t.Fatalf("expected model-a, got %s", res.ModelVerdicts[0].Model)
	}
}

func TestADRReviewer_ResolveClients_FilterMissFallsToOffline(t *testing.T) {
	c1 := &mockADRClient{name: "model-a", verdict: &ModelVerdict{Model: "model-a", Verdict: "approve", Score: 0.9}}
	reviewer := NewADRReviewer(c1)
	// Request a model that doesn't match — should fall through to offline named models
	res, err := reviewer.Review(context.Background(), &ADRReviewRequest{
		ADR:    ADR{ID: NewADRID(), Title: "test", Decision: "decision"},
		Models: []string{"architecture-consistency"},
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(res.ModelVerdicts) < 1 {
		t.Fatal("expected at least 1 verdict")
	}
}

func TestADRReviewer_ResolveClients_NoModelsUsesDefaults(t *testing.T) {
	reviewer := NewADRReviewer()
	clients := reviewer.resolveClients(nil)
	if len(clients) != len(defaultADRModels) {
		t.Fatalf("expected %d default clients, got %d", len(defaultADRModels), len(clients))
	}
}

func TestWithReviewModelClients(t *testing.T) {
	mockMC := &mockReviewModelClient{
		info: review.ModelInfo{Model: "test-llm", Provider: "openai"},
		result: &review.ModelReviewResult{
			Verdict:  "approved",
			Findings: []review.Finding{},
		},
	}
	reviewer := NewADRReviewer()
	r := reviewer.WithReviewModelClients(mockMC)
	if r != reviewer {
		t.Fatal("WithReviewModelClients should return same reviewer")
	}
	clients := reviewer.resolveClients(nil)
	if len(clients) != 1 {
		t.Fatalf("expected 1 adapted client, got %d", len(clients))
	}
}

func TestWithReviewModelClients_NilClientSkipped(t *testing.T) {
	reviewer := NewADRReviewer()
	reviewer.WithReviewModelClients(nil)
	if len(reviewer.clients) != 0 {
		t.Fatalf("nil client should be skipped, got %d clients", len(reviewer.clients))
	}
}

func TestAdaptReviewModelClient_NameFromModel(t *testing.T) {
	mc := &mockReviewModelClient{
		info: review.ModelInfo{Model: "gpt-4", Provider: "openai"},
	}
	adapter := AdaptReviewModelClient(mc)
	if adapter.Name() != "gpt-4" {
		t.Fatalf("expected name gpt-4, got %s", adapter.Name())
	}
}

func TestAdaptReviewModelClient_NameFromProvider(t *testing.T) {
	mc := &mockReviewModelClient{
		info: review.ModelInfo{Model: "", Provider: "anthropic"},
	}
	adapter := AdaptReviewModelClient(mc)
	if adapter.Name() != "anthropic" {
		t.Fatalf("expected name anthropic, got %s", adapter.Name())
	}
}

func TestAdaptReviewModelClient_NameFallback(t *testing.T) {
	mc := &mockReviewModelClient{
		info: review.ModelInfo{Model: "", Provider: ""},
	}
	adapter := AdaptReviewModelClient(mc)
	if adapter.Name() != "review-model" {
		t.Fatalf("expected name review-model, got %s", adapter.Name())
	}
}

func TestAdaptReviewModelClient_ReviewADR(t *testing.T) {
	mc := &mockReviewModelClient{
		info: review.ModelInfo{Model: "test-model"},
		result: &review.ModelReviewResult{
			Verdict: "approved",
			Findings: []review.Finding{
				{Description: "minor issue", Severity: "low"},
			},
		},
	}
	adapter := AdaptReviewModelClient(mc)
	adr := &ADR{
		ID:        NewADRID(),
		Title:     "Test ADR",
		Status:    StatusProposed,
		Context:   "ctx",
		Decision:  "decide",
	}
	verdict, err := adapter.ReviewADR(context.Background(), adr)
	if err != nil {
		t.Fatalf("ReviewADR: %v", err)
	}
	if verdict.Model != "test-model" {
		t.Fatalf("expected model test-model, got %s", verdict.Model)
	}
	if verdict.Verdict != "approve" {
		t.Fatalf("expected verdict approve, got %s", verdict.Verdict)
	}
	if verdict.Score != 0.9 {
		t.Fatalf("expected score 0.9, got %f", verdict.Score)
	}
}

func TestAdaptReviewModelClient_ReviewADR_WarnVerdict(t *testing.T) {
	mc := &mockReviewModelClient{
		info: review.ModelInfo{Model: "test-model"},
		result: &review.ModelReviewResult{
			Verdict: "pass_with_notes",
		},
	}
	adapter := AdaptReviewModelClient(mc)
	adr := &ADR{ID: NewADRID(), Title: "Test"}
	verdict, err := adapter.ReviewADR(context.Background(), adr)
	if err != nil {
		t.Fatalf("ReviewADR: %v", err)
	}
	if verdict.Verdict != "warn" {
		t.Fatalf("expected warn, got %s", verdict.Verdict)
	}
	if verdict.Score != 0.65 {
		t.Fatalf("expected 0.65, got %f", verdict.Score)
	}
}

func TestAdaptReviewModelClient_ReviewADR_BlockVerdict(t *testing.T) {
	mc := &mockReviewModelClient{
		info: review.ModelInfo{Model: "test-model"},
		result: &review.ModelReviewResult{
			Verdict: "block",
		},
	}
	adapter := AdaptReviewModelClient(mc)
	adr := &ADR{ID: NewADRID(), Title: "Test"}
	verdict, err := adapter.ReviewADR(context.Background(), adr)
	if err != nil {
		t.Fatalf("ReviewADR: %v", err)
	}
	if verdict.Verdict != "reject" {
		t.Fatalf("expected reject, got %s", verdict.Verdict)
	}
	if verdict.Score != 0.3 {
		t.Fatalf("expected 0.3, got %f", verdict.Score)
	}
}

func TestAdaptReviewModelClient_ReviewADR_CriticalFindings(t *testing.T) {
	mc := &mockReviewModelClient{
		info: review.ModelInfo{Model: "test-model"},
		result: &review.ModelReviewResult{
			Verdict: "approved",
			Findings: []review.Finding{
				{Description: "critical issue", Severity: "critical"},
				{Description: "high issue", Severity: "high"},
				{Description: "medium issue", Severity: "medium"},
			},
		},
	}
	adapter := AdaptReviewModelClient(mc)
	adr := &ADR{ID: NewADRID(), Title: "Test"}
	verdict, err := adapter.ReviewADR(context.Background(), adr)
	if err != nil {
		t.Fatalf("ReviewADR: %v", err)
	}
	// Approved → 0.9, then -0.1 for critical + -0.1 for high = ~0.7
	if verdict.Score < 0.69 || verdict.Score > 0.71 {
		t.Fatalf("expected ~0.7, got %f", verdict.Score)
	}
	if len(verdict.Concerns) != 3 {
		t.Fatalf("expected 3 concerns, got %d", len(verdict.Concerns))
	}
}

func TestAdaptReviewModelClient_ReviewADR_ReviewError(t *testing.T) {
	mc := &mockReviewModelClient{
		info: review.ModelInfo{Model: "test-model"},
		err:  fmt.Errorf("network error"),
	}
	adapter := AdaptReviewModelClient(mc)
	adr := &ADR{ID: NewADRID(), Title: "Test"}
	_, err := adapter.ReviewADR(context.Background(), adr)
	if err == nil {
		t.Fatal("expected error from Review")
	}
}

func TestAdaptReviewModelClient_ReviewADR_NilResult(t *testing.T) {
	mc := &mockReviewModelClient{
		info:   review.ModelInfo{Model: "test-model"},
		result: nil,
	}
	adapter := AdaptReviewModelClient(mc)
	adr := &ADR{ID: NewADRID(), Title: "Test"}
	_, err := adapter.ReviewADR(context.Background(), adr)
	if err == nil {
		t.Fatal("expected error for nil result")
	}
}

func TestAdaptReviewModelClient_ReviewADR_ScoreClampedToZero(t *testing.T) {
	mc := &mockReviewModelClient{
		info: review.ModelInfo{Model: "test-model"},
		result: &review.ModelReviewResult{
			Verdict: "approved",
			Findings: []review.Finding{
				{Description: "c1", Severity: "critical"},
				{Description: "c2", Severity: "critical"},
				{Description: "c3", Severity: "critical"},
				{Description: "c4", Severity: "critical"},
				{Description: "c5", Severity: "critical"},
				{Description: "c6", Severity: "critical"},
				{Description: "c7", Severity: "critical"},
				{Description: "c8", Severity: "critical"},
				{Description: "c9", Severity: "critical"},
				{Description: "c10", Severity: "critical"},
				{Description: "c11", Severity: "critical"},
			},
		},
	}
	adapter := AdaptReviewModelClient(mc)
	adr := &ADR{ID: NewADRID(), Title: "Test"}
	verdict, err := adapter.ReviewADR(context.Background(), adr)
	if err != nil {
		t.Fatalf("ReviewADR: %v", err)
	}
	if verdict.Score != 0 {
		t.Fatalf("expected clamped 0, got %f", verdict.Score)
	}
}

func TestOfflineADRClient_NilADR(t *testing.T) {
	c := newOfflineADRClient("architecture-consistency")
	_, err := c.ReviewADR(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil ADR")
	}
}

func TestOfflineADRClient_Name(t *testing.T) {
	c := newOfflineADRClient("security-surface")
	if c.Name() != "security-surface" {
		t.Fatalf("expected security-surface, got %s", c.Name())
	}
}

func TestReviewArchitecture_CompleteADR(t *testing.T) {
	a := &ADR{
		Title:        "Use PostgreSQL",
		Context:      "Need ACID transactions",
		Decision:     "Adopt PostgreSQL",
		Consequences: "Migration effort required",
		Alternatives: []Alternative{
			{Description: "PostgreSQL", Tradeoffs: "Mature"},
			{Description: "MongoDB", Tradeoffs: "Flexible", RejectedBecause: "Weaker transactions"},
		},
		EvidenceLinks: []EvidenceLink{{Type: EvidenceSpecRef, SpecRef: "spec-1"}},
	}
	v := reviewArchitecture("arch-model", a)
	if v.Model != "arch-model" {
		t.Fatalf("model: %s", v.Model)
	}
	if v.Verdict != "approve" {
		t.Fatalf("expected approve for complete ADR, got %s (score %f)", v.Verdict, v.Score)
	}
	if len(v.Concerns) != 0 {
		t.Fatalf("expected no concerns for complete ADR, got %v", v.Concerns)
	}
}

func TestReviewArchitecture_EmptyADR(t *testing.T) {
	a := &ADR{Title: "Test"}
	v := reviewArchitecture("arch-model", a)
	// Empty decision, context, consequences, no alternatives, no evidence
	// 0.85 - 0.4 - 0.15 - 0.2 - 0.25 - 0.1 = -0.25 → clamped to 0
	if v.Score != 0 {
		t.Fatalf("expected score 0, got %f", v.Score)
	}
	if v.Verdict != "reject" {
		t.Fatalf("expected reject, got %s", v.Verdict)
	}
}

func TestReviewArchitecture_TwoAltsNoRejected(t *testing.T) {
	a := &ADR{
		Title:        "Test",
		Context:      "ctx",
		Decision:     "decide",
		Consequences: "cons",
		Alternatives: []Alternative{
			{Description: "A", Tradeoffs: "t"},
			{Description: "B", Tradeoffs: "t"},
		},
		EvidenceLinks: []EvidenceLink{{Type: EvidenceSpecRef, SpecRef: "x"}},
	}
	v := reviewArchitecture("arch-model", a)
	// 0.85 - 0.05 (2 alts, 0 rejected) = ~0.80
	if v.Score < 0.79 || v.Score > 0.81 {
		t.Fatalf("expected ~0.8, got %f", v.Score)
	}
	found := false
	for _, s := range v.Suggestions {
		if strings.Contains(s, "rejected") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected suggestion about rejected alternatives: %v", v.Suggestions)
	}
}

func TestReviewSecurity_Variants(t *testing.T) {
	// Security-sensitive with controls
	a1 := &ADR{
		Title:        "OAuth auth with TLS and RBAC",
		Context:      "Need auth",
		Decision:     "OIDC with RBAC and audit logging",
		Consequences: "Audit trail for auth decisions",
		Alternatives: []Alternative{{Description: "OAuth", Tradeoffs: "standard"}},
		EvidenceLinks: []EvidenceLink{{Type: EvidenceSpecRef, SpecRef: "x"}},
	}
	v1 := reviewSecurity("sec-model", a1)
	if v1.Score < 0.8 {
		t.Fatalf("expected high score for security with controls, got %f", v1.Score)
	}

	// Security-sensitive WITHOUT controls
	a2 := &ADR{
		Title:    "Store passwords in plaintext",
		Context:  "Need auth without crypto",
		Decision: "Plaintext password storage",
		Alternatives: []Alternative{{Description: "A"}},
		EvidenceLinks: []EvidenceLink{{Type: EvidenceSpecRef, SpecRef: "x"}},
	}
	v2 := reviewSecurity("sec-model", a2)
	if v2.Score >= 0.8 {
		t.Fatalf("expected lower score for security without controls, got %f", v2.Score)
	}
	if len(v2.Concerns) == 0 {
		t.Fatal("expected concerns for security without controls")
	}

	// External API without rate-limit/auth
	a3 := &ADR{
		Title:    "Public external API",
		Context:  "Internet-facing service",
		Decision: "Expose on the internet",
		Alternatives: []Alternative{{Description: "A"}},
		EvidenceLinks: []EvidenceLink{{Type: EvidenceSpecRef, SpecRef: "x"}},
	}
	v3 := reviewSecurity("sec-model", a3)
	found := false
	for _, c := range v3.Concerns {
		if strings.Contains(strings.ToLower(c), "rate-limit") || strings.Contains(strings.ToLower(c), "externally") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected rate-limit/external concern: %v", v3.Concerns)
	}

	// No alternatives
	a4 := &ADR{
		Title:        "Some decision",
		Context:      "ctx",
		Decision:     "decide",
		Consequences: "cons",
		EvidenceLinks: []EvidenceLink{{Type: EvidenceSpecRef, SpecRef: "x"}},
	}
	v4 := reviewSecurity("sec-model", a4)
	found = false
	for _, c := range v4.Concerns {
		if strings.Contains(strings.ToLower(c), "no alternatives") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected no-alternatives concern: %v", v4.Concerns)
	}
}

func TestReviewPerformance_Variants(t *testing.T) {
	// Event sourcing with SLOs
	a1 := &ADR{
		Title:        "Event sourcing with retention",
		Context:      "Need consumer lag budgets",
		Decision:     "Use Kafka with backpressure strategy",
		Consequences: "Throughput SLOs defined",
		Alternatives: []Alternative{{Description: "A"}},
	}
	v1 := reviewPerformance("perf-model", a1)
	if v1.Score < 0.82 {
		t.Fatalf("expected base score for perf with SLOs, got %f", v1.Score)
	}

	// Event sourcing WITHOUT SLOs
	a2 := &ADR{
		Title:    "Kafka event stream queue",
		Context:  "Fast message delivery",
		Decision: "Just use it",
	}
	v2 := reviewPerformance("perf-model", a2)
	if v2.Score >= 0.82 {
		t.Fatalf("expected lower score for async without SLOs, got %f", v2.Score)
	}
	found := false
	for _, c := range v2.Concerns {
		if strings.Contains(strings.ToLower(c), "async") || strings.Contains(strings.ToLower(c), "slo") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected SLO concern: %v", v2.Concerns)
	}

	// Cache without TTL
	a3 := &ADR{
		Title:    "Redis cache",
		Context:  "Just cache",
		Decision: "Cache everything",
	}
	v3 := reviewPerformance("perf-model", a3)
	found = false
	for _, c := range v3.Concerns {
		if strings.Contains(strings.ToLower(c), "ttl") || strings.Contains(strings.ToLower(c), "invalidat") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected TTL concern: %v", v3.Concerns)
	}

	// Migration without rollback
	a4 := &ADR{
		Title:    "Database migration rewrite",
		Context:  "Big rewrite",
		Decision: "Rewrite everything",
	}
	v4 := reviewPerformance("perf-model", a4)
	found = false
	for _, c := range v4.Concerns {
		if strings.Contains(strings.ToLower(c), "rollback") || strings.Contains(strings.ToLower(c), "migration") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected rollback concern: %v", v4.Concerns)
	}

	// Empty consequences
	a5 := &ADR{Title: "Test", Context: "ctx", Decision: "decide"}
	v5 := reviewPerformance("perf-model", a5)
	found = false
	for _, c := range v5.Concerns {
		if strings.Contains(strings.ToLower(c), "operational consequences") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected operational consequences concern: %v", v5.Concerns)
	}
}

func TestVerdictFromScore(t *testing.T) {
	tests := []struct {
		score      float64
		wantVerdict string
	}{
		{0.9, "approve"},
		{0.7, "warn"},
		{0.3, "reject"},
		{-0.5, "reject"}, // clamped to 0 → reject
		{1.5, "approve"}, // clamped to 1 → approve
		{0.5, "warn"}, // exactly 0.5 → warn (below 0.75 threshold)
		{0.49, "reject"},  // just under 0.5 → reject
		{0.74, "warn"},    // just under 0.75 → warn
	}
	for _, tt := range tests {
		v := verdictFromScore("model", tt.score, nil, nil, "")
		if v.Verdict != tt.wantVerdict {
			t.Errorf("score %f: got %s want %s", tt.score, v.Verdict, tt.wantVerdict)
		}
	}
	// Empty rationale should get default
	v := verdictFromScore("model", 0.9, nil, nil, "")
	if !strings.Contains(v.Rationale, "Offline structural review") {
		t.Fatalf("expected default rationale: %q", v.Rationale)
	}
	// Concerns appended to rationale
	v2 := verdictFromScore("model", 0.5, []string{"issue1"}, nil, "custom rationale")
	if !strings.Contains(v2.Rationale, "concerns:") {
		t.Fatalf("expected concerns in rationale: %q", v2.Rationale)
	}
}

func TestComputeConsensus(t *testing.T) {
	// All approve
	allApprove := []ModelVerdict{
		{Model: "a", Verdict: "approve", Score: 0.9},
		{Model: "b", Verdict: "approve", Score: 0.85},
	}
	c1 := computeConsensus(allApprove)
	if c1 < 0.75 {
		t.Fatalf("expected consensus >= 0.75 for all approve, got %f", c1)
	}

	// Mixed approve + reject
	mixed := []ModelVerdict{
		{Model: "a", Verdict: "approve", Score: 0.9},
		{Model: "b", Verdict: "reject", Score: 0.2},
	}
	c2 := computeConsensus(mixed)
	// approve: s=0.9, reject: s=0.2 (since < 0.49, stays 0.2)
	// avg = (0.9 + 0.2) / 2 = 0.55
	if c2 < 0.5 || c2 > 0.6 {
		t.Fatalf("expected consensus ~0.55 for mixed, got %f", c2)
	}

	// All reject
	allReject := []ModelVerdict{
		{Model: "a", Verdict: "reject", Score: 0.8}, // s clamped to 0.4
		{Model: "b", Verdict: "block", Score: 0.1},  // s stays 0.1
	}
	c3 := computeConsensus(allReject)
	// (0.4 + 0.1) / 2 = 0.25
	if c3 > 0.3 {
		t.Fatalf("expected low consensus for all reject, got %f", c3)
	}

	// Warn with low score gets bumped up
	warn := []ModelVerdict{
		{Model: "a", Verdict: "warn", Score: 0.2},
	}
	c4 := computeConsensus(warn)
	// warn with score < 0.5 → s = 0.55
	if c4 != 0.55 {
		t.Fatalf("expected 0.55, got %f", c4)
	}

	// Warn with high score gets pushed down
	warn2 := []ModelVerdict{
		{Model: "a", Verdict: "warn", Score: 0.9},
	}
	c5 := computeConsensus(warn2)
	// warn with score > 0.74 → s = 0.7
	if c5 != 0.7 {
		t.Fatalf("expected 0.7, got %f", c5)
	}

	// Empty
	if computeConsensus(nil) != 0 {
		t.Fatal("expected 0 for empty")
	}

	// Pass verdict treated like approve
	passVerdict := []ModelVerdict{
		{Model: "a", Verdict: "pass", Score: 0.5}, // bumped to 0.75
	}
	c6 := computeConsensus(passVerdict)
	if c6 != 0.75 {
		t.Fatalf("expected 0.75 for pass with low score, got %f", c6)
	}
}

func TestDetectConflicts(t *testing.T) {
	// No conflict — all same class
	sameClass := []ModelVerdict{
		{Model: "a", Verdict: "approve", Score: 0.9, Rationale: "good"},
		{Model: "b", Verdict: "approve", Score: 0.85, Rationale: "good"},
	}
	if detectConflicts(sameClass) != nil {
		t.Fatal("expected nil for same class")
	}

	// Single verdict — no conflict
	single := []ModelVerdict{{Model: "a", Verdict: "approve", Score: 0.9}}
	if detectConflicts(single) != nil {
		t.Fatal("expected nil for single verdict")
	}

	// Empty — no conflict
	if detectConflicts(nil) != nil {
		t.Fatal("expected nil for empty")
	}

	// Conflict — approve vs reject
	conflicting := []ModelVerdict{
		{Model: "a", Verdict: "approve", Score: 0.9, Rationale: "fine"},
		{Model: "b", Verdict: "reject", Score: 0.2, Rationale: "bad"},
	}
	conflicts := detectConflicts(conflicting)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].Topic != "overall-verdict" {
		t.Fatalf("expected overall-verdict topic, got %q", conflicts[0].Topic)
	}
	if len(conflicts[0].Positions) != 2 {
		t.Fatalf("expected 2 positions, got %d", len(conflicts[0].Positions))
	}
}

func TestNormalizeVerdictClass(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"approve", "approve"},
		{"approved", "approve"},
		{"pass", "approve"},
		{"warn", "warn"},
		{"pass_with_notes", "warn"},
		{"reject", "reject"},
		{"block", "reject"},
		{"unknown_verdict", "unknown_verdict"},
		{"  APPROVE  ", "approve"},
	}
	for _, tt := range tests {
		got := normalizeVerdictClass(tt.input)
		if got != tt.want {
			t.Errorf("normalizeVerdictClass(%q): got %q want %q", tt.input, got, tt.want)
		}
	}
}

func TestCollectSuggestions(t *testing.T) {
	verdicts := []ModelVerdict{
		{Model: "a", Suggestions: []string{"fix X", "add Y"}},
		{Model: "b", Suggestions: []string{"fix X", "add Z"}},      // "fix X" is dup
		{Model: "c", Suggestions: []string{"", "  ", "add W"}},    // empty trimmed
	}
	suggestions := collectSuggestions(verdicts)
	if len(suggestions) != 4 {
		t.Fatalf("expected 4 unique suggestions, got %d: %v", len(suggestions), suggestions)
	}
	// Verify no duplicates
	seen := map[string]bool{}
	for _, s := range suggestions {
		if seen[s] {
			t.Fatalf("duplicate suggestion: %s", s)
		}
		seen[s] = true
	}
	// Empty verdicts
	if collectSuggestions(nil) != nil {
		t.Fatal("expected nil for empty")
	}
}

func TestFormatADRAsReviewPayload(t *testing.T) {
	a := &ADR{
		ID:       NewADRID(),
		Title:    "Test ADR",
		Status:   StatusProposed,
		Context:  "The context",
		Decision: "The decision",
		Alternatives: []Alternative{
			{Description: "Alt 1", Tradeoffs: "T1", RejectedBecause: "R1"},
		},
		Consequences: "The consequences",
	}
	payload := formatADRAsReviewPayload(a)
	if !strings.Contains(payload, "Test ADR") {
		t.Fatal("expected title in payload")
	}
	if !strings.Contains(payload, StatusProposed) {
		t.Fatal("expected status in payload")
	}
	if !strings.Contains(payload, "The context") {
		t.Fatal("expected context in payload")
	}
	if !strings.Contains(payload, "The decision") {
		t.Fatal("expected decision in payload")
	}
	if !strings.Contains(payload, "Alt 1") {
		t.Fatal("expected alternative in payload")
	}
	if !strings.Contains(payload, "T1") {
		t.Fatal("expected tradeoffs in payload")
	}
	if !strings.Contains(payload, "R1") {
		t.Fatal("expected rejected because in payload")
	}
	if !strings.Contains(payload, "The consequences") {
		t.Fatal("expected consequences in payload")
	}
}

func TestReview_FullWithAdaptedClients(t *testing.T) {
	mc1 := &mockReviewModelClient{
		info: review.ModelInfo{Model: "llm-arch"},
		result: &review.ModelReviewResult{
			Verdict:  "approved",
			Findings: []review.Finding{},
		},
	}
	mc2 := &mockReviewModelClient{
		info: review.ModelInfo{Model: "llm-sec"},
		result: &review.ModelReviewResult{
			Verdict:  "pass_with_notes",
			Findings: []review.Finding{{Description: "sec issue", Severity: "medium"}},
		},
	}
	reviewer := NewADRReviewer().WithReviewModelClients(mc1, mc2)
	res, err := reviewer.Review(context.Background(), &ADRReviewRequest{
		ADR: ADR{
			ID:       NewADRID(),
			Title:    "Test",
			Status:   StatusProposed,
			Context:  "ctx",
			Decision: "decide",
		},
		ConsensusThreshold: 0.6,
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(res.ModelVerdicts) != 2 {
		t.Fatalf("expected 2 verdicts, got %d", len(res.ModelVerdicts))
	}
	if !res.Passed {
		t.Fatalf("expected pass with threshold 0.6, consensus %f", res.ConsensusScore)
	}
}

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockADRClient struct {
	name       string
	verdict    *ModelVerdict
	err        error
	nilVerdict bool
}

func (m *mockADRClient) Name() string { return m.name }
func (m *mockADRClient) ReviewADR(_ context.Context, _ *ADR) (*ModelVerdict, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.nilVerdict {
		return nil, nil
	}
	return m.verdict, nil
}

type mockReviewModelClient struct {
	info   review.ModelInfo
	result *review.ModelReviewResult
	err    error
}

func (m *mockReviewModelClient) Review(_ context.Context, _ review.ReviewRequest) (*review.ModelReviewResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func (m *mockReviewModelClient) Info() review.ModelInfo { return m.info }
