package dispatcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxBodySteps bounds how many steps DecomposeTaskWithBody extracts from a
// section body. Spec sections can be hundreds of lines; a single dispatch
// should stay reviewable.
const maxBodySteps = 16

// minStepLen is the shortest body fragment worth calling a step. Below this
// the candidate is a heading stub, a list-artifact or a diagram label.
const minStepLen = 20

// actionVerbs drive step extraction. A candidate sentence that contains any
// of these substrings is considered actionable.
var actionVerbs = []string{
	"create", "write", "implement", "add", "update", "delete",
	"remove", "refactor", "test", "verify", "build", "deploy",
	"configure", "setup", "set up", "run", "check", "read",
	"modify", "edit", "replace", "migrate", "extract", "merge",
}

// specHeading is a level-1 or level-2 ATX heading located outside a fenced
// code block. Index is the 0-based line index of the heading line itself.
type specHeading struct {
	Level int
	Text  string
	Index int
}

// DecomposeSpec reads a spec markdown file and extracts tasks from it.
//
// Decomposition follows an explicit fallback ladder so that every readable,
// non-empty markdown document yields at least one task:
//
//  1. Legacy markers — an H1 heading beginning "# Helix Feature" (or
//     "# HELIX FEATURE", case-insensitive) and any H2 heading containing
//     "PHASE" or "FEATURE". This is the historical rule and produces exactly
//     the same tasks, in the same order, with the same IDs and priorities as
//     before; it is evaluated first so existing specs do not change.
//  2. Otherwise, if the document has H2 ("## ") sections, each H2 section
//     becomes one task, titled by the heading text. This covers the repo's
//     own titled-H1 specs ("## 1. Mission", "## 2. Scope", ...). A document
//     with several top-level H1s and H2 sections also lands here — splitting
//     on H2 is the only sane generalisation, and erroring would violate the
//     never-fail contract below.
//  3. Otherwise (no H2 sections at all) the whole file becomes ONE task
//     titled by its H1, or by the file name when there is no H1.
//
// Every task also carries Body: the section text between its heading and the
// next task boundary. The Description remains the heading text alone because
// callers depend on it.
//
// ErrDecomposeFailed is returned only for empty or unreadable input, never for
// a readable document that happens to use headings the legacy rule ignored.
//
// Headings inside fenced code blocks (``` / ~~~) are not task boundaries —
// shell comments such as "# restart the container" would otherwise fracture
// the document.
func DecomposeSpec(specPath string) ([]Task, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrSpecNotFound, specPath, err)
	}

	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if !hasNonBlankLine(lines) {
		return nil, fmt.Errorf("%w: empty spec %s", ErrDecomposeFailed, specPath)
	}

	headings := scanHeadings(lines)
	boundaries := selectBoundaries(headings)
	return assembleTasks(specPath, lines, boundaries), nil
}

// selectBoundaries applies the decomposition ladder and returns the heading
// lines that start a task, in document order.
func selectBoundaries(headings []specHeading) []specHeading {
	var legacy []specHeading
	for _, h := range headings {
		switch h.Level {
		case 1:
			if isHelixFeatureHeading(h.Text) {
				legacy = append(legacy, h)
			}
		case 2:
			upper := strings.ToUpper(h.Text)
			if strings.Contains(upper, "PHASE") || strings.Contains(upper, "FEATURE") {
				legacy = append(legacy, h)
			}
		}
	}
	if len(legacy) > 0 {
		return legacy
	}

	var sections []specHeading
	for _, h := range headings {
		if h.Level == 2 {
			sections = append(sections, h)
		}
	}
	if len(sections) > 0 {
		return sections
	}

	// No boundaries: the caller builds a single whole-file task.
	return nil
}

// assembleTasks turns boundary headings into tasks, capturing each section's
// body. With no boundaries it produces exactly one task for the whole file.
func assembleTasks(specPath string, lines []string, boundaries []specHeading) []Task {
	if len(boundaries) == 0 {
		return []Task{wholeFileTask(specPath, lines)}
	}

	tasks := make([]Task, 0, len(boundaries))
	for i, b := range boundaries {
		end := len(lines)
		if i+1 < len(boundaries) {
			end = boundaries[i+1].Index
		}

		priority := i + 1
		tasks = append(tasks, Task{
			ID:          fmt.Sprintf("task-%03d", priority),
			SpecRef:     specPath,
			Description: strings.TrimSpace(b.Text),
			Body:        strings.TrimSpace(strings.Join(lines[b.Index+1:end], "\n")),
			Priority:    priority,
			Status:      StatusPending,
		})
	}
	return tasks
}

