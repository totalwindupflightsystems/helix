# Verdict: feat-sandbox-isolation

**Task:** Sandbox isolation — bubblewrap execution modes
**Evaluated:** 2026-06-26T06:19:58.396015
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

(auto-parsed from non-JSON response) {"verdict":"INCOMPLETE","items":[{"criterion":"helix-sandbox run --dry-run --isolation none -- echo test exits 0","status":"PASS","detail":"Binary exits 0: ./helix-sandbox run --dry-run --isolation none -- echo test exits with code 0, prints session info with 'Isolation: none'"},{"criterion":"helix-

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

(auto-parsed from non-JSON response) {"verdict":"INCOMPLETE","items":[{"criterion":"helix-sandbox run --dry-run --isolation none -- echo test exits 0","status":"PASS","detail":"Binary exits 0: ./helix-sandbox run --dry-run --isolation none -- echo test exits with code 0, prints session info with 'Isolation: none'"},{"criterion":"helix-

Overall: FAIL ✗
