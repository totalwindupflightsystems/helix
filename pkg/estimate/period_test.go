package estimate

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Tests for PeriodManager — spec §8.3 (Budget Period Management)
//
// Day-of-week reference for test dates:
//   2026-06-28 = Sunday  (period start)
//   2026-06-29 = Monday
//   2026-07-01 = Wednesday
//   2026-07-04 = Saturday (last day of period)
//   2026-07-05 = Sunday (next period start)
// ---------------------------------------------------------------------------

func TestNewPeriodManager_DefaultConfig(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())
	if pm.config.ResetHourUTC != 0 {
		t.Errorf("ResetHourUTC = %d, want 0", pm.config.ResetHourUTC)
	}
	if pm.config.AlertWindowMinutes != 60 {
		t.Errorf("AlertWindowMinutes = %d, want 60", pm.config.AlertWindowMinutes)
	}
}

func TestNewPeriodManager_InvalidConfig(t *testing.T) {
	pm := NewPeriodManager(PeriodConfig{ResetHourUTC: 25})
	if pm.config.ResetHourUTC != 0 {
		t.Errorf("invalid config should default to 0, got %d", pm.config.ResetHourUTC)
	}
}

func TestPeriodStart_SundayMidnight(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())

	sunday := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC) // Sunday
	start := pm.PeriodStart(sunday)

	if !start.Equal(sunday) {
		t.Errorf("PeriodStart(Sunday 00:00) = %v, want %v", start, sunday)
	}
}

func TestPeriodStart_MidWeek(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())

	wednesday := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC) // Wednesday
	start := pm.PeriodStart(wednesday)

	// Should be the most recent Sunday (June 28).
	expected := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)
	if !start.Equal(expected) {
		t.Errorf("PeriodStart(Wed) = %v, want %v", start, expected)
	}
}

func TestPeriodStart_Saturday(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())

	saturday := time.Date(2026, 7, 4, 23, 0, 0, 0, time.UTC) // Saturday
	start := pm.PeriodStart(saturday)

	expected := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)
	if !start.Equal(expected) {
		t.Errorf("PeriodStart(Sat) = %v, want %v", start, expected)
	}
}

func TestPeriodEnd_OneWeekLater(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())

	wednesday := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC) // Wednesday
	end := pm.PeriodEnd(wednesday)

	expected := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC) // Next Sunday
	if !end.Equal(expected) {
		t.Errorf("PeriodEnd = %v, want %v", end, expected)
	}
}

func TestIsInPeriod_CurrentTime(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC) // Wednesday

	if !pm.IsInPeriod(now, now) {
		t.Errorf("time should be in its own period")
	}

	lastWeek := now.AddDate(0, 0, -8)
	if pm.IsInPeriod(lastWeek, now) {
		t.Errorf("last week time should not be in current period")
	}

	nextWeek := now.AddDate(0, 0, 8)
	if pm.IsInPeriod(nextWeek, now) {
		t.Errorf("next week time should not be in current period")
	}
}

func TestNextReset(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())
	wednesday := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	reset := pm.NextReset(wednesday)

	expected := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	if !reset.Equal(expected) {
		t.Errorf("NextReset = %v, want %v", reset, expected)
	}
}

func TestTimeUntilReset(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())

	// Saturday 23:00 UTC → 1 hour until reset.
	saturday := time.Date(2026, 7, 4, 23, 0, 0, 0, time.UTC)
	d := pm.TimeUntilReset(saturday)

	if d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("TimeUntilReset(Sat 23:00) = %v, want ~1h", d)
	}
}

func TestShouldResetAlert_OutsideWindow(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())

	wednesday := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	if pm.ShouldResetAlert(wednesday) {
		t.Errorf("Wednesday should not trigger reset alert")
	}
}

func TestShouldResetAlert_WithinWindow(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())

	// Saturday 23:30 UTC → 30 min until reset (within 60 min window).
	saturday := time.Date(2026, 7, 4, 23, 30, 0, 0, time.UTC)
	if !pm.ShouldResetAlert(saturday) {
		t.Errorf("Saturday 23:30 should trigger reset alert (within 1h window)")
	}
}

func TestShouldResetAlert_ExactlyAtWindowEdge(t *testing.T) {
	pm := NewPeriodManager(PeriodConfig{AlertWindowMinutes: 60})

	// 60 minutes before reset → should be in window.
	saturday := time.Date(2026, 7, 4, 23, 0, 0, 0, time.UTC)
	if !pm.ShouldResetAlert(saturday) {
		t.Errorf("60 min before reset should trigger alert (edge case)")
	}
}

func TestCanRollover_AlwaysFalse(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())
	if pm.CanRollover() {
		t.Errorf("CanRollover should always be false in v1 per spec §8.3")
	}
}

func TestResetBudgets_AllAgentsReset(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())
	agents := []BudgetInfo{
		{AgentName: "a1", BudgetWeekly: 100, BudgetUsed: 50},
		{AgentName: "a2", BudgetWeekly: 200, BudgetUsed: 150},
		{AgentName: "a3", BudgetWeekly: 50, BudgetUsed: 50},
	}

	result := pm.ResetBudgets(agents)

	for i, a := range result {
		if a.BudgetUsed != 0 {
			t.Errorf("agent %s BudgetUsed = %.2f, want 0 after reset", a.AgentName, a.BudgetUsed)
		}
		if a.BudgetWeekly != agents[i].BudgetWeekly {
			t.Errorf("agent %s BudgetWeekly changed from %.2f to %.2f",
				a.AgentName, agents[i].BudgetWeekly, a.BudgetWeekly)
		}
	}
}

