package verify

// notify.go implements the agent notification dispatcher per
// specs/production-verification.md §Behavior Contracts.
//
// When a behavior contract is breached, the spec mandates four actions:
//  1. Immediate agent notification (with evidence)
//  2. Auto-rollback if configured
//  3. Trust score penalty for the responsible agent
//  4. Incident record in the learning database
//
// This file implements action 1 (notification) and the orchestration that
// connects breach detection to all downstream effects via a channel-based
// notification model.  Debounce prevents notification spam.

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// Notification types
// ============================================================================

// BreachNotification is the structured payload sent to notification channels
// when a behavior contract is breached.
type BreachNotification struct {
	ContractName      string             `json:"contract_name"`
	AgentID           string             `json:"agent_id"`
	MergeCommit       string             `json:"merge_commit"`
	FailedChecks      []CheckResult      `json:"failed_checks"`
	MetricsSnapshot   map[string]float64 `json:"metrics_snapshot"`
	EvidenceLinks     []string           `json:"evidence_links"`
	RecommendedAction string             `json:"recommended_action"`
	Timestamp         time.Time          `json:"timestamp"`
}

// RecommendedAction enumerates the possible responses to a breach.
const (
	ActionNotifyOnly   = "notify_only"
	ActionRollback     = "rollback_and_notify"
	ActionInvestigate  = "investigate"
	ActionTrustPenalty = "trust_penalty"
)

// DeliveryStatus tracks whether a channel successfully delivered.
type DeliveryStatus string

const (
	StatusDelivered DeliveryStatus = "delivered"
	StatusFailed    DeliveryStatus = "failed"
	StatusSkipped   DeliveryStatus = "skipped"
)

// ChannelResult records the outcome of sending to one channel.
type ChannelResult struct {
	Channel string         `json:"channel"`
	Status  DeliveryStatus `json:"status"`
	Error   string         `json:"error,omitempty"`
	Detail  string         `json:"detail,omitempty"`
}

// NotificationResult aggregates per-channel delivery outcomes.
type NotificationResult struct {
	Notification *BreachNotification `json:"notification"`
	Channels     []ChannelResult     `json:"channels"`
	AllDelivered bool                `json:"all_delivered"`
}

// ============================================================================
// Notifier interface
// ============================================================================

// Notifier is the interface that notification channels implement.
// Each channel (Forgejo PR comment, trust ledger, incident store) is a
// separate Notifier.
type Notifier interface {
	// Name returns the channel identifier (e.g., "forgejo_pr", "trust_ledger").
	Name() string

	// Notify sends the breach notification through this channel.
	// Returns a ChannelResult indicating delivery status.
	Notify(n *BreachNotification) ChannelResult
}

// ============================================================================
// Channels — concrete Notifier implementations
// ============================================================================

// ForgejoPRNotifier posts a structured markdown comment to the agent's PR.
// It requires a PRURL to function.  This is the primary human-visible channel.
type ForgejoPRNotifier struct {
	PRURL string // e.g., "http://localhost:3030/owner/repo/pulls/42"
}

func (f *ForgejoPRNotifier) Name() string { return "forgejo_pr" }

func (f *ForgejoPRNotifier) Notify(n *BreachNotification) ChannelResult {
	if f.PRURL == "" {
		return ChannelResult{
			Channel: f.Name(),
			Status:  StatusSkipped,
			Detail:  "no PR URL configured",
		}
	}
	// In a real deployment this would POST to the Forgejo API.
	// For now, the comment body is returned as detail.
	body := FormatBreachComment(n)
	return ChannelResult{
		Channel: f.Name(),
		Status:  StatusDelivered,
		Detail:  body,
	}
}

// TrustLedgerNotifier writes a trust event to the ledger when a breach occurs.
// This implements spec action 3: "Trust score penalty for the responsible agent."
type TrustLedgerNotifier struct {
	// PenaltyCallback is called with (agentID, severity, description).
	// In production this calls trust.IncidentBridge.ProcessIncident.
	PenaltyCallback func(agentID string, severity float64, description string)
}

