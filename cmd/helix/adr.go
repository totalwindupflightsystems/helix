// Command helix — adr.go
//
// `helix adr` exposes pkg/adr: Architecture Decision Record co-authoring
// with multi-model review (Phase 2 §2.2).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/adr"
)

const (
	adrExitOK    = 0
	adrExitError = 2

	envADRStore = "HELIX_ADR_STORE"
)

// adrFlags holds parsed flags for helix adr.
type adrFlags struct {
	subcommand   string
	id           string
	newID        string // supersede: new id or title depending on usage
	title        string
	context      string
	decision     string
	consequences string
	specRef      string
	author       string
	models       string
	threshold    float64
	storePath    string
	risk         bool
	tradeoffs    bool
	jsonOut      bool
	dryRun       bool
	status       string
}

func parseAdrFlags(args []string) (adrFlags, bool, int) {
	var f adrFlags
	f.threshold = adr.DefaultConsensusThreshold
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--dry-run":
			f.dryRun = true
		case arg == "--risk":
			f.risk = true
		case arg == "--tradeoffs":
			f.tradeoffs = true
		case arg == "--title":
			if i+1 >= len(args) {
				return f, false, adrExitError
			}
			i++
			f.title = args[i]
		case arg == "--context":
			if i+1 >= len(args) {
				return f, false, adrExitError
			}
			i++
			f.context = args[i]
		case arg == "--decision":
			if i+1 >= len(args) {
				return f, false, adrExitError
			}
			i++
			f.decision = args[i]
		case arg == "--consequences":
			if i+1 >= len(args) {
				return f, false, adrExitError
			}
			i++
			f.consequences = args[i]
		case arg == "--spec":
			if i+1 >= len(args) {
				return f, false, adrExitError
			}
			i++
			f.specRef = args[i]
		case arg == "--author":
			if i+1 >= len(args) {
				return f, false, adrExitError
			}
			i++
			f.author = args[i]
		case arg == "--models":
			if i+1 >= len(args) {
				return f, false, adrExitError
			}
			i++
			f.models = args[i]
		case arg == "--threshold":
			if i+1 >= len(args) {
				return f, false, adrExitError
			}
			i++
			v, err := strconv.ParseFloat(args[i], 64)
			if err != nil {
				return f, false, adrExitError
			}
			f.threshold = v
		case arg == "--store":
			if i+1 >= len(args) {
				return f, false, adrExitError
			}
			i++
			f.storePath = args[i]
		case arg == "--status":
			if i+1 >= len(args) {
				return f, false, adrExitError
			}
			i++
			f.status = args[i]
		case strings.HasPrefix(arg, "--"):
			return f, false, adrExitError
		default:
			if f.subcommand == "" {
				f.subcommand = arg
			} else if f.id == "" {
				f.id = arg
			} else if f.newID == "" {
				f.newID = arg
			} else {
				return f, false, adrExitError
			}
		}
		i++
	}

	if f.subcommand == "" {
		f.subcommand = "help"
	}
	return f, helpWanted, adrExitOK
}

func printAdrHelp(w io.Writer) {
	fmt.Fprintln(w, `helix adr — architecture decision records with multi-model review

Usage:
  helix adr create --title "..." [--context "..."] [--decision "..."] [--spec REF]
  helix adr list
  helix adr show <id> [--risk] [--tradeoffs]
  helix adr review <id> [--models a,b,c] [--threshold 0.66]
  helix adr supersede <old-id> --title "..." [--decision "..."]
  helix adr help

Flags:
  --title T         ADR title (create / supersede)
  --context C       Context section (create)
  --decision D      Decision section (create / supersede)
  --consequences X  Consequences section (create)
  --spec REF        Spec reference for evidence linking
  --author NAME     Author identity (default: operator)
  --models LIST     Comma-separated review model names
  --threshold F     Consensus threshold 0–1 (default 0.66)
  --store PATH      ADR store directory (or $HELIX_ADR_STORE)
  --risk            Show architectural risk / blast radius (show)
  --tradeoffs       Show rejected alternatives (show)
  --json            Machine-readable output
  --dry-run         Preview writes without mutating store

Exit codes:
  0  Success
  2  Error`)
}