func TestResetBudgets_EmptyList(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())
	result := pm.ResetBudgets([]BudgetInfo{})
	if len(result) != 0 {
		t.Errorf("empty list should return empty, got %d", len(result))
	}
}

func TestResetAgent_SingleAgent(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())
	agent := BudgetInfo{AgentName: "a1", BudgetWeekly: 100, BudgetUsed: 75}

	result := pm.ResetAgent(agent)

	if result.BudgetUsed != 0 {
		t.Errorf("BudgetUsed = %.2f, want 0", result.BudgetUsed)
	}
	if result.BudgetWeekly != 100 {
		t.Errorf("BudgetWeekly = %.2f, want 100", result.BudgetWeekly)
	}
}

func TestResetAgentList_BatchReset(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())
	agents := []BudgetInfo{
		{AgentName: "a1", BudgetWeekly: 100, BudgetUsed: 50},
		{AgentName: "a2", BudgetWeekly: 200, BudgetUsed: 150},
	}

	result := pm.ResetAgentList(agents)

	if len(result) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(result))
	}
	for _, a := range result {
		if a.BudgetUsed != 0 {
			t.Errorf("agent %s not reset", a.AgentName)
		}
	}
}

func TestResetBudgets_DoesNotMutateOriginal(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())
	original := []BudgetInfo{
		{AgentName: "a1", BudgetWeekly: 100, BudgetUsed: 50},
	}

	_ = pm.ResetBudgets(original)

	if original[0].BudgetUsed != 50 {
		t.Errorf("original slice should not be mutated, BudgetUsed = %.2f", original[0].BudgetUsed)
	}
}

func TestPeriodInfo(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())
	wednesday := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	info := pm.PeriodInfo(wednesday)

	if info.DayOfWeek != "Wednesday" {
		t.Errorf("DayOfWeek = %q, want Wednesday", info.DayOfWeek)
	}
	if info.IsAlertMode {
		t.Errorf("Wednesday should not be in alert mode")
	}
	if info.Start.IsZero() {
		t.Errorf("Start should not be zero")
	}
	if info.End.IsZero() {
		t.Errorf("End should not be zero")
	}
}

func TestPeriodInfo_AlertMode(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())
	saturday := time.Date(2026, 7, 4, 23, 30, 0, 0, time.UTC)

	info := pm.PeriodInfo(saturday)

	if !info.IsAlertMode {
		t.Errorf("Saturday 23:30 should be in alert mode")
	}
}

func TestFormatPeriodInfo(t *testing.T) {
	info := PeriodInfo{
		Start:       time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC),
		End:         time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
		Now:         time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		TimeLeft:    4 * 24 * time.Hour,
		DayOfWeek:   "Wednesday",
		IsAlertMode: false,
	}

	out := FormatPeriodInfo(info)
	if out == "" {
		t.Errorf("FormatPeriodInfo should not be empty")
	}
}

func TestFormatPeriodInfo_AlertMode(t *testing.T) {
	info := PeriodInfo{
		Start:       time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC),
		End:         time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
		Now:         time.Date(2026, 7, 4, 23, 30, 0, 0, time.UTC),
		TimeLeft:    30 * time.Minute,
		DayOfWeek:   "Saturday",
		IsAlertMode: true,
	}

	out := FormatPeriodInfo(info)
	if !containsString(out, "RESET IMMINENT") {
		t.Errorf("alert mode should contain [RESET IMMINENT]: %s", out)
	}
}

func TestPeriodStart_CustomResetHour(t *testing.T) {
	pm := NewPeriodManager(PeriodConfig{ResetHourUTC: 6, AlertWindowMinutes: 30})

	// Sunday at 05:00 → before reset at 06:00 → still in previous period.
	// Previous Sunday is June 21 at 06:00.
	sundayEarly := time.Date(2026, 6, 28, 5, 0, 0, 0, time.UTC) // Sunday
	start := pm.PeriodStart(sundayEarly)

	expected := time.Date(2026, 6, 21, 6, 0, 0, 0, time.UTC)
	if !start.Equal(expected) {
		t.Errorf("PeriodStart(Sunday 05:00 with reset at 06:00) = %v, want %v", start, expected)
	}
}

func TestPeriodStart_NonUTCTime(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())

	// 12:00 EST = 16:00 UTC on same day (July 1).
	est, _ := time.LoadLocation("America/New_York")
	t1 := time.Date(2026, 7, 1, 12, 0, 0, 0, est)

	start := pm.PeriodStart(t1)
	expected := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC) // Previous Sunday
	if !start.Equal(expected) {
		t.Errorf("PeriodStart(EST time) = %v, want %v", start, expected)
	}
}

func TestPeriodBoundaries_SundayReset(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())

	// Sunday July 5 at 00:00:01 → in the NEW period.
	justAfterReset := time.Date(2026, 7, 5, 0, 0, 1, 0, time.UTC)
	start := pm.PeriodStart(justAfterReset)
	expected := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	if !start.Equal(expected) {
		t.Errorf("Sunday 00:00:01 should start new period: got %v, want %v", start, expected)
	}
}

func TestPeriodBoundaries_SaturdayLastSecond(t *testing.T) {
	pm := NewPeriodManager(DefaultPeriodConfig())

	// Saturday at 23:59:59 → still in the current period.
	lastSecond := time.Date(2026, 7, 4, 23, 59, 59, 0, time.UTC)
	start := pm.PeriodStart(lastSecond)
	expected := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)
	if !start.Equal(expected) {
		t.Errorf("Saturday 23:59:59 should be in current period: got %v, want %v", start, expected)
	}
}

// helper
func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
