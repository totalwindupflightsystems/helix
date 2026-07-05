// Package health — notifier.go
//
// Alert notifier with pluggable channels. Implements spec §8.4 alert
// routing: firing alerts are fanned out to all configured notifiers.
//
// Notifier implementations:
//   - StdoutNotifier  — JSON-line per alert to an io.Writer (default: stderr)
//   - FileNotifier    — append JSONL to ~/.helix/alerts.jsonl (mode 0o600)
//   - MultiNotifier   — fan-out, partial-success tolerant
//   - TelegramNotifier — stub via Telegram Bot API (requires env vars)
package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// Notifier Interface
// ============================================================================

// Notifier sends a single AlertResult to a destination channel.
type Notifier interface {
	Name() string
	Send(ctx context.Context, alert AlertResult) error
}

// ============================================================================
// StdoutNotifier
// ============================================================================

// StdoutNotifier writes one JSON line per alert to the configured writer.
// By default it writes to os.Stderr so stdout remains grep-able for CLI tools.
type StdoutNotifier struct {
	W io.Writer
}

// NewStdoutNotifier creates a StdoutNotifier writing to w.
// If w is nil, os.Stderr is used.
func NewStdoutNotifier(w io.Writer) *StdoutNotifier {
	if w == nil {
		w = os.Stderr
	}
	return &StdoutNotifier{W: w}
}

// Name returns "stdout".
func (n *StdoutNotifier) Name() string { return "stdout" }

// Send writes a single JSON-encoded alert line to the writer.
func (n *StdoutNotifier) Send(ctx context.Context, alert AlertResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("stdout notifier: marshal alert: %w", err)
	}
	_, err = fmt.Fprintf(n.W, "%s\n", data)
	return err
}

// ============================================================================
// FileNotifier
// ============================================================================

// FileNotifier appends JSONL alert records to a file (default: ~/.helix/alerts.jsonl).
// The file is created with mode 0o600 and appends are atomic via mutex.
type FileNotifier struct {
	mu   sync.Mutex
	path string
}

// NewFileNotifier creates a FileNotifier writing to the given path.
// If path is empty, defaults to ~/.helix/alerts.jsonl.
// The parent directory is created if missing. The file is created with 0o600.
func NewFileNotifier(path string) (*FileNotifier, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("file notifier: cannot determine home dir: %w", err)
		}
		path = filepath.Join(home, ".helix", "alerts.jsonl")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("file notifier: mkdir %s: %w", dir, err)
	}
	// Create or open the file with append mode and 0o600 perms.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("file notifier: open %s: %w", path, err)
	}
	f.Close()
	return &FileNotifier{path: path}, nil
}

// Name returns "file".
func (n *FileNotifier) Name() string { return "file" }

// Path returns the configured file path.
func (n *FileNotifier) Path() string { return n.path }

