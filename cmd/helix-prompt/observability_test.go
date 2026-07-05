package main

import (
	"os"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/internal/observability"
)

func TestRunRootWithObs_EmitsLogLine(t *testing.T) {
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	if _, err := observability.Init(observability.Options{
		App:    "helix-prompt",
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
	if !strings.Contains(output, `"app":"helix-prompt"`) {
		t.Errorf("expected app=helix-prompt in observability output:\n%s", output)
	}
}
