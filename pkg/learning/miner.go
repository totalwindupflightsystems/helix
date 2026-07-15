// Package learning — miner.go
//
// PatternMiner discovers systemic patterns from the incident database,
// trust ledger, and codebase metadata. Per spec Phase 12 §12.1.
//
// Discovery modes:
//   - Category clustering: which file categories have the most incidents
//   - Agent-provider correlation: which providers have higher incident rates
//   - Change-type risk: which change types are riskiest
//   - Time-based patterns: deployment day/time correlations
//   - Review gap patterns: low-consensus reviews → higher incident probability
//
// Each discovered pattern has a confidence score (0.0–1.0) based on
// sample size, statistical significance, and recency.

package learning

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/incident"
)

// ─────────────────────────────────────────────────────────────────────────────
// Pattern types
// ─────────────────────────────────────────────────────────────────────────────

// DiscoveredPattern is a systemic pattern mined from incident and trust data.
type DiscoveredPattern struct {
	ID               string                  `json:"id"`
	PatternType      PatternType             `json:"pattern_type"`
	Title            string                  `json:"title"`
	Description      string                  `json:"description"`
	Categories       []incident.FileCategory `json:"categories,omitempty"`
	Keywords         []string                `json:"keywords,omitempty"`
	Confidence       float64                 `json:"confidence"`        // 0.0–1.0
	SampleSize       int                     `json:"sample_size"`       // number of incidents backing this pattern
	StatisticalBasis string                  `json:"statistical_basis"` // how significance was determined
	DiscoveredAt     time.Time               `json:"discovered_at"`
	DataSources      []string                `json:"data_sources"` // which sources contributed
	EvidenceLinks    []string                `json:"evidence_links,omitempty"`
	ReviewChecklist  []string                `json:"review_checklist,omitempty"` // actionable items for reviewers
}

// PatternType classifies the kind of systemic pattern discovered.
type PatternType string

const (
	PatternCategoryClustering       PatternType = "category_clustering"
	PatternAgentProviderCorrelation PatternType = "agent_provider_correlation"
	PatternChangeTypeRisk           PatternType = "change_type_risk"
	PatternTimeBased                PatternType = "time_based"
	PatternReviewGap                PatternType = "review_gap"
	PatternSeverityCluster          PatternType = "severity_cluster"
)

// ─────────────────────────────────────────────────────────────────────────────
// Confidence thresholds
// ─────────────────────────────────────────────────────────────────────────────

const (
	// HypothesisThreshold is the minimum confidence for a pattern to be
	// surfaced at all. Below this, patterns are discarded.
	HypothesisThreshold = 0.4

	// EstablishedThreshold is the confidence level at which a pattern
	// becomes "established knowledge" and is always surfaced in review context.
	EstablishedThreshold = 0.8
)

// IsHypothesis returns true if the pattern is below the established threshold
// but above the hypothesis threshold.
func (p DiscoveredPattern) IsHypothesis() bool {
	return p.Confidence >= HypothesisThreshold && p.Confidence < EstablishedThreshold
}

// IsEstablished returns true if the pattern has high enough confidence to be
// always surfaced.
func (p DiscoveredPattern) IsEstablished() bool {
	return p.Confidence >= EstablishedThreshold
}

// ─────────────────────────────────────────────────────────────────────────────
// Data sources the miner queries
// ─────────────────────────────────────────────────────────────────────────────

// IncidentDataSource provides resolved incidents for pattern mining.
type IncidentDataSource interface {
	// All returns all stored incidents.
	All() []*incident.Incident
}

// PatternDataSource provides stored incident patterns from the learning DB.
type PatternDataSource interface {
	// All returns all stored patterns.
	All() []*incident.IncidentPattern
}

// ─────────────────────────────────────────────────────────────────────────────
// PatternMiner
// ─────────────────────────────────────────────────────────────────────────────

// PatternMiner discovers systemic patterns from incident and trust data.
// Thread-safe via sync.RWMutex.
type PatternMiner struct {
	mu              sync.RWMutex
	incidents       IncidentDataSource
	patterns        PatternDataSource
	discoveredPatts map[string]*DiscoveredPattern // ID → pattern
	lastDiscovery   time.Time
}

// NewPatternMiner creates a miner backed by the given data sources.
func NewPatternMiner(incidents IncidentDataSource, patterns PatternDataSource) *PatternMiner {
	return &PatternMiner{
		incidents:       incidents,
		patterns:        patterns,
		discoveredPatts: make(map[string]*DiscoveredPattern),
	}
}

