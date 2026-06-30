package estimate

import (
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Budget period management (spec §8.3 — Budget Period Management)
// ---------------------------------------------------------------------------

// PeriodConfig holds the configuration for weekly budget period management.
type PeriodConfig struct {
	// ResetHourUTC is the hour (0-23) at which the weekly reset runs.
	// Default: 0 (midnight UTC). Per spec: "Cron job runs Sunday 00:01 UTC".
	ResetHourUTC int
	// AlertWindowMinutes is the time before reset when ShouldResetAlert returns true.
	// Default: 60 (1 hour).
	AlertWindowMinutes int
}

// DefaultPeriodConfig returns the spec-compliant default configuration.
func DefaultPeriodConfig() PeriodConfig {
	return PeriodConfig{
		ResetHourUTC:       0,
		AlertWindowMinutes: 60,
	}
}

// PeriodManager manages the weekly budget period lifecycle per spec §8.3:
//   - Period: Sunday 00:00 UTC to Saturday 23:59:59 UTC
//   - Reset: budget_used_usd = 0 for all agents
//   - Rollover: NOT supported in v1
//   - Overdraft: NOT allowed
type PeriodManager struct {
	config PeriodConfig
}

// NewPeriodManager creates a manager with the given config (or default).
func NewPeriodManager(config PeriodConfig) *PeriodManager {
	if config.ResetHourUTC < 0 || config.ResetHourUTC > 23 {
		config = DefaultPeriodConfig()
	}
	if config.AlertWindowMinutes <= 0 {
		config.AlertWindowMinutes = 60
	}
	return &PeriodManager{config: config}
}

// PeriodStart returns the start of the budget period containing the given time.
// The period starts on Sunday at ResetHourUTC.
func (pm *PeriodManager) PeriodStart(t time.Time) time.Time {
	// Normalize to UTC.
	t = t.UTC()

	// Find the most recent Sunday at ResetHourUTC.
	daysSinceSunday := int(t.Weekday()) // Sunday=0, Monday=1, ...
	start := time.Date(t.Year(), t.Month(), t.Day(), pm.config.ResetHourUTC, 0, 0, 0, time.UTC)
	start = start.AddDate(0, 0, -daysSinceSunday)

	// If current time is before today's reset, we're in last week's period.
	if t.Before(time.Date(t.Year(), t.Month(), t.Day(), pm.config.ResetHourUTC, 0, 0, 0, time.UTC)) && daysSinceSunday == 0 {
		// Before Sunday's reset hour → still in previous period.
		start = start.AddDate(0, 0, -7)
	}

	return start
}

// PeriodEnd returns the end of the budget period containing the given time.
// The period ends on the following Sunday at ResetHourUTC (exclusive).
func (pm *PeriodManager) PeriodEnd(t time.Time) time.Time {
	return pm.PeriodStart(t).AddDate(0, 0, 7)
}

// IsInPeriod checks whether a timestamp falls within the current budget period
// relative to the reference time (typically time.Now()).
func (pm *PeriodManager) IsInPeriod(checkTime, referenceTime time.Time) bool {
	start := pm.PeriodStart(referenceTime)
	end := pm.PeriodEnd(referenceTime)
	return !checkTime.Before(start) && checkTime.Before(end)
}

// NextReset returns the time of the next budget reset after the given time.
func (pm *PeriodManager) NextReset(t time.Time) time.Time {
	periodEnd := pm.PeriodEnd(t)
	return periodEnd
}

// TimeUntilReset returns the duration until the next budget reset.
func (pm *PeriodManager) TimeUntilReset(t time.Time) time.Duration {
	return pm.NextReset(t).Sub(t)
}

// ShouldResetAlert returns true if the next reset is within the alert window.
// This signals the cron to prepare for the upcoming reset.
func (pm *PeriodManager) ShouldResetAlert(t time.Time) bool {
	remaining := pm.TimeUntilReset(t)
	return remaining > 0 && remaining <= time.Duration(pm.config.AlertWindowMinutes)*time.Minute
}

// CanRollover returns false — spec §8.3: "Rollover: NOT supported in v1".
func (pm *PeriodManager) CanRollover() bool {
	return false
}

// ResetBudgets resets budget_used to 0 for all agents.
// Returns the updated agents (the caller is responsible for persistence).
func (pm *PeriodManager) ResetBudgets(agents []BudgetInfo) []BudgetInfo {
	result := make([]BudgetInfo, len(agents))
	for i, a := range agents {
		result[i] = a
		result[i].BudgetUsed = 0
	}
	return result
}

// ResetAgent resets a single agent's budget_used to 0.
func (pm *PeriodManager) ResetAgent(agent BudgetInfo) BudgetInfo {
	agent.BudgetUsed = 0
	return agent
}

// ResetAgentList resets multiple agents' budgets in batch.
// Convenience wrapper for ResetBudgets.
func (pm *PeriodManager) ResetAgentList(agents []BudgetInfo) []BudgetInfo {
	return pm.ResetBudgets(agents)
}

// PeriodInfo is a summary of the current budget period state.
type PeriodInfo struct {
	Start       time.Time     `json:"start"`
	End         time.Time     `json:"end"`
	Now         time.Time     `json:"now"`
	TimeLeft    time.Duration `json:"time_left"`
	DayOfWeek   string        `json:"day_of_week"`
	IsAlertMode bool          `json:"is_alert_mode"`
}

// PeriodInfo returns a summary of the current period for display/logging.
func (pm *PeriodManager) PeriodInfo(now time.Time) PeriodInfo {
	return PeriodInfo{
		Start:       pm.PeriodStart(now),
		End:         pm.PeriodEnd(now),
		Now:         now,
		TimeLeft:    pm.TimeUntilReset(now),
		DayOfWeek:   now.UTC().Weekday().String(),
		IsAlertMode: pm.ShouldResetAlert(now),
	}
}

// FormatPeriodInfo renders PeriodInfo as a human-readable string.
func FormatPeriodInfo(info PeriodInfo) string {
	return fmt.Sprintf(
		"Budget period: %s → %s UTC (day=%s, %s remaining%s)",
		info.Start.Format("2006-01-02 15:04"),
		info.End.Format("2006-01-02 15:04"),
		info.DayOfWeek,
		formatDuration(info.TimeLeft),
		alertSuffix(info.IsAlertMode),
	)
}

func alertSuffix(alert bool) string {
	if alert {
		return " [RESET IMMINENT]"
	}
	return ""
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		return "0s"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd%dh%dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
