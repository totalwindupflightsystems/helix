# Verdict: df-009-mergegate-fail-closed

**Task:** DF-009: mergegate fails CLOSED when changed files cannot be collected
**Evaluated:** 2026-08-03T20:27:53.674662
**Result:** ✓ PASS

## Pipeline Stages

- ✓ **tier1**
  -   ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ lint: Traceback (most recent call last):
  File "<string>", line 1, in <module>
    from engine.guard_mana
  ✓ secrets: Traceback (most recent call last):
  File "<string>", line 1, in <module>
    from engine.guard_mana
  ✓ tests: Traceback (most recent call last):
  File "<string>", line 1, in <module>
    from engine.guard_mana
  ✓ build: Traceback (most recent call last):
  File "<string>", line 1, in <module>
    from engine.guard_mana
- ✓ **tier2**
  - COMPLETE
  ✓ pkg/mergegate/hook.go: collectChangedFiles error on an update push to a protected branch sets Allowed=false (fail closed) with an explicit internal-error reason — no error path may set Allowed=true: hook.go (commit 9330b62) lines 238-247: on collectChangedFiles error for update push, result.Allowed=false, result.Skipped=false, reason 'could not collect changed files: %v (internal error — push blocked, gate failed closed)'. Only dry-run path sets Allowed=true but with 'would reject' reason (reporting mode, not approval). TestEvaluateHookCollectErrorFailsClosed verifies Allowed=false and REJECTED on stderr.
  ✓ The old approve-on-error path ("allowing — likely a new branch") is gone; no reason string contains 'allowing' for an internal error: Old path removed; new error reason is 'internal error — push blocked, gate failed closed'. Test asserts !strings.Contains(r.Reason, 'allowing'). grep confirms no 'allowing' in any reason string.
  ✓ Genuine new-branch pushes (ref.IsCreate()) skip the gate with Skipped=true and a clear new-branch reason — distinct from Allowed=true (gate applied and passed): hook.go lines 228-234: ref.IsCreate() sets Skipped=true, Allowed=true, reason 'new branch creation (no prior state to gate), skipping gate'. Distinct from gate-applied-and-passed (Skipped=false, reason 'all gate checks passed'). TestEvaluateHookNewBranchSkipsGate verifies.
  ✓ Output distinguishes 'skipped' from 'allowed' (e.g. dry-run reports 'would reject' instead of allowing): hook.go line 242 dry-run path reports 'dry-run: would reject — could not collect changed files: %v (internal error)'. TestEvaluateHookSkippedVsAllowedDistinction verifies skipped (new branch) vs allowed (update passed) distinction in output within a single invocation.
  ✓ pkg/mergegate tests pass: new tests cover (a) collect error on update push -> REJECTED, (b) new-branch skip path, (c) skipped-vs-allowed distinction; go test -count=1 ./pkg/mergegate/... green: Tests TestEvaluateHookCollectErrorFailsClosed (line 482), TestEvaluateHookNewBranchSkipsGate (line 529), TestEvaluateHookSkippedVsAllowedDistinction (line 572) cover all three cases. go test -count=1 ./pkg/mergegate/... passes (ok, 1.958s).
  ✓ Commit 9330b62 exists with trailers Co-authored-by: Alexis Okuwa <wojonstech@gmail.com> and Prompt: prompts/coding-hermes/v1.md; go build ./... and gitreins guard PASS: Commit 9330b623250682e3bc1e7c668e3cc3dd16a643b6 exists (HEAD) with trailers 'Co-authored-by: Alexis Okuwa <wojonstech@gmail.com>' and 'Prompt: prompts/coding-hermes/v1.md'. go build ./... passes. gitreins guard checks all pass: go test ./pkg/... green, go vet clean, no secrets, no LSP diagnostics.
All 6 criteria verified PASS: mergegate now fails closed on collect errors, new-branch pushes skip via ref.IsCreate(), skipped vs allowed distinguished in output, all three new tests pass, and commit 9330b62 with required trailers and passing guard exists.