// wholeFileTask builds the single task used when a document has no boundaries.
// The title comes from the first H1; the body is everything after that heading
// (or the whole document when there is no H1).
func wholeFileTask(specPath string, lines []string) Task {
	title := ""
	bodyStart := 0
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			bodyStart = i + 1
			break
		}
	}
	if title == "" {
		title = fileTitle(specPath)
		bodyStart = 0
	}

	return Task{
		ID:          "task-001",
		SpecRef:     specPath,
		Description: title,
		Body:        strings.TrimSpace(strings.Join(lines[bodyStart:], "\n")),
		Priority:    1,
		Status:      StatusPending,
	}
}

// fileTitle derives a task title from a spec path when the document has no H1.
func fileTitle(specPath string) string {
	base := filepath.Base(specPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	if trimmed := strings.TrimSpace(base); trimmed != "" {
		return trimmed
	}
	return filepath.Base(specPath)
}

// scanHeadings returns every H1/H2 heading outside fenced code blocks.
func scanHeadings(lines []string) []specHeading {
	var out []specHeading
	inFence := false
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if isFence(trimmed) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "## "):
			out = append(out, specHeading{
				Level: 2,
				Text:  strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")),
				Index: i,
			})
		case strings.HasPrefix(trimmed, "# "):
			out = append(out, specHeading{
				Level: 1,
				Text:  strings.TrimSpace(strings.TrimPrefix(trimmed, "# ")),
				Index: i,
			})
		}
	}
	return out
}

// isHelixFeatureHeading reports whether an H1 heading opens a Helix feature
// section ("# Helix Feature N — Title").
func isHelixFeatureHeading(text string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(text)), "HELIX FEATURE")
}

// isFence reports whether a trimmed line opens or closes a code fence.
func isFence(trimmed string) bool {
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// hasNonBlankLine reports whether the document carries any content at all.
func hasNonBlankLine(lines []string) bool {
	for _, raw := range lines {
		if strings.TrimSpace(raw) != "" {
			return true
		}
	}
	return false
}

// DecomposeTask breaks a task description into actionable steps. Each sentence
// (delimited by ". " or newline) that contains an action verb becomes a step.
//
// Its signature and output (empty ExpectedOutput) are unchanged for existing
// callers; use DecomposeTaskWithBody when the task carries a spec section body.
func DecomposeTask(taskDesc string) ([]Step, error) {
	if strings.TrimSpace(taskDesc) == "" {
		return nil, fmt.Errorf("%w: empty task description", ErrDecomposeFailed)
	}

	// Split on newlines first, then on sentence boundaries.
	raw := strings.ReplaceAll(taskDesc, "\n", ". ")
	parts := strings.Split(raw, ". ")

	var steps []Step
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if containsActionVerb(part) {
			steps = append(steps, Step{
				Action:         part,
				ExpectedOutput: "",
				Status:         StepPending,
			})
		}
	}

	if len(steps) == 0 {
		// Fallback: treat the entire description as one step.
		steps = append(steps, Step{
			Action:         taskDesc,
			ExpectedOutput: "",
			Status:         StepPending,
		})
	}

	return steps, nil
}

// DecomposeTaskWithBody derives a task's steps from its spec section body,
// falling back to the description when the body yields nothing actionable.
//
// This is the entry point used when a WorkItem is built (ForgejoLoop.Plan and
// AssignAgent): a heading alone produces a heading-shaped step with an empty
// expected output, which is not a plan. Every returned step carries a
// non-empty ExpectedOutput so completion markers are checkable.
func DecomposeTaskWithBody(taskDesc, body string) ([]Step, error) {
	steps, err := DecomposeTask(taskDesc)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(body) != "" {
		if bodySteps := stepsFromBody(body, maxBodySteps); len(bodySteps) > 0 {
			steps = bodySteps
		}
	}
	stampExpectedOutput(steps, taskDesc)
	return steps, nil
}

// stepsFromBody extracts actionable sentences from a section body.
//
// Markdown scaffolding is dropped rather than turned into steps: headings,
// table separator rows, list markers, blockquote markers and fenced code
// blocks (shell comments and YAML keys are noise, not actions). Consecutive
// prose lines are joined into paragraphs before sentence splitting, so a
// wrapped sentence yields one step instead of one fragment per source line.
func stepsFromBody(body string, limit int) []Step {
	var steps []Step
	seen := make(map[string]bool)

	add := func(candidate string) {
		if len(steps) >= limit {
			return
		}
		candidate = cleanStepLine(candidate)
		if !isStepCandidate(candidate) {
			return
		}
		key := strings.ToLower(candidate)
		if seen[key] {
			return
		}
		seen[key] = true
		steps = append(steps, Step{Action: candidate, Status: StepPending})
	}

	for _, block := range proseBlocks(body) {
		for _, sentence := range strings.Split(block, ". ") {
			add(sentence)
		}
	}
	return steps
}

