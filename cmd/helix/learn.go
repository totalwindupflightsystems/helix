// Command helix — learn.go
//
// `helix learn skills` wraps pkg/learning.SkillRegistry: publish, list,
// show, and deprecate transferable agent skills (Phase 12 §12.2).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/learning"
)

const (
	learnExitOK    = 0
	learnExitError = 2
)

// learnFlags holds parsed flags for helix learn skills subcommands.
type learnFlags struct {
	// top-level: "skills"
	resource string
	// skills subcommand: list|publish|deprecate|show
	subcommand string

	// list
	domain   string
	minTrust float64
	// publish
	name        string
	promptFile  string
	prompt      string
	author      string
	trust       float64
	merges      int
	version     string
	description string
	// deprecate / show
	skillID string
	reason  string

	skillsPath string
	jsonOut    bool
	dryRun     bool
}

func runLearnWithDryRun(args []string, stdout, stderr io.Writer, dryRun bool) int {
	f, ok := parseLearnFlags(args, stderr)
	if !ok {
		return learnExitError
	}
	f.dryRun = dryRun

	if f.resource != "skills" {
		fmt.Fprintf(stderr, "learn: unknown resource %q (available: skills)\n", f.resource)
		printLearnUsage(stderr)
		return learnExitError
	}

	switch f.subcommand {
	case "list":
		return runLearnSkillsList(f, stdout, stderr)
	case "publish":
		return runLearnSkillsPublish(f, stdout, stderr)
	case "deprecate":
		return runLearnSkillsDeprecate(f, stdout, stderr)
	case "show":
		return runLearnSkillsShow(f, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "learn skills: unknown subcommand %q\n", f.subcommand)
		printLearnUsage(stderr)
		return learnExitError
	}
}

func parseLearnFlags(args []string, stderr io.Writer) (learnFlags, bool) {
	var f learnFlags
	f.minTrust = 0
	f.trust = -1 // unset sentinel
	f.merges = -1

	if len(args) == 0 {
		printLearnUsage(stderr)
		return f, false
	}

	f.resource = args[0]
	if f.resource == "help" || f.resource == "-h" || f.resource == "--help" {
		printLearnUsage(stderr)
		return f, false
	}
	if f.resource != "skills" {
		return f, true
	}

	if len(args) < 2 {
		printLearnUsage(stderr)
		return f, false
	}
	f.subcommand = args[1]
	if f.subcommand == "help" || f.subcommand == "-h" || f.subcommand == "--help" {
		printLearnUsage(stderr)
		return f, false
	}

	i := 2
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			printLearnUsage(stderr)
			return f, false
		case arg == "--json":
			f.jsonOut = true
		case arg == "--domain":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "learn: --domain requires a value")
				return f, false
			}
			i++
			f.domain = args[i]
		case arg == "--min-trust":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "learn: --min-trust requires a value")
				return f, false
			}
			i++
			var v float64
			if _, err := fmt.Sscanf(args[i], "%f", &v); err != nil {
				fmt.Fprintf(stderr, "learn: invalid --min-trust %q\n", args[i])
				return f, false
			}
			f.minTrust = v
		case arg == "--name":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "learn: --name requires a value")
				return f, false
			}
			i++
			f.name = args[i]
		case arg == "--prompt-file":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "learn: --prompt-file requires a value")
				return f, false
			}
			i++
			f.promptFile = args[i]
		case arg == "--prompt":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "learn: --prompt requires a value")
				return f, false
			}
			i++
			f.prompt = args[i]
		case arg == "--author":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "learn: --author requires a value")
				return f, false
			}
			i++
			f.author = args[i]
		case arg == "--trust":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "learn: --trust requires a value")
				return f, false
			}
			i++
			var v float64
			if _, err := fmt.Sscanf(args[i], "%f", &v); err != nil {
				fmt.Fprintf(stderr, "learn: invalid --trust %q\n", args[i])
				return f, false
			}
			f.trust = v
		case arg == "--merges":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "learn: --merges requires a value")
				return f, false
			}
			i++
			var v int
			if _, err := fmt.Sscanf(args[i], "%d", &v); err != nil {
				fmt.Fprintf(stderr, "learn: invalid --merges %q\n", args[i])
				return f, false
			}
			f.merges = v
		case arg == "--version":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "learn: --version requires a value")
				return f, false
			}
			i++
			f.version = args[i]
		case arg == "--description":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "learn: --description requires a value")
				return f, false
			}
			i++
			f.description = args[i]
		case arg == "--reason":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "learn: --reason requires a value")
				return f, false
			}
			i++
			f.reason = args[i]
		case arg == "--skills-path":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "learn: --skills-path requires a value")
				return f, false
			}
			i++
			f.skillsPath = args[i]
		default:
			if strings.HasPrefix(arg, "--") {
				fmt.Fprintf(stderr, "learn: unknown flag %q\n", arg)
				return f, false
			}
			// positional skill-id for show/deprecate
			if f.skillID == "" {
				f.skillID = arg
			} else {
				fmt.Fprintf(stderr, "learn: unexpected argument %q\n", arg)
				return f, false
			}
		}
		i++
	}
	return f, true
}

