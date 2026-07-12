// Package adr implements Architecture Decision Records with co-authoring
// and multi-model review (Phase 2 §2.2). ADRs follow the MADR format and
// are evidence-linked to specs, incidents, and marketplace patterns.
package adr

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ADR status constants.
const (
	StatusProposed   = "proposed"
	StatusAccepted   = "accepted"
	StatusDeprecated = "deprecated"
	StatusSuperseded = "superseded"
)

// Evidence link type constants.
const (
	EvidenceSpecRef            = "spec_ref"
	EvidenceIncidentRef        = "incident_ref"
	EvidenceMarketplacePattern = "marketplace_pattern"
)

// DefaultADRsDir is the default directory under the user home for ADRs.
const DefaultADRsDir = ".helix/adrs"

// ADR is an Architecture Decision Record (MADR-style).
type ADR struct {
	ID            string         `json:"id" yaml:"id"`
	Number        int            `json:"number" yaml:"number"`
	Slug          string         `json:"slug" yaml:"slug"`
	Title         string         `json:"title" yaml:"title"`
	Status        string         `json:"status" yaml:"status"`
	Context       string         `json:"context" yaml:"context"`
	Decision      string         `json:"decision" yaml:"decision"`
	Alternatives  []Alternative  `json:"alternatives,omitempty" yaml:"alternatives,omitempty"`
	Consequences  string         `json:"consequences" yaml:"consequences"`
	EvidenceLinks []EvidenceLink `json:"evidence_links,omitempty" yaml:"evidence_links,omitempty"`
	ReviewScore   float64        `json:"review_score,omitempty" yaml:"review_score,omitempty"`
	Authors       []string       `json:"authors,omitempty" yaml:"authors,omitempty"`
	Supersedes    string         `json:"supersedes,omitempty" yaml:"supersedes,omitempty"`
	SupersededBy  string         `json:"superseded_by,omitempty" yaml:"superseded_by,omitempty"`
	RiskScore     float64        `json:"risk_score,omitempty" yaml:"risk_score,omitempty"`
	BlastRadius   string         `json:"blast_radius,omitempty" yaml:"blast_radius,omitempty"`
	CreatedAt     time.Time      `json:"created_at" yaml:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at" yaml:"updated_at"`
}

// Alternative is a considered option with tradeoff analysis.
type Alternative struct {
	Description     string `json:"description" yaml:"description"`
	Tradeoffs       string `json:"tradeoffs" yaml:"tradeoffs"`
	RejectedBecause string `json:"rejected_because,omitempty" yaml:"rejected_because,omitempty"`
}

// EvidenceLink cites a spec section, incident, or marketplace pattern.
type EvidenceLink struct {
	Type               string `json:"type" yaml:"type"` // spec_ref | incident_ref | marketplace_pattern
	SpecRef            string `json:"spec_ref,omitempty" yaml:"spec_ref,omitempty"`
	IncidentRef        string `json:"incident_ref,omitempty" yaml:"incident_ref,omitempty"`
	MarketplacePattern string `json:"marketplace_pattern,omitempty" yaml:"marketplace_pattern,omitempty"`
	Description        string `json:"description,omitempty" yaml:"description,omitempty"`
}

// ADRReviewRequest is the input for multi-model ADR review.
type ADRReviewRequest struct {
	ADR                ADR      `json:"adr"`
	Models             []string `json:"models"`
	ConsensusThreshold float64  `json:"consensus_threshold"`
}

