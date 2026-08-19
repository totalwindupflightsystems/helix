// Package integration provides end-to-end integration test harnesses for the
// Helix platform. These tests exercise the full agent lifecycle against real
// local services (Forgejo, Chimera). They skip gracefully when the required
// service is unreachable, so they pass cleanly in environments without one
// (e.g. CI) and run for real against a live local Forgejo.
//
// Usage:
//
//	go test -short -count=1 ./pkg/integration/...   # run E2E suite (skips if Forgejo unreachable)
//	go test -count=1 ./pkg/integration/...          # run full integration suite (TestFullLoop; skips if Forgejo unreachable)
//
// Environment variables:
//
//	GOAWAY=1 — skip real network calls even when not in -short mode
//	FORGEJO_URL — override default http://localhost:3030
//	CHIMERA_URL — override default http://localhost:8765
package integration
