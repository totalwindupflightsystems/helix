package marketplace

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Spec §17 Example Outputs — CLI display formatters
// ---------------------------------------------------------------------------

// FormatAgentTable renders a list of agents as a CLI table (spec §17 List Agents).
// Columns: NAME, TIER, TRUST, RATING, TASKS, COST/AVG, CAPABILITIES.
// Agents are sorted by trust score descending.
func FormatAgentTable(agents []*Agent) string {
	if len(agents) == 0 {
		return "No agents found."
	}

	// Sort by trust score descending
	sorted := make([]*Agent, len(agents))
	copy(sorted, agents)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].TrustScore > sorted[j].TrustScore
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-12s %-6s %-6s %-8s %-7s %-10s %s\n",
		"NAME", "TIER", "TRUST", "RATING", "TASKS", "COST/AVG", "CAPABILITIES"))
	sb.WriteString(fmt.Sprintf("%-12s %-6s %-6s %-8s %-7s %-10s %s\n",
		"----", "----", "-----", "------", "-----", "--------", "------------"))

	for _, a := range sorted {
		rating := formatStarRatingShort(a.Ratings.Average)
		cost := formatCostPerTask(a.Budget.AverageTaskCost)
		caps := capabilitiesString(a.Capabilities)
		sb.WriteString(fmt.Sprintf("%-12s %-6s %-6d %-8s %-7d %-10s %s\n",
			a.Name, a.Tier, a.TrustScore, rating, a.Performance.TasksCompleted, cost, caps))
	}

	sb.WriteString(fmt.Sprintf("\n%d agents listed.\n", len(sorted)))
	return sb.String()
}

