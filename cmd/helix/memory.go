// Package main — helix memory CLI: wraps pkg/memory lifecycle pipeline.
//
// `helix memory <subcommand>` exposes the Hivemind memory bank pipeline
// (spec §8.6) for operator inspection: append raw events to the inbox,
// compile them into deduplicated MemoryEntry records, run the full
// lifecycle pipeline, and print the resulting index.
package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/memory"
)

const (
	memoryExitOK    = 0
	memoryExitError = 2
)

type memoryFlags struct {
	subcommand string // append | compile | run | index | list | inbox-status
	agent      string
	repo       string
	eventType  string
	summary    string
	file       string
	operation  string
	tags       string // comma-separated
	jsonOut    bool
}

// memorySubcommands lists the supported memory subcommands.
var memorySubcommands = []string{
	"append", "compile", "run", "index", "list", "inbox-status",
}

// parseMemoryFlags parses args for `helix memory <sub> [flags]`.
func parseMemoryFlags(args []string, stdout, stderr io.Writer) (memoryFlags, int) {
	var f memoryFlags
	f.subcommand = "inbox-status" // default

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			printMemoryHelp(stdout)
			return f, memoryExitOK
		case arg == "--json":
			f.jsonOut = true
		case arg == "--agent":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "memory: --agent requires a value")
				return f, memoryExitError
			}
			i++
			f.agent = args[i]
		case arg == "--repo":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "memory: --repo requires a value")
				return f, memoryExitError
			}
			i++
			f.repo = args[i]
		case arg == "--event-type":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "memory: --event-type requires a value")
				return f, memoryExitError
			}
			i++
			f.eventType = args[i]
		case arg == "--summary":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "memory: --summary requires a value")
				return f, memoryExitError
			}
			i++
			f.summary = args[i]
		case arg == "--file":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "memory: --file requires a value")
				return f, memoryExitError
			}
			i++
			f.file = args[i]
		case arg == "--operation":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "memory: --operation requires a value")
				return f, memoryExitError
			}
			i++
			f.operation = args[i]
		case arg == "--tags":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "memory: --tags requires a value (comma-separated)")
				return f, memoryExitError
			}
			i++
			f.tags = args[i]
		case strings.HasPrefix(arg, "--agent="):
			f.agent = strings.TrimPrefix(arg, "--agent=")
		case strings.HasPrefix(arg, "--repo="):
			f.repo = strings.TrimPrefix(arg, "--repo=")
		case strings.HasPrefix(arg, "--event-type="):
			f.eventType = strings.TrimPrefix(arg, "--event-type=")
		case strings.HasPrefix(arg, "--summary="):
			f.summary = strings.TrimPrefix(arg, "--summary=")
		case strings.HasPrefix(arg, "--file="):
			f.file = strings.TrimPrefix(arg, "--file=")
		case strings.HasPrefix(arg, "--operation="):
			f.operation = strings.TrimPrefix(arg, "--operation=")
		case strings.HasPrefix(arg, "--tags="):
			f.tags = strings.TrimPrefix(arg, "--tags=")
		case strings.HasPrefix(arg, "--"):
			fmt.Fprintf(stderr, "memory: unknown flag %q\n", arg)
			return f, memoryExitError
		default:
			if isMemorySubcommand(arg) {
				f.subcommand = arg
			} else {
				fmt.Fprintf(stderr, "memory: unknown subcommand %q (available: %s)\n",
					arg, strings.Join(memorySubcommands, ", "))
				return f, memoryExitError
			}
		}
		i++
	}

	return f, memoryExitOK
}

func isMemorySubcommand(s string) bool {
	for _, c := range memorySubcommands {
		if c == s {
			return true
		}
	}
	return false
}