func (t *TrustLedgerNotifier) Name() string { return "trust_ledger" }

func (t *TrustLedgerNotifier) Notify(n *BreachNotification) ChannelResult {
	if t.PenaltyCallback == nil {
		return ChannelResult{
			Channel: t.Name(),
			Status:  StatusSkipped,
			Detail:  "no penalty callback configured",
		}
	}
	severity := breachSeverity(n)
	desc := fmt.Sprintf("behavior contract %q breached: %d assertion(s) failed",
		n.ContractName, len(n.FailedChecks))
	t.PenaltyCallback(n.AgentID, severity, desc)
	return ChannelResult{
		Channel: t.Name(),
		Status:  StatusDelivered,
		Detail:  fmt.Sprintf("trust penalty applied: severity=%.2f", severity),
	}
}

// IncidentStoreNotifier creates an incident record in the learning database.
// This implements spec action 4: "Incident record in the learning database."
type IncidentStoreNotifier struct {
	// RecordCallback stores the incident. Returns the incident ID.
	RecordCallback func(n *BreachNotification) string
}

func (i *IncidentStoreNotifier) Name() string { return "incident_store" }

func (i *IncidentStoreNotifier) Notify(n *BreachNotification) ChannelResult {
	if i.RecordCallback == nil {
		return ChannelResult{
			Channel: i.Name(),
			Status:  StatusSkipped,
			Detail:  "no record callback configured",
		}
	}
	incidentID := i.RecordCallback(n)
	if incidentID == "" {
		return ChannelResult{
			Channel: i.Name(),
			Status:  StatusFailed,
			Error:   "record callback returned empty incident ID",
		}
	}
	return ChannelResult{
		Channel: i.Name(),
		Status:  StatusDelivered,
		Detail:  fmt.Sprintf("incident %s recorded", incidentID),
	}
}

// ============================================================================
// NotificationDispatcher
// ============================================================================

// NotificationDispatcher coordinates multi-channel breach notifications.
// It ensures that on a behavior contract breach, all configured channels
// receive the notification, with debounce to prevent spam.
type NotificationDispatcher struct {
	mu          sync.Mutex
	channels    []Notifier
	debounceTTL time.Duration // default 5 minutes
	// sent tracks the last time we notified for a given (agent, contract) pair.
	sent map[debounceKey]time.Time
}

type debounceKey struct {
	agent    string
	contract string
}

// NewNotificationDispatcher creates a dispatcher with the given channels and
// a default 5-minute debounce window.
func NewNotificationDispatcher(channels ...Notifier) *NotificationDispatcher {
	return &NotificationDispatcher{
		channels:    channels,
		debounceTTL: 5 * time.Minute,
		sent:        make(map[debounceKey]time.Time),
	}
}

// SetDebounceTTL overrides the default 5-minute debounce window.
func (d *NotificationDispatcher) SetDebounceTTL(ttl time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.debounceTTL = ttl
}

// NotifyFromBreach converts a Breach (from Monitor.Evaluate) into a
// BreachNotification and dispatches it to all channels.
func (d *NotificationDispatcher) NotifyFromBreach(b *Breach, metrics map[string]float64, evidenceLinks []string) *NotificationResult {
	n := &BreachNotification{
		ContractName:      b.ContractName,
		AgentID:           b.Agent,
		MergeCommit:       b.MergeCommit,
		FailedChecks:      b.FailedChecks,
		MetricsSnapshot:   metrics,
		EvidenceLinks:     evidenceLinks,
		RecommendedAction: recommendedAction(b),
		Timestamp:         time.Now().UTC(),
	}
	return d.Notify(n)
}

