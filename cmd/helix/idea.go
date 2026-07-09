// Command helix — idea.go
//
// `helix idea` exposes pkg/ideation (Phase 1 §1.1–1.3): capture, list, show,
// validate, prioritize, promote, close, advocate.
//
// Offline-capable: validation uses deterministic concept agents (no Chimera).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/totalwindupflightsystems/helix/pkg/ideation"
)

const (
	ideaExitOK    = 0
	ideaExitError = 2

	envIdeaStore = "HELIX_IDEA_STORE"
)

// ideaFlags holds parsed flags for helix idea.
type ideaFlags struct {
	subcommand string
	id         string
	title      string
	body       string
	tags       []string
	source     string
	agent      string
	model      string
	evidence   []ideation.EvidenceRef
	storePath  string
	specDir    string
	to         string
	reason     string
	position   string
	jsonOut    bool
	riskDetail bool
	dryRun     bool
	status     string
}

func parseIdeaFlags(args []string) (ideaFlags, bool, int) {
	var f ideaFlags
	f.source = ideation.SourceHuman
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--risk":
			f.riskDetail = true
		case arg == "--dry-run":
			f.dryRun = true
		case arg == "--title":
			if i+1 >= len(args) {
				return f, false, ideaExitError
			}
			i++
			f.title = args[i]
		case arg == "--body":
			if i+1 >= len(args) {
				return f, false, ideaExitError
			}
			i++
			f.body = args[i]
		case arg == "--tags":
			if i+1 >= len(args) {
				return f, false, ideaExitError
			}
			i++
			for _, t := range strings.Split(args[i], ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					f.tags = append(f.tags, t)
				}
			}
		case arg == "--source":
			if i+1 >= len(args) {
				return f, false, ideaExitError
			}
			i++
			f.source = args[i]
		case arg == "--agent":
			if i+1 >= len(args) {
				return f, false, ideaExitError
			}
			i++
			f.agent = args[i]
		case arg == "--model":
			if i+1 >= len(args) {
				return f, false, ideaExitError
			}
			i++
			f.model = args[i]
		case arg == "--evidence":
			if i+1 >= len(args) {
				return f, false, ideaExitError
			}
			i++
			ev, err := parseEvidenceArg(args[i])
			if err != nil {
				return f, false, ideaExitError
			}
			f.evidence = append(f.evidence, ev)
		case arg == "--store":
			if i+1 >= len(args) {
				return f, false, ideaExitError
			}
			i++
			f.storePath = args[i]
		case arg == "--spec-dir":
			if i+1 >= len(args) {
				return f, false, ideaExitError
			}
			i++
			f.specDir = args[i]
		case arg == "--to":
			if i+1 >= len(args) {
				return f, false, ideaExitError
			}
			i++
			f.to = args[i]
		case arg == "--reason":
			if i+1 >= len(args) {
				return f, false, ideaExitError
			}
			i++
			f.reason = args[i]
		case arg == "--position":
			if i+1 >= len(args) {
				return f, false, ideaExitError
			}
			i++
			f.position = args[i]
		case arg == "--status":
			if i+1 >= len(args) {
				return f, false, ideaExitError
			}
			i++
			f.status = args[i]
		case strings.HasPrefix(arg, "--"):
			return f, false, ideaExitError
		default:
			if f.subcommand == "" {
				f.subcommand = arg
			} else if f.id == "" {
				f.id = arg
			} else {
				return f, false, ideaExitError
			}
		}
		i++
	}

	if f.subcommand == "" {
		f.subcommand = "help"
	}
	return f, helpWanted, ideaExitOK
}

func parseEvidenceArg(s string) (ideation.EvidenceRef, error) {
	// type:ref[:description]
	parts := strings.SplitN(s, ":", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ideation.EvidenceRef{}, fmt.Errorf("invalid --evidence %q (want type:ref[:desc])", s)
	}
	ev := ideation.EvidenceRef{Type: parts[0], Ref: parts[1]}
	if len(parts) == 3 {
		ev.Description = parts[2]
	}
	return ev, nil
}

