# Helix API Reference

Package-level reference for the Helix platform libraries. These pages list the
exported types and method signatures so integrators can use the packages
without reading Go source first.

## Packages

| Package | Page | Purpose |
|---------|------|---------|
| `pkg/forgejo` | [forgejo.md](forgejo.md) | Forgejo REST API client: branches, repos, PRs, users, webhooks |
| `pkg/estimate` | [estimate.md](estimate.md) | Pre-flight token cost estimation, budget approval gates |
| `pkg/identity` | [identity.md](identity.md) | Agent identity: HID documents, Forgejo provisioning, OAuth2 registration |

## Related

- [Getting Started](../GETTING-STARTED.md) — onboarding guide
- [Specifications](../specs/) — feature specs and platform docs

## Conventions

- All packages are Go 1.25+, importable as
  `github.com/totalwindupflightsystems/helix/pkg/<name>`.
- Errors are returned as Go `error` values; `pkg/forgejo` and `pkg/identity`
  expose typed errors (`APIError`, `TypedError`) for classification.
- Method signatures below were generated from `go doc` on `master` and match
  the current source. When in doubt, `go doc pkg/<name>` in the repo root is
  authoritative.
