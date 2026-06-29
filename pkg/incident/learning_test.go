package incident

import (
	"testing"
	"time"
)

// --- LearningDatabase Store/Get/Count ---

func TestLearningDB_StoreAndGet(t *testing.T) {
	db := NewLearningDatabase()
	p := &IncidentPattern{
		ID:          "inc-1",
		AgentID:     "agent-a",
		Categories:  []FileCategory{CategoryAuth},
		Severity:    SeverityHigh,
		Description: "SQL injection in auth handler",
		Timestamp:   time.Now(),
	}
	db.Store(p)

	if db.Count() != 1 {
		t.Errorf("Count() = %d, want 1", db.Count())
	}

	got := db.Get("inc-1")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Description != "SQL injection in auth handler" {
		t.Errorf("Description = %q", got.Description)
	}
}

func TestLearningDB_GetMissing(t *testing.T) {
	db := NewLearningDatabase()
	if db.Get("nonexistent") != nil {
		t.Error("expected nil for missing pattern")
	}
}

func TestLearningDB_StoreMultiple(t *testing.T) {
	db := NewLearningDatabase()
	for i := 0; i < 5; i++ {
		db.Store(&IncidentPattern{
			ID:       "inc-" + string(rune('a'+i)),
			Severity: SeverityLow,
		})
	}
	if db.Count() != 5 {
		t.Errorf("Count() = %d, want 5", db.Count())
	}
}

func TestLearningDB_StoreOverwrites(t *testing.T) {
	db := NewLearningDatabase()
	db.Store(&IncidentPattern{ID: "inc-1", Description: "v1"})
	db.Store(&IncidentPattern{ID: "inc-1", Description: "v2"})
	if db.Count() != 1 {
		t.Errorf("Count() = %d, want 1 (overwrite)", db.Count())
	}
	if db.Get("inc-1").Description != "v2" {
		t.Error("expected v2 after overwrite")
	}
}

// --- StoreFromIncident ---

func TestStoreFromIncident(t *testing.T) {
	db := NewLearningDatabase()
	inc := &Incident{
		ID:          "inc-from-incident",
		AgentID:     "agent-x",
		Severity:    SeverityCritical,
		Description: "Memory leak in database connection pool",
		Evidence:    []string{"heap dump shows leaked connections"},
		Timestamp:   time.Now(),
	}
	db.StoreFromIncident(inc, []FileCategory{CategoryDatabase}, ChangeModify, []string{"memory", "leak"}, "unclosed connections", []string{"check connection lifecycle"})

	p := db.Get("inc-from-incident")
	if p == nil {
		t.Fatal("pattern not stored")
	}
	if p.AgentID != "agent-x" {
		t.Errorf("AgentID = %q", p.AgentID)
	}
	if p.RootCause != "unclosed connections" {
		t.Errorf("RootCause = %q", p.RootCause)
	}
	if len(p.LessonsLearned) != 1 || p.LessonsLearned[0] != "check connection lifecycle" {
		t.Errorf("LessonsLearned = %v", p.LessonsLearned)
	}
	// Keywords should include explicit + extracted from description
	found := false
	for _, kw := range p.Keywords {
		if kw == "memory" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'memory' in keywords")
	}
}

// --- FeedReviewContext ---

func TestFeedReviewContext_CategoryMatch(t *testing.T) {
	db := NewLearningDatabase()
	db.Store(&IncidentPattern{
		ID:             "inc-1",
		Categories:     []FileCategory{CategoryAuth, CategoryDatabase},
		Severity:       SeverityCritical,
		Keywords:       []string{"injection"},
		LessonsLearned: []string{"validate all inputs", "use parameterized queries"},
		Timestamp:      time.Now(),
	})
	db.Store(&IncidentPattern{
		ID:         "inc-2",
		Categories: []FileCategory{CategoryConfig},
		Severity:   SeverityLow,
		Keywords:   []string{"typo"},
		Timestamp:  time.Now(),
	})

	ctx := PRContext{
		Categories: []FileCategory{CategoryAuth},
	}
	report := db.FeedReviewContext(ctx)

	if len(report.Items) != 1 {
		t.Fatalf("expected 1 matching item, got %d", len(report.Items))
	}
	if report.Items[0].Pattern.ID != "inc-1" {
		t.Errorf("expected inc-1, got %s", report.Items[0].Pattern.ID)
	}
	if report.MaxSimilarity != report.Items[0].Similarity {
		t.Error("MaxSimilarity should equal top item")
	}
}

