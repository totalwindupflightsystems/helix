package adr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