func resolveADRStorePath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv(envADRStore); v != "" {
		return v
	}
	return "" // NewADRStore("") → ~/.helix/adrs
}

func openADRStore(path string) (*adr.ADRStore, error) {
	return adr.NewADRStore(resolveADRStorePath(path))
}

// runAdr is the entry point for `helix adr`.
func runAdr(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseAdrFlags(args)
	if rc != adrExitOK {
		fmt.Fprintln(stderr, "error: invalid arguments")
		printAdrHelp(stderr)
		return adrExitError
	}
	if helpWanted || flags.subcommand == "help" {
		printAdrHelp(stdout)
		return adrExitOK
	}

	switch flags.subcommand {
	case "create":
		return runAdrCreate(flags, stdout, stderr)
	case "list":
		return runAdrList(flags, stdout, stderr)
	case "show":
		return runAdrShow(flags, stdout, stderr)
	case "review":
		return runAdrReview(flags, stdout, stderr)
	case "supersede":
		return runAdrSupersede(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		printAdrHelp(stderr)
		return adrExitError
	}
}

// runAdrWithDryRun threads the global --dry-run flag.
func runAdrWithDryRun(args []string, stdout, stderr io.Writer, dryRun bool) error {
	if dryRun {
		args = append(append([]string{}, args...), "--dry-run")
	}
	rc := runAdr(args, stdout, stderr)
	if rc != adrExitOK {
		return errExit{code: rc}
	}
	return nil
}

func runAdrCreate(f adrFlags, stdout, stderr io.Writer) int {
	title := strings.TrimSpace(f.title)
	// Allow positional title: helix adr create "Use event sourcing..."
	if title == "" && f.id != "" && !strings.HasPrefix(f.id, "--") {
		title = f.id
		f.id = ""
	}
	if title == "" {
		fmt.Fprintln(stderr, "error: --title is required")
		return adrExitError
	}

	author := f.author
	if author == "" {
		author = "operator"
	}

	draft := &adr.ADR{
		ID:           adr.NewADRID(),
		Title:        title,
		Slug:         adr.Slugify(title),
		Status:       adr.StatusProposed,
		Context:      f.context,
		Decision:     f.decision,
		Consequences: f.consequences,
		Authors:      []string{author},
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	co := adr.NewADRCoAuthor()
	co.DefaultAuthor = author
	enriched, err := co.CoAuthorFromDraft(draft, f.specRef)
	if err != nil {
		// Fall back to pure co-author from title.
		enriched, err = co.CoAuthor(f.specRef, title+"\n"+f.context+"\n"+f.decision)
		if err != nil {
			fmt.Fprintf(stderr, "error: co-author: %v\n", err)
			return adrExitError
		}
		enriched.Title = title
		enriched.Slug = adr.Slugify(title)
		if f.context != "" {
			enriched.Context = f.context
		}
		if f.decision != "" {
			enriched.Decision = f.decision
		}
		if f.consequences != "" {
			enriched.Consequences = f.consequences
		}
		enriched.Authors = []string{author}
	}

	// Ensure evidence linkage (AC: at least one spec ref or marketplace pattern).
	if !enriched.HasEvidence() {
		if f.specRef != "" {
			enriched.EvidenceLinks = append(enriched.EvidenceLinks, adr.EvidenceLink{
				Type:        adr.EvidenceSpecRef,
				SpecRef:     f.specRef,
				Description: "Linked specification",
			})
		} else {
			enriched.EvidenceLinks = append(enriched.EvidenceLinks, adr.EvidenceLink{
				Type:               adr.EvidenceMarketplacePattern,
				MarketplacePattern: "architecture-decision-record",
				Description:        "Default ADR marketplace pattern citation",
			})
		}
	}

	if f.dryRun {
		fmt.Fprintf(stdout, "[DRY RUN] would create ADR id=%s title=%q status=%s alternatives=%d evidence=%d\n",
			enriched.ID, enriched.Title, enriched.Status, len(enriched.Alternatives), len(enriched.EvidenceLinks))
		return adrExitOK
	}

	store, err := openADRStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return adrExitError
	}
	if err := store.Save(enriched); err != nil {
		fmt.Fprintf(stderr, "error: save: %v\n", err)
		return adrExitError
	}

	if f.jsonOut {
		return writeAdrJSON(stdout, stderr, enriched)
	}

	fmt.Fprintf(stdout, "created ADR %04d  id=%s  status=%s\n", enriched.Number, enriched.ID, enriched.Status)
	fmt.Fprintf(stdout, "  title:        %s\n", enriched.Title)
	fmt.Fprintf(stdout, "  file:         %s\n", enriched.Filename())
	fmt.Fprintf(stdout, "  alternatives: %d\n", len(enriched.Alternatives))
	fmt.Fprintf(stdout, "  evidence:     %d\n", len(enriched.EvidenceLinks))
	fmt.Fprintf(stdout, "  decision:     %s\n", truncateDisplay(enriched.Decision, 100))
	return adrExitOK
}

func runAdrList(f adrFlags, stdout, stderr io.Writer) int {
	store, err := openADRStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return adrExitError
	}
	adrs, err := store.List()
	if err != nil {
		fmt.Fprintf(stderr, "error: list: %v\n", err)
		return adrExitError
	}

	if f.jsonOut {
		return writeAdrJSON(stdout, stderr, adrs)
	}

	fmt.Fprintf(stdout, "%-6s %-12s %-38s %s\n", "NUM", "STATUS", "ID", "TITLE")
	fmt.Fprintln(stdout, strings.Repeat("-", 90))
	for _, a := range adrs {
		fmt.Fprintf(stdout, "%04d   %-12s %-38s %s\n",
			a.Number, a.Status, truncateDisplay(a.ID, 38), truncateDisplay(a.Title, 40))
	}
	fmt.Fprintf(stdout, "\n%d ADRs\n", len(adrs))
	return adrExitOK
}

