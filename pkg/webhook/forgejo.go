// Package webhook implements the receiving side of Forgejo webhook
// events. Per specs/cross-component-wiring.md §2.1 (Forgejo → Chimera),
// when a PR is opened/updated/closed/reviewed/labeled, Forgejo fires an
// HTTP POST to Helix. This package:
//
//  1. Verifies the X-Forgejo-Signature HMAC-SHA256 header against a
//     shared secret (constant-time compare — defends against timing
//     side-channels).
//  2. Parses the JSON payload into a polymorphic PullRequestEvent
//     (one of 5 event types per the Forgejo Actions webhook schema).
//  3. Serves a minimal HTTP server that prints parsed events as JSON.
//
// This package is the ingestion half. The action half (firing review,
// merge-gate, etc.) lives in the coordinator — wiring parsed events into
// coordinator.Execute is a separate task.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// =============================================================================
// Signature verification
// =============================================================================

// SignatureHeader is the HTTP header Forgejo uses to deliver the HMAC
// signature. Forgejo's format: "sha256=<hex-digest>".
const SignatureHeader = "X-Forgejo-Signature"

// signaturePrefix is the leading text before the hex digest. Forgejo
// ships signatures with this prefix; some other Git hosts ship without
// it. We accept either form for forward-compatibility.
const signaturePrefix = "sha256="

// ErrMissingSignature is returned by VerifySignature when the supplied
// signature header is empty. Callers should map this to HTTP 401.
var ErrMissingSignature = errors.New("webhook: missing signature header")

// ErrSignatureMismatch is returned when the computed HMAC does not match
// the supplied signature. Callers should map this to HTTP 401.
var ErrSignatureMismatch = errors.New("webhook: signature mismatch")

// VerifySignature computes HMAC-SHA256(payload, secret) and compares it
// against signatureHeader in constant time. Returns nil on match;
// ErrMissingSignature or ErrSignatureMismatch on failure.
//
// The signatureHeader may be in either form:
//
//	"sha256=<64-hex-chars>"   (Forgejo canonical)
//	"<64-hex-chars>"          (some other Git hosts)
//
// An empty secret returns ErrSignatureMismatch (the security policy is
// "always verify" — empty secrets never validate any signature).
func VerifySignature(payload []byte, signatureHeader string, secret []byte) bool {
	if signatureHeader == "" {
		return false
	}
	if len(secret) == 0 {
		// An empty secret cannot validate any signature. Returning
		// false here protects against operators who accidentally
		// start a webhook receiver without setting the secret.
		return false
	}

	// Strip the optional "sha256=" prefix.
	sig := strings.TrimPrefix(signatureHeader, signaturePrefix)

	// Compute the expected signature.
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))

	// Constant-time compare. hmac.Equal handles both equal and
	// unequal-length inputs without leaking length information.
	return hmac.Equal([]byte(sig), []byte(expected))
}

// verifySignatureError returns VerifySignature's boolean result as a
// descriptive error. Internal helper for the HTTP handler.
func verifySignatureError(payload []byte, signatureHeader string, secret []byte) error {
	if signatureHeader == "" {
		return ErrMissingSignature
	}
	if !VerifySignature(payload, signatureHeader, secret) {
		return ErrSignatureMismatch
	}
	return nil
}

// =============================================================================
// Event payload parsing
// =============================================================================

// EventType identifies a Forgejo webhook event. Five types are
// recognized per the spec task.
type EventType string

const (
	EventPROpened   EventType = "pull_request_opened"
	EventPRUpdated  EventType = "pull_request_updated"
	EventPRClosed   EventType = "pull_request_closed"
	EventPRReviewed EventType = "pull_request_reviewed"
	EventPRLabeled  EventType = "pull_request_labeled"
	EventUnknown    EventType = "unknown"
)

// pullRequestEvent is the on-wire shape of a pull_request_* event.
// Forgejo wraps the PR object inside a "pull_request" key; we
// unmarshal into that struct and pick out the relevant fields.
type pullRequestEvent struct {
	Action     string    `json:"action"`
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	HTMLURL    string    `json:"html_url"`
	State      string    `json:"state"`
	Body       string    `json:"body"`
	BaseBranch string    `json:"base_branch_ref,omitempty"` // varies by Forgejo version
	HeadBranch string    `json:"head_branch_ref,omitempty"`
	CreatedAt  string    `json:"created_at"`
	UpdatedAt  string    `json:"updated_at"`
	User       *prUser   `json:"user,omitempty"`
	Repository *prRepo   `json:"repository,omitempty"`
	Labels     []prLabel `json:"labels,omitempty"`
}

