# Verdict: GAP-041

**Task:** Deprecate dispatch/dispatcher duplicate: one canonical name
**Evaluated:** 2026-08-24T05:21:21.418616
**Result:** ✓ PASS

## Pipeline Stages

- ✓ **tier1**
  -   ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ build: 
  ✓ tests: 
  ✓ lint: 
  ✓ secrets: 
- ✓ **tier2**
  - COMPLETE
  ✓ PASS: 'helix --help' shows at most ONE dispatch* entry (the canonical command), and invoking the deprecated one prints a DEPRECATED banner while still working. Follow the GAP-040 adversarial pattern: keep the switch case alive, remove from builtinSubcommands, update cli_surface_test.go core/hidden lists, all tests pass.: (1) `helix --help` shows exactly ONE dispatch* entry: `dispatch` (canonical) — /tmp/helix-test --help | grep -i dispatch returns only "dispatch ... canonical (dispatcher is deprecated)". (2) `helix dispatcher help` prints "DEPRECATED: 'helix dispatcher' is deprecated — use 'helix dispatch'..." to stderr (dispatcher.go:175) and still works (exit 0, dispatches to subcommand logic). (3) Switch case "dispatcher" kept alive at main.go:441. (4) "dispatcher" removed from builtinSubcommands (main.go:84-95). (5) cli_surface_test.go core list removed "dispatcher" (lines 60-61) and hidden list added "dispatcher" (line 79). (6) Tests pass: `go test -short -count=1 ./...` in cmd/helix → "ok github.com/.../cmd/helix 6.653s"; TestCLIHelp → ok; config test_command `go test -short -count=1 ./pkg/...` → all ok; `go build` exit 0; LSP diagnostics empty.
GAP-041 fully implemented: dispatcher deprecated in favor of canonical dispatch, hidden from --help, DEPRECATED banner printed while still functional, switch case alive, builtinSubcommands and cli_surface_test.go updated, and all tests pass.

## Summary

Judge Result: GAP-041

Stage tier1: PASS
    ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ build: 
  ✓ tests: 
  ✓ lint: 
  ✓ secrets: 

Stage tier2: PASS
  COMPLETE
  ✓ PASS: 'helix --help' shows at most ONE dispatch* entry (the canonical command), and invoking the deprecated one prints a DEPRECATED banner while still working. Follow the GAP-040 adversarial pattern: keep the switch case alive, remove from builtinSubcommands, update cli_surface_test.go core/hidden lists, all tests pass.: (1) `helix --help` shows exactly ONE dispatch* entry: `dispatch` (canonical) — /tmp/helix-test --help | grep -i dispatch returns only "dispatch ... canonical (dispatcher is deprecated)". (2) `helix dispatcher help` prints "DEPRECATED: 'helix dispatcher' is deprecated — use 'helix dispatch'..." to stderr (dispatcher.go:175) and still works (exit 0, dispatches to subcommand logic). (3) Switch case "dispatcher" kept alive at main.go:441. (4) "dispatcher" removed from builtinSubcommands (main.go:84-95). (5) cli_surface_test.go core list removed "dispatcher" (lines 60-61) and hidden list added "dispatcher" (line 79). (6) Tests pass: `go test -short -count=1 ./...` in cmd/helix → "ok github.com/.../cmd/helix 6.653s"; TestCLIHelp → ok; config test_command `go test -short -count=1 ./pkg/...` → all ok; `go build` exit 0; LSP diagnostics empty.
GAP-041 fully implemented: dispatcher deprecated in favor of canonical dispatch, hidden from --help, DEPRECATED banner printed while still functional, switch case alive, builtinSubcommands and cli_surface_test.go updated, and all tests pass.

Overall: PASS ✓
