# Verdict: df-017

**Task:** Fix helix status/doctor chimera probe false-down
**Evaluated:** 2026-08-23T00:30:19.242860
**Result:** ✗ FAIL

## Pipeline Stages

- ✗ **tier1**
  -   ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✗ secrets: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
  ✗ lint: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
  ✗ tests: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
  ✗ build: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
- ✓ **tier2**
  - COMPLETE
  ✓ helix status with no flags prints 'Overall: healthy' and exits 0 while chimera liveness is fast but /v1/health readiness is slow; chimera probed at fast endpoint (GET /health or /v1/health/live) not /v1/health; unit tests cover the probe path: Code fix in commit ac568f7. (1) status.go:328 prints 'Overall: healthy' (StateHealthy="healthy" per aggregator.go:32) and renderStatusTable returns 0 when AllHealthy (status.go:364). (2) chimera probed at fast endpoint: pkg/health/checker.go:114 URL=http://localhost:8765/health; cmd/helix/status.go:209-215 all chimera-backed subsystems (chimera/negotiate/trust/review/verify/marketplace/estimate) use /health; cmd/helix/doctor.go:72 ChimeraURL=http://localhost:8765/health. (3) unit tests cover probe path: checker_test.go TestDefaultServices_ChimeraFastLiveness PASS, status_test.go TestDefaultSubsystemProbes_ChimeraFastLiveness PASS, doctor_test.go TestDefaultDoctorConfig asserts /health PASS. Verified via actual runs: 'go test -short -count=1 ./pkg/health/...' -> ok (2.731s); 'go test -short -count=1 ./cmd/helix/' -> ok (9.974s); 'go build ./cmd/helix/ ./pkg/health/' -> exit 0; no LSP diagnostics.
The chimera probe false-down fix is complete: status/doctor/health now probe the fast /health liveness endpoint instead of slow /v1/health, 'Overall: healthy' + exit 0 logic confirmed, and unit tests covering the probe path all pass.

## Summary

Judge Result: df-017

Stage tier1: FAIL
    ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✗ secrets: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
  ✗ lint: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
  ✗ tests: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo
  ✗ build: Traceback (most recent call last):
  File "<string>", line 1, in <module>
ModuleNotFoundError: No mo

Stage tier2: PASS
  COMPLETE
  ✓ helix status with no flags prints 'Overall: healthy' and exits 0 while chimera liveness is fast but /v1/health readiness is slow; chimera probed at fast endpoint (GET /health or /v1/health/live) not /v1/health; unit tests cover the probe path: Code fix in commit ac568f7. (1) status.go:328 prints 'Overall: healthy' (StateHealthy="healthy" per aggregator.go:32) and renderStatusTable returns 0 when AllHealthy (status.go:364). (2) chimera probed at fast endpoint: pkg/health/checker.go:114 URL=http://localhost:8765/health; cmd/helix/status.go:209-215 all chimera-backed subsystems (chimera/negotiate/trust/review/verify/marketplace/estimate) use /health; cmd/helix/doctor.go:72 ChimeraURL=http://localhost:8765/health. (3) unit tests cover probe path: checker_test.go TestDefaultServices_ChimeraFastLiveness PASS, status_test.go TestDefaultSubsystemProbes_ChimeraFastLiveness PASS, doctor_test.go TestDefaultDoctorConfig asserts /health PASS. Verified via actual runs: 'go test -short -count=1 ./pkg/health/...' -> ok (2.731s); 'go test -short -count=1 ./cmd/helix/' -> ok (9.974s); 'go build ./cmd/helix/ ./pkg/health/' -> exit 0; no LSP diagnostics.
The chimera probe false-down fix is complete: status/doctor/health now probe the fast /health liveness endpoint instead of slow /v1/health, 'Overall: healthy' + exit 0 logic confirmed, and unit tests covering the probe path all pass.

Overall: FAIL ✗
