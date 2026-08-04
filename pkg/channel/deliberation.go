// Package channel — deliberation.go
//
// Chimera auto-trigger orchestration for agent-disagreement channels
// (SPEC-024 §5 step 4 / §6).
//
// When two or more agents converse in a ChannelDeliberation room and the
// configured disagreement scorer crosses the threshold, the Deliberator
// builds a prompt from channel history (+ optional code context), dispatches
// it to Chimera, and posts the verdict back into the channel as a
// MsgChimeraVerdict authored by "chimera". The optional VerdictHandler is
// invoked on success so downstream code (PR auto-close, human notification,
// agent adjust-and-repush) can react without pkg/channel importing
// pkg/forgejo.
//
// The default DisagreementScorer is a lightweight keyword/stance heuristic
// suitable for early-stage agent traffic; production deployments should
// inject a richer scorer (e.g. an embedding-similarity or LLM-judge scorer)
// via DeliberatorOpts.Scorer.
//
// Design notes:
//
//   - DeliberationClient is an interface so tests can inject a fake without
//     touching the network. The default HTTP implementation posts to
//     {baseURL}/api/v1/deliberate (the same wire contract ChimeraModelClient
//     in pkg/review already uses).
//   - The trigger loop guard is purely based on the most recent message:
//     if the tail of the conversation is already a MsgChimeraVerdict, no new
//     verdict is posted. This is a best-effort guard; an external lock would
//     be needed for cross-process safety, which is out of scope here.
//   - HTTP errors, non-2xx responses, malformed JSON, and context deadlines
//     all surface as errors and MUST NOT post a verdict message. The caller
//     decides whether to retry.
package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Constants & thresholds
// ---------------------------------------------------------------------------

// Default message-count threshold below which the Deliberator will not
// trigger even if disagreement is high. SPEC-024 §5 step 4 ties this to
// "message_count > threshold" — we default to 2 (the minimum for 2 distinct
// agents to both speak) and expose it so callers can raise it for noisier
// channels.
const DefaultMessageCountThreshold = 2

// DefaultDisagreementThreshold is the minimum disagreement score (0.0–1.0)
// required to trigger Chimera. SPEC-024 §5 step 4 mandates > 0.3.
const DefaultDisagreementThreshold = 0.3

// DefaultHTTPTimeout bounds the Chimera HTTP request. It is intentionally
// short — deliberation should be quick, and the caller can wrap the
// Deliberate call in a longer-lived context if needed.
const DefaultHTTPTimeout = 30 * time.Second

// DefaultFormation is the Chimera formation preset used when the caller
// does not specify one. "balanced" is a sane middle-ground between the
// rigorous (slow) and fast (cheap) presets.
const DefaultFormation = "balanced"

// ChimeraAuthorName is the Author field used for verdict messages posted
// back into the channel. Agents and humans render this as the verdict
// poster; the AuthorType field (AuthorChimera) carries the semantic
// classification.
const ChimeraAuthorName = "chimera"

// ---------------------------------------------------------------------------
// Verdict
// ---------------------------------------------------------------------------

// VerdictOutcome is the high-level outcome of a Chimera deliberation.
type VerdictOutcome string

const (
	// VerdictPass means Chimera endorses the prevailing direction.
	VerdictPass VerdictOutcome = "pass"
	// VerdictConditional means Chimera endorses the direction subject to
	// the conditions in Verdict.Conditions (SPEC-024 §6 step 4 — agents
	// adjust and re-push).
	VerdictConditional VerdictOutcome = "conditional"
	// VerdictFail means Chimera rejects the prevailing direction
	// (SPEC-024 §6 step 5 — PR auto-close, human notification).
	VerdictFail VerdictOutcome = "fail"
)

// ValidVerdictOutcomes lists every recognised VerdictOutcome.
var ValidVerdictOutcomes = map[VerdictOutcome]bool{
	VerdictPass:        true,
	VerdictConditional: true,
	VerdictFail:        true,
}

// Verdict is the parsed Chimera deliberation result. Trace is the raw trace
// payload returned by Chimera (opaque, passthrough) so callers can render
// it or archive it without re-parsing.
type Verdict struct {
	Outcome    VerdictOutcome `json:"outcome"`
	Summary    string         `json:"summary"`
	Content    string         `json:"content,omitempty"`
	Conditions []string       `json:"conditions,omitempty"`
	Trace      any            `json:"trace,omitempty"`
}