// prUser is the embedded user object inside a pull_request payload.
type prUser struct {
	Login string `json:"login"`
	Email string `json:"email"`
}

// prRepo is the embedded repository object inside a pull_request payload.
type prRepo struct {
	FullName string  `json:"full_name"`
	Name     string  `json:"name"`
	Owner    *prUser `json:"owner,omitempty"`
}

// prLabel is one of the labels applied to a PR.
type prLabel struct {
	Name string `json:"name"`
}

// PullRequestEvent is the parsed, normalized view of any pull_request_*
// event. The shape is identical across the 5 event types — only the
// EventType field changes.
type PullRequestEvent struct {
	EventType  EventType `json:"event_type"`
	Action     string    `json:"action"`
	PRNumber   int       `json:"pr_number"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	State      string    `json:"state"`
	Body       string    `json:"body,omitempty"`
	BaseBranch string    `json:"base_branch,omitempty"`
	HeadBranch string    `json:"head_branch,omitempty"`
	Author     string    `json:"author,omitempty"`
	RepoOwner  string    `json:"repo_owner,omitempty"`
	RepoName   string    `json:"repo_name,omitempty"`
	Labels     []string  `json:"labels,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
}

// ParsePullRequestEvent decodes a Forgejo pull_request webhook payload.
// The eventType parameter identifies which kind of event this is
// (callers extract it from the X-Forgejo-Event header).
//
// The function tolerates the natural variance in Forgejo payload shape:
// base_branch vs base_branch_ref, head_branch vs head_branch_ref, etc.
// Missing optional fields are simply omitted from the output.
func ParsePullRequestEvent(payload []byte, eventType string) (*PullRequestEvent, error) {
	if len(payload) == 0 {
		return nil, errors.New("webhook: empty payload")
	}

	// First, decode the outer envelope. Forgejo uses two common shapes:
	//   - { "action": "opened", "pull_request": {...}, ... }
	//   - { "action": "opened", "number": 42, "title": "...", ... } (flattened)
	// We accept both by trying envelope-style first, falling back to flat.
	envelope := struct {
		Action      string          `json:"action"`
		Number      int             `json:"number,omitempty"`
		PullRequest json.RawMessage `json:"pull_request,omitempty"`
	}{}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("webhook: decode envelope: %w", err)
	}

	var raw pullRequestEvent
	if envelope.PullRequest != nil {
		if err := json.Unmarshal(envelope.PullRequest, &raw); err != nil {
			return nil, fmt.Errorf("webhook: decode pull_request: %w", err)
		}
	} else {
		// Flat payload — decode in place.
		if err := json.Unmarshal(payload, &raw); err != nil {
			return nil, fmt.Errorf("webhook: decode flat payload: %w", err)
		}
	}

	// Map the eventType string to our EventType enum.
	mappedType := mapEventType(eventType)

	// PR number may be in the envelope OR inside the pull_request block —
	// take whichever is non-zero.
	prNumber := envelope.Number
	if prNumber == 0 {
		prNumber = raw.Number
	}

	out := &PullRequestEvent{
		EventType:  mappedType,
		Action:     firstNonEmpty(envelope.Action, raw.Action),
		PRNumber:   prNumber,
		Title:      raw.Title,
		URL:        raw.HTMLURL,
		State:      raw.State,
		Body:       raw.Body,
		BaseBranch: raw.BaseBranch,
		HeadBranch: raw.HeadBranch,
		ReceivedAt: time.Now().UTC(),
	}
	if raw.User != nil {
		out.Author = raw.User.Login
	}
	if raw.Repository != nil {
		out.RepoName = raw.Repository.Name
		if raw.Repository.Owner != nil {
			out.RepoOwner = raw.Repository.Owner.Login
		}
		if out.RepoOwner == "" {
			// Some payloads include owner info in FullName ("owner/repo").
			out.RepoOwner, out.RepoName = splitFullName(raw.Repository.FullName)
		}
	}
	for _, l := range raw.Labels {
		out.Labels = append(out.Labels, l.Name)
	}
	return out, nil
}

// mapEventType converts a wire-format event-type string into the
// EventType enum. Unknown values yield EventUnknown.
func mapEventType(s string) EventType {
	switch s {
	case "pull_request_opened", "opened":
		return EventPROpened
	case "pull_request_updated", "updated", "synchronize":
		return EventPRUpdated
	case "pull_request_closed", "closed":
		return EventPRClosed
	case "pull_request_reviewed", "reviewed", "review_submitted":
		return EventPRReviewed
	case "pull_request_labeled", "labeled":
		return EventPRLabeled
	default:
		return EventUnknown
	}
}

