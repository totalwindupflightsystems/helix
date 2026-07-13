// Command helix — disp.go
//
// `helix dispatcher` is a thin operator CLI for inspecting and driving the
// pkg/dispatcher Ralph Loop engine (task decomposition, agent assignment,
// cost guard, loop execution). For the higher-level "spec → PR" flow use
// `helix dispatch`.
//
// Subcommands:
//
//	status        Decompose a spec file and report the work items that
//	               would be dispatched, their priority, and the cost-guard
//	               outcome for the first task against a tier.
//	tick          Run a single Ralph Loop tick on a spec file: decompose,
//	               assign to the lowest-loaded capable agent, and execute
//	               the resulting WorkItem (no Forgejo side-effects).
//	list-tasks    Decompose a spec file and print one line per Task.
//	help          Show this help.
//
// Flags:
//
//	--spec PATH         Path to a spec markdown file (used by status,
//	                    tick, list-tasks).
//	--agent NAME        Agent profile JSON string (e.g.
//	                    '{"name":"alice","capability":"go","max_load":3}').
//	                    Used by status/tick to evaluate assignment.
//	--tier TIER         Trust tier (Provisional, Observed, Trusted,
//	                    Veteran) for cost-guard evaluation.
//	--json              Structured JSON output instead of human-readable
//	                    tables.
//
// All subcommands are dry-run safe — they never touch Forgejo or any
// networked service. The `tick` subcommand runs the local Ralph Loop
// (acquire lock → worktree → execute steps → commit stub) but skips the
// CreateBranch/CreatePR steps that `helix dispatch` performs.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	disp "github.com/totalwindupflightsystems/helix/pkg/dispatcher"
	"github.com/totalwindupflightsystems/helix/pkg/estimate"
	"github.com/totalwindupflightsystems/helix/pkg/identity"
	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

const (
	dispExitOK    = 0
	dispExitBlock = 1 // cost guard blocked the dispatch
	dispExitError = 2 // invocation / IO error
)

// -----------------------------------------------------------------------------
// Flags
// -----------------------------------------------------------------------------

type dispFlags struct {
	subcommand string // status, tick, list-tasks, clarify, clarifications, help
	specPath   string // --spec PATH
	agentJSON  string // --agent JSON string
	tier       string // --tier
	jsonOut    bool   // --json
	answer     string // --answer (for clarify subcommand)
}

func parseDispatcherFlags(args []string) (dispFlags, bool, int) {
	var f dispFlags
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--spec":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: --spec requires a path\n")
				return f, false, dispExitError
			}
			f.specPath = args[i+1]
			i++
		case arg == "--agent":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: --agent requires a JSON profile\n")
				return f, false, dispExitError
			}
			f.agentJSON = args[i+1]
			i++
		case arg == "--tier":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: --tier requires a value\n")
				return f, false, dispExitError
			}
			f.tier = args[i+1]
			i++
		case arg == "--answer":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: --answer requires a value\n")
				return f, false, dispExitError
			}
			f.answer = args[i+1]
			i++
		case len(arg) > 2 && arg[0] == '-' && arg[1] == '-':
			fmt.Fprintf(os.Stderr, "error: unknown flag %q\n", arg)
			return f, false, dispExitError
		default:
			if f.subcommand == "" {
				f.subcommand = arg
			} else {
				fmt.Fprintf(os.Stderr, "error: unexpected positional arg %q\n", arg)
				return f, false, dispExitError
			}
		}
		i++
	}

	if f.subcommand == "" {
		f.subcommand = "help"
	}
	return f, helpWanted, dispExitOK
}

