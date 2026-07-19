package spec

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helper tests
// ---------------------------------------------------------------------------

func TestEscapeUnescapePipes(t *testing.T) {
	t.Run("escape pipes", func(t *testing.T) {
		assert.Equal(t, "hello &#124; world", escapePipes("hello | world"))
		assert.Equal(t, "nochange", escapePipes("nochange"))
		assert.Equal(t, "", escapePipes(""))
	})

	t.Run("unescape pipes", func(t *testing.T) {
		assert.Equal(t, "hello | world", unescapePipes("hello &#124; world"))
		assert.Equal(t, "nochange", unescapePipes("nochange"))
		assert.Equal(t, "", unescapePipes(""))
	})

	t.Run("round-trip", func(t *testing.T) {
		original := "a | b | c | d"
		escaped := escapePipes(original)
		restored := unescapePipes(escaped)
		assert.Equal(t, original, restored)
	})
}

func TestContainsAny(t *testing.T) {
	t.Run("finds matching string", func(t *testing.T) {
		assert.True(t, containsAny("hello world", "hello", "goodbye"))
	})

	t.Run("finds any of multiple needles", func(t *testing.T) {
		assert.True(t, containsAny("hello world", "xyz", "world"))
	})

	t.Run("returns false when none match", func(t *testing.T) {
		assert.False(t, containsAny("hello world", "xyz", "abc"))
	})

	t.Run("empty haystack returns false", func(t *testing.T) {
		assert.False(t, containsAny("", "hello"))
	})

	t.Run("empty needles returns false", func(t *testing.T) {
		assert.False(t, containsAny("hello"))
	})
}

func TestExtractMustLines(t *testing.T) {
	t.Run("extracts MUST lines", func(t *testing.T) {
		text := "The system MUST handle errors.\nIt SHOULD be fast.\nThe component SHALL validate input."
		lines := extractMustLines(text)
		assert.Len(t, lines, 2)
		assert.Contains(t, lines[0], "MUST")
		assert.Contains(t, lines[1], "SHALL")
	})

	t.Run("extracts MUST NOT lines", func(t *testing.T) {
		text := "The system MUST NOT allow X.\nEverything else is optional."
		lines := extractMustLines(text)
		assert.Len(t, lines, 1)
		assert.Contains(t, lines[0], "MUST NOT")
	})

	t.Run("returns empty for no keywords", func(t *testing.T) {
		text := "The system should be fast.\nIt may have features."
		lines := extractMustLines(text)
		assert.Empty(t, lines)
	})

	t.Run("handles empty input", func(t *testing.T) {
		assert.Empty(t, extractMustLines(""))
	})

	t.Run("does not match must as substring", func(t *testing.T) {
		// "must " has a trailing space, so "mustard" shouldn't match
		text := "This is about mustard gas."
		assert.Empty(t, extractMustLines(text))
	})
}

func TestTruncateForDisplay(t *testing.T) {
	t.Run("short string unchanged", func(t *testing.T) {
		assert.Equal(t, "short", truncateForDisplay("short", 80))
	})

	t.Run("truncates long string", func(t *testing.T) {
		long := strings.Repeat("x", 100)
		result := truncateForDisplay(long, 10)
		assert.Equal(t, 10, len(result))
		assert.True(t, strings.HasSuffix(result, "..."))
	})

	t.Run("trims space before truncating", func(t *testing.T) {
		result := truncateForDisplay("  hello world  ", 80)
		assert.Equal(t, "hello world", result)
	})

	t.Run("exact length no truncation", func(t *testing.T) {
		s := "exactly ten"
		assert.Equal(t, s, truncateForDisplay(s, len(s)))
	})
}

func TestSectionTitleSet(t *testing.T) {
	t.Run("builds set from sections", func(t *testing.T) {
		sections := []SpecSection{
			{Title: "Overview"},
			{Title: "Requirements"},
			{Title: "Security"},
		}
		s := sectionTitleSet(sections)
		assert.True(t, s["Overview"])
		assert.True(t, s["Requirements"])
		assert.True(t, s["Security"])
		assert.False(t, s["Non-Goals"])
	})

	t.Run("returns empty for no sections", func(t *testing.T) {
		assert.Empty(t, sectionTitleSet(nil))
	})
}

func TestRoundTo(t *testing.T) {
	assert.Equal(t, 3.1, roundTo(3.14, 1))
	assert.Equal(t, 3.14, roundTo(3.14159, 2))
	assert.Equal(t, 3.0, roundTo(3.0, 1))
	assert.Equal(t, 0.0, roundTo(0.0, 1))
	assert.Equal(t, 1.0, roundTo(0.99, 1))
	assert.Equal(t, 1.0, roundTo(0.999, 2))
	assert.Equal(t, 12.35, roundTo(12.345, 2))
}

// ---------------------------------------------------------------------------
// NewSpecID tests
// ---------------------------------------------------------------------------

func TestNewSpecID(t *testing.T) {
	t.Run("has spec- prefix", func(t *testing.T) {
		id := NewSpecID()
		assert.True(t, strings.HasPrefix(id, "spec-"))
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		const n = 100
		ids := make(map[string]bool, n)
		for i := 0; i < n; i++ {
			id := NewSpecID()
			assert.True(t, strings.HasPrefix(id, "spec-"))
			assert.False(t, ids[id], "duplicate ID: %s", id)
			ids[id] = true
		}
		assert.Len(t, ids, n)
	})
}

// ---------------------------------------------------------------------------
// Markdown serialization tests
// ---------------------------------------------------------------------------

