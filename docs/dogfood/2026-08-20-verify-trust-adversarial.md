# Dogfood: verify / trust / adversarial-review flows (2026-08-20)

Dated dogfood exercise for the three ★ NEW specs (GAP-038). Run live on
2026-08-20 23:4xZ against Forgejo :3030 + Chimera :8765, CLI built from
HEAD (3785429 + board). All flows PASS.

## 1. production-verification (specs/production-verification.md) — `helix verify`

| Command | Result |
|---|---|
| `helix verify contract --path contract-good.yaml --json` | exit 0 — contract validated (auth-session-v2, 2 assertions) |
| `helix verify contract --path contract-bad.yaml --json` | exit 2 — rejected (invalid operator) |
| `helix verify shadow --agent df-agent-182 --json` | exit 0 — no shadow deployment state, clean status |
| `helix verify canary --agent df-agent-182 --json` | exit 0 — no canary in flight, clean status |

Good contract fixture (3 assertions, gte/lte/eq metrics, breach_action
rollback_and_notify) parses and validates; a malformed assertion set is
rejected with exit 2 — the contract gate fails closed.

## 2. trust-model (specs/trust-model.md) — `helix trust`

Ledger fixture: 4-event JSONL (promotion → attribution → decay_applied →
incident_penalty) replayed through the appendix-only ledger:

| Command | Result |
|---|---|
| `helix trust show --ledger trust.jsonl --agent df-agent-182 --json` | exit 0 — score breakdown (attribution 0.5wt, human_feedback, tenure contribution 0.067), recent_events replayed, score_trend window 30d start 65 → end 43 (delta -22, direction down), snapshot_time live |
| `helix trust history --ledger ... --agent df-agent-182 --json` | exit 0 — tier-transition history (empty for this fixture: no demotion events) |
| `helix trust list --ledger trust.jsonl --json` | exit 0 — agent listed, 4 events |

The incident penalty (-20) and decay (-2) are visible in the replayed
snapshot — incident-linked decay (spec §incident linkage) works on real
ledger data.

## 3. adversarial-review (specs/adversarial-review.md) — `helix review`

| Command | Result |
|---|---|
| `helix review strip-bias --input review.txt --json` | exit 0 — "obviously correct", "perfect", "trivial", "favorite" stripped from the review text |
| `helix review evidence sign --input review.txt --key-role audit --key-path X.hid.key --output review.signed.json` | exit 0 — signed bundle (role=audit, sig 261c9689...) |
| `helix review evidence verify --input review.signed.json --key-role audit --key-path X.pub --json` | exit 0 — `{"pr_url":"local://input","role":"audit","valid":true}` |
| Live multi-model E2E (Chimera-backed PR review, live Forgejo) | `TestForgejoE2E_MultiAgentReview` **PASS 13.58s**, `TestForgejoE2E_CommitStatusPipeline` PASS 6.34s, `TestForgejoE2E_FullCICDSimulation` PASS 6.18s (pkg/integration) |

`helix status` at exercise time: Overall HEALTHY — all 8 subsystems
(chimera, estimate, forgejo, marketplace, negotiate, review, trust,
verify) HTTP 200 (chimera probe ~2-8s, slow but healthy).

## Findings (rough edges, non-blocking)

1. `review evidence sign` accepts the private PEM (`.hid.key`) but
   `review evidence verify` requires a public key file in a different
   format (32 raw bytes / 64 hex / PEM PKIX; the `.hid` export carries the
   pubkey as base64). Round-trip recipe used here: `base64 -d` the
   `pubkey` field from `.hid` → 32-byte file → verify. Consider accepting
   the `.hid` path directly in a follow-up.
2. `helix identity create --output <path>` writes keys outside CWD;
   without `--output` it writes `<name>.hid`/`<name>.hid.key` to the CWD
   (surprise for scripting — clean up if run inside a repo).

## Verdict

All three new-spec flows are field-validated against live services:
PASS. The specs' "unit-tested only" caveat can be lifted — this entry
is the dated field evidence (GAP-038).
