
## Dogfood Findings (2026-09-01)
Verdict: PROMISING-BUT-ROUGH
Promise: {"entry_point":"CLI: unified `helix` binary (thin dispatcher delegating subcommands like identity/estimate/negotiate/prompt/marketplace/release/verify/sandbox to sibling binaries in repo root or PATH); platform stack also runs via Docker Compose (Forgejo :3030/:2222, Chimera :8765). No HTTP server o

- [P0] Negotiate doesn't negotiate: tie-break 404s and escalates silently with rc=0 — Promise: 'If they deadlock, Chimera's arbiter formation breaks the tie' and 'debate <pr-url> Start or debug a negotiation between two agents'. Real use: flag help admits '(v1: Forgejo fetch not implem
- [P1] Estimate can't pre-flight freshly provisioned agents (hardcoded testdata, HELIX_KNOWN_FRIENDS ignored) — Real use: flags surfaced one at a time (--model, then --provider), then 'CONFIG_ERROR: agent "test-agent" not found in pkg/estimate/testdata/known-friends.json'. Freshly provisioned Forgejo agent (id 
- [P1] make docker-up (documented run command) fails on pre-existing container, no idempotency — Real use: 'Error response from daemon: Conflict. The container name "/helix-forgejo" is already in use' — after building the full image (~40s). docker compose ps shows zero containers while docker ps 
- [P1] Trust scoring is a dead end out of the box: --ledger required, no default, no sample ledger ships — Real use: 'helix trust list/show/history' → 'error: --ledger <path> is required'; no default path and repo grep finds zero sample ledgers. The promised 'trust scoring in the agent marketplace' is unru
- [P1] Verification/health surfaces misreport reality: shadow deltas wrong, doctor contradicts status — Real use: 'helix verify shadow' reports success_rate 99.5→0 as delta +100.00% and p99_latency_ms 500→0 as -20.00% — mathematically wrong differential output from the tool whose job is differential ana

## Dogfood Findings (2026-09-04)
Verdict: PROMISING-BUT-ROUGH
Promise: {"entry_point":"Unified `helix` CLI (thin dispatcher to sibling binaries helix-identity, helix-estimate, helix-negotiate, helix-prompt, helix-marketplace, helix-release, helix-verify, sandbox), backed by a Docker Compose stack (Forgejo web :3030/SSH :2222, Chimera :8765) and Go library packages unde

- [P0] helix estimate check fails for freshly provisioned agents — estimate hardcodes pkg/estimate/testdata/known-friends.json and ignores HELIX_KNOWN_FRIENDS env (--known-friends flag exists but env is dead), so the agent provisioned moments earlier (dogfood-scout-0
- [P1] make docker-up and scripts/up.sh fail on container-name conflict — Both documented stack starters die with 'Conflict. The container name /helix-forgejo is already in use' (up.sh after ~83s of image builds); not idempotent, requires destructive docker compose down fir
- [P1] helix verify shadow differential math is wrong — success_rate 99.5 → 0 reported as delta +100.00% and p99_latency_ms 500 → 0 as -20.00% — the deltas are mathematically incorrect in a tool whose entire purpose is verification, undermining trust in it
- [P1] helix trust subcommands are a dead end — 'helix trust list' errors '--ledger <path> is required' with no default and no sample ledger ships in the repo, so the trust/reputation surface of the promise is unusable out of the box.
- [P2] Contradictory health verdicts with no guidance — helix doctor exits rc=1 (4 of 9 checks failed) while helix status --json reports 'overall: healthy' rc=0; no output explains which subsystems are optional on a minimal host, and manual Forgejo API ver
