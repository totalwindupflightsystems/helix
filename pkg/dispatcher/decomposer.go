package dispatcher

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// DecomposeSpec reads a spec markdown file and extracts tasks from sections
// headed with "Phase" or "Feature". Each section becomes one Task. Priority
// is assigned based on section ordering (first section = highest priority 1).
//
// The spec file is expected to use ## headings. A line like "## PHASE 1: Title"
// or "## Feature: Title" starts a new task. The heading text becomes the task
// description; the body until the next heading is captured as context.
func DecomposeSpec(specPath string) ([]Task, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrSpecNotFound, specPath, err)
	}

	var tasks []Task
	var currentDesc strings.Builder
	var currentHeading string
	priority := 0

	flush := func() {
		if currentHeading == "" {
			return
		}
		priority++
		tasks = append(tasks, Task{
			ID:          fmt.Sprintf("task-%03d", priority),
			SpecRef:     specPath,
			Description: strings.TrimSpace(currentHeading),
			Priority:    priority,
			Status:      StatusPending,
		})
		currentHeading = ""
		currentDesc.Reset()
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Detect ## headings
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			heading := strings.TrimPrefix(trimmed, "## ")
			upper := strings.ToUpper(heading)
			if strings.Contains(upper, "PHASE") || strings.Contains(upper, "FEATURE") {
				currentHeading = heading
			}
		}
	}
	// Flush last section
	flush()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("dispatcher: error reading spec: %w", err)
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("%w: no Phase or Feature sections found in %s", ErrDecomposeFailed, specPath)
	}

	return tasks, nil
}

// DecomposeTask breaks a task description into actionable steps. Each sentence
// (delimited by ". " or newline) that contains an action verb becomes a step.
func DecomposeTask(taskDesc string) ([]Step, error) {
	if strings.TrimSpace(taskDesc) == "" {
		return nil, fmt.Errorf("%w: empty task description", ErrDecomposeFailed)
	}

	// Split on newlines first, then on sentence boundaries.
	raw := strings.ReplaceAll(taskDesc, "\n", ". ")
	parts := strings.Split(raw, ". ")

	actionVerbs := []string{
		"create", "write", "implement", "add", "update", "delete",
		"remove", "refactor", "test", "verify", "build", "deploy",
		"configure", "setup", "set up", "run", "check", "read",
		"modify", "edit", "replace", "migrate", "extract", "merge",
	}

	var steps []Step
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		for _, verb := range actionVerbs {
			if strings.Contains(lower, verb) {
				steps = append(steps, Step{
					Action:         part,
					ExpectedOutput: "",
					Status:         StepPending,
				})
				break
			}
		}
	}

	if len(steps) == 0 {
		// Fallback: treat the entire description as one step.
		steps = append(steps, Step{
			Action:         taskDesc,
			ExpectedOutput: "",
			Status:         StepPending,
		})
	}

	return steps, nil
}