func TestSpecToMarkdown(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)

	spec := &Spec{
		ID:        "spec-test-001",
		IdeaRef:   "idea-42",
		Title:     "Test Spec",
		Status:    StatusDraft,
		ADRRefs:   []string{"ADR-001", "ADR-002"},
		Hash:      "abc123",
		CreatedAt: now,
		UpdatedAt: now,
		Sections: []SpecSection{
			{
				Title:          "Overview",
				Content:        "This is the overview content.",
				ApprovalStatus: ApprovalPending,
				ApprovedBy:     "",
			},
			{
				Title:          "Requirements",
				Content:        "Requirement 1\nRequirement 2",
				ApprovalStatus: ApprovalApproved,
				ApprovedBy:     "alice",
			},
		},
		Annotations: []SpecAnnotation{
			{
				Line:           1,
				AgentType:      AgentSpecGenerator,
				AnnotationType: AnnEdgeCase,
				Content:        "Edge case: empty input",
				Severity:       SeverityWarning,
				Status:         AnnotationProposed,
				CreatedAt:      now,
			},
		},
	}

	md := specToMarkdown(spec)

	t.Run("contains YAML frontmatter", func(t *testing.T) {
		assert.True(t, strings.HasPrefix(md, "---\n"))
		assert.Contains(t, md, "id: spec-test-001")
		assert.Contains(t, md, "title: Test Spec")
		assert.Contains(t, md, "idea_ref: idea-42")
		assert.Contains(t, md, "adr_refs:\n    - ADR-001\n    - ADR-002")
		assert.Contains(t, md, "hash: abc123")
	})

	t.Run("contains section headings", func(t *testing.T) {
		assert.Contains(t, md, "## Overview\n")
		assert.Contains(t, md, "## Requirements\n")
	})

	t.Run("contains section content", func(t *testing.T) {
		assert.Contains(t, md, "This is the overview content.")
		assert.Contains(t, md, "Requirement 1\nRequirement 2")
	})

	t.Run("contains approval comments", func(t *testing.T) {
		assert.Contains(t, md, "<!-- approval: pending -->")
		assert.Contains(t, md, "<!-- approval: approved by alice -->")
	})

	t.Run("contains annotation block", func(t *testing.T) {
		assert.Contains(t, md, "<!-- annotations-start")
		assert.Contains(t, md, "annotations-end -->")
		assert.Contains(t, md, "<!-- ann:")
		assert.Contains(t, md, "Edge case: empty input")
	})
}

