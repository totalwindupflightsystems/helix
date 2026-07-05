package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/log"
)

// =============================================================================
// ResolveLogConfig
// =============================================================================

func TestResolveLogConfig_Defaults(t *testing.T) {
	t.Setenv(envHelixLogFormat, "")
	t.Setenv(envHelixLog, "")
	t.Setenv(envHelixLogFile, "")

	format, level, sink, err := resolveLogConfig("")
	require.NoError(t, err)
	assert.Equal(t, "text", format)
	assert.Equal(t, log.LevelInfo, level)
	assert.NotNil(t, sink)
}

func TestResolveLogConfig_Flag(t *testing.T) {
	t.Setenv(envHelixLogFormat, "json")
	format, _, _, err := resolveLogConfig("text")
	require.NoError(t, err)
	assert.Equal(t, "text", format, "flag should beat env")
}

func TestResolveLogConfig_Env(t *testing.T) {
	t.Setenv(envHelixLogFormat, "json")
	format, _, _, err := resolveLogConfig("")
	require.NoError(t, err)
	assert.Equal(t, "json", format)
}

func TestResolveLogConfig_LogfmtAlias(t *testing.T) {
	t.Setenv(envHelixLogFormat, "")
	format, _, _, err := resolveLogConfig("logfmt")
	require.NoError(t, err)
	assert.Equal(t, "text", format, "logfmt should be aliased to text")
}

func TestResolveLogConfig_DebugEnv(t *testing.T) {
	cases := []string{"1", "true", "DEBUG"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			t.Setenv(envHelixLog, v)
			_, level, _, err := resolveLogConfig("")
			require.NoError(t, err)
			assert.Equal(t, log.LevelDebug, level)
		})
	}
}

func TestResolveLogConfig_DebugEnvOff(t *testing.T) {
	t.Setenv(envHelixLog, "")
	_, level, _, err := resolveLogConfig("")
	require.NoError(t, err)
	assert.Equal(t, log.LevelInfo, level)
}

func TestResolveLogConfig_FileSink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "helix.log")
	t.Setenv(envHelixLogFile, path)

	_, _, sink, err := resolveLogConfig("")
	require.NoError(t, err)
	require.NotNil(t, sink)
	// Closing the file is the caller's responsibility — but we should
	// at least verify it opens and writes.
	_, err = sink.Write([]byte("test\n"))
	require.NoError(t, err)
	if f, ok := sink.(interface{ Close() error }); ok {
		_ = f.Close()
	}
}

// =============================================================================
// Init/set/restore patterns
// =============================================================================

func TestInitHelixLog_SetsGlobal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "init.log")
	t.Setenv(envHelixLogFormat, "json")
	t.Setenv(envHelixLog, "")
	t.Setenv(envHelixLogFile, path)

	restore := setHelixLog(nil)
	defer restore()

	got, err := initHelixLog("")
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.NotNil(t, helixLog, "global helixLog must be set after init")
}

func TestSetHelixLog_Restore(t *testing.T) {
	prev := helixLog
	t.Cleanup(func() { helixLog = prev })

	var buf bytes.Buffer
	newLog, _ := log.New(&buf, "text", log.LevelInfo)
	restore := setHelixLog(newLog)
	assert.Same(t, newLog, helixLog)

	restore()
	assert.Same(t, prev, helixLog)
}

// =============================================================================
// RunWithObs
// =============================================================================

func TestRunWithObs_HappyPath(t *testing.T) {
	prev := helixLog
	t.Cleanup(func() { helixLog = prev })

	var buf bytes.Buffer
	l, err := log.New(&buf, "json", log.LevelInfo)
	require.NoError(t, err)
	helixLog = l
	dryRun = false
	t.Cleanup(func() { dryRun = false })
	t.Setenv(envHelixAgentID, "")

	var called bool
	err = RunWithObs("test_observer", func() error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))
	assert.Equal(t, "test_observer", entry["subcommand"])
	assert.Equal(t, float64(0), entry["rc"])
	assert.Contains(t, entry, "duration_ms")
	assert.Equal(t, false, entry["dry_run"])
	assert.Equal(t, float64(os.Getpid()), entry["pid"])
}