// ModelVerdict is one model's independent assessment of an ADR.
type ModelVerdict struct {
	Model       string   `json:"model"`
	Verdict     string   `json:"verdict"` // approve | warn | reject
	Score       float64  `json:"score"`   // 0–1 agreement with decision
	Rationale   string   `json:"rationale"`
	Concerns    []string `json:"concerns,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
}

// ConflictingAssessment surfaces divergent model opinions.
type ConflictingAssessment struct {
	Topic     string            `json:"topic"`
	Positions map[string]string `json:"positions"` // model → stance
	Rationale string            `json:"rationale"`
}

// ADRReviewResult aggregates multi-model review of an ADR.
type ADRReviewResult struct {
	ADRID                  string                  `json:"adr_id"`
	ModelVerdicts          []ModelVerdict          `json:"model_verdicts"`
	ConsensusScore         float64                 `json:"consensus_score"` // 0–1
	ConsensusThreshold     float64                 `json:"consensus_threshold"`
	Passed                 bool                    `json:"passed"`
	ConflictingAssessments []ConflictingAssessment `json:"conflicting_assessments,omitempty"`
	SuggestedAlternatives  []string                `json:"suggested_alternatives,omitempty"`
	Summary                string                  `json:"summary"`
	ReviewedAt             time.Time               `json:"reviewed_at"`
}

// NewADRID returns a UUID v4 string (RFC 4122).
func NewADRID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to timestamp-based pseudo-id.
		return fmt.Sprintf("00000000-0000-4000-8000-%012x", time.Now().UnixNano()&0xffffffffffff)
	}
	// Set version (4) and variant (RFC 4122).
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// StatusDisplay returns a human-readable label for an ADR status.
func StatusDisplay(status string) string {
	switch status {
	case StatusProposed:
		return "Proposed"
	case StatusAccepted:
		return "Accepted"
	case StatusDeprecated:
		return "Deprecated"
	case StatusSuperseded:
		return "Superseded"
	default:
		if status == "" {
			return "Unknown"
		}
		return strings.ToUpper(status[:1]) + status[1:]
	}
}

// ValidStatus reports whether s is a known ADR status.
func ValidStatus(s string) bool {
	switch s {
	case StatusProposed, StatusAccepted, StatusDeprecated, StatusSuperseded:
		return true
	default:
		return false
	}
}

// ValidTransition reports whether moving from → to is allowed.
func ValidTransition(from, to string) bool {
	if !ValidStatus(from) || !ValidStatus(to) {
		return false
	}
	if from == to {
		return true
	}
	switch from {
	case StatusProposed:
		return to == StatusAccepted || to == StatusDeprecated || to == StatusSuperseded
	case StatusAccepted:
		return to == StatusDeprecated || to == StatusSuperseded
	case StatusDeprecated:
		return to == StatusSuperseded
	case StatusSuperseded:
		return false
	default:
		return false
	}
}

// HasEvidence reports whether the ADR has at least one evidence link.
func (a *ADR) HasEvidence() bool {
	return a != nil && len(a.EvidenceLinks) > 0
}

// Filename returns the on-disk basename: <NNNN>-<slug>.md
func (a *ADR) Filename() string {
	n := a.Number
	if n <= 0 {
		n = 1
	}
	slug := a.Slug
	if slug == "" {
		slug = Slugify(a.Title)
	}
	if slug == "" {
		slug = "untitled"
	}
	return fmt.Sprintf("%04d-%s.md", n, slug)
}

// Slugify converts a title into a filesystem-safe slug.
func Slugify(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	// Replace non-alphanumeric with hyphens.
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
		s = strings.Trim(s, "-")
	}
	return s
}

// ---------------------------------------------------------------------------
// ADRStore — YAML-frontmatter markdown at ~/.helix/adrs/<NNNN>-<slug>.md
// ---------------------------------------------------------------------------

// ADRStore persists ADRs as markdown files with YAML frontmatter.
type ADRStore struct {
	root string
}

// NewADRStore creates a store rooted at root. Empty root resolves to
// ~/.helix/adrs via os.UserHomeDir.
func NewADRStore(root string) (*ADRStore, error) {
	expanded, err := resolveStoreRoot(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(expanded, 0o755); err != nil {
		return nil, fmt.Errorf("adr: mkdir %s: %w", expanded, err)
	}
	return &ADRStore{root: expanded}, nil
}

// Root returns the absolute store root directory.
func (s *ADRStore) Root() string { return s.root }

// Save writes the ADR as a markdown file with YAML frontmatter.
func (s *ADRStore) Save(a *ADR) error {
	if a == nil {
		return fmt.Errorf("adr: adr is nil")
	}
	if a.ID == "" {
		return fmt.Errorf("adr: id is required")
	}
	if strings.TrimSpace(a.Title) == "" {
		return fmt.Errorf("adr: title is required")
	}
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	if a.Status == "" {
		a.Status = StatusProposed
	}
	if a.Slug == "" {
		a.Slug = Slugify(a.Title)
	}
	if a.Number <= 0 {
		n, err := s.nextNumber()
		if err != nil {
			return err
		}
		a.Number = n
	}

	// Remove any previous file for this ID (number/slug may have changed).
	if old, err := s.findPathByID(a.ID); err == nil && old != "" {
		_ = os.Remove(old)
	}

	content := adrToMarkdown(a)
	path := filepath.Join(s.root, a.Filename())
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("adr: write %s: %w", path, err)
	}
	return nil
}

// Load reads an ADR by ID (UUID), number string ("0001"), or filename stem.
func (s *ADRStore) Load(idOrRef string) (*ADR, error) {
	if idOrRef == "" {
		return nil, fmt.Errorf("adr: id is required")
	}
	path, err := s.resolvePath(idOrRef)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("adr: load %s: %w", idOrRef, err)
	}
	a, err := markdownToADR(raw)
	if err != nil {
		return nil, fmt.Errorf("adr: parse %s: %w", idOrRef, err)
	}
	return a, nil
}

// List returns all ADRs sorted by Number ascending.
func (s *ADRStore) List() ([]ADR, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("adr: list %s: %w", s.root, err)
	}
	var adrs []ADR
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.root, entry.Name()))
		if err != nil {
			continue
		}
		a, err := markdownToADR(raw)
		if err != nil {
			continue
		}
		adrs = append(adrs, *a)
	}
	sort.Slice(adrs, func(i, j int) bool {
		return adrs[i].Number < adrs[j].Number
	})
	return adrs, nil
}

// Supersede marks oldID as superseded by newADR, links lineage, and saves both.
// newADR may already exist (partial) or be a newly constructed ADR.
func (s *ADRStore) Supersede(oldID string, newADR *ADR) (*ADR, error) {
	old, err := s.Load(oldID)
	if err != nil {
		return nil, err
	}
	if newADR == nil {
		return nil, fmt.Errorf("adr: new adr is nil")
	}
	if newADR.ID == "" {
		newADR.ID = NewADRID()
	}
	if newADR.Title == "" {
		return nil, fmt.Errorf("adr: new adr title is required")
	}
	if !ValidTransition(old.Status, StatusSuperseded) && old.Status != StatusSuperseded {
		// Allow force supersede of any non-empty status except already linked cycle.
		if old.Status == "" {
			old.Status = StatusProposed
		}
	}

	newADR.Supersedes = old.ID
	if newADR.Status == "" {
		newADR.Status = StatusProposed
	}
	if newADR.Slug == "" {
		newADR.Slug = Slugify(newADR.Title)
	}
	if newADR.Number <= 0 {
		n, err := s.nextNumber()
		if err != nil {
			return nil, err
		}
		newADR.Number = n
	}

	// Ensure evidence link back to superseded ADR.
	hasLink := false
	for _, e := range newADR.EvidenceLinks {
		if e.SpecRef == old.ID || e.Description == "supersedes "+old.ID {
			hasLink = true
			break
		}
	}
	if !hasLink {
		newADR.EvidenceLinks = append(newADR.EvidenceLinks, EvidenceLink{
			Type:        EvidenceSpecRef,
			SpecRef:     "adr:" + old.ID,
			Description: fmt.Sprintf("Supersedes ADR %04d (%s)", old.Number, old.Title),
		})
	}

	if err := s.Save(newADR); err != nil {
		return nil, err
	}

	old.Status = StatusSuperseded
	old.SupersededBy = newADR.ID
	if err := s.Save(old); err != nil {
		return nil, err
	}
	return newADR, nil
}

func (s *ADRStore) nextNumber() (int, error) {
	adrs, err := s.List()
	if err != nil {
		return 0, err
	}
	maxN := 0
	for _, a := range adrs {
		if a.Number > maxN {
			maxN = a.Number
		}
	}
	return maxN + 1, nil
}

func (s *ADRStore) resolvePath(idOrRef string) (string, error) {
	// Direct filename.
	if strings.HasSuffix(idOrRef, ".md") {
		p := filepath.Join(s.root, filepath.Base(idOrRef))
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	// Stem match: 0001-slug
	candidate := filepath.Join(s.root, idOrRef+".md")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	// By UUID or number prefix.
	if p, err := s.findPathByID(idOrRef); err == nil && p != "" {
		return p, nil
	}
	// Number only (e.g. "1" or "0001").
	if n, err := strconv.Atoi(strings.TrimLeft(idOrRef, "0")); err == nil || idOrRef == "0" || idOrRef == "0000" {
		_ = n
		entries, err := os.ReadDir(s.root)
		if err != nil {
			return "", fmt.Errorf("adr: list %s: %w", s.root, err)
		}
		prefix := ""
		if num, err := strconv.Atoi(idOrRef); err == nil {
			prefix = fmt.Sprintf("%04d-", num)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".md") {
				return filepath.Join(s.root, e.Name()), nil
			}
		}
	}
	return "", fmt.Errorf("adr: not found: %s", idOrRef)
}

func (s *ADRStore) findPathByID(id string) (string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(s.root, e.Name())
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		a, err := markdownToADR(raw)
		if err != nil {
			continue
		}
		if a.ID == id {
			return p, nil
		}
	}
	return "", fmt.Errorf("adr: not found: %s", id)
}

// ---------------------------------------------------------------------------
// Markdown serialization
// ---------------------------------------------------------------------------

type adrFrontmatter struct {
	ID            string         `yaml:"id"`
	Number        int            `yaml:"number"`
	Slug          string         `yaml:"slug"`
	Title         string         `yaml:"title"`
	Status        string         `yaml:"status"`
	Authors       []string       `yaml:"authors,omitempty"`
	EvidenceLinks []EvidenceLink `yaml:"evidence_links,omitempty"`
	ReviewScore   float64        `yaml:"review_score,omitempty"`
	RiskScore     float64        `yaml:"risk_score,omitempty"`
	BlastRadius   string         `yaml:"blast_radius,omitempty"`
	Supersedes    string         `yaml:"supersedes,omitempty"`
	SupersededBy  string         `yaml:"superseded_by,omitempty"`
	CreatedAt     string         `yaml:"created_at"`
	UpdatedAt     string         `yaml:"updated_at"`
}

func adrToMarkdown(a *ADR) string {
	var b strings.Builder

	fm := adrFrontmatter{
		ID:            a.ID,
		Number:        a.Number,
		Slug:          a.Slug,
		Title:         a.Title,
		Status:        a.Status,
		Authors:       a.Authors,
		EvidenceLinks: a.EvidenceLinks,
		ReviewScore:   a.ReviewScore,
		RiskScore:     a.RiskScore,
		BlastRadius:   a.BlastRadius,
		Supersedes:    a.Supersedes,
		SupersededBy:  a.SupersededBy,
		CreatedAt:     a.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     a.UpdatedAt.Format(time.RFC3339),
	}
	fmData, _ := yaml.Marshal(fm)

	b.WriteString("---\n")
	b.Write(fmData)
	b.WriteString("---\n\n")
	b.WriteString("# ")
	b.WriteString(a.Title)
	b.WriteString("\n\n")

	writeSection(&b, "Context", a.Context)
	writeSection(&b, "Decision", a.Decision)

	if len(a.Alternatives) > 0 {
		b.WriteString("## Alternatives\n\n")
		for i, alt := range a.Alternatives {
			b.WriteString(fmt.Sprintf("### Alternative %d\n\n", i+1))
			b.WriteString(strings.TrimSpace(alt.Description))
			b.WriteString("\n\n")
			if alt.Tradeoffs != "" {
				b.WriteString("**Tradeoffs:** ")
				b.WriteString(strings.TrimSpace(alt.Tradeoffs))
				b.WriteString("\n\n")
			}
			if alt.RejectedBecause != "" {
				b.WriteString("**Rejected because:** ")
				b.WriteString(strings.TrimSpace(alt.RejectedBecause))
				b.WriteString("\n\n")
			}
		}
	}

	writeSection(&b, "Consequences", a.Consequences)
	return b.String()
}

func writeSection(b *strings.Builder, title, content string) {
	b.WriteString("## ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n\n")
}

func markdownToADR(raw []byte) (*ADR, error) {
	text := string(raw)
	if !strings.HasPrefix(text, "---\n") {
		return nil, fmt.Errorf("missing YAML frontmatter")
	}
	rest := text[4:]
	endIdx := strings.Index(rest, "\n---\n")
	if endIdx < 0 {
		endIdx = strings.Index(rest, "\n---")
		if endIdx < 0 {
			return nil, fmt.Errorf("unterminated YAML frontmatter")
		}
	}
	fmText := rest[:endIdx]
	body := ""
	if endIdx+4 < len(rest) {
		cutAt := endIdx + 4
		if cutAt < len(rest) && rest[cutAt] == '\n' {
			cutAt++
		}
		body = rest[cutAt:]
	}

	var fm adrFrontmatter
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	a := &ADR{
		ID:            fm.ID,
		Number:        fm.Number,
		Slug:          fm.Slug,
		Title:         fm.Title,
		Status:        fm.Status,
		Authors:       fm.Authors,
		EvidenceLinks: fm.EvidenceLinks,
		ReviewScore:   fm.ReviewScore,
		RiskScore:     fm.RiskScore,
		BlastRadius:   fm.BlastRadius,
		Supersedes:    fm.Supersedes,
		SupersededBy:  fm.SupersededBy,
		CreatedAt:     parseRFC3339(fm.CreatedAt),
		UpdatedAt:     parseRFC3339(fm.UpdatedAt),
	}

	// Parse body sections.
	lines := strings.Split(body, "\n")
	var current string
	var content strings.Builder
	var alts []Alternative
	var currentAlt *Alternative

	flush := func() {
		if currentAlt != nil {
			currentAlt.Description = strings.TrimSpace(content.String())
			alts = append(alts, *currentAlt)
			currentAlt = nil
			content.Reset()
			return
		}
		if current == "" {
			content.Reset()
			return
		}
		text := strings.TrimSpace(content.String())
		switch current {
		case "Context":
			a.Context = text
		case "Decision":
			a.Decision = text
		case "Consequences":
			a.Consequences = text
		case "Alternatives":
			// no-op; sub-alts handled separately
		}
		content.Reset()
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			// Document title — already in frontmatter.
			continue
		}
		if strings.HasPrefix(trimmed, "### Alternative") {
			if current == "Alternatives" || currentAlt != nil {
				if currentAlt != nil {
					currentAlt.Description = strings.TrimSpace(content.String())
					alts = append(alts, *currentAlt)
					content.Reset()
				}
				currentAlt = &Alternative{}
				current = "Alternatives"
				continue
			}
		}
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			current = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			currentAlt = nil
			continue
		}
		if currentAlt != nil {
			if strings.HasPrefix(trimmed, "**Tradeoffs:**") {
				currentAlt.Tradeoffs = strings.TrimSpace(strings.TrimPrefix(trimmed, "**Tradeoffs:**"))
				continue
			}
			if strings.HasPrefix(trimmed, "**Rejected because:**") {
				currentAlt.RejectedBecause = strings.TrimSpace(strings.TrimPrefix(trimmed, "**Rejected because:**"))
				continue
			}
		}
		content.WriteString(line)
		content.WriteString("\n")
	}
	flush()
	if len(alts) > 0 {
		a.Alternatives = alts
	}
	if a.Status == "" {
		a.Status = StatusProposed
	}
	return a, nil
}

func parseRFC3339(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func resolveStoreRoot(root string) (string, error) {
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("adr: home dir: %w", err)
		}
		return filepath.Join(home, DefaultADRsDir), nil
	}
	if strings.HasPrefix(root, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("adr: home dir: %w", err)
		}
		return filepath.Join(home, root[2:]), nil
	}
	return filepath.Abs(root)
}