// isStepCandidate rejects the residue that survives scaffolding removal:
// fragments, separators and stub-length lines.
func isStepCandidate(candidate string) bool {
	if candidate == "" || len(candidate) < minStepLen {
		return false
	}
	if isTableSeparator(candidate) {
		return false
	}
	if strings.Trim(candidate, "-—*_ 	") == "" {
		return false
	}
	return len(strings.Fields(candidate)) >= 3
}

// proseBlocks groups a markdown body into candidate blocks: paragraphs of
// joined prose lines, plus one block per list item and per table row.
func proseBlocks(body string) []string {
	var blocks []string
	var para []string

	flushPara := func() {
		if len(para) > 0 {
			blocks = append(blocks, strings.Join(para, " "))
			para = nil
		}
	}

	inFence := false
	for _, raw := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(raw)
		switch {
		case isFence(trimmed):
			flushPara()
			inFence = !inFence
		case inFence, trimmed == "":
			if trimmed == "" {
				flushPara()
			}
		case strings.HasPrefix(trimmed, "#"):
			flushPara() // headings and sub-headings are not actions
		case isTableSeparator(trimmed):
			flushPara()
		case isBlockStart(trimmed):
			flushPara()
			blocks = append(blocks, trimmed)
		default:
			para = append(para, trimmed)
		}
	}
	flushPara()
	return blocks
}

// isBlockStart reports whether a line starts a self-contained block (list item
// or table row) rather than continuing a prose paragraph.
func isBlockStart(trimmed string) bool {
	if strings.HasPrefix(trimmed, "|") {
		return true
	}
	for _, marker := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(trimmed, marker) {
			return true
		}
	}
	return hasOrderedListMarker(trimmed)
}

// hasOrderedListMarker reports whether trimmed begins with "12. " / "12) ".
func hasOrderedListMarker(trimmed string) bool {
	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits >= len(trimmed)-1 {
		return false
	}
	if trimmed[digits] != '.' && trimmed[digits] != ')' {
		return false
	}
	return strings.HasPrefix(trimmed[digits+1:], " ")
}

// cleanStepLine strips markdown list/table/quote markers and collapses
// whitespace. Returns "" for lines that are pure scaffolding.
func cleanStepLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, ">")
	s = strings.TrimSpace(s)

	// Table rows: drop the outer pipes, keep the prose.
	if strings.HasPrefix(s, "|") || strings.HasSuffix(s, "|") {
		s = strings.Trim(s, "|")
		s = strings.TrimSpace(s)
		s = strings.ReplaceAll(s, "|", " ")
	}

	// List markers: "- ", "* ", "+ ", "1. ", "1) ".
	for _, marker := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(s, marker) {
			s = strings.TrimSpace(strings.TrimPrefix(s, marker))
			break
		}
	}
	s = strings.TrimSpace(stripOrderedListMarker(s))
	s = strings.Join(strings.Fields(s), " ")

	// A sentence fragment must carry some substance to be a step.
	if len([]rune(s)) < 8 {
		return ""
	}
	// Trailing sentence punctuation is cosmetic for a step action.
	s = strings.TrimSpace(strings.TrimRight(s, "."))
	return s
}

// stripOrderedListMarker removes a leading "12. " / "12) " marker.
func stripOrderedListMarker(s string) string {
	digits := 0
	for digits < len(s) && s[digits] >= '0' && s[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits >= len(s)-1 {
		return s
	}
	if s[digits] != '.' && s[digits] != ')' {
		return s
	}
	rest := s[digits+1:]
	if !strings.HasPrefix(rest, " ") {
		return s
	}
	return strings.TrimSpace(rest)
}

// isTableSeparator reports whether a line is a markdown table separator row
// such as "|---|---|" or ":--|--:".
func isTableSeparator(trimmed string) bool {
	if !strings.Contains(trimmed, "-") {
		return false
	}
	for _, r := range trimmed {
		switch r {
		case '|', '-', ':', ' ', '\t':
		default:
			return false
		}
	}
	return true
}

// containsActionVerb reports whether s contains any known action verb.
func containsActionVerb(s string) bool {
	lower := strings.ToLower(s)
	for _, verb := range actionVerbs {
		if strings.Contains(lower, verb) {
			return true
		}
	}
	return false
}

// stampExpectedOutput gives every step a non-empty expected output derived
// from its position in the task, so a completion marker has something to
// check. Steps mutated in place.
func stampExpectedOutput(steps []Step, taskDesc string) {
	label := oneLineLabel(taskDesc, 60)
	for i := range steps {
		if strings.TrimSpace(steps[i].ExpectedOutput) != "" {
			continue
		}
		steps[i].ExpectedOutput = fmt.Sprintf("step %d/%d of %q complete", i+1, len(steps), label)
	}
}

// oneLineLabel collapses whitespace and truncates to limit runes.
func oneLineLabel(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}