func printDispatcherHelp(w io.Writer) {
	fmt.Fprintln(w, `helix dispatcher — inspect and drive the Ralph Loop engine

Usage:
  helix dispatcher status          --spec PATH [--agent JSON] [--tier TIER] [--json]
  helix dispatcher tick            --spec PATH --agent JSON [--tier TIER]    [--json]
  helix dispatcher list-tasks      --spec PATH                              [--json]
  helix dispatcher clarifications  list                                     [--json]
  helix dispatcher clarify         <task-id> --answer "..."                 [--json]
  helix dispatcher help

Subcommands:
  status          Decompose the spec and print a per-task summary with the
                  cost-guard outcome (when --agent and --tier are provided).
  tick            Run a single Ralph Loop tick on the spec: decompose →
                  assign → execute (no Forgejo side-effects).
  list-tasks      Decompose the spec and print a numbered list of tasks.
  clarifications  List pending and resolved clarification requests.
  clarify         Resolve a pending clarification with an answer.
  help            Show this help.

Flags:
  --spec PATH         Path to the spec markdown file (decomposed into tasks)
  --agent JSON        Agent profile JSON; needed for assignment / cost-guard
                      Example: {"name":"alice","capability":"go","max_load":3,"current_load":0}
  --tier TIER         Trust tier for cost-guard: Provisional|Observed|Trusted|Veteran
  --answer TEXT       Resolution text for the clarify subcommand
  --json              Emit structured JSON instead of human-readable output
  --help, -h          Show this help

Exit codes:
  0  Success (dispatch evaluated; cost-guard decision surfaced in output)
  1  Cost-guard blocked the dispatch
  2  Invocation / IO error`)
}

// -----------------------------------------------------------------------------
// Entry point
// -----------------------------------------------------------------------------

// runDispatcherWithDryRun wraps runDispatcher with the global --dry-run flag.
func runDispatcherWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	rc := runDispatcher(args, stdout, stderr)
	if rc != 0 && rc != dispExitBlock {
		return errExit{code: rc}
	}
	return nil
}

func runDispatcher(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseDispatcherFlags(args)
	if rc != dispExitOK {
		return rc
	}
	if helpWanted {
		printDispatcherHelp(stdout)
		return dispExitOK
	}

	switch flags.subcommand {
	case "help":
		printDispatcherHelp(stdout)
		return dispExitOK
	case "status":
		return runDispatcherStatus(flags, stdout, stderr)
	case "tick":
		return runDispatcherTick(flags, stdout, stderr)
	case "list-tasks":
		return runDispatcherListTasks(flags, stdout, stderr)
	case "clarify":
		return runDispatcherClarify(flags, stdout, stderr)
	case "clarifications":
		return runDispatcherClarifications(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n\n", flags.subcommand)
		printDispatcherHelp(stderr)
		return dispExitError
	}
}

// -----------------------------------------------------------------------------
// Subcommands
// -----------------------------------------------------------------------------

// dispStatusReport is the JSON-shape of `helix dispatcher status`.
type dispStatusReport struct {
	Spec         string                `json:"spec"`
	TaskCount    int                   `json:"task_count"`
	Tasks        []dispTaskSummary     `json:"tasks"`
	CostGuard    *disp.CostGuardResult `json:"cost_guard,omitempty"`
	AgentProfile *disp.AgentProfile    `json:"agent_profile,omitempty"`
}

type dispTaskSummary struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
	Status      string `json:"status"`
}

