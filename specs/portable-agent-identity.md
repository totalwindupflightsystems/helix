# SPEC-022: Portable Agent Identity

**Status:** Draft · **Author:** Helix Foreman (DeepSeek V4 Pro) · **Date:** 2026-07-30
**Gap:** Buzz agents have Nostr-native cryptographic identities that travel across systems. Helix agents are locked to a single Forgejo instance.

## 1. Problem

Agents in Helix are Forgejo users — their identity IS their Forgejo account. If Forgejo goes down, agents disappear. If you move to a different forge, agents start from scratch. Buzz proved that agent identity must be **portable, cryptographic, and platform-independent**.

## 2. Solution

Every Helix agent gets a **Helix Identity Document (HID)** — a self-signed JSON document containing:
- `agent_id`: UUIDv7
- `pubkey`: Ed25519 public key
- `forge_handles`: map of `{forge_url: {username, proof_signature}}`
- `capabilities`: list of domain strengths with evidence
- `trust_score`: portable score with timestamps
- `created_at`, `updated_at`

The HID is signed by the agent's private key. Any forge can verify the signature and trust the agent's identity without a centralized registry.

Agent actions (commits, PRs) include a `Helix-Agent-ID` header with the HID fingerprint. This decouples agent identity from git forge identity — the agent exists independently.

## 3. Interface

```go
type AgentIdentity struct {
    ID            string              `json:"agent_id"`
    PubKey        ed25519.PublicKey   `json:"pubkey"`
    ForgeHandles  map[string]ForgeHandle `json:"forge_handles"`
    Capabilities  []CapabilityClaim   `json:"capabilities"`
    TrustScore    TrustSnapshot       `json:"trust_score"`
    Signatures    []Signature         `json:"signatures"`
}

func (id *AgentIdentity) Sign(privKey ed25519.PrivateKey) (*HID, error)
func (id *AgentIdentity) Verify(hid *HID) (bool, error)
func (id *AgentIdentity) RegisterForge(forgeURL, username string) error
func (id *AgentIdentity) Export(path string) error
func ImportHID(path string) (*AgentIdentity, error)
```

## 4. CLI

```
helix identity create   --name STRING [--output PATH]    → HID file
helix identity register --forge URL --agent HID_PATH     → Forge registration
helix identity verify   --hid PATH                       → verify signature
helix identity export   --format [json|nostr]            → portable
helix identity import   --path PATH                      → import
helix identity list     --forge URL                      → list agents
```

## 5. Forge Integration

Forgejo OAuth/OIDC bridge:
1. Agent creates HID locally
2. Agent registers HID with Forgejo via OAuth flow
3. Forgejo links agent account to HID fingerprint
4. Future auth: agent signs challenge with HID private key
5. Cross-forge: agent can prove same identity to another Forgejo

## 6. Nostr Compatibility (Phase 2)

The HID keypair is compatible with Nostr nsec/npub. Agent identity can be broadcast as a Nostr kind 0 event. Trust scores, capability claims, and incident reports are Nostr events. This makes Helix agents visible in Buzz and any Nostr-compatible workspace.

## 7. Files

```
pkg/identity/
  hid.go           # Core HID types + sign/verify
  hid_test.go      # Signature verification tests
  forge.go         # Forgejo OAuth registration
  forge_test.go    # Forge registration tests
  nostr.go         # Nostr compatibility bridge
  nostr_test.go    # Nostr event generation
  portable.go      # Export/import
  portable_test.go # Cross-instance identity tests

cmd/helix/identity_hid.go  # CLI commands
```

## 8. Testing

- Ed25519 sign + verify round-trip
- Import → verify → modify → detect tampering
- Register HID with Forgejo → verify linked account
- Create HID → export → import on different instance → verify same agent
- Nostr kind 0 event generation from HID
- Multi-forge: register same HID with two Forgejo instances

## 9. Build Order

1. `pkg/identity/hid.go` — core types + Ed25519 sign/verify
2. `pkg/identity/portable.go` — export/import
3. `pkg/identity/forge.go` — Forgejo OAuth registration
4. `cmd/helix/identity_hid.go` — CLI commands
5. `pkg/identity/nostr.go` — Nostr bridge (Phase 2)
