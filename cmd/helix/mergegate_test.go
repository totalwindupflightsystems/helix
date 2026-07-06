package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/mergegate"
	"github.com/totalwindupflightsystems/helix/pkg/review"
	"github.com/totalwindupflightsystems/helix/pkg/verify"
)

// ============================================================================
// Flag parsing tests
// ============================================================================

func TestParseMergeGateFlags_DefaultsToHelp(t *testing.T) {
	flags, helpWanted, rc := parseMergeGateFlags([]string{})
	assert.Equal(t, mgExitOK, rc)
	assert.False(t, helpWanted)
	assert.Equal(t, "help", flags.subcommand)
}

func TestParseMergeGateFlags_ExplicitHelp(t *testing.T) {
	flags, helpWanted, rc := parseMergeGateFlags([]string{"--help"})
	assert.Equal(t, mgExitOK, rc)
	assert.True(t, helpWanted)
	assert.Equal(t, "help", flags.subcommand) // --help parsed before subcommand assignment
}

func TestParseMergeGateFlags_CheckSubcommand(t *testing.T) {
	flags, _, rc := parseMergeGateFlags([]string{"check", "--trust", "observed"})
	require.Equal(t, mgExitOK, rc)
	assert.Equal(t, "check", flags.subcommand)
	assert.Equal(t, "observed", flags.trustTier)
}

func TestParseMergeGateFlags_AllFlags(t *testing.T) {
	flags, _, rc := parseMergeGateFlags([]string{
		"check",
		"--evidence", "/tmp/ev.json",
		"--contract", "/tmp/contract.yaml",
		"--trust", "trusted",
		"--agent", "agent-007",
		"--json",
		"--skip-contract",
		"--skip-cost",
	})
	require.Equal(t, mgExitOK, rc)
	assert.Equal(t, "check", flags.subcommand)
	assert.Equal(t, "/tmp/ev.json", flags.evidence)
	assert.Equal(t, "/tmp/contract.yaml", flags.contract)
	assert.Equal(t, "trusted", flags.trustTier)
	assert.Equal(t, "agent-007", flags.agent)
	assert.True(t, flags.jsonOut)
	assert.True(t, flags.skipContract)
	assert.True(t, flags.skipCost)
}

func TestParseMergeGateFlags_EvidenceNeedsValue(t *testing.T) {
	_, _, rc := parseMergeGateFlags([]string{"check", "--evidence"})
	assert.Equal(t, mgExitError, rc)
}

func TestParseMergeGateFlags_ContractNeedsValue(t *testing.T) {
	_, _, rc := parseMergeGateFlags([]string{"check", "--contract"})
	assert.Equal(t, mgExitError, rc)
}

func TestParseMergeGateFlags_TrustNeedsValue(t *testing.T) {
	_, _, rc := parseMergeGateFlags([]string{"check", "--trust"})
	assert.Equal(t, mgExitError, rc)
}

func TestParseMergeGateFlags_UnknownFlag(t *testing.T) {
	_, _, rc := parseMergeGateFlags([]string{"check", "--bogus"})
	assert.Equal(t, mgExitError, rc)
}

// ============================================================================
// Help tests
// ============================================================================

func TestRunMergeGate_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"help"}, &stdout, &stderr)
	assert.Equal(t, mgExitOK, rc)
	assert.Contains(t, stdout.String(), "helix mergegate")
	assert.Contains(t, stdout.String(), "check")
	assert.Contains(t, stdout.String(), "checks")
}

func TestRunMergeGate_NoArgs_ShowsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{}, &stdout, &stderr)
	assert.Equal(t, mgExitOK, rc)
	assert.Contains(t, stdout.String(), "helix mergegate")
}

func TestRunMergeGate_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"bogus"}, &stdout, &stderr)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, stderr.String(), "unknown subcommand")
}

// ============================================================================
// checks listing tests
// ============================================================================

func TestRunMergeGate_Checks(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"checks"}, &stdout, &stderr)
	assert.Equal(t, mgExitOK, rc)
	output := stdout.String()
	assert.Contains(t, output, "evidence")
	assert.Contains(t, output, "consensus")
	assert.Contains(t, output, "contract")
	assert.Contains(t, output, "trust")
	assert.Contains(t, output, "cost")
}

func TestRunMergeGate_Checks_JSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"checks", "--json"}, &stdout, &stderr)
	assert.Equal(t, mgExitOK, rc)

	var items []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	err := json.Unmarshal(stdout.Bytes(), &items)
	require.NoError(t, err)
	assert.Len(t, items, 5)
	assert.Equal(t, "evidence", items[0].ID)
	assert.Equal(t, "cost", items[4].ID)
}