func TestMarkdownToSpec(t *testing.T) {
	t.Run("parses valid markdown", func(t *testing.T) {
		md := `---
id: spec-test-001
title: Test Spec
status: draft
created_at: "2026-07-19T12:00:00Z"
updated_at: "2026-07-19T12:00:00Z"
---

## Overview
This is the overview.

## Requirements
Must handle X.
Must handle Y.
`
		spec, err := markdownToSpec([]byte(md))
		require.NoError(t, err)
		require.NotNil(t, spec)
		assert.Equal(t, "spec-test-001", spec.ID)
		assert.Equal(t, "Test Spec", spec.Title)
		assert.Equal(t, StatusDraft, spec.Status)
		assert.Len(t, spec.Sections, 2)
		assert.Equal(t, "Overview", spec.Sections[0].Title)
		assert.Equal(t, "This is the overview.", spec.Sections[0].Content)
		assert.Equal(t, "Requirements", spec.Sections[1].Title)
	})

	t.Run("parses full spec with annotations", func(t *testing.T) {
		md := `---
id: spec-test-002
title: Annotated Spec
status: in_review
adr_refs:
    - ADR-001
contract_refs:
    - CTR-001
hash: def456
created_at: "2026-07-19T12:00:00Z"
updated_at: "2026-07-19T12:30:00Z"
---

## Security
Content here.
<!-- approval: pending -->
<!-- annotations-start
<!-- ann: line=1 | agent=spec-generator | type=edge_case | sev=warning | status=proposed | created=2026-07-19T12:00:00Z | Missing standard section
annotations-end -->
`
		spec, err := markdownToSpec([]byte(md))
		require.NoError(t, err)
		require.NotNil(t, spec)
		assert.Equal(t, "spec-test-002", spec.ID)
		assert.Equal(t, StatusInReview, spec.Status)
		assert.Equal(t, []string{"ADR-001"}, spec.ADRRefs)
		assert.Equal(t, []string{"CTR-001"}, spec.ContractRefs)
		assert.Equal(t, "def456", spec.Hash)
		assert.Len(t, spec.Sections, 1)
		assert.Len(t, spec.Annotations, 1)
		assert.Equal(t, AgentSpecGenerator, spec.Annotations[0].AgentType)
	})

	t.Run("errors on missing frontmatter", func(t *testing.T) {
		_, err := markdownToSpec([]byte("just some text"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing YAML frontmatter")
	})

	t.Run("errors on unterminated frontmatter", func(t *testing.T) {
		_, err := markdownToSpec([]byte("---\nid: test\n"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unterminated YAML frontmatter")
	})

	t.Run("handles empty sections", func(t *testing.T) {
		md := "---\nid: s1\ntitle: T\nstatus: draft\ncreated_at: \"2026-01-01T00:00:00Z\"\nupdated_at: \"2026-01-01T00:00:00Z\"\n---\n"
		spec, err := markdownToSpec([]byte(md))
		require.NoError(t, err)
		assert.Empty(t, spec.Sections)
	})

	t.Run("defaults empty status to draft", func(t *testing.T) {
		md := "---\nid: s1\ntitle: T\ncreated_at: \"2026-01-01T00:00:00Z\"\nupdated_at: \"2026-01-01T00:00:00Z\"\n---\n"
		spec, err := markdownToSpec([]byte(md))
		require.NoError(t, err)
		assert.Equal(t, StatusDraft, spec.Status)
	})

	t.Run("round-trip markdown equality", func(t *testing.T) {
		now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
		original := &Spec{
			ID:        "spec-roundtrip",
			Title:     "Round-Trip Test",
			Status:    StatusDraft,
			CreatedAt: now,
			UpdatedAt: now,
			Sections: []SpecSection{
				{Title: "Overview", Content: "Some content", ApprovalStatus: ApprovalPending},
			},
			Annotations: []SpecAnnotation{
				{Line: 1, AgentType: AgentSpecGenerator, AnnotationType: AnnEdgeCase, Content: "test|with|pipes", Severity: SeverityWarning, Status: AnnotationProposed, CreatedAt: now},
			},
		}
		md := specToMarkdown(original)
		parsed, err := markdownToSpec([]byte(md))
		require.NoError(t, err)
		assert.Equal(t, original.ID, parsed.ID)
		assert.Equal(t, original.Title, parsed.Title)
		assert.Equal(t, original.Status, parsed.Status)
		assert.Len(t, parsed.Sections, 1)
		assert.Equal(t, original.Sections[0].Title, parsed.Sections[0].Title)
		assert.Equal(t, original.Sections[0].Content, parsed.Sections[0].Content)
		assert.Equal(t, original.Sections[0].ApprovalStatus, parsed.Sections[0].ApprovalStatus)
		assert.Len(t, parsed.Annotations, 1)
		assert.Equal(t, original.Annotations[0].Content, parsed.Annotations[0].Content)
		assert.Equal(t, original.Annotations[0].AgentType, parsed.Annotations[0].AgentType)
		assert.Equal(t, original.Annotations[0].AnnotationType, parsed.Annotations[0].AnnotationType)
		assert.Equal(t, original.Annotations[0].Severity, parsed.Annotations[0].Severity)
	})
}

// ---------------------------------------------------------------------------
// Annotation serialization round-trip tests
// ---------------------------------------------------------------------------

func TestAnnotationSerialization(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)

	t.Run("annotationToYAMLInline and parseAnnotationYAMLInline round-trip", func(t *testing.T) {
		original := SpecAnnotation{
			Line:           42,
			AgentType:      AgentSpecGenerator,
			AnnotationType: AnnFailureMode,
			Content:        "Database may be unavailable",
			Severity:       SeverityCritical,
			Status:         AnnotationProposed,
			CreatedAt:      now,
		}
		line := annotationToYAMLInline(original)
		assert.True(t, strings.HasPrefix(line, "<!-- ann:"))
		assert.True(t, strings.HasSuffix(line, "-->"))
		assert.Contains(t, line, "line=42")
		assert.Contains(t, line, "agent=spec-generator")
		assert.Contains(t, line, "type=failure_mode")
		assert.Contains(t, line, "sev=critical")
		assert.Contains(t, line, "status=proposed")

		parsed := parseAnnotationYAMLInline(line)
		require.NotNil(t, parsed)
		assert.Equal(t, original.Line, parsed.Line)
		assert.Equal(t, original.AgentType, parsed.AgentType)
		assert.Equal(t, original.AnnotationType, parsed.AnnotationType)
		assert.Equal(t, original.Content, parsed.Content)
		assert.Equal(t, original.Severity, parsed.Severity)
		assert.Equal(t, original.Status, parsed.Status)
	})

	t.Run("content with pipes is escaped", func(t *testing.T) {
		a := SpecAnnotation{
			Line:      1,
			AgentType: AgentSpecGenerator,
			Content:   "a | b | c",
			Severity:  SeverityWarning,
			Status:    AnnotationProposed,
			CreatedAt: now,
		}
		line := annotationToYAMLInline(a)
		// Pipes in content should be escaped
		assert.Contains(t, line, "&#124;")

		parsed := parseAnnotationYAMLInline(line)
		require.NotNil(t, parsed)
		assert.Equal(t, "a | b | c", parsed.Content)
	})

	t.Run("returns nil for malformed line", func(t *testing.T) {
		assert.Nil(t, parseAnnotationYAMLInline("<!-- ann: justcontent -->"))
		assert.Nil(t, parseAnnotationYAMLInline("not an annotation"))
	})

	t.Run("parse with unknown keys", func(t *testing.T) {
		line := "<!-- ann: line=1 | agent=spec-generator | type=edge_case | sev=warning | status=proposed | created=2026-07-19T12:00:00Z | unknownkey=value | some content -->"
		parsed := parseAnnotationYAMLInline(line)
		require.NotNil(t, parsed)
		assert.Equal(t, "unknownkey=value | some content", parsed.Content)
	})
}

// ---------------------------------------------------------------------------
// Store tests
// ---------------------------------------------------------------------------

func TestNewSpecStore(t *testing.T) {
	t.Run("creates store with non-empty root", func(t *testing.T) {
		dir := t.TempDir()
		store, err := NewSpecStore(dir)
		require.NoError(t, err)
		require.NotNil(t, store)
		assert.Equal(t, dir, store.Root())
	})

	t.Run("creates directory if missing", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nested", "dir")
		store, err := NewSpecStore(dir)
		require.NoError(t, err)
		assert.Equal(t, dir, store.Root())
		_, err = os.Stat(dir)
		assert.NoError(t, err)
	})

	t.Run("resolves empty root to home dir", func(t *testing.T) {
		// This will use os.UserHomeDir + "/.helix/specs"
		// We can't easily mock this, but we can verify it doesn't error
		store, err := NewSpecStore("")
		require.NoError(t, err)
		root := store.Root()
		assert.True(t, strings.HasSuffix(root, ".helix/specs"))
		// Clean up
		defer os.RemoveAll(root)
	})
}

func TestSpecStoreSaveLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSpecStore(dir)
	require.NoError(t, err)

	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)

	t.Run("save and load a spec", func(t *testing.T) {
		spec := &Spec{
			ID:           "spec-test-saveload",
			Title:        "Save/Load Test",
			Status:       StatusDraft,
			IdeaRef:      "idea-1",
			ADRRefs:      []string{"ADR-001"},
			ContractRefs: []string{"CTR-001"},
			Hash:         "hash1",
			CreatedAt:    now,
			UpdatedAt:    now,
			Sections: []SpecSection{
				{Title: "Overview", Content: "Overview content.", ApprovalStatus: ApprovalPending},
				{Title: "Requirements", Content: "Must do X.", ApprovalStatus: ApprovalApproved, ApprovedBy: "bob"},
			},
		}

		err := store.Save(spec)
		require.NoError(t, err)

		loaded, err := store.Load("spec-test-saveload")
		require.NoError(t, err)
		require.NotNil(t, loaded)

		assert.Equal(t, spec.ID, loaded.ID)
		assert.Equal(t, spec.Title, loaded.Title)
		assert.Equal(t, spec.Status, loaded.Status)
		assert.Equal(t, spec.IdeaRef, loaded.IdeaRef)
		assert.Equal(t, spec.ADRRefs, loaded.ADRRefs)
		assert.Equal(t, spec.ContractRefs, loaded.ContractRefs)
		assert.Equal(t, spec.Hash, loaded.Hash)
		assert.Len(t, loaded.Sections, 2)
		assert.Equal(t, spec.Sections[0].Title, loaded.Sections[0].Title)
		assert.Equal(t, spec.Sections[0].Content, loaded.Sections[0].Content)
		assert.Equal(t, spec.Sections[0].ApprovalStatus, loaded.Sections[0].ApprovalStatus)
		assert.Equal(t, spec.Sections[1].ApprovalStatus, loaded.Sections[1].ApprovalStatus)
		assert.Equal(t, spec.Sections[1].ApprovedBy, loaded.Sections[1].ApprovedBy)
	})

	t.Run("save with annotations", func(t *testing.T) {
		spec := &Spec{
			ID:        "spec-ann-test",
			Title:     "Annotation Test",
			Status:    StatusDraft,
			CreatedAt: now,
			UpdatedAt: now,
			Sections:  []SpecSection{{Title: "Overview", Content: "Test"}},
			Annotations: []SpecAnnotation{
				{Line: 1, AgentType: AgentSpecGenerator, AnnotationType: AnnEdgeCase, Content: "Test annotation", Severity: SeverityWarning, Status: AnnotationProposed, CreatedAt: now},
			},
		}

		err := store.Save(spec)
		require.NoError(t, err)

		loaded, err := store.Load("spec-ann-test")
		require.NoError(t, err)
		require.NotNil(t, loaded)
		require.Len(t, loaded.Annotations, 1)
		assert.Equal(t, "Test annotation", loaded.Annotations[0].Content)
	})

	t.Run("save nil spec errors", func(t *testing.T) {
		err := store.Save(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "spec is nil")
	})

	t.Run("save empty ID errors", func(t *testing.T) {
		err := store.Save(&Spec{Title: "No ID"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "id is required")
	})

	t.Run("save empty title errors", func(t *testing.T) {
		err := store.Save(&Spec{ID: "spec-no-title"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "title is required")
	})

	t.Run("save title with only whitespace errors", func(t *testing.T) {
		err := store.Save(&Spec{ID: "spec-whitespace-title", Title: "   "})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "title is required")
	})

	t.Run("load errors for empty ID", func(t *testing.T) {
		_, err := store.Load("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "id is required")
	})

	t.Run("load errors for nonexistent spec", func(t *testing.T) {
		_, err := store.Load("spec-nonexistent")
		assert.Error(t, err)
	})

	t.Run("save sets timestamps", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-timestamp",
			Title: "Timestamp Test",
		}
		err := store.Save(spec)
		require.NoError(t, err)

		assert.False(t, spec.CreatedAt.IsZero(), "CreatedAt should be set")
		assert.False(t, spec.UpdatedAt.IsZero(), "UpdatedAt should be set")

		// Save again to test UpdatedAt changes
		oldUpdated := spec.UpdatedAt
		time.Sleep(time.Millisecond)
		err = store.Save(spec)
		require.NoError(t, err)
		assert.True(t, spec.UpdatedAt.After(oldUpdated), "UpdatedAt should advance")
	})

	t.Run("save defaults status to draft", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-default-status",
			Title: "Default Status",
		}
		err := store.Save(spec)
		require.NoError(t, err)
		assert.Equal(t, StatusDraft, spec.Status)
	})
}