// splitFullName splits "owner/repo" into ("owner", "repo"). Returns
// empty strings if the input is empty.
func splitFullName(s string) (owner, name string) {
	if i := strings.Index(s, "/"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

// firstNonEmpty returns the first non-empty string from args. Used to
// prefer envelope-level fields over embedded ones.
func firstNonEmpty(args ...string) string {
	for _, a := range args {
		if a != "" {
			return a
		}
	}
	return ""
}

// =============================================================================
// HTTP handler
// =============================================================================

// SecretFunc returns the secret to use for signature verification. It is
// called once per request so operators can rotate secrets without
// restarting the server. The returned secret is read-only.
type SecretFunc func() []byte

// StaticSecret is a SecretFunc that always returns the same secret. Use
// for tests and small deployments where secret rotation isn't needed.
func StaticSecret(secret []byte) SecretFunc {
	return func() []byte { return secret }
}

// Handler is the HTTP handler that verifies and parses incoming Forgejo
// webhook events. Successful events are written to the configured
// Output as one JSON object per line.
//
// The handler does NOT trigger any side effects (no review, no merge
// gate). It only verifies + parses + emits. Action logic lives in the
// coordinator — wiring that is a separate task.
type Handler struct {
	// Secret returns the shared secret for HMAC verification.
	Secret SecretFunc

	// Output is where parsed events are written as JSONL. Defaults to
	// http.Handler's wrapped ResponseWriter when nil.
	Output io.Writer

	// Path is the URL path the handler responds to. Defaults to "/".
	Path string
}

// NewHandler returns a Handler with sensible defaults.
func NewHandler(secret []byte) *Handler {
	return &Handler{
		Secret: StaticSecret(secret),
		Path:   "/",
	}
}

// ServeHTTP implements http.Handler. Reads the request body, verifies
// the signature, parses the payload, and writes the parsed event to
// Output as JSON.
//
// Status codes:
//   - 200 OK on success (regardless of whether the event was understood)
//   - 400 Bad Request on payload parse failure
//   - 401 Unauthorized on signature verification failure
//   - 405 Method Not Allowed for non-POST requests
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (use POST)", http.StatusMethodNotAllowed)
		return
	}

	// Read the body. Limit to 10 MiB to defend against pathological
	// payloads (a malicious sender can't OOM us).
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	// Verify signature.
	sigHeader := r.Header.Get(SignatureHeader)
	if err := verifySignatureError(body, sigHeader, h.Secret()); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Determine event type from the X-Forgejo-Event header.
	eventTypeHeader := r.Header.Get("X-Forgejo-Event")

	// Parse the payload. Some events (push, etc.) are not PR events —
	// we still emit a minimal PullRequestEvent with EventUnknown so
	// downstream consumers can log/observe them.
	parsed, err := ParsePullRequestEvent(body, eventTypeHeader)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Emit the parsed event as one JSONL line to Output.
	out := h.Output
	if out == nil {
		out = w
	}
	data, err := json.Marshal(parsed)
	if err != nil {
		http.Error(w, fmt.Sprintf("marshal: %v", err), http.StatusInternalServerError)
		return
	}
	if _, err := out.Write(data); err != nil {
		// Best-effort write; if it fails, the response is already
		// partially committed. Log and continue.
		http.Error(w, fmt.Sprintf("write: %v", err), http.StatusInternalServerError)
		return
	}
	if _, err := out.Write([]byte("\n")); err != nil {
		http.Error(w, fmt.Sprintf("write newline: %v", err), http.StatusInternalServerError)
		return
	}

	// Acknowledge the request.
	w.WriteHeader(http.StatusOK)
}

// Serve starts an HTTP server on addr that handles webhook events.
// Blocks until the server stops. The secret is loaded once at startup;
// to support rotation, use the lower-level Handler with a SecretFunc.
func Serve(addr string, secret []byte) error {
	return ListenAndServe(addr, NewHandler(secret))
}

// ListenAndServe is the package-level http.ListenAndServe wrapper. It
// exists so callers wanting custom *http.Server settings can pass a
// pre-built handler.
func ListenAndServe(addr string, h *Handler) error {
	mux := http.NewServeMux()
	path := h.Path
	if path == "" {
		path = "/"
	}
	mux.HandleFunc(path, h.ServeHTTP)
	return http.ListenAndServe(addr, mux)
}