func openSkillRegistry(f learnFlags) (*learning.SkillRegistry, error) {
	path := f.skillsPath
	if path == "" {
		path = learning.DefaultSkillsPath()
	}
	// Expand leading ~/
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	store, err := learning.NewFileSkillStore(path)
	if err != nil {
		return nil, err
	}
	return learning.NewSkillRegistry(store), nil
}

func runLearnSkillsList(f learnFlags, stdout, stderr io.Writer) int {
	if f.dryRun {
		fmt.Fprintf(stdout, "[dry-run] would list skills domain=%q min-trust=%.2f\n", f.domain, f.minTrust)
		return learnExitOK
	}
	reg, err := openSkillRegistry(f)
	if err != nil {
		fmt.Fprintf(stderr, "learn skills list: %v\n", err)
		return learnExitError
	}
	skills, err := reg.List(f.domain, f.minTrust)
	if err != nil {
		fmt.Fprintf(stderr, "learn skills list: %v\n", err)
		return learnExitError
	}

	if f.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(skills)
		return learnExitOK
	}
	if len(skills) == 0 {
		fmt.Fprintln(stdout, "No skills found.")
		return learnExitOK
	}
	fmt.Fprintf(stdout, "%-24s %-12s %-8s %6s %7s %s\n",
		"ID", "DOMAIN", "TRUST", "USAGE", "SUCCESS", "NAME")
	fmt.Fprintln(stdout, strings.Repeat("-", 90))
	for _, sk := range skills {
		dep := ""
		if sk.Deprecated {
			dep = " [deprecated]"
		}
		fmt.Fprintf(stdout, "%-24s %-12s %7.2f %6d %6.0f%% %s%s\n",
			truncateSkillID(sk.ID, 24), sk.Domain, sk.TrustWeight, sk.UsageCount,
			sk.SuccessRate*100, sk.Name, dep)
	}
	return learnExitOK
}

func runLearnSkillsPublish(f learnFlags, stdout, stderr io.Writer) int {
	if f.name == "" {
		fmt.Fprintln(stderr, "learn skills publish: --name is required")
		return learnExitError
	}
	if f.domain == "" {
		fmt.Fprintln(stderr, "learn skills publish: --domain is required")
		return learnExitError
	}
	if f.author == "" {
		fmt.Fprintln(stderr, "learn skills publish: --author is required")
		return learnExitError
	}
	if f.trust < 0 {
		fmt.Fprintln(stderr, "learn skills publish: --trust is required")
		return learnExitError
	}
	if f.merges < 0 {
		fmt.Fprintln(stderr, "learn skills publish: --merges is required")
		return learnExitError
	}

	prompt := f.prompt
	if f.promptFile != "" {
		data, err := os.ReadFile(f.promptFile)
		if err != nil {
			fmt.Fprintf(stderr, "learn skills publish: read --prompt-file: %v\n", err)
			return learnExitError
		}
		prompt = string(data)
	}
	if strings.TrimSpace(prompt) == "" {
		fmt.Fprintln(stderr, "learn skills publish: --prompt-file or --prompt is required")
		return learnExitError
	}

	if f.dryRun {
		fmt.Fprintf(stdout, "[dry-run] would publish skill name=%q domain=%q author=%q trust=%.2f merges=%d\n",
			f.name, f.domain, f.author, f.trust, f.merges)
		return learnExitOK
	}

	reg, err := openSkillRegistry(f)
	if err != nil {
		fmt.Fprintf(stderr, "learn skills publish: %v\n", err)
		return learnExitError
	}

	skill := learning.Skill{
		Name:          f.name,
		Domain:        f.domain,
		Description:   f.description,
		Prompt:        prompt,
		AuthorAgentID: f.author,
		Version:       f.version,
	}
	if err := reg.Publish(f.author, f.trust, f.merges, skill); err != nil {
		fmt.Fprintf(stderr, "learn skills publish: %v\n", err)
		return learnExitError
	}

	// Re-list to find the published skill by name (fresh ID).
	list, err := reg.List(f.domain, 0)
	if err != nil {
		fmt.Fprintf(stderr, "learn skills publish: published but list failed: %v\n", err)
		return learnExitError
	}
	var published *learning.Skill
	for i := range list {
		if list[i].Name == f.name && list[i].AuthorAgentID == f.author {
			published = &list[i]
			break
		}
	}
	if published == nil && len(list) > 0 {
		published = &list[0]
	}

	if f.jsonOut && published != nil {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(published)
		return learnExitOK
	}
	if published != nil {
		fmt.Fprintf(stdout, "Published skill %s (%s) domain=%s trust_weight=%.2f\n",
			published.ID, published.Name, published.Domain, published.TrustWeight)
	} else {
		fmt.Fprintf(stdout, "Published skill %q in domain %s\n", f.name, f.domain)
	}
	return learnExitOK
}

