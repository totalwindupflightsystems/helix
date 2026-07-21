// Command helix — incident_attr.go
//
// Git-based change-path discovery helpers used by `helix incident attribute`.
// Mechanical extraction from the original incident.go; no behavior change.

package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/incident"
)

// SeverityMediumStr is the medium severity string for trust penalty calculation.
const SeverityMediumStr = "medium"

// discoverChangePaths queries git log for changed files within the given
// time window, then runs git blame on each to identify author/reviewer/approver.
func discoverChangePaths(since string, limit int) ([]incident.ChangePath, error) {
	// Convert shorthand formats to git-compatible relative dates.
	gitSince := sinceToGitSince(since)

	// Get commits in the time window.
	output, err := gitLog(gitSince, limit)
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	if output == "" {
		return nil, nil
	}

	// Collect unique files from commit output.
	files := uniqueFilesFromGitLog(output)

	// For each file, run git blame to find the merge commit + author.
	var paths []incident.ChangePath
	for _, f := range files {
		blameOut, err := gitBlame(f)
		if err != nil {
			// Skip files that can't be blamed (deleted, binary, etc.)
			continue
		}
		path := parseBlameOutput(blameOut, f)
		if path.AuthorID != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// sinceToGitSince converts shorthand duration formats (24h, 7d, 30m, 2w)
// to git-compatible relative date strings. Unknown formats pass through.
func sinceToGitSince(since string) string {
	// If it already looks git-compatible, pass through.
	if strings.Contains(since, ".") || strings.Contains(since, " ") || strings.Contains(since, "-") {
		return since
	}
	// Parse shorthand: <number><unit>
	for i, c := range since {
		if c >= '0' && c <= '9' {
			continue
		}
		num := since[:i]
		unit := since[i:]
		switch unit {
		case "h", "hr", "hrs":
			return num + ".hours"
		case "d":
			return num + ".days"
		case "w", "wk", "wks":
			return num + ".weeks"
		case "m":
			return num + ".minutes"
		case "s":
			return num + ".seconds"
		}
		break
	}
	return since
}

// gitLog runs `git log` to get changed files since the given duration.
func gitLog(since string, limit int) (string, error) {
	cmd := exec.Command("git", "log", "--since="+since,
		"--name-only", "--pretty=format:%H %an %s",
		"-n", fmt.Sprintf("%d", limit))
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// uniqueFilesFromGitLog parses git log output and returns unique file paths.
func uniqueFilesFromGitLog(output string) []string {
	seen := make(map[string]bool)
	var files []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "commit ") {
			continue
		}
		// Skip lines that look like commit hashes + metadata (don't contain "/.")
		if !strings.Contains(line, "/") && !strings.HasSuffix(line, ".go") &&
			!strings.HasSuffix(line, ".py") && !strings.HasSuffix(line, ".ts") &&
			!strings.HasSuffix(line, ".rs") && !strings.HasSuffix(line, ".js") {
			continue
		}
		if !seen[line] {
			seen[line] = true
			files = append(files, line)
		}
	}
	return files
}

// gitBlame runs `git blame` on a file to find the commit hash and author.
func gitBlame(file string) (string, error) {
	cmd := exec.Command("git", "blame", "--porcelain", "-w", file)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// parseBlameOutput extracts ChangePath from git blame porcelain output.
func parseBlameOutput(blameOutput, filePath string) incident.ChangePath {
	lines := strings.Split(blameOutput, "\n")
	if len(lines) < 2 {
		return incident.ChangePath{FilePath: filePath}
	}

	// First line: <commit-hash> <orig-line> <final-line> <line-count>
	firstFields := strings.Fields(lines[0])
	if len(firstFields) < 1 || len(firstFields[0]) < 7 {
		return incident.ChangePath{FilePath: filePath}
	}
	commitHash := firstFields[0]

	// Parse porcelain fields: author <name>, committer <name>, summary <text>
	var author string
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "author ") {
			author = strings.TrimPrefix(line, "author ")
			break
		}
		_ = strings.HasPrefix(line, "author-mail ")
		// Unused — prefer author name field for identity extraction.
	}

	return incident.ChangePath{
		FilePath: filePath,
		MergeSHA: commitHash,
		AuthorID: author,
	}
}