func printMemoryHelp(w io.Writer) {
	fmt.Fprintf(w, `helix memory — Hivemind memory bank lifecycle pipeline (spec §8.6)

Usage:
  helix memory append        Append a raw event to the inbox (requires --agent --repo --event-type --summary)
  helix memory compile       Compile current inbox into deduplicated CompiledEntry records
  helix memory run           Run the full lifecycle (compile + persist + index) on the inbox
  helix memory index         Print the index for a namespace (default: lists all)
  helix memory list          List all persisted MemoryEntry records
  helix memory inbox-status  Show inbox event count and oldest/newest events

Flags:
  --agent <name>         Agent ID (required for append)
  --repo <org/repo>      Repo slug (required for append)
  --event-type <type>    Event type (required for append): gate_failure, merge, pr_review, etc.
  --summary <text>       Human-readable summary (required for append)
  --file <path>          Primary file touched (optional)
  --operation <op>       Operation: write, delete, rename (optional)
  --tags <a,b,c>         Comma-separated tags (optional)
  --json                 JSON output
  --help                 Show this help

Note: This CLI operates on an in-memory store for operator inspection.
Long-term storage should use a git-backed or DuckDB-backed MemoryStore.
`)
}

// runMemory dispatches to the appropriate subcommand handler.
func runMemory(args []string, stdout, stderr io.Writer) int {
	flags, rc := parseMemoryFlags(args, stdout, stderr)
	if rc != memoryExitOK {
		return rc
	}

	switch flags.subcommand {
	case "append":
		return runMemoryAppend(flags, stdout, stderr)
	case "compile":
		return runMemoryCompile(flags, stdout, stderr)
	case "run":
		return runMemoryRun(flags, stdout, stderr)
	case "index":
		return runMemoryIndex(flags, stdout, stderr)
	case "list":
		return runMemoryList(flags, stdout, stderr)
	case "inbox-status":
		return runMemoryInboxStatus(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "memory: unknown subcommand %q\n", flags.subcommand)
		return memoryExitError
	}
}

// requireEventFlags validates that the four mandatory fields for append are set.
func requireEventFlags(flags memoryFlags, stderr io.Writer) int {
	missing := []string{}
	if flags.agent == "" {
		missing = append(missing, "--agent")
	}
	if flags.repo == "" {
		missing = append(missing, "--repo")
	}
	if flags.eventType == "" {
		missing = append(missing, "--event-type")
	}
	if flags.summary == "" {
		missing = append(missing, "--summary")
	}
	if len(missing) > 0 {
		fmt.Fprintf(stderr, "memory: append requires %s\n", strings.Join(missing, ", "))
		return memoryExitError
	}
	return memoryExitOK
}

// runMemoryAppend adds an event to the inbox and reports whether it was
// merged into an existing batch or appended as new.
func runMemoryAppend(flags memoryFlags, stdout, stderr io.Writer) int {
	if rc := requireEventFlags(flags, stderr); rc != memoryExitOK {
		return rc
	}

	inbox := memory.NewInbox(0)
	evt := memory.InboxEvent{
		Agent:     flags.agent,
		Repo:      flags.repo,
		EventType: memory.EventType(flags.eventType),
		Summary:   flags.summary,
		File:      flags.file,
		Operation: flags.operation,
	}
	if flags.tags != "" {
		evt.Tags = strings.Split(flags.tags, ",")
	}

	appended, err := inbox.Append(evt)
	if err != nil {
		// Coalesced into existing event — that's success, not failure.
		fmt.Fprintf(stdout, "MERGED %s\n", err.Error())
		return memoryExitOK
	}

	if flags.jsonOut {
		fmt.Fprintf(stdout, `{"id":%q,"agent":%q,"repo":%q,"event_type":%q,"received_at":%q}`+"\n",
			appended.ID, appended.Agent, appended.Repo,
			string(appended.EventType), appended.ReceivedAt.Format(time.RFC3339))
		return memoryExitOK
	}

	fmt.Fprintf(stdout, "APPENDED %s\n", appended.ID)
	fmt.Fprintf(stdout, "  agent:       %s\n", appended.Agent)
	fmt.Fprintf(stdout, "  repo:        %s\n", appended.Repo)
	fmt.Fprintf(stdout, "  event_type:  %s\n", appended.EventType)
	fmt.Fprintf(stdout, "  received_at: %s\n", appended.ReceivedAt.Format(time.RFC3339))
	return memoryExitOK
}

