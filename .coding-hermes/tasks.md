# Helix Coding Tasks — Foreman Queue

## [ ] Write Go tests for pkg/identity (types_test.go, syncer_test.go)
- **Priority:** high
- **Model:** MiniMax-M3
- **Files:** pkg/identity/types_test.go, pkg/identity/syncer_test.go
- **Fixtures:** pkg/identity/testdata/known-friends.json
- **AC:** `go test ./pkg/identity/... -count=1` passes with >80% coverage on types.go and syncer.go

## [ ] Write Go tests for pkg/sandbox (config_test.go, isolation_test.go)
- **Priority:** high
- **Model:** MiniMax-M3
- **Files:** pkg/sandbox/config_test.go, pkg/sandbox/isolation_test.go
- **Fixtures:** pkg/sandbox/testdata/valid-config.yaml, invalid-config.yaml
- **AC:** `go test ./pkg/sandbox/... -count=1` passes with >80% coverage on config.go and isolation.go

## [ ] Feature 1 Phase 2: implement Forgejo HTTP transport in provisioner.go
- **Priority:** high
- **Model:** glm-5.2
- **Provider:** zai-glm
- **Files:** pkg/identity/provisioner.go (replace 6 ErrNotImplemented stubs)
- **Spec:** specs/agent-identity.md §8
- **Env:** FORGEJO_URL=http://localhost:3030, FORGEJO_ADMIN_USER=helio
- **AC:** `helix-identity sync --dry-run` shows real Forgejo calls (not stubs)

## [ ] Feature 2 stubs: Go CLI + packages for cost estimator
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** zai-glm
- **Spec:** specs/cost-estimator.md (739 lines)
- **Files:** cmd/helix-estimate/main.go, pkg/estimate/*.go
- **AC:** `go build ./cmd/helix-estimate/` exits 0

## [ ] Feature 3 stubs: Go CLI + packages for PR negotiation
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** zai-glm
- **Spec:** specs/pr-negotiation.md (678 lines)
- **Files:** cmd/helix-negotiate/main.go, pkg/negotiate/*.go
- **AC:** `go build ./cmd/helix-negotiate/` exits 0

## [ ] Feature 4 stubs: Go CLI + packages for prompt registry
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** zai-glm
- **Spec:** specs/prompt-registry.md (684 lines)
- **Files:** cmd/helix-prompt/main.go, pkg/prompt/*.go
- **AC:** `go build ./cmd/helix-prompt/` exits 0

## [ ] Feature 5 stubs: Go CLI + packages for agent marketplace
- **Priority:** low
- **Model:** glm-5.2
- **Provider:** zai-glm
- **Spec:** specs/agent-marketplace.md (637 lines)
- **Files:** cmd/helix-marketplace/main.go, pkg/marketplace/*.go
- **AC:** `go build ./cmd/helix-marketplace/` exits 0

## [ ] Create prompts/ directory with initial prompt registrations
- **Priority:** low
- **Model:** deepseek-v4-flash
- **Files:** prompts/agent-identity/v1.0.0/prompt.md + metadata.yaml, prompts/_index.yaml
- **AC:** `helix prompt list` shows registered prompts