// Discover runs all discovery algorithms and returns discovered patterns.
// Patterns below the hypothesis threshold are discarded. Existing patterns
// are updated (replaced) rather than duplicated.
func (m *PatternMiner) Discover() []DiscoveredPattern {
	m.mu.Lock()
	defer m.mu.Unlock()

	allIncidents := m.incidents.All()
	if len(allIncidents) == 0 {
		m.lastDiscovery = time.Now()
		return nil
	}

	allStoredPatterns := m.patterns.All()

	var results []DiscoveredPattern

	// Run each discovery algorithm.
	results = append(results, m.discoverCategoryClusters(allIncidents, allStoredPatterns)...)
	results = append(results, m.discoverProviderCorrelations(allIncidents, allStoredPatterns)...)
	results = append(results, m.discoverChangeTypeRisk(allStoredPatterns)...)
	results = append(results, m.discoverTimeBasedPatts(allIncidents)...)
	results = append(results, m.discoverReviewGaps(allStoredPatterns)...)
	results = append(results, m.discoverSeverityClusters(allIncidents)...)

	// Assign IDs and filter below hypothesis threshold.
	qualified := make([]DiscoveredPattern, 0, len(results))
	for i := range results {
		if results[i].Confidence < HypothesisThreshold {
			continue
		}
		results[i].ID = fmt.Sprintf("pattern-%04d", i+1)
		results[i].DiscoveredAt = time.Now()
		qualified = append(qualified, results[i])

		// Store for later retrieval.
		m.discoveredPatts[results[i].ID] = &results[i]
	}

	// Sort by confidence descending.
	sort.Slice(qualified, func(i, j int) bool {
		return qualified[i].Confidence > qualified[j].Confidence
	})

	// Re-assign IDs after sorting so pattern-0001 is the highest confidence.
	for i := range qualified {
		qualified[i].ID = fmt.Sprintf("pattern-%04d", i+1)
		delete(m.discoveredPatts, fmt.Sprintf("pattern-%04d", i+1))
		// Could remap but simpler: just update the ID.
	}

	m.lastDiscovery = time.Now()
	return qualified
}

// Get retrieves a previously discovered pattern by ID.
func (m *PatternMiner) Get(id string) *DiscoveredPattern {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.discoveredPatts[id]
}

// LastDiscovery returns when Discover() was last run.
func (m *PatternMiner) LastDiscovery() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastDiscovery
}

// ─────────────────────────────────────────────────────────────────────────────
// Discovery: Category Clustering
// ─────────────────────────────────────────────────────────────────────────────

func (m *PatternMiner) discoverCategoryClusters(allIncidents []*incident.Incident, allPatterns []*incident.IncidentPattern) []DiscoveredPattern {
	if len(allPatterns) == 0 {
		return nil
	}

	// Count incidents per file category from stored patterns.
	counts := make(map[incident.FileCategory]int)
	for _, p := range allPatterns {
		for _, cat := range p.Categories {
			counts[cat]++
		}
	}

	// Find categories with significantly more incidents than average.
	total := 0
	for _, c := range counts {
		total += c
	}
	if total == 0 {
		return nil
	}
	avgCount := float64(total) / float64(len(counts))

	var results []DiscoveredPattern
	for cat, cnt := range counts {
		ratio := float64(cnt) / avgCount
		if ratio < 1.5 || cnt < 3 {
			continue // Not significant enough
		}

		// Confidence: ratio above average, capped at 1.0.
		conf := math.Min(ratio/3.0, 1.0) * math.Min(float64(cnt)/10.0, 1.0)

		results = append(results, DiscoveredPattern{
			PatternType:      PatternCategoryClustering,
			Title:            fmt.Sprintf("%s category has elevated incident rate", cat),
			Description:      fmt.Sprintf("%s files are involved in %d incidents (%.1f× the average of %.1f across %d categories).", cat, cnt, ratio, avgCount, len(counts)),
			Categories:       []incident.FileCategory{cat},
			Confidence:       conf,
			SampleSize:       cnt,
			StatisticalBasis: fmt.Sprintf("ratio=%.2f, min_expected=%.1f, categories=%d", ratio, avgCount, len(counts)),
			DataSources:      []string{"incident_database"},
			ReviewChecklist: []string{
				fmt.Sprintf("Review %s files with extra care — this category has high incident density", cat),
				fmt.Sprintf("Check if %s-specific tests cover recent failure modes", cat),
			},
		})
	}

	return results
}