## Summary

Judge Result: df-009-mergegate-fail-closed

Stage tier1: PASS
    ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ lint: Traceback (most recent call last):
  File "<string>", line 1, in <module>
    from engine.guard_mana
  ✓ secrets: Traceback (most recent call last):
  File "<string>", line 1, in <module>
    from engine.guard_mana
  ✓ tests: Traceback (most recent call last):
  File "<string>", line 1, in <module>
    from engine.guard_mana
  ✓ build: Traceback (most recent call last):
  File "<string>", line 1, in <module>
    from engine.guard_mana

Stage tier2: PASS
  COMPLETE
  ✓ pkg/mergegate/hook.go: collectChangedFiles error on an update push to a protected branch sets Allowed=false (fail closed) with an explicit internal-error reason — no error path may set Allowed=true: hook.go (commit 9330b62) lines 238-247: on collectChangedFiles error for update push, result.Allowed=false, result.Skipped=false, reason 'could not collect changed files: %v (internal error — push blocked, gate failed closed)'. Only dry-run path sets Allowed=true but with 'would reject' reason (reporting mode, not approval). TestEvaluateHookCollectErrorFailsClosed verifies Allowed=false and REJECTED on stderr.
  ✓ The old approve-on-error path ("allowing — likely a new branch") is gone; no reason string contains 'allowing' for an internal error: Old path removed; new error reason is 'internal error — push blocked, gate failed closed'. Test asserts !strings.Contains(r.Reason, 'allowing'). grep confirms no 'allowing' in any reason string.
  ✓ Genuine new-branch pushes (ref.IsCreate()) skip the gate with Skipped=true and a clear new-branch reason — distinct from Allowed=true (gate applied and passed): hook.go lines 228-234: ref.IsCreate() sets Skipped=true, Allowed=true, reason 'new branch creation (no prior state to gate), skipping gate'. Distinct from gate-applied-and-passed (Skipped=false, reason 'all gate checks passed'). TestEvaluateHookNewBranchSkipsGate verifies.
  ✓ Output distinguishes 'skipped' from 'allowed' (e.g. dry-run reports 'would reject' instead of allowing): hook.go line 242 dry-run path reports 'dry-run: would reject — could not collect changed files: %v (internal error)'. TestEvaluateHookSkippedVsAllowedDistinction verifies skipped (new branch) vs allowed (update passed) distinction in output within a single invocation.
  ✓ pkg/mergegate tests pass: new tests cover (a) collect error on update push -> REJECTED, (b) new-branch skip path, (c) skipped-vs-allowed distinction; go test -count=1 ./pkg/mergegate/... green: Tests TestEvaluateHookCollectErrorFailsClosed (line 482), TestEvaluateHookNewBranchSkipsGate (line 529), TestEvaluateHookSkippedVsAllowedDistinction (line 572) cover all three cases. go test -count=1 ./pkg/mergegate/... passes (ok, 1.958s).
  ✓ Commit 9330b62 exists with trailers Co-authored-by: Alexis Okuwa <wojonstech@gmail.com> and Prompt: prompts/coding-hermes/v1.md; go build ./... and gitreins guard PASS: Commit 9330b623250682e3bc1e7c668e3cc3dd16a643b6 exists (HEAD) with trailers 'Co-authored-by: Alexis Okuwa <wojonstech@gmail.com>' and 'Prompt: prompts/coding-hermes/v1.md'. go build ./... passes. gitreins guard checks all pass: go test ./pkg/... green, go vet clean, no secrets, no LSP diagnostics.
All 6 criteria verified PASS: mergegate now fails closed on collect errors, new-branch pushes skip via ref.IsCreate(), skipped vs allowed distinguished in output, all three new tests pass, and commit 9330b62 with required trailers and passing guard exists.

Overall: PASS ✓
