# Verdict: feat-sandbox-isolation

**Task:** Sandbox isolation — bubblewrap execution modes
**Evaluated:** 2026-06-26T06:23:11.976882
**Result:** ✗ FAIL

## Pipeline Stages

- ✓ **tier1**
  -   ✓ lsp: 
  ✓ lint: 
  ✓ build: 
  ✓ tests: 
  ✓ secrets: 
- ✗ **tier2**
  - INCOMPLETE
  ✓ helix-sandbox run --dry-run --isolation none -- echo test exits 0: Binary exits 0: /tmp/helix-sandbox run --dry-run --isolation none -- echo test exits with code 0, prints session info with 'Isolation: none'
  ✓ helix-sandbox run --dry-run --isolation workspace -- echo test exits 0: Binary exits 0: /tmp/helix-sandbox run --dry-run --isolation workspace -- echo test exits with code 0, prints session info with 'Isolation: workspace'
  ✓ helix-sandbox run --dry-run --isolation full -- echo test exits 0: Binary exits 0: /tmp/helix-sandbox run --dry-run --isolation full -- echo test exits with code 0, prints session info with 'Isolation: full'
  ✗ Invalid isolation level (e.g., 'invalid') exits 1 with error message: Binary exits with code 2 (not 1) for invalid isolation: 'error: sandbox: configuration is invalid: unknown isolation level "invalid"' — exit code is 2 instead of 1
  ✗ Missing command after -- exits 1 with usage: Binary exits with code 2 (not 1) when no command specified: 'error: no command specified after '--'' with usage text — exit code is 2 instead of 1
  ✓ helix-sandbox --help prints usage with all flags and exits 0: Binary exits 0: /tmp/helix-sandbox --help exits with code 0 and prints full usage with all flags (dry-run, isolation, memory-limit, network, session-id, time-limit, verbose, workdir) and subcommands (run, help, version)
4 of 6 criteria pass; 2 fail because invalid isolation and missing command exit with code 2 instead of 1 as specified

## Summary

Judge Result: feat-sandbox-isolation

Stage tier1: PASS
    ✓ lsp: 
  ✓ lint: 
  ✓ build: 
  ✓ tests: 
  ✓ secrets: 

Stage tier2: FAIL
  INCOMPLETE
  ✓ helix-sandbox run --dry-run --isolation none -- echo test exits 0: Binary exits 0: /tmp/helix-sandbox run --dry-run --isolation none -- echo test exits with code 0, prints session info with 'Isolation: none'
  ✓ helix-sandbox run --dry-run --isolation workspace -- echo test exits 0: Binary exits 0: /tmp/helix-sandbox run --dry-run --isolation workspace -- echo test exits with code 0, prints session info with 'Isolation: workspace'
  ✓ helix-sandbox run --dry-run --isolation full -- echo test exits 0: Binary exits 0: /tmp/helix-sandbox run --dry-run --isolation full -- echo test exits with code 0, prints session info with 'Isolation: full'
  ✗ Invalid isolation level (e.g., 'invalid') exits 1 with error message: Binary exits with code 2 (not 1) for invalid isolation: 'error: sandbox: configuration is invalid: unknown isolation level "invalid"' — exit code is 2 instead of 1
  ✗ Missing command after -- exits 1 with usage: Binary exits with code 2 (not 1) when no command specified: 'error: no command specified after '--'' with usage text — exit code is 2 instead of 1
  ✓ helix-sandbox --help prints usage with all flags and exits 0: Binary exits 0: /tmp/helix-sandbox --help exits with code 0 and prints full usage with all flags (dry-run, isolation, memory-limit, network, session-id, time-limit, verbose, workdir) and subcommands (run, help, version)
4 of 6 criteria pass; 2 fail because invalid isolation and missing command exit with code 2 instead of 1 as specified

Overall: FAIL ✗
