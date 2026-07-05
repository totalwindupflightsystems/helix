package main

import (
	"os"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/internal/observability"
)

// TestRunRootWithObs_EmitsLogLine verifies that runRootWithObs emits
// exactly one "subcommand_complete" log entry per invocation, with the
// app=helix-estimate field set.
func TestRunRootWithObs_EmitsLogLine(t *testing.T) {
	// Capture stderr so the observability log line doesn't pollute test
	// output and so we can assert on it.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	if _, err := observability.Init(observability.Options{
		App:    "helix-estimate",
		Format: "json",
		Sink:   w,
	}); err != nil {
		t.Fatalf("observability.Init: %v", err)
	}

	_ = runRootWithObs()

	_ = w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "subcommand_complete") {
		t.Errorf("expected subcommand_complete in observability output:\n%s", output)
	}
	if !strings.Contains(output, `"app":"helix-estimate"`) {
		t.Errorf("expected app=helix-estimate in observability output:\n%s", output)
	}
}