func printIdeaHelp(w io.Writer) {
	fmt.Fprintln(w, `helix idea — idea capture, validation, prioritization (Phase 1 §1.1–1.3)

Usage:
  helix idea capture --title T --body B [flags]
  helix idea list [--status S] [--json]
  helix idea show <id> [--json]
  helix idea validate <id> [--json] [--risk]
  helix idea prioritize [--json]
  helix idea promote <id> --to spec [--spec-dir DIR] [--json]
  helix idea close <id> --reason R
  helix idea advocate <id> --agent A --position for|against|priority [--json]
  helix idea help

Flags:
  --title T          Idea title (required for capture)
  --body B           Idea body
  --tags a,b         Comma-separated tags
  --source S         human|agent|chimera (default human)
  --agent NAME       Source agent id
  --model M          Source model id
  --evidence t:r:d   Evidence ref (repeatable)
  --store PATH       Ideas JSONL path (or $HELIX_IDEA_STORE)
  --spec-dir DIR     Spec output root for promote (default: specs)
  --to spec          Promotion target (currently only "spec")
  --reason R         Close reason
  --position P       Advocacy position
  --status S         Filter list by status
  --json             Machine-readable output
  --risk             Show risk detail on validate
  --dry-run          Preview writes without mutating store

Exit codes:
  0  Success
  2  Error`)
}

func resolveIdeaStorePath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv(envIdeaStore); v != "" {
		return v
	}
	return "" // NewStore("") → ~/.helix/ideas/ideas.jsonl
}

func openIdeaStore(path string) (*ideation.Store, error) {
	return ideation.NewStore(resolveIdeaStorePath(path))
}

// runIdea is the entry point for `helix idea`.
func runIdea(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseIdeaFlags(args)
	if rc != ideaExitOK {
		fmt.Fprintln(stderr, "error: invalid arguments")
		printIdeaHelp(stderr)
		return ideaExitError
	}
	if helpWanted || flags.subcommand == "help" {
		printIdeaHelp(stdout)
		return ideaExitOK
	}

	switch flags.subcommand {
	case "capture":
		return runIdeaCapture(flags, stdout, stderr)
	case "list":
		return runIdeaList(flags, stdout, stderr)
	case "show":
		return runIdeaShow(flags, stdout, stderr)
	case "validate":
		return runIdeaValidate(flags, stdout, stderr)
	case "prioritize":
		return runIdeaPrioritize(flags, stdout, stderr)
	case "promote":
		return runIdeaPromote(flags, stdout, stderr)
	case "close":
		return runIdeaClose(flags, stdout, stderr)
	case "advocate":
		return runIdeaAdvocate(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		printIdeaHelp(stderr)
		return ideaExitError
	}
}

// runIdeaWithDryRun threads the global --dry-run flag.
func runIdeaWithDryRun(args []string, stdout, stderr io.Writer, dryRun bool) error {
	if dryRun {
		args = append(append([]string{}, args...), "--dry-run")
	}
	rc := runIdea(args, stdout, stderr)
	if rc != ideaExitOK {
		return errExit{code: rc}
	}
	return nil
}

func runIdeaCapture(f ideaFlags, stdout, stderr io.Writer) int {
	if strings.TrimSpace(f.title) == "" {
		fmt.Fprintln(stderr, "error: --title is required")
		return ideaExitError
	}
	if f.source != "" && !ideation.ValidSource(f.source) {
		fmt.Fprintf(stderr, "error: invalid --source %q (want human|agent|chimera)\n", f.source)
		return ideaExitError
	}

	idea := &ideation.Idea{
		Title:       f.title,
		Body:        f.body,
		Tags:        f.tags,
		Source:      f.source,
		SourceAgent: f.agent,
		SourceModel: f.model,
		Evidence:    f.evidence,
	}

	if f.dryRun {
		fmt.Fprintf(stdout, "[DRY RUN] would capture idea title=%q tags=%v source=%s\n", idea.Title, idea.Tags, idea.Source)
		return ideaExitOK
	}

	store, err := openIdeaStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return ideaExitError
	}
	if err := ideation.Capture(store, idea); err != nil {
		fmt.Fprintf(stderr, "error: capture: %v\n", err)
		return ideaExitError
	}

	if f.jsonOut {
		return writeJSON(stdout, stderr, idea)
	}
	fmt.Fprintf(stdout, "captured idea %s  status=%s  source=%s\n", idea.ID, idea.Status, idea.Source)
	fmt.Fprintf(stdout, "  title: %s\n", idea.Title)
	return ideaExitOK
}