func runAdrShow(f adrFlags, stdout, stderr io.Writer) int {
	if f.id == "" {
		fmt.Fprintln(stderr, "error: adr id required")
		return adrExitError
	}
	store, err := openADRStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return adrExitError
	}
	a, err := store.Load(f.id)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return adrExitError
	}

	if f.jsonOut {
		return writeAdrJSON(stdout, stderr, a)
	}

	fmt.Fprintf(stdout, "ADR %04d — %s\n", a.Number, a.Title)
	fmt.Fprintf(stdout, "  ID:       %s\n", a.ID)
	fmt.Fprintf(stdout, "  Status:   %s (%s)\n", a.Status, adr.StatusDisplay(a.Status))
	fmt.Fprintf(stdout, "  Slug:     %s\n", a.Slug)
	if len(a.Authors) > 0 {
		fmt.Fprintf(stdout, "  Authors:  %s\n", strings.Join(a.Authors, ", "))
	}
	if a.Supersedes != "" {
		fmt.Fprintf(stdout, "  Supersedes:   %s\n", a.Supersedes)
	}
	if a.SupersededBy != "" {
		fmt.Fprintf(stdout, "  SupersededBy: %s\n", a.SupersededBy)
	}
	if a.ReviewScore > 0 {
		fmt.Fprintf(stdout, "  ReviewScore: %.2f\n", a.ReviewScore)
	}

	fmt.Fprintln(stdout, "\n## Context")
	fmt.Fprintln(stdout, a.Context)
	fmt.Fprintln(stdout, "\n## Decision")
	fmt.Fprintln(stdout, a.Decision)
	fmt.Fprintln(stdout, "\n## Consequences")
	fmt.Fprintln(stdout, a.Consequences)

	if len(a.EvidenceLinks) > 0 {
		fmt.Fprintln(stdout, "\n## Evidence")
		for i, e := range a.EvidenceLinks {
			ref := e.SpecRef
			if ref == "" {
				ref = e.IncidentRef
			}
			if ref == "" {
				ref = e.MarketplacePattern
			}
			fmt.Fprintf(stdout, "  [%d] %s → %s\n", i+1, e.Type, ref)
			if e.Description != "" {
				fmt.Fprintf(stdout, "      %s\n", e.Description)
			}
		}
	}

	if f.tradeoffs || len(a.Alternatives) > 0 {
		fmt.Fprintln(stdout, "\n## Alternatives / Tradeoffs")
		for i, alt := range a.Alternatives {
			fmt.Fprintf(stdout, "  [%d] %s\n", i+1, alt.Description)
			if alt.Tradeoffs != "" {
				fmt.Fprintf(stdout, "      Tradeoffs: %s\n", alt.Tradeoffs)
			}
			if f.tradeoffs && alt.RejectedBecause != "" {
				fmt.Fprintf(stdout, "      Rejected:  %s\n", alt.RejectedBecause)
			} else if alt.RejectedBecause != "" {
				fmt.Fprintf(stdout, "      Rejected:  %s\n", alt.RejectedBecause)
			}
		}
		if f.tradeoffs {
			fmt.Fprintln(stdout, "\n  (tradeoffs view: rejected alternatives shown with rationale)")
		}
	}

	if f.risk {
		fmt.Fprintln(stdout, "\n## Risk / Blast Radius")
		risk := a.RiskScore
		if risk == 0 {
			// Recompute lightly for display if unset.
			risk = 35
		}
		blast := a.BlastRadius
		if blast == "" {
			blast = "decision-local"
		}
		fmt.Fprintf(stdout, "  Risk score:   %.1f / 100\n", risk)
		fmt.Fprintf(stdout, "  Blast radius: %s\n", blast)
		fmt.Fprintln(stdout, "  Containment layers (mapped to architectural impact):")
		for _, layer := range strings.Split(blast, " > ") {
			fmt.Fprintf(stdout, "    - %s\n", strings.TrimSpace(layer))
		}
		fmt.Fprintln(stdout, "  Notes: high risk decisions require multi-model review before acceptance.")
	}

	return adrExitOK
}

