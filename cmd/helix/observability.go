// Command helix — observability.go
//
// Unified observability wrapper per specs/SPECIFICATION.md §10.7
// (Monitoring SLAs). Every helix subcommand emits exactly one final
// structured log line with subcommand name, exit code, wall-clock
// duration, optional dry_run / agent_id / pid fields.
//
// Configuration is driven by:
//
//	--log-format text|json        Global flag (also via HELIX_LOG_FORMAT env)
//	HELIX_LOG=1                   Enable DEBUG-level entries (currently unused
//	                              by builtins, but exposed for consistency)
//	HELIX_LOG_FORMAT=text|json    Same as --log-format
//	HELIX_LOG_FILE=path           Override stderr with a file (created with
//	                              0600 permissions; append mode by default)
//
// The logger is process-global (one *log.Logger instance in the helix
// binary); wrapping is intentional so every Emit shares the same mutex
// and the same format. If you need a different sink for one subcommand,
// construct a local Logger and pass it explicitly.
//
// Wrapping strategy:
//
//	runWithObs(name, run) calls run() (a function returning errExit-or-
//	nil). On return, it emits exactly one "subcommand_complete" entry at
//	INFO and returns the original error so main.go can choose the right
//	exit code.
//
// Tests: every run*WithDryRun in cmd/helix should be replaced by a call
// to runWithObs so the wrapper is applied uniformly. The wrapper is
// exported (capital R) so it can be unit-tested standalone without
// running a real subcommand.
package main

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/log"
)

// env observability variables. Constants live here so tests can use
// `t.Setenv` without sprinkling string literals through the file.
const (
	envHelixLog        = "HELIX_LOG"
	envHelixLogFormat  = "HELIX_LOG_FORMAT"
	envHelixLogFile    = "HELIX_LOG_FILE"
	envHelixAgentID    = "HELIX_AGENT_ID"
	defaultLogFormat   = "text"
	oldLogFormatLegacy = "logfmt" // accepted alias for "text"
)

// logFileMode is the permissions used when HELIX_LOG_FILE creates the
// destination for the first time. umask respected.
const logFileMode = 0o600

// resolveLogConfig reads the env vars + parses the optional --log-format
// flag. The returned format string is normalised to one of
// "text" | "json"; unknown values surface as an error rather than
// silently falling back to "text".
//
// Caller-supplied `format` is honoured even if env vars disagree — the
// flag wins. Pass "" to defer to env vars / default.
func resolveLogConfig(formatFlag string) (format string, level log.Level, sink io.Writer, err error) {
	format = strings.TrimSpace(formatFlag)
	if format == "" {
		format = strings.TrimSpace(os.Getenv(envHelixLogFormat))
	}
	if strings.ToLower(strings.TrimSpace(format)) == oldLogFormatLegacy {
		format = "text"
	}
	if format == "" {
		format = defaultLogFormat
	}

	level = log.LevelInfo
	if v := strings.ToLower(strings.TrimSpace(os.Getenv(envHelixLog))); v != "" {
		if v == "1" || v == "true" || v == "debug" {
			level = log.LevelDebug
		}
	}

	sink = os.Stderr
	if path := strings.TrimSpace(os.Getenv(envHelixLogFile)); path != "" {
		f, ferr := openLogFile(path)
		if ferr != nil {
			return "", 0, nil, ferr
		}
		sink = f
	}
	return format, level, sink, nil
}

// openLogFile opens path in append mode (creating if missing). The
// returned *os.File is the caller's responsibility to close — pass it
// to a Logger and the Logger will own it; or close it explicitly when
// done. We return just the file and let the standard sink wrapper own
// the lifecycle when configured via process exit.
func openLogFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFileMode)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// =============================================================================
// Process-global logger
// =============================================================================

// helixLog is the package-level logger set up in main(). It is nil
// until initHelixLog() has run. runWithObs treats a nil logger as a
// no-op so subcommands can be invoked without observability in tests.
//
// Modifying helixLog after init is permitted but unusual — the helper
// setHelixLog() exists for test setup/teardown.
var helixLog *log.Logger

// setHelixLog replaces the process-global logger. Returns a closure
// that restores the previous value; useful in tests that need to swap
// for a buffer-backed logger.
func setHelixLog(l *log.Logger) func() {
	prev := helixLog
	helixLog = l
	return func() { helixLog = prev }
}

// initHelixLog sets up the process-global logger from env vars + flag.
// Called from main() exactly once on startup. Returns an error so
// main() can decide whether to fail fast or warn-and-continue.
//
// The logger is set up to write to stderr (or a file if HELIX_LOG_FILE
// is set). Format is determined by --log-format and HELIX_LOG_FORMAT.
func initHelixLog(logFormatFlag string) (*log.Logger, error) {
	format, level, sink, err := resolveLogConfig(logFormatFlag)
	if err != nil {
		return nil, err
	}
	l, err := log.New(sink, format, level)
	if err != nil {
		return nil, err
	}
	helixLog = l
	return l.WithApp(AppName), nil
}

// =============================================================================
// Subcommand wrapper
// =============================================================================

// RunWithObs wraps a subcommand with structured observability. Exactly
// one "subcommand_complete" entry is emitted on every invocation
// (success or failure). When observability is uninitialised (helixLog
// is nil), run() is called directly with no log entry.
//
// `name` is the subcommand name (e.g. "adversarial", "secrets"); it
// becomes the `subcommand` field in the emitted log entry and also
// appears in the `subcommand` field. `agentID` is captured at call
// time from HELIX_AGENT_ID; we don't accept it as a parameter because
// every subcommand should derive it consistently.
//
// The returned error matches run()'s (typically an errExit wrapping a
// non-zero exit code). The wrapper preserves it so main.go's exit-code
// logic still works.
func RunWithObs(name string, run func() error) error {
	return runWithObsInternal(name, "", run)
}

// RunWithObsAgent is the variant that accepts an explicit agent ID.
// Prefer RunWithObs unless the calling context has authoritative agent
// knowledge (e.g. test fixtures).
func RunWithObsAgent(name, agentID string, run func() error) error {
	return runWithObsInternal(name, agentID, run)
}

func runWithObsInternal(name, agentID string, run func() error) error {
	start := time.Now()
	err := run()
	duration := time.Since(start)

	rc := 0
	if e, ok := err.(errExit); ok {
		rc = e.code
	}

	// Record metrics on the global prom store (if installed). Skipped
	// in tests that don't set a store — keeps the wrapper safe.
	if promStore != nil {
		promStore.RecordInvocation(name, rc, duration)
	}

	if helixLog != nil {
		fields := map[string]any{
			"subcommand":  name,
			"rc":          rc,
			"duration_ms": duration.Milliseconds(),
			"dry_run":     dryRun,
		}
		if aid := agentID; aid != "" {
			fields["agent_id"] = aid
		} else if aid = os.Getenv(envHelixAgentID); aid != "" {
			fields["agent_id"] = aid
		}
		fields["pid"] = os.Getpid()

		// Use INFO for clean exits, WARN for non-zero exits so log
		// filters can pick up problems without parsing the rc field.
		lvl := log.LevelInfo
		msg := "subcommand_complete"
		if rc != 0 {
			lvl = log.LevelWarn
		}
		helixLog.Emit(lvl, msg, fields)
	}
	return err
}