func runIdeaList(f ideaFlags, stdout, stderr io.Writer) int {
	store, err := openIdeaStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return ideaExitError
	}
	ideas, err := store.List()
	if err != nil {
		fmt.Fprintf(stderr, "error: list: %v\n", err)
		return ideaExitError
	}
	if f.status != "" {
		filtered := ideas[:0]
		for _, idea := range ideas {
			if idea.Status == f.status {
				filtered = append(filtered, idea)
			}
		}
		ideas = filtered
	}

	if f.jsonOut {
		return writeJSON(stdout, stderr, ideas)
	}
	fmt.Fprintf(stdout, "%-26s %-12s %-8s %s\n", "ID", "STATUS", "SOURCE", "TITLE")
	fmt.Fprintln(stdout, strings.Repeat("-", 80))
	for _, idea := range ideas {
		fmt.Fprintf(stdout, "%-26s %-12s %-8s %s\n",
			truncateStr(idea.ID, 26), idea.Status, idea.Source, truncateStr(idea.Title, 40))
	}
	fmt.Fprintf(stdout, "\n%d ideas\n", len(ideas))
	return ideaExitOK
}

func runIdeaShow(f ideaFlags, stdout, stderr io.Writer) int {
	if f.id == "" {
		fmt.Fprintln(stderr, "error: idea id required")
		return ideaExitError
	}
	store, err := openIdeaStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return ideaExitError
	}
	idea, err := store.Get(f.id)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return ideaExitError
	}
	if f.jsonOut {
		return writeJSON(stdout, stderr, idea)
	}
	fmt.Fprintf(stdout, "ID:       %s\n", idea.ID)
	fmt.Fprintf(stdout, "Title:    %s\n", idea.Title)
	fmt.Fprintf(stdout, "Status:   %s\n", idea.Status)
	fmt.Fprintf(stdout, "Source:   %s\n", idea.Source)
	if idea.SourceAgent != "" {
		fmt.Fprintf(stdout, "Agent:    %s\n", idea.SourceAgent)
	}
	if idea.SourceModel != "" {
		fmt.Fprintf(stdout, "Model:    %s\n", idea.SourceModel)
	}
	if len(idea.Tags) > 0 {
		fmt.Fprintf(stdout, "Tags:     %s\n", strings.Join(idea.Tags, ", "))
	}
	fmt.Fprintf(stdout, "Risk:     %.1f\n", idea.RiskScore)
	fmt.Fprintf(stdout, "Cost:     $%.2f\n", idea.CostTotal)
	fmt.Fprintf(stdout, "Score:    %.4f\n", idea.Score)
	if idea.PromotedTo != "" {
		fmt.Fprintf(stdout, "Promoted: %s\n", idea.PromotedTo)
	}
	fmt.Fprintf(stdout, "Created:  %s\n", idea.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(stdout, "Updated:  %s\n", idea.UpdatedAt.Format(time.RFC3339))
	fmt.Fprintln(stdout, "Body:")
	fmt.Fprintln(stdout, idea.Body)
	if len(idea.Evidence) > 0 {
		fmt.Fprintln(stdout, "Evidence:")
		for _, ev := range idea.Evidence {
			fmt.Fprintf(stdout, "  - [%s] %s", ev.Type, ev.Ref)
			if ev.Description != "" {
				fmt.Fprintf(stdout, " — %s", ev.Description)
			}
			fmt.Fprintln(stdout)
		}
	}
	return ideaExitOK
}

func runIdeaValidate(f ideaFlags, stdout, stderr io.Writer) int {
	if f.id == "" {
		fmt.Fprintln(stderr, "error: idea id required")
		return ideaExitError
	}
	store, err := openIdeaStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return ideaExitError
	}
	idea, err := store.Get(f.id)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return ideaExitError
	}

	report, err := ideation.NewIdeaValidator().Validate(idea)
	if err != nil {
		fmt.Fprintf(stderr, "error: validate: %v\n", err)
		return ideaExitError
	}

	if !f.dryRun {
		idea.RiskScore = report.RiskScore
		idea.Status = ideation.StatusValidated
		if err := store.Update(idea); err != nil {
			fmt.Fprintf(stderr, "error: update after validate: %v\n", err)
			return ideaExitError
		}
	}

	if f.jsonOut {
		return writeJSON(stdout, stderr, report)
	}

	fmt.Fprintf(stdout, "Validation for %s\n", report.IdeaID)
	fmt.Fprintf(stdout, "  Verdict:   %s\n", report.Verdict)
	fmt.Fprintf(stdout, "  RiskScore: %.1f\n", report.RiskScore)
	fmt.Fprintf(stdout, "  Agents:    %s\n", strings.Join(report.AgentsRun, ", "))
	if f.riskDetail || true {
		fmt.Fprintln(stdout, "  Findings:")
		for _, finding := range report.Findings {
			fmt.Fprintf(stdout, "    [%s/%s] %s\n", finding.AgentType, finding.Severity, finding.Description)
			if finding.Recommendation != "" {
				fmt.Fprintf(stdout, "      → %s\n", finding.Recommendation)
			}
		}
	}
	return ideaExitOK
}