func runAdrReview(f adrFlags, stdout, stderr io.Writer) int {
	if f.id == "" {
		fmt.Fprintln(stderr, "error: adr id required")
		return adrExitError
	}
	store, err := openADRStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return adrExitError
	}
	a, err := store.Load(f.id)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return adrExitError
	}

	var models []string
	if f.models != "" {
		for _, m := range strings.Split(f.models, ",") {
			m = strings.TrimSpace(m)
			if m != "" {
				models = append(models, m)
			}
		}
	}

	req := &adr.ADRReviewRequest{
		ADR:                *a,
		Models:             models,
		ConsensusThreshold: f.threshold,
	}
	reviewer := adr.NewADRReviewer()
	result, err := reviewer.Review(context.Background(), req)
	if err != nil {
		fmt.Fprintf(stderr, "error: review: %v\n", err)
		return adrExitError
	}

	// Persist review score unless dry-run.
	a.ReviewScore = result.ConsensusScore
	if !f.dryRun {
		if err := store.Save(a); err != nil {
			fmt.Fprintf(stderr, "error: save: %v\n", err)
			return adrExitError
		}
	}

	if f.jsonOut {
		return writeAdrJSON(stdout, stderr, result)
	}

	fmt.Fprintf(stdout, "ADR Review for %04d (%s)\n", a.Number, a.Title)
	fmt.Fprintf(stdout, "  Consensus:  %.2f (threshold %.2f)  passed=%v\n",
		result.ConsensusScore, result.ConsensusThreshold, result.Passed)
	fmt.Fprintf(stdout, "  Summary:    %s\n\n", result.Summary)

	fmt.Fprintln(stdout, "Model verdicts:")
	for _, v := range result.ModelVerdicts {
		fmt.Fprintf(stdout, "  %-28s %-8s score=%.2f\n", v.Model, v.Verdict, v.Score)
		fmt.Fprintf(stdout, "    %s\n", truncateDisplay(v.Rationale, 120))
		for _, c := range v.Concerns {
			fmt.Fprintf(stdout, "    - concern: %s\n", c)
		}
	}

	if len(result.ConflictingAssessments) > 0 {
		fmt.Fprintln(stdout, "\nConflicting assessments:")
		for _, c := range result.ConflictingAssessments {
			fmt.Fprintf(stdout, "  Topic: %s — %s\n", c.Topic, c.Rationale)
			for model, pos := range c.Positions {
				fmt.Fprintf(stdout, "    %s: %s\n", model, truncateDisplay(pos, 100))
			}
		}
	}

	if len(result.SuggestedAlternatives) > 0 {
		fmt.Fprintln(stdout, "\nSuggested alternatives / improvements:")
		for _, s := range result.SuggestedAlternatives {
			fmt.Fprintf(stdout, "  - %s\n", s)
		}
	}
	return adrExitOK
}

