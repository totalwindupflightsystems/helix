// Package dispatcher provides the Helix orchestration layer that replaces
// Axiom. It decomposes specifications into tasks, assigns tasks to capable
// agents, and drives the Ralph Loop execution pipeline.
package dispatcher

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/trust"
)

// FileCategory maps a changed file's path/extension to a risk category used
// for determining the required trust tier.
type FileCategory string

const (
	CatInfrastructure FileCategory = "infrastructure" // IaC, Docker, K8s
	CatAuth           FileCategory = "auth"           // auth, sessions, tokens
	CatCICD           FileCategory = "ci_cd"          // CI/CD pipelines, deployment
	CatConfig         FileCategory = "config"         // configuration files
	CatDocs           FileCategory = "docs"           // documentation
	CatGeneral        FileCategory = "general"        // application code, features
)

// FileCategoryTier maps each file category to its minimum required trust tier.
// Per spec §3.2: IaC→Tier 1 (Observed), CI/CD→Tier 3 (Veteran), auth→Tier 2 (Trusted).
var FileCategoryTier = map[FileCategory]trust.TrustTier{
	CatInfrastructure: trust.TierObserved,
	CatAuth:           trust.TierTrusted,
	CatCICD:           trust.TierVeteran,
	CatConfig:         trust.TierObserved,
	CatDocs:           trust.TierProvisional,
	CatGeneral:        trust.TierProvisional,
}

// extensionCategories maps file extensions/suffixes to file categories.
var extensionCategories = []struct {
	ext  string
	cat  FileCategory
	full bool // true: must match full filename, not suffix
}{
	// Infrastructure
	{ext: "Dockerfile", cat: CatInfrastructure, full: true},
	{ext: "docker-compose", cat: CatInfrastructure, full: false},
	{ext: ".tf", cat: CatInfrastructure, full: false},
	{ext: ".tfvars", cat: CatInfrastructure, full: false},
	{ext: ".hcl", cat: CatInfrastructure, full: false},
	{ext: "Makefile", cat: CatInfrastructure, full: true},
	{ext: ".mk", cat: CatInfrastructure, full: false},
	{ext: ".yaml", cat: CatCICD, full: false}, // CI/CD pipelines
	{ext: ".yml", cat: CatCICD, full: false},
	{ext: ".toml", cat: CatConfig, full: false},

	// Auth
	{ext: "auth", cat: CatAuth, full: false},
	{ext: "token", cat: CatAuth, full: false},
	{ext: "session", cat: CatAuth, full: false},
	{ext: "oauth", cat: CatAuth, full: false},
	{ext: "jwt", cat: CatAuth, full: false},
	{ext: "crypto", cat: CatAuth, full: false},

	// Docs
	{ext: ".md", cat: CatDocs, full: false},
	{ext: ".txt", cat: CatDocs, full: false},
	{ext: ".adoc", cat: CatDocs, full: false},
}

// ClassifyFileCategory determines the file category for a given file path.
// It checks the filename and path components against known patterns.
func ClassifyFileCategory(path string) FileCategory {
	base := filepath.Base(path)
	lower := strings.ToLower(base)
	lowerPath := strings.ToLower(path)

	// Full-filename matches first (before extension-based matching).
	if strings.EqualFold(base, "Dockerfile") || strings.EqualFold(base, "Makefile") {
		return CatInfrastructure
	}
	// Prefix matches for compound names.
	if strings.HasPrefix(lower, "docker-compose") {
		return CatInfrastructure
	}

	for _, ec := range extensionCategories {
		if ec.full {
			continue // already handled above
		}
		if strings.HasSuffix(lower, ec.ext) {
			return ec.cat
		}
	}

	// Check for CI/CD and auth keywords in the path.
	if strings.Contains(lowerPath, ".github/workflows") || strings.Contains(lowerPath, ".gitlab-ci") {
		return CatCICD
	}
	if strings.Contains(lowerPath, "/auth/") || strings.Contains(lowerPath, "/identity/") ||
		strings.Contains(lowerPath, "/tokens/") || strings.Contains(lowerPath, "/sessions/") ||
		strings.Contains(lowerPath, "/oauth/") {
		return CatAuth
	}
	if strings.Contains(lowerPath, "/infra/") || strings.Contains(lowerPath, "/deploy/") ||
		strings.Contains(lowerPath, "/docker/") {
		return CatInfrastructure
	}

	return CatGeneral
}

// RequiredTierForFiles returns the highest required tier across a set of files.
// If any file requires a higher tier, the task requires that tier.
func RequiredTierForFiles(files []string) trust.TrustTier {
	highest := trust.TierProvisional
	for _, f := range files {
		cat := ClassifyFileCategory(f)
		tier, ok := FileCategoryTier[cat]
		if !ok {
			continue
		}
		if trust.CompareTiers(tier, highest) > 0 {
			highest = tier
		}
	}
	return highest
}

// ValidateTierAssignment checks whether an agent can be assigned a task based
// on trust tier. Returns nil if the assignment is valid, or an error describing
// why it is not.
func ValidateTierAssignment(agent AgentProfile, task Task) error {
	if task.RequiredTier == "" {
		task.RequiredTier = trust.TierProvisional
	}
	if trust.CompareTiers(agent.Tier, task.RequiredTier) < 0 {
		return fmt.Errorf("agent %s has tier %s but task %s requires tier %s: agent cannot self-assign above tier",
			agent.Name, agent.Tier, task.ID, task.RequiredTier)
	}
	return nil
}

// CanSelfAssign reports whether an agent can self-assign to a task based on
// their trust tier relative to the task's required tier.
func CanSelfAssign(agent AgentProfile, task Task) bool {
	return ValidateTierAssignment(agent, task) == nil
}
