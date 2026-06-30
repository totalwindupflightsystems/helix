package forgejo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ============================================================================
// Webhook Event Types
// ============================================================================

// EventType is the Forgejo webhook event type header value.
type EventType string

const (
	EventPROpened        EventType = "pull_request_opened"
	EventPRUpdated       EventType = "pull_request_updated"
	EventPRClosed        EventType = "pull_request_closed"
	EventPush            EventType = "push"
	EventReviewSubmitted EventType = "review_submitted"
	EventUnknown         EventType = "unknown"
)

// WebhookPayload is the raw payload from Forgejo.
// Forgejo sends JSON with action-specific fields.
type WebhookPayload struct {
	Action      string          `json:"action"`
	PRNumber    int             `json:"number,omitempty"`
	PullRequest json.RawMessage `json:"pull_request,omitempty"`
	Repository  json.RawMessage `json:"repository,omitempty"`
	Sender      json.RawMessage `json:"sender,omitempty"`
	Commits     json.RawMessage `json:"commits,omitempty"`
	Review      json.RawMessage `json:"review,omitempty"`
}

// PREventInfo is parsed PR-related info from a webhook.
type PREventInfo struct {
	PRNumber  int    `json:"pr_number"`
	PRURL     string `json:"pr_url"`
	Title     string `json:"title"`
	Action    string `json:"action"`
	Author    string `json:"author"`
	RepoOwner string `json:"repo_owner"`
	RepoName  string `json:"repo_name"`
}

// PushEventInfo is parsed push event info.
type PushEventInfo struct {
	Ref       string   `json:"ref"`
	Before    string   `json:"before"`
	After     string   `json:"after"`
	Commits   []string `json:"commits"`
	RepoOwner string   `json:"repo_owner"`
	RepoName  string   `json:"repo_name"`
	Pusher    string   `json:"pusher"`
}

// ReviewEventInfo is parsed review submission info.
type ReviewEventInfo struct {
	PRNumber int    `json:"pr_number"`
	PRURL    string `json:"pr_url"`
	Reviewer string `json:"reviewer"`
	Type     string `json:"review_type"`
	Body     string `json:"body"`
	State    string `json:"state"`
}

// ============================================================================
// Webhook Result
// ============================================================================

// WebhookAction is what the handler decided to do.
type WebhookAction string

const (
	ActionProcessed WebhookAction = "processed"
	ActionSkipped   WebhookAction = "skipped"
	ActionError     WebhookAction = "error"
)

// WebhookResult is the outcome of processing a webhook event.
type WebhookResult struct {
	EventType  EventType        `json:"event_type"`
	Action     WebhookAction    `json:"action"`
	Message    string           `json:"message,omitempty"`
	PRInfo     *PREventInfo     `json:"pr_info,omitempty"`
	PushInfo   *PushEventInfo   `json:"push_info,omitempty"`
	ReviewInfo *ReviewEventInfo `json:"review_info,omitempty"`
}

// ============================================================================
// Event Handler Interface
// ============================================================================

// EventHandler is a callback for a specific event type.
// Returns a result describing what was done.
type EventHandler interface {
	OnPROpened(info PREventInfo) WebhookResult
	OnPRUpdated(info PREventInfo) WebhookResult
	OnPRClosed(info PREventInfo) WebhookResult
	OnPush(info PushEventInfo) WebhookResult
	OnReviewSubmitted(info ReviewEventInfo) WebhookResult
}

// ============================================================================
// WebhookHandler
// ============================================================================

// WebhookHandler receives and dispatches Forgejo webhook events.
type WebhookHandler struct {
	// secret is the HMAC shared secret for signature verification.
	// If empty, signature verification is skipped.
	secret []byte
	// handler is the event callback target.
	handler EventHandler
}

// WebhookOption configures a WebhookHandler.
type WebhookOption func(*WebhookHandler)

// WithWebhookSecret sets the HMAC verification secret.
func WithWebhookSecret(secret string) WebhookOption {
	return func(h *WebhookHandler) { h.secret = []byte(secret) }
}

// WithEventHandler sets the event handler callback.
func WithEventHandler(handler EventHandler) WebhookOption {
	return func(h *WebhookHandler) { h.handler = handler }
}

