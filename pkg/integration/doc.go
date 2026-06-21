// Package integration provides end-to-end integration test harnesses for the
// Helix platform. These tests exercise the full agent lifecycle against real
// local services (Forgejo, Chimera) and are guarded by testing.Short() so they
// are skipped during normal unit test runs.
//
// Usage:
//
//	go test -short -count=1 ./pkg/integration/...   # skip integration tests
//	go test -count=1 ./pkg/integration/...          # run integration tests
//
// Environment variables:
//
//	GOAWAY=1 — skip real network calls even when not in -short mode
//	FORGEJO_URL — override default http://localhost:3000
//	CHIMERA_URL — override default http://localhost:8765
package integration