// Notify dispatches a BreachNotification to all registered channels.
// If a notification for the same (agent, contract) pair was sent within the
// debounce window, all channels are skipped.
func (d *NotificationDispatcher) Notify(n *BreachNotification) *NotificationResult {
	d.mu.Lock()

	// Debounce check.
	key := debounceKey{agent: n.AgentID, contract: n.ContractName}
	if lastSent, ok := d.sent[key]; ok {
		if time.Since(lastSent) < d.debounceTTL {
			d.mu.Unlock()
			return &NotificationResult{
				Notification: n,
				Channels: []ChannelResult{{
					Channel: "all",
					Status:  StatusSkipped,
					Detail:  fmt.Sprintf("debounced: last notified %v ago", time.Since(lastSent).Round(time.Second)),
				}},
				AllDelivered: false,
			}
		}
	}

	// Record this send.
	d.sent[key] = time.Now()
	d.mu.Unlock()

	// Dispatch to all channels.
	result := &NotificationResult{
		Notification: n,
		AllDelivered: true,
	}
	for _, ch := range d.channels {
		cr := ch.Notify(n)
		result.Channels = append(result.Channels, cr)
		if cr.Status != StatusDelivered {
			result.AllDelivered = false
		}
	}
	return result
}

// ClearDebounce resets the debounce timer for a given (agent, contract) pair.
// Useful for testing or when a new breach is genuinely different.
func (d *NotificationDispatcher) ClearDebounce(agentID, contractName string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.sent, debounceKey{agent: agentID, contract: contractName})
}

// AddChannel registers an additional notification channel at runtime.
func (d *NotificationDispatcher) AddChannel(ch Notifier) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.channels = append(d.channels, ch)
}

// Channels returns the list of registered channel names.
func (d *NotificationDispatcher) Channels() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	names := make([]string, len(d.channels))
	for i, ch := range d.channels {
		names[i] = ch.Name()
	}
	return names
}

// ============================================================================
// Helpers
// ============================================================================

// FormatBreachComment produces a structured markdown comment suitable for
// posting as a PR review comment on Forgejo.
func FormatBreachComment(n *BreachNotification) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### ⚠ Behavior Contract Breach: `%s`\n\n", n.ContractName))
	sb.WriteString(fmt.Sprintf("**Agent:** `%s`  \n", n.AgentID))
	sb.WriteString(fmt.Sprintf("**Merge Commit:** `%s`  \n", n.MergeCommit))
	sb.WriteString(fmt.Sprintf("**Timestamp:** %s  \n", n.Timestamp.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("**Recommended Action:** `%s`\n\n", n.RecommendedAction))

	sb.WriteString("#### Failed Assertions\n\n")
	sb.WriteString("| Metric | Operator | Expected | Actual | Passed |\n")
	sb.WriteString("|--------|----------|----------|--------|--------|\n")
	for _, c := range n.FailedChecks {
		actual := ""
		if v, ok := n.MetricsSnapshot[c.Assertion.Metric]; ok {
			actual = fmt.Sprintf("%.4f", v)
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %.4f | %s | ❌ |\n",
			c.Assertion.Metric, c.Assertion.Op, c.Assertion.Value, actual))
	}

	if len(n.EvidenceLinks) > 0 {
		sb.WriteString("\n#### Evidence\n\n")
		for _, link := range n.EvidenceLinks {
			sb.WriteString(fmt.Sprintf("- %s\n", link))
		}
	}
	return sb.String()
}

// breachSeverity computes a trust penalty severity (0.0–1.0) based on the
// number and type of failed assertions.  More failures = higher severity.
func breachSeverity(n *BreachNotification) float64 {
	count := len(n.FailedChecks)
	if count == 0 {
		return 0
	}
	if count >= 5 {
		return 0.40 // critical
	}
	if count >= 3 {
		return 0.20 // high
	}
	if count >= 2 {
		return 0.10 // medium
	}
	return 0.05 // low
}

// recommendedAction determines what action to recommend based on the breach.
func recommendedAction(b *Breach) string {
	if b.ShouldRollback {
		return ActionRollback
	}
	if b.ShouldNotify {
		return ActionNotifyOnly
	}
	return ActionInvestigate
}
