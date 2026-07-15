// Command helix is the unified CLI entry point for the Helix platform.
//
// It wraps all sub-CLIs (helix-identity, helix-estimate, helix-negotiate,
// helix-prompt, helix-marketplace, sandbox) behind a single binary with
// consistent global flags and version reporting.
//
// Subcommands:
//
//	helix identity    → delegates to helix-identity
//	helix estimate    → delegates to helix-estimate
//	helix negotiate   → delegates to helix-negotiate
//	helix prompt      → delegates to helix-prompt
//	helix marketplace → delegates to helix-marketplace
//	helix sandbox     → delegates to sandbox
//	helix version     → prints the current version
//	helix status      → checks all component health
//
// Global flags:
//
//	--verbose   enable verbose output
//	--config    path to config file (default: ~/.helix/config.yaml)
//	--dry-run   preview commands without executing
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	bannerPkg "github.com/totalwindupflightsystems/helix/pkg/banner"
)

const (
	// Version is the current Helix version. Overridden at build time via
	// -ldflags "-X main.Version=$(git describe --tags)".
	Version = "0.1.0-dev"
	// AppName is the canonical CLI name.
	AppName = "helix"
)

// ---------------------------------------------------------------------------
// Global flags
// ---------------------------------------------------------------------------

var (
	verbose    bool
	cfgFile    string
	dryRun     bool
	logFmt     string // --log-format flag (defaults to HELIX_LOG_FORMAT or "text")
	showBanner bool   // --banner flag, opt-in to print the ASCII banner before subcommand
)

// ---------------------------------------------------------------------------
// Subcommand registry
// ---------------------------------------------------------------------------

