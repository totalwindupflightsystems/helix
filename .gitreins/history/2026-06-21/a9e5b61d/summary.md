# Verdict: qc-build

**Task:** Helix project builds and passes static checks
**Evaluated:** 2026-06-21T12:41:54.049595
**Result:** ✗ FAIL

## Pipeline Stages

- ✓ **tier1**
  -   ✓ lint: 
  ✓ build: 
  ✓ tests: 
  ✓ secrets: 
- ✗ **tier2**
  - INCOMPLETE
  ✓ go build ./cmd/... exits 0 — all CLI binaries compile: Run at /home/kara/helix, exit code 0, no errors
  ✓ go vet ./... exits 0 — no warnings or errors: Run at /home/kara/helix, exit code 0, no output
  ✗ go test -short -count=1 ./... exits 0 — all tests pass: go test -short -count=1 -timeout 30s ./pkg/identity/... times out (>30s). Root cause: TestProvisioner_Stubs subtests (CreateUser_real_returns_network_error, RegisterKey_real_returns_network_error, etc.) at /home/kara/helix/pkg/identity/types_test.go:1241 make real HTTP requests to https://forgejo.example.com without checking testing.Short(), triggering doWithRetry with exponential backoff (1s,2s,4s sleep). CLI commands ./cmd/..., ./pkg/estimate, ./pkg/prompt, ./pkg/sandbox, ./pkg/negotiate, ./pkg/marketplace all pass.
  ✓ Binary files produced: helix-identity, sandbox (and helix-estimate/negotiate/prompt/marketplace when implemented): All 6 binaries exist in repo root: helix-identity (7179612 bytes), sandbox (3086473 bytes), helix-estimate (4654379 bytes), helix-negotiate (9833021 bytes), helix-prompt (5212979 bytes), helix-marketplace (4795302 bytes)
3 of 4 criteria pass; Criterion 3 fails because TestProvisioner_Stubs in pkg/identity/types_test.go makes real network calls without testing.Short() guard, causing the test suite to hang beyond the timeout.

## Summary

Judge Result: qc-build

Stage tier1: PASS
    ✓ lint: 
  ✓ build: 
  ✓ tests: 
  ✓ secrets: 

Stage tier2: FAIL
  INCOMPLETE
  ✓ go build ./cmd/... exits 0 — all CLI binaries compile: Run at /home/kara/helix, exit code 0, no errors
  ✓ go vet ./... exits 0 — no warnings or errors: Run at /home/kara/helix, exit code 0, no output
  ✗ go test -short -count=1 ./... exits 0 — all tests pass: go test -short -count=1 -timeout 30s ./pkg/identity/... times out (>30s). Root cause: TestProvisioner_Stubs subtests (CreateUser_real_returns_network_error, RegisterKey_real_returns_network_error, etc.) at /home/kara/helix/pkg/identity/types_test.go:1241 make real HTTP requests to https://forgejo.example.com without checking testing.Short(), triggering doWithRetry with exponential backoff (1s,2s,4s sleep). CLI commands ./cmd/..., ./pkg/estimate, ./pkg/prompt, ./pkg/sandbox, ./pkg/negotiate, ./pkg/marketplace all pass.
  ✓ Binary files produced: helix-identity, sandbox (and helix-estimate/negotiate/prompt/marketplace when implemented): All 6 binaries exist in repo root: helix-identity (7179612 bytes), sandbox (3086473 bytes), helix-estimate (4654379 bytes), helix-negotiate (9833021 bytes), helix-prompt (5212979 bytes), helix-marketplace (4795302 bytes)
3 of 4 criteria pass; Criterion 3 fails because TestProvisioner_Stubs in pkg/identity/types_test.go makes real network calls without testing.Short() guard, causing the test suite to hang beyond the timeout.

Overall: FAIL ✗
