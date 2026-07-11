package spec

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultSpecsDir is the default directory under the user home for specs.
const DefaultSpecsDir = ".helix/specs"

// SpecStore persists specs as markdown files with YAML frontmatter at
// <root>/<spec-id>.md.
type SpecStore struct {
	root string
}

// NewSpecStore creates a store rooted at root. Empty root resolves to
// ~/.helix/specs via os.UserHomeDir.
func NewSpecStore(root string) (*SpecStore, error) {
	expanded, err := resolveStoreRoot(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(expanded, 0o755); err != nil {
		return nil, fmt.Errorf("spec: mkdir %s: %w", expanded, err)
	}
	return &SpecStore{root: expanded}, nil
}

// Root returns the absolute store root directory.
func (s *SpecStore) Root() string { return s.root }

// NewSpecID returns a random hex spec ID prefixed with "spec-".
func NewSpecID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("spec-%x", time.Now().UnixNano())
	}
	return "spec-" + hex.EncodeToString(b)
}

// Save writes the spec as a markdown file with YAML frontmatter.
func (s *SpecStore) Save(spec *Spec) error {
	if spec == nil {
		return fmt.Errorf("spec: spec is nil")
	}
	if spec.ID == "" {
		return fmt.Errorf("spec: id is required")
	}
	if strings.TrimSpace(spec.Title) == "" {
		return fmt.Errorf("spec: title is required")
	}
	now := time.Now().UTC()
	if spec.CreatedAt.IsZero() {
		spec.CreatedAt = now
	}
	spec.UpdatedAt = now
	if spec.Status == "" {
		spec.Status = StatusDraft
	}

	content := specToMarkdown(spec)
	path := filepath.Join(s.root, spec.ID+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("spec: write %s: %w", path, err)
	}
	return nil
}

// Load reads a spec by ID from disk.
func (s *SpecStore) Load(id string) (*Spec, error) {
	if id == "" {
		return nil, fmt.Errorf("spec: id is required")
	}
	path := filepath.Join(s.root, id+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("spec: load %s: %w", id, err)
	}
	spec, err := markdownToSpec(raw)
	if err != nil {
		return nil, fmt.Errorf("spec: parse %s: %w", id, err)
	}
	return spec, nil
}

// List returns all specs sorted by UpdatedAt descending.
func (s *SpecStore) List() ([]Spec, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("spec: list %s: %w", s.root, err)
	}
	var specs []Spec
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.root, entry.Name()))
		if err != nil {
			continue
		}
		spec, err := markdownToSpec(raw)
		if err != nil {
			continue
		}
		specs = append(specs, *spec)
	}
	sort.Slice(specs, func(i, j int) bool {
		return specs[i].UpdatedAt.After(specs[j].UpdatedAt)
	})
	return specs, nil
}

// ---------------------------------------------------------------------------
// Markdown serialization
// ---------------------------------------------------------------------------

type frontmatter struct {
	ID           string   `yaml:"id"`
	IdeaRef      string   `yaml:"idea_ref,omitempty"`
	Title        string   `yaml:"title"`
	Status       string   `yaml:"status"`
	ADRRefs      []string `yaml:"adr_refs,omitempty"`
	ContractRefs []string `yaml:"contract_refs,omitempty"`
	Hash         string   `yaml:"hash,omitempty"`
	CreatedAt    string   `yaml:"created_at"`
	UpdatedAt    string   `yaml:"updated_at"`
}

func specToMarkdown(spec *Spec) string {
	var b strings.Builder

	fm := frontmatter{
		ID:           spec.ID,
		IdeaRef:      spec.IdeaRef,
		Title:        spec.Title,
		Status:       spec.Status,
		ADRRefs:      spec.ADRRefs,
		ContractRefs: spec.ContractRefs,
		Hash:         spec.Hash,
		CreatedAt:    spec.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    spec.UpdatedAt.Format(time.RFC3339),
	}

	fmData, _ := yaml.Marshal(fm)

	b.WriteString("---\n")
	b.Write(fmData)
	b.WriteString("---\n\n")

	for _, sec := range spec.Sections {
		b.WriteString("## ")
		b.WriteString(sec.Title)
		b.WriteString("\n")
		// Encode approval status as an HTML comment so it round-trips.
		if sec.ApprovalStatus != "" {
			if sec.ApprovedBy != "" {
				b.WriteString(fmt.Sprintf("<!-- approval: %s by %s -->\n", sec.ApprovalStatus, sec.ApprovedBy))
			} else {
				b.WriteString(fmt.Sprintf("<!-- approval: %s -->\n", sec.ApprovalStatus))
			}
		}
		b.WriteString(strings.TrimSpace(sec.Content))
		b.WriteString("\n\n")
	}

	if len(spec.Annotations) > 0 {
		b.WriteString("<!-- annotations-start\n")
		for _, ann := range spec.Annotations {
			b.WriteString(annotationToYAMLInline(ann))
			b.WriteString("\n")
		}
		b.WriteString("annotations-end -->\n")
	}

	return b.String()
}

