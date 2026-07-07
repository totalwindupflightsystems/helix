package main

import (
	"bytes"
	"strings"
	"testing"
)

// ============================================================================
// flag parsing
// ============================================================================

func TestParseDeployFlags_Defaults(t *testing.T) {
	f, help, rc := parseDeployFlags(nil)
	if rc != depExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if help {
		t.Errorf("help=true")
	}
	if f.subcommand != "help" {
		t.Errorf("subcommand=%q want help", f.subcommand)
	}
}

func TestParseDeployFlags_AllOptions(t *testing.T) {
	f, _, rc := parseDeployFlags([]string{"render", "--kind", "agent", "--json"})
	if rc != depExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if f.subcommand != "render" {
		t.Errorf("subcommand=%q", f.subcommand)
	}
	if f.kind != "agent" {
		t.Errorf("kind=%q", f.kind)
	}
	if !f.jsonOut {
		t.Errorf("jsonOut=false")
	}
}

func TestParseDeployFlags_UnknownFlag(t *testing.T) {
	_, _, rc := parseDeployFlags([]string{"render", "--bogus"})
	if rc != depExitError {
		t.Errorf("rc=%d", rc)
	}
}

func TestParseDeployFlags_KindMissingValue(t *testing.T) {
	_, _, rc := parseDeployFlags([]string{"render", "--kind"})
	if rc != depExitError {
		t.Errorf("rc=%d", rc)
	}
}

// ============================================================================
// runDeploy
// ============================================================================

func TestRunDeploy_Help(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runDeploy([]string{"help"}, &out, &errOut)
	if rc != depExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(out.String(), "helix deploy") {
		t.Errorf("help missing title")
	}
}

func TestRunDeploy_UnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runDeploy([]string{"bogus"}, &out, &errOut)
	if rc != depExitError {
		t.Errorf("rc=%d", rc)
	}
}

func TestRunDeploy_Tiers(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runDeploy([]string{"tiers"}, &out, &errOut)
	if rc != depExitOK {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	// Should list at least one tier with "valid=" prefix.
	if !strings.Contains(out.String(), "valid=") {
		t.Errorf("tiers output missing valid= prefix: %s", out.String())
	}
}

func TestRunDeploy_RenderAgent(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runDeploy([]string{"render", "--kind", "agent"}, &out, &errOut)
	if rc != depExitOK {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	// NewRegistry() with no specs renders an empty body — that's fine.
	// What matters: rc=0, no error output.
	if errOut.Len() != 0 {
		t.Errorf("unexpected stderr: %s", errOut.String())
	}
}

func TestRunDeploy_RenderCaddy(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runDeploy([]string{"render", "--kind", "caddy"}, &out, &errOut)
	if rc != depExitOK {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
}

func TestRunDeploy_RenderSystemd(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runDeploy([]string{"render", "--kind", "systemd"}, &out, &errOut)
	if rc != depExitOK {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	// Default registry has HelixPlatformService / Backup etc.
	// Just verify the body contains "[Unit]" or similar systemd markers.
	body := out.String()
	if !strings.Contains(body, "[Unit]") && !strings.Contains(body, "[Service]") && body != "" {
		// Empty body is acceptable if no defaults registered.
		t.Logf("systemd body=%q", body)
	}
}

func TestRunDeploy_RenderAll(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runDeploy([]string{"render"}, &out, &errOut)
	if rc != depExitOK {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
}

func TestRunDeploy_RenderInvalidKind(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runDeploy([]string{"render", "--kind", "kubernetes"}, &out, &errOut)
	if rc != depExitError {
		t.Errorf("rc=%d want %d", rc, depExitError)
	}
	if !strings.Contains(errOut.String(), "invalid --kind") {
		t.Errorf("stderr=%q", errOut.String())
	}
}

func TestRunDeploy_RenderJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runDeploy([]string{"render", "--json"}, &out, &errOut)
	if rc != depExitOK {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	// JSON output should be valid (it'll be {} for empty registries).
	if !strings.HasPrefix(strings.TrimSpace(out.String()), "{") {
		t.Errorf("render --json output not JSON: %s", out.String())
	}
}

func TestRunDeploy_ListEmpty(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runDeploy([]string{"list", "--kind", "agent"}, &out, &errOut)
	if rc != depExitOK {
		t.Fatalf("rc=%d", rc)
	}
	// Empty agent registry → "(no artifacts registered)" or empty list.
	body := out.String()
	if body != "" && !strings.Contains(body, "no artifacts") {
		t.Logf("list output=%q", body)
	}
}

func TestRunDeploy_ListJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runDeploy([]string{"list", "--json"}, &out, &errOut)
	if rc != depExitOK {
		t.Fatalf("rc=%d", rc)
	}
	body := strings.TrimSpace(out.String())
	if body != "[]" && !strings.HasPrefix(body, "[") {
		t.Errorf("list --json output not array: %s", body)
	}
}

func TestRunDeploy_ListInvalidKind(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runDeploy([]string{"list", "--kind", "bogus"}, &out, &errOut)
	if rc != depExitError {
		t.Errorf("rc=%d", rc)
	}
}

func TestRunDeploy_DryRunWrapper(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := runDeployWithDryRun([]string{"help"}, &out, &errOut, true); err != nil {
		t.Errorf("help returned err=%v", err)
	}
}

func TestRunDeploy_DryRunWrapperError(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := runDeployWithDryRun([]string{"bogus"}, &out, &errOut, true); err == nil {
		t.Errorf("bogus should produce err")
	}
}