// ---------------------------------------------------------------------------
// DeliberationClient
// ---------------------------------------------------------------------------

// DeliberationClient is the minimal contract the Deliberator needs from
// Chimera. Tests inject a fake; production uses HTTPChimeraClient.
type DeliberationClient interface {
	// Deliberate dispatches the prompt to Chimera with the given formation
	// preset and returns the parsed verdict. Context deadlines are
	// honoured; HTTP, parse, and timeout errors all surface as errors.
	Deliberate(ctx context.Context, prompt, formation string) (Verdict, error)
}

// HTTPChimeraClient is the default DeliberationClient. It POSTs JSON to
// {BaseURL}/api/v1/deliberate and parses {"result", "trace"} — the same
// wire contract pkg/review.ChimeraModelClient uses. APIKey is optional;
// pass "" for an unauthenticated deployment.
//
// Timeout defaults to DefaultHTTPTimeout when zero.
type HTTPChimeraClient struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
	// Client is the underlying *http.Client. If nil, one is created with
	// the configured Timeout. Callers can inject a custom client (e.g. for
	// retries or instrumentation) without changing the wire shape.
	Client *http.Client
}

// NewHTTPChimeraClient returns an HTTPChimeraClient with default Timeout
// applied when cfg.Timeout is zero.
func NewHTTPChimeraClient(baseURL, apiKey string, timeout time.Duration) *HTTPChimeraClient {
	if timeout <= 0 {
		timeout = DefaultHTTPTimeout
	}
	return &HTTPChimeraClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Timeout: timeout,
		Client:  &http.Client{Timeout: timeout},
	}
}