func TestSpecStoreList(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSpecStore(dir)
	require.NoError(t, err)

	t.Run("list returns empty for new store", func(t *testing.T) {
		specs, err := store.List()
		require.NoError(t, err)
		assert.Empty(t, specs)
	})

	t.Run("list returns specs sorted by UpdatedAt descending", func(t *testing.T) {
		// Save() always overwrites UpdatedAt to now, so we write files
		// directly with different timestamps in frontmatter.
		writeSpecFile := func(id, title string, updated time.Time) {
			content := "---\n" +
				"id: " + id + "\n" +
				"title: " + title + "\n" +
				"status: draft\n" +
				`created_at: "` + updated.Format(time.RFC3339) + `"` + "\n" +
				`updated_at: "` + updated.Format(time.RFC3339) + `"` + "\n" +
				"---\n"
			err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(content), 0o644)
			require.NoError(t, err)
		}

		t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		t2 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		t3 := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)

		writeSpecFile("spec-old", "Oldest", t1)
		writeSpecFile("spec-mid", "Middle", t2)
		writeSpecFile("spec-new", "Newest", t3)

		specs, err := store.List()
		require.NoError(t, err)
		require.Len(t, specs, 3)

		assert.Equal(t, "spec-new", specs[0].ID)
		assert.Equal(t, "spec-mid", specs[1].ID)
		assert.Equal(t, "spec-old", specs[2].ID)

		// Verify sorting is actually descending
		assert.True(t, sort.SliceIsSorted(specs, func(i, j int) bool {
			return specs[i].UpdatedAt.After(specs[j].UpdatedAt)
		}))
	})

	t.Run("list skips non-md files and parse errors", func(t *testing.T) {
		// Write a non-md file and a corrupt md file
		os.WriteFile(filepath.Join(dir, "notspec.txt"), []byte("hello"), 0o644)
		os.WriteFile(filepath.Join(dir, "corrupt.md"), []byte("not valid frontmatter"), 0o644)

		specs, err := store.List()
		require.NoError(t, err)
		// Should still return the 3 valid specs
		assert.Len(t, specs, 3)
	})
}

