package caddy

import (
	"sort"
	"strings"
	"testing"
)

// =============================================================================
// Domain validation
// =============================================================================

func TestValidateDomain(t *testing.T) {
	good := []string{
		"helixloop.dev",
		"chimera.helixloop.dev",
		"a.b.c.d.example.com",
	}
	for _, d := range good {
		t.Run("good/"+d, func(t *testing.T) {
			if err := ValidateDomain(d); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}

	bad := []struct {
		in  string
		why string
	}{
		{"", "empty"},
		{"   ", "whitespace"},
		{"localhost", "single label"},
		{".helixloop.dev", "leading dot"},
		{"helixloop.dev.", "trailing dot"},
		{"chimera..helixloop.dev", "consecutive dots"},
		{"chimera helix.dev", "space"},
		{"chimera\nhelix.dev", "newline"},
	}
	for _, c := range bad {
		t.Run("bad/"+c.why, func(t *testing.T) {
			if err := ValidateDomain(c.in); err == nil {
				t.Errorf("expected error for %q", c.in)
			}
		})
	}
}

// =============================================================================
// Backend validation
// =============================================================================

func TestValidateBackend(t *testing.T) {
	good := []string{
		"forgejo:3000",
		"127.0.0.1:3030",
		"http://forgejo:3000",
		"https://forgejo.example.com",
		"chimera:8001",
	}
	for _, b := range good {
		t.Run("good/"+b, func(t *testing.T) {
			if err := ValidateBackend(b); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}

	bad := []struct {
		in  string
		why string
	}{
		{"", "empty"},
		{"   ", "whitespace"},
		{"not a url", "spaces"},
		{"://missing-scheme", "missing host"},
	}
	for _, c := range bad {
		t.Run("bad/"+c.why, func(t *testing.T) {
			if err := ValidateBackend(c.in); err == nil {
				t.Errorf("expected error for %q", c.in)
			}
		})
	}
}

// =============================================================================
// Vhost validation
// =============================================================================

func TestVhost_Validate(t *testing.T) {
	good := Vhost{
		Name:    "forgejo",
		Domain:  "helixloop.dev",
		Backend: "forgejo:3000",
	}
	if err := good.Validate(); err != nil {
		t.Errorf("good vhost: %v", err)
	}

	withAuth := good
	withAuth.BasicAuth = &BasicAuthConfig{
		Username:       "alice",
		HashedPassword: "plaintext-or-bcrypt",
	}
	if err := withAuth.Validate(); err != nil {
		t.Errorf("with-auth vhost: %v", err)
	}

	withRewrite := good
	withRewrite.PathRewrite = "/api/v1"
	if err := withRewrite.Validate(); err != nil {
		t.Errorf("with-rewrite vhost: %v", err)
	}

	withRate := good
	withRate.RateLimit = 100
	if err := withRate.Validate(); err != nil {
		t.Errorf("with-rate vhost: %v", err)
	}

	cases := []struct {
		name string
		in   Vhost
		want string
	}{
		{"empty name", Vhost{Domain: "x.example.com", Backend: "x:1"}, "name is required"},
		{"bad domain", Vhost{Name: "x", Domain: "no-dot", Backend: "x:1"}, "single label"},
		{"bad backend", Vhost{Name: "x", Domain: "x.example.com", Backend: ""}, "backend is required"},
		{"rewrite missing slash", Vhost{Name: "x", Domain: "x.example.com", Backend: "x:1", PathRewrite: "noslash"}, "must start with /"},
		{"basic_auth no user", Vhost{Name: "x", Domain: "x.example.com", Backend: "x:1", BasicAuth: &BasicAuthConfig{HashedPassword: "p"}}, "username is required"},
		{"basic_auth no pass", Vhost{Name: "x", Domain: "x.example.com", Backend: "x:1", BasicAuth: &BasicAuthConfig{Username: "u"}}, "password is required"},
		{"negative rate", Vhost{Name: "x", Domain: "x.example.com", Backend: "x:1", RateLimit: -1}, "rate_limit must be >= 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.in.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err.Error(), c.want)
			}
		})
	}
}

// =============================================================================
// Registry
// =============================================================================

func TestRegistry_AddGet(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Get("nope"); ok {
		t.Error("expected empty registry")
	}

	r.Add(Vhost{Name: "a", Domain: "a.example.com", Backend: "a:1"})
	v, ok := r.Get("a")
	if !ok || v.Name != "a" {
		t.Errorf("Get(a): %+v ok=%v", v, ok)
	}

	// Replace.
	r.Add(Vhost{Name: "a", Domain: "a2.example.com", Backend: "a:2"})
	v, _ = r.Get("a")
	if v.Domain != "a2.example.com" {
		t.Errorf("replace failed: %q", v.Domain)
	}
}

func TestRegistry_Names_PreserveInsertionOrder(t *testing.T) {
	r := NewRegistry()
	r.Add(Vhost{Name: "zebra", Domain: "z.example.com", Backend: "z:1"})
	r.Add(Vhost{Name: "alpha", Domain: "a.example.com", Backend: "a:1"})
	r.Add(Vhost{Name: "mike", Domain: "m.example.com", Backend: "m:1"})

	// Names() returns insertion order — the canonical SpecSection93
	// ordering is set by Add() in DefaultRegistry(). Callers that want
	// alphabetical output sort the returned slice themselves.
	names := r.Names()
	want := []string{"zebra", "alpha", "mike"}
	if len(names) != len(want) {
		t.Fatalf("got %v want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d]=%q want %q", i, names[i], want[i])
		}
	}
}

func TestRegistry_Names_Sorted_ReturnsAlphabetical(t *testing.T) {
	// Sanity test for callers that DO want alphabetical output:
	// NamesSorted() (new helper, identical to sort.Strings on Names()).
	// We don't add a helper — we verify the well-known idiom.
	r := NewRegistry()
	r.Add(Vhost{Name: "zebra", Domain: "z.example.com", Backend: "z:1"})
	r.Add(Vhost{Name: "alpha", Domain: "a.example.com", Backend: "a:1"})
	r.Add(Vhost{Name: "mike", Domain: "m.example.com", Backend: "m:1"})

	names := append([]string(nil), r.Names()...)
	sort.Strings(names)
	want := []string{"alpha", "mike", "zebra"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("sorted[%d]=%q want %q", i, names[i], want[i])
		}
	}
}

