// Command helix — notify.go
//
// `helix notify` wraps pkg/learning.ContextBus: cross-agent notification
// bus with domain subscriptions, budget tracking, and stream delivery
// (Phase 12 §12.3).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/learning"
)

const (
	notifyExitOK    = 0
	notifyExitError = 2
)

// notifyFlags holds parsed flags for helix notify subcommands.
type notifyFlags struct {
	subcommand string
	from       string
	to         string
	domain     string
	finding    string
	evidence   string
	priority   string
	agent      string
	tier       string
	domains    string
	unreadOnly bool
	jsonOut    bool
	dryRun     bool
	interval   int // stream poll interval in seconds
}

func runNotifyWithDryRun(args []string, stdout, stderr io.Writer, dryRun bool) int {
	f, ok := parseNotifyFlags(args, stderr)
	if !ok {
		return notifyExitError
	}
	f.dryRun = dryRun

	switch f.subcommand {
	case "publish", "share":
		return runNotifyPublish(f, stdout, stderr)
	case "inbox":
		return runNotifyInbox(f, stdout, stderr)
	case "subscribe":
		return runNotifySubscribe(f, stdout, stderr)
	case "unsubscribe":
		return runNotifyUnsubscribe(f, stdout, stderr)
	case "stream":
		return runNotifyStream(f, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "notify: unknown subcommand %q\n", f.subcommand)
		fmt.Fprintf(stderr, "Available: publish, inbox, subscribe, unsubscribe, stream\n")
		return notifyExitError
	}
}

func parseNotifyFlags(args []string, stderr io.Writer) (notifyFlags, bool) {
	var f notifyFlags

	if len(args) == 0 {
		printNotifyUsage(stderr)
		return f, false
	}

	f.subcommand = args[0]
	i := 1
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			printNotifyUsage(stderr)
			return f, false
		case arg == "--json":
			f.jsonOut = true
		case arg == "--unread-only":
			f.unreadOnly = true
		case arg == "--from":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "notify: --from requires a value")
				return f, false
			}
			i++
			f.from = args[i]
		case arg == "--to":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "notify: --to requires a value")
				return f, false
			}
			i++
			f.to = args[i]
		case arg == "--domain":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "notify: --domain requires a value")
				return f, false
			}
			i++
			f.domain = args[i]
		case arg == "--finding":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "notify: --finding requires a value")
				return f, false
			}
			i++
			f.finding = args[i]
		case arg == "--evidence":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "notify: --evidence requires a value")
				return f, false
			}
			i++
			f.evidence = args[i]
		case arg == "--priority":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "notify: --priority requires a value")
				return f, false
			}
			i++
			f.priority = args[i]
		case arg == "--agent":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "notify: --agent requires a value")
				return f, false
			}
			i++
			f.agent = args[i]
		case arg == "--tier":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "notify: --tier requires a value")
				return f, false
			}
			i++
			f.tier = args[i]
		case arg == "--domains":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "notify: --domains requires a value")
				return f, false
			}
			i++
			f.domains = args[i]
		case arg == "--interval":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "notify: --interval requires a value (seconds)")
				return f, false
			}
			i++
			fmt.Sscanf(args[i], "%d", &f.interval)
		default:
			fmt.Fprintf(stderr, "notify: unknown flag %q\n", arg)
			return f, false
		}
		i++
	}
	return f, true
}

// ─────────────────────────────────────────────────────────────────────────────
// Subcommand: publish
// ─────────────────────────────────────────────────────────────────────────────

func runNotifyPublish(f notifyFlags, stdout, stderr io.Writer) int {
	if f.from == "" {
		fmt.Fprintln(stderr, "notify publish: --from (agent ID) is required")
		return notifyExitError
	}
	if f.domain == "" {
		fmt.Fprintln(stderr, "notify publish: --domain is required (valid: auth, database, api, infra, security, testing, docs)")
		return notifyExitError
	}
	if f.finding == "" {
		fmt.Fprintln(stderr, "notify publish: --finding (description) is required")
		return notifyExitError
	}

	priority := learning.Priority(f.priority)
	if f.priority == "" {
		priority = learning.PriorityInfo
	}

	var evidenceLinks []string
	if f.evidence != "" {
		evidenceLinks = strings.Split(f.evidence, ",")
	}

	cb, err := learning.NewContextBus("")
	if err != nil {
		fmt.Fprintf(stderr, "notify publish: %v\n", err)
		return notifyExitError
	}

	sf, err := cb.Publish(f.from, f.to, learning.Domain(f.domain), f.finding, evidenceLinks, priority)
	if err != nil {
		fmt.Fprintf(stderr, "notify publish: %v\n", err)
		return notifyExitError
	}

	if f.jsonOut {
		data, _ := json.MarshalIndent(sf, "", "  ")
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintf(stdout, "Published finding %s\n", sf.ID)
		fmt.Fprintf(stdout, "  From:    %s\n", sf.FromAgentID)
		if sf.ToAgentID != "" {
			fmt.Fprintf(stdout, "  To:      %s\n", sf.ToAgentID)
		}
		fmt.Fprintf(stdout, "  Domain:  %s\n", sf.Domain)
		fmt.Fprintf(stdout, "  Priority: %s\n", sf.Priority)
		fmt.Fprintf(stdout, "  Finding: %s\n", sf.Finding)
		if len(sf.EvidenceLinks) > 0 {
			fmt.Fprintf(stdout, "  Evidence: %s\n", strings.Join(sf.EvidenceLinks, ", "))
		}
	}
	return notifyExitOK
}

