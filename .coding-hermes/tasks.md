
## Dogfood Findings (2026-09-01)
Verdict: PROMISING-BUT-ROUGH
Promise: {"entry_point":"CLI: unified `helix` binary (thin dispatcher delegating subcommands like identity/estimate/negotiate/prompt/marketplace/release/verify/sandbox to sibling binaries in repo root or PATH); platform stack also runs via Docker Compose (Forgejo :3030/:2222, Chimera :8765). No HTTP server o

- [P0] Negotiate doesn't negotiate: tie-break 404s and escalates silently with rc=0 — Promise: 'If they deadlock, Chimera's arbiter formation breaks the tie' and 'debate <pr-url> Start or debug a negotiation between two agents'. Real use: flag help admits '(v1: Forgejo fetch not implem
- [P1] Estimate can't pre-flight freshly provisioned agents (hardcoded testdata, HELIX_KNOWN_FRIENDS ignored) — Real use: flags surfaced one at a time (--model, then --provider), then 'CONFIG_ERROR: agent "test-agent" not found in pkg/estimate/testdata/known-friends.json'. Freshly provisioned Forgejo agent (id 
- [P1] make docker-up (documented run command) fails on pre-existing container, no idempotency — Real use: 'Error response from daemon: Conflict. The container name "/helix-forgejo" is already in use' — after building the full image (~40s). docker compose ps shows zero containers while docker ps 
- [P1] Trust scoring is a dead end out of the box: --ledger required, no default, no sample ledger ships — Real use: 'helix trust list/show/history' → 'error: --ledger <path> is required'; no default path and repo grep finds zero sample ledgers. The promised 'trust scoring in the agent marketplace' is unru
- [P1] Verification/health surfaces misreport reality: shadow deltas wrong, doctor contradicts status — Real use: 'helix verify shadow' reports success_rate 99.5→0 as delta +100.00% and p99_latency_ms 500→0 as -20.00% — mathematically wrong differential output from the tool whose job is differential ana