// runDispatcherStatus decomposes the spec, lists the resulting tasks, and
// (if --agent + --tier were provided) runs the cost guard for the first
// task.
func runDispatcherStatus(flags dispFlags, stdout, stderr io.Writer) int {
	if flags.specPath == "" {
		fmt.Fprintln(stderr, "error: --spec is required")
		printDispatcherHelp(stderr)
		return dispExitError
	}
	if _, err := os.Stat(flags.specPath); err != nil {
		fmt.Fprintf(stderr, "error: spec file not found: %s\n", flags.specPath)
		return dispExitError
	}

	tasks, err := disp.DecomposeSpec(flags.specPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: decompose failed: %v\n", err)
		return dispExitError
	}

	summaries := make([]dispTaskSummary, 0, len(tasks))
	for _, t := range tasks {
		summaries = append(summaries, dispTaskSummary{
			ID:          t.ID,
			Description: oneLine(t.Description),
			Priority:    t.Priority,
			Status:      string(t.Status),
		})
	}
	// Stable order by priority ascending.
	sort.SliceStable(summaries, func(i, j int) bool {
		return summaries[i].Priority < summaries[j].Priority
	})

	report := dispStatusReport{
		Spec:      flags.specPath,
		TaskCount: len(summaries),
		Tasks:     summaries,
	}

	// Optional cost guard for the first task when an agent + tier were given.
	if flags.agentJSON != "" || flags.tier != "" {
		if flags.agentJSON == "" || flags.tier == "" {
			fmt.Fprintln(stderr, "error: --agent and --tier must be provided together")
			return dispExitError
		}
		parsedAgent, err := parseAgentJSON(flags.agentJSON)
		if err != nil {
			fmt.Fprintf(stderr, "error: invalid --agent JSON: %v\n", err)
			return dispExitError
		}
		tier, err := parseTrustTier(flags.tier)
		if err != nil {
			fmt.Fprintf(stderr, "error: invalid --tier: %v\n", err)
			return dispExitError
		}
		report.AgentProfile = &parsedAgent
		cgResult := evaluateCostGuard(&parsedAgent, tier, tasks)
		report.CostGuard = &cgResult
	}

	if flags.jsonOut {
		out, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: marshal: %v\n", err)
			return dispExitError
		}
		fmt.Fprintln(stdout, string(out))
		if report.CostGuard != nil && report.CostGuard.Decision == disp.CostGuardBlocked {
			return dispExitBlock
		}
		return dispExitOK
	}

	// Human-readable.
	fmt.Fprintf(stdout, "Spec: %s\n", report.Spec)
	fmt.Fprintf(stdout, "Tasks: %d\n\n", report.TaskCount)
	printTaskTable(stdout, report.Tasks)
	if report.CostGuard != nil {
		fmt.Fprintln(stdout)
		printCostGuardReport(stdout, *report.CostGuard, *report.AgentProfile)
		if report.CostGuard.Decision == disp.CostGuardBlocked {
			return dispExitBlock
		}
	}
	return dispExitOK
}

// runDispatcherListTasks decomposes the spec and prints a numbered list.
func runDispatcherListTasks(flags dispFlags, stdout, stderr io.Writer) int {
	if flags.specPath == "" {
		fmt.Fprintln(stderr, "error: --spec is required")
		printDispatcherHelp(stderr)
		return dispExitError
	}
	if _, err := os.Stat(flags.specPath); err != nil {
		fmt.Fprintf(stderr, "error: spec file not found: %s\n", flags.specPath)
		return dispExitError
	}
	tasks, err := disp.DecomposeSpec(flags.specPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: decompose failed: %v\n", err)
		return dispExitError
	}

	if flags.jsonOut {
		type taskJSON struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			Priority    int    `json:"priority"`
		}
		out := make([]taskJSON, 0, len(tasks))
		for _, t := range tasks {
			out = append(out, taskJSON{
				ID:          t.ID,
				Description: oneLine(t.Description),
				Priority:    t.Priority,
			})
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return dispExitOK
	}

	fmt.Fprintf(stdout, "Spec %s — %d task(s)\n\n", flags.specPath, len(tasks))
	for i, t := range tasks {
		fmt.Fprintf(stdout, "  %d. [p=%d] %s — %s\n", i+1, t.Priority, t.ID, oneLine(t.Description))
	}
	return dispExitOK
}

