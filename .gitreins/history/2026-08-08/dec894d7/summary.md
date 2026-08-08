# Verdict: GITLEAKS-FIXTURE-ANNOTATE

**Task:** Allowlist identity test fixtures in .gitleaks.toml
**Evaluated:** 2026-08-08T19:43:56.206039
**Result:** ✓ PASS

## Pipeline Stages

- ✓ **tier1**
  -   ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ build: 
  ✓ lint: 
  ✓ secrets: 
  ✓ tests: 
- ✓ **tier2**
  - COMPLETE
  ✓ pkg/identity/syncer_test.go, extra_coverage_test.go, provisioner_http_test.go are exempted via .gitleaks.toml [allowlist] paths (guard builtin scanner skips them when staged): .gitleaks.toml line 20: '''pkg/identity/(syncer|extra_coverage|provisioner_http)_test\.go''' matches all 3 fixture files (verified via grep); all 3 test files exist in pkg/identity/. GR-GAP-005 builtin scanner reads this allowlist via _load_gitleaks_allowlist to skip them when staged.
  ✓ The .gitleaks.toml allowlist entry is scoped to pkg/identity/(syncer|extra_coverage|provisioner_http)_test\.go — no broad docs/specs/markdown re-narrowing: Allowlist paths list contains only \.git/, \.gitreins/, \.gitreins/history/, .*\.log$, vendor/, and the scoped identity pattern. No docs/specs/md patterns. Verified regex does NOT match docs/guide.md, other test files, or non-test source. Tick #109 narrowing preserved.
  ✓ gitreins guard passes 4/4 with the change staged (secrets clean, build ok, lint ok, tests ok): Secrets scan: no secret patterns in .gitleaks.toml (grep exit 1). Build: go build ./... exit 0. Lint: go vet ./... exit 0. Tests: go test -count=1 ./... 60/60 ok. Commit message confirms guard PASS 4/4 (diff mode, full-suite safety trigger).
  ✓ Full test suite passes (go test -count=1 -timeout 300s ./... → 60/60 packages ok): go test -count=1 -timeout 300s ./... with TMPDIR=/home/kara/.cache/go-tmp → 60/60 packages ok, 0 FAIL (output /tmp/testout2.txt).
All 4 criteria verified: .gitleaks.toml allowlist scoped to the 3 identity fixture files, no broad docs/specs/md patterns, guard 4/4 passes, and full test suite 60/60 packages ok.

## Summary

Judge Result: GITLEAKS-FIXTURE-ANNOTATE

Stage tier1: PASS
    ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ build: 
  ✓ lint: 
  ✓ secrets: 
  ✓ tests: 

Stage tier2: PASS
  COMPLETE
  ✓ pkg/identity/syncer_test.go, extra_coverage_test.go, provisioner_http_test.go are exempted via .gitleaks.toml [allowlist] paths (guard builtin scanner skips them when staged): .gitleaks.toml line 20: '''pkg/identity/(syncer|extra_coverage|provisioner_http)_test\.go''' matches all 3 fixture files (verified via grep); all 3 test files exist in pkg/identity/. GR-GAP-005 builtin scanner reads this allowlist via _load_gitleaks_allowlist to skip them when staged.
  ✓ The .gitleaks.toml allowlist entry is scoped to pkg/identity/(syncer|extra_coverage|provisioner_http)_test\.go — no broad docs/specs/markdown re-narrowing: Allowlist paths list contains only \.git/, \.gitreins/, \.gitreins/history/, .*\.log$, vendor/, and the scoped identity pattern. No docs/specs/md patterns. Verified regex does NOT match docs/guide.md, other test files, or non-test source. Tick #109 narrowing preserved.
  ✓ gitreins guard passes 4/4 with the change staged (secrets clean, build ok, lint ok, tests ok): Secrets scan: no secret patterns in .gitleaks.toml (grep exit 1). Build: go build ./... exit 0. Lint: go vet ./... exit 0. Tests: go test -count=1 ./... 60/60 ok. Commit message confirms guard PASS 4/4 (diff mode, full-suite safety trigger).
  ✓ Full test suite passes (go test -count=1 -timeout 300s ./... → 60/60 packages ok): go test -count=1 -timeout 300s ./... with TMPDIR=/home/kara/.cache/go-tmp → 60/60 packages ok, 0 FAIL (output /tmp/testout2.txt).
All 4 criteria verified: .gitleaks.toml allowlist scoped to the 3 identity fixture files, no broad docs/specs/md patterns, guard 4/4 passes, and full test suite 60/60 packages ok.

Overall: PASS ✓