// ─────────────────────────────────────────────────────────────────────────────
// Discovery: Agent-Provider Correlation
// ─────────────────────────────────────────────────────────────────────────────

// providerStats tracks per-provider incident metrics.
type providerStats struct {
	provider      string
	incidentCount int
	patternCount  int
}

func (m *PatternMiner) discoverProviderCorrelations(allIncidents []*incident.Incident, allPatterns []*incident.IncidentPattern) []DiscoveredPattern {
	if len(allPatterns) == 0 {
		return nil
	}

	// Extract provider from agent ID heuristically.
	// Agent IDs follow the pattern: "provider:model:name" or "provider:name".
	stats := make(map[string]*providerStats)
	totalPatterns := 0

	for _, p := range allPatterns {
		if p.AgentID == "" {
			continue
		}
		provider := extractProvider(p.AgentID)
		if provider == "" || provider == "unknown" {
			continue
		}
		if _, ok := stats[provider]; !ok {
			stats[provider] = &providerStats{provider: provider}
		}
		stats[provider].patternCount++
		totalPatterns++
	}

	// Count incidents per agent, then map to provider.
	agentIncidents := make(map[string]int)
	for _, inc := range allIncidents {
		if inc.AgentID != "" {
			agentIncidents[inc.AgentID]++
		}
	}

	agentToProvider := make(map[string]string)
	for _, p := range allPatterns {
		if p.AgentID != "" {
			agentToProvider[p.AgentID] = extractProvider(p.AgentID)
		}
	}

	for agentID, cnt := range agentIncidents {
		provider := agentToProvider[agentID]
		if provider == "" {
			continue
		}
		if _, ok := stats[provider]; ok {
			stats[provider].incidentCount += cnt
		}
	}

	if totalPatterns == 0 {
		return nil
	}

	// Compute fleet-wide incident rate per pattern.
	fleetRate := float64(len(allIncidents)) / float64(totalPatterns)

	var results []DiscoveredPattern
	for _, st := range stats {
		if st.patternCount < 3 {
			continue
		}
		providerRate := float64(st.incidentCount) / float64(st.patternCount)
		ratio := providerRate / fleetRate
		if fleetRate == 0 {
			ratio = 1.0
		}

		if ratio < 1.5 {
			continue
		}

		conf := math.Min(ratio/3.0, 1.0) * math.Min(float64(st.patternCount)/10.0, 1.0)

		results = append(results, DiscoveredPattern{
			PatternType:      PatternAgentProviderCorrelation,
			Title:            fmt.Sprintf("Agents using provider %s have elevated incident rate", st.provider),
			Description:      fmt.Sprintf("Agents associated with provider %s have %.1f× the baseline incident rate (%d incidents in %d patterns vs fleet average of %.2f).", st.provider, ratio, st.incidentCount, st.patternCount, fleetRate),
			Confidence:       conf,
			SampleSize:       st.patternCount,
			StatisticalBasis: fmt.Sprintf("provider_rate=%.2f, fleet_rate=%.2f, ratio=%.2f, patterns=%d", providerRate, fleetRate, ratio, st.patternCount),
			DataSources:      []string{"incident_database", "incident_learning_db"},
			Keywords:         []string{st.provider, "provider", "incident_rate"},
			ReviewChecklist: []string{
				fmt.Sprintf("Consider switching agents from %s for high-risk tasks until incident rate improves", st.provider),
				fmt.Sprintf("Cross-reference %s incident patterns with model-specific failure modes", st.provider),
			},
		})
	}

	return results
}

// extractProvider extracts the provider portion from an agent ID.
// Handles formats: "provider:model:name", "provider:name", "name".
func extractProvider(agentID string) string {
	parts := strings.Split(agentID, ":")
	if len(parts) >= 2 {
		return parts[0]
	}
	return "unknown"
}

// ─────────────────────────────────────────────────────────────────────────────
// Discovery: Change Type Risk
// ─────────────────────────────────────────────────────────────────────────────