// ============================================================================
// check tests
// ============================================================================

func TestRunMergeGate_Check_NoTrust_Error(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"check"}, &stdout, &stderr)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, stderr.String(), "--trust")
}

func TestRunMergeGate_Check_InvalidTrust_Error(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"check", "--trust", "super"}, &stdout, &stderr)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, stderr.String(), "invalid trust tier")
}

func TestRunMergeGate_Check_NoEvidence_BlockedByEvidenceCheck(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"check", "--trust", "trusted"}, &stdout, &stderr)
	// No evidence → evidence check FAILS → BLOCKED
	assert.Equal(t, mgExitBlock, rc)
	output := stdout.String()
	assert.Contains(t, output, "BLOCKED")
}

func TestRunMergeGate_Check_AllowedWithEvidenceOnly(t *testing.T) {
	// Create a valid evidence bundle
	bundle := review.NewEvidenceBundle(
		"http://localhost:3030/org/repo/pulls/1",
		"rev-001",
		review.Formation{
			Primary:     review.ModelInfo{Model: "glm-5.2", Provider: "zai"},
			Adversarial: review.ModelInfo{Model: "minimax-m3", Provider: "minimax"},
			Audit:       review.ModelInfo{Model: "deepseek-v4-flash", Provider: "deepseek"},
		},
		"abc123",
		"def456",
	)
	bundle.SetConsensus("approved", "approved", "approved")

	tmpDir := t.TempDir()
	evPath := filepath.Join(tmpDir, "evidence.json")
	data, err := json.Marshal(bundle)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(evPath, data, 0o644))

	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{
		"check", "--evidence", evPath, "--trust", "trusted",
		"--skip-contract", "--skip-cost",
	}, &stdout, &stderr)
	assert.Equal(t, mgExitOK, rc, "stderr: %s", stderr.String())
	output := stdout.String()
	assert.Contains(t, output, "ALLOWED")
}

func TestRunMergeGate_Check_JSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"check", "--trust", "trusted", "--json", "--skip-contract", "--skip-cost"}, &stdout, &stderr)
	assert.Equal(t, mgExitBlock, rc)

	var report mergegate.GateReport
	err := json.Unmarshal(stdout.Bytes(), &report)
	require.NoError(t, err)
	assert.Equal(t, "BLOCKED", string(report.Decision))
}

func TestRunMergeGate_Check_InvalidEvidenceJSON(t *testing.T) {
	tmpDir := t.TempDir()
	evPath := filepath.Join(tmpDir, "bad.json")
	require.NoError(t, os.WriteFile(evPath, []byte("not json"), 0o644))

	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"check", "--evidence", evPath, "--trust", "trusted"}, &stdout, &stderr)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, stderr.String(), "parsing evidence bundle")
}

func TestRunMergeGate_Check_EvidenceFileNotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"check", "--evidence", "/nonexistent/ev.json", "--trust", "trusted"}, &stdout, &stderr)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, stderr.String(), "reading evidence bundle")
}

func TestRunMergeGate_Check_ContractFileNotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"check", "--contract", "/nonexistent/contract.yaml", "--trust", "trusted"}, &stdout, &stderr)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, stderr.String(), "reading behavior contract")
}

func TestRunMergeGate_Check_InvalidContractYAML(t *testing.T) {
	tmpDir := t.TempDir()
	contractPath := filepath.Join(tmpDir, "bad.yaml")
	require.NoError(t, os.WriteFile(contractPath, []byte("not: valid: yaml: :"), 0o644))

	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"check", "--contract", contractPath, "--trust", "trusted"}, &stdout, &stderr)
	assert.Equal(t, mgExitError, rc)
	assert.Contains(t, stderr.String(), "parsing behavior contract")
}

func TestRunMergeGate_Check_ValidContract(t *testing.T) {
	tmpDir := t.TempDir()
	contractPath := filepath.Join(tmpDir, "contract.yaml")
	contractYAML := `contract:
  name: "api-latency"
  agent: "agent-007"
  merge_commit: "abc123"
  assertions:
    - metric: "success_rate"
      operator: "gte"
      value: 99.0
      window: "1h"
  breach_action: "rollback"
`
	require.NoError(t, os.WriteFile(contractPath, []byte(contractYAML), 0o644))

	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"check", "--contract", contractPath, "--trust", "trusted", "--skip-cost"}, &stdout, &stderr)
	// No evidence → BLOCKED, but contract check should PASS
	assert.Equal(t, mgExitBlock, rc)
	output := stdout.String()
	assert.Contains(t, output, "contract")
}