// ─────────────────────────────────────────────────────────────────────────────
// Subcommand: inbox
// ─────────────────────────────────────────────────────────────────────────────

func runNotifyInbox(f notifyFlags, stdout, stderr io.Writer) int {
	if f.agent == "" {
		fmt.Fprintln(stderr, "notify inbox: --agent is required")
		return notifyExitError
	}
	if f.tier == "" {
		f.tier = "provisional"
	}

	cb, err := learning.NewContextBus("")
	if err != nil {
		fmt.Fprintf(stderr, "notify inbox: %v\n", err)
		return notifyExitError
	}

	findings, err := cb.GetInbox(f.agent, f.tier, f.unreadOnly)
	if err != nil {
		fmt.Fprintf(stderr, "notify inbox: %v\n", err)
		return notifyExitError
	}

	if f.jsonOut {
		data, _ := json.MarshalIndent(findings, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return notifyExitOK
	}

	budget := learning.DailyBudget(f.tier)
	used := cb.DailyUsage(f.agent)
	fmt.Fprintf(stdout, "Inbox for %s (tier: %s, budget: %d/day, used: %d):\n\n", f.agent, f.tier, budget, used)

	if len(findings) == 0 {
		fmt.Fprintln(stdout, "  (no findings)")
		return notifyExitOK
	}

	for _, sf := range findings {
		icon := "ℹ"
		switch sf.Priority {
		case learning.PriorityCritical:
			icon = "🔴"
		case learning.PriorityWarning:
			icon = "⚠"
		}
		fmt.Fprintf(stdout, "%s [%s] %s — %s\n", icon, sf.Domain, sf.ID[:8], sf.Timestamp.Format(time.RFC3339))
		fmt.Fprintf(stdout, "     From: %s\n", sf.FromAgentID)
		if sf.ToAgentID != "" {
			fmt.Fprintf(stdout, "     To:   %s\n", sf.ToAgentID)
		}
		fmt.Fprintf(stdout, "     %s\n", sf.Finding)
		if len(sf.EvidenceLinks) > 0 {
			fmt.Fprintf(stdout, "     Evidence: %s\n", strings.Join(sf.EvidenceLinks, ", "))
		}
		fmt.Fprintln(stdout)
	}
	return notifyExitOK
}

// ─────────────────────────────────────────────────────────────────────────────
// Subcommand: subscribe
// ─────────────────────────────────────────────────────────────────────────────

func runNotifySubscribe(f notifyFlags, stdout, stderr io.Writer) int {
	if f.agent == "" {
		fmt.Fprintln(stderr, "notify subscribe: --agent is required")
		return notifyExitError
	}
	if f.domains == "" {
		fmt.Fprintln(stderr, "notify subscribe: --domains is required (comma-separated)")
		return notifyExitError
	}

	domainStrs := strings.Split(f.domains, ",")
	domains := make([]learning.Domain, 0, len(domainStrs))
	for _, d := range domainStrs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		domains = append(domains, learning.Domain(d))
	}

	if len(domains) == 0 {
		fmt.Fprintln(stderr, "notify subscribe: no valid domains provided")
		return notifyExitError
	}

	cb, err := learning.NewContextBus("")
	if err != nil {
		fmt.Fprintf(stderr, "notify subscribe: %v\n", err)
		return notifyExitError
	}

	if err := cb.Subscribe(f.agent, domains); err != nil {
		fmt.Fprintf(stderr, "notify subscribe: %v\n", err)
		return notifyExitError
	}

	sub := cb.GetSubscription(f.agent)
	domainNames := make([]string, len(sub.Domains))
	for i, d := range sub.Domains {
		domainNames[i] = string(d)
	}

	if f.jsonOut {
		data, _ := json.MarshalIndent(sub, "", "  ")
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintf(stdout, "Agent %s subscribed to: %s\n", f.agent, strings.Join(domainNames, ", "))
	}
	return notifyExitOK
}

// ─────────────────────────────────────────────────────────────────────────────
// Subcommand: unsubscribe
// ─────────────────────────────────────────────────────────────────────────────