// Deliberate implements DeliberationClient.
func (c *HTTPChimeraClient) Deliberate(ctx context.Context, prompt, formation string) (Verdict, error) {
	if c == nil {
		return Verdict{}, fmt.Errorf("channel: nil HTTPChimeraClient")
	}
	if c.BaseURL == "" {
		return Verdict{}, fmt.Errorf("channel: HTTPChimeraClient has empty BaseURL")
	}
	if prompt == "" {
		return Verdict{}, fmt.Errorf("channel: prompt must not be empty")
	}
	if formation == "" {
		formation = DefaultFormation
	}

	body, err := json.Marshal(map[string]any{
		"prompt":    prompt,
		"formation": formation,
	})
	if err != nil {
		return Verdict{}, fmt.Errorf("channel: marshal deliberate request: %w", err)
	}

	url := strings.TrimRight(c.BaseURL, "/") + "/api/v1/deliberate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Verdict{}, fmt.Errorf("channel: build deliberate request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: c.Timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return Verdict{}, fmt.Errorf("channel: chimera HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the full body (bounded) so we can include it in error messages.
	const maxRead = 1 << 20 // 1 MiB
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRead))
	if err != nil {
		return Verdict{}, fmt.Errorf("channel: read chimera response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Verdict{}, fmt.Errorf("channel: chimera returned status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	// Chimera wire format: {"result": "...", "trace": [...]}.
	// `result` is a free-form text block; we parse it into a Verdict via
	// ParseVerdict so callers can inject alternative parsers later. Trace
	// is passed through verbatim.
	var wire struct {
		Result string `json:"result"`
		Trace  any    `json:"trace"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Verdict{}, fmt.Errorf("channel: parse chimera response JSON: %w", err)
	}

	verdict, err := ParseVerdict(wire.Result)
	if err != nil {
		return Verdict{}, err
	}
	verdict.Trace = wire.Trace
	return verdict, nil
}

// ---------------------------------------------------------------------------
// Verdict parser
// ---------------------------------------------------------------------------

// ParseVerdict extracts a Verdict from the free-form text block Chimera
// returns. We expect a small structured header; anything before the header
// becomes Content, the Summary is the line following "Summary:", and
// "PASS"/"FAIL"/"CONDITIONAL" lines drive the Outcome. "Condition:" lines
// populate Verdict.Conditions.
//
// This is intentionally lenient — Chimera's prompt-to-verdict shape will
// evolve, and we want callers to be able to override the parser later via
// DeliberatorOpts if a stricter schema is required.
func ParseVerdict(result string) (Verdict, error) {
	v := Verdict{Content: result}
	lines := strings.Split(result, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)

		switch {
		case strings.HasPrefix(upper, "VERDICT:"):
			outcome := strings.TrimSpace(strings.TrimPrefix(upper, "VERDICT:"))
			switch outcome {
			case "PASS":
				v.Outcome = VerdictPass
			case "FAIL":
				v.Outcome = VerdictFail
			case "CONDITIONAL", "CONDITIONALLY PASS", "CONDITIONALLY-PASS":
				v.Outcome = VerdictConditional
			default:
				return Verdict{}, fmt.Errorf("channel: unrecognised verdict outcome %q", outcome)
			}
		case strings.HasPrefix(upper, "SUMMARY:"):
			v.Summary = strings.TrimSpace(strings.TrimPrefix(trimmed, strings.TrimSpace(strings.SplitN(trimmed, ":", 2)[0])+":"))
		case strings.HasPrefix(upper, "CONDITION:"):
			cond := strings.TrimSpace(strings.TrimPrefix(trimmed, strings.TrimSpace(strings.SplitN(trimmed, ":", 2)[0])+":"))
			if cond != "" {
				v.Conditions = append(v.Conditions, cond)
			}
		}
		_ = i // keep linter happy when only Outcome parsing is exercised
	}

	// Default to pass when no explicit VERDICT: line is present and the
	// text contains no obvious failure marker. This mirrors how reviewers
	// read a free-form summary that omits the structured header.
	if v.Outcome == "" {
		upper := strings.ToUpper(result)
		switch {
		case strings.Contains(upper, "VERDICT: FAIL") || strings.Contains(upper, "REJECT") || strings.Contains(upper, "FAIL"):
			v.Outcome = VerdictFail
		case strings.Contains(upper, "CONDITIONAL"):
			v.Outcome = VerdictConditional
		default:
			v.Outcome = VerdictPass
		}
	}

	// If no Summary line was parsed, the first non-empty line becomes the
	// summary so the channel always has something to render.
	if v.Summary == "" {
		for _, line := range lines {
			if s := strings.TrimSpace(line); s != "" && !strings.HasPrefix(strings.ToUpper(s), "VERDICT:") {
				v.Summary = s
				break
			}
		}
	}

	return v, nil
}

// ---------------------------------------------------------------------------
// Disagreement scoring
// ---------------------------------------------------------------------------

// DisagreementScorer assigns a disagreement score in [0.0, 1.0] to a slice
// of channel messages from a deliberation channel. Scores above the
// configured threshold (DefaultDisagreementThreshold == 0.3) trigger Chimera.
type DisagreementScorer interface {
	Score(messages []*ChannelMessage) float64
}

// KeywordDisagreementScorer is the default heuristic scorer. It rewards
// messages authored by distinct agent identities that contain disagree-style
// language ("disagree", "oppose", "no", "wrong", "cannot accept", "block",
// "reject", "-1") and returns a score capped at 1.0.
//
// The formula:
//
//	score = min(1.0, distinct_agent_authors * stance_hits / total_agent_messages)
//
// where stance_hits counts messages whose lower-cased content contains any
// of the default keywords, and distinct_agent_authors counts unique Author
// values for AuthorAgent messages (humans and chimera are ignored).
//
// This is a deliberate placeholder — production deployments should inject a
// richer scorer (embedding similarity, LLM judge, etc.) via
// DeliberatorOpts.Scorer.
type KeywordDisagreementScorer struct {
	// Keywords is the lower-cased list of disagreement signals. Defaults
	// are populated by NewKeywordDisagreementScorer when the field is nil.
	Keywords []string
}

// NewKeywordDisagreementScorer returns a scorer with the default keyword
// set. Callers can mutate d.Keywords afterwards to extend the heuristic.
func NewKeywordDisagreementScorer() *KeywordDisagreementScorer {
	return &KeywordDisagreementScorer{
		Keywords: []string{
			"disagree", "oppose", "wrong", "cannot accept",
			"block", "reject", "-1", "no",
		},
	}
}

// Score implements DisagreementScorer.
func (s *KeywordDisagreementScorer) Score(messages []*ChannelMessage) float64 {
	if s == nil || len(messages) == 0 {
		return 0
	}
	keywords := s.Keywords
	if len(keywords) == 0 {
		keywords = NewKeywordDisagreementScorer().Keywords
	}

	distinctAuthors := map[string]bool{}
	totalAgent := 0
	stanceHits := 0

	for _, m := range messages {
		if m == nil || m.AuthorType != AuthorAgent {
			continue
		}
		totalAgent++
		distinctAuthors[m.Author] = true

		content := strings.ToLower(m.Content)
		for _, kw := range keywords {
			if kw == "" {
				continue
			}
			if strings.Contains(content, kw) {
				stanceHits++
				break // one hit per message is enough
			}
		}
	}

	if totalAgent == 0 || len(distinctAuthors) < 2 {
		return 0
	}

	// Weight by distinct authors (more agents disagreeing = stronger
	// signal) and scaled by the fraction of agent messages that exhibit
	// stance language. Clamp to [0, 1].
	raw := float64(len(distinctAuthors)) * float64(stanceHits) / float64(totalAgent)
	if raw > 1.0 {
		raw = 1.0
	}
	return raw
}

// ---------------------------------------------------------------------------
// Verdict handler
// ---------------------------------------------------------------------------

// VerdictHandler is invoked after a verdict message is successfully posted
// to the channel. Implementations can close PRs (FAIL), notify humans, or
// adjust agent behaviour (PASS-with-conditions).
type VerdictHandler interface {
	OnVerdict(ctx context.Context, channelID string, verdict Verdict) error
}

// NopVerdictHandler is the default VerdictHandler — it does nothing. It
// exists so NewDeliberator can wire a non-nil handler without callers
// having to think about it.
type NopVerdictHandler struct{}

// OnVerdict implements VerdictHandler.
func (NopVerdictHandler) OnVerdict(_ context.Context, _ string, _ Verdict) error {
	return nil
}

// ---------------------------------------------------------------------------
// Deliberator
// ---------------------------------------------------------------------------

// DeliberatorOpts configures a Deliberator. Zero values pick the defaults.
type DeliberatorOpts struct {
	// Client is the Chimera transport. Defaults to an HTTPChimeraClient
	// pointed at DefaultBaseURL when nil.
	Client DeliberationClient
	// Handler is invoked after a verdict is posted. Defaults to
	// NopVerdictHandler when nil.
	Handler VerdictHandler
	// Scorer assigns a disagreement score in [0, 1]. Defaults to
	// NewKeywordDisagreementScorer() when nil.
	Scorer DisagreementScorer
	// MessageCountThreshold is the minimum message count before the
	// Deliberator will consider triggering. Defaults to
	// DefaultMessageCountThreshold when zero.
	MessageCountThreshold int
	// DisagreementThreshold is the minimum disagreement score required to
	// trigger. Defaults to DefaultDisagreementThreshold when zero.
	DisagreementThreshold float64
	// Formation is the Chimera formation preset. Defaults to
	// DefaultFormation when empty.
	Formation string
	// ChimeraBaseURL is used when Client is nil. Defaults to
	// DefaultChimeraBaseURL when empty.
	ChimeraBaseURL string
}

// DefaultChimeraBaseURL is the canonical localhost Chimera service used in
// tests and dev. Production should override via DeliberatorOpts.ChimeraBaseURL.
const DefaultChimeraBaseURL = "http://localhost:8765"

// Deliberator orchestrates disagreement detection and Chimera auto-trigger.
// It is safe to share a Deliberator across goroutines — its internal state
// is read-only after construction.
type Deliberator struct {
	client                DeliberationClient
	handler               VerdictHandler
	scorer                DisagreementScorer
	messageCountThreshold int
	disagreementThreshold float64
	formation             string
}

// NewDeliberator returns a Deliberator with defaults applied. Callers
// override fields via DeliberatorOpts; nil opts yields the safest defaults
// (localhost Chimera, nop handler, keyword scorer, threshold 2 / 0.3).
func NewDeliberator(opts ...DeliberatorOpts) *Deliberator {
	o := DeliberatorOpts{}
	if len(opts) > 0 {
		o = opts[0]
	}

	client := o.Client
	if client == nil {
		base := o.ChimeraBaseURL
		if base == "" {
			base = DefaultChimeraBaseURL
		}
		client = NewHTTPChimeraClient(base, "", DefaultHTTPTimeout)
	}

	handler := o.Handler
	if handler == nil {
		handler = NopVerdictHandler{}
	}

	scorer := o.Scorer
	if scorer == nil {
		scorer = NewKeywordDisagreementScorer()
	}

	msgThreshold := o.MessageCountThreshold
	if msgThreshold <= 0 {
		msgThreshold = DefaultMessageCountThreshold
	}

	disagreementThreshold := o.DisagreementThreshold
	if disagreementThreshold <= 0 {
		disagreementThreshold = DefaultDisagreementThreshold
	}

	formation := o.Formation
	if formation == "" {
		formation = DefaultFormation
	}

	return &Deliberator{
		client:                client,
		handler:               handler,
		scorer:                scorer,
		messageCountThreshold: msgThreshold,
		disagreementThreshold: disagreementThreshold,
		formation:             formation,
	}
}

// Client returns the underlying DeliberationClient (useful in tests).
func (d *Deliberator) Client() DeliberationClient { return d.client }

// Handler returns the registered VerdictHandler.
func (d *Deliberator) Handler() VerdictHandler { return d.handler }

// Scorer returns the registered DisagreementScorer.
func (d *Deliberator) Scorer() DisagreementScorer { return d.scorer }

// MessageCountThreshold returns the configured message-count threshold.
func (d *Deliberator) MessageCountThreshold() int { return d.messageCountThreshold }

// DisagreementThreshold returns the configured disagreement-score threshold.
func (d *Deliberator) DisagreementThreshold() float64 { return d.disagreementThreshold }

// Formation returns the configured Chimera formation preset.
func (d *Deliberator) Formation() string { return d.formation }

// ---------------------------------------------------------------------------
// Trigger evaluation
// ---------------------------------------------------------------------------

// ShouldTrigger reports whether the Deliberator would call Chimera for the
// given channel + messages. It is exposed so callers can pre-flight without
// mutating the message store. DeliberateIfNeeded uses the same rules.
//
// The check is:
//
//	channel.Type == ChannelDeliberation
//	  AND len(messages) > MessageCountThreshold
//	  AND scorer.Score(messages) > DisagreementThreshold
//	  AND last message is NOT already a MsgChimeraVerdict
//	    authored by AuthorChimera (loop guard)
func (d *Deliberator) ShouldTrigger(channel *Channel, messages []*ChannelMessage) bool {
	if d == nil || channel == nil {
		return false
	}
	if channel.Type != ChannelDeliberation {
		return false
	}
	if len(messages) <= d.messageCountThreshold {
		return false
	}
	if d.scorer.Score(messages) <= d.disagreementThreshold {
		return false
	}
	if isAlreadyDeliberating(messages) {
		return false
	}
	return true
}

// isAlreadyDeliberating is the loop guard: if the most recent message in
// the channel is already a Chimera verdict, we skip. Chimera verdicts can
// legitimately chain (e.g. a FAIL triggers a re-deliberation after agents
// adjust), but the natural read of "already deliberating" is "the last
// thing the channel saw was a verdict — wait for new agent traffic".
func isAlreadyDeliberating(messages []*ChannelMessage) bool {
	if len(messages) == 0 {
		return false
	}
	last := messages[len(messages)-1]
	if last == nil {
		return false
	}
	return last.Type == MsgChimeraVerdict && last.AuthorType == AuthorChimera
}

// ---------------------------------------------------------------------------
// Orchestration
// ---------------------------------------------------------------------------

// DeliberateIfNeeded evaluates whether Chimera should run for the given
// channel + messages, and if so builds a prompt from the channel history
// (+ optional code context), calls Chimera, posts a MsgChimeraVerdict back
// into the supplied store, and invokes the configured VerdictHandler.
//
// Returns:
//
//   - (verdict, nil)             — verdict was posted successfully.
//   - (zero, ErrNotTriggered)    — pre-conditions not met; no HTTP call.
//   - (zero, chimeraErr)         — Chimera HTTP/parse error; no verdict posted.
//   - (verdict, handlerErr)      — verdict posted but handler returned an error.
//     The verdict is returned so the caller can still audit; the handler error
//     is returned so the caller can decide whether to retry the handler.
//
// DeliberateIfNeeded respects ctx for both the HTTP call and the handler.
func (d *Deliberator) DeliberateIfNeeded(
	ctx context.Context,
	channel *Channel,
	store MessageStore,
	messages []*ChannelMessage,
	codeContext string,
) (Verdict, error) {
	if d == nil {
		return Verdict{}, fmt.Errorf("channel: nil Deliberator")
	}
	if channel == nil {
		return Verdict{}, fmt.Errorf("channel: nil channel")
	}

	if !d.ShouldTrigger(channel, messages) {
		return Verdict{}, ErrNotTriggered
	}

	prompt := BuildPrompt(channel, messages, codeContext)
	verdict, err := d.client.Deliberate(ctx, prompt, d.formation)
	if err != nil {
		return Verdict{}, err
	}

	// Post the verdict back into the channel so the SSE stream picks it
	// up and the channel history reflects the resolution.
	if store != nil {
		if err := d.postVerdict(channel, store, verdict); err != nil {
			return Verdict{}, fmt.Errorf("channel: post verdict: %w", err)
		}
	}

	// Invoke the handler. A handler error must not silently swallow the
	// verdict — return both so the caller can audit/retry.
	if err := d.handler.OnVerdict(ctx, channel.ID, verdict); err != nil {
		return verdict, fmt.Errorf("channel: verdict handler: %w", err)
	}

	return verdict, nil
}

// postVerdict writes the verdict into the supplied store. The verdict is
// authored by "chimera" (AuthorChimera) and carries ChimeraTrace so the
// full deliberation is archived alongside the message.
func (d *Deliberator) postVerdict(channel *Channel, store MessageStore, verdict Verdict) error {
	summary := verdict.Summary
	if summary == "" {
		summary = verdict.Content
	}
	if summary == "" {
		summary = string(verdict.Outcome)
	}

	msg := NewChannelMessage(
		channel.ID,
		ChimeraAuthorName,
		AuthorChimera,
		MsgChimeraVerdict,
		summary,
	)
	msg.ChimeraTrace = verdict.Trace
	if len(verdict.Conditions) > 0 {
		// Surface conditions in the message body so they're visible
		// without rendering the full trace.
		msg.Content = summary + "\n\nConditions:\n- " + strings.Join(verdict.Conditions, "\n- ")
	}
	return store.Send(msg)
}

// BuildPrompt assembles the Chimera prompt from channel history and the
// optional code context. It is exported so tests can assert the prompt
// shape and so future callers (e.g. CLI tools) can preview what would be
// sent.
func BuildPrompt(channel *Channel, messages []*ChannelMessage, codeContext string) string {
	var b strings.Builder
	b.WriteString("Helix deliberation channel ")
	b.WriteString(channel.Name)
	b.WriteString(" (id=")
	b.WriteString(channel.ID)
	b.WriteString(")\n\n")

	if codeContext != "" {
		b.WriteString("## Code context\n\n")
		b.WriteString(codeContext)
		b.WriteString("\n\n")
	}

	b.WriteString("## Channel history\n\n")
	if len(messages) == 0 {
		b.WriteString("(no messages)\n")
	} else {
		for _, m := range messages {
			if m == nil {
				continue
			}
			fmt.Fprintf(&b, "[%s] %s (%s, %s): %s\n",
				m.Timestamp.UTC().Format(time.RFC3339),
				m.Author,
				m.AuthorType,
				m.Type,
				strings.TrimSpace(m.Content),
			)
		}
	}

	b.WriteString("\n## Instructions\n\n")
	b.WriteString("You are Chimera. The agents above disagree. ")
	b.WriteString("Return a structured verdict in this exact format:\n\n")
	b.WriteString("VERDICT: PASS|FAIL|CONDITIONAL\n")
	b.WriteString("Summary: <one-paragraph plain-English summary>\n")
	b.WriteString("Condition: <only when VERDICT is CONDITIONAL — list every condition on its own line; omit the line for PASS/FAIL>\n")

	return b.String()
}

// ErrNotTriggered is returned by DeliberateIfNeeded when the pre-conditions
// for Chimera are not met (channel type, message count, disagreement score,
// or loop guard). Callers should treat it as a benign no-op.
var ErrNotTriggered = fmt.Errorf("channel: deliberation not triggered")
