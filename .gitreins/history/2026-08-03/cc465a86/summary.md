# Verdict: DF-005

**Task:** prompt register/list accept flat prompts/<c>/v<N>.md layout
**Evaluated:** 2026-08-03T02:43:04.608518
**Result:** ✗ FAIL

## Pipeline Stages

- ✓ **tier1**
  -   ✓ lsp: 
  ✓ trust_tier:   File category 'docs': requires provisional+, agent is provisional — OK
  File category 'code': req
  ✓ lint: 
  ✓ build: 
  ✓ tests: 
  ✓ secrets: 
- ✗ **tier2**
  - INCOMPLETE
  ✗ helix-prompt register coding-hermes v1 --dry-run exits 0 from repo root and prints PROMPT REGISTERED with a hash — resolves the flat prompts/coding-hermes/v1.md file: Command prints PROMPT REGISTERED with hash (sha256:6454e7e4...) and resolves the flat file, but exits 10, not 0. cmd/helix-prompt/main.go:181 calls os.Exit(prompt.ExitDryRun) where ExitDryRun=10 (pkg/prompt/types.go:128). This pre-existing dry-run sentinel was not changed by the task, so the literal 'exits 0' requirement is unmet.
  ✓ helix-prompt list shows all 5 prompt components including flat-layout ones (coding-hermes, deps-upgrade, e2e-forgejo, src-002) in addition to agent-identity: Verified via CLI: list output shows agent-identity (indexed) plus coding-hermes, deps-upgrade, e2e-forgejo, src-002 as flat unregistered entries.
  ✓ helix-prompt register nonexistent-comp v9 --dry-run exits non-zero with a clear error naming the attempted paths: Exits 1 with error naming attempted paths: prompts/nonexistent-comp/v9/prompt.md, prompts/nonexistent-comp/vv9.md, prompts/nonexistent-comp/v9.md (ResolvePromptPath error in pkg/prompt/registry.go).
  ✓ pkg/prompt exposes a path-resolution helper that tries nested prompts/<c>/<v>/prompt.md first, then flat prompts/<c>/v<N>.md: Exported ResolvePromptPath (pkg/prompt/registry.go:44) tries nested first, then flat v<N>.md and bare <v>.md; covered by TestResolvePromptPath_Layouts.
  ✓ register with an explicit --prompt-file path behaves unchanged: register coding-hermes v1 --prompt-file prompts/coding-hermes/v1.md --dry-run works and prints PROMPT REGISTERED; explicit path used as-is (main.go runRegister only resolves when promptFile=="").
  ✓ go build ./... and go vet ./... pass; golangci-lint run ./... reports 0 issues: go build ./... exit 0; go vet ./... exit 0; golangci-lint run ./... reports '0 issues' exit 0.
  ✓ go test -short -count=1 ./... passes for all 60 packages including new tests for both-layout resolution and flat listing: go test -short -count=1 ./... passes for 60 packages (grep -c '^ok' = 60); TestResolvePromptPath_Layouts and TestList_IncludesFlatUnregistered pass.
  ✓ gofmt clean on changed files; no new dependencies added (stdlib + cobra + yaml only): gofmt -l on changed files returns empty (clean); commit 372a85b did not modify go.mod/go.sum, so no new dependencies.
  ✓ AGENTS.md / flag help text no longer describes the nested path as the only layout: Flag help text (cmd/helix-prompt/main.go:132) and specs now describe fallback to flat layout; AGENTS.md uses flat prompts/<name>/v<N>.md only.
  ✓ commit includes Co-authored-by trailer and Prompt: prompts/coding-hermes/v1.md link: Commit 372a85b message includes 'Co-authored-by: Alexis Okuwa <wojonstech@gmail.com>' and 'Prompt: prompts/coding-hermes/v1.md'.