func TestFeedReviewContext_KeywordMatch(t *testing.T) {
	db := NewLearningDatabase()
	db.Store(&IncidentPattern{
		ID:         "inc-1",
		Categories: []FileCategory{CategoryAPI},
		Keywords:   []string{"rate", "limit", "throttle"},
		Severity:   SeverityMedium,
		Timestamp:  time.Now(),
	})
	db.Store(&IncidentPattern{
		ID:         "inc-2",
		Categories: []FileCategory{CategoryAPI},
		Keywords:   []string{"caching"},
		Severity:   SeverityLow,
		Timestamp:  time.Now(),
	})

	ctx := PRContext{
		Categories: []FileCategory{CategoryAPI},
		Keywords:   []string{"rate", "limit", "queue"},
	}
	report := db.FeedReviewContext(ctx)

	if len(report.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(report.Items))
	}
	// inc-1 should rank higher (keyword overlap: rate+limit)
	if report.Items[0].Pattern.ID != "inc-1" {
		t.Errorf("expected inc-1 first, got %s", report.Items[0].Pattern.ID)
	}
}

func TestFeedReviewContext_NoMatches(t *testing.T) {
	db := NewLearningDatabase()
	db.Store(&IncidentPattern{
		ID:         "inc-1",
		Categories: []FileCategory{CategoryCrypto},
		Keywords:   []string{"aes", "encryption"},
		Severity:   SeverityLow,
		Timestamp:  time.Now(),
	})

	ctx := PRContext{
		Categories: []FileCategory{CategoryDoc},
		Keywords:   []string{"readme"},
	}
	report := db.FeedReviewContext(ctx)

	if len(report.Items) != 0 {
		t.Errorf("expected 0 matches, got %d", len(report.Items))
	}
}

func TestFeedReviewContext_EmptyDB(t *testing.T) {
	db := NewLearningDatabase()
	ctx := PRContext{Categories: []FileCategory{CategoryAuth}}
	report := db.FeedReviewContext(ctx)

	if len(report.Items) != 0 {
		t.Errorf("expected 0 items from empty DB")
	}
	if report.TotalIncidents != 0 {
		t.Errorf("TotalIncidents = %d, want 0", report.TotalIncidents)
	}
}

func TestFeedReviewContext_ReviewCriteriaCollected(t *testing.T) {
	db := NewLearningDatabase()
	db.Store(&IncidentPattern{
		ID:             "inc-1",
		Categories:     []FileCategory{CategoryAuth},
		Keywords:       []string{"password"},
		Severity:       SeverityHigh,
		LessonsLearned: []string{"hash passwords", "use bcrypt"},
		Timestamp:      time.Now(),
	})
	db.Store(&IncidentPattern{
		ID:             "inc-2",
		Categories:     []FileCategory{CategoryAuth},
		Keywords:       []string{"session"},
		Severity:       SeverityMedium,
		LessonsLearned: []string{"hash passwords", "expire sessions"},
		Timestamp:      time.Now(),
	})

	ctx := PRContext{
		Categories: []FileCategory{CategoryAuth},
		Keywords:   []string{"password", "session"},
	}
	report := db.FeedReviewContext(ctx)

	// Should have 3 unique criteria: "hash passwords", "use bcrypt", "expire sessions"
	if len(report.ReviewCriteria) != 3 {
		t.Errorf("expected 3 unique criteria, got %d: %v", len(report.ReviewCriteria), report.ReviewCriteria)
	}
}

func TestFeedReviewContext_ItemsSortedBySimilarity(t *testing.T) {
	db := NewLearningDatabase()
	// inc-1 matches on category + keyword + severity
	db.Store(&IncidentPattern{
		ID:         "inc-1",
		Categories: []FileCategory{CategoryAuth},
		Keywords:   []string{"password", "hash"},
		Severity:   SeverityCritical,
		Timestamp:  time.Now(),
	})
	// inc-2 matches on category only
	db.Store(&IncidentPattern{
		ID:         "inc-2",
		Categories: []FileCategory{CategoryAuth},
		Keywords:   []string{"random"},
		Severity:   SeverityLow,
		Timestamp:  time.Now(),
	})

	ctx := PRContext{
		Categories: []FileCategory{CategoryAuth},
		Keywords:   []string{"password", "hash"},
	}
	report := db.FeedReviewContext(ctx)

	if len(report.Items) < 2 {
		t.Fatalf("expected at least 2 items")
	}
	if report.Items[0].Similarity < report.Items[1].Similarity {
		t.Error("items not sorted by similarity descending")
	}
}

