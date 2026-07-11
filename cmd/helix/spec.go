// Command helix — spec.go
//
// `helix spec` exposes pkg/spec: spec co-authoring with adversarial annotation
// and 12-dimension completeness scoring.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/spec"
)

const (
	specExitOK    = 0
	specExitError = 2

	envSpecStore = "HELIX_SPEC_STORE"
)

// specFlags holds parsed flags for helix spec.
type specFlags struct {
	subcommand string
	id         string
	ideaID     string
	title      string
	section    string
	approvedBy string
	storePath  string
	jsonOut    bool
	dryRun     bool
}

func parseSpecFlags(args []string) (specFlags, bool, int) {
	var f specFlags
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
		case arg == "--title":
			if i+1 >= len(args) {
				return f, false, specExitError
			}
			i++
			f.title = args[i]
		case arg == "--idea":
			if i+1 >= len(args) {
				return f, false, specExitError
			}
			i++
			f.ideaID = args[i]
		case arg == "--section":
			if i+1 >= len(args) {
				return f, false, specExitError
			}
			i++
			f.section = args[i]
		case arg == "--by":
			if i+1 >= len(args) {
				return f, false, specExitError
			}
			i++
			f.approvedBy = args[i]
		case arg == "--store":
			if i+1 >= len(args) {
				return f, false, specExitError
			}
			i++
			f.storePath = args[i]
		case strings.HasPrefix(arg, "--"):
			return f, false, specExitError
		default:
			if f.subcommand == "" {
				f.subcommand = arg
			} else if f.id == "" {
				f.id = arg
			} else {
				return f, false, specExitError
			}
		}
		i++
	}

	if f.subcommand == "" {
		f.subcommand = "help"
	}
	return f, helpWanted, specExitOK
}

func printSpecHelp(w io.Writer) {
	fmt.Fprintln(w, `helix spec — spec co-authoring with adversarial annotation

Usage:
  helix spec create <idea-id> [--title "..."]
  helix spec review <spec-id>
  helix spec gap-analysis <spec-id>
  helix spec approve <spec-id> --section "<name>"
  helix spec show <spec-id>
  helix spec list
  helix spec help

Flags:
  --title T      Spec title (create)
  --idea ID      Override idea reference (defaults to positional arg)
  --section S    Section name to approve (approve)
  --by NAME      Approver identity (approve, default: operator)
  --store PATH   Spec store directory (or $HELIX_SPEC_STORE)
  --json         Machine-readable output
  --dry-run      Preview writes without mutating store

Exit codes:
  0  Success
  2  Error`)
}

func resolveSpecStorePath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv(envSpecStore); v != "" {
		return v
	}
	return "" // NewSpecStore("") → ~/.helix/specs
}

func openSpecStore(path string) (*spec.SpecStore, error) {
	return spec.NewSpecStore(resolveSpecStorePath(path))
}

// runSpec is the entry point for `helix spec`.
func runSpec(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseSpecFlags(args)
	if rc != specExitOK {
		fmt.Fprintln(stderr, "error: invalid arguments")
		printSpecHelp(stderr)
		return specExitError
	}
	if helpWanted || flags.subcommand == "help" {
		printSpecHelp(stdout)
		return specExitOK
	}

	switch flags.subcommand {
	case "create":
		return runSpecCreate(flags, stdout, stderr)
	case "review":
		return runSpecReview(flags, stdout, stderr)
	case "gap-analysis":
		return runSpecGapAnalysis(flags, stdout, stderr)
	case "approve":
		return runSpecApprove(flags, stdout, stderr)
	case "show":
		return runSpecShow(flags, stdout, stderr)
	case "list":
		return runSpecList(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		printSpecHelp(stderr)
		return specExitError
	}
}

// runSpecWithDryRun threads the global --dry-run flag.
func runSpecWithDryRun(args []string, stdout, stderr io.Writer, dryRun bool) error {
	if dryRun {
		args = append(append([]string{}, args...), "--dry-run")
	}
	rc := runSpec(args, stdout, stderr)
	if rc != specExitOK {
		return errExit{code: rc}
	}
	return nil
}

func runSpecCreate(f specFlags, stdout, stderr io.Writer) int {
	if f.id == "" {
		fmt.Fprintln(stderr, "error: idea id required")
		return specExitError
	}

	title := f.title
	if title == "" {
		fmt.Fprintln(stderr, "error: --title is required")
		return specExitError
	}

	ideaRef := f.ideaID
	if ideaRef == "" {
		ideaRef = f.id // positional arg is the idea-id
	}

	s := &spec.Spec{
		ID:      spec.NewSpecID(),
		IdeaRef: ideaRef,
		Title:   title,
		Status:  spec.StatusDraft,
		Sections: []spec.SpecSection{
			{Title: "Overview", Content: fmt.Sprintf("Specification derived from idea %s.\n\n_Replace with intent and context._", ideaRef), ApprovalStatus: spec.ApprovalPending},
			{Title: "Requirements", Content: "_Enumerate MUST/SHALL requirements here._", ApprovalStatus: spec.ApprovalPending},
			{Title: "Non-Goals", Content: "_Explicitly list out-of-scope items._", ApprovalStatus: spec.ApprovalPending},
			{Title: "Constraints", Content: "_Technical, budgetary, and timeline constraints._", ApprovalStatus: spec.ApprovalPending},
			{Title: "Acceptance Criteria", Content: "_Measurable success criteria._", ApprovalStatus: spec.ApprovalPending},
		},
	}

	if f.dryRun {
		fmt.Fprintf(stdout, "[DRY RUN] would create spec id=%s title=%q idea_ref=%s\n", s.ID, s.Title, s.IdeaRef)
		return specExitOK
	}

	store, err := openSpecStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return specExitError
	}
	if err := store.Save(s); err != nil {
		fmt.Fprintf(stderr, "error: save: %v\n", err)
		return specExitError
	}

	if f.jsonOut {
		return writeSpecJSON(stdout, stderr, s)
	}
	fmt.Fprintf(stdout, "created spec %s  status=%s\n", s.ID, s.Status)
	fmt.Fprintf(stdout, "  title: %s\n", s.Title)
	fmt.Fprintf(stdout, "  idea:  %s\n", s.IdeaRef)
	fmt.Fprintf(stdout, "  sections: %d\n", len(s.Sections))
	return specExitOK
}

