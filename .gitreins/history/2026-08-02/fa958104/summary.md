# Verdict: DF-001

**Task:** mergegate hook exits non-zero on REJECT — pre-receive gate blocks pushes
**Evaluated:** 2026-08-02T12:15:10.640719
**Result:** ✓ PASS

## Pipeline Stages

- ✓ **tier1**
  -   ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ lint: 
  ✓ build: 
  ✓ tests: 
  ✓ secrets: 
- ✓ **tier2**
  - COMPLETE
  ✓ helix mergegate hook exits 1 when any ref is rejected (live repro: branch-deletion push prints REJECTED, exit code 1): cmd/helix/mergegate.go:399-425 runMergeGateHook returns mgExitBlock(1) when EvaluateHook errors; runMergeGateWithDryRun:380-383 surfaces non-zero rc as errExit; main.go:82-90 os.Exit(ee.code). Test TestRunMergeGateWithDryRun_Blocked (mergegate_test.go:296-306) asserts mgExitBlock + REJECTED on stderr.
  ✓ helix mergegate hook --dry-run still exits 0 on a rejecting ref (dry-run must not block): pkg/mergegate/hook.go:207-214 branch deletion with cfg.DryRun sets Skipped=true,Allowed=true; runMergeGateWithDryRun:377-378 re-applies --dry-run from globalDryRun. Test TestRunMergeGateWithDryRun_BlockedDryRunReturnsNil (mergegate_test.go:308-315) asserts NoError + ALLOWED.
  ✓ main() honors errExit codes (documented contract 0 allowed / 1 blocked / 2 invocation error): cmd/helix/main.go:82-90 uses errors.As(err,&ee) and os.Exit(ee.code); errExit type defined dispatch.go:173-175; codes mgExitOK=0/mgExitBlock=1/mgExitError=2 (mergegate.go:22-24); help text mergegate.go:174-176 documents 0 ALLOWED/1 BLOCKED/2 invocation error.
  ✓ integration test TestPreReceiveHookBlocksRejectedPush: real bare repo + scripts/helix-pre-receive.sh + built binary; push to protected main is REJECTED, push to feature/x succeeds: pkg/mergegate/pre_receive_integration_test.go:75 TestPreReceiveHookBlocksRejectedPush builds real binary via TestMain (go build ./cmd/helix), installs scripts/helix-pre-receive.sh as bare repo pre-receive hook, push to protected main REJECTED (lines 156-159), push to feature/x succeeds (lines 170-173). Ran: --- PASS (0.23s), not skipped.
  ✓ full suite green: TMPDIR=/home/kara/.cache/go-tmp go test -short -count=1 ./... (60/60 packages): Command returned 60 'ok' packages with exit code 0; pkg/mergegate ok 3.855s.
All 5 criteria verified: mergegate hook exits 1 on REJECT, dry-run exits 0, main() honors errExit codes, the real-bare-repo integration test passes, and the full suite is green at 60/60 packages.

## Summary

Judge Result: DF-001

Stage tier1: PASS
    ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ lint: 
  ✓ build: 
  ✓ tests: 
  ✓ secrets: 

Stage tier2: PASS
  COMPLETE
  ✓ helix mergegate hook exits 1 when any ref is rejected (live repro: branch-deletion push prints REJECTED, exit code 1): cmd/helix/mergegate.go:399-425 runMergeGateHook returns mgExitBlock(1) when EvaluateHook errors; runMergeGateWithDryRun:380-383 surfaces non-zero rc as errExit; main.go:82-90 os.Exit(ee.code). Test TestRunMergeGateWithDryRun_Blocked (mergegate_test.go:296-306) asserts mgExitBlock + REJECTED on stderr.
  ✓ helix mergegate hook --dry-run still exits 0 on a rejecting ref (dry-run must not block): pkg/mergegate/hook.go:207-214 branch deletion with cfg.DryRun sets Skipped=true,Allowed=true; runMergeGateWithDryRun:377-378 re-applies --dry-run from globalDryRun. Test TestRunMergeGateWithDryRun_BlockedDryRunReturnsNil (mergegate_test.go:308-315) asserts NoError + ALLOWED.
  ✓ main() honors errExit codes (documented contract 0 allowed / 1 blocked / 2 invocation error): cmd/helix/main.go:82-90 uses errors.As(err,&ee) and os.Exit(ee.code); errExit type defined dispatch.go:173-175; codes mgExitOK=0/mgExitBlock=1/mgExitError=2 (mergegate.go:22-24); help text mergegate.go:174-176 documents 0 ALLOWED/1 BLOCKED/2 invocation error.
  ✓ integration test TestPreReceiveHookBlocksRejectedPush: real bare repo + scripts/helix-pre-receive.sh + built binary; push to protected main is REJECTED, push to feature/x succeeds: pkg/mergegate/pre_receive_integration_test.go:75 TestPreReceiveHookBlocksRejectedPush builds real binary via TestMain (go build ./cmd/helix), installs scripts/helix-pre-receive.sh as bare repo pre-receive hook, push to protected main REJECTED (lines 156-159), push to feature/x succeeds (lines 170-173). Ran: --- PASS (0.23s), not skipped.
  ✓ full suite green: TMPDIR=/home/kara/.cache/go-tmp go test -short -count=1 ./... (60/60 packages): Command returned 60 'ok' packages with exit code 0; pkg/mergegate ok 3.855s.
All 5 criteria verified: mergegate hook exits 1 on REJECT, dry-run exits 0, main() honors errExit codes, the real-bare-repo integration test passes, and the full suite is green at 60/60 packages.

Overall: PASS ✓
