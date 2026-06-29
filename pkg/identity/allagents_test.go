package identity

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// KnownFriends.AllAgents
// =============================================================================

func TestKnownFriends_AllAgents(t *testing.T) {
	t.Run("empty known friends", func(t *testing.T) {
		k := &KnownFriends{Agents: map[string]*Agent{}}
		got := k.AllAgents()
		if len(got) != 0 {
			t.Errorf("AllAgents() len = %d, want 0", len(got))
		}
	})

	t.Run("nil agent in map - skipped", func(t *testing.T) {
		k := &KnownFriends{Agents: map[string]*Agent{
			"alice": nil,
			"bob":   {Name: "bob", Status: StatusActive, Tier: TierPro},
		}}
		got := k.AllAgents()
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1 (nil skipped)", len(got))
		}
		if got[0].Name != "bob" {
			t.Errorf("Name = %q, want bob", got[0].Name)
		}
	})

	t.Run("agent with empty name - backfilled from key", func(t *testing.T) {
		k := &KnownFriends{Agents: map[string]*Agent{
			"charlie": {Name: "", Status: StatusActive, Tier: TierFlash},
		}}
		got := k.AllAgents()
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		if got[0].Name != "charlie" {
			t.Errorf("Name = %q, want charlie (backfill)", got[0].Name)
		}
	})

	t.Run("agents across all statuses - all included and sorted", func(t *testing.T) {
		k := &KnownFriends{Agents: map[string]*Agent{
			"zulu":     {Name: "zulu", Status: StatusOffboarded, Tier: TierPro},
			"alpha":    {Name: "alpha", Status: StatusActive, Tier: TierFlash},
			"mike":     {Name: "mike", Status: StatusPending, Tier: TierPro},
			"kilo":     {Name: "kilo", Status: StatusActive, Tier: TierPro},
			"november": {Name: "november", Status: StatusActive, Tier: TierFlash},
		}}
		got := k.AllAgents()
		if len(got) != 5 {
			t.Fatalf("len = %d, want 5", len(got))
		}
		// All should be sorted alphabetically
		want := []string{"alpha", "kilo", "mike", "november", "zulu"}
		for i, name := range want {
			if got[i].Name != name {
				t.Errorf("AllAgents()[%d].Name = %q, want %q", i, got[i].Name, name)
			}
		}
	})

	t.Run("mutation test - backfill does not affect subsequent calls", func(t *testing.T) {
		// Verify that backfilling Name from map key is stable.
		// The first call sets Name; subsequent calls should see the same value.
		k := &KnownFriends{Agents: map[string]*Agent{
			"echo": {Name: "", Status: StatusActive, Tier: TierPro},
		}}
		got1 := k.AllAgents()
		if got1[0].Name != "echo" {
			t.Fatalf("first call: Name = %q, want echo", got1[0].Name)
		}
		got2 := k.AllAgents()
		if got2[0].Name != "echo" {
			t.Errorf("second call: Name = %q, want echo (backfill stable)", got2[0].Name)
		}
	})
}

