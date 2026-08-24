# Verdict: GAP-041b

**Task:** Reopen: dispatch vs dispatcher binary stale
**Evaluated:** 2026-08-24T17:56:50.935301
**Result:** ✗ FAIL

## Pipeline Stages

- ✗ **tier1**
  -   ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✗ lint: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
  ✗ build: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
  ✗ secrets: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
  ✗ tests: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
- ✓ **tier2**
  - COMPLETE
  ✓ PASS: rebuilt binaries from HEAD; ./helix --help lists exactly one dispatch entry (canonical 'dispatch', dispatcher deprecated note) and 'helix dispatcher help' still works with DEPRECATED banner.: Verified via command output: (1) `./helix --help` lists exactly one dispatch entry — `grep -c -i dispatch` returns 1, the line being "dispatch  Run the full spec→PR pipeline (Ralph loop execution) — canonical (dispatcher is deprecated)"; no separate dispatcher subcommand appears. (2) `./helix dispatcher help` exits 0 and prints "DEPRECATED: 'helix dispatcher' is deprecated — use 'helix dispatch' for the full spec→PR pipeline; the dispatcher subcommands (status/tick/list-tasks) remain functional." followed by full help. (3) Binaries rebuilt from HEAD: helix binary timestamp Aug 24 12:52 vs HEAD cd4774f 2026-08-24 12:53:35; working tree only modifies .gitreins/tasks.yaml and .gitreins/usage.jsonl (task tracking), no Go source changes.
The rebuilt helix binary from HEAD lists exactly one dispatch entry (canonical 'dispatch' with dispatcher deprecated note) and 'helix dispatcher help' still works with the DEPRECATED banner.

## Summary

Judge Result: GAP-041b

Stage tier1: FAIL
    ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✗ lint: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
  ✗ build: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
  ✗ secrets: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
  ✗ tests: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo

Stage tier2: PASS
  COMPLETE
  ✓ PASS: rebuilt binaries from HEAD; ./helix --help lists exactly one dispatch entry (canonical 'dispatch', dispatcher deprecated note) and 'helix dispatcher help' still works with DEPRECATED banner.: Verified via command output: (1) `./helix --help` lists exactly one dispatch entry — `grep -c -i dispatch` returns 1, the line being "dispatch  Run the full spec→PR pipeline (Ralph loop execution) — canonical (dispatcher is deprecated)"; no separate dispatcher subcommand appears. (2) `./helix dispatcher help` exits 0 and prints "DEPRECATED: 'helix dispatcher' is deprecated — use 'helix dispatch' for the full spec→PR pipeline; the dispatcher subcommands (status/tick/list-tasks) remain functional." followed by full help. (3) Binaries rebuilt from HEAD: helix binary timestamp Aug 24 12:52 vs HEAD cd4774f 2026-08-24 12:53:35; working tree only modifies .gitreins/tasks.yaml and .gitreins/usage.jsonl (task tracking), no Go source changes.
The rebuilt helix binary from HEAD lists exactly one dispatch entry (canonical 'dispatch' with dispatcher deprecated note) and 'helix dispatcher help' still works with the DEPRECATED banner.

Overall: FAIL ✗