func TestFeedReviewContext_ChangeTypeMatch(t *testing.T) {
	db := NewLearningDatabase()
	db.Store(&IncidentPattern{
		ID:         "inc-1",
		Categories: []FileCategory{CategoryAPI},
		ChangeType: ChangeMigration,
		Keywords:   []string{"framework"},
		Severity:   SeverityHigh,
		Timestamp:  time.Now(),
	})
	db.Store(&IncidentPattern{
		ID:         "inc-2",
		Categories: []FileCategory{CategoryAPI},
		ChangeType: ChangeNew,
		Keywords:   []string{"framework"},
		Severity:   SeverityHigh,
		Timestamp:  time.Now(),
	})

	ctx := PRContext{
		Categories: []FileCategory{CategoryAPI},
		ChangeType: ChangeMigration,
		Keywords:   []string{"framework"},
	}
	report := db.FeedReviewContext(ctx)

	// inc-1 should rank higher (same change type)
	if report.Items[0].Pattern.ID != "inc-1" {
		t.Errorf("expected inc-1 first (same change type), got %s", report.Items[0].Pattern.ID)
	}
}

func TestFeedReviewContext_MatchReasons(t *testing.T) {
	db := NewLearningDatabase()
	db.Store(&IncidentPattern{
		ID:         "inc-1",
		Categories: []FileCategory{CategoryAuth},
		Keywords:   []string{"token"},
		ChangeType: ChangeModify,
		Severity:   SeverityCritical,
		Timestamp:  time.Now(),
	})

	ctx := PRContext{
		Categories: []FileCategory{CategoryAuth},
		ChangeType: ChangeModify,
		Keywords:   []string{"token"},
	}
	report := db.FeedReviewContext(ctx)

	if len(report.Items) != 1 {
		t.Fatal("expected 1 item")
	}
	reasons := report.Items[0].MatchReasons
	if len(reasons) == 0 {
		t.Error("expected match reasons")
	}
}

func TestFeedReviewContext_TotalIncidentsReported(t *testing.T) {
	db := NewLearningDatabase()
	for i := 0; i < 10; i++ {
		db.Store(&IncidentPattern{
			ID:         "inc-" + string(rune('a'+i)),
			Categories: []FileCategory{CategoryConfig},
			Keywords:   []string{"unused"},
			Severity:   SeverityLow,
			Timestamp:  time.Now(),
		})
	}
	ctx := PRContext{Categories: []FileCategory{CategoryAuth}}
	report := db.FeedReviewContext(ctx)

	if report.TotalIncidents != 10 {
		t.Errorf("TotalIncidents = %d, want 10", report.TotalIncidents)
	}
}

// --- scoreSimilarity unit tests ---

func TestScoreSimilarity_FullMatch(t *testing.T) {
	pattern := &IncidentPattern{
		Categories: []FileCategory{CategoryAuth, CategoryCrypto},
		Keywords:   []string{"password", "hash"},
		ChangeType: ChangeModify,
		Severity:   SeverityCritical,
	}
	ctx := PRContext{
		Categories: []FileCategory{CategoryAuth, CategoryCrypto},
		Keywords:   []string{"password", "hash"},
		ChangeType: ChangeModify,
	}
	score, reasons := scoreSimilarity(pattern, ctx)
	if score < 0.9 {
		t.Errorf("expected high similarity, got %.2f", score)
	}
	if len(reasons) < 3 {
		t.Errorf("expected multiple match reasons, got %v", reasons)
	}
}

func TestScoreSimilarity_NoMatch(t *testing.T) {
	pattern := &IncidentPattern{
		Categories: []FileCategory{CategoryCrypto},
		Keywords:   []string{"aes"},
		ChangeType: ChangeNew,
		Severity:   SeverityLow,
	}
	ctx := PRContext{
		Categories: []FileCategory{CategoryDoc},
		Keywords:   []string{"readme"},
		ChangeType: ChangeModify,
	}
	score, _ := scoreSimilarity(pattern, ctx)
	// Severity Low doesn't get the boost, so score should be 0
	if score != 0 {
		t.Errorf("expected 0 similarity, got %.2f", score)
	}
}