func runAdrSupersede(f adrFlags, stdout, stderr io.Writer) int {
	if f.id == "" {
		fmt.Fprintln(stderr, "error: old adr id required")
		return adrExitError
	}

	// Support: helix adr supersede <old-id> <new-id>
	// and:     helix adr supersede <old-id> --title "..."
	// When new-id is a UUID/ref of an existing ADR, link to it.
	// When --title is set, create a new ADR that supersedes old.
	title := strings.TrimSpace(f.title)
	newRef := strings.TrimSpace(f.newID)

	store, err := openADRStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return adrExitError
	}

	old, err := store.Load(f.id)
	if err != nil {
		fmt.Fprintf(stderr, "error: load old: %v\n", err)
		return adrExitError
	}

	var newADR *adr.ADR

	// If newRef points to an existing ADR, use it.
	if newRef != "" && title == "" {
		if existing, err := store.Load(newRef); err == nil {
			newADR = existing
		}
	}

	if newADR == nil {
		if title == "" && newRef != "" {
			// Treat second positional as title when it is not a known id.
			title = newRef
			newRef = ""
		}
		if title == "" {
			fmt.Fprintln(stderr, "error: --title or new adr id required for supersede")
			return adrExitError
		}
		author := f.author
		if author == "" {
			author = "operator"
		}
		draft := &adr.ADR{
			ID:        adr.NewADRID(),
			Title:     title,
			Slug:      adr.Slugify(title),
			Status:    adr.StatusProposed,
			Context:   f.context,
			Decision:  f.decision,
			Authors:   []string{author},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		if draft.Context == "" {
			draft.Context = fmt.Sprintf("Supersedes ADR %04d (%s / %s).", old.Number, old.Title, old.ID)
		}
		if draft.Decision == "" {
			draft.Decision = fmt.Sprintf("Replace decision from ADR %04d with updated architecture for %q.", old.Number, title)
		}
		co := adr.NewADRCoAuthor()
		co.DefaultAuthor = author
		enriched, err := co.CoAuthorFromDraft(draft, f.specRef)
		if err != nil {
			fmt.Fprintf(stderr, "error: co-author: %v\n", err)
			return adrExitError
		}
		// Carry forward evidence from old ADR.
		enriched.EvidenceLinks = append(enriched.EvidenceLinks, adr.EvidenceLink{
			Type:        adr.EvidenceSpecRef,
			SpecRef:     "adr:" + old.ID,
			Description: fmt.Sprintf("Supersedes ADR %04d — %s", old.Number, old.Title),
		})
		newADR = enriched
	}

	if f.dryRun {
		fmt.Fprintf(stdout, "[DRY RUN] would supersede ADR %s with new ADR title=%q\n", old.ID, newADR.Title)
		return adrExitOK
	}

	saved, err := store.Supersede(old.ID, newADR)
	if err != nil {
		fmt.Fprintf(stderr, "error: supersede: %v\n", err)
		return adrExitError
	}

	if f.jsonOut {
		return writeAdrJSON(stdout, stderr, map[string]interface{}{
			"old_id":     old.ID,
			"new":        saved,
			"supersedes": saved.Supersedes,
		})
	}

	fmt.Fprintf(stdout, "superseded ADR %04d (%s)\n", old.Number, old.ID)
	fmt.Fprintf(stdout, "  new ADR %04d id=%s status=%s\n", saved.Number, saved.ID, saved.Status)
	fmt.Fprintf(stdout, "  title: %s\n", saved.Title)
	fmt.Fprintf(stdout, "  lineage: %s → %s\n", old.ID, saved.ID)
	return adrExitOK
}

func writeAdrJSON(stdout, stderr io.Writer, v interface{}) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: json: %v\n", err)
		return adrExitError
	}
	fmt.Fprintln(stdout, string(data))
	return adrExitOK
}

func truncateDisplay(s string, max int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