The flat-layout resolution, listing, error handling, build/lint/tests, docs, and commit trailer all pass, but criterion 1 fails because the register --dry-run command exits 10 (pre-existing ExitDryRun sentinel) rather than the required exit 0.

## Summary

Judge Result: DF-005

Stage tier1: PASS
    ✓ lsp: 
  ✓ trust_tier:   File category 'docs': requires provisional+, agent is provisional — OK
  File category 'code': req
  ✓ lint: 
  ✓ build: 
  ✓ tests: 
  ✓ secrets: 

Stage tier2: FAIL
  INCOMPLETE
  ✗ helix-prompt register coding-hermes v1 --dry-run exits 0 from repo root and prints PROMPT REGISTERED with a hash — resolves the flat prompts/coding-hermes/v1.md file: Command prints PROMPT REGISTERED with hash (sha256:6454e7e4...) and resolves the flat file, but exits 10, not 0. cmd/helix-prompt/main.go:181 calls os.Exit(prompt.ExitDryRun) where ExitDryRun=10 (pkg/prompt/types.go:128). This pre-existing dry-run sentinel was not changed by the task, so the literal 'exits 0' requirement is unmet.
  ✓ helix-prompt list shows all 5 prompt components including flat-layout ones (coding-hermes, deps-upgrade, e2e-forgejo, src-002) in addition to agent-identity: Verified via CLI: list output shows agent-identity (indexed) plus coding-hermes, deps-upgrade, e2e-forgejo, src-002 as flat unregistered entries.
  ✓ helix-prompt register nonexistent-comp v9 --dry-run exits non-zero with a clear error naming the attempted paths: Exits 1 with error naming attempted paths: prompts/nonexistent-comp/v9/prompt.md, prompts/nonexistent-comp/vv9.md, prompts/nonexistent-comp/v9.md (ResolvePromptPath error in pkg/prompt/registry.go).
  ✓ pkg/prompt exposes a path-resolution helper that tries nested prompts/<c>/<v>/prompt.md first, then flat prompts/<c>/v<N>.md: Exported ResolvePromptPath (pkg/prompt/registry.go:44) tries nested first, then flat v<N>.md and bare <v>.md; covered by TestResolvePromptPath_Layouts.
  ✓ register with an explicit --prompt-file path behaves unchanged: register coding-hermes v1 --prompt-file prompts/coding-hermes/v1.md --dry-run works and prints PROMPT REGISTERED; explicit path used as-is (main.go runRegister only resolves when promptFile=="").
  ✓ go build ./... and go vet ./... pass; golangci-lint run ./... reports 0 issues: go build ./... exit 0; go vet ./... exit 0; golangci-lint run ./... reports '0 issues' exit 0.
  ✓ go test -short -count=1 ./... passes for all 60 packages including new tests for both-layout resolution and flat listing: go test -short -count=1 ./... passes for 60 packages (grep -c '^ok' = 60); TestResolvePromptPath_Layouts and TestList_IncludesFlatUnregistered pass.
  ✓ gofmt clean on changed files; no new dependencies added (stdlib + cobra + yaml only): gofmt -l on changed files returns empty (clean); commit 372a85b did not modify go.mod/go.sum, so no new dependencies.
  ✓ AGENTS.md / flag help text no longer describes the nested path as the only layout: Flag help text (cmd/helix-prompt/main.go:132) and specs now describe fallback to flat layout; AGENTS.md uses flat prompts/<name>/v<N>.md only.
  ✓ commit includes Co-authored-by trailer and Prompt: prompts/coding-hermes/v1.md link: Commit 372a85b message includes 'Co-authored-by: Alexis Okuwa <wojonstech@gmail.com>' and 'Prompt: prompts/coding-hermes/v1.md'.
The flat-layout resolution, listing, error handling, build/lint/tests, docs, and commit trailer all pass, but criterion 1 fails because the register --dry-run command exits 10 (pre-existing ExitDryRun sentinel) rather than the required exit 0.

Overall: FAIL ✗