// NewWebhookHandler creates a webhook handler.
func NewWebhookHandler(opts ...WebhookOption) *WebhookHandler {
	h := &WebhookHandler{}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ============================================================================
// Signature Verification
// ============================================================================

// VerifySignature checks the HMAC-SHA256 signature of the request body.
// The Forgejo header is X-Forgejo-Signature (or X-Gitea-Signature for compat).
// Returns true if the signature is valid or if no secret is configured.
func (h *WebhookHandler) VerifySignature(header http.Header, body []byte) bool {
	if len(h.secret) == 0 {
		return true // no secret configured, skip verification
	}

	sig := header.Get("X-Forgejo-Signature")
	if sig == "" {
		sig = header.Get("X-Gitea-Signature") // backward compat
	}
	if sig == "" {
		return false // secret configured but no signature header
	}

	mac := hmac.New(sha256.New, h.secret)
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expected))
}

// ============================================================================
// Parse — extract event type and payload
// ============================================================================

// ParseEventType extracts the event type from the request headers.
// Forgejo sends the event type in the X-Forgejo-Event header (or X-Gitea-Event).
func ParseEventType(header http.Header) EventType {
	event := header.Get("X-Forgejo-Event")
	if event == "" {
		event = header.Get("X-Gitea-Event")
	}
	return MapEventType(event)
}

// MapEventType converts a Forgejo event header string to an EventType.
func MapEventType(event string) EventType {
	event = strings.ToLower(strings.TrimSpace(event))
	switch {
	case strings.Contains(event, "pull_request"):
		return EventPROpened // will be refined by action in payload
	case strings.Contains(event, "push"):
		return EventPush
	case strings.Contains(event, "review"):
		return EventReviewSubmitted
	default:
		return EventUnknown
	}
}

// ParseWebhook parses the raw request body into a WebhookPayload.
func ParseWebhook(body []byte) (*WebhookPayload, error) {
	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse webhook payload: %w", err)
	}
	return &payload, nil
}

// ============================================================================
// Parse PREventInfo from payload
// ============================================================================