func TestScoreSimilarity_PartialMatch(t *testing.T) {
	pattern := &IncidentPattern{
		Categories: []FileCategory{CategoryAuth, CategoryAPI},
		Keywords:   []string{"token", "session"},
		ChangeType: ChangeModify,
		Severity:   SeverityMedium,
	}
	ctx := PRContext{
		Categories: []FileCategory{CategoryAuth},
		Keywords:   []string{"token"},
		ChangeType: ChangeModify,
	}
	score, _ := scoreSimilarity(pattern, ctx)
	if score <= 0 {
		t.Error("expected positive similarity for partial match")
	}
	if score >= 1.0 {
		t.Error("expected similarity < 1.0 for partial match")
	}
}

// --- CategoryOverlap ---

func TestCategoryOverlap_FullOverlap(t *testing.T) {
	score := categoryOverlap([]FileCategory{CategoryAuth, CategoryAPI}, []FileCategory{CategoryAuth, CategoryAPI})
	if score != 1.0 {
		t.Errorf("expected 1.0 for full overlap, got %.2f", score)
	}
}

func TestCategoryOverlap_NoOverlap(t *testing.T) {
	score := categoryOverlap([]FileCategory{CategoryAuth}, []FileCategory{CategoryConfig})
	if score != 0 {
		t.Errorf("expected 0 for no overlap, got %.2f", score)
	}
}

func TestCategoryOverlap_PartialOverlap(t *testing.T) {
	score := categoryOverlap([]FileCategory{CategoryAuth, CategoryAPI, CategoryConfig}, []FileCategory{CategoryAuth, CategoryDatabase})
	// intersection: {auth} = 1, union: {auth, api, config, database} = 4 → 0.25
	if score != 0.25 {
		t.Errorf("expected 0.25 for partial overlap, got %.2f", score)
	}
}

func TestCategoryOverlap_EmptyLists(t *testing.T) {
	score := categoryOverlap(nil, []FileCategory{CategoryAuth})
	if score != 0 {
		t.Errorf("expected 0 for empty list, got %.2f", score)
	}
}

// --- KeywordOverlap ---

func TestKeywordOverlap_FullOverlap(t *testing.T) {
	score := keywordOverlap([]string{"password", "hash"}, []string{"password", "hash"})
	if score != 1.0 {
		t.Errorf("expected 1.0, got %.2f", score)
	}
}

func TestKeywordOverlap_CaseInsensitive(t *testing.T) {
	score := keywordOverlap([]string{"Password", "HASH"}, []string{"password", "hash"})
	if score != 1.0 {
		t.Errorf("expected 1.0 (case insensitive), got %.2f", score)
	}
}

func TestKeywordOverlap_PartialOverlap(t *testing.T) {
	score := keywordOverlap([]string{"sql", "injection", "auth"}, []string{"sql", "xss"})
	// intersection: {sql} = 1, union: {sql, injection, auth, xss} = 4 → 0.25
	if score != 0.25 {
		t.Errorf("expected 0.25, got %.2f", score)
	}
}

func TestKeywordOverlap_EmptyLists(t *testing.T) {
	score := keywordOverlap(nil, []string{"test"})
	if score != 0 {
		t.Errorf("expected 0 for empty, got %.2f", score)
	}
}

// --- CategorizeFile ---

func TestCategorizeFile(t *testing.T) {
	tests := []struct {
		path     string
		expected FileCategory
	}{
		{"pkg/auth/handler.go", CategoryAuth},
		{"internal/login/session.go", CategoryAuth},
		{"pkg/crypto/cipher.go", CategoryCrypto},
		{"pkg/database/migration.go", CategoryDatabase},
		{"internal/api/handler.go", CategoryAPI},
		{"deploy/docker-compose.yaml", CategoryInfra},
		{"config/app.yaml", CategoryConfig},
		{"pkg/auth/handler_test.go", CategoryAuth}, // auth matches before _test
		{"README.md", CategoryDoc},
		{"infra/main.tf", CategoryIaC},
		{".github/workflows/ci.yaml", CategoryConfig}, // yaml matches before ci
		{"pkg/net/http_client.go", CategoryNetworking},
		{"pkg/utils/misc.go", CategoryOther},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := CategorizeFile(tc.path)
			if got != tc.expected {
				t.Errorf("CategorizeFile(%q) = %q, want %q", tc.path, got, tc.expected)
			}
		})
	}
}

