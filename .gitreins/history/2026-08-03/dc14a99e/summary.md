# Verdict: DF-005

**Task:** prompt register/list accept flat prompts/<c>/v<N>.md layout
**Evaluated:** 2026-08-03T02:47:17.791436
**Result:** ✓ PASS

## Pipeline Stages

- ✓ **tier1**
  -   ✓ lsp: 
  ✓ trust_tier:   File category 'docs': requires provisional+, agent is provisional — OK
  File category 'code': req
  ✓ build: 
  ✓ lint: 
  ✓ tests: 
  ✓ secrets: 
- ✓ **tier2**
  - COMPLETE
  ✓ helix-prompt register coding-hermes v1 --dry-run from repo root prints PROMPT REGISTERED with a hash and does not error (dry-run exit code 10 = ExitDryRun sentinel per spec §13, pre-existing intentional behavior) — resolves the flat prompts/coding-hermes/v1.md file: CLI run prints 'PROMPT REGISTERED' with hash sha256:6454e7e455e3d63390eebea4a926162794db249ca79910aca248898e423a212f and resolves flat prompts/coding-hermes/v1.md (ls confirms v1.md exists). Exit code 10 = ExitDryRun sentinel, explicitly acknowledged as intentional in the task criterion.
  ✓ helix-prompt list shows all 5 prompt components including flat-layout ones (coding-hermes, deps-upgrade, e2e-forgejo, src-002) in addition to agent-identity: CLI list output shows agent-identity (active, indexed) plus coding-hermes, deps-upgrade, e2e-forgejo, src-002 as flat unregistered entries — all 5 components present.
  ✓ helix-prompt register nonexistent-comp v9 --dry-run exits non-zero with a clear error naming the attempted paths: CLI run exits 1 with error naming attempted paths: prompts/nonexistent-comp/v9/prompt.md, prompts/nonexistent-comp/vv9.md, prompts/nonexistent-comp/v9.md (ResolvePromptPath error).
  ✓ pkg/prompt exposes a path-resolution helper that tries nested prompts/<c>/<v>/prompt.md first, then flat prompts/<c>/v<N>.md: Exported ResolvePromptPath at pkg/prompt/registry.go:47 tries nested prompts/<c>/<v>/prompt.md first, then flat prompts/<c>/v<N>.md, then bare <v>.md. Covered by TestResolvePromptPath_Layouts (registry_flat_test.go:14).
  ✓ register with an explicit --prompt-file path behaves unchanged: CLI run 'register coding-hermes v1 --prompt-file prompts/coding-hermes/v1.md --dry-run' works and prints PROMPT REGISTERED with same hash; runRegister only resolves when promptFile=="" (main.go).
  ✓ go build ./... and go vet ./... pass; golangci-lint run ./... reports 0 issues: go build ./... exit 0; go vet ./... exit 0; golangci-lint run ./... reports '0 issues.' exit 0.
  ✓ go test -short -count=1 ./... passes for all 60 packages including new tests for both-layout resolution and flat listing: go test -short -count=1 ./... passes for all 60 packages (grep -c '^ok' = 60, DONE 0). New tests TestResolvePromptPath_Layouts and TestList_IncludesFlatUnregistered present in registry_flat_test.go and pass.
  ✓ gofmt clean on changed files; no new dependencies added (stdlib + cobra + yaml only): gofmt -l on all changed files returns empty (clean). Commit 372a85b did not modify go.mod/go.sum, so no new dependencies added.
  ✓ AGENTS.md / flag help text no longer describes the nested path as the only layout: Flag help text (main.go:132) and both specs (prompt-registry.md, prompt-registry-v2.md) now describe fallback to flat layout; AGENTS.md uses flat prompts/<name>/v<N>.md only (lines 64, 93).
  ✓ commit includes Co-authored-by trailer and Prompt: prompts/coding-hermes/v1.md link: Commit 372a85b message includes 'Co-authored-by: Alexis Okuwa <wojonstech@gmail.com>' and 'Prompt: prompts/coding-hermes/v1.md' (verified via git show --format=%B).
All 10 criteria pass: flat-layout resolution (ResolvePromptPath), listing of all 5 components, error handling, build/vet/lint/tests (60 packages), gofmt/deps, docs, and commit trailer all verified; criterion 1's exit code 10 is the intentional ExitDryRun sentinel acknowledged in the task definition.