func runIdeaPrioritize(f ideaFlags, stdout, stderr io.Writer) int {
	store, err := openIdeaStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return ideaExitError
	}
	ideas, err := store.List()
	if err != nil {
		fmt.Fprintf(stderr, "error: list: %v\n", err)
		return ideaExitError
	}

	// Exclude closed ideas from ranking.
	active := make([]*ideation.Idea, 0, len(ideas))
	for _, idea := range ideas {
		if idea.Status != ideation.StatusClosed {
			active = append(active, idea)
		}
	}

	p := ideation.NewIdeaPrioritizer(store.Path())
	roadmap, err := p.Prioritize(active)
	if err != nil {
		fmt.Fprintf(stderr, "error: prioritize: %v\n", err)
		return ideaExitError
	}

	if !f.dryRun {
		for i := range roadmap.Ideas {
			pi := &roadmap.Ideas[i]
			// Persist cost/score/status onto store.
			idea, err := store.Get(pi.ID)
			if err != nil {
				continue
			}
			idea.CostTotal = pi.CostEstimate
			idea.Score = pi.Score
			idea.RiskScore = pi.RiskScore
			if idea.Status == ideation.StatusDraft || idea.Status == ideation.StatusValidated {
				idea.Status = ideation.StatusPrioritized
			}
			_ = store.Update(idea)
		}
	}

	if f.jsonOut {
		return writeJSON(stdout, stderr, roadmap)
	}

	fmt.Fprintf(stdout, "%-4s %-26s %-8s %-8s %-8s %s\n", "RANK", "ID", "SCORE", "RISK", "COST", "TITLE")
	fmt.Fprintln(stdout, strings.Repeat("-", 90))
	for _, pi := range roadmap.Ideas {
		fmt.Fprintf(stdout, "%-4d %-26s %-8.4f %-8.1f $%-7.2f %s\n",
			pi.Rank, truncateStr(pi.ID, 26), pi.Score, pi.RiskScore, pi.CostEstimate, truncateStr(pi.Title, 36))
	}
	fmt.Fprintf(stdout, "\n%d ideas ranked (version %d)\n", len(roadmap.Ideas), roadmap.Version)
	return ideaExitOK
}

func runIdeaPromote(f ideaFlags, stdout, stderr io.Writer) int {
	if f.id == "" {
		fmt.Fprintln(stderr, "error: idea id required")
		return ideaExitError
	}
	if f.to == "" {
		f.to = "spec"
	}
	if f.to != "spec" {
		fmt.Fprintf(stderr, "error: unsupported --to %q (only \"spec\")\n", f.to)
		return ideaExitError
	}

	store, err := openIdeaStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return ideaExitError
	}
	idea, err := store.Get(f.id)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return ideaExitError
	}
	if idea.RiskScore >= 70 {
		fmt.Fprintln(stderr, "error: idea failed validation (risk_score>=70); re-validate after addressing findings")
		return ideaExitError
	}

	specDir := f.specDir
	if specDir == "" {
		specDir = "specs"
	}
	ideasDir := filepath.Join(specDir, "ideas")
	slug := slugify(idea.Title)
	filename := fmt.Sprintf("%s-%s.md", idea.ID, slug)
	specPath := filepath.Join(ideasDir, filename)

	content := fmt.Sprintf(`---
id: %s
title: %q
status: draft
idea_ref: %s
created_at: %s
---

# %s

%s

## Tags

%s

## Evidence

`, idea.ID, idea.Title, idea.ID, time.Now().UTC().Format(time.RFC3339), idea.Title, idea.Body, strings.Join(idea.Tags, ", "))
	if len(idea.Evidence) == 0 {
		content += "_None recorded._\n"
	} else {
		for _, ev := range idea.Evidence {
			content += fmt.Sprintf("- **%s** `%s`", ev.Type, ev.Ref)
			if ev.Description != "" {
				content += " — " + ev.Description
			}
			content += "\n"
		}
	}

	if f.dryRun {
		fmt.Fprintf(stdout, "[DRY RUN] would promote %s → %s\n", idea.ID, specPath)
		return ideaExitOK
	}

	if err := os.MkdirAll(ideasDir, 0o755); err != nil {
		// Fall back to home dir if repo specs/ not writable.
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			fmt.Fprintf(stderr, "error: mkdir %s: %v\n", ideasDir, err)
			return ideaExitError
		}
		ideasDir = filepath.Join(home, ".helix", "specs", "ideas")
		specPath = filepath.Join(ideasDir, filename)
		if err := os.MkdirAll(ideasDir, 0o755); err != nil {
			fmt.Fprintf(stderr, "error: mkdir %s: %v\n", ideasDir, err)
			return ideaExitError
		}
	}

	if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
		fmt.Fprintf(stderr, "error: write spec: %v\n", err)
		return ideaExitError
	}
	if err := store.Promote(idea.ID, specPath); err != nil {
		fmt.Fprintf(stderr, "error: promote: %v\n", err)
		return ideaExitError
	}

	updated, _ := store.Get(idea.ID)
	if f.jsonOut {
		return writeJSON(stdout, stderr, map[string]interface{}{
			"idea":      updated,
			"spec_path": specPath,
		})
	}
	fmt.Fprintf(stdout, "promoted idea %s → %s\n", idea.ID, specPath)
	return ideaExitOK
}

