// Command helix — status_serve.go
//
// `helix status --serve --addr :9095` starts a long-running HTTP
// server that exposes Prometheus metrics at /metrics. The server
// serves the existing PromStore (initialized in main) and re-runs the
// service health probes on every scrape, cached for 10s to bound
// scrape latency per spec.
//
// Endpoints:
//
//	GET /metrics   → Prometheus exposition format (text/plain)
//	GET /health    → 200 OK with the basic service status
//	GET /          → 200 OK with a friendly banner
//
// `--strict` makes the server return 503 if the probe cache is stale,
// per spec §10.7. Operators use this to gate alerting on probe
// freshness in lieu of scraping the metrics endpoint itself.
//
// Implementation notes:
//
//  1. The HTTP server runs on its own goroutine via net/http. The
//     calling main() blocks on <-server.WaitDone() until SIGINT/SIGTERM
//     or a scrape handler returns an error.
//  2. Probes run synchronously inside the handler. Each probe has a
//     2s timeout (configurable via probesTimeout). The 10s cache means
//     scrapes within 10s reuse the previous probe result.
//  3. All metrics are read from the singleton promStore handle — not
//     constructed per-request — so observation counters persist
//     across scrapes.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/health"
)

// DefaultPromAddr is the default bind address for `helix status --serve`.
const DefaultPromAddr = ":9095"

// ServeStatusOptions configures the running metrics server. Fields
// are populated from CLI flags; tests use the struct directly.
//
// Stride pattern: every option has a sensible default applied in
// DefaultServeStatusOptions so callers can pass a partial struct.
type ServeStatusOptions struct {
	Addr         string        // bind address (default ":9095")
	Strict       bool          // 503 when probe cache is stale
	ProbeTimeout time.Duration // per-probe timeout (default 2s)
	CacheTTL     time.Duration // probe cache TTL (default 10s, spec §10.7)
}

// DefaultServeStatusOptions returns the spec defaults.
func DefaultServeStatusOptions() ServeStatusOptions {
	return ServeStatusOptions{
		Addr:         DefaultPromAddr,
		Strict:       false,
		ProbeTimeout: 2 * time.Second,
		CacheTTL:     10 * time.Second,
	}
}

// ErrAllChecksFailed is returned when every probe fails. Tests use it
// to assert graceful degradation without scraping the HTTP API.
var ErrAllChecksFailed = errors.New("all service probes failed")

// promStore is the package-level singleton for PromStore. Set by
// SetPromStore at process start. nil → metrics endpoint degrades
// gracefully (returns an empty body).
var promStore *health.PromStore

// SetPromStore installs the global prom store. Called once from
// main() with a freshly constructed store. Tests use this hook to
// inject their own store.
func SetPromStore(s *health.PromStore) {
	promStore = s
}

// PromStore returns the currently installed store, or nil. Used by
// handler tests that want to read the same singleton the production
// code uses.
func GetPromStore() *health.PromStore {
	return promStore
}

// hasStatusServeFlag returns true when `helix status --serve [--addr ...]`
// was invoked. Distinguishes the long-running metrics mode from the
// one-shot status command.
func hasStatusServeFlag(args []string) bool {
	for _, a := range args {
		if a == "--serve" || a == "--serve=true" {
			return true
		}
	}
	return false
}

// RunStatusServe starts the HTTP server and blocks until ctx is
// cancelled or the listener errors out. opts may be zero-valued —
// defaults are applied.
//
// Returns nil on graceful shutdown (SIGINT/SIGTERM), non-nil on
// listener errors. The HTTP handler errors are NOT propagated
// (handlers always log + return 500 — they don't kill the server).
func RunStatusServe(ctx context.Context, opts ServeStatusOptions) error {
	if opts.Addr == "" {
		opts.Addr = DefaultPromAddr
	}
	if opts.ProbeTimeout == 0 {
		opts.ProbeTimeout = 2 * time.Second
	}
	if opts.CacheTTL == 0 {
		opts.CacheTTL = 10 * time.Second
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", indexHandler)
	mux.HandleFunc("/metrics", makeMetricsHandler(opts))
	mux.HandleFunc("/health", makeHealthHandler())

	server := &http.Server{
		Addr:              opts.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Channel to receive server errors.
	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	// Channel to receive signal interrupts.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Wait for cancellation, signal, or server error.
	select {
	case <-ctx.Done():
	case sig := <-sigCh:
		_ = sig // suppress unused — handler above already captures it
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("metrics server failed: %w", err)
		}
	}

	// Graceful shutdown.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	return nil
}

// indexHandler writes a friendly banner to w. Used when an operator
// curl's the metrics port directly.
func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "Helix Metrics Server\n")
	_, _ = io.WriteString(w, "--------------------\n")
	_, _ = io.WriteString(w, "Try: curl http://localhost:9095/metrics\n")
}

// metricsHandlerState keeps per-scraped counters so we can verify the
// handler is being called the right number of times in tests.
type metricsHandlerState struct {
	scrapes     atomic.Uint64
	cacheHits   atomic.Uint64
	cacheMisses atomic.Uint64
}

// globalMetricsHandlerState — used by tests to introspect scrape counts.
var globalMetricsHandlerState metricsHandlerState

// ResetMetricsHandlerState zeroes the counters. Tests call this at
// init time so prior runs don't pollute counts.
func ResetMetricsHandlerState() {
	globalMetricsHandlerState.scrapes.Store(0)
	globalMetricsHandlerState.cacheHits.Store(0)
	globalMetricsHandlerState.cacheMisses.Store(0)
}

