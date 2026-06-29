package incident

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// Incident Learning Database
//
// Per spec (production-verification.md §Integration Points):
//   "All incidents → learning database → future review training"
//
// The LearningDatabase stores incident patterns and maps them to review
// criteria. When a similar code change is detected (by file category, change
// type), the system surfaces relevant past incidents as review context.
//
// Pattern similarity scoring uses keyword overlap + severity match to rank
// which past incidents are most relevant to a new PR.
// =============================================================================

// FileCategory classifies the type of code being changed. Maps to review
// criteria so that incidents from similar categories are surfaced first.
type FileCategory string

const (
	CategoryAuth       FileCategory = "auth"
	CategoryCrypto     FileCategory = "crypto"
	CategoryDatabase   FileCategory = "database"
	CategoryAPI        FileCategory = "api"
	CategoryInfra      FileCategory = "infra"
	CategoryConfig     FileCategory = "config"
	CategoryTest       FileCategory = "test"
	CategoryDoc        FileCategory = "doc"
	CategoryIaC        FileCategory = "iac"
	CategoryCI         FileCategory = "ci"
	CategoryNetworking FileCategory = "networking"
	CategoryOther      FileCategory = "other"
)

// ChangeType describes what kind of change triggered the review.
type ChangeType string

const (
	ChangeNew       ChangeType = "new"       // new file/feature
	ChangeModify    ChangeType = "modify"    // modifying existing code
	ChangeDelete    ChangeType = "delete"    // removing code
	ChangeRefactor  ChangeType = "refactor"  // restructuring
	ChangeMigration ChangeType = "migration" // framework/version migration
)

// IncidentPattern is the stored representation of an incident in the learning
// database. It captures the patterns that make the incident relevant for
// future reviews.
type IncidentPattern struct {
	ID            string        `json:"id"`
	AgentID       string        `json:"agent_id"`
	Categories    []FileCategory `json:"categories"`
	ChangeType    ChangeType    `json:"change_type"`
	Severity      string        `json:"severity"`
	Keywords      []string      `json:"keywords"`       // extracted from description + evidence
	Description   string        `json:"description"`
	RootCause     string        `json:"root_cause"`
	LessonsLearned []string     `json:"lessons_learned"` // actionable review criteria derived from incident
	Evidence      []string      `json:"evidence"`
	Timestamp     time.Time     `json:"timestamp"`
}

// PRContext describes a new PR being reviewed, used to find relevant incidents.
type PRContext struct {
	Categories []FileCategory `json:"categories"`
	ChangeType ChangeType     `json:"change_type"`
	Keywords   []string       `json:"keywords"`
	Files      []string       `json:"files"`
}

// ReviewContextItem is one past incident surfaced as relevant review context.
type ReviewContextItem struct {
	Pattern      IncidentPattern `json:"pattern"`
	Similarity   float64         `json:"similarity"`   // 0.0–1.0
	MatchReasons []string        `json:"match_reasons"` // why this was surfaced
}

// ReviewContextReport is the output of FeedReviewContext — a ranked list of
// past incidents relevant to a new PR, with actionable review criteria.
type ReviewContextReport struct {
	Items            []ReviewContextItem `json:"items"`
	ReviewCriteria   []string            `json:"review_criteria"`    // accumulated lessons learned
	MaxSimilarity    float64             `json:"max_similarity"`
	TotalIncidents   int                 `json:"total_incidents_searched"`
}

// LearningDatabase stores incident patterns and provides similarity-based
// retrieval for the review context feed.
type LearningDatabase struct {
	mu       sync.RWMutex
	patterns map[string]*IncidentPattern // ID → pattern
}

// NewLearningDatabase creates an empty learning database.
func NewLearningDatabase() *LearningDatabase {
	return &LearningDatabase{
		patterns: make(map[string]*IncidentPattern),
	}
}

// Store adds an incident pattern to the learning database.
func (db *LearningDatabase) Store(pattern *IncidentPattern) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.patterns[pattern.ID] = pattern
}