// ParsePRInfo extracts PR-related info from a webhook payload.
func ParsePRInfo(payload *WebhookPayload) (*PREventInfo, error) {
	if payload.PRNumber == 0 && len(payload.PullRequest) == 0 {
		return nil, fmt.Errorf("no PR data in payload")
	}

	info := &PREventInfo{
		PRNumber: payload.PRNumber,
		Action:   payload.Action,
	}

	// Try to extract more detail from pull_request JSON.
	if len(payload.PullRequest) > 0 {
		var pr struct {
			URL   string `json:"url"`
			Title string `json:"title"`
			User  struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err := json.Unmarshal(payload.PullRequest, &pr); err == nil {
			info.PRURL = pr.URL
			info.Title = pr.Title
			info.Author = pr.User.Login
		}
	}

	// Try to extract repo info.
	if len(payload.Repository) > 0 {
		var repo struct {
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(payload.Repository, &repo); err == nil {
			info.RepoOwner = repo.Owner.Login
			info.RepoName = repo.Name
		}
	}

	return info, nil
}

// ParsePushInfo extracts push event info from a webhook payload.
func ParsePushInfo(payload *WebhookPayload) (*PushEventInfo, error) {
	info := &PushEventInfo{}

	// Extract commits.
	if len(payload.Commits) > 0 {
		var commits []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(payload.Commits, &commits); err == nil {
			for _, c := range commits {
				info.Commits = append(info.Commits, c.ID)
			}
		}
	}

	// Extract repo info.
	if len(payload.Repository) > 0 {
		var repo struct {
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(payload.Repository, &repo); err == nil {
			info.RepoOwner = repo.Owner.Login
			info.RepoName = repo.Name
		}
	}

	return info, nil
}

// ParseReviewInfo extracts review submission info from a webhook payload.
func ParseReviewInfo(payload *WebhookPayload) (*ReviewEventInfo, error) {
	info := &ReviewEventInfo{}

	if payload.PRNumber > 0 {
		info.PRNumber = payload.PRNumber
	}

	if len(payload.Review) > 0 {
		var review struct {
			Reviewer struct {
				Login string `json:"login"`
			} `json:"reviewer"`
			Type  string `json:"type"`
			Body  string `json:"body"`
			State string `json:"state"`
		}
		if err := json.Unmarshal(payload.Review, &review); err == nil {
			info.Reviewer = review.Reviewer.Login
			info.Type = review.Type
			info.Body = review.Body
			info.State = review.State
		}
	}

	return info, nil
}

// ============================================================================
// Handle — process a webhook request
// ============================================================================

// HandleRequest processes an incoming HTTP webhook request from Forgejo.
// It verifies the signature, parses the event, and dispatches to the handler.
func (h *WebhookHandler) HandleRequest(r *http.Request) WebhookResult {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return WebhookResult{
			EventType: EventUnknown,
			Action:    ActionError,
			Message:   fmt.Sprintf("read body: %v", err),
		}
	}
	defer r.Body.Close()

	return h.HandleBody(r.Header, body)
}

// HandleBody processes a webhook given the raw headers and body bytes.
// Useful for testing without constructing a full http.Request.
func (h *WebhookHandler) HandleBody(header http.Header, body []byte) WebhookResult {
	// Verify signature.
	if !h.VerifySignature(header, body) {
		return WebhookResult{
			EventType: EventUnknown,
			Action:    ActionError,
			Message:   "HMAC signature verification failed",
		}
	}

	// Parse event type.
	eventType := ParseEventType(header)
	if eventType == EventUnknown {
		return WebhookResult{
			EventType: EventUnknown,
			Action:    ActionSkipped,
			Message:   fmt.Sprintf("unrecognized event type: %s", header.Get("X-Forgejo-Event")),
		}
	}

	// Parse payload.
	payload, err := ParseWebhook(body)
	if err != nil {
		return WebhookResult{
			EventType: eventType,
			Action:    ActionError,
			Message:   fmt.Sprintf("parse payload: %v", err),
		}
	}

	// Dispatch to handler.
	if h.handler == nil {
		return WebhookResult{
			EventType: eventType,
			Action:    ActionSkipped,
			Message:   "no event handler configured",
		}
	}

	return h.dispatch(eventType, payload)
}

// dispatch routes the event to the appropriate handler method.
func (h *WebhookHandler) dispatch(eventType EventType, payload *WebhookPayload) WebhookResult {
	switch eventType {
	case EventPROpened:
		info, err := ParsePRInfo(payload)
		if err != nil {
			return WebhookResult{EventType: eventType, Action: ActionError, Message: err.Error()}
		}
		// Refine event type based on action.
		action := strings.ToLower(payload.Action)
		switch {
		case action == "opened" || action == "reopened":
			result := h.handler.OnPROpened(*info)
			result.EventType = EventPROpened
			result.PRInfo = info
			return result
		case action == "closed":
			result := h.handler.OnPRClosed(*info)
			result.EventType = EventPRClosed
			result.PRInfo = info
			return result
		default:
			result := h.handler.OnPRUpdated(*info)
			result.EventType = EventPRUpdated
			result.PRInfo = info
			return result
		}

	case EventPush:
		info, _ := ParsePushInfo(payload)
		result := h.handler.OnPush(*info)
		result.EventType = EventPush
		result.PushInfo = info
		return result

	case EventReviewSubmitted:
		info, _ := ParseReviewInfo(payload)
		result := h.handler.OnReviewSubmitted(*info)
		result.EventType = EventReviewSubmitted
		result.ReviewInfo = info
		return result

	default:
		return WebhookResult{
			EventType: eventType,
			Action:    ActionSkipped,
			Message:   fmt.Sprintf("unhandled event type: %s", eventType),
		}
	}
}

// ============================================================================
// NoOpHandler — default handler that skips everything
// ============================================================================

// NoOpHandler is an EventHandler that skips all events.
// Useful as a default when the caller just wants to verify parsing.
type NoOpHandler struct{}

func (n NoOpHandler) OnPROpened(info PREventInfo) WebhookResult {
	return WebhookResult{Action: ActionSkipped, Message: "no-op handler"}
}
func (n NoOpHandler) OnPRUpdated(info PREventInfo) WebhookResult {
	return WebhookResult{Action: ActionSkipped, Message: "no-op handler"}
}
func (n NoOpHandler) OnPRClosed(info PREventInfo) WebhookResult {
	return WebhookResult{Action: ActionSkipped, Message: "no-op handler"}
}
func (n NoOpHandler) OnPush(info PushEventInfo) WebhookResult {
	return WebhookResult{Action: ActionSkipped, Message: "no-op handler"}
}
func (n NoOpHandler) OnReviewSubmitted(info ReviewEventInfo) WebhookResult {
	return WebhookResult{Action: ActionSkipped, Message: "no-op handler"}
}