// runDispatcherTick runs a single Ralph Loop tick: decompose → assign → execute.
// No Forgejo side-effects — the loop runs locally (lock + worktree + commit)
// but skips CreateBranch / CreatePR which are `helix dispatch`'s job.
func runDispatcherTick(flags dispFlags, stdout, stderr io.Writer) int {
	if flags.specPath == "" || flags.agentJSON == "" {
		fmt.Fprintln(stderr, "error: --spec and --agent are required for tick")
		printDispatcherHelp(stderr)
		return dispExitError
	}
	if _, err := os.Stat(flags.specPath); err != nil {
		fmt.Fprintf(stderr, "error: spec file not found: %s\n", flags.specPath)
		return dispExitError
	}
	agent, err := parseAgentJSON(flags.agentJSON)
	if err != nil {
		fmt.Fprintf(stderr, "error: invalid --agent JSON: %v\n", err)
		return dispExitError
	}

	tasks, err := disp.DecomposeSpec(flags.specPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: decompose failed: %v\n", err)
		return dispExitError
	}
	if len(tasks) == 0 {
		fmt.Fprintln(stderr, "error: spec yielded no tasks")
		return dispExitError
	}

	result, err := disp.AssignAgent(tasks[0], []disp.AgentProfile{agent})
	if err != nil {
		fmt.Fprintf(stderr, "error: assign failed: %v\n", err)
		return dispExitError
	}

	// Build a fresh Dispatcher with the assigned agent and execute the loop.
	disp := disp.NewDispatcher([]disp.AgentProfile{result.WorkItem.Agent})
	loopErr := disp.ExecuteLoop(result.WorkItem)

	if flags.jsonOut {
		out := map[string]any{
			"task_id":       result.WorkItem.Task.ID,
			"agent_name":    result.WorkItem.Agent.Name,
			"steps_planned": len(result.WorkItem.Steps),
			"loop_error":    loopErrString(loopErr),
			"steps":         stepSummaries(result.WorkItem.Steps),
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(b))
		if loopErr != nil {
			return dispExitError
		}
		return dispExitOK
	}

	fmt.Fprintf(stdout, "Tick: dispatched %q to agent %q\n", result.WorkItem.Task.ID, result.WorkItem.Agent.Name)
	fmt.Fprintf(stdout, "Steps planned: %d\n", len(result.WorkItem.Steps))
	for i, s := range result.WorkItem.Steps {
		fmt.Fprintf(stdout, "  %d. %s → %s [%s]\n", i+1, s.Action, oneLine(s.ExpectedOutput), s.Status)
	}
	if loopErr != nil {
		fmt.Fprintf(stdout, "Loop error: %v\n", loopErr)
		return dispExitError
	}
	fmt.Fprintln(stdout, "Loop completed.")
	return dispExitOK
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// parseAgentJSON decodes a --agent JSON string into an AgentProfile.
func parseAgentJSON(s string) (disp.AgentProfile, error) {
	var a disp.AgentProfile
	if err := json.Unmarshal([]byte(s), &a); err != nil {
		return a, fmt.Errorf("decode: %w", err)
	}
	if a.Name == "" {
		return a, fmt.Errorf("missing 'name'")
	}
	if a.Capability == "" {
		return a, fmt.Errorf("missing 'capability'")
	}
	if a.MaxLoad == 0 {
		a.MaxLoad = 3 // sensible default
	}
	return a, nil
}

// parseTrustTier maps the --tier string to trust.TrustTier.
func parseTrustTier(s string) (trust.TrustTier, error) {
	switch strings.ToLower(s) {
	case "provisional":
		return trust.TierProvisional, nil
	case "observed":
		return trust.TierObserved, nil
	case "trusted":
		return trust.TierTrusted, nil
	case "veteran":
		return trust.TierVeteran, nil
	default:
		return "", fmt.Errorf("unknown tier %q (Provisional|Observed|Trusted|Veteran)", s)
	}
}

// evaluateCostGuard builds an Estimator + CostGuard and checks the first task
// against the given agent+tier. The estimator has no caches → the cost guard
// will report an APPROVED decision with zero cost (it's just exercising the
// pipeline, not querying a real model).
func evaluateCostGuard(agent *disp.AgentProfile, tier trust.TrustTier, tasks []disp.Task) disp.CostGuardResult {
	permExp := identity.NewPermissionExpansion()
	// NewEstimator signature is (pricing *PricingYAML, tier string). Passing
	// nil and empty tier relies on Estimator's internal defaults — for this
	// CLI's purpose (exercising the cost-guard pipeline against a parsed
	// spec), an empty pricing YAML produces a zero-cost estimate which is
	// APPROVED against any non-zero tier cap.
	est := estimate.NewEstimator(nil, "")
	cg := disp.NewCostGuard(permExp, est)

	// Build a minimal TaskDesc — the cost guard only needs Description and
	// the estimator will apply defaults for the rest.
	td := estimate.TaskDesc{
		Description: tasks[0].Description,
	}
	res, err := cg.Check(agent.Name, tier, td)
	if err != nil {
		return disp.CostGuardResult{
			Decision: disp.CostGuardEscalated,
			AgentID:  agent.Name,
			Tier:     tier,
			Reason:   fmt.Sprintf("cost guard error: %v", err),
		}
	}
	return res
}

// printTaskTable writes a fixed-width task table to w.
func printTaskTable(w io.Writer, tasks []dispTaskSummary) {
	if len(tasks) == 0 {
		fmt.Fprintln(w, "  (no tasks)")
		return
	}
	// Compute column widths.
	maxID := len("ID")
	maxDesc := len("DESCRIPTION")
	maxPrio := len("P")
	maxStatus := len("STATUS")
	for _, t := range tasks {
		if len(t.ID) > maxID {
			maxID = len(t.ID)
		}
		if len(t.Description) > maxDesc {
			maxDesc = len(t.Description)
		}
		if len(fmt.Sprintf("%d", t.Priority)) > maxPrio {
			maxPrio = len(fmt.Sprintf("%d", t.Priority))
		}
		if len(t.Status) > maxStatus {
			maxStatus = len(t.Status)
		}
	}
	// Cap description width to keep the table readable.
	const descCap = 60
	if maxDesc > descCap {
		maxDesc = descCap
	}

	fmt.Fprintf(w, "  %-*s  %-*s  %-*s  %-*s\n", maxID, "ID", maxDesc, "DESCRIPTION", maxPrio, "P", maxStatus, "STATUS")
	fmt.Fprintf(w, "  %s  %s  %s  %s\n", strings.Repeat("-", maxID), strings.Repeat("-", maxDesc), strings.Repeat("-", maxPrio), strings.Repeat("-", maxStatus))
	for _, t := range tasks {
		desc := t.Description
		if len(desc) > descCap {
			desc = desc[:descCap-3] + "..."
		}
		fmt.Fprintf(w, "  %-*s  %-*s  %-*d  %-*s\n",
			maxID, t.ID,
			maxDesc, desc,
			maxPrio, t.Priority,
			maxStatus, t.Status,
		)
	}
}

// printCostGuardReport writes a cost-guard verdict block to w.
func printCostGuardReport(w io.Writer, r disp.CostGuardResult, a disp.AgentProfile) {
	fmt.Fprintf(w, "Cost guard — agent=%s tier=%s\n", a.Name, r.Tier)
	fmt.Fprintf(w, "  Decision:      %s\n", r.Decision)
	fmt.Fprintf(w, "  Cap per job:   $%.2f\n", r.CostCapPerJob)
	fmt.Fprintf(w, "  Estimated cost: $%.4f\n", r.EstimatedCost)
	fmt.Fprintf(w, "  Reason:        %s\n", r.Reason)
}

// oneLine collapses whitespace and truncates a multi-line description to a
// single readable line.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const cap = 80
	if len(s) > cap {
		return s[:cap-3] + "..."
	}
	return s
}