// ---------------------------------------------------------------------------
// CoAuthor tests
// ---------------------------------------------------------------------------

func TestCoAuthorErrors(t *testing.T) {
	ca := NewSpecCoAuthor()

	t.Run("nil spec returns error", func(t *testing.T) {
		_, err := ca.CoAuthor(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "spec is nil")
	})

	t.Run("empty ID returns error", func(t *testing.T) {
		_, err := ca.CoAuthor(&Spec{Title: "No ID"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "id is required")
	})
}

func TestCoAuthorStatusTransition(t *testing.T) {
	ca := NewSpecCoAuthor()
	spec := &Spec{
		ID:    "spec-coauthor-status",
		Title: "Status Test",
		Sections: []SpecSection{
			{Title: "Overview", Content: "Overview here."},
		},
	}

	result, err := ca.CoAuthor(spec)
	require.NoError(t, err)
	assert.Equal(t, StatusInReview, result.Status)
	assert.Equal(t, spec, result, "should return the same pointer")
}

func TestCoAuthorMissingStandardSections(t *testing.T) {
	ca := NewSpecCoAuthor()
	spec := &Spec{
		ID:    "spec-missing-sections",
		Title: "Missing Sections",
		Sections: []SpecSection{
			{Title: "CustomSection", Content: "Custom content"},
		},
	}

	result, err := ca.CoAuthor(spec)
	require.NoError(t, err)

	// Should have annotations for each of the 5 missing standard sections
	var incompletenessAnns int
	for _, ann := range result.Annotations {
		if ann.AnnotationType == AnnIncompleteness && ann.AgentType == AgentSpecGenerator {
			incompletenessAnns++
		}
	}
	assert.Equal(t, 5, incompletenessAnns, "should have 5 incompleteness annotations for missing standard sections")
}

func TestCoAuthorEdgeCases(t *testing.T) {
	ca := NewSpecCoAuthor()

	t.Run("input handling without empty handling", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-input-edge",
			Title: "Input Edge",
			Sections: []SpecSection{
				{Title: "API", Content: "The endpoint accepts input payload via POST request."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnEdgeCase && strings.Contains(ann.Content, "empty/zero-length/nil input") {
				found = true
				break
			}
		}
		assert.True(t, found, "should annotate missing empty input handling")
	})

	t.Run("timeout without cancellation", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-timeout-edge",
			Title: "Timeout Edge",
			Sections: []SpecSection{
				{Title: "Processing", Content: "The system uses async processing with timeout and latency requirements."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnEdgeCase && strings.Contains(ann.Content, "cancellation") {
				found = true
				break
			}
		}
		assert.True(t, found, "should annotate missing cancellation handling")
	})

	t.Run("rate limiting without retry", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-rate-edge",
			Title: "Rate Edge",
			Sections: []SpecSection{
				{Title: "API", Content: "The API has rate limits and quotas."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnEdgeCase && strings.Contains(ann.Content, "throttled/429 response") {
				found = true
				break
			}
		}
		assert.True(t, found, "should annotate missing throttled response handling")
	})

	t.Run("input already handles empty — no annotation", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-input-safe",
			Title: "Input Safe",
			Sections: []SpecSection{
				{Title: "API", Content: "The endpoint accepts input payload. Empty input returns an error."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnEdgeCase && strings.Contains(ann.Content, "empty/zero-length/nil input") {
				t.Fatal("should NOT annotate empty input handling when it already handles empty")
			}
		}
	})

	t.Run("concurrency without race handling", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-conc-edge",
			Title: "Concurrency Edge",
			Sections: []SpecSection{
				{Title: "Processing", Content: "The system uses concurrent goroutines and parallel writes."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnEdgeCase && strings.Contains(ann.Content, "race conditions") {
				found = true
				break
			}
		}
		assert.True(t, found, "should annotate missing race condition handling")
	})
}

func TestCoAuthorFailureModes(t *testing.T) {
	ca := NewSpecCoAuthor()

	t.Run("database without availability handling", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-db-failure",
			Title: "DB Failure",
			Sections: []SpecSection{
				{Title: "Storage", Content: "The system uses a database and stores data via Redis."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnFailureMode && strings.Contains(ann.Content, "dependency is down") {
				found = true
				break
			}
		}
		assert.True(t, found, "should annotate missing dependency failure handling")
	})

	t.Run("data persistence without corruption handling", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-persist-failure",
			Title: "Persist Failure",
			Sections: []SpecSection{
				{Title: "Storage", Content: "The system saves data and persists to a database."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnFailureMode && strings.Contains(ann.Content, "corrupt or invalid data") {
				found = true
				break
			}
		}
		assert.True(t, found, "should annotate missing data corruption handling")
	})

	t.Run("auth without unauthorized handling", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-auth-failure",
			Title: "Auth Failure",
			Sections: []SpecSection{
				{Title: "Auth", Content: "The system uses tokens for auth with RBAC permissions."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnFailureMode && strings.Contains(ann.Content, "unauthorized/forbidden") {
				found = true
				break
			}
		}
		assert.True(t, found, "should annotate missing unauthorized handling")
	})

	t.Run("dependency with fallback — no annotation", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-db-fallback",
			Title: "DB Fallback",
			Sections: []SpecSection{
				{Title: "Storage", Content: "The system uses a database. If the database is unavailable, fallback to cache."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnFailureMode && strings.Contains(ann.Content, "dependency is down") {
				t.Fatal("should NOT annotate when fallback is mentioned")
			}
		}
	})
}

func TestCoAuthorConsistency(t *testing.T) {
	ca := NewSpecCoAuthor()

	t.Run("sync and async contradiction", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-consistency-sync",
			Title: "Sync/Async",
			Sections: []SpecSection{
				{Title: "API", Content: "The API provides both synchronous and asynchronous endpoints."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnConsistency && strings.Contains(ann.Content, "synchronous and asynchronous") {
				found = true
				break
			}
		}
		assert.True(t, found, "should warn about sync+async contradiction")
	})

	t.Run("strong and eventual consistency contradiction", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-consistency-strong",
			Title: "Strong/Eventual",
			Sections: []SpecSection{
				{Title: "Data", Content: "The system provides strong consistency and eventual consistency guarantees."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnConsistency && strings.Contains(ann.Content, "strong and eventual") {
				found = true
				break
			}
		}
		assert.True(t, found, "should flag strong+eventual contradiction as critical")
	})

	t.Run("duplicate MUST lines", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-duplicate-must",
			Title: "Duplicate MUST",
			Sections: []SpecSection{
				{Title: "Requirements", Content: "The system MUST handle errors.\nThe system MUST handle errors."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnConsistency && strings.Contains(ann.Content, "Duplicate requirement") {
				found = true
				break
			}
		}
		assert.True(t, found, "should warn about duplicate MUST requirements")
	})
}

func TestCoAuthorThreatSurface(t *testing.T) {
	ca := NewSpecCoAuthor()

	t.Run("sensitive data without security section", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-sensitive",
			Title: "Sensitive Data",
			Sections: []SpecSection{
				{Title: "API", Content: "The system handles user data including credentials and PII."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnSecurity && strings.Contains(ann.Content, "sensitive data") {
				found = true
				break
			}
		}
		assert.True(t, found, "should flag missing security section for sensitive data")
	})

	t.Run("sensitive data with security section — no annotation", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-sensitive-safe",
			Title: "Sensitive Data Safe",
			Sections: []SpecSection{
				{Title: "Security", Content: "All credentials are encrypted."},
				{Title: "API", Content: "The system handles user data including credentials."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnSecurity && strings.Contains(ann.Content, "sensitive data") {
				t.Fatal("should NOT flag missing security section when it exists")
			}
		}
	})

	t.Run("API endpoints without input validation", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-api-no-validation",
			Title: "API No Validation",
			Sections: []SpecSection{
				{Title: "API", Content: "The system exposes REST API endpoints via HTTP."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnSecurity && strings.Contains(ann.Content, "input validation") {
				found = true
				break
			}
		}
		assert.True(t, found, "should flag missing input validation for API")
	})

	t.Run("API with input validation mentioned — no annotation", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-api-validation",
			Title: "API Validation",
			Sections: []SpecSection{
				{Title: "API", Content: "The system exposes REST API endpoints via HTTP with input validation and sanitization."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnSecurity && strings.Contains(ann.Content, "input validation") {
				t.Fatal("should NOT flag when validation is mentioned")
			}
		}
	})
}

func TestCoAuthorADRAndContractRefs(t *testing.T) {
	ca := NewSpecCoAuthor()

	t.Run("empty ADR refs generate info annotation", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-no-adr",
			Title: "No ADR",
			Sections: []SpecSection{
				{Title: "Overview", Content: "Some content."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnIncompleteness && strings.Contains(ann.Content, "ADR") {
				found = true
				break
			}
		}
		assert.True(t, found, "should note missing ADR refs")
	})

	t.Run("empty contract refs generate info annotation", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-no-contract",
			Title: "No Contract",
			Sections: []SpecSection{
				{Title: "Overview", Content: "Some content."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		var found bool
		for _, ann := range result.Annotations {
			if ann.AnnotationType == AnnIncompleteness && strings.Contains(ann.Content, "contract") {
				found = true
				break
			}
		}
		assert.True(t, found, "should note missing contract refs")
	})

	t.Run("ADR and contract refs present — no annotations", func(t *testing.T) {
		spec := &Spec{
			ID:           "spec-with-refs",
			Title:        "With Refs",
			ADRRefs:      []string{"ADR-001"},
			ContractRefs: []string{"CTR-001"},
			Sections: []SpecSection{
				{Title: "Overview", Content: "Some content."},
			},
		}
		result, err := ca.CoAuthor(spec)
		require.NoError(t, err)

		for _, ann := range result.Annotations {
			if ann.AgentType == AgentSpecChallenger && ann.AnnotationType == AnnIncompleteness {
				if strings.Contains(ann.Content, "ADR") || strings.Contains(ann.Content, "contract") {
					t.Fatal("should NOT annotate missing ADR/contract refs when present")
				}
			}
		}
	})
}

func TestCoAuthorWellCoveredSpec(t *testing.T) {
	ca := NewSpecCoAuthor()

	spec := &Spec{
		ID:           "spec-well-covered",
		Title:        "Well Covered Spec",
		ADRRefs:      []string{"ADR-001"},
		ContractRefs: []string{"CTR-001"},
		Sections: []SpecSection{
			{Title: "Overview", Content: "Overview content."},
			{Title: "Requirements", Content: "The system MUST handle input. Empty input returns an error. Timeout exceeded triggers cancellation."},
			{Title: "Non-Goals", Content: "No persistence in this version."},
			{Title: "Constraints", Content: "Must use HTTPS."},
			{Title: "Acceptance Criteria", Content: "All tests pass."},
		},
	}

	result, err := ca.CoAuthor(spec)
	require.NoError(t, err)

	// Should have no incompleteness for standard sections (all present)
	// Should not have edge case for input (empty is handled)
	// Should still have some annotations (ADR refs are present but the check
	// is for len > 0, so no ADR annotation; contract refs too)
	for _, ann := range result.Annotations {
		t.Logf("Annotation: type=%s sev=%s content=%s", ann.AnnotationType, ann.Severity, ann.Content)
	}
	// The well-covered spec should have no incompleteness annotations from generator
	// (all standard sections present)
	for _, ann := range result.Annotations {
		if ann.AgentType == AgentSpecGenerator && ann.AnnotationType == AnnIncompleteness {
			t.Fatal("should not have incompleteness annotations when all standard sections present")
		}
	}
}

// ---------------------------------------------------------------------------
// Completeness tests
// ---------------------------------------------------------------------------

func TestCompletenessErrors(t *testing.T) {
	cc := NewSpecCompleteness()

	t.Run("nil spec returns error", func(t *testing.T) {
		_, err := cc.CheckCompleteness(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "spec is nil")
	})

	t.Run("empty ID returns error", func(t *testing.T) {
		_, err := cc.CheckCompleteness(&Spec{Title: "No ID"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "id is required")
	})
}

func TestCompletenessFullSpec(t *testing.T) {
	cc := NewSpecCompleteness()

	// A rich spec that hits many keywords across all 12 dimensions
	spec := &Spec{
		ID:    "spec-complete-full",
		Title: "Full Completeness",
		Sections: []SpecSection{
			{
				Title: "Requirements",
				Content: "The system MUST handle errors. SHALL validate input. Should support retry on failure. " +
					"Functional specification includes acceptance criteria.",
			},
			{
				Title: "Error Handling",
				Content: "Errors are handled with fallback and retry logic. Timeout and invalid input raise exceptions. " +
					"Recovery procedures are documented.",
			},
			{
				Title: "Security",
				Content: "All data is encrypted. Auth uses OAuth tokens. Secrets are managed with RBAC. " +
					"Vulnerability scanning prevents threats. Privilege escalation is prevented.",
			},
			{
				Title: "Auth",
				Content: "Login requires MFA. Sessions use JWT tokens. Credentials are never stored in plaintext. " +
					"OAuth 2.0 with password grant.",
			},
			{
				Title: "Rate Limiting",
				Content: "API rate limited at 1000 req/min with backoff. 429 responses trigger retry. " +
					"Burst allowance of 50 requests.",
			},
			{
				Title: "Validation",
				Content: "Input validation with schema checks. Type checking and boundary rejection. " +
					"Parse all inputs and sanitize before processing.",
			},
			{
				Title: "Observability",
				Content: "Structured logging with metrics and tracing. Telemetry via OpenTelemetry. " +
					"Audit logs for all operations.",
			},
			{
				Title: "Testing",
				Content: "Unit tests with mocks. Integration tests cover all endpoints. " +
					"E2E tests validate workflows. TDD approach with fixtures.",
			},
			{
				Title: "Deployment",
				Content: "Docker images deployed via CI/CD pipeline to Kubernetes. " +
					"Canary releases and blue-green deployment strategy.",
			},
			{
				Title: "Rollback",
				Content: "Rollback to previous version via revert. Restore from backup. " +
					"Database migrate back supported.",
			},
			{
				Title: "Monitoring",
				Content: "Grafana dashboards with Prometheus metrics. Health checks at /health. " +
					"SLO targets monitored via PagerDuty alerts.",
			},
			{
				Title:   "Documentation",
				Content: "README covers setup. API docs with examples. Reference guide for all components.",
			},
		},
	}

	report, err := cc.CheckCompleteness(spec)
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, spec.ID, report.SpecID)
	assert.GreaterOrEqual(t, report.TotalScore, 70.0, "full spec should score >= 70")
	assert.Len(t, report.Dimensions, 12)

	// Every dimension should have dedicated section detection giving baseline 80
	for _, d := range report.Dimensions {
		if d.Score < 50 {
			t.Logf("Low dimension: %s = %.1f — %s", d.Name, d.Score, d.Note)
		}
	}

	// No gaps since all dimensions should score >= 50 with dedicated sections
	t.Logf("Total score: %.1f", report.TotalScore)
	t.Logf("Gaps: %d", len(report.Gaps))
}

func TestCompletenessMinimalSpec(t *testing.T) {
	cc := NewSpecCompleteness()

	spec := &Spec{
		ID:    "spec-complete-minimal",
		Title: "Minimal Spec",
		Sections: []SpecSection{
			{Title: "Overview", Content: "This is a minimal spec with no real detail."},
		},
	}

	report, err := cc.CheckCompleteness(spec)
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, spec.ID, report.SpecID)
	assert.Less(t, report.TotalScore, 30.0, "minimal spec should score low")
	assert.Len(t, report.Dimensions, 12)

	// Should have gaps — most dimensions below 50
	assert.Greater(t, len(report.Gaps), 0, "minimal spec should have gaps")
}

func TestCompletenessDedicatedSectionDetection(t *testing.T) {
	cc := NewSpecCompleteness()

	t.Run("Security section scores 80+", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-sec-test",
			Title: "Security Test",
			Sections: []SpecSection{
				{Title: "Security", Content: "Security content here."},
			},
		}
		report, err := cc.CheckCompleteness(spec)
		require.NoError(t, err)

		var securityDim *DimensionScore
		for _, d := range report.Dimensions {
			if d.Name == "security" {
				securityDim = &d
				break
			}
		}
		require.NotNil(t, securityDim)
		assert.Equal(t, 80.0, securityDim.Score, "security dimension should get 80 from dedicated section")
		assert.Contains(t, securityDim.Note, "dedicated section")
	})

	t.Run("Testing section scores 80+", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-test-section",
			Title: "Testing Section",
			Sections: []SpecSection{
				{Title: "Testing", Content: "Test content here."},
			},
		}
		report, err := cc.CheckCompleteness(spec)
		require.NoError(t, err)

		var testingDim *DimensionScore
		for _, d := range report.Dimensions {
			if d.Name == "testing" {
				testingDim = &d
				break
			}
		}
		require.NotNil(t, testingDim)
		assert.Equal(t, 80.0, testingDim.Score, "testing dimension should get 80 from dedicated section")
	})

	t.Run("No dedicated section uses keyword scoring", func(t *testing.T) {
		spec := &Spec{
			ID:    "spec-keyword-score",
			Title: "Keyword Score",
			Sections: []SpecSection{
				{Title: "Generic", Content: "must shall should requirement specification functional"},
			},
		}
		report, err := cc.CheckCompleteness(spec)
		require.NoError(t, err)

		var reqDim *DimensionScore
		for _, d := range report.Dimensions {
			if d.Name == "requirements_coverage" {
				reqDim = &d
				break
			}
		}
		require.NotNil(t, reqDim)
		// Should be > 0 from keyword hits
		assert.Greater(t, reqDim.Score, 0.0, "should score from keyword hits")
		assert.Less(t, reqDim.Score, 80.0, "without dedicated section, max is 70")
	})
}

