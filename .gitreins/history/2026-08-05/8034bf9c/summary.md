# Verdict: cov-003

**Task:** Coverage: cmd/helix spec-workflow >=65%
**Evaluated:** 2026-08-05T13:56:26.993621
**Result:** ✓ PASS

## Pipeline Stages

- ✓ **tier1**
  -   ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ lint: 
  ✓ tests: 
  ✓ build: 
  ✓ secrets: 
- ✓ **tier2**
  - COMPLETE
  ✓ go test -short -count=1 -cover ./cmd/helix/ reports >=65% statement coverage: go test -short -count=1 -cover ./cmd/helix/ reports 'coverage: 66.5% of statements' which is >= 65%
  ✓ go tool cover -func shows cmd/helix/spec.go, adr.go, contract.go each >=60% function coverage: go tool cover -func shows all functions in spec.go (min 75.0% runSpecList), adr.go (min 60.8% runAdrCreate), contract.go (min 74.1% runContractConsumerCheck) each >= 60%
  ✓ go test -race -short -count=1 ./cmd/helix/ passes with no data races: go test -race -short -count=1 ./cmd/helix/ passes with exit 0, 'ok ... 13.714s', no data races reported
  ✓ go build ./... exits 0: go build ./... exits 0
All four coverage criteria verified: 66.5% statement coverage, all functions in spec.go/adr.go/contract.go >=60%, race test passes, and build exits 0.

## Summary

Judge Result: cov-003

Stage tier1: PASS
    ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ lint: 
  ✓ tests: 
  ✓ build: 
  ✓ secrets: 

Stage tier2: PASS
  COMPLETE
  ✓ go test -short -count=1 -cover ./cmd/helix/ reports >=65% statement coverage: go test -short -count=1 -cover ./cmd/helix/ reports 'coverage: 66.5% of statements' which is >= 65%
  ✓ go tool cover -func shows cmd/helix/spec.go, adr.go, contract.go each >=60% function coverage: go tool cover -func shows all functions in spec.go (min 75.0% runSpecList), adr.go (min 60.8% runAdrCreate), contract.go (min 74.1% runContractConsumerCheck) each >= 60%
  ✓ go test -race -short -count=1 ./cmd/helix/ passes with no data races: go test -race -short -count=1 ./cmd/helix/ passes with exit 0, 'ok ... 13.714s', no data races reported
  ✓ go build ./... exits 0: go build ./... exits 0
All four coverage criteria verified: 66.5% statement coverage, all functions in spec.go/adr.go/contract.go >=60%, race test passes, and build exits 0.

Overall: PASS ✓