// Send appends a single JSON-encoded alert line to the file.
func (n *FileNotifier) Send(ctx context.Context, alert AlertResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("file notifier: marshal alert: %w", err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	f, err := os.OpenFile(n.path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("file notifier: open %s: %w", n.path, err)
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// ============================================================================
// MultiNotifier
// ============================================================================

// MultiNotifier fans out alerts to multiple notifiers.
// It collects errors from all notifiers and returns them joined.
// If any notifier fails, the others still receive the alert.
type MultiNotifier struct {
	notifiers []Notifier
}

// NewMultiNotifier creates a MultiNotifier from the given notifiers.
func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

// Name returns "multi" with the names of all child notifiers.
func (n *MultiNotifier) Name() string {
	names := make([]string, len(n.notifiers))
	for i, nt := range n.notifiers {
		names[i] = nt.Name()
	}
	return "multi(" + strings.Join(names, ",") + ")"
}

// Add appends a notifier to the fan-out list.
func (n *MultiNotifier) Add(nt Notifier) {
	n.notifiers = append(n.notifiers, nt)
}

// Notifiers returns the list of child notifiers.
func (n *MultiNotifier) Notifiers() []Notifier {
	return n.notifiers
}

// Send dispatches the alert to all child notifiers.
// Errors from individual notifiers are collected and returned as a joined error.
// If all succeed, returns nil.
func (n *MultiNotifier) Send(ctx context.Context, alert AlertResult) error {
	var errs []string
	for _, nt := range n.notifiers {
		if err := nt.Send(ctx, alert); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", nt.Name(), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("multi notifier: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ============================================================================
// TelegramNotifier
// ============================================================================

// TelegramNotifier sends alerts via the Telegram Bot API.
// Requires TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID env vars.
// Calls https://api.telegram.org/bot{token}/sendMessage with Markdown formatting.
// Fail-fast on 4xx, retry-once on 5xx, 10s timeout.
type TelegramNotifier struct {
	BotToken string
	ChatID   string
	Timeout  time.Duration
	Client   *http.Client
}

// NewTelegramNotifier creates a TelegramNotifier from env vars.
// Returns nil (not an error) if TELEGRAM_BOT_TOKEN or TELEGRAM_CHAT_ID are unset.
// Callers should check for nil before adding to a MultiNotifier.
func NewTelegramNotifier() *TelegramNotifier {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if token == "" || chatID == "" {
		return nil
	}
	return &TelegramNotifier{
		BotToken: token,
		ChatID:   chatID,
		Timeout:  10 * time.Second,
		Client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Name returns "telegram".
func (n *TelegramNotifier) Name() string { return "telegram" }

// formatTelegramMessage renders an AlertResult as a Telegram Markdown message.
func (n *TelegramNotifier) formatTelegramMessage(alert AlertResult) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("*%s* — %s\n", alert.Rule.Name, string(alert.State)))
	b.WriteString(fmt.Sprintf("Severity: `%s`\n", string(alert.Rule.Severity)))
	b.WriteString(fmt.Sprintf("Value: %.2f (threshold: %.2f)\n", alert.Value, alert.Threshold))
	b.WriteString(fmt.Sprintf("Annotation: %s\n", alert.Rule.Annotation))
	if alert.FiredAt != (time.Time{}) {
		b.WriteString(fmt.Sprintf("Fired at: %s\n", alert.FiredAt.Format(time.RFC3339)))
	}
	if len(alert.Labels) > 0 {
		var labels []string
		for k, v := range alert.Labels {
			labels = append(labels, fmt.Sprintf("%s=%s", k, v))
		}
		b.WriteString(fmt.Sprintf("Labels: %s\n", strings.Join(labels, ", ")))
	}
	return b.String()
}

// Send posts the alert to the Telegram Bot API.
// 4xx → fail-fast (auth/config error). 5xx → retry once. 10s timeout.
func (n *TelegramNotifier) Send(ctx context.Context, alert AlertResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if n.BotToken == "" || n.ChatID == "" {
		return fmt.Errorf("telegram notifier: TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID required")
	}
	if n.Timeout <= 0 {
		n.Timeout = 10 * time.Second
	}
	if n.Client == nil {
		n.Client = &http.Client{Timeout: n.Timeout}
	}

	msg := n.formatTelegramMessage(alert)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.BotToken)
	body := fmt.Sprintf(`{"chat_id":%q,"text":%q,"parse_mode":"Markdown"}`, n.ChatID, msg)

	doRequest := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return n.Client.Do(req)
	}

	resp, err := doRequest()
	if err != nil {
		return fmt.Errorf("telegram notifier: request failed: %w", err)
	}

	// Retry once on 5xx
	if resp.StatusCode >= 500 {
		resp.Body.Close()
		resp, err = doRequest()
		if err != nil {
			return fmt.Errorf("telegram notifier: retry failed: %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram notifier: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ============================================================================
// NotifyEngine — ties AlertEngine + Notifier together
// ============================================================================

// NotifyEngine evaluates alerts and dispatches firing ones to notifiers.
type NotifyEngine struct {
	engine     *AlertEngine
	notifier   Notifier
	dryRun     bool
	onlyFiring bool
}

// NewNotifyEngine creates a NotifyEngine wrapping an AlertEngine and a Notifier.
func NewNotifyEngine(engine *AlertEngine, notifier Notifier) *NotifyEngine {
	return &NotifyEngine{
		engine:     engine,
		notifier:   notifier,
		onlyFiring: true,
	}
}

// WithDryRun sets dry-run mode: alerts are evaluated but not sent.
func (e *NotifyEngine) WithDryRun(dry bool) *NotifyEngine {
	e.dryRun = dry
	return e
}

// WithOnlyFiring controls whether only firing alerts (true) or all alerts (false)
// are dispatched to notifiers. Default: true.
func (e *NotifyEngine) WithOnlyFiring(only bool) *NotifyEngine {
	e.onlyFiring = only
	return e
}

// NotifyReport summarizes the notification dispatch.
type NotifyReport struct {
	Summary   AlertSummary `json:"summary"`
	Notified  int          `json:"notified"`
	Skipped   int          `json:"skipped"`
	Errors    []string     `json:"errors,omitempty"`
	DryRun    bool         `json:"dry_run"`
	Notifiers []string     `json:"notifiers"`
}

// EvaluateAndNotify evaluates alert rules against a snapshot and dispatches
// firing alerts to the configured notifier(s). Returns a NotifyReport.
func (e *NotifyEngine) EvaluateAndNotify(ctx context.Context, snapshot MetricsSnapshot) NotifyReport {
	results := e.engine.EvaluateRules(snapshot)
	summary := SummarizeResults(results)

	report := NotifyReport{
		Summary: summary,
		DryRun:  e.dryRun,
	}
	if e.notifier != nil {
		report.Notifiers = []string{e.notifier.Name()}
		if mn, ok := e.notifier.(*MultiNotifier); ok {
			report.Notifiers = nil
			for _, child := range mn.Notifiers() {
				report.Notifiers = append(report.Notifiers, child.Name())
			}
		}
	}

	for _, r := range results {
		if e.onlyFiring && !r.IsFiring() {
			report.Skipped++
			continue
		}

		if e.dryRun {
			report.Notified++
			continue
		}

		if e.notifier == nil {
			report.Notified++
			continue
		}

		if err := e.notifier.Send(ctx, r); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", e.notifier.Name(), err))
		} else {
			report.Notified++
		}
	}

	return report
}

// FormatNotifyReport renders a human-readable summary of the notification dispatch.
func FormatNotifyReport(report NotifyReport) string {
	var b strings.Builder
	b.WriteString(report.Summary.FormatSummary())
	b.WriteString("\n")
	if report.DryRun {
		b.WriteString(fmt.Sprintf("[DRY RUN] Would notify %d alerts to %s\n",
			report.Notified, strings.Join(report.Notifiers, ", ")))
	} else {
		b.WriteString(fmt.Sprintf("Notified %d alerts to %s (%d skipped)\n",
			report.Notified, strings.Join(report.Notifiers, ", "), report.Skipped))
	}
	if len(report.Errors) > 0 {
		b.WriteString(fmt.Sprintf("Errors (%d):\n", len(report.Errors)))
		for _, e := range report.Errors {
			b.WriteString(fmt.Sprintf("  - %s\n", e))
		}
	}
	return b.String()
}

// ============================================================================
// Helpers
// ============================================================================

// LoadMetricsSnapshotFromJSON reads a JSON-encoded MetricsSnapshot from a file path.
func LoadMetricsSnapshotFromJSON(path string) (MetricsSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("read metrics file %s: %w", path, err)
	}
	var snap MetricsSnapshot
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&snap); err != nil {
		return MetricsSnapshot{}, fmt.Errorf("decode metrics JSON: %w", err)
	}
	if snap.Timestamp.IsZero() {
		snap.Timestamp = time.Now()
	}
	if snap.AgentCosts == nil {
		snap.AgentCosts = make(map[string]float64)
	}
	if snap.GatePassRates == nil {
		snap.GatePassRates = make(map[string]float64)
	}
	if snap.PRCycleTimes == nil {
		snap.PRCycleTimes = make(map[string]float64)
	}
	if snap.AgentUptimes == nil {
		snap.AgentUptimes = make(map[string]float64)
	}
	if snap.CostPerPR == nil {
		snap.CostPerPR = make(map[string]float64)
	}
	if snap.WeeklyAvgCostPerPR == nil {
		snap.WeeklyAvgCostPerPR = make(map[string]float64)
	}
	return snap, nil
}

// NotifyReportToJSON marshals a NotifyReport to indented JSON.
func NotifyReportToJSON(report NotifyReport) (string, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