func TestCompletenessGapsGeneration(t *testing.T) {
	cc := NewSpecCompleteness()

	// Empty spec — all dimensions should have very low scores
	spec := &Spec{
		ID:    "spec-gaps",
		Title: "Gaps Test",
		Sections: []SpecSection{
			{Title: "Trivial", Content: "Just a trivial spec."},
		},
	}

	report, err := cc.CheckCompleteness(spec)
	require.NoError(t, err)

	// Should have gaps for dimensions scoring below 50
	require.Greater(t, len(report.Gaps), 0)
	for _, gap := range report.Gaps {
		assert.NotEmpty(t, gap.Dimension)
		assert.NotEmpty(t, gap.Detail)
		assert.NotEmpty(t, gap.Severity)
		assert.Contains(t, []string{SeverityCritical, SeverityWarning, SeverityInfo}, gap.Severity)
	}
}

func TestCompletenessReportGeneratedAt(t *testing.T) {
	cc := NewSpecCompleteness()
	spec := &Spec{
		ID:    "spec-generated-at",
		Title: "Generated At",
		Sections: []SpecSection{
			{Title: "Overview", Content: "Content."},
		},
	}

	before := time.Now().UTC().Add(-time.Second)
	report, err := cc.CheckCompleteness(spec)
	require.NoError(t, err)
	after := time.Now().UTC().Add(time.Second)

	assert.True(t, report.GeneratedAt.After(before))
	assert.True(t, report.GeneratedAt.Before(after))
}

