package log

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// ParseLevel / Level.String
// =============================================================================

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in  string
		out Level
		err bool
	}{
		{"", LevelInfo, false},
		{"info", LevelInfo, false},
		{"INFO", LevelInfo, false},
		{" debug ", LevelDebug, false},
		{"trace", LevelDebug, false},
		{"warn", LevelWarn, false},
		{"warning", LevelWarn, false},
		{"error", LevelError, false},
		{"err", LevelError, false},
		{"fatal", LevelInfo, true},
	}
	for _, c := range cases {
		got, err := ParseLevel(c.in)
		if c.err {
			assert.Error(t, err, c.in)
			continue
		}
		require.NoError(t, err, c.in)
		assert.Equal(t, c.out, got, c.in)
	}
}

func TestLevelString(t *testing.T) {
	assert.Equal(t, "info", LevelInfo.String())
	assert.Equal(t, "warn", LevelWarn.String())
	assert.Equal(t, "error", LevelError.String())
	assert.Equal(t, "debug", LevelDebug.String())
	assert.Contains(t, Level(99).String(), "level(")
}

// =============================================================================
// New
// =============================================================================

func TestNew_DefaultsAndValidation(t *testing.T) {
	var buf bytes.Buffer
	l, err := New(&buf, "", LevelInfo)
	require.NoError(t, err)
	assert.Equal(t, "text", l.format)
	assert.Equal(t, LevelInfo, l.min)

	// bad format
	_, err = New(&buf, "yaml", LevelInfo)
	assert.Error(t, err)

	// nil writer → stderr
	l, err = New(nil, "json", LevelInfo)
	require.NoError(t, err)
	assert.NotNil(t, l)
}

func TestNew_FormatVariants(t *testing.T) {
	for _, f := range []string{"json", "JSON", "text", "TEXT"} {
		var buf bytes.Buffer
		_, err := New(&buf, f, LevelInfo)
		assert.NoError(t, err, f)
	}
}

// =============================================================================
// Emit — JSON format
// =============================================================================

func TestEmit_JSON(t *testing.T) {
	var buf bytes.Buffer
	l, err := New(&buf, "json", LevelInfo)
	require.NoError(t, err)

	l.Emit(LevelInfo, "hello", map[string]any{
		"a": "value",
		"b": 42,
	})

	// Output is one JSON object on one line.
	out := strings.TrimSpace(buf.String())
	assert.True(t, strings.HasPrefix(out, "{"))
	assert.True(t, strings.HasSuffix(out, "}"))
	assert.Contains(t, out, "\"msg\":\"hello\"")
	assert.Contains(t, out, "\"level\":\"info\"")
	assert.Contains(t, out, "\"a\":\"value\"")
	assert.Contains(t, out, "\"b\":42")
	assert.Contains(t, out, "\"ts\"")

	// Parses as valid JSON.
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	assert.Equal(t, "hello", decoded["msg"])
	assert.Equal(t, "info", decoded["level"])
	assert.Equal(t, "value", decoded["a"])
	assert.Equal(t, float64(42), decoded["b"])
}

func TestEmit_JSON_KeyOrder(t *testing.T) {
	// Two Loggers emitting the same fields should produce identical
	// (sorted-key) JSON — important for log diffing in tests.
	var buf1, buf2 bytes.Buffer
	l1, _ := New(&buf1, "json", LevelInfo)
	l2, _ := New(&buf2, "json", LevelInfo)
	fields := map[string]any{"a": 1, "b": 2, "c": 3}
	l1.Emit(LevelInfo, "test", fields)
	// Even with reordered input, output key order must be sorted.
	l2.Emit(LevelInfo, "test", map[string]any{"c": 3, "b": 2, "a": 1})

	// Strip the ts field so the timestamp difference doesn't break the
	// equality assertion. We already verify ts exists in TestEmit_JSON.
	out1 := stripField(buf1.String(), "ts")
	out2 := stripField(buf2.String(), "ts")
	assert.Equal(t, out1, out2)
}