func TestRegistry_Vhosts_OrderMatchesNames(t *testing.T) {
	r := NewRegistry()
	r.Add(Vhost{Name: "z", Domain: "z.example.com", Backend: "z:1"})
	r.Add(Vhost{Name: "a", Domain: "a.example.com", Backend: "a:1"})
	vhosts := r.Vhosts()
	if vhosts[0].Name != "z" || vhosts[1].Name != "a" {
		t.Errorf("Vhosts()=%v want [z, a]", []string{vhosts[0].Name, vhosts[1].Name})
	}
}

func TestRegistry_Validate_FirstErrorWins(t *testing.T) {
	r := NewRegistry()
	r.Add(Vhost{Name: "good", Domain: "a.example.com", Backend: "a:1"})
	r.Add(Vhost{Name: "bad", Domain: "nodot", Backend: "b:1"})
	err := r.Validate()
	if err == nil {
		t.Fatal("expected error from registry validation")
	}
	if !strings.Contains(err.Error(), "single label") {
		t.Errorf("expected 'single label' error, got %v", err)
	}
}

func TestRegistry_SetTLSEmail(t *testing.T) {
	r := NewRegistry()
	r.SetTLSEmail("ops@example.com")
	if r.TLSEmail() != "ops@example.com" {
		t.Errorf("TLSEmail()=%q", r.TLSEmail())
	}
}

// =============================================================================
// DefaultRegistry — spec §9.3 conformance
// =============================================================================

