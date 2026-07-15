// Command helix — models.go
//
// `helix models` wraps pkg/learning.ModelEvaluator: per-model production
// outcome tracking, rotation enforcement, and selection scoring
// (Phase 12 §12.4).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/learning"
)

var sharedEval *learning.ModelEvaluator

func getSharedEval() *learning.ModelEvaluator {
	if sharedEval == nil {
		sharedEval = learning.NewModelEvaluator()
	}
	return sharedEval
}

const (
	modelsExitOK    = 0
	modelsExitError = 2
)

// modelsFlags holds parsed flags for helix models subcommands.
type modelsFlags struct {
	subcommand string
	modelID    string
	reason     string
	sortBy     string
	jsonOut    bool
	dryRun     bool
}

func runModelsWithDryRun(args []string, stdout, stderr io.Writer, dryRun bool) int {
	f, ok := parseModelsFlags(args, stderr)
	if !ok {
		return modelsExitError
	}
	f.dryRun = dryRun

	switch f.subcommand {
	case "list":
		return runModelsList(f, stdout, stderr)
	case "show":
		return runModelsShow(f, stdout, stderr)
	case "evaluate":
		return runModelsEvaluate(f, stdout, stderr)
	case "rotate":
		return runModelsRotate(f, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "models: unknown subcommand %q\n", f.subcommand)
		fmt.Fprintf(stderr, "Available: list, show, evaluate, rotate\n")
		return modelsExitError
	}
}

func parseModelsFlags(args []string, stderr io.Writer) (modelsFlags, bool) {
	var f modelsFlags

	if len(args) == 0 {
		printModelsUsage(stderr)
		return f, false
	}

	f.subcommand = args[0]
	i := 1
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			printModelsUsage(stderr)
			return f, false
		case arg == "--json":
			f.jsonOut = true
		case arg == "--sort-by":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "models: --sort-by requires a value")
				return f, false
			}
			i++
			f.sortBy = args[i]
		case arg == "--reason":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "models: --reason requires a value")
				return f, false
			}
			i++
			f.reason = args[i]
		default:
			// First positional arg after subcommand is model ID.
			if f.modelID == "" && !strings.HasPrefix(arg, "--") {
				f.modelID = arg
			} else {
				fmt.Fprintf(stderr, "models: unknown flag %q\n", arg)
				return f, false
			}
		}
		i++
	}

	return f, true
}

func runModelsList(f modelsFlags, stdout, stderr io.Writer) int {
	me := getSharedEval()
	if f.dryRun {
		fmt.Fprintln(stdout, "[dry-run] would list models")
		return modelsExitOK
	}

	sortBy := f.sortBy
	if sortBy == "" {
		sortBy = "incident-rate"
	}

	models := me.ModelsSortedBy(sortBy)

	if f.jsonOut {
		type modelEntry struct {
			ModelID             string  `json:"model_id"`
			IncidentRate        float64 `json:"incident_rate"`
			FalsePositiveRate   float64 `json:"false_positive_rate"`
			TotalMerges         int     `json:"total_merges"`
			IncidentsAttributed int     `json:"incidents_attributed"`
			TotalReviews        int     `json:"total_reviews"`
			FalsePositives      int     `json:"false_positives"`
			AvgTrustScore       float64 `json:"avg_trust_score"`
			AvgCostPerMerge     float64 `json:"avg_cost_per_merge"`
			ActiveAgents        int     `json:"active_agents"`
			InRotation          bool    `json:"in_rotation"`
			Flagged             bool    `json:"flagged"`
		}
		entries := make([]modelEntry, 0, len(models))
		for _, m := range models {
			entries = append(entries, modelEntry{
				ModelID:             m.ModelID,
				IncidentRate:        m.IncidentRate,
				FalsePositiveRate:   m.FalsePositiveRate,
				TotalMerges:         m.TotalMerges,
				IncidentsAttributed: m.IncidentsAttributed,
				TotalReviews:        m.TotalReviews,
				FalsePositives:      m.FalsePositives,
				AvgTrustScore:       m.AvgTrustScore,
				AvgCostPerMerge:     m.AvgCostPerMerge,
				ActiveAgents:        m.ActiveAgents,
				InRotation:          me.IsInReviewRotation(m.ModelID),
				Flagged:             me.IsFlagged(m.ModelID),
			})
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(entries)
		return modelsExitOK
	}

	if len(models) == 0 {
		fmt.Fprintln(stdout, "No models tracked. Record merges, incidents, or reviews to populate metrics.")
		return modelsExitOK
	}

	fmt.Fprintf(stdout, "%-30s %8s %8s %6s %8s %6s %8s %s\n",
		"MODEL", "IRATE", "FPRATE", "MERGES", "INCIDENTS", "REVIEWS", "COST", "STATUS")
	fmt.Fprintln(stdout, strings.Repeat("-", 100))
	for _, m := range models {
		status := "active"
		if !me.IsInReviewRotation(m.ModelID) {
			status = "removed"
		} else if me.IsFlagged(m.ModelID) {
			status = "flagged"
		}
		fmt.Fprintf(stdout, "%-30s %7.1f%% %7.1f%% %6d %8d %6d %7.4f %s\n",
			m.ModelID,
			m.IncidentRate*100,
			m.FalsePositiveRate*100,
			m.TotalMerges,
			m.IncidentsAttributed,
			m.TotalReviews,
			m.AvgCostPerMerge,
			status,
		)
	}
	fmt.Fprintf(stdout, "\nFleet average incident rate: %.1f%%\n", me.FleetAvgIncidentRate()*100)
	return modelsExitOK
}

func runModelsShow(f modelsFlags, stdout, stderr io.Writer) int {
	me := getSharedEval()
	if f.modelID == "" {
		fmt.Fprintln(stderr, "models show: <model-id> is required")
		return modelsExitError
	}
	if f.dryRun {
		fmt.Fprintf(stdout, "[dry-run] would show model %s\n", f.modelID)
		return modelsExitOK
	}

	m, ok := me.GetMetrics(f.modelID)
	if !ok {
		fmt.Fprintf(stderr, "models: model %q not found\n", f.modelID)
		return modelsExitError
	}

	if f.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]interface{}{
			"model_id":             m.ModelID,
			"incident_rate":        m.IncidentRate,
			"false_positive_rate":  m.FalsePositiveRate,
			"total_merges":         m.TotalMerges,
			"incidents_attributed": m.IncidentsAttributed,
			"total_reviews":        m.TotalReviews,
			"false_positives":      m.FalsePositives,
			"avg_trust_score":      m.AvgTrustScore,
			"avg_cost_per_merge":   m.AvgCostPerMerge,
			"active_agents":        m.ActiveAgents,
			"in_rotation":          me.IsInReviewRotation(m.ModelID),
			"flagged":              me.IsFlagged(m.ModelID),
			"last_evaluated":       m.LastEvaluated,
		})
		return modelsExitOK
	}

	fmt.Fprintf(stdout, "Model: %s\n", m.ModelID)
	fmt.Fprintf(stdout, "  Incident Rate:      %.1f%% (%d incidents / %d merges)\n",
		m.IncidentRate*100, m.IncidentsAttributed, m.TotalMerges)
	fmt.Fprintf(stdout, "  False Positive Rate: %.1f%% (%d FP / %d reviews)\n",
		m.FalsePositiveRate*100, m.FalsePositives, m.TotalReviews)
	fmt.Fprintf(stdout, "  Avg Trust Score:    %.3f\n", m.AvgTrustScore)
	fmt.Fprintf(stdout, "  Avg Cost/Merge:     %.6f\n", m.AvgCostPerMerge)
	fmt.Fprintf(stdout, "  Active Agents:      %d\n", m.ActiveAgents)
	fmt.Fprintf(stdout, "  In Rotation:        %v\n", me.IsInReviewRotation(m.ModelID))
	fmt.Fprintf(stdout, "  Flagged:            %v\n", me.IsFlagged(m.ModelID))
	fmt.Fprintf(stdout, "  Last Evaluated:     %s\n", m.LastEvaluated.Format("2006-01-02 15:04:05"))
	return modelsExitOK
}

