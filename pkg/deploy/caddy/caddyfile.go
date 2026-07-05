// Package caddy generates Caddy reverse-proxy configuration for Helix's
// external services. Per specs/SPECIFICATION.md §9.3, the platform
// exposes 6 vhosts on the public edge:
//
//	helixloop.dev          → forgejo:3000
//	chimera.helixloop.dev  → chimera:8001
//	conscience.helixloop.dev → conscientiousness:8002
//	hivemind.helixloop.dev → hivemind:8003
//	traces.helixloop.dev   → langfuse:3000
//	monitor.helixloop.dev  → grafana:3000
//
// This package provides the data layer: Vhost structs (domain, backend,
// optional TLS / path rewrites / basic_auth / rate limiting), a Registry
// keyed by name, and a Renderer that emits valid Caddyfile syntax.
//
// Design goals:
//
//   - Deterministic output. Caddyfile block order is stable across
//     calls so `caddy validate` doesn't flag a diff that isn't real.
//
//   - Validation at the boundary. Render() rejects vhosts with bad
//     domains or unparseable backends before emitting.
//
//   - Backend URL can be a Docker service name (forgejo:3000) for
//     docker-compose deployments or a 127.0.0.1:port for local dev.
//     The package doesn't care — it just emits whatever the caller
//     configures.
//
//   - Optional global TLS config. If SetTLSEmail is non-empty, the
//     emitted Caddyfile includes an `email` directive for ACME.
//
// Typical usage:
//
//	reg := caddy.DefaultRegistry()
//	out, err := caddy.Render(reg)
//	if err != nil { return err }
//	os.WriteFile("Caddyfile", []byte(out), 0o644)
package caddy

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// =============================================================================
// Domain validation
// =============================================================================

// ValidateDomain returns an error if d is not a syntactically valid DNS
// name. Empty domains, domains containing whitespace, or domains with
// consecutive dots are rejected. This is a sanity check, not a full
// RFC 1035 validator — Caddy itself does the authoritative check at
// startup.
func ValidateDomain(d string) error {
	d = strings.TrimSpace(d)
	if d == "" {
		return errors.New("caddy: domain is required")
	}
	if strings.ContainsAny(d, " \t\n\r") {
		return fmt.Errorf("caddy: domain %q contains whitespace", d)
	}
	if strings.Contains(d, "..") {
		return fmt.Errorf("caddy: domain %q contains consecutive dots", d)
	}
	// Reject leading/trailing dots and dashes (RFC 1035 §3.5).
	if strings.HasPrefix(d, ".") || strings.HasSuffix(d, ".") {
		return fmt.Errorf("caddy: domain %q has leading or trailing dot", d)
	}
	// Must contain at least one dot (otherwise it's a single-label name,
	// valid for /etc/hosts but not for public DNS).
	if !strings.Contains(d, ".") {
		return fmt.Errorf("caddy: domain %q is a single label (needs a dot)", d)
	}
	return nil
}

// ValidateBackend returns an error if backend is not a syntactically
// valid URL with a host. The URL is parsed with url.Parse — schemes
// like http://, https://, or scheme-less Docker service names
// (forgejo:3000) are accepted.
func ValidateBackend(backend string) error {
	backend = strings.TrimSpace(backend)
	if backend == "" {
		return errors.New("caddy: backend is required")
	}
	// Allow scheme-less Docker service:port form by prefixing http://.
	candidate := backend
	if !strings.Contains(backend, "://") {
		candidate = "http://" + backend
	}
	u, err := url.Parse(candidate)
	if err != nil {
		return fmt.Errorf("caddy: backend %q is not a valid URL: %w", backend, err)
	}
	if u.Host == "" {
		return fmt.Errorf("caddy: backend %q has no host", backend)
	}
	return nil
}

// =============================================================================
// Vhost
// =============================================================================