// runMemoryCompile batches the inbox into CompiledEntry records (no persistence).
func runMemoryCompile(flags memoryFlags, stdout, stderr io.Writer) int {
	inbox := memory.NewInbox(0)
	compiler := memory.NewCompiler(inbox)
	entries := compiler.Compile()

	if flags.jsonOut {
		fmt.Fprintf(stdout, `{"compiled_count":%d,"entries":[`, len(entries))
		for i, e := range entries {
			if i > 0 {
				fmt.Fprint(stdout, ",")
			}
			fmt.Fprintf(stdout, `{"id":%q,"agent":%q,"repo":%q,"event_type":%q,"summary":%q}`,
				e.ID, e.Agent, e.Repo, string(e.EventType), e.Summary)
		}
		fmt.Fprint(stdout, "]}\n")
		return memoryExitOK
	}

	fmt.Fprintf(stdout, "%d compiled entries\n", len(entries))
	for _, e := range entries {
		fmt.Fprintf(stdout, "  %s [%s] %s — %s\n",
			e.ID, e.EventType, e.Agent, e.Summary)
	}
	return memoryExitOK
}

// runMemoryRun runs the full lifecycle: compile → persist → index.
func runMemoryRun(flags memoryFlags, stdout, stderr io.Writer) int {
	store := memory.NewMemStore()
	inbox := memory.NewInbox(0)
	lc := memory.NewLifecycle(inbox, store)

	persisted, indexes, err := lc.Run(time.Time{})
	if err != nil {
		fmt.Fprintf(stderr, "memory: lifecycle run failed: %v\n", err)
		return memoryExitError
	}

	if flags.jsonOut {
		fmt.Fprintf(stdout, `{"persisted_count":%d,"index_count":%d,"namespaces":[`,
			len(persisted), len(indexes))
		for i, idx := range indexes {
			if i > 0 {
				fmt.Fprint(stdout, ",")
			}
			fmt.Fprintf(stdout, `%q`, idx.Namespace)
		}
		fmt.Fprint(stdout, "]}\n")
		return memoryExitOK
	}

	fmt.Fprintf(stdout, "Lifecycle run: %d persisted, %d indexes built\n",
		len(persisted), len(indexes))
	for _, idx := range indexes {
		fmt.Fprintf(stdout, "%s\n", idx.Format())
	}
	return memoryExitOK
}

// runMemoryIndex prints the index for a namespace (or all namespaces).
func runMemoryIndex(flags memoryFlags, stdout, stderr io.Writer) int {
	store := memory.NewMemStore()
	inbox := memory.NewInbox(0)
	lc := memory.NewLifecycle(inbox, store)

	// Seed at least one event to produce a non-empty lifecycle
	if flags.agent != "" && flags.repo != "" && flags.eventType != "" && flags.summary != "" {
		evt := memory.InboxEvent{
			Agent:     flags.agent,
			Repo:      flags.repo,
			EventType: memory.EventType(flags.eventType),
			Summary:   flags.summary,
		}
		_, _ = inbox.Append(evt)
	}

	_, _, err := lc.Run(time.Time{})
	if err != nil {
		fmt.Fprintf(stderr, "memory: lifecycle run failed: %v\n", err)
		return memoryExitError
	}

	now := time.Now().UTC()
	if flags.agent != "" {
		ns := memory.Namespace("/agents/" + flags.agent)
		idx, err := memory.BuildIndex(store, ns, now)
		if err != nil {
			fmt.Fprintf(stderr, "memory: index build failed: %v\n", err)
			return memoryExitError
		}
		fmt.Fprint(stdout, idx.Format())
		return memoryExitOK
	}

	if flags.jsonOut {
		fmt.Fprintln(stdout, `{"namespaces":[]}`)
		return memoryExitOK
	}
	fmt.Fprintln(stdout, "Provide --agent <name> to view a specific namespace index.")
	return memoryExitOK
}