// stripField removes a top-level JSON key from s so deterministic
// equality assertions can ignore non-deterministic fields like ts.
func stripField(s, key string) string {
	out := s
	prefix := `"` + key + `":`
	for i := 0; i < len(out)-len(prefix); i++ {
		if out[i:i+len(prefix)] == prefix {
			// skip until matching closing quote/brace/etc.
			j := i + len(prefix)
			// Skip past a quoted string (handle escape).
			if j < len(out) && out[j] == '"' {
				j++
				for j < len(out) {
					if out[j] == '\\' {
						j += 2
						continue
					}
					if out[j] == '"' {
						j++
						break
					}
					j++
				}
			} else {
				// Scan non-quoted value (number, true/false/null).
				for j < len(out) && out[j] != ',' && out[j] != '}' {
					j++
				}
			}
			// Drop trailing comma if present.
			if j < len(out) && out[j] == ',' {
				j++
			}
			out = out[:i] + out[j:]
			break
		}
	}
	return out
}

// =============================================================================
// Emit — text format
// =============================================================================

func TestEmit_Text(t *testing.T) {
	var buf bytes.Buffer
	l, err := New(&buf, "text", LevelInfo)
	require.NoError(t, err)

	l.Emit(LevelWarn, "warning", map[string]any{
		"subcommand": "adversarial",
		"rc":         1,
	})

	out := buf.String()
	assert.Contains(t, out, "lvl=warn")
	assert.Contains(t, out, "msg=warning")
	assert.Contains(t, out, "rc=1")
	// sorted so subcommand before rc? Sort puts 'r' before 's'... let's
	// just verify all keys present.
	assert.Contains(t, out, "subcommand=adversarial")
}

// =============================================================================
// Emit — well-known fields
// =============================================================================

func TestEmit_RequiresFields(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, "json", LevelDebug)

	l.Emit(LevelInfo, "", nil)

	out := buf.String()
	assert.Contains(t, out, "\"msg\":\"(no message)\"")
	assert.Contains(t, out, "\"ts\"")
	assert.Contains(t, out, "\"level\":\"info\"")
}

func TestEmit_AppBaseline(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, "json", LevelInfo)
	l = l.WithApp("helix-cli")

	l.Emit(LevelInfo, "x", nil)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &decoded))
	assert.Equal(t, "helix-cli", decoded["app"])
}

func TestEmit_DropsBelowMin(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, "text", LevelInfo)

	l.Emit(LevelDebug, "should be dropped", nil)
	assert.Empty(t, buf.String(), "Debug entries should be dropped when min=Info")
}

func TestEmit_AllowsAtOrAboveMin(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, "text", LevelInfo)

	l.Emit(LevelInfo, "info", nil)
	l.Emit(LevelWarn, "warn", nil)
	l.Emit(LevelError, "error", nil)

	assert.Contains(t, buf.String(), "info")
	assert.Contains(t, buf.String(), "warn")
	assert.Contains(t, buf.String(), "error")
}

func TestSetMinLevel(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, "text", LevelError)

	l.Emit(LevelInfo, "before", nil)
	assert.Empty(t, buf.String())

	l.SetMinLevel(LevelInfo)
	l.Emit(LevelInfo, "after", nil)
	assert.Contains(t, buf.String(), "after")
}

// =============================================================================
// Reserved ordering (text mode)
// =============================================================================

func TestEmit_Text_ReservedFirst(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, "text", LevelInfo)

	l.Emit(LevelInfo, "hi", map[string]any{"z_custom": "v"})

	// Reserved keys ts/lvl/msg come before user keys.
	out := buf.String()
	tsIdx := strings.Index(out, "ts=")
	msgIdx := strings.Index(out, "msg=")
	zIdx := strings.Index(out, "z_custom=")
	assert.True(t, tsIdx >= 0 && msgIdx >= 0 && zIdx >= 0)
	assert.True(t, tsIdx < zIdx, "ts should come before user keys")
	assert.True(t, msgIdx < zIdx, "msg should come before user keys")
}

// =============================================================================
// Concurrency
// =============================================================================

func TestEmit_ConcurrentSafe(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, "json", LevelInfo)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			l.Emit(LevelInfo, "concurrent", map[string]any{"n": n})
		}(i)
	}
	wg.Wait()

	// Each Emit produces exactly one line. Verify line count.
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	assert.Len(t, lines, 50)
}

// =============================================================================
// Field type coverage
// =============================================================================