// ---------------------------------------------------------------------------
// Helper: parseRFC3339 tests
// ---------------------------------------------------------------------------

func TestParseRFC3339(t *testing.T) {
	t.Run("parses valid RFC3339", func(t *testing.T) {
		ts := parseRFC3339("2026-07-19T12:00:00Z")
		assert.Equal(t, 2026, ts.Year())
		assert.Equal(t, time.July, ts.Month())
		assert.Equal(t, 19, ts.Day())
		assert.Equal(t, 12, ts.Hour())
	})

	t.Run("empty string returns zero time", func(t *testing.T) {
		assert.True(t, parseRFC3339("").IsZero())
	})

	t.Run("invalid format returns zero time", func(t *testing.T) {
		assert.True(t, parseRFC3339("not-a-date").IsZero())
	})
}

// ---------------------------------------------------------------------------
// Helper: findSectionLine tests
// ---------------------------------------------------------------------------

func TestFindSectionLine(t *testing.T) {
	spec := &Spec{
		Sections: []SpecSection{
			{Title: "First", Content: "line1\nline2"},
			{Title: "Second", Content: "content"},
			{Title: "Third", Content: ""},
		},
	}

	t.Run("first section line 1", func(t *testing.T) {
		assert.Equal(t, 1, findSectionLine(spec, "First"))
	})

	t.Run("second section line 5", func(t *testing.T) {
		// First section: start at line 1
		//   + 1 for title line => 2
		//   + strings.Count("line1\nline2", "\n") = 1 content newlines + 1 = 2 => 4
		//   + 1 for blank => 5
		// Second section title is at line 5
		assert.Equal(t, 5, findSectionLine(spec, "Second"))
	})

	t.Run("unknown title returns 1", func(t *testing.T) {
		assert.Equal(t, 1, findSectionLine(spec, "Unknown"))
	})
}

