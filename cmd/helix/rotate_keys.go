package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/identity"
)

// ============================================================================
// helix identity rotate-keys CLI — key rotation lifecycle (spec §5.5, §6.7)
// ============================================================================

const (
	rotateExitOK       = 0
	rotateExitAction   = 1 // rotation needed (non-zero to signal action required)
	rotateExitError    = 2 // invocation error
	rotateExitNotFound = 3 // state file not found
)

// rotateKeysFlags holds parsed CLI flags for the rotate-keys subcommand.
type rotateKeysFlags struct {
	stateFile   string
	jsonOut     bool
	execute     bool
	dryRun      bool
	nowOverride string // --at for testing; ISO 8601 timestamp
}

// stateFileEntry represents a single key entry in the JSON state file.
type stateFileEntry struct {
	AgentName string `json:"agent_name"`
	KeyType   string `json:"key_type"`
	KeyHash   string `json:"key_hash"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Status    string `json:"status"`
}

// stateFileData is the top-level JSON structure of the state file.
type stateFileData struct {
	Keys []stateFileEntry `json:"keys"`
}

// parseRotateKeysFlags parses the args for `helix identity rotate-keys`.
// If --help is found, helpWanted is true and exitCode is rotateExitOK.
func parseRotateKeysFlags(args []string) (flags rotateKeysFlags, helpWanted bool, exitCode int) {
	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			return flags, true, rotateExitOK
		case arg == "--json":
			flags.jsonOut = true
		case arg == "--execute":
			flags.execute = true
		case arg == "--dry-run":
			flags.dryRun = true
		case arg == "--state-file":
			if i+1 < len(args) {
				flags.stateFile = args[i+1]
				i++
			} else {
				return flags, false, rotateExitError
			}
		case arg == "--at":
			if i+1 < len(args) {
				flags.nowOverride = args[i+1]
				i++
			} else {
				return flags, false, rotateExitError
			}
		case strings.HasPrefix(arg, "--"):
			return flags, false, rotateExitError
		default:
			return flags, false, rotateExitError
		}
		i++
	}

	return flags, false, rotateExitOK
}

// printRotateKeysHelp prints the help text for the rotate-keys subcommand.
func printRotateKeysHelp(w io.Writer) {
	fmt.Fprintln(w, `helix identity rotate-keys — key rotation lifecycle (spec §5.5, §6.7)

Usage:
  helix identity rotate-keys --state-file <path> [--json] [--execute] [--dry-run] [--at <timestamp>]

Reads a JSON state file containing key metadata for all agents, evaluates each key
against the spec §5.5 rotation policies, and produces a rotation plan showing which
keys need rotation, why, and the recommended action.

Flags:
  --state-file <path>   Path to JSON file containing key registry state
  --json                Output structured JSON rotation plan
  --execute             Also print executable shell commands for each rotation
                         (does NOT execute them — just outputs the commands)
  --dry-run             Equivalent to running without --execute (read-only)
  --at <timestamp>      Override "now" with a specific ISO 8601 timestamp (for testing)
  --help, -h            Show this help

State file format (JSON):
  {
    "keys": [
      {
        "agent_name": "agent-001",
        "key_type": "ssh",
        "key_hash": "sha256:abcdef...",
        "created_at": "2026-01-15T10:00:00Z",
        "expires_at": "",
        "status": "active"
      }
    ]
  }

Exit codes:
  0  Success (no rotations needed, or plan generated with --execute)
  1  Rotations needed (plan has actions — review before executing)
  2  Invocation error
  3  State file not found`)
}

// runRotateKeys is the entry point for `helix identity rotate-keys`.
func runRotateKeys(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseRotateKeysFlags(args)
	if rc != rotateExitOK {
		return rc
	}
	if helpWanted {
		printRotateKeysHelp(stdout)
		return rotateExitOK
	}

	if flags.stateFile == "" {
		fmt.Fprintf(stderr, "error: rotate-keys requires --state-file <path>\n")
		return rotateExitError
	}

	data, err := os.ReadFile(flags.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(stderr, "error: state file not found: %s\n", flags.stateFile)
			return rotateExitNotFound
		}
		fmt.Fprintf(stderr, "error: reading state file: %v\n", err)
		return rotateExitError
	}

	// Parse the state file JSON
	var sf stateFileData
	if err := json.Unmarshal(data, &sf); err != nil {
		fmt.Fprintf(stderr, "error: parsing state file: %v\n", err)
		return rotateExitError
	}

	// Build the registry from the state file
	registry := identity.NewAgentKeyRegistry()
	skipped := 0
	for _, entry := range sf.Keys {
		kt, ok := parseKeyType(entry.KeyType)
		if !ok {
			skipped++
			continue
		}

		createdAt, err := time.Parse(time.RFC3339, entry.CreatedAt)
		if err != nil {
			skipped++
			continue
		}

		var expiresAt time.Time
		if entry.ExpiresAt != "" {
			expiresAt, err = time.Parse(time.RFC3339, entry.ExpiresAt)
			if err != nil {
				expiresAt = time.Time{}
			}
		}

		registry.RegisterKey(entry.AgentName, kt, entry.KeyHash, createdAt, expiresAt)

		// Apply status overrides
		switch strings.ToLower(entry.Status) {
		case "dead":
			registry.MarkDead(entry.AgentName, kt)
		case "expired":
			if info, ok := registry.GetKey(entry.AgentName, kt); ok {
				info.Status = identity.KeyStatusExpired
				registry.Keys[entry.AgentName+":"+string(kt)] = info
			}
		}
	}

	// Determine "now"
	now := time.Now()
	if flags.nowOverride != "" {
		parsed, err := time.Parse(time.RFC3339, flags.nowOverride)
		if err != nil {
			fmt.Fprintf(stderr, "error: invalid --at timestamp: %v\n", err)
			return rotateExitError
		}
		now = parsed
	}

	// Generate the rotation plan
	policies := identity.DefaultRotationPolicies()
	plan := registry.PlanRotation(policies, now)

	if flags.jsonOut {
		return outputPlanJSON(plan, flags, stdout, stderr)
	}

	return outputPlanText(plan, flags, stdout, stderr)
}

// outputPlanText renders the rotation plan in human-readable text format.
func outputPlanText(plan *identity.RotationPlan, flags rotateKeysFlags, stdout, stderr io.Writer) int {
	fmt.Fprint(stdout, identity.FormatRotationPlan(plan))

	if flags.execute {
		fmt.Fprintln(stdout, "\nExecutable Rotation Commands:")
		fmt.Fprintln(stdout, "=============================")
		for _, action := range plan.Actions {
			cmd := generateRotationCommand(action)
			fmt.Fprintf(stdout, "  [%s] %s\n", action.Urgency, cmd)
		}
		if len(plan.Actions) == 0 {
			fmt.Fprintln(stdout, "  (no rotations needed)")
		}
	}

	if len(plan.Actions) > 0 && !flags.execute {
		fmt.Fprintf(stdout, "\n%d key(s) need rotation. Run with --execute to see commands.\n", len(plan.Actions))
		return rotateExitAction
	}

	return rotateExitOK
}

// outputPlanJSON renders the rotation plan as structured JSON.
func outputPlanJSON(plan *identity.RotationPlan, flags rotateKeysFlags, stdout, stderr io.Writer) int {
	type commandEntry struct {
		AgentName string                   `json:"agent_name"`
		KeyType   string                   `json:"key_type"`
		Command   string                   `json:"command"`
		Urgency   identity.RotationUrgency `json:"urgency"`
	}

	type planOutput struct {
		GeneratedAt time.Time                 `json:"generated_at"`
		ActionCount int                       `json:"action_count"`
		Skipped     int                       `json:"skipped"`
		Actions     []identity.RotationAction `json:"actions"`
		Commands    []commandEntry            `json:"commands,omitempty"`
	}

	out := planOutput{
		GeneratedAt: plan.Generated,
		ActionCount: len(plan.Actions),
		Skipped:     plan.Skipped,
		Actions:     plan.Actions,
	}

	if flags.execute {
		for _, a := range plan.Actions {
			out.Commands = append(out.Commands, commandEntry{
				AgentName: a.AgentName,
				KeyType:   string(a.KeyType),
				Command:   generateRotationCommand(a),
				Urgency:   a.Urgency,
			})
		}
		if out.Commands == nil {
			out.Commands = []commandEntry{}
		}
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return rotateExitError
	}
	fmt.Fprintln(stdout, string(data))

	if len(plan.Actions) > 0 && !flags.execute {
		return rotateExitAction
	}
	return rotateExitOK
}

// generateRotationCommand produces a shell command string for a rotation action.
// It does NOT execute the command — just outputs it for the operator to review.
func generateRotationCommand(action identity.RotationAction) string {
	switch action.KeyType {
	case identity.KeyTypeSSH:
		return fmt.Sprintf("helix-identity keygen --agent %s --type ssh", action.AgentName)
	case identity.KeyTypePAT:
		return fmt.Sprintf("helix-identity pat-create --agent %s", action.AgentName)
	case identity.KeyTypeOpenRouter:
		return fmt.Sprintf("openrouter key create --label %s", action.AgentName)
	default:
		return fmt.Sprintf("# unknown key type %q for agent %s", action.KeyType, action.AgentName)
	}
}

// parseKeyType converts a string to a KeyType. Returns false for unknown types.
func parseKeyType(s string) (identity.KeyType, bool) {
	switch strings.ToLower(s) {
	case "ssh":
		return identity.KeyTypeSSH, true
	case "pat":
		return identity.KeyTypePAT, true
	case "openrouter":
		return identity.KeyTypeOpenRouter, true
	default:
		return "", false
	}
}

// runRotateKeysWithDryRun wraps runRotateKeys with the global --dry-run flag.
func runRotateKeysWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	if globalDryRun {
		hasDryRun := false
		for _, a := range args {
			if a == "--dry-run" {
				hasDryRun = true
				break
			}
		}
		if !hasDryRun {
			args = append([]string{"--dry-run"}, args...)
		}
	}
	rc := runRotateKeys(args, stdout, stderr)
	if rc != 0 && rc != rotateExitAction {
		return errExit{code: rc}
	}
	// rotateExitAction (1) is an expected result, not an error
	return nil
}