// StoreFromIncident creates and stores an IncidentPattern from an Incident
// record and additional metadata.
func (db *LearningDatabase) StoreFromIncident(inc *Incident, categories []FileCategory, changeType ChangeType, keywords []string, rootCause string, lessonsLearned []string) {
	pattern := &IncidentPattern{
		ID:             inc.ID,
		AgentID:        inc.AgentID,
		Categories:     categories,
		ChangeType:     changeType,
		Severity:       inc.Severity,
		Keywords:       mergeKeywords(keywords, inc.Description, inc.Evidence),
		Description:    inc.Description,
		RootCause:      rootCause,
		LessonsLearned: lessonsLearned,
		Evidence:       inc.Evidence,
		Timestamp:      inc.Timestamp,
	}
	db.Store(pattern)
}

// Get retrieves a pattern by ID.
func (db *LearningDatabase) Get(id string) *IncidentPattern {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.patterns[id]
}

// Count returns the total number of stored patterns.
func (db *LearningDatabase) Count() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.patterns)
}

// All returns all patterns (for testing/debugging).
func (db *LearningDatabase) All() []*IncidentPattern {
	db.mu.RLock()
	defer db.mu.RUnlock()
	result := make([]*IncidentPattern, 0, len(db.patterns))
	for _, p := range db.patterns {
		result = append(result, p)
	}
	return result
}

// FeedReviewContext finds past incidents relevant to a new PR and returns them
// ranked by similarity. This is the core of the learning feedback loop — past
// mistakes inform future reviews.
func (db *LearningDatabase) FeedReviewContext(ctx PRContext) *ReviewContextReport {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var items []ReviewContextItem

	for _, pattern := range db.patterns {
		similarity, reasons := scoreSimilarity(pattern, ctx)
		if similarity > 0 {
			items = append(items, ReviewContextItem{
				Pattern:      *pattern,
				Similarity:   similarity,
				MatchReasons: reasons,
			})
		}
	}

	// Sort by similarity descending
	sort.Slice(items, func(i, j int) bool {
		return items[i].Similarity > items[j].Similarity
	})

	maxSim := 0.0
	if len(items) > 0 {
		maxSim = items[0].Similarity
	}

	// Collect unique review criteria from matched incidents
	criteriaSet := make(map[string]bool)
	for _, item := range items {
		for _, lesson := range item.Pattern.LessonsLearned {
			criteriaSet[lesson] = true
		}
	}
	var criteria []string
	for c := range criteriaSet {
		criteria = append(criteria, c)
	}
	sort.Strings(criteria)

	return &ReviewContextReport{
		Items:          items,
		ReviewCriteria: criteria,
		MaxSimilarity:  maxSim,
		TotalIncidents: len(db.patterns),
	}
}

// scoreSimilarity computes a similarity score (0.0–1.0) between a stored
// incident pattern and a PR context. The score is based on:
//   - Category overlap (40%): shared file categories
//   - Keyword overlap (40%): shared keywords
//   - Change type match (10%): same change type
//   - Severity match (10%): same severity level (only when severity is relevant)
func scoreSimilarity(pattern *IncidentPattern, ctx PRContext) (float64, []string) {
	var reasons []string

	// Category overlap (40%)
	catScore := categoryOverlap(pattern.Categories, ctx.Categories)
	if catScore > 0 {
		reasons = append(reasons, "matching file categories")
	}

	// Keyword overlap (40%)
	kwScore := keywordOverlap(pattern.Keywords, ctx.Keywords)
	if kwScore > 0 {
		reasons = append(reasons, "shared keywords")
	}

	// Change type match (10%)
	ctScore := 0.0
	if pattern.ChangeType == ctx.ChangeType && ctx.ChangeType != "" {
		ctScore = 1.0
		reasons = append(reasons, "same change type")
	}

	// Severity is not in PRContext, so severity match only applies when
	// the pattern has high severity — surface severe incidents more readily.
	// This is a small boost (10%) for critical/high incidents.
	sevScore := 0.0
	if pattern.Severity == SeverityCritical || pattern.Severity == SeverityHigh {
		sevScore = 1.0
		reasons = append(reasons, "high-severity incident")
	}

	total := catScore*0.40 + kwScore*0.40 + ctScore*0.10 + sevScore*0.10

	return total, reasons
}