// runMemoryList prints all persisted MemoryEntry records.
func runMemoryList(flags memoryFlags, stdout, stderr io.Writer) int {
	store := memory.NewMemStore()
	inbox := memory.NewInbox(0)
	lc := memory.NewLifecycle(inbox, store)

	if flags.agent != "" && flags.repo != "" && flags.eventType != "" && flags.summary != "" {
		evt := memory.InboxEvent{
			Agent:     flags.agent,
			Repo:      flags.repo,
			EventType: memory.EventType(flags.eventType),
			Summary:   flags.summary,
		}
		_, _ = inbox.Append(evt)
	}

	persisted, _, err := lc.Run(time.Time{})
	if err != nil {
		fmt.Fprintf(stderr, "memory: lifecycle run failed: %v\n", err)
		return memoryExitError
	}

	if flags.jsonOut {
		fmt.Fprintf(stdout, `{"entry_count":%d,"entries":[`, len(persisted))
		for i, e := range persisted {
			if i > 0 {
				fmt.Fprint(stdout, ",")
			}
			fmt.Fprintf(stdout, `{"key":%q,"domain":%q,"created_at":%q}`,
				e.Key, string(e.Domain), e.CreatedAt.Format(time.RFC3339))
		}
		fmt.Fprint(stdout, "]}\n")
		return memoryExitOK
	}

	fmt.Fprintf(stdout, "%d persisted entries\n", len(persisted))
	sort.Slice(persisted, func(i, j int) bool { return persisted[i].Key < persisted[j].Key })
	for _, e := range persisted {
		fmt.Fprintf(stdout, "  %s [%s] created=%s\n",
			e.Key, e.Domain, e.CreatedAt.Format(time.RFC3339))
	}
	return memoryExitOK
}

// runMemoryInboxStatus reports the current inbox state.
func runMemoryInboxStatus(flags memoryFlags, stdout, stderr io.Writer) int {
	inbox := memory.NewInbox(0)

	// Optionally append a demo event if flags are provided
	if flags.agent != "" && flags.repo != "" && flags.eventType != "" && flags.summary != "" {
		evt := memory.InboxEvent{
			Agent:     flags.agent,
			Repo:      flags.repo,
			EventType: memory.EventType(flags.eventType),
			Summary:   flags.summary,
		}
		_, _ = inbox.Append(evt)
	}

	count := inbox.Count()
	snap := inbox.Snapshot()

	if flags.jsonOut {
		first, last := "", ""
		if count > 0 {
			first = snap[0].ID
			last = snap[len(snap)-1].ID
		}
		fmt.Fprintf(stdout, `{"event_count":%d,"first_id":%q,"last_id":%q,"batch_window":%q}`+"\n",
			count, first, last, memory.BatchWindow)
		return memoryExitOK
	}

	fmt.Fprintf(stdout, "Inbox: %d events\n", count)
	fmt.Fprintf(stdout, "  batch_window: %v\n", memory.BatchWindow)
	fmt.Fprintf(stdout, "  default_max_batch_size: %d\n", memory.DefaultMaxBatchSize)
	if count > 0 {
		fmt.Fprintf(stdout, "  first: %s (%s)\n", snap[0].ID, snap[0].ReceivedAt.Format(time.RFC3339))
		fmt.Fprintf(stdout, "  last:  %s (%s)\n", snap[len(snap)-1].ID, snap[len(snap)-1].ReceivedAt.Format(time.RFC3339))
	}
	return memoryExitOK
}

// runMemoryWithDryRun wires the global --dry-run flag through. Memory
// subcommands are operational inspectors — dry-run has no separate effect
// since lifecycle execution is idempotent against the in-memory store.
func runMemoryWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	_ = globalDryRun // no destructive ops
	rc := runMemory(args, stdout, stderr)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}