// subcommand maps a subcommand name to the binary it delegates to.
var subcommands = map[string]string{
	"identity":    "helix-identity",
	"estimate":    "helix-estimate",
	"negotiate":   "helix-negotiate",
	"prompt":      "helix-prompt",
	"marketplace": "helix-marketplace",
	"sandbox":     "sandbox",
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	_ = cfgFile // reserved for --config flag in Phase 2

	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// rootCmd builds the root cobra-free command dispatcher. We use a simple
// flag-based approach to avoid pulling in cobra as a dependency for this
// thin wrapper.
func rootCmd() *dispatcher {
	d := &dispatcher{
		usage: func() {
			printUsage()
		},
	}
	return d
}

// ---------------------------------------------------------------------------
// Dispatcher
// ---------------------------------------------------------------------------

type dispatcher struct {
	usage func()
}

// Execute parses os.Args and dispatches to the appropriate subcommand.
func (d *dispatcher) Execute() error {
	return d.dispatch(os.Args[1:])
}

// dispatch parses the given args and dispatches to the appropriate subcommand.
// Exported for testing — callers should use Execute() in production.
func (d *dispatcher) dispatch(args []string) error {

	// Parse global flags
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--verbose":
			verbose = true
		case "--config":
			if i+1 < len(args) {
				cfgFile = args[i+1]
				i++
			} else {
				return fmt.Errorf("--config requires a value")
			}
		case "--dry-run":
			dryRun = true
		case "--log-format":
			if i+1 < len(args) {
				logFmt = args[i+1]
				i++
			} else {
				return fmt.Errorf("--log-format requires a value (text|json)")
			}
		case "--banner":
			// Opt-in: print the ASCII art banner to stdout before the
			// subcommand dispatches. Most CI scripts shouldn't see the
			// banner (it adds noise to grep-able output), so this is
			// explicitly NOT on by default.
			showBanner = true
		case "--help", "-h":
			d.usage()
			return nil
		case "--version":
			printVersion()
			return nil
		default:
			filtered = append(filtered, args[i])
		}
	}

	if len(filtered) == 0 {
		d.usage()
		return nil
	}

	// Set up structured observability AFTER the global flags have been
	// parsed (not in main()) so the --log-format flag actually takes
	// effect. We do this before any subcommand dispatch so every
	// invocation emits exactly one final structured log line.
	// Idempotent across dispatch() calls — first call wins; subsequent
	// calls are no-ops so a re-entrant dispatch (e.g. from tests)
	// doesn't reset the logger.
	if helixLog == nil {
		if _, err := initHelixLog(logFmt); err != nil {
			fmt.Fprintf(os.Stderr, "helix: failed to initialise logger: %v\n", err)
			return err
		}
	}

	name := filtered[0]
	rest := filtered[1:]

	// If --banner was set, emit the ASCII art banner to stdout before
	// dispatching the subcommand. Done here (not in the subcommand
	// handlers) so the banner prefixes every invocation uniformly.
	if showBanner {
		fmt.Fprint(os.Stdout, bannerPkg.Render(Version))
	}

	// Handle built-in commands
	switch name {
	case "version":
		printVersion()
		return nil
	case "banner":
		// The `helix banner` subcommand itself. Delegates to the
		// banner handler so `--compact` etc. work uniformly.
		return RunWithObs("banner", func() error {
			rc := runBanner(rest, os.Stdout, os.Stderr)
			if rc != 0 {
				return errExit{code: rc}
			}
			return nil
		})
	case "status":
		// `helix status --serve [--addr :9095]` starts the long-running
		// HTTP /metrics server. Detect the flag before delegating to the
		// one-shot status runner.
		if hasStatusServeFlag(rest) {
			return RunWithObs("status-serve", func() error {
				return runStatusServeCLI(rest, os.Stdout, os.Stderr)
			})
		}
		return RunWithObs("status", func() error {
			return runStatusWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "doctor":
		// The --suggest flag is opt-in. With it, every failing check
		// gets operator-facing remediation hints from
		// pkg/health.RemediationRegistry. Exit code is 0 if all known,
		// 1 if any failing check has no known remediation (ambiguous).
		if hasDoctorSuggest(rest) {
			return RunWithObs("doctor-suggest", func() error {
				rc := runDoctorSuggest(parseDoctorSuggestFlags(rest), os.Stdout, os.Stderr)
				if rc != 0 {
					return errExit{code: rc}
				}
				return nil
			})
		}
		return RunWithObs("doctor", func() error {
			ec := runDoctorWithConfig(parseDoctorFlags(rest), os.Stdout)
			if ec != nil {
				return errExit{code: 1}
			}
			return nil
		})
	case "dispatch":
		// The global --dry-run flag (parsed in dispatch() above) is
		// honoured by every subcommand. Thread it into the dispatch
		// handler explicitly so dispatch's --dry-run flag isn't shadowed
		// by the global parser.
		return RunWithObs("dispatch", func() error {
			return runDispatchWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "coapproval":
		return RunWithObs("coapproval", func() error {
			return runCoapprovalWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "adversarial":
		return RunWithObs("adversarial", func() error {
			return runAdversarialWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "secrets":
		return RunWithObs("secrets", func() error {
			return runSecretsWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "pipeline":
		// `helix pipeline <run|show|validate>` wires the unified CLI to
		// pkg/coordinator.PRLifecycleCoordinator. The global --dry-run flag
		// is honoured for the `run` subcommand by runPipeline itself (it
		// defaults to stub subsystems, so dry-run has no separate effect
		// — both modes skip downstream services).
		return RunWithObs("pipeline", func() error {
			rc := runPipeline(rest, os.Stdout, os.Stderr)
			if rc != 0 {
				return errExit{code: rc}
			}
			return nil
		})
	case "webhook":
		// `helix webhook <serve>` starts the Forgejo webhook ingestion
		// server (see pkg/webhook). The global --dry-run flag is
		// ignored — there's nothing to dry-run for an HTTP listener.
		return RunWithObs("webhook", func() error {
			rc := runWebhook(rest, os.Stdout, os.Stderr)
			if rc != 0 {
				return errExit{code: rc}
			}
			return nil
		})
	case "incident":
		// `helix incident <declare|list|show|update|stats>` exposes
		// pkg/security.IncidentResponseEngine to operators (spec §6.7).
		return RunWithObs("incident", func() error {
			rc := runIncident(rest, os.Stdout, os.Stderr)
			if rc != 0 {
				return errExit{code: rc}
			}
			return nil
		})
	case "config":
		// `helix config <env-check|...>` exposes configuration-validation
		// utilities. Currently only `env-check` (spec §9.6) is wired.
		// The global --dry-run flag has no separate effect — env-check
		// is itself a read-only validator and dry-run semantics apply
		// implicitly.
		return RunWithObs("config", func() error {
			rc := runConfig(rest, os.Stdout, os.Stderr)
			if rc != 0 {
				return errExit{code: rc}
			}
			return nil
		})
	case "alerts":
		// `helix alerts <notify|list-rules|help>` wires the
		// pkg/health.AlertEngine + Notifier pipeline (spec §8.4).
		// The global --dry-run flag is threaded through so operators
		// can preview alert evaluations without dispatching.
		return RunWithObs("alerts", func() error {
			return runAlertsWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "retry":
		// `helix retry <status|chaos|reset>` exposes the retry layer's
		// circuit breaker state and chaos injection (spec §14.1/§14.3).
		// The global --dry-run flag has no separate effect on status/reset
		// (both are read-only); chaos mode requires HELIX_CHAOS_ENABLED=1.
		return RunWithObs("retry", func() error {
			return runRetryWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "backup":
		// `helix backup <status|validate>` exposes the backup strategy
		// targets and compliance checks (spec §10.1).
		return RunWithObs("backup", func() error {
			return runBackupWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "degradation":
		// `helix degradation <list|check>` exposes the graceful degradation
		// policy registry (spec §14.2).
		return RunWithObs("degradation", func() error {
			return runDegradationWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "audit":
		// `helix audit <trace|steps|validate>` exposes the 12-step audit
		// chain checker (spec §6.5).
		return RunWithObs("audit", func() error {
			return runAuditWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "identity":
		// `helix identity rotate-keys` is a built-in — intercept before
		// delegating to the external helix-identity binary. All other
		// `helix identity <sub>` commands fall through to the binary.
		if len(rest) > 0 && rest[0] == "rotate-keys" {
			return RunWithObs("identity-rotate-keys", func() error {
				return runRotateKeysWithDryRun(rest[1:], os.Stdout, os.Stderr, dryRun)
			})
		}
	case "api":
		// `helix api <serve|contracts|validate|services>` exposes the
		// 5-service API contract schema (spec §15).
		return RunWithObs("api", func() error {
			return runAPIWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "integration":
		// `helix integration <test|list>` runs integration tests
		// with service-reachability skip guards (spec §12.3, §4).
		return RunWithObs("integration", func() error {
			return runIntegrationWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "trust":
		// `helix trust <show|history|list>` queries the trust ledger
		// for agent trust scores and tier history (spec trust-model.md).
		return RunWithObs("trust", func() error {
			return runTrustWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "forgejo":
		// `helix forgejo <ping|user-list|user-info|pr-list|branch-list>`
		// exposes pkg/forgejo REST adapter for operator inspection
		// (spec cross-component-wiring.md §2).
		return RunWithObs("forgejo", func() error {
			return runForgejoWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "review":
		// `helix review <strip-bias|fp-stats|fp-record|evidence|custody>`
		// exposes pkg/review BiasStripper, FPTracker, and EvidenceBundle
		// subsystems (spec adversarial-review.md).
		return RunWithObs("review", func() error {
			return runReviewWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "dispatcher":
		// `helix dispatcher <status|tick|list-tasks>` inspects and drives
		// the pkg/dispatcher Ralph Loop engine (task decomposition,
		// assignment, cost guard) — distinct from `helix dispatch` which
		// runs the full spec→PR pipeline.
		return RunWithObs("dispatcher", func() error {
			return runDispatcherWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "mergegate":
		// `helix mergegate <check|checks>` runs the pre-merge validation
		// gate composing all 5 Helix quality checks (spec adversarial-review.md,
		// production-verification.md, trust-model.md).
		return RunWithObs("mergegate", func() error {
			return runMergeGateWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "lifecycle":
		// `helix lifecycle <run|stages>` runs the PR lifecycle coordinator
		// pipeline (spec cross-component-wiring.md §1-§5).
		return RunWithObs("lifecycle", func() error {
			return runLifecycleWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "verify":
		// `helix verify <shadow|canary|contract>` covers production
		// verification operations (spec production-verification.md).
		return RunWithObs("verify", func() error {
			return runVerifyWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "security":
		// `helix security <check|checklist>` runs the deployment security
		// hardening checklist (spec §6.6).
		return RunWithObs("security", func() error {
			return runSecurityWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "forcemerge":
		// `helix forcemerge <record|review|report|path>` records and
		// inspects force-merge override audit entries (spec SPECIFICATION.md
		// §5.4 + §6.6). The override defeats the co-approval invariant so
		// every use must be logged with a justification and reviewed.
		return RunWithObs("forcemerge", func() error {
			return runForceMergeWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "vuln":
		// `helix vuln <scan|parse|list>` runs the dependency vulnerability
		// scanner (spec SPECIFICATION.md §6.6). Wraps govulncheck, npm
		// audit, and pip-audit behind a unified operator surface.
		return RunWithObs("vuln", func() error {
			return runVulnWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "deploy":
		// `helix deploy <render|list|tiers>` exposes the declarative
		// deployment registries (agent compose / caddy vhosts / systemd
		// units) for operator inspection (spec §8).
		return RunWithObs("deploy", func() error {
			return runDeployWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "ci":
		// `helix ci <render|validate|defaults>` exposes the Forgejo Actions
		// workflow generator/validator (spec §12.5).
		return RunWithObs("ci", func() error {
			return runCIWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "recovery":
		// `helix recovery <matrix|lookup|components|scenarios|key-rotation|scaling|severity>`
		// exposes pkg/recovery runbook registry + DR scenario catalog (spec §14.1, §10.3).
		return RunWithObs("recovery", func() error {
			return runRecoveryWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "memory":
		// `helix memory <append|compile|run|index|list|inbox-status>` exposes
		// the Hivemind memory bank lifecycle pipeline (spec §8.6).
		return RunWithObs("memory", func() error {
			return runMemoryWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "idea":
		// `helix idea <capture|list|show|validate|prioritize|promote|close|advocate>`
		// Phase 1 ideation: capture → offline validate → prioritize → promote to spec.
		return RunWithObs("idea", func() error {
			return runIdeaWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "adr":
		// `helix adr <create|list|show|review|supersede>`
		// ADR co-authoring with multi-model review (Phase 2 §2.2).
		return RunWithObs("adr", func() error {
			return runAdrWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "spec":
		// `helix spec <create|review|gap-analysis|approve|show|list>`
		// Spec co-authoring with adversarial annotation + 12-dimension
		// completeness scoring (Phase 2 §2.1).
		return RunWithObs("spec", func() error {
			return runSpecWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "design":
		// `helix design <review>`
		// Design review with adversarial agents (Phase 2 §2.3).
		return RunWithObs("design", func() error {
			return runDesignWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "contract":
		// `helix contract <create|validate|freeze|diff|consumer-check|list|show>`
		// API contract generation and breaking change detection (Phase 2 §2.4).
		return RunWithObs("contract", func() error {
			return runContractWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
		})
	case "notify":
		// `helix notify <publish|inbox|subscribe|unsubscribe|stream>`
		// Cross-agent notification bus for structured findings with
		// domain subscriptions and budget-tracked inbox delivery
		// (Phase 12 §12.3).
		return RunWithObs("notify", func() error {
			rc := runNotifyWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
			if rc != 0 {
				return errExit{code: rc}
			}
						return nil
					})
				case "models":
					// `helix models <list|show|evaluate|rotate>`
					// Model evaluation and rotation — per-model production outcome
					// tracking with FP rate and incident rate rotation rules
					// (Phase 12 §12.4).
					return RunWithObs("models", func() error {
						rc := runModelsWithDryRun(rest, os.Stdout, os.Stderr, dryRun)
						if rc != 0 {
							return errExit{code: rc}
						}
						return nil
					})
				}

	// Delegate to subcommand binary
	binary, ok := subcommands[name]
	if !ok {
		return fmt.Errorf("unknown subcommand %q\n\nAvailable subcommands: %s",
			name, strings.Join(sortedKeys(subcommands), ", "))
	}

	return execSubcommand(binary, rest)
}

// ---------------------------------------------------------------------------
// Built-in commands
// ---------------------------------------------------------------------------

func printVersion() {
	fmt.Printf("%s %s (%s/%s)\n", AppName, Version, runtime.GOOS, runtime.GOARCH)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func execSubcommand(binary string, args []string) error {
	// Find binary in PATH
	binPath, err := lookPath(binary)
	if err != nil {
		return fmt.Errorf("subcommand %q not found (%s).\n\nInstall it with:\n  cd cmd/%s && go build -o %s\n  sudo mv %s /usr/local/bin/\n",
			binary, binary, binary, binary, binary)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] executing: %s %s\n", binPath, strings.Join(args, " "))
	}
	if dryRun {
		fmt.Printf("[DRY RUN] %s %s\n", binPath, strings.Join(args, " "))
		return nil
	}

	cmd := exec.Command(binPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// urlToAddr extracts the host:port from a URL for TCP dialing.
// (Kept for callers that still want a quick TCP probe — not currently
// used by the unified status path which goes through pkg/health.)
func urlToAddr(rawURL string) string {
	// Minimal URL parsing without net/url to avoid import cycles
	rawURL = strings.TrimPrefix(rawURL, "http://")
	rawURL = strings.TrimPrefix(rawURL, "https://")
	// Strip path
	if idx := strings.Index(rawURL, "/"); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	// Add default port if missing
	if !strings.Contains(rawURL, ":") {
		rawURL = rawURL + ":80"
	}
	return rawURL
}

func lookPath(name string) (string, error) {
	// Prefer project-local binaries
	localPaths := []string{
		filepath.Join(".", name),
		filepath.Join("cmd", name, name),
	}
	for _, p := range localPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	// Fall back to PATH
	return exec.LookPath(name)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple sort
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func printUsage() {
	fmt.Printf(`%s — Helix Agent-First Software Development Platform

Usage: %s [global-flags] <subcommand> [subcommand-args]

Subcommands:
  identity     Provision agent accounts in Forgejo
  estimate     Estimate task cost before execution
  negotiate    Negotiate task contracts between agents
  prompt       Manage and attest prompt files
  marketplace  Search and discover agents
  sandbox      Run commands in a sandboxed environment
  version      Print version information
  banner       Print the HELIX ASCII art banner
  status       Check all component health
  doctor       Run platform diagnostic checks
  dispatch     Dispatch a spec to an agent for execution
  dispatcher   Inspect and drive the Ralph Loop engine (status/tick/list-tasks)
  review       Adversarial review + change management dashboard
  verify       Production verification (shadow/canary/contract)
  trust        Query trust ledger snapshots
  mergegate    Pre-merge validation gate (5 quality checks)
  security     Deployment security hardening checklist
  forgejo      Inspect the connected Forgejo instance
  coapproval   Run human+agent co-approval protocol
  adversarial  Run adversarial review
  secrets      Inspect and rotate secrets
  pipeline     Run PR lifecycle coordinator
  lifecycle    PR lifecycle coordinator stages
  webhook      Run Forgejo webhook ingestion server
  incident     Declare, list, and resolve incidents (spec §6.7)
  config       Validate configuration (env-check)
  alerts       Alert engine notify/list-rules
  retry        Retry layer circuit breaker / chaos
  backup       Backup strategy status/validate
  degradation  Graceful degradation policy registry
  audit        12-step audit chain checker
  api          API contract schema surface
  integration  Integration tests with skip guards
  forcemerge   Force-merge override audit entries (spec §5.4)
  vuln         Dependency vulnerability scanner (spec §6.6)
  deploy       Render deployment artifacts (agent compose / caddy / systemd)
  ci           Generate/validate Forgejo Actions workflow (spec §12.5)
  recovery     Error-recovery runbook + DR scenarios (spec §14.1, §10.3)
  memory       Hivemind memory bank lifecycle (spec §8.6)
  idea         Idea capture, validation, prioritization (Phase 1)
  adr          Architecture Decision Records + multi-model review (Phase 2)
  spec         Spec co-authoring with adversarial annotation (Phase 2)
  design       Design review with adversarial agents (Phase 2)
  contract     API contract generation + breaking changes (Phase 2)
  notify       Cross-agent notification bus — publish, inbox, stream (Phase 12)
  models       Model evaluation and rotation — list, show, evaluate, rotate (Phase 12)

Global Flags:
  --verbose   Enable verbose output
  --config    Path to config file (default: ~/.helix/config.yaml)
  --dry-run   Preview commands without executing
  --help      Show this help message
  --version   Show version

Examples:
  %s identity sync
  %s estimate --task "Write a Go HTTP server"
  %s marketplace search --capability go
  %s review dashboard --pr 42 --files pkg/trust/ledger.go --repo .
  %s status
`, AppName, AppName, AppName, AppName, AppName, AppName, AppName)
}