func runIdeaClose(f ideaFlags, stdout, stderr io.Writer) int {
	if f.id == "" {
		fmt.Fprintln(stderr, "error: idea id required")
		return ideaExitError
	}
	if strings.TrimSpace(f.reason) == "" {
		fmt.Fprintln(stderr, "error: --reason is required")
		return ideaExitError
	}
	if f.dryRun {
		fmt.Fprintf(stdout, "[DRY RUN] would close %s reason=%q\n", f.id, f.reason)
		return ideaExitOK
	}
	store, err := openIdeaStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return ideaExitError
	}
	if err := store.Close(f.id, f.reason); err != nil {
		fmt.Fprintf(stderr, "error: close: %v\n", err)
		return ideaExitError
	}
	fmt.Fprintf(stdout, "closed idea %s (%s)\n", f.id, f.reason)
	return ideaExitOK
}

func runIdeaAdvocate(f ideaFlags, stdout, stderr io.Writer) int {
	if f.id == "" {
		fmt.Fprintln(stderr, "error: idea id required")
		return ideaExitError
	}
	if f.agent == "" {
		fmt.Fprintln(stderr, "error: --agent is required")
		return ideaExitError
	}
	if f.position == "" {
		fmt.Fprintln(stderr, "error: --position is required")
		return ideaExitError
	}

	store, err := openIdeaStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return ideaExitError
	}
	// Ensure idea exists.
	if _, err := store.Get(f.id); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return ideaExitError
	}

	rec := ideation.AdvocacyRecord{
		AgentID:     f.agent,
		Position:    f.position,
		SubmittedAt: time.Now().UTC(),
	}
	if f.dryRun {
		fmt.Fprintf(stdout, "[DRY RUN] would advocate idea=%s agent=%s position=%s\n", f.id, f.agent, f.position)
		return ideaExitOK
	}

	p := ideation.NewIdeaPrioritizer(store.Path())
	if err := p.SubmitAdvocacy(f.id, rec); err != nil {
		fmt.Fprintf(stderr, "error: advocate: %v\n", err)
		return ideaExitError
	}
	if f.jsonOut {
		return writeJSON(stdout, stderr, rec)
	}
	fmt.Fprintf(stdout, "advocacy recorded: idea=%s agent=%s position=%s\n", f.id, f.agent, f.position)
	return ideaExitOK
}

func writeJSON(stdout, stderr io.Writer, v interface{}) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: json: %v\n", err)
		return ideaExitError
	}
	fmt.Fprintln(stdout, string(data))
	return ideaExitOK
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := nonSlug.ReplaceAllString(b.String(), "-")
	out = strings.Trim(out, "-")
	if out == "" {
		return "idea"
	}
	if len(out) > 48 {
		out = out[:48]
		out = strings.Trim(out, "-")
	}
	return out
}