## Summary

Judge Result: DF-005

Stage tier1: PASS
    ✓ lsp: 
  ✓ trust_tier:   File category 'docs': requires provisional+, agent is provisional — OK
  File category 'code': req
  ✓ build: 
  ✓ lint: 
  ✓ tests: 
  ✓ secrets: 

Stage tier2: PASS
  COMPLETE
  ✓ helix-prompt register coding-hermes v1 --dry-run from repo root prints PROMPT REGISTERED with a hash and does not error (dry-run exit code 10 = ExitDryRun sentinel per spec §13, pre-existing intentional behavior) — resolves the flat prompts/coding-hermes/v1.md file: CLI run prints 'PROMPT REGISTERED' with hash sha256:6454e7e455e3d63390eebea4a926162794db249ca79910aca248898e423a212f and resolves flat prompts/coding-hermes/v1.md (ls confirms v1.md exists). Exit code 10 = ExitDryRun sentinel, explicitly acknowledged as intentional in the task criterion.
  ✓ helix-prompt list shows all 5 prompt components including flat-layout ones (coding-hermes, deps-upgrade, e2e-forgejo, src-002) in addition to agent-identity: CLI list output shows agent-identity (active, indexed) plus coding-hermes, deps-upgrade, e2e-forgejo, src-002 as flat unregistered entries — all 5 components present.
  ✓ helix-prompt register nonexistent-comp v9 --dry-run exits non-zero with a clear error naming the attempted paths: CLI run exits 1 with error naming attempted paths: prompts/nonexistent-comp/v9/prompt.md, prompts/nonexistent-comp/vv9.md, prompts/nonexistent-comp/v9.md (ResolvePromptPath error).
  ✓ pkg/prompt exposes a path-resolution helper that tries nested prompts/<c>/<v>/prompt.md first, then flat prompts/<c>/v<N>.md: Exported ResolvePromptPath at pkg/prompt/registry.go:47 tries nested prompts/<c>/<v>/prompt.md first, then flat prompts/<c>/v<N>.md, then bare <v>.md. Covered by TestResolvePromptPath_Layouts (registry_flat_test.go:14).
  ✓ register with an explicit --prompt-file path behaves unchanged: CLI run 'register coding-hermes v1 --prompt-file prompts/coding-hermes/v1.md --dry-run' works and prints PROMPT REGISTERED with same hash; runRegister only resolves when promptFile=="" (main.go).
  ✓ go build ./... and go vet ./... pass; golangci-lint run ./... reports 0 issues: go build ./... exit 0; go vet ./... exit 0; golangci-lint run ./... reports '0 issues.' exit 0.
  ✓ go test -short -count=1 ./... passes for all 60 packages including new tests for both-layout resolution and flat listing: go test -short -count=1 ./... passes for all 60 packages (grep -c '^ok' = 60, DONE 0). New tests TestResolvePromptPath_Layouts and TestList_IncludesFlatUnregistered present in registry_flat_test.go and pass.
  ✓ gofmt clean on changed files; no new dependencies added (stdlib + cobra + yaml only): gofmt -l on all changed files returns empty (clean). Commit 372a85b did not modify go.mod/go.sum, so no new dependencies added.
  ✓ AGENTS.md / flag help text no longer describes the nested path as the only layout: Flag help text (main.go:132) and both specs (prompt-registry.md, prompt-registry-v2.md) now describe fallback to flat layout; AGENTS.md uses flat prompts/<name>/v<N>.md only (lines 64, 93).
  ✓ commit includes Co-authored-by trailer and Prompt: prompts/coding-hermes/v1.md link: Commit 372a85b message includes 'Co-authored-by: Alexis Okuwa <wojonstech@gmail.com>' and 'Prompt: prompts/coding-hermes/v1.md' (verified via git show --format=%B).
All 10 criteria pass: flat-layout resolution (ResolvePromptPath), listing of all 5 components, error handling, build/vet/lint/tests (60 packages), gofmt/deps, docs, and commit trailer all verified; criterion 1's exit code 10 is the intentional ExitDryRun sentinel acknowledged in the task definition.

Overall: PASS ✓
