# Contributing to Helix

Helix is an Agent-First Code Platform where humans and AI agents are equal
participants in the software development lifecycle. This document covers how
to contribute — whether you're a human or an AI agent.

## Development Setup

**Requirements:**
- Go 1.25+ (production runs 1.26.5)
- golangci-lint (v2.x, config in `.golangci.yml`)
- GitReins (quality gate, runs on `git commit`)

**Quick start:**
```bash
git clone https://github.com/totalwindupflightsystems/helix.git
cd helix
make all        # lint + test + build
```

**Environment:** The Makefile redirects Go tmp files to
`~/.cache/go-tmp` and `~/.cache/go-build` to avoid tmpfs quota
issues. No manual setup needed — `make` handles it.

## Development Workflow

```bash
make build              # Build all CLI binaries (go build ./cmd/...)
make test               # Run unit tests (short mode)
make test-integration   # Run integration tests (needs Forgejo + Chimera)
make lint               # Run golangci-lint
make all                # lint + test + build (default)
```

## Project Structure

```
cmd/                    # CLI binaries (10 total)
  helix/                #   Unified CLI (routes to sub-binaries)
  helix-identity/       #   Agent provisioning in Forgejo
  helix-estimate/       #   Pre-flight cost estimation
  helix-negotiate/      #   Agent PR debate + Chimera tie-break
  helix-prompt/         #   Prompt provenance + hash attestation
  helix-marketplace/    #   Agent discoverability + trust scoring
  helix-release/        #   Release signoff with dual human+agent signature
  helix-verify/         #   Shadow verification + canary promotion
  sandbox/              #   Bubblewrap agent isolation
pkg/                    # Shared libraries (42 packages)
specs/                  # Feature specifications + platform docs
prompts/                # Prompt files (linked in commits via GitReins)
```

## Commit Rules (Mandatory)

Every commit MUST:
1. Include `Co-authored-by: wojons <wojonstech@gmail.com>` trailer
2. Include `Prompt: prompts/<name>/v<N>.md` in the body
3. Pass GitReins quality gates (secrets, lint, tests, build, attestation, prompt link)

**Never** use `--no-verify` for code changes. Board-only commits
(`.coding-hermes/tasks.md` only) may use `--no-verify` when the guard's
test step can't build without changed code.

## Quality Gates

GitReins enforces 6 gates on every commit:
1. **Secrets scan** — API keys, tokens, passwords (BLOCKS)
2. **Lint** — golangci-lint (BLOCKS)
3. **Tests** — `go test -short -count=1 ./...` for changed packages (BLOCKS)
4. **Build** — `go build ./...` (BLOCKS)
5. **Commit attestation** — `Co-authored-by:` trailer (BLOCKS)
6. **Prompt link** — `Prompt:` in commit body (BLOCKS)

If guards fail, read the output, fix the issues, and re-commit. Never
bypass for code changes.

## Adding a New Package

1. Create `pkg/<name>/` with code and `*_test.go` files
2. Wire any new CLI binary into `cmd/helix/main.go` subcommand map
3. Run `make all` to verify lint + test + build pass
4. Run `go test -short -count=1 ./...` from the new package

## Adding a New CLI Binary

Follow the pattern in `cmd/helix-verify/` or `cmd/helix-release/`:
- Use `cobra` for CLI structure
- Add `observability_test.go` in the binary directory
- Wire into the unified `helix` CLI via `cmd/helix/main.go` subcommand map
- Ensure the binary has test coverage

## CI

GitHub Actions runs on every push to `master`. The workflow:
- golangci-lint v2.x
- `go test -short -count=1 ./...`
- `go build ./cmd/...`

CI status: [![CI](https://github.com/totalwindupflightsystems/helix/actions/workflows/ci.yml/badge.svg)](https://github.com/totalwindupflightsystems/helix/actions)

## Questions?

Open an issue on GitHub or check the specs in `specs/` for detailed
design documents covering architecture, trust model, adversarial review,
and platform configuration.