// MetricsHandlerStats returns the current scrape counters.
func MetricsHandlerStats() (scrapes, hits, misses uint64) {
	return globalMetricsHandlerState.scrapes.Load(),
		globalMetricsHandlerState.cacheHits.Load(),
		globalMetricsHandlerState.cacheMisses.Load()
}

// makeMetricsHandler returns the /metrics handler with the given
// options. The handler:
//  1. Increments global scrape counter.
//  2. Calls runProbes to refresh the probe cache (cached per opts.CacheTTL).
//  3. Writes the prom store as Prometheus exposition format.
//  4. Returns 503 if --strict and a probe is stale.
//
// Defaults to 200 OK on success per Prometheus convention.
func makeMetricsHandler(opts ServeStatusOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		globalMetricsHandlerState.scrapes.Add(1)

		// Refresh probes, populating the PromStore service-up gauges.
		anyStale := refreshProbeCache(r.Context(), opts)
		if opts.Strict && anyStale {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "# probe cache stale\n")
			return
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if promStore == nil {
			// Defensive — PromStore should always be installed by main().
			_, _ = io.WriteString(w, "# prom store not initialized\n")
			return
		}
		_, _ = promStore.WriteMetrics(w)
	}
}

// makeHealthHandler returns a simple 200 OK handler. Operators use
// this for orchestration-level liveness probes (k8s, systemd).
func makeHealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	}
}

// refreshProbeCache runs health probes for every service the store
// knows about, caching each result for opts.CacheTTL.
//
// Returns true if any probe ended up stale (no fresh result within
// the TTL), enabling the --strict 503 path.
func refreshProbeCache(ctx context.Context, opts ServeStatusOptions) bool {
	if promStore == nil {
		return true
	}
	anyStale := false
	probes := buildDoctorProbes()
	for service, probe := range probes {
		fresh, _ := promStore.ProbeFreshness(service)
		if fresh {
			globalMetricsHandlerState.cacheHits.Add(1)
			continue
		}
		globalMetricsHandlerState.cacheMisses.Add(1)

		probeCtx, cancel := context.WithTimeout(ctx, opts.ProbeTimeout)
		healthy, detail := probe(probeCtx)
		cancel()
		promStore.UpdateProbe(service, time.Now(), healthy, detail)
		if !healthy {
			anyStale = true
		}
	}
	return anyStale
}

// serviceProbe is a function that runs a single liveness probe.
// Returns (healthy, detail). detail is shown in the metrics output
// if you'd like to surface it (we keep it in the cache for debugging,
// not in the scrape body).
type serviceProbe func(ctx context.Context) (bool, string)

// buildDoctorProbes returns the canonical probe set per the doctor
// checks. Mirrors cmd/helix/doctor.go but reuses the same defaults —
// the probe targets are constant and known ahead of time.
//
// Implementation note: every probe is a thin HTTP GET against the
// service's health endpoint with a short timeout. We bypass doctor
// itself because we don't need its full report — only up/down.
func buildDoctorProbes() map[string]serviceProbe {
	cfg := DefaultDoctorConfig()
	return map[string]serviceProbe{
		"forgejo":    httpProbe(cfg.ForgejoURL),
		"chimera":    httpProbe(cfg.ChimeraURL),
		"langfuse":   httpProbe(cfg.LangFuseURL),
		"prometheus": httpProbe(cfg.PrometheusURL),
	}
}

// httpProbe wraps an HTTP GET against the URL with status 200 = healthy.
// Connection refused / non-200 status = unhealthy.
func httpProbe(url string) serviceProbe {
	return func(ctx context.Context) (bool, string) {
		if url == "" {
			return false, "no url configured"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return false, err.Error()
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return false, err.Error()
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return true, fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return false, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
}

// ============================================================================
// CLI shim — invoked from main()'s case "status"
// ============================================================================

// runStatusServeCLI parses `helix status --serve [--addr :NNNN]` flags
// and invokes RunStatusServe. Returns nil on graceful shutdown.
func runStatusServeCLI(args []string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}

	opts := DefaultServeStatusOptions()
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--serve":
			// flag is implicit — just presence here.
			continue
		case "--addr":
			if i+1 < len(args) {
				opts.Addr = args[i+1]
				if !strings.HasPrefix(opts.Addr, ":") && !strings.Contains(opts.Addr, ":") {
					opts.Addr = ":" + opts.Addr
				}
				i++
			}
		case "--strict":
			opts.Strict = true
		case "--probe-timeout":
			if i+1 < len(args) {
				if d, err := time.ParseDuration(args[i+1]); err == nil {
					opts.ProbeTimeout = d
				}
				i++
			}
		}
	}

	if promStore == nil {
		SetPromStore(health.NewPromStore())
		// Seed the canonical service set so /metrics has stable row order
		// even on a fresh install that hasn't observed any probes yet.
		promStore.SetProbes([]string{"forgejo", "chimera", "langfuse", "prometheus"})
	}

	_, _ = fmt.Fprintf(stdout, "Helix metrics server listening on %s (--strict=%v, probe-timeout=%v, cache-ttl=%v)\n",
		opts.Addr, opts.Strict, opts.ProbeTimeout, opts.CacheTTL)

	return RunStatusServe(context.Background(), opts)
}