func TestRunWithObs_WarnOnNonZero(t *testing.T) {
	prev := helixLog
	t.Cleanup(func() { helixLog = prev })

	var buf bytes.Buffer
	l, err := log.New(&buf, "json", log.LevelInfo)
	require.NoError(t, err)
	helixLog = l

	err = RunWithObs("failsub", func() error {
		return errExit{code: 7}
	})
	require.Error(t, err)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))
	assert.Equal(t, "failsub", entry["subcommand"])
	assert.Equal(t, float64(7), entry["rc"])
	assert.Equal(t, "warn", entry["level"])
}

func TestRunWithObs_NilLoggerNoOp(t *testing.T) {
	prev := helixLog
	t.Cleanup(func() { helixLog = prev })
	helixLog = nil

	var called bool
	err := RunWithObs("silent", func() error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called, "wrapped function must run even with nil logger")
}

func TestRunWithObs_ExplicitAgentID(t *testing.T) {
	prev := helixLog
	t.Cleanup(func() { helixLog = prev })

	var buf bytes.Buffer
	l, err := log.New(&buf, "json", log.LevelInfo)
	require.NoError(t, err)
	helixLog = l
	t.Setenv(envHelixAgentID, "")

	err = RunWithObsAgent("withagent", "agent-007", func() error { return nil })
	require.NoError(t, err)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))
	assert.Equal(t, "agent-007", entry["agent_id"], "explicit agentID wins over env")
}

func TestRunWithObs_EnvAgentID(t *testing.T) {
	prev := helixLog
	t.Cleanup(func() { helixLog = prev })

	var buf bytes.Buffer
	l, err := log.New(&buf, "json", log.LevelInfo)
	require.NoError(t, err)
	helixLog = l
	t.Setenv(envHelixAgentID, "env-agent-42")

	err = RunWithObs("envrun", func() error { return nil })
	require.NoError(t, err)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))
	assert.Equal(t, "env-agent-42", entry["agent_id"], "env-derived agentID used when RunWithObs is called")
}

func TestRunWithObs_DryRunInherited(t *testing.T) {
	prev := helixLog
	t.Cleanup(func() { helixLog = prev })
	t.Cleanup(func() { dryRun = false })
	dryRun = true

	var buf bytes.Buffer
	l, err := log.New(&buf, "json", log.LevelInfo)
	require.NoError(t, err)
	helixLog = l

	err = RunWithObs("dryrun-test", func() error { return nil })
	require.NoError(t, err)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))
	assert.Equal(t, true, entry["dry_run"], "global dry_run must surface in log entry")
}

// =============================================================================
// Logger end-to-end via RunWithObs
// =============================================================================

func TestRunWithObs_TextFormat(t *testing.T) {
	prev := helixLog
	t.Cleanup(func() { helixLog = prev })

	var buf bytes.Buffer
	l, err := log.New(&buf, "text", log.LevelInfo)
	require.NoError(t, err)
	helixLog = l
	dryRun = false
	t.Cleanup(func() { dryRun = false })

	err = RunWithObs("text_test", func() error { return nil })
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "lvl=info")
	assert.Contains(t, out, "msg=subcommand_complete")
	assert.Contains(t, out, "subcommand=text_test")
	assert.Contains(t, out, "rc=0")
}

// =============================================================================
// Generic error passthrough
// =============================================================================

func TestRunWithObs_NonExitError(t *testing.T) {
	prev := helixLog
	t.Cleanup(func() { helixLog = prev })

	var buf bytes.Buffer
	l, _ := log.New(&buf, "json", log.LevelInfo)
	helixLog = l

	customErr := errors.New("network down")
	got := RunWithObs("errobs", func() error { return customErr })

	assert.True(t, errors.Is(got, customErr), "non-errExit errors must passthrough untouched")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))
	assert.Equal(t, float64(0), entry["rc"], "non-exit error → rc still 0 in log entry")
}

// =============================================================================
// Helpers used in observability.go
// =============================================================================