// categoryOverlap computes the Jaccard similarity between two category lists.
func categoryOverlap(a, b []FileCategory) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := make(map[FileCategory]bool)
	for _, c := range a {
		setA[c] = true
	}
	intersection := 0
	union := make(map[FileCategory]bool)
	for _, c := range a {
		union[c] = true
	}
	for _, c := range b {
		if setA[c] {
			intersection++
		}
		union[c] = true
	}
	if len(union) == 0 {
		return 0
	}
	return float64(intersection) / float64(len(union))
}

// keywordOverlap computes the Jaccard similarity between two keyword lists.
func keywordOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	normalizedA := normalizeKeywords(a)
	normalizedB := normalizeKeywords(b)

	setA := make(map[string]bool)
	for _, k := range normalizedA {
		setA[k] = true
	}
	intersection := 0
	union := make(map[string]bool)
	for _, k := range normalizedA {
		union[k] = true
	}
	for _, k := range normalizedB {
		if setA[k] {
			intersection++
		}
		union[k] = true
	}
	if len(union) == 0 {
		return 0
	}
	return float64(intersection) / float64(len(union))
}

// normalizeKeywords lowercases and trims keywords for comparison.
func normalizeKeywords(keywords []string) []string {
	result := make([]string, 0, len(keywords))
	for _, k := range keywords {
		normalized := strings.ToLower(strings.TrimSpace(k))
		if normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

// mergeKeywords combines explicit keywords with tokens extracted from the
// description and evidence strings.
func mergeKeywords(keywords []string, description string, evidence []string) []string {
	merged := make(map[string]bool)

	// Add explicit keywords
	for _, k := range keywords {
		merged[strings.ToLower(strings.TrimSpace(k))] = true
	}

	// Extract tokens from description
	for _, word := range tokenize(description) {
		if len(word) > 2 {
			merged[word] = true
		}
	}

	// Extract tokens from evidence
	for _, e := range evidence {
		for _, word := range tokenize(e) {
			if len(word) > 2 {
				merged[word] = true
			}
		}
	}

	result := make([]string, 0, len(merged))
	for k := range merged {
		result = append(result, k)
	}
	return result
}

// tokenize splits a string into lowercase word tokens.
func tokenize(s string) []string {
	s = strings.ToLower(s)
	// Split on non-alphanumeric
	return strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
}

// CategorizeFile determines the FileCategory from a file path.
func CategorizeFile(path string) FileCategory {
	path = strings.ToLower(path)
	switch {
	case strings.Contains(path, "auth") || strings.Contains(path, "login") || strings.Contains(path, "session"):
		return CategoryAuth
	case strings.Contains(path, "crypt") || strings.Contains(path, "cipher") || strings.Contains(path, "key"):
		return CategoryCrypto
	case strings.Contains(path, "migration") || strings.Contains(path, "model") || strings.Contains(path, "query") || strings.Contains(path, "repo"):
		return CategoryDatabase
	case strings.Contains(path, "handler") || strings.Contains(path, "route") || strings.Contains(path, "api") || strings.Contains(path, "endpoint"):
		return CategoryAPI
	case strings.Contains(path, "docker") || strings.Contains(path, "compose") || strings.Contains(path, "deploy"):
		return CategoryInfra
	case strings.Contains(path, "config") || strings.Contains(path, "yaml") || strings.Contains(path, "toml"):
		return CategoryConfig
	case strings.Contains(path, "_test") || strings.Contains(path, "spec"):
		return CategoryTest
	case strings.Contains(path, ".md") || strings.Contains(path, "doc"):
		return CategoryDoc
	case strings.Contains(path, ".tf") || strings.Contains(path, "terraform"):
		return CategoryIaC
	case strings.Contains(path, ".github") || strings.Contains(path, "ci") || strings.Contains(path, "workflow"):
		return CategoryCI
	case strings.Contains(path, "net") || strings.Contains(path, "http") || strings.Contains(path, "grpc"):
		return CategoryNetworking
	default:
		return CategoryOther
	}
}

// CategorizeFiles maps a list of file paths to their categories, returning
// the unique set.
func CategorizeFiles(paths []string) []FileCategory {
	seen := make(map[FileCategory]bool)
	for _, p := range paths {
		seen[CategorizeFile(p)] = true
	}
	result := make([]FileCategory, 0, len(seen))
	for c := range seen {
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool {
		return string(result[i]) < string(result[j])
	})
	return result
}
