package ideation

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// AdvocacyRecord is one agent advocacy vote on an idea.
type AdvocacyRecord struct {
	AgentID     string        `json:"agent_id"`
	Position    string        `json:"position"` // for|against|priority
	Evidence    []EvidenceRef `json:"evidence,omitempty"`
	SubmittedAt time.Time     `json:"submitted_at"`
}

// Advocacy position constants.
const (
	PositionFor      = "for"
	PositionAgainst  = "against"
	PositionPriority = "priority"
)

// PrioritizedIdea is an idea with rank, cost, and advocacy metadata.
type PrioritizedIdea struct {
	Idea
	Rank            int              `json:"rank"`
	Score           float64          `json:"score"`
	CostEstimate    float64          `json:"cost_estimate"` // USD
	RiskScore       float64          `json:"risk_score"`
	AdvocacyRecords []AdvocacyRecord `json:"advocacy,omitempty"`
	Dependencies    []string         `json:"dependencies,omitempty"`
}

// Roadmap is a deterministic prioritized ordering of ideas.
type Roadmap struct {
	Ideas       []PrioritizedIdea `json:"ideas"`
	GeneratedAt time.Time         `json:"generated_at"`
	Version     int               `json:"version"`
}

// IdeaPrioritizer ranks ideas using cost, risk, and advocacy.
// Advocacy is loaded/saved from advocacy.jsonl sibling to the ideas store.
type IdeaPrioritizer struct {
	mu             sync.Mutex
	advocacy       map[string][]AdvocacyRecord // ideaID → records
	advocacyPath   string
	persistEnabled bool
}

// NewIdeaPrioritizer constructs a prioritizer. Empty path keeps advocacy
// in-memory only. Non-empty path (typically ideas.jsonl path) loads/saves
// advocacy from a sibling advocacy.jsonl in the same directory.
func NewIdeaPrioritizer(path string) *IdeaPrioritizer {
	p := &IdeaPrioritizer{
		advocacy: make(map[string][]AdvocacyRecord),
	}
	if path != "" {
		dir := filepath.Dir(path)
		// If path is just a filename with no dir, use that name as store file.
		if dir == "." || dir == "" {
			// Treat path as ideas file in current dir.
			p.advocacyPath = filepath.Join(".", "advocacy.jsonl")
		} else {
			p.advocacyPath = filepath.Join(dir, "advocacy.jsonl")
		}
		// Special case: if caller passed a directory, put advocacy there.
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			p.advocacyPath = filepath.Join(path, "advocacy.jsonl")
		}
		// If path looks like ideas.jsonl, sibling advocacy.jsonl.
		if strings.HasSuffix(path, DefaultIdeasFile) || strings.HasSuffix(path, ".jsonl") {
			p.advocacyPath = filepath.Join(filepath.Dir(path), "advocacy.jsonl")
		}
		p.persistEnabled = true
		_ = p.loadAdvocacy()
	}
	return p
}

// SubmitAdvocacy records an advocacy position for an idea.
func (p *IdeaPrioritizer) SubmitAdvocacy(ideaID string, rec AdvocacyRecord) error {
	if ideaID == "" {
		return fmt.Errorf("ideation: idea id is required")
	}
	if strings.TrimSpace(rec.AgentID) == "" {
		return fmt.Errorf("ideation: agent_id is required")
	}
	switch rec.Position {
	case PositionFor, PositionAgainst, PositionPriority:
		// ok
	default:
		return fmt.Errorf("ideation: invalid position %q (want for|against|priority)", rec.Position)
	}
	if rec.SubmittedAt.IsZero() {
		rec.SubmittedAt = time.Now().UTC()
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.advocacy[ideaID] = append(p.advocacy[ideaID], rec)
	if p.persistEnabled {
		return p.appendAdvocacyUnlocked(ideaID, rec)
	}
	return nil
}

// AdvocacyFor returns advocacy records for an idea (copy).
func (p *IdeaPrioritizer) AdvocacyFor(ideaID string) []AdvocacyRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	recs := p.advocacy[ideaID]
	if len(recs) == 0 {
		return nil
	}
	out := make([]AdvocacyRecord, len(recs))
	copy(out, recs)
	return out
}