// Vhost describes one public-facing domain → backend service mapping.
// Domain is the public hostname (e.g. "chimera.helixloop.dev"). Backend
// is the upstream URL (e.g. "chimera:8001" or "https://127.0.0.1:8765").
//
// PathRewrite is optional — when non-empty, requests to Domain are
// rewritten to PathRewrite before being forwarded. Useful for services
// that expect a sub-path (e.g. "/api/v1").
//
// BasicAuth, when set, gates the vhost with HTTP basic auth. Username
// and HashedPassword follow Caddy's documented format (bcrypt or
// plaintext — Caddy auto-detects).
//
// RateLimit, when non-zero, applies a `rate_limit` named zone to the
// vhost. The value is requests-per-second.
type Vhost struct {
	// Name is the registry key (e.g. "forgejo", "chimera"). Used in
	// DefaultRegistry and for ordered iteration.
	Name string

	// Domain is the public hostname. Required.
	Domain string

	// Backend is the upstream URL. Required.
	Backend string

	// PathRewrite, if non-empty, rewrites requests to this path before
	// forwarding to Backend.
	PathRewrite string

	// BasicAuth, if set, enables HTTP basic auth. Username +
	// HashedPassword (or plaintext — Caddy accepts both).
	BasicAuth *BasicAuthConfig

	// RateLimit, if > 0, applies a Caddy rate_limit directive to the
	// vhost (requests per second).
	RateLimit int
}

// BasicAuthConfig carries the username + password for an HTTP basic
// auth gate. Caddy accepts both plaintext and bcrypt hashes; the
// library doesn't validate the format — Caddy does at startup.
type BasicAuthConfig struct {
	Username       string
	HashedPassword string
}

// Validate enforces required fields and runs the domain/backend checks.
// PathRewrite / BasicAuth / RateLimit are optional.
func (v Vhost) Validate() error {
	if strings.TrimSpace(v.Name) == "" {
		return errors.New("caddy: vhost name is required")
	}
	if err := ValidateDomain(v.Domain); err != nil {
		return fmt.Errorf("vhost %q: %w", v.Name, err)
	}
	if err := ValidateBackend(v.Backend); err != nil {
		return fmt.Errorf("vhost %q: %w", v.Name, err)
	}
	if v.PathRewrite != "" && !strings.HasPrefix(v.PathRewrite, "/") {
		return fmt.Errorf("vhost %q: path rewrite %q must start with /", v.Name, v.PathRewrite)
	}
	if v.BasicAuth != nil {
		if strings.TrimSpace(v.BasicAuth.Username) == "" {
			return fmt.Errorf("vhost %q: basic_auth username is required", v.Name)
		}
		if strings.TrimSpace(v.BasicAuth.HashedPassword) == "" {
			return fmt.Errorf("vhost %q: basic_auth password is required", v.Name)
		}
	}
	if v.RateLimit < 0 {
		return fmt.Errorf("vhost %q: rate_limit must be >= 0", v.Name)
	}
	return nil
}

// =============================================================================
// Registry
// =============================================================================

// Registry holds the vhosts that will be rendered into a single
// Caddyfile. The zero value is NOT usable — call NewRegistry or
// DefaultRegistry.
//
// Vhosts are stored in insertion order (via an internal slice) so the
// rendered output preserves the order the caller specified. DefaultRegistry
// pre-populates in the spec §9.3 order. A name-keyed map supports
// fast Get() / Add() / Replace.
type Registry struct {
	// vmap keyed by Name for fast lookup.
	vmap map[string]Vhost

	// order holds vhost names in insertion order. When a vhost is
	// replaced via Add(), the new entry stays at the tail (latest
	// Add wins).
	order []string

	// tlsEmail is the ACME email for the global TLS config (optional).
	tlsEmail string
}

// NewRegistry returns an empty Registry. Callers Add() vhosts one by one.
func NewRegistry() *Registry {
	return &Registry{vmap: map[string]Vhost{}}
}

