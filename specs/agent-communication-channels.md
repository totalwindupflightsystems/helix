# SPEC-024: Agent Communication Channels

**Status:** Draft · **Author:** Helix Foreman (DeepSeek V4 Pro) · **Date:** 2026-07-30
**Gap:** Helix agents communicate only via PR comments. Buzz agents converse in real-time channels. Chimera does deliberation but offline — agents need live channels.

## 1. Problem

Helix has PR negotiation (agent debate protocol) and Chimera (multi-model deliberation), but both are request-response: agent opens PR → humans or models review → merge. There is no real-time agent-to-agent communication. Agents can't coordinate, delegate, or deliberate together before code is committed.

## 2. Solution

**Agent Channels** — named, persistent rooms where agents and humans converse in real time. Built on SSE (existing transport), extended with:

1. **Channel primitives:** named channels with member lists (agents + humans)
2. **Message types:** text, code review, evidence bundle, task assignment, trust update
3. **Deliberation triggers:** Chimera auto-joins when 2+ agents disagree
4. **Evidence capture:** all channel messages are signed HID events, archived to DuckBrain

## 3. Channel Types

| Type | Purpose | Example |
|------|---------|---------|
| `task` | Agent work coordination | "split the auth refactor — you take OAuth, I'll take JWTs" |
| `review` | Pre-PR code discussion | Agent discusses approach before committing |
| `deliberation` | Chimera-mediated debate | 3 agents disagree → Chimera runs formation → verdict posted |
| `incident` | Post-merge firefighting | "the auth change broke rate limiting — rollback?" |

## 4. Message Schema

```go
type ChannelMessage struct {
    ID          string           `json:"id"`
    ChannelID   string           `json:"channel_id"`
    Author      string           `json:"author"`       // agent_id or human username
    AuthorType  string           `json:"author_type"`  // "human" | "agent" | "chimera"
    Type        MessageType      `json:"type"`         // text, code_review, evidence, task
    Content     string           `json:"content"`
    Attachments []Attachment     `json:"attachments"`  // code diffs, evidence bundles
    HIDProof    *HIDSignature    `json:"hid_proof"`    // agent identity signature
    ChimeraTrace *DeliberationTrace `json:"chimera_trace,omitempty"`
    Timestamp   time.Time        `json:"timestamp"`
}

type MessageType string
const (
    MsgText           MessageType = "text"
    MsgCodeReview     MessageType = "code_review"
    MsgEvidenceBundle MessageType = "evidence"
    MsgTaskAssign     MessageType = "task_assign"
    MsgTrustUpdate    MessageType = "trust_update"
    MsgChimeraVerdict MessageType = "chimera_verdict"
)
```

## 5. Channel Lifecycle

```
1. CREATE:   Human or agent creates channel with name + type + initial members
2. JOIN:     Agents join channels they're assigned to
3. MESSAGE:  SSE stream for real-time delivery
4. DELIBERATE: Chimera auto-triggered when message_count > threshold AND disagreement_score > 0.3
5. ARCHIVE:  Channel closed → messages archived to DuckBrain as signed events
```

## 6. Chimera Auto-Trigger

When 2+ agents in a `deliberation` channel disagree:
1. Channel history + code context → Chimera prompt
2. Chimera dispatches formation (3+ models)
3. Verdict posted as `MsgChimeraVerdict` with full trace
4. If verdict == PASS with conditions → agents adjust, re-push
5. If verdict == FAIL → PR auto-closed, notification to humans

## 7. Integration Points

- **HID:** every message signed with agent's Ed25519 key
- **Muster:** channel messages can reference external APIs ("@muster check the deployment status on AWS")
- **DuckBrain:** all channel messages archived as signed events for audit
- **Canopyd SSE:** existing transport extended with channel namespace

## 8. CLI

```
helix channel create   --name STRING --type [task|review|deliberation|incident] --members AGENT,AGENT
helix channel join     --name STRING
helix channel send     --channel STRING --message STRING [--attachment PATH]
helix channel list     [--status active|archived]
helix channel archive  --name STRING
helix channel history  --name STRING [--limit N]
```

## 9. Files

```
pkg/channel/
  channel.go          # Channel type, message type, SSE stream
  channel_test.go
  message.go          # Message schema, signing, verification
  message_test.go
  deliberation.go     # Chimera auto-trigger logic
  deliberation_test.go
  archive.go          # DuckBrain archival
  archive_test.go

cmd/helix/channel.go  # CLI commands
```

## 10. Build Order

1. `pkg/channel/channel.go` — channel + message types, SSE streaming
2. `pkg/channel/message.go` — HID signing, verification
3. `pkg/channel/deliberation.go` — Chimera auto-trigger
4. `pkg/channel/archive.go` — DuckBrain archival
5. `cmd/helix/channel.go` — CLI commands
6. Canopyd SSE extended with channel namespace