func markdownToSpec(raw []byte) (*Spec, error) {
	text := string(raw)

	// Parse frontmatter delimited by --- ... ---.
	if !strings.HasPrefix(text, "---\n") {
		return nil, fmt.Errorf("missing YAML frontmatter")
	}
	rest := text[4:]
	endIdx := strings.Index(rest, "\n---\n")
	if endIdx < 0 {
		// Maybe frontmatter ends at the last --- with no trailing newline.
		endIdx = strings.Index(rest, "\n---")
		if endIdx < 0 {
			return nil, fmt.Errorf("unterminated YAML frontmatter")
		}
	}
	fmText := rest[:endIdx]
	body := ""
	if endIdx+4 < len(rest) {
		// +4 for "\n---", +1 for the newline after it
		cutAt := endIdx + 4
		if cutAt < len(rest) && rest[cutAt] == '\n' {
			cutAt++
		}
		body = rest[cutAt:]
	}

	var fm frontmatter
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	spec := &Spec{
		ID:           fm.ID,
		IdeaRef:      fm.IdeaRef,
		Title:        fm.Title,
		Status:       fm.Status,
		ADRRefs:      fm.ADRRefs,
		ContractRefs: fm.ContractRefs,
		Hash:         fm.Hash,
	}
	spec.CreatedAt = parseRFC3339(fm.CreatedAt)
	spec.UpdatedAt = parseRFC3339(fm.UpdatedAt)

	// Parse sections and annotations from body.
	lines := strings.Split(body, "\n")
	var currentSection *SpecSection
	var sectionContent strings.Builder

	flushSection := func() {
		if currentSection != nil {
			currentSection.Content = strings.TrimSpace(sectionContent.String())
			spec.Sections = append(spec.Sections, *currentSection)
			currentSection = nil
			sectionContent.Reset()
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for annotation block.
		if trimmed == "<!-- annotations-start" {
			flushSection()
			continue
		}
		if trimmed == "annotations-end -->" {
			continue
		}
		if strings.HasPrefix(trimmed, "<!-- ann:") {
			ann := parseAnnotationYAMLInline(trimmed)
			if ann != nil {
				spec.Annotations = append(spec.Annotations, *ann)
			}
			continue
		}

		// Section heading.
		if strings.HasPrefix(trimmed, "## ") {
			flushSection()
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			currentSection = &SpecSection{Title: title}
			sectionContent.Reset()
			continue
		}

		// Approval comment.
		if strings.HasPrefix(trimmed, "<!-- approval:") {
			apStr := strings.TrimPrefix(trimmed, "<!-- approval:")
			apStr = strings.TrimSuffix(apStr, "-->")
			apStr = strings.TrimSpace(apStr)
			parts := strings.SplitN(apStr, " by ", 2)
			status := strings.TrimSpace(parts[0])
			approvedBy := ""
			if len(parts) == 2 {
				approvedBy = strings.TrimSpace(parts[1])
			}
			if currentSection != nil {
				currentSection.ApprovalStatus = status
				currentSection.ApprovedBy = approvedBy
			}
			continue
		}

		if currentSection != nil {
			sectionContent.WriteString(line)
			sectionContent.WriteString("\n")
		}
	}
	flushSection()

	if spec.Status == "" {
		spec.Status = StatusDraft
	}
	return spec, nil
}

// annotationToYAMLInline serializes an annotation as a single-line HTML comment
// that can be parsed back. We use a simple pipe-delimited format inside the
// comment to avoid YAML multi-line complexity.
func annotationToYAMLInline(a SpecAnnotation) string {
	return fmt.Sprintf("<!-- ann: line=%d | agent=%s | type=%s | sev=%s | status=%s | created=%s | %s -->",
		a.Line,
		a.AgentType,
		a.AnnotationType,
		a.Severity,
		a.Status,
		a.CreatedAt.Format(time.RFC3339),
		escapePipes(a.Content),
	)
}

func parseAnnotationYAMLInline(line string) *SpecAnnotation {
	// Strip <!-- ann: prefix and --> suffix.
	s := strings.TrimPrefix(line, "<!-- ann:")
	s = strings.TrimSuffix(s, "-->")
	s = strings.TrimSpace(s)

	// Split into metadata part and content part on " | " before the last pipe-separated content.
	// Format: key=val | key=val | ... | content
	parts := strings.Split(s, " | ")
	if len(parts) < 2 {
		return nil
	}

	ann := &SpecAnnotation{}
	contentParts := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if idx := strings.Index(p, "="); idx > 0 {
			key := strings.TrimSpace(p[:idx])
			val := strings.TrimSpace(p[idx+1:])
			switch key {
			case "line":
				var lineInt int
				if _, err := fmt.Sscanf(val, "%d", &lineInt); err == nil {
					ann.Line = lineInt
				}
			case "agent":
				ann.AgentType = val
			case "type":
				ann.AnnotationType = val
			case "sev":
				ann.Severity = val
			case "status":
				ann.Status = val
			case "created":
				ann.CreatedAt = parseRFC3339(val)
			default:
				contentParts = append(contentParts, p)
			}
		} else {
			contentParts = append(contentParts, p)
		}
	}
	ann.Content = unescapePipes(strings.Join(contentParts, " | "))
	return ann
}

func escapePipes(s string) string {
	return strings.ReplaceAll(s, "|", "&#124;")
}

func unescapePipes(s string) string {
	return strings.ReplaceAll(s, "&#124;", "|")
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
			return "", fmt.Errorf("spec: home dir: %w", err)
		}
		return filepath.Join(home, DefaultSpecsDir), nil
	}
	if strings.HasPrefix(root, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("spec: home dir: %w", err)
		}
		return filepath.Join(home, root[2:]), nil
	}
	return filepath.Abs(root)
}