func TestDefaultRegistry_HasAllSixVhosts(t *testing.T) {
	r := DefaultRegistry()
	for _, name := range []string{"forgejo", "chimera", "conscience", "hivemind", "traces", "monitor"} {
		if _, ok := r.Get(name); !ok {
			t.Errorf("DefaultRegistry missing %q", name)
		}
	}
}

func TestDefaultRegistry_Backends(t *testing.T) {
	cases := map[string]string{
		"forgejo":    "forgejo:3000",
		"chimera":    "chimera:8001",
		"conscience": "conscientiousness:8002",
		"hivemind":   "hivemind:8003",
		"traces":     "langfuse:3000",
		"monitor":    "grafana:3000",
	}
	r := DefaultRegistry()
	for name, want := range cases {
		v, _ := r.Get(name)
		if v.Backend != want {
			t.Errorf("%s backend=%q want %q", name, v.Backend, want)
		}
	}
}

// =============================================================================
// Render
// =============================================================================

func TestRender_DefaultRegistry_MatchesSpec(t *testing.T) {
	out, err := Render(DefaultRegistry())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if out != SpecSection93 {
		t.Errorf("Render(DefaultRegistry) does not match spec §9.3.\nGot:\n%s\nWant:\n%s", out, SpecSection93)
	}
}

func TestRender_NilRegistry(t *testing.T) {
	_, err := Render(nil)
	if err == nil {
		t.Fatal("expected error for nil registry")
	}
}

func TestRender_ValidationFails(t *testing.T) {
	r := NewRegistry()
	r.Add(Vhost{Name: "x", Domain: "nodot", Backend: "x:1"})
	_, err := Render(r)
	if err == nil {
		t.Fatal("expected validation error from Render")
	}
}

func TestRender_WithTLSEmail(t *testing.T) {
	r := DefaultRegistry()
	r.SetTLSEmail("ops@example.com")
	out, err := Render(r)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.HasPrefix(out, "{\n    email ops@example.com\n}\n\n") {
		t.Errorf("expected TLS email block at top, got:\n%s", out)
	}
}

func TestRender_WithBasicAuth(t *testing.T) {
	r := NewRegistry()
	r.Add(Vhost{
		Name:    "secret",
		Domain:  "secret.example.com",
		Backend: "secret:9000",
		BasicAuth: &BasicAuthConfig{
			Username:       "admin",
			HashedPassword: "plaintext-or-bcrypt",
		},
	})
	out, err := Render(r)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"basicauth {", "admin plaintext-or-bcrypt", "reverse_proxy secret:9000"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

func TestRender_WithRateLimit(t *testing.T) {
	r := NewRegistry()
	r.Add(Vhost{Name: "x", Domain: "x.example.com", Backend: "x:1", RateLimit: 50})
	out, err := Render(r)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "rate_limit 50 r/s") {
		t.Errorf("expected rate_limit directive, got:\n%s", out)
	}
}