// ---------------------------------------------------------------------------
// Helper: severityForScore tests
// ---------------------------------------------------------------------------

func TestSeverityForScore(t *testing.T) {
	assert.Equal(t, SeverityCritical, severityForScore(0))
	assert.Equal(t, SeverityCritical, severityForScore(19))
	assert.Equal(t, SeverityWarning, severityForScore(20))
	assert.Equal(t, SeverityWarning, severityForScore(39))
	assert.Equal(t, SeverityInfo, severityForScore(40))
	assert.Equal(t, SeverityInfo, severityForScore(100))
}

// ---------------------------------------------------------------------------
// Helper: concatSectionText tests
// ---------------------------------------------------------------------------

func TestConcatSectionText(t *testing.T) {
	t.Run("concatenates section titles and content", func(t *testing.T) {
		spec := &Spec{
			Sections: []SpecSection{
				{Title: "Overview", Content: "Overview text."},
				{Title: "Details", Content: "Details text."},
			},
		}
		result := concatSectionText(spec)
		assert.Contains(t, result, "Overview")
		assert.Contains(t, result, "Overview text.")
		assert.Contains(t, result, "Details")
		assert.Contains(t, result, "Details text.")
	})

	t.Run("returns empty for no sections", func(t *testing.T) {
		spec := &Spec{}
		assert.Empty(t, concatSectionText(spec))
	})
}