func runModelsEvaluate(f modelsFlags, stdout, stderr io.Writer) int {
	me := getSharedEval()
	if f.dryRun {
		fmt.Fprintln(stdout, "[dry-run] would evaluate all models")
		return modelsExitOK
	}

	me.EvaluateAll()

	if f.jsonOut {
		_ = json.NewEncoder(stdout).Encode(map[string]interface{}{
			"status":                  "evaluated",
			"model_count":             len(me.ListModels()),
			"fleet_avg_incident_rate": me.FleetAvgIncidentRate(),
		})
		return modelsExitOK
	}

	fmt.Fprintf(stdout, "Evaluated %d model(s). Fleet avg incident rate: %.1f%%\n",
		len(me.ListModels()), me.FleetAvgIncidentRate()*100)

	events := me.ListEvents()
	if len(events) > 0 {
		last := events[len(events)-1]
		fmt.Fprintf(stdout, "Latest event: %s — %s (%s)\n", last.ModelID, last.EventType, last.Reason)
	}
	return modelsExitOK
}

func runModelsRotate(f modelsFlags, stdout, stderr io.Writer) int {
	me := getSharedEval()
	if f.modelID == "" {
		fmt.Fprintln(stderr, "models rotate: <model-id> is required")
		return modelsExitError
	}
	if f.reason == "" {
		fmt.Fprintln(stderr, "models rotate: --reason is required")
		return modelsExitError
	}
	if f.dryRun {
		fmt.Fprintf(stdout, "[dry-run] would rotate model %s: %s\n", f.modelID, f.reason)
		return modelsExitOK
	}

	me.RemoveFromRotation(f.modelID, f.reason)

	if f.jsonOut {
		_ = json.NewEncoder(stdout).Encode(map[string]string{
			"model_id": f.modelID,
			"status":   "removed",
			"reason":   f.reason,
		})
		return modelsExitOK
	}

	fmt.Fprintf(stdout, "Model %s removed from review rotation: %s\n", f.modelID, f.reason)
	return modelsExitOK
}

func printModelsUsage(stderr io.Writer) {
	fmt.Fprint(os.Stderr, `Usage: helix models <subcommand> [flags]

Subcommands:
  list      List all tracked models with metrics [--sort-by incident-rate|false-positive-rate|cost] [--json]
  show      Show detailed metrics for a model [--json]
  evaluate  Trigger manual evaluation of all models [--json]
  rotate    Remove a model from review rotation <model-id> --reason "..."

Flags:
  --sort-by KEY    Sort key for list: incident-rate, false-positive-rate, cost (default: incident-rate)
  --reason TEXT    Reason for rotation (required for rotate)
  --json           JSON output
`)
}