func runSpecReview(f specFlags, stdout, stderr io.Writer) int {
	if f.id == "" {
		fmt.Fprintln(stderr, "error: spec id required")
		return specExitError
	}

	store, err := openSpecStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return specExitError
	}
	s, err := store.Load(f.id)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return specExitError
	}

	coAuthor := spec.NewSpecCoAuthor()
	annotated, err := coAuthor.CoAuthor(s)
	if err != nil {
		fmt.Fprintf(stderr, "error: co-author: %v\n", err)
		return specExitError
	}

	if !f.dryRun {
		if err := store.Save(annotated); err != nil {
			fmt.Fprintf(stderr, "error: save: %v\n", err)
			return specExitError
		}
	}

	if f.jsonOut {
		return writeSpecJSON(stdout, stderr, annotated)
	}

	fmt.Fprintf(stdout, "Spec Review for %s\n", annotated.ID)
	fmt.Fprintf(stdout, "  Title:  %s\n", annotated.Title)
	fmt.Fprintf(stdout, "  Status: %s\n", annotated.Status)
	fmt.Fprintf(stdout, "  Annotations: %d\n\n", len(annotated.Annotations))

	for i, ann := range annotated.Annotations {
		fmt.Fprintf(stdout, "  [%d] %s/%s [%s] line %d\n", i+1, ann.AgentType, ann.AnnotationType, ann.Severity, ann.Line)
		fmt.Fprintf(stdout, "      %s\n", ann.Content)
	}
	return specExitOK
}

func runSpecGapAnalysis(f specFlags, stdout, stderr io.Writer) int {
	if f.id == "" {
		fmt.Fprintln(stderr, "error: spec id required")
		return specExitError
	}

	store, err := openSpecStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return specExitError
	}
	s, err := store.Load(f.id)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return specExitError
	}

	checker := spec.NewSpecCompleteness()
	report, err := checker.CheckCompleteness(s)
	if err != nil {
		fmt.Fprintf(stderr, "error: completeness: %v\n", err)
		return specExitError
	}

	if f.jsonOut {
		return writeSpecJSON(stdout, stderr, report)
	}

	fmt.Fprintf(stdout, "Gap Analysis for %s\n\n", report.SpecID)
	fmt.Fprintf(stdout, "Total Score: %.1f / 100\n\n", report.TotalScore)
	fmt.Fprintf(stdout, "%-26s %8s %8s  %s\n", "DIMENSION", "SCORE", "MAX", "NOTE")
	fmt.Fprintln(stdout, strings.Repeat("-", 80))
	for _, dim := range report.Dimensions {
		fmt.Fprintf(stdout, "%-26s %8.1f %8.1f  %s\n", dim.Name, dim.Score, dim.MaxScore, dim.Note)
	}

	if len(report.Gaps) > 0 {
		fmt.Fprintf(stdout, "\n%d gaps identified:\n", len(report.Gaps))
		for _, gap := range report.Gaps {
			fmt.Fprintf(stdout, "  [%s] %s — %s\n", gap.Severity, gap.Dimension, gap.Detail)
		}
	}
	return specExitOK
}