func runNotifyUnsubscribe(f notifyFlags, stdout, stderr io.Writer) int {
	if f.agent == "" {
		fmt.Fprintln(stderr, "notify unsubscribe: --agent is required")
		return notifyExitError
	}

	var domains []learning.Domain
	if f.domains != "" {
		for _, d := range strings.Split(f.domains, ",") {
			d = strings.TrimSpace(d)
			if d != "" {
				domains = append(domains, learning.Domain(d))
			}
		}
	}

	cb, err := learning.NewContextBus("")
	if err != nil {
		fmt.Fprintf(stderr, "notify unsubscribe: %v\n", err)
		return notifyExitError
	}

	if err := cb.Unsubscribe(f.agent, domains); err != nil {
		fmt.Fprintf(stderr, "notify unsubscribe: %v\n", err)
		return notifyExitError
	}

	if f.jsonOut {
		fmt.Fprintf(stdout, `{"agent":"%s","unsubscribed":true}`+"\n", f.agent)
	} else {
		if len(domains) == 0 {
			fmt.Fprintf(stdout, "Agent %s unsubscribed from all domains\n", f.agent)
		} else {
			fmt.Fprintf(stdout, "Agent %s unsubscribed from: %s\n", f.agent, f.domains)
		}
	}
	return notifyExitOK
}

// ─────────────────────────────────────────────────────────────────────────────
// Subcommand: stream
// ─────────────────────────────────────────────────────────────────────────────

func runNotifyStream(f notifyFlags, stdout, stderr io.Writer) int {
	if f.agent == "" {
		fmt.Fprintln(stderr, "notify stream: --agent is required")
		return notifyExitError
	}
	if f.tier == "" {
		f.tier = "provisional"
	}
	if f.interval < 1 {
		f.interval = 2
	}

	cb, err := learning.NewContextBus("")
	if err != nil {
		fmt.Fprintf(stderr, "notify stream: %v\n", err)
		return notifyExitError
	}

	seen := make(map[string]bool)
	// Pre-populate with existing finding IDs so we only show new ones.
	for _, sf := range cb.ListFindings("") {
		seen[sf.ID] = true
	}

	fmt.Fprintf(stdout, "Streaming findings for %s (tier: %s, poll: %ds, ctrl-c to exit)...\n",
		f.agent, f.tier, f.interval)

	for {
		findings, err := cb.GetInbox(f.agent, f.tier, true)
		if err != nil {
			fmt.Fprintf(stderr, "notify stream: %v\n", err)
			return notifyExitError
		}

		for _, sf := range findings {
			if seen[sf.ID] {
				continue
			}
			seen[sf.ID] = true

			tag := fmt.Sprintf("[%s]", sf.ID[:8])
			if sf.Priority == learning.PriorityCritical {
				tag = "[CRITICAL]"
			}
			fmt.Fprintf(stdout, "%s %s %s from %s: %s\n",
				sf.Timestamp.Format("15:04:05"), tag, sf.Domain, sf.FromAgentID, sf.Finding)
		}

		time.Sleep(time.Duration(f.interval) * time.Second)
	}
}

func printNotifyUsage(w io.Writer) {
	fmt.Fprint(w, `helix notify — Cross-agent notification bus (Phase 12 §12.3)

Usage: helix notify <subcommand> [flags]

Subcommands:
  publish     Publish a finding to the notification bus
  inbox       Show findings for an agent
  subscribe   Subscribe an agent to domains
  unsubscribe Remove an agent from domains
  stream      Stream new findings in real time (ctrl-c to exit)

Publish flags:
  --from      Publishing agent ID (required)
  --to        Target agent ID (optional — empty = domain broadcast)
  --domain    Finding domain: auth, database, api, infra, security, testing, docs (required)
  --finding   Structured description of the finding (required)
  --evidence  Comma-separated evidence links (PRs, commits, incidents)
  --priority  Priority: info, warning, critical (default: info)
  --json      JSON output

Inbox flags:
  --agent      Agent ID to query (required)
  --tier       Trust tier: provisional, observed, trusted, veteran (default: provisional)
  --unread-only Show only unconsumed findings
  --json       JSON output

Subscribe flags:
  --agent    Agent ID to subscribe (required)
  --domains  Comma-separated domains (required)

Unsubscribe flags:
  --agent    Agent ID to unsubscribe (required)
  --domains  Comma-separated domains (empty = all)

Stream flags:
  --agent     Agent ID to stream for (required)
  --tier      Trust tier for budget (default: provisional)
  --interval  Poll interval in seconds (default: 2)

Examples:
  helix notify publish --from agent-1 --domain auth --finding "Session refresh fails at UTC midnight" --evidence pr-1234 --priority warning
  helix notify inbox --agent agent-2 --tier trusted --unread-only
  helix notify subscribe --agent agent-1 --domains auth,security,api
  helix notify stream --agent agent-1
`)
}