func TestCategorizeFiles_UniqueSet(t *testing.T) {
	paths := []string{
		"pkg/auth/handler.go",
		"pkg/auth/handler_test.go",
		"internal/api/route.go",
		"config/app.yaml",
		"pkg/auth/middleware.go", // same category as handler.go
	}
	cats := CategorizeFiles(paths)
	// Should have unique categories: auth, api, config
	if len(cats) != 3 {
		t.Errorf("expected 3 unique categories, got %d: %v", len(cats), cats)
	}
}

// --- mergeKeywords ---

func TestMergeKeywords(t *testing.T) {
	kw := mergeKeywords([]string{"explicit"}, "This has some words in it", []string{"evidence token here"})
	// Should contain: explicit, this, has, some, words, evidence, token, here
	// (words > 2 chars)
	set := make(map[string]bool)
	for _, k := range kw {
		set[k] = true
	}
	if !set["explicit"] {
		t.Error("expected 'explicit' in merged keywords")
	}
	if !set["evidence"] {
		t.Error("expected 'evidence' in merged keywords")
	}
	if !set["words"] {
		t.Error("expected 'words' from description")
	}
}

// --- All ---

func TestLearningDB_All(t *testing.T) {
	db := NewLearningDatabase()
	db.Store(&IncidentPattern{ID: "a"})
	db.Store(&IncidentPattern{ID: "b"})
	db.Store(&IncidentPattern{ID: "c"})

	all := db.All()
	if len(all) != 3 {
		t.Errorf("expected 3 patterns, got %d", len(all))
	}
}

// --- Integration: full learning flow ---

func TestFullLearningFlow(t *testing.T) {
	db := NewLearningDatabase()

	// Simulate storing incidents from production
	incidents := []*Incident{
		{
			ID:          "inc-sql-injection",
			AgentID:     "agent-1",
			Severity:    SeverityCritical,
			Description: "SQL injection vulnerability in auth handler",
			Evidence:    []string{"unescaped user input in query"},
			Timestamp:   time.Now(),
		},
		{
			ID:          "inc-memory-leak",
			AgentID:     "agent-2",
			Severity:    SeverityHigh,
			Description: "Memory leak in database connection pool",
			Evidence:    []string{"unclosed connections accumulate"},
			Timestamp:   time.Now(),
		},
	}

	db.StoreFromIncident(incidents[0], []FileCategory{CategoryAuth, CategoryDatabase}, ChangeModify, []string{"sql", "injection"}, "unescaped input", []string{"use parameterized queries", "validate all inputs"})
	db.StoreFromIncident(incidents[1], []FileCategory{CategoryDatabase}, ChangeModify, []string{"memory", "leak"}, "unclosed connections", []string{"check connection lifecycle", "use defer Close()"})

	// Now a new PR touches auth + database code
	ctx := PRContext{
		Categories: []FileCategory{CategoryAuth, CategoryDatabase},
		ChangeType: ChangeModify,
		Keywords:   []string{"sql", "query", "input"},
		Files:      []string{"pkg/auth/handler.go", "pkg/database/query.go"},
	}
	report := db.FeedReviewContext(ctx)

	if len(report.Items) < 1 {
		t.Fatal("expected at least 1 relevant incident")
	}

	// SQL injection incident should rank highest (matches auth+db+keywords+severity)
	top := report.Items[0]
	if top.Pattern.ID != "inc-sql-injection" {
		t.Errorf("expected inc-sql-injection first, got %s", top.Pattern.ID)
	}
	if top.Similarity < 0.3 {
		t.Errorf("expected high similarity, got %.2f", top.Similarity)
	}

	// Should have accumulated review criteria from both incidents
	if len(report.ReviewCriteria) < 3 {
		t.Errorf("expected at least 3 review criteria, got %d: %v", len(report.ReviewCriteria), report.ReviewCriteria)
	}
}