// loopErrString converts an ExecuteLoop error into a JSON-safe string.
func loopErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// stepSummaries converts steps to a JSON-safe shape.
type stepSummary struct {
	Action string `json:"action"`
	Output string `json:"expected_output"`
	Status string `json:"status"`
}

func stepSummaries(steps []disp.Step) []stepSummary {
	out := make([]stepSummary, 0, len(steps))
	for _, s := range steps {
		out = append(out, stepSummary{
			Action: s.Action,
			Output: s.ExpectedOutput,
			Status: string(s.Status),
		})
	}
	return out
}

// -----------------------------------------------------------------------------
// Clarification subcommands
// -----------------------------------------------------------------------------

// runDispatcherClarify resolves a pending clarification for a task.
//
//	helix dispatcher clarify <task-id> --answer "use library X"
//
// The resolution is persisted and the agent can resume execution.
func runDispatcherClarify(flags dispFlags, stdout, stderr io.Writer) int {
	taskID := flags.specPath // re-use --spec for task ID (positional)
	if taskID == "" {
		// The first positional after "clarify" is the task ID.
		// parseDispatcherFlags puts it in specPath if it appears before flags.
		// For better UX we allow it as the first positional arg.
		fmt.Fprintln(stderr, "error: <task-id> is required (e.g. helix dispatcher clarify task-001 --answer \"...\")")
		printDispatcherHelp(stderr)
		return dispExitError
	}
	if flags.answer == "" {
		fmt.Fprintln(stderr, "error: --answer is required")
		printDispatcherHelp(stderr)
		return dispExitError
	}

	cs := disp.NewClarificationStore(disp.DefaultClarificationDir())

	// Load the existing pending clarification.
	rec, err := cs.Load(taskID)
	if err != nil {
		fmt.Fprintf(stderr, "error: no pending clarification for task %q: %v\n", taskID, err)
		return dispExitError
	}
	if rec.Status == "resolved" {
		fmt.Fprintf(stderr, "note: clarification for task %q is already resolved\n", taskID)
	}

	resp := disp.NewClarificationResponse(taskID, flags.answer, "human:operator")
	resolved, err := cs.Resolve(&rec.Request, resp)
	if err != nil {
		fmt.Fprintf(stderr, "error: save resolution: %v\n", err)
		return dispExitError
	}

	if flags.jsonOut {
		b, _ := json.MarshalIndent(resolved, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return dispExitOK
	}

	fmt.Fprintf(stdout, "Clarification resolved for task %q\n", taskID)
	fmt.Fprintf(stdout, "  Question:   %s\n", oneLine(rec.Request.Question))
	fmt.Fprintf(stdout, "  Resolution: %s\n", flags.answer)
	fmt.Fprintf(stdout, "  Resolved by: human:operator\n")
	return dispExitOK
}

// runDispatcherClarifications lists pending (and optionally resolved)
// clarification requests.
//
//	helix dispatcher clarifications list
//
// The subcommand takes no required flags — it reads from the on-disk
// clarification store.
func runDispatcherClarifications(flags dispFlags, stdout, stderr io.Writer) int {
	cs := disp.NewClarificationStore(disp.DefaultClarificationDir())

	filter := disp.ClarificationFilter{}
	if flags.answer != "" {
		// Re-use --answer to filter by status.
		filter.Status = flags.answer
	}
	if flags.agentJSON != "" {
		filter.AgentName = flags.agentJSON
	}

	records, err := cs.List(filter)
	if err != nil {
		fmt.Fprintf(stderr, "error: list clarifications: %v\n", err)
		return dispExitError
	}

	if flags.jsonOut {
		out := make([]map[string]any, 0, len(records))
		for _, r := range records {
			entry := map[string]any{
				"task_id":    r.Request.TaskID,
				"status":     r.Status,
				"question":   r.Request.Question,
				"created_at": r.CreatedAt,
			}
			if r.Response != nil {
				entry["resolution"] = r.Response.Resolution
				entry["resolved_by"] = r.Response.ResolvedBy
				entry["resolved_at"] = r.Response.ResolvedAt
			}
			out = append(out, entry)
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return dispExitOK
	}

	if len(records) == 0 {
		fmt.Fprintln(stdout, "No clarification requests found.")
		return dispExitOK
	}

	fmt.Fprintf(stdout, "Clarifications: %d\n\n", len(records))
	for _, r := range records {
		status := "PENDING"
		if r.Status == "resolved" {
			status = "RESOLVED"
		}
		fmt.Fprintf(stdout, "  [%s] %s\n", status, r.Request.TaskID)
		fmt.Fprintf(stdout, "    Question: %s\n", oneLine(r.Request.Question))
		if r.Response != nil {
			fmt.Fprintf(stdout, "    Resolution: %s\n", oneLine(r.Response.Resolution))
			fmt.Fprintf(stdout, "    Resolved by: %s\n", r.Response.ResolvedBy)
		}
		fmt.Fprintln(stdout)
	}
	return dispExitOK
}