// Prioritize ranks ideas deterministically (score desc, title asc).
// Mutates idea CostTotal/Score/Status for validated/draft ideas in the
// returned PrioritizedIdea copies; callers should persist via store.Update.
func (p *IdeaPrioritizer) Prioritize(ideas []*Idea) (*Roadmap, error) {
	if ideas == nil {
		ideas = []*Idea{}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	ranked := make([]PrioritizedIdea, 0, len(ideas))
	for _, idea := range ideas {
		if idea == nil {
			continue
		}
		// Skip closed ideas from active roadmap.
		if idea.Status == StatusClosed {
			continue
		}
		cost := estimateCost(idea)
		risk := idea.RiskScore
		recs := p.advocacy[idea.ID]
		score := compositeScore(cost, risk, recs)

		pi := PrioritizedIdea{
			Idea:            *idea,
			Score:           score,
			CostEstimate:    cost,
			RiskScore:       risk,
			AdvocacyRecords: append([]AdvocacyRecord(nil), recs...),
		}
		// Update embedded Idea fields for persistence by CLI.
		pi.Idea.CostTotal = cost
		pi.Idea.Score = score
		if pi.Idea.Status == StatusDraft || pi.Idea.Status == StatusValidated || pi.Idea.Status == "" {
			pi.Idea.Status = StatusPrioritized
		}
		// Also reflect on PrioritizedIdea top-level for JSON convenience.
		// (Score already set above.)
		ranked = append(ranked, pi)
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].Title < ranked[j].Title
	})
	for i := range ranked {
		ranked[i].Rank = i + 1
	}

	return &Roadmap{
		Ideas:       ranked,
		GeneratedAt: time.Now().UTC(),
		Version:     1,
	}, nil
}

// EstimateCost is exported for CLI dry-run previews.
func EstimateCost(idea *Idea) float64 {
	return estimateCost(idea)
}

func estimateCost(idea *Idea) float64 {
	if idea == nil {
		return 0
	}
	cost := 0.05 + 0.02*float64(len(idea.Tags)) + 0.0001*float64(len(idea.Body)) + 0.03*float64(len(idea.Evidence))
	// Unvalidated ideas get a 20% uncertainty premium.
	switch idea.Status {
	case StatusValidated, StatusPrioritized, StatusPromoted:
		// no premium
	default:
		cost *= 1.2
	}
	// Round up to nearest cent.
	return math.Ceil(cost*100) / 100
}

func compositeScore(cost, risk float64, recs []AdvocacyRecord) float64 {
	costNorm := cost / 5.0
	if costNorm > 1.0 {
		costNorm = 1.0
	}
	riskNorm := risk / 100.0
	if riskNorm < 0 {
		riskNorm = 0
	}
	if riskNorm > 1 {
		riskNorm = 1
	}

	advFor, advAgainst := 0, 0
	for _, r := range recs {
		switch r.Position {
		case PositionFor, PositionPriority:
			advFor++
		case PositionAgainst:
			advAgainst++
		}
	}
	advScore := (float64(advFor-advAgainst) + 1) / 5.0
	if advScore > 1.0 {
		advScore = 1.0
	}
	if advScore < 0 {
		advScore = 0
	}

	return (1-costNorm)*0.4 + (1-riskNorm)*0.3 + advScore*0.3
}

// advocacyLine is the on-disk JSONL shape.
type advocacyLine struct {
	IdeaID string         `json:"idea_id"`
	Record AdvocacyRecord `json:"record"`
}

func (p *IdeaPrioritizer) loadAdvocacy() error {
	if p.advocacyPath == "" {
		return nil
	}
	f, err := os.Open(p.advocacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var al advocacyLine
		if err := json.Unmarshal([]byte(line), &al); err != nil {
			continue
		}
		if al.IdeaID == "" {
			continue
		}
		p.advocacy[al.IdeaID] = append(p.advocacy[al.IdeaID], al.Record)
	}
	return sc.Err()
}

func (p *IdeaPrioritizer) appendAdvocacyUnlocked(ideaID string, rec AdvocacyRecord) error {
	if p.advocacyPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.advocacyPath), 0o755); err != nil {
		return fmt.Errorf("ideation: mkdir advocacy: %w", err)
	}
	f, err := os.OpenFile(p.advocacyPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("ideation: open advocacy: %w", err)
	}
	defer f.Close()
	al := advocacyLine{IdeaID: ideaID, Record: rec}
	data, err := json.Marshal(al)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}