func TestRender_WithPathRewrite(t *testing.T) {
	r := NewRegistry()
	r.Add(Vhost{
		Name:        "api",
		Domain:      "api.example.com",
		Backend:     "api:8080",
		PathRewrite: "/v1",
	})
	out, err := Render(r)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"uri strip_prefix /v1", "reverse_proxy api:8080/v1"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

func TestRender_BlankLinesBetweenBlocks(t *testing.T) {
	r := NewRegistry()
	r.Add(Vhost{Name: "a", Domain: "a.example.com", Backend: "a:1"})
	r.Add(Vhost{Name: "b", Domain: "b.example.com", Backend: "b:1"})
	out, err := Render(r)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Two blocks separated by exactly one blank line (i.e. "\n\n").
	if !strings.Contains(out, "}\n\na.example.com") && !strings.Contains(out, "}\n\nb.example.com") {
		// The order is sorted: a then b
		if !strings.Contains(out, "}\n\nb.example.com") {
			t.Errorf("expected blank-line separator between blocks, got:\n%s", out)
		}
	}
}

func TestRender_DeterministicOrder(t *testing.T) {
	r := NewRegistry()
	r.Add(Vhost{Name: "z", Domain: "z.example.com", Backend: "z:1"})
	r.Add(Vhost{Name: "a", Domain: "a.example.com", Backend: "a:1"})
	r.Add(Vhost{Name: "m", Domain: "m.example.com", Backend: "m:1"})
	out1, _ := Render(r)
	out2, _ := Render(r)
	if out1 != out2 {
		t.Errorf("Render is non-deterministic")
	}
	// Names appear in insertion order — Render() deterministically uses
	// r.order (not alphabetical sort) so SpecSection93 is preserved.
	zIdx := strings.Index(out1, "z.example.com")
	aIdx := strings.Index(out1, "a.example.com")
	mIdx := strings.Index(out1, "m.example.com")
	if !(zIdx < aIdx && aIdx < mIdx) {
		t.Errorf("expected insertion order z < a < m, got positions z=%d a=%d m=%d", zIdx, aIdx, mIdx)
	}
}

// =============================================================================
// FormatVhost
// =============================================================================

func TestFormatVhost_Good(t *testing.T) {
	v := Vhost{Name: "x", Domain: "x.example.com", Backend: "x:1"}
	out, err := FormatVhost(v)
	if err != nil {
		t.Fatalf("FormatVhost: %v", err)
	}
	if !strings.Contains(out, "x.example.com {") {
		t.Errorf("expected domain in output:\n%s", out)
	}
	if !strings.Contains(out, "reverse_proxy x:1") {
		t.Errorf("expected reverse_proxy in output:\n%s", out)
	}
}

func TestFormatVhost_Bad(t *testing.T) {
	_, err := FormatVhost(Vhost{Name: "x", Domain: "nodot", Backend: "x:1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// LocalDevRegistry
// =============================================================================

func TestLocalDevRegistry_BackendsUseLoopback(t *testing.T) {
	r := LocalDevRegistry()
	for _, name := range r.Names() {
		v, _ := r.Get(name)
		if !strings.HasPrefix(v.Backend, "127.0.0.1:") {
			t.Errorf("LocalDevRegistry vhost %q backend=%q (expected 127.0.0.1:port)", name, v.Backend)
		}
	}
}

func TestLocalDevRegistry_HasSameSixNames(t *testing.T) {
	want := map[string]bool{
		"forgejo": false, "chimera": false, "conscience": false,
		"hivemind": false, "traces": false, "monitor": false,
	}
	r := LocalDevRegistry()
	for _, n := range r.Names() {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("LocalDevRegistry missing %q", name)
		}
	}
}

// =============================================================================
// Render: full integration smoke
// =============================================================================

func TestRender_EndToEnd_AllFeatures(t *testing.T) {
	r := NewRegistry()
	r.SetTLSEmail("ops@helixloop.dev")
	r.Add(Vhost{Name: "forgejo", Domain: "helixloop.dev", Backend: "forgejo:3000"})
	r.Add(Vhost{
		Name:      "chimera",
		Domain:    "chimera.helixloop.dev",
		Backend:   "chimera:8001",
		RateLimit: 100,
	})
	r.Add(Vhost{
		Name:        "conscience",
		Domain:      "conscience.helixloop.dev",
		Backend:     "conscientiousness:8002",
		PathRewrite: "/v1",
		BasicAuth:   &BasicAuthConfig{Username: "admin", HashedPassword: "bcrypt-hash"},
	})
	r.Add(Vhost{Name: "hivemind", Domain: "hivemind.helixloop.dev", Backend: "hivemind:8003"})
	r.Add(Vhost{Name: "traces", Domain: "traces.helixloop.dev", Backend: "langfuse:3000"})
	r.Add(Vhost{Name: "monitor", Domain: "monitor.helixloop.dev", Backend: "grafana:3000"})

	out, err := Render(r)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Spot-check every feature is rendered.
	for _, want := range []string{
		"email ops@helixloop.dev",
		"helixloop.dev {",
		"chimera.helixloop.dev {",
		"rate_limit 100 r/s",
		"conscience.helixloop.dev {",
		"uri strip_prefix /v1",
		"basicauth {",
		"admin bcrypt-hash",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}