func runLearnSkillsDeprecate(f learnFlags, stdout, stderr io.Writer) int {
	if f.skillID == "" {
		fmt.Fprintln(stderr, "learn skills deprecate: <skill-id> is required")
		return learnExitError
	}
	if f.reason == "" {
		fmt.Fprintln(stderr, "learn skills deprecate: --reason is required")
		return learnExitError
	}
	if f.dryRun {
		fmt.Fprintf(stdout, "[dry-run] would deprecate skill %s reason=%q\n", f.skillID, f.reason)
		return learnExitOK
	}
	reg, err := openSkillRegistry(f)
	if err != nil {
		fmt.Fprintf(stderr, "learn skills deprecate: %v\n", err)
		return learnExitError
	}
	if err := reg.Deprecate(f.skillID, f.reason); err != nil {
		fmt.Fprintf(stderr, "learn skills deprecate: %v\n", err)
		return learnExitError
	}
	if f.jsonOut {
		_ = json.NewEncoder(stdout).Encode(map[string]string{
			"id":     f.skillID,
			"status": "deprecated",
			"reason": f.reason,
		})
		return learnExitOK
	}
	fmt.Fprintf(stdout, "Deprecated skill %s: %s\n", f.skillID, f.reason)
	return learnExitOK
}

func runLearnSkillsShow(f learnFlags, stdout, stderr io.Writer) int {
	if f.skillID == "" {
		fmt.Fprintln(stderr, "learn skills show: <skill-id> is required")
		return learnExitError
	}
	if f.dryRun {
		fmt.Fprintf(stdout, "[dry-run] would show skill %s\n", f.skillID)
		return learnExitOK
	}
	reg, err := openSkillRegistry(f)
	if err != nil {
		fmt.Fprintf(stderr, "learn skills show: %v\n", err)
		return learnExitError
	}
	sk, err := reg.Get(f.skillID)
	if err != nil {
		fmt.Fprintf(stderr, "learn skills show: %v\n", err)
		return learnExitError
	}
	if f.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(sk)
		return learnExitOK
	}
	fmt.Fprintf(stdout, "ID:           %s\n", sk.ID)
	fmt.Fprintf(stdout, "Name:         %s\n", sk.Name)
	fmt.Fprintf(stdout, "Version:      %s\n", sk.Version)
	fmt.Fprintf(stdout, "Author:       %s\n", sk.AuthorAgentID)
	fmt.Fprintf(stdout, "Domain:       %s\n", sk.Domain)
	fmt.Fprintf(stdout, "Description:  %s\n", sk.Description)
	fmt.Fprintf(stdout, "Trust Weight: %.3f\n", sk.TrustWeight)
	fmt.Fprintf(stdout, "Usage Count:  %d\n", sk.UsageCount)
	fmt.Fprintf(stdout, "Success Rate: %.1f%%\n", sk.SuccessRate*100)
	fmt.Fprintf(stdout, "Deprecated:   %v\n", sk.Deprecated)
	if sk.DeprecationReason != "" {
		fmt.Fprintf(stdout, "Deprecation:  %s\n", sk.DeprecationReason)
	}
	if len(sk.EvidenceTags) > 0 {
		fmt.Fprintf(stdout, "Evidence:     %s\n", strings.Join(sk.EvidenceTags, ", "))
	}
	fmt.Fprintf(stdout, "Created:      %s\n", sk.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(stdout, "Updated:      %s\n", sk.UpdatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintln(stdout, "--- Prompt ---")
	fmt.Fprintln(stdout, sk.Prompt)
	return learnExitOK
}

func truncateSkillID(id string, n int) string {
	if len(id) <= n {
		return id
	}
	return id[:n-3] + "..."
}

func printLearnUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: helix learn skills <subcommand> [flags]

Subcommands:
  list        List skills [--domain DOMAIN] [--min-trust 0.7] [--json]
  publish     Publish a skill (Trusted tier ≥0.65, ≥5 domain merges)
              --name NAME --domain DOMAIN --prompt-file PATH|--prompt TEXT
              --author AGENT-ID --trust 0.8 --merges 8
              [--description TEXT] [--version SEMVER] [--json]
  deprecate   Deprecate a skill <skill-id> --reason "..."
  show        Show skill detail <skill-id> [--json]

Flags:
  --skills-path PATH   JSON store path (default: ~/.helix/skills/skills.json)
  --json               JSON output
`)
}