func runSpecApprove(f specFlags, stdout, stderr io.Writer) int {
	if f.id == "" {
		fmt.Fprintln(stderr, "error: spec id required")
		return specExitError
	}
	if f.section == "" {
		fmt.Fprintln(stderr, "error: --section is required")
		return specExitError
	}

	approvedBy := f.approvedBy
	if approvedBy == "" {
		approvedBy = "operator"
	}

	store, err := openSpecStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return specExitError
	}
	s, err := store.Load(f.id)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return specExitError
	}

	found := false
	for i := range s.Sections {
		if strings.EqualFold(s.Sections[i].Title, f.section) {
			s.Sections[i].ApprovalStatus = spec.ApprovalApproved
			s.Sections[i].ApprovedBy = approvedBy
			found = true
			break
		}
	}
	if !found {
		fmt.Fprintf(stderr, "error: section %q not found in spec %s\n", f.section, f.id)
		return specExitError
	}

	// If all sections approved, mark spec as approved.
	allApproved := true
	for _, sec := range s.Sections {
		if sec.ApprovalStatus != spec.ApprovalApproved {
			allApproved = false
			break
		}
	}
	if allApproved {
		s.Status = spec.StatusApproved
	}

	if f.dryRun {
		fmt.Fprintf(stdout, "[DRY RUN] would approve section %q in spec %s\n", f.section, f.id)
		return specExitOK
	}

	if err := store.Save(s); err != nil {
		fmt.Fprintf(stderr, "error: save: %v\n", err)
		return specExitError
	}

	fmt.Fprintf(stdout, "approved section %q in spec %s (by %s)\n", f.section, s.ID, approvedBy)
	if allApproved {
		fmt.Fprintf(stdout, "all sections approved — spec status is now %q\n", s.Status)
	}
	return specExitOK
}

func runSpecShow(f specFlags, stdout, stderr io.Writer) int {
	if f.id == "" {
		fmt.Fprintln(stderr, "error: spec id required")
		return specExitError
	}

	store, err := openSpecStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return specExitError
	}
	s, err := store.Load(f.id)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return specExitError
	}

	if f.jsonOut {
		return writeSpecJSON(stdout, stderr, s)
	}

	fmt.Fprintf(stdout, "ID:     %s\n", s.ID)
	fmt.Fprintf(stdout, "Title:  %s\n", s.Title)
	fmt.Fprintf(stdout, "Status: %s\n", s.Status)
	if s.IdeaRef != "" {
		fmt.Fprintf(stdout, "Idea:   %s\n", s.IdeaRef)
	}
	if len(s.ADRRefs) > 0 {
		fmt.Fprintf(stdout, "ADRs:   %s\n", strings.Join(s.ADRRefs, ", "))
	}
	if len(s.ContractRefs) > 0 {
		fmt.Fprintf(stdout, "Contracts: %s\n", strings.Join(s.ContractRefs, ", "))
	}

	fmt.Fprintln(stdout, "\nSections:")
	for _, sec := range s.Sections {
		status := sec.ApprovalStatus
		if status == "" {
			status = spec.ApprovalPending
		}
		fmt.Fprintf(stdout, "\n  ## %s  [%s]\n", sec.Title, status)
		if sec.ApprovedBy != "" {
			fmt.Fprintf(stdout, "     approved by %s\n", sec.ApprovedBy)
		}
		for _, line := range strings.Split(sec.Content, "\n") {
			fmt.Fprintf(stdout, "     %s\n", line)
		}
	}

	if len(s.Annotations) > 0 {
		fmt.Fprintf(stdout, "\nAnnotations (%d):\n", len(s.Annotations))
		for i, ann := range s.Annotations {
			fmt.Fprintf(stdout, "  [%d] %s/%s [%s] line %d — %s\n", i+1, ann.AgentType, ann.AnnotationType, ann.Severity, ann.Line, ann.Content)
		}
	}
	return specExitOK
}

func runSpecList(f specFlags, stdout, stderr io.Writer) int {
	store, err := openSpecStore(f.storePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: store: %v\n", err)
		return specExitError
	}
	specs, err := store.List()
	if err != nil {
		fmt.Fprintf(stderr, "error: list: %v\n", err)
		return specExitError
	}

	if f.jsonOut {
		return writeSpecJSON(stdout, stderr, specs)
	}

	fmt.Fprintf(stdout, "%-26s %-12s %-8s %s\n", "ID", "STATUS", "SECTIONS", "TITLE")
	fmt.Fprintln(stdout, strings.Repeat("-", 80))
	for _, s := range specs {
		fmt.Fprintf(stdout, "%-26s %-12s %-8d %s\n",
			truncateStr(s.ID, 26), s.Status, len(s.Sections), truncateStr(s.Title, 40))
	}
	fmt.Fprintf(stdout, "\n%d specs\n", len(specs))
	return specExitOK
}

func writeSpecJSON(stdout, stderr io.Writer, v interface{}) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: json: %v\n", err)
		return specExitError
	}
	fmt.Fprintln(stdout, string(data))
	return specExitOK
}