func TestEmit_FieldTypes(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, "json", LevelInfo)

	l.Emit(LevelInfo, "types", map[string]any{
		"s":     "string",
		"i":     42,
		"b":     true,
		"f":     3.14,
		"nil":   nil,
		"bytes": []byte("hello"),
	})
	t.Logf("RAW OUTPUT: %q", buf.String())
	// Shouldn't crash. Just verify it parses.
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &decoded))
	assert.Equal(t, "string", decoded["s"])
	assert.Equal(t, float64(42), decoded["i"])
	assert.Equal(t, true, decoded["b"])
	assert.Equal(t, float64(3.14), decoded["f"])
	assert.Equal(t, nil, decoded["nil"])
	assert.Equal(t, "aGVsbG8=", decoded["bytes"], "[]byte should be base64 in JSON")
}

// =============================================================================
// Comprehensive type coverage for renderJSONValue / renderTextValue
// =============================================================================

func TestEmit_JSON_AllIntegerSizes(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, "json", LevelInfo)
	l.Emit(LevelInfo, "ints", map[string]any{
		"i":   int(42),
		"i8":  int8(-8),
		"i16": int16(-1600),
		"i32": int32(-32000),
		"i64": int64(-9999999),
		"u":   uint(42),
		"u8":  uint8(8),
		"u16": uint16(1600),
		"u32": uint32(32000),
		"u64": uint64(9999999),
		"f32": float32(3.14),
		"f64": float64(2.71828),
	})
	var d map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &d))
	assert.Equal(t, float64(42), d["i"])
	assert.Equal(t, float64(-8), d["i8"])
	assert.Equal(t, float64(1600), d["u16"])
	assert.Equal(t, float64(-9999999), d["i64"])
	assert.Equal(t, float64(2.71828), d["f64"])
}

func TestEmit_Text_AllTypes(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, "text", LevelInfo)
	l.Emit(LevelInfo, "types", map[string]any{
		"string":  "hello",
		"qstring": "with space",
		"int":     42,
		"bool":    true,
		"nil":     nil,
	})
	out := buf.String()
	assert.Contains(t, out, "string=hello")
	assert.Contains(t, out, "qstring=") // quoted because contains space
	assert.Contains(t, out, `"with space"`)
	assert.Contains(t, out, "int=42")
	assert.Contains(t, out, "bool=true")
	assert.Contains(t, out, "nil=null")
}

func TestEmit_Text_TimeAndBytes(t *testing.T) {
	// Text mode auto-inserts a `ts` field; we only verify byte
	// rendering here (the time value path is exercised by JSON).
	var buf bytes.Buffer
	l, _ := New(&buf, "text", LevelInfo)
	l.Emit(LevelInfo, "t", map[string]any{
		"data":  []byte("ok"),
		"qdata": []byte("with space"),
	})
	out := buf.String()
	assert.Contains(t, out, "data=ok")
	assert.Contains(t, out, `"with space"`)
}

func TestEmit_Text_TimeValue(t *testing.T) {
	// Verifies renderTextValue on a time.Time field with a key other
	// than the auto-inserted "ts" — the auto-ts uses renderTextValue
	// under the hood too.
	var buf bytes.Buffer
	l, _ := New(&buf, "text", LevelInfo)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	l.Emit(LevelInfo, "t", map[string]any{
		"observed_at": now,
	})
	out := buf.String()
	assert.Contains(t, out, "observed_at=2026-01-02T03:04:05Z")
}

func TestShortKey(t *testing.T) {
	assert.Equal(t, "ts", shortKey("ts"))
	assert.Equal(t, "lvl", shortKey("level"))
	assert.Equal(t, "msg", shortKey("msg"))
	assert.Equal(t, "app", shortKey("app"))
	assert.Equal(t, "other", shortKey("other"))
}

// =============================================================================
// Nil receiver safety
// =============================================================================

func TestEmit_NilReceiver(t *testing.T) {
	// Should not panic.
	var l *Logger
	l.Emit(LevelInfo, "x", nil)
}

// =============================================================================
// Writer error tolerance
// =============================================================================

type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) { return 0, fmt.Errorf("simulated") }

func TestEmit_WriterErrorSwallowed(t *testing.T) {
	l, _ := New(errWriter{}, "text", LevelInfo)
	// Should not panic. No assertion needed beyond not panicking.
	l.Emit(LevelInfo, "ok", map[string]any{"a": 1})
}

// =============================================================================
// Default logger
// =============================================================================

func TestDefaultLogger(t *testing.T) {
	l := DefaultLogger()
	require.NotNil(t, l)
	// Default is text/stderr/Info; we only verify construction without
	// emitting (which would write to the test's stderr).
	assert.Equal(t, "text", l.format)
	assert.Equal(t, LevelInfo, l.min)
}