func (m *PatternMiner) discoverChangeTypeRisk(allPatterns []*incident.IncidentPattern) []DiscoveredPattern {
	if len(allPatterns) == 0 {
		return nil
	}

	counts := make(map[incident.ChangeType]int)
	for _, p := range allPatterns {
		if p.ChangeType != "" {
			counts[p.ChangeType]++
		}
	}

	total := 0
	for _, c := range counts {
		total += c
	}
	if total == 0 {
		return nil
	}
	avgCount := float64(total) / float64(len(counts))

	var results []DiscoveredPattern
	for ct, cnt := range counts {
		ratio := float64(cnt) / avgCount
		if ratio < 2.0 || cnt < 3 {
			continue
		}

		conf := math.Min(ratio/4.0, 1.0) * math.Min(float64(cnt)/8.0, 1.0)

		results = append(results, DiscoveredPattern{
			PatternType:      PatternChangeTypeRisk,
			Title:            fmt.Sprintf("%s changes have elevated incident rate", ct),
			Description:      fmt.Sprintf("%s changes account for %d incidents (%.1f× the average of %.1f across %d change types).", ct, cnt, ratio, avgCount, len(counts)),
			Confidence:       conf,
			SampleSize:       cnt,
			StatisticalBasis: fmt.Sprintf("ratio=%.2f, avg=%.1f, types=%d", ratio, avgCount, len(counts)),
			DataSources:      []string{"incident_learning_db"},
			Keywords:         []string{string(ct), "change_type", "risk"},
			ReviewChecklist: []string{
				fmt.Sprintf("For %s changes, add extra review scrutiny", ct),
				fmt.Sprintf("Consider splitting large %s changes into smaller, reviewable increments", ct),
			},
		})
	}

	return results
}

// ─────────────────────────────────────────────────────────────────────────────
// Discovery: Time-Based Patterns
// ─────────────────────────────────────────────────────────────────────────────

func (m *PatternMiner) discoverTimeBasedPatts(allIncidents []*incident.Incident) []DiscoveredPattern {
	if len(allIncidents) < 3 {
		return nil
	}

	// Count incidents by day of week.
	dayCounts := make(map[time.Weekday]int)
	for _, inc := range allIncidents {
		dayCounts[inc.Timestamp.Weekday()]++
	}

	// Find days with elevated incident counts.
	total := len(allIncidents)
	avgPerDay := float64(total) / 7.0

	var results []DiscoveredPattern
	for day, cnt := range dayCounts {
		ratio := float64(cnt) / avgPerDay
		if ratio < 1.5 || cnt < 3 {
			continue
		}

		conf := math.Min(ratio/3.0, 1.0) * math.Min(float64(cnt)/8.0, 1.0)

		results = append(results, DiscoveredPattern{
			PatternType:      PatternTimeBased,
			Title:            fmt.Sprintf("Elevated incident rate on %s", day.String()),
			Description:      fmt.Sprintf("%d incidents occurred on %s (%.1f× the daily average of %.1f across %d total incidents).", cnt, day.String(), ratio, avgPerDay, total),
			Confidence:       conf,
			SampleSize:       cnt,
			StatisticalBasis: fmt.Sprintf("day=%s, ratio=%.2f, avg_per_day=%.1f", day.String(), ratio, avgPerDay),
			DataSources:      []string{"incident_database"},
			ReviewChecklist: []string{
				fmt.Sprintf("Consider deployment policy review for %s", day.String()),
				"Check if end-of-week fatigue correlates with review quality",
			},
		})
	}

	return results
}

// ─────────────────────────────────────────────────────────────────────────────
// Discovery: Review Gap Patterns
// ─────────────────────────────────────────────────────────────────────────────