// =============================================================================
// parseRetryAfter
// =============================================================================

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name string
		val  string
		want time.Duration
	}{
		{
			name: "empty string falls back to 5s",
			val:  "",
			want: 5 * time.Second,
		},
		{
			name: "positive integer seconds",
			val:  "30",
			want: 30 * time.Second,
		},
		{
			name: "zero seconds",
			val:  "0",
			want: 0,
		},
		{
			name: "large integer",
			val:  "3600",
			want: 3600 * time.Second,
		},
		{
			name: "negative integer falls back",
			val:  "-1",
			want: 5 * time.Second,
		},
		{
			name: "unparseable string falls back",
			val:  "not-a-number",
			want: 5 * time.Second,
		},
		{
			name: "whitespace falls back",
			val:  "   ",
			want: 5 * time.Second,
		},
		{
			name: "floating point falls back",
			val:  "3.14",
			want: 5 * time.Second,
		},
		{
			name: "HTTP-date in the future — returns computed duration",
			// http.ParseTime accepts RFC1123, RFC850, and ANSIC formats.
			// Use RFC1123 with GMT timezone for deterministic parsing.
			val: time.Now().UTC().Add(2 * time.Minute).Format("Mon, 02 Jan 2006 15:04:05 GMT"),
			want: func() time.Duration {
				return 0 // tested dynamically below
			}(),
		},
		{
			name: "HTTP-date in the past falls back",
			val:  "Mon, 01 Jan 2020 00:00:00 GMT",
			want: 5 * time.Second,
		},
		{
			name: "invalid HTTP-date format falls back",
			val:  "not-a-date",
			want: 5 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRetryAfter(tt.val)
			if tt.name == "HTTP-date in the future — returns computed duration" {
				// Dynamic test: the duration should be > 0 and roughly
				// equal to ~2 minutes (within a 10s tolerance window).
				if got <= 0 {
					t.Errorf("expected positive duration, got %v", got)
				}
				expected := 2 * time.Minute
				delta := got - expected
				if delta < 0 {
					delta = -delta
				}
				if delta > 10*time.Second {
					t.Errorf("got %v, want ~%v (±10s)", got, expected)
				}
				return
			}
			if got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.val, got, tt.want)
			}
		})
	}
}

// =============================================================================
// readAndCloseBody
// =============================================================================

func TestReadAndCloseBody(t *testing.T) {
	t.Run("normal body", func(t *testing.T) {
		body := `{"message": "hello"}`
		resp := &http.Response{
			Body: io.NopCloser(strings.NewReader(body)),
		}
		got := readAndCloseBody(resp)
		if got != body {
			t.Errorf("got %q, want %q", got, body)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		resp := &http.Response{
			Body: io.NopCloser(strings.NewReader("")),
		}
		got := readAndCloseBody(resp)
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("body with surrounding whitespace - trimmed", func(t *testing.T) {
		resp := &http.Response{
			Body: io.NopCloser(strings.NewReader("\n  hello world  \n")),
		}
		got := readAndCloseBody(resp)
		if want := "hello world"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("body with only whitespace - trimmed to empty", func(t *testing.T) {
		resp := &http.Response{
			Body: io.NopCloser(strings.NewReader("   \n\t  ")),
		}
		got := readAndCloseBody(resp)
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("error reading body returns empty", func(t *testing.T) {
		// Use an errorReader that fails on read
		resp := &http.Response{
			Body: io.NopCloser(&errorReader{}),
		}
		got := readAndCloseBody(resp)
		if got != "" {
			t.Errorf("got %q, want empty on read error", got)
		}
	})

	t.Run("body is consumed — second read fails", func(t *testing.T) {
		body := "payload"
		resp := &http.Response{
			Body: io.NopCloser(bytes.NewBufferString(body)),
		}
		_ = readAndCloseBody(resp)
		// After readAndCloseBody, the body should be closed and consumed
		remaining, err := io.ReadAll(resp.Body)
		if err == nil && len(remaining) > 0 {
			t.Errorf("expected body to be fully consumed, got %d bytes remaining", len(remaining))
		}
	})

	t.Run("large body", func(t *testing.T) {
		body := strings.Repeat("x", 10000)
		resp := &http.Response{
			Body: io.NopCloser(strings.NewReader(body)),
		}
		got := readAndCloseBody(resp)
		if got != body {
			t.Errorf("len = %d, want %d", len(got), len(body))
		}
	})

	t.Run("real httptest response", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("real response body"))
		}))
		defer ts.Close()

		resp, err := http.Get(ts.URL)
		if err != nil {
			t.Fatal(err)
		}
		got := readAndCloseBody(resp)
		if got != "real response body" {
			t.Errorf("got %q, want %q", got, "real response body")
		}
	})
}

// =============================================================================
// errorReader helper — fails on every read
// =============================================================================

type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