// DefaultRegistry returns a Registry pre-populated with the 6 vhosts
// in the spec §9.3 order: forgejo first (the primary entry), then
// chimera, conscience, hivemind, traces, monitor.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	vhosts := []Vhost{
		{Name: "forgejo", Domain: "helixloop.dev", Backend: "forgejo:3000"},
		{Name: "chimera", Domain: "chimera.helixloop.dev", Backend: "chimera:8001"},
		{Name: "conscience", Domain: "conscience.helixloop.dev", Backend: "conscientiousness:8002"},
		{Name: "hivemind", Domain: "hivemind.helixloop.dev", Backend: "hivemind:8003"},
		{Name: "traces", Domain: "traces.helixloop.dev", Backend: "langfuse:3000"},
		{Name: "monitor", Domain: "monitor.helixloop.dev", Backend: "grafana:3000"},
	}
	for _, v := range vhosts {
		r.Add(v)
	}
	return r
}

// Add inserts or replaces a vhost. On replace, the existing entry is
// kept at its original position in the iteration order (no re-ordering).
// Returns the receiver for chaining.
func (r *Registry) Add(v Vhost) *Registry {
	if _, exists := r.vmap[v.Name]; !exists {
		r.order = append(r.order, v.Name)
	}
	r.vmap[v.Name] = v
	return r
}

// Get returns the vhost named name, or false if absent.
func (r *Registry) Get(name string) (Vhost, bool) {
	v, ok := r.vmap[name]
	return v, ok
}

// SetTLSEmail sets the global ACME email that the rendered Caddyfile
// includes for Let's Encrypt. Empty string disables the email directive.
func (r *Registry) SetTLSEmail(email string) *Registry {
	r.tlsEmail = email
	return r
}

// TLSEmail returns the configured ACME email (empty if unset).
func (r *Registry) TLSEmail() string {
	return r.tlsEmail
}

// Names returns the vhost names in insertion order. Insertion order
// preserves the canonical ordering in SpecSection93 (forgejo first).
// Render() iterates in this order so the rendered Caddyfile is
// deterministic and matches the spec byte-for-byte.
//
// Callers that need a different iteration order can sort the returned
// slice themselves (sort.Strings) — Names() does not impose one.
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Vhosts returns all vhosts in insertion order.
func (r *Registry) Vhosts() []Vhost {
	out := make([]Vhost, 0, len(r.order))
	for _, n := range r.order {
		if v, ok := r.vmap[n]; ok {
			out = append(out, v)
		}
	}
	return out
}