// ============================================================================
// Dry-run wrapper tests
// ============================================================================

func TestRunMergeGateWithDryRun_OK(t *testing.T) {
	err := runMergeGateWithDryRun([]string{"checks"}, &bytes.Buffer{}, &bytes.Buffer{}, false)
	assert.NoError(t, err)
}

func TestRunMergeGateWithDryRun_InvocationError(t *testing.T) {
	err := runMergeGateWithDryRun([]string{"check", "--trust", "bad"}, &bytes.Buffer{}, &bytes.Buffer{}, false)
	assert.Error(t, err)
	var exitErr errExit
	assert.ErrorAs(t, err, &exitErr)
	assert.Equal(t, mgExitError, exitErr.code)
}

func TestRunMergeGateWithDryRun_BlockedNotError(t *testing.T) {
	// BLOCKED (rc=1) should NOT be treated as error
	err := runMergeGateWithDryRun([]string{"check", "--trust", "trusted", "--skip-contract", "--skip-cost"}, &bytes.Buffer{}, &bytes.Buffer{}, false)
	assert.NoError(t, err, "BLOCKED is a valid gate result, not an invocation error")
}

// ============================================================================
// Report formatting tests
// ============================================================================

func TestPrintMergeGateReport_Allowed(t *testing.T) {
	var buf bytes.Buffer
	report := mergegate.GateReport{
		Decision: mergegate.DecisionAllowed,
		AgentID:  "test-agent",
		Checks: []mergegate.CheckResult{
			{Name: "evidence", Status: mergegate.CheckPass, Reason: "valid bundle"},
			{Name: "consensus", Status: mergegate.CheckPass, Reason: "all agreed"},
		},
	}
	printMergeGateReport(&buf, report)
	output := buf.String()
	assert.Contains(t, output, "ALLOWED")
	assert.Contains(t, output, "test-agent")
}

func TestPrintMergeGateReport_Blocked(t *testing.T) {
	var buf bytes.Buffer
	report := mergegate.GateReport{
		Decision: mergegate.DecisionBlocked,
		AgentID:  "test-agent",
		Checks: []mergegate.CheckResult{
			{Name: "evidence", Status: mergegate.CheckFail, Reason: "missing bundle"},
			{Name: "consensus", Status: mergegate.CheckPass, Reason: "agreed"},
		},
		Blockers: []string{"missing bundle"},
	}
	printMergeGateReport(&buf, report)
	output := buf.String()
	assert.Contains(t, output, "BLOCKED")
	assert.Contains(t, output, "Blockers")
	assert.Contains(t, output, "missing bundle")
}

func TestStatusEmoji(t *testing.T) {
	assert.Equal(t, "✅", statusEmoji(mergegate.CheckPass))
	assert.Equal(t, "❌", statusEmoji(mergegate.CheckFail))
	assert.Equal(t, "⏭️", statusEmoji(mergegate.CheckSkipped))
	assert.Equal(t, "⚠️", statusEmoji(mergegate.CheckWarning))
	assert.Equal(t, "?", statusEmoji(mergegate.CheckStatus("bogus")))
}

// ============================================================================
// Integration: verify the contract is actually parsed and loaded
// ============================================================================

func TestRunMergeGate_Check_ContractParsed(t *testing.T) {
	tmpDir := t.TempDir()
	contractPath := filepath.Join(tmpDir, "contract.yaml")
	contractYAML := `contract:
  name: "test-contract"
  agent: "agent-x"
  merge_commit: "sha123"
  assertions:
    - metric: "latency_p99"
      operator: "lte"
      value: 100
      window: "5m"
  breach_action: "rollback"
`
	require.NoError(t, os.WriteFile(contractPath, []byte(contractYAML), 0o644))

	// We just verify the contract loads without error in a full gate run
	var stdout, stderr bytes.Buffer
	rc := runMergeGate([]string{"check", "--contract", contractPath, "--trust", "veteran", "--skip-cost"}, &stdout, &stderr)
	// Veteran tier + no evidence → evidence check FAILS → BLOCKED
	assert.Equal(t, mgExitBlock, rc)
	// But the contract should have parsed
	_ = verify.BehaviorContract{} // verify import works
}

func TestMergeGateAllChecksCount(t *testing.T) {
	assert.Len(t, mgAllChecks, 5)
}