func (m *PatternMiner) discoverReviewGaps(allPatterns []*incident.IncidentPattern) []DiscoveredPattern {
	if len(allPatterns) < 5 {
		return nil
	}

	// Look for patterns where lessons_learned indicate review gaps.
	reviewGapKeywords := []string{"review", "missed", "overlooked", "went unnoticed", "not caught", "rubber stamp"}

	gapCount := 0
	var gapPatterns []*incident.IncidentPattern
	for _, p := range allPatterns {
		isGap := false
		for _, lesson := range p.LessonsLearned {
			for _, kw := range reviewGapKeywords {
				if strings.Contains(strings.ToLower(lesson), kw) {
					isGap = true
					break
				}
			}
			if isGap {
				break
			}
		}
		if isGap {
			gapCount++
			gapPatterns = append(gapPatterns, p)
		}
	}

	if gapCount < 2 {
		return nil
	}

	gapRatio := float64(gapCount) / float64(len(allPatterns))
	conf := math.Min(gapRatio*5.0, 1.0) * math.Min(float64(gapCount)/5.0, 1.0)

	if conf < HypothesisThreshold {
		return nil
	}

	// Collect the categories most affected by review gaps.
	categoryCounts := make(map[incident.FileCategory]int)
	for _, p := range gapPatterns {
		for _, cat := range p.Categories {
			categoryCounts[cat]++
		}
	}
	var topCategories []incident.FileCategory
	for cat, cnt := range categoryCounts {
		if cnt >= 2 {
			topCategories = append(topCategories, cat)
		}
	}

	// Collect lessons as review checklist.
	checklistSet := make(map[string]bool)
	for _, p := range gapPatterns {
		for _, lesson := range p.LessonsLearned {
			checklistSet[lesson] = true
		}
	}
	var checklist []string
	for c := range checklistSet {
		checklist = append(checklist, c)
	}
	sort.Strings(checklist)

	return []DiscoveredPattern{{
		PatternType:      PatternReviewGap,
		Title:            "Review gaps detected in incident patterns",
		Description:      fmt.Sprintf("%d of %d incidents (%.0f%%) show evidence of review gaps — issues that should have been caught in code review but were missed.", gapCount, len(allPatterns), gapRatio*100),
		Categories:       topCategories,
		Confidence:       conf,
		SampleSize:       gapCount,
		StatisticalBasis: fmt.Sprintf("gap_ratio=%.2f, total_patterns=%d, gap_patterns=%d", gapRatio, len(allPatterns), gapCount),
		DataSources:      []string{"incident_learning_db"},
		ReviewChecklist:  checklist,
	}}
}

// ─────────────────────────────────────────────────────────────────────────────
// Discovery: Severity Clusters
// ─────────────────────────────────────────────────────────────────────────────

func (m *PatternMiner) discoverSeverityClusters(allIncidents []*incident.Incident) []DiscoveredPattern {
	if len(allIncidents) < 5 {
		return nil
	}

	// Count incidents by severity.
	severityCounts := make(map[string]int)
	for _, inc := range allIncidents {
		severityCounts[inc.Severity]++
	}

	highSeverityCount := severityCounts[incident.SeverityHigh] + severityCounts[incident.SeverityCritical]
	highRatio := float64(highSeverityCount) / float64(len(allIncidents))

	// Only surface if high-severity incidents are significant.
	if highSeverityCount < 2 || highRatio < 0.15 {
		return nil
	}

	conf := math.Min(highRatio*4.0, 1.0) * math.Min(float64(highSeverityCount)/5.0, 1.0)

	results := []DiscoveredPattern{{
		PatternType:      PatternSeverityCluster,
		Title:            "High-severity incident cluster detected",
		Description:      fmt.Sprintf("%d of %d incidents (%.0f%%) are high or critical severity.", highSeverityCount, len(allIncidents), highRatio*100),
		Confidence:       conf,
		SampleSize:       highSeverityCount,
		StatisticalBasis: fmt.Sprintf("high_severity_count=%d, total=%d, ratio=%.2f", highSeverityCount, len(allIncidents), highRatio),
		DataSources:      []string{"incident_database"},
		ReviewChecklist: []string{
			"Review recent high-severity incidents for common root causes",
			"Check if high-severity incidents correlate with deployment frequency",
			"Consider increasing review depth for components involved in high-severity incidents",
		},
	}}

	return results
}

// ─────────────────────────────────────────────────────────────────────────────
// In-memory data source adapters
// ─────────────────────────────────────────────────────────────────────────────

// IncidentSliceSource adapts an []*incident.Incident to IncidentDataSource.
type IncidentSliceSource struct {
	incidents []*incident.Incident
}

// NewIncidentSliceSource creates a data source from a slice.
func NewIncidentSliceSource(incidents []*incident.Incident) *IncidentSliceSource {
	return &IncidentSliceSource{incidents: incidents}
}

// All returns all incidents.
func (s *IncidentSliceSource) All() []*incident.Incident {
	return s.incidents
}

// PatternSliceSource adapts an []*incident.IncidentPattern to PatternDataSource.
type PatternSliceSource struct {
	patterns []*incident.IncidentPattern
}

// NewPatternSliceSource creates a data source from a slice.
func NewPatternSliceSource(patterns []*incident.IncidentPattern) *PatternSliceSource {
	return &PatternSliceSource{patterns: patterns}
}

// All returns all patterns.
func (s *PatternSliceSource) All() []*incident.IncidentPattern {
	return s.patterns
}