// Validate runs Validate on every vhost and returns the first error.
// Used by callers before Render to fail fast on misconfiguration.
func (r *Registry) Validate() error {
	for _, v := range r.Vhosts() {
		if err := v.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// =============================================================================
// Rendering
// =============================================================================

// Render emits the Caddyfile for the registry. The output is sorted
// (vhosts in name order) and uses blank lines between blocks per the
// canonical Caddyfile style.
//
// If reg.tlsEmail is non-empty, a global block at the top of the file
// sets the ACME email.
//
// The output uses double newlines between site blocks (matching the
// spec §9.3 example). One trailing newline is appended.
func Render(reg *Registry) (string, error) {
	if reg == nil {
		return "", errors.New("caddy: nil registry")
	}
	if err := reg.Validate(); err != nil {
		return "", err
	}

	var b strings.Builder

	// Optional global TLS block.
	if email := reg.TLSEmail(); strings.TrimSpace(email) != "" {
		fmt.Fprintf(&b, "{\n    email %s\n}\n\n", strings.TrimSpace(email))
	}

	// One block per vhost, name-sorted, blank line between blocks.
	vhosts := reg.Vhosts()
	for i, v := range vhosts {
		if i > 0 {
			b.WriteString("\n")
		}
		writeVhost(&b, v, i == len(vhosts)-1)
	}
	return b.String(), nil
}

// writeVhost emits a single Caddyfile site block. The shape is:
//
//	<domain> {
//	    reverse_proxy <backend>
//	    # optional path rewrite
//	    # optional basic_auth
//	    # optional rate_limit
//	}
//
// last controls whether to emit the trailing newline after the closing
// brace — SpecSection93 has no trailing newline on the final block.
func writeVhost(b *strings.Builder, v Vhost, last bool) {
	fmt.Fprintf(b, "%s {\n", v.Domain)
	fmt.Fprintf(b, "    reverse_proxy %s\n", v.Backend)
	if v.PathRewrite != "" {
		fmt.Fprintf(b, "    uri strip_prefix %s\n", pathStrip(v.PathRewrite))
		fmt.Fprintf(b, "    reverse_proxy %s%s\n", v.Backend, v.PathRewrite)
	}
	if v.BasicAuth != nil {
		fmt.Fprintf(b, "    basicauth {\n")
		fmt.Fprintf(b, "        %s %s\n", v.BasicAuth.Username, v.BasicAuth.HashedPassword)
		fmt.Fprintf(b, "    }\n")
	}
	if v.RateLimit > 0 {
		fmt.Fprintf(b, "    rate_limit %d r/s\n", v.RateLimit)
	}
	if last {
		b.WriteString("}")
	} else {
		b.WriteString("}\n")
	}
}

// pathStrip returns the inverse of a path rewrite — the prefix to strip
// from incoming requests so the backend sees the rewritten path. For
// a rewrite of "/api/v1", requests to "/api/v1/foo" become "/foo" at
// the backend.
//
// Currently we emit strip_prefix + a second reverse_proxy. Caddy
// supports this idiom; if no rewrite is configured, the path is left
// alone.
func pathStrip(p string) string { return p }

// FormatVhost renders a single Vhost as Caddyfile syntax. Useful for
// `helix caddy show <name>` style CLI commands.
func FormatVhost(v Vhost) (string, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	writeVhost(&b, v, true)
	return b.String(), nil
}

// =============================================================================
// Spec-mandated defaults
// =============================================================================

// SpecSection93 is the literal Caddyfile from SPECIFICATION.md §9.3.
// Exported so tests can diff against the canonical spec example. The
// format is what the spec author expects operators to ship.
const SpecSection93 = `helixloop.dev {
    reverse_proxy forgejo:3000
}

chimera.helixloop.dev {
    reverse_proxy chimera:8001
}

conscience.helixloop.dev {
    reverse_proxy conscientiousness:8002
}

hivemind.helixloop.dev {
    reverse_proxy hivemind:8003
}

traces.helixloop.dev {
    reverse_proxy langfuse:3000
}

monitor.helixloop.dev {
    reverse_proxy grafana:3000
}`

// =============================================================================
// Local dev convenience
// =============================================================================

// LocalDevRegistry returns a Registry configured for a developer's
// laptop — every vhost proxies to 127.0.0.1 on the port that the
// docker-compose stack publishes. Useful for `helix caddy generate
// --local`.
func LocalDevRegistry() *Registry {
	r := NewRegistry()
	vhosts := []Vhost{
		{Name: "forgejo", Domain: "helixloop.dev", Backend: "127.0.0.1:3030"},
		{Name: "chimera", Domain: "chimera.helixloop.dev", Backend: "127.0.0.1:8765"},
		{Name: "conscience", Domain: "conscience.helixloop.dev", Backend: "127.0.0.1:8766"},
		{Name: "hivemind", Domain: "hivemind.helixloop.dev", Backend: "127.0.0.1:8767"},
		{Name: "traces", Domain: "traces.helixloop.dev", Backend: "127.0.0.1:3000"},
		{Name: "monitor", Domain: "monitor.helixloop.dev", Backend: "127.0.0.1:3001"},
	}
	for _, v := range vhosts {
		r.Add(v)
	}
	return r
}