// FormatAgentDetail renders the detailed agent view (spec §17 Search by Capability + Agent Deprecation Warning).
// Shows capabilities, cost profile, performance metrics, and ratings.
func FormatAgentDetail(a *Agent) string {
	var sb strings.Builder

	// Header with trust warning for low-trust agents
	if a.TrustScore < 30 {
		sb.WriteString(fmt.Sprintf("AGENT: %s (%s, trust=%d ⚠️)\n", a.Name, a.Tier, a.TrustScore))
		sb.WriteString("WARNING: Trust score has been below 30.\n")
		sb.WriteString("         Auto-deprecation may apply if trust does not improve.\n\n")
	} else {
		sb.WriteString(fmt.Sprintf("AGENT: %s (%s, trust=%d)\n", a.Name, a.Tier, a.TrustScore))
	}

	sb.WriteString(fmt.Sprintf("  Capabilities: %s\n", capabilitiesString(a.Capabilities)))
	sb.WriteString(fmt.Sprintf("  Cost profile: %s ($%.2f/task avg)\n",
		a.Budget.CostProfile, a.Budget.AverageTaskCost))
	sb.WriteString(fmt.Sprintf("  Tasks completed: %d | Acceptance rate: %.0f%%\n",
		a.Performance.TasksCompleted, a.Performance.PrAcceptanceRate*100))
	sb.WriteString(fmt.Sprintf("  Rating: %s (%.1f/5.0, %d reviews)\n",
		formatStarRatingFloat(a.Ratings.Average), a.Ratings.Average, a.Ratings.Count))

	// Recent reviews (up to 3)
	if len(a.Reviews) > 0 {
		sb.WriteString("\nRECENT REVIEWS:\n")
		recent := a.Reviews
		if len(recent) > 3 {
			recent = recent[:3]
		}
		for _, r := range recent {
			comment := r.Comment
			if comment == "" {
				comment = "(no comment)"
			}
			sb.WriteString(fmt.Sprintf("  %s %s (%s): %q\n",
				formatStarRating(r.Rating), r.Author, r.Date, comment))
		}
	}

	// Deprecation status
	if a.Status == StatusDeprecated {
		sb.WriteString("\nDEPRECATED")
		if a.DeprecatedAt != "" {
			sb.WriteString(fmt.Sprintf(" since %s", a.DeprecatedAt))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// FormatRatingSubmission renders the rating confirmation output (spec §17 Rate an Agent).
func FormatRatingSubmission(agentName string, rating int, author string, comment string, newAverage float64, newCount int, isHuman bool) string {
	var sb strings.Builder
	sb.WriteString("RATING SUBMITTED:\n")
	sb.WriteString(fmt.Sprintf("  Agent:  %s\n", agentName))
	sb.WriteString(fmt.Sprintf("  Rating: %s (%d/5)\n", formatStarRating(rating), rating))

	humanMark := ""
	if isHuman {
		humanMark = " ✅"
	}
	sb.WriteString(fmt.Sprintf("  Author: %s (human)%s\n", author, humanMark))

	if comment != "" {
		sb.WriteString(fmt.Sprintf("  Comment: %q\n", comment))
	}

	sb.WriteString(fmt.Sprintf("\nAgent %s new average: %.1f (%d reviews)\n", agentName, newAverage, newCount))
	return sb.String()
}

// FormatDeprecationNotice renders a deprecation progress warning (spec §17 Agent Deprecation Warning).
func FormatDeprecationNotice(agentName string, trustScore int, daysBelowThreshold int, daysUntilDeprecation int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("WARNING: Trust score has been below 30 for %d days.\n", daysBelowThreshold))
	sb.WriteString(fmt.Sprintf("         Auto-deprecation in %d days if trust does not improve.\n", daysUntilDeprecation))
	return sb.String()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// formatStarRating renders a 1-5 integer rating as filled/half/empty stars.
// 5 → ★★★★★, 4 → ★★★★☆, 3 → ★★★☆☆
func formatStarRating(rating int) string {
	if rating < 0 {
		rating = 0
	}
	if rating > 5 {
		rating = 5
	}
	stars := strings.Repeat("★", rating) + strings.Repeat("☆", 5-rating)
	return stars
}

// formatStarRatingFloat renders a float rating (0.0–5.0) with half-star support.
// 4.7 → ★★★★½, 4.0 → ★★★★☆, 5.0 → ★★★★★
func formatStarRatingFloat(rating float64) string {
	if rating < 0 {
		rating = 0
	}
	if rating > 5 {
		rating = 5
	}
	full := int(rating)
	half := rating-float64(full) >= 0.5
	empty := 5 - full
	if half {
		empty--
	}
	stars := strings.Repeat("★", full)
	if half {
		stars += "½"
	}
	stars += strings.Repeat("☆", empty)
	return stars
}

// formatStarRatingShort renders a compact rating string for table display.
// 4.7 → ★4.7, 0.0 → ★0.0
func formatStarRatingShort(rating float64) string {
	return fmt.Sprintf("★%.1f", rating)
}

// formatCostPerTask renders the average cost per task for table display.
// 0.12 → $0.12, 0.038 → $0.04
func formatCostPerTask(cost float64) string {
	return fmt.Sprintf("$%.2f", cost)
}

// FormatAgentsByStatus renders agents grouped by status (active, deprecated, retired).
func FormatAgentsByStatus(agents []*Agent) string {
	groups := map[AgentStatus][]*Agent{}
	for _, a := range agents {
		groups[a.Status] = append(groups[a.Status], a)
	}

	statuses := []AgentStatus{StatusActive, StatusDeprecated, StatusRetired}
	var sb strings.Builder

	for _, status := range statuses {
		group := groups[status]
		if len(group) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n=== %s (%d) ===\n", strings.ToUpper(string(status)), len(group)))
		for _, a := range group {
			sb.WriteString(fmt.Sprintf("  %-12s trust=%-3d tier=%-5s tasks=%-4d\n",
				a.Name, a.TrustScore, a.Tier, a.Performance.TasksCompleted))
		}
	}
	return sb.String()
}

// FormatTrustDistribution renders a histogram-like view of agent trust scores.
// Bins: 0-19 (critical), 20-39 (low), 40-59 (medium), 60-79 (good), 80-100 (excellent).
func FormatTrustDistribution(agents []*Agent) string {
	bins := [5]int{}
	binLabels := []string{"0-19  ", "20-39 ", "40-59 ", "60-79 ", "80-100"}

	for _, a := range agents {
		switch {
		case a.TrustScore < 20:
			bins[0]++
		case a.TrustScore < 40:
			bins[1]++
		case a.TrustScore < 60:
			bins[2]++
		case a.TrustScore < 80:
			bins[3]++
		default:
			bins[4]++
		}
	}

	maxCount := 0
	for _, c := range bins {
		if c > maxCount {
			maxCount = c
		}
	}

	var sb strings.Builder
	sb.WriteString("TRUST DISTRIBUTION:\n")
	for i, count := range bins {
		barLen := 0
		if maxCount > 0 {
			barLen = count * 20 / maxCount
		}
		bar := strings.Repeat("█", barLen)
		sb.WriteString(fmt.Sprintf("  %s │%s %d\n", binLabels[i], bar, count))
	}
	return sb.String()
}

// FormatSearchResults renders search results (spec §17 Search by Capability).
func FormatSearchResults(results []*Agent, query SearchQuery) string {
	if len(results) == 0 {
		return "No agents found matching criteria.\n"
	}

	var sb strings.Builder
	for _, a := range results {
		sb.WriteString(FormatAgentDetail(a))
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("%d agent(s) found.\n", len(results)))
	return sb.String()
}

// FormatRegistrySummary renders a high-level summary of the marketplace registry.
func FormatRegistrySummary(agents []*Agent) string {
	if len(agents) == 0 {
		return "MARKETPLACE: empty (0 agents registered)\n"
	}

	total := len(agents)
	active := 0
	deprecated := 0
	retired := 0
	totalTasks := 0
	trustSum := 0
	ratingSum := 0.0
	ratingCount := 0

	for _, a := range agents {
		switch a.Status {
		case StatusActive:
			active++
		case StatusDeprecated:
			deprecated++
		case StatusRetired:
			retired++
		}
		totalTasks += a.Performance.TasksCompleted
		trustSum += a.TrustScore
		if a.Ratings.Count > 0 {
			ratingSum += a.Ratings.Average * float64(a.Ratings.Count)
			ratingCount += a.Ratings.Count
		}
	}

	avgTrust := 0
	if total > 0 {
		avgTrust = trustSum / total
	}
	avgRating := 0.0
	if ratingCount > 0 {
		avgRating = ratingSum / float64(ratingCount)
	}

	var sb strings.Builder
	sb.WriteString("MARKETPLACE SUMMARY:\n")
	sb.WriteString(fmt.Sprintf("  Total agents: %d (active=%d, deprecated=%d, retired=%d)\n",
		total, active, deprecated, retired))
	sb.WriteString(fmt.Sprintf("  Average trust: %d\n", avgTrust))
	sb.WriteString(fmt.Sprintf("  Average rating: %.1f (%d reviews)\n", avgRating, ratingCount))
	sb.WriteString(fmt.Sprintf("  Total tasks completed: %d\n", totalTasks))
	sb.WriteString(fmt.Sprintf("  Generated: %s\n", time.Now().UTC().Format("2006-01-02 15:04:05 UTC")))
	return sb.String()
}
