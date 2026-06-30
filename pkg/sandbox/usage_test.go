package sandbox

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Fake cgroup filesystem helpers
// ---------------------------------------------------------------------------

// setupFakeCgroup creates a temporary directory that mimics a cgroup v2
// filesystem at the path CgroupPath() would produce (CgroupRoot/helix/sessionID).
// Returns the cgroupRoot (parent dir) so callers can set it in SandboxConfig.
func setupFakeCgroup(t *testing.T, memCurrent, memMax, cpuUsageUsec, oom int) string {
	t.Helper()
	root := t.TempDir()
	// CgroupPath() = CgroupRoot/helix/SessionID — create that path
	cgPath := filepath.Join(root, "helix", "test-session-1")
	_ = os.MkdirAll(cgPath, 0o755)

	if memCurrent >= 0 {
		writeFakeFile(t, cgPath, "memory.current", intToStr(memCurrent))
	}
	if memMax >= 0 {
		writeFakeFile(t, cgPath, "memory.max", intToStr(memMax))
	}
	if cpuUsageUsec >= 0 {
		content := "usage_usec " + intToStr(cpuUsageUsec) + "\nuser_usec " + intToStr(cpuUsageUsec/2) + "\nsystem_usec " + intToStr(cpuUsageUsec/2) + "\n"
		_ = os.WriteFile(filepath.Join(cgPath, "cpu.stat"), []byte(content), 0o644)
	}
	if oom >= 0 {
		content := "low 0\nhigh 0\nmax 0\noom " + intToStr(oom) + "\noom_kill " + intToStr(oom) + "\n"
		_ = os.WriteFile(filepath.Join(cgPath, "memory.events"), []byte(content), 0o644)
	}

	return root
}

func writeFakeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	_ = os.WriteFile(filepath.Join(dir, name), []byte(content+"\n"), 0o644)
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	result := ""
	for n > 0 {
		digit := byte('0' + n%10)
		result = string(digit) + result
		n /= 10
	}
	return result
}

func makeConfigWithCgroup(cgroupRoot string) SandboxConfig {
	return SandboxConfig{
		SessionID:   "test-session-1",
		Isolation:   IsolationWorkspace,
		Workdir:     "/workspace",
		TimeLimit:   600,
		MemoryLimit: 2048,
		Network:     NetworkNone,
		SessionRoot: "/tmp/helix-test",
		BwrapPath:   "/bin/true",
		CgroupRoot:  cgroupRoot,
	}
}

// ---------------------------------------------------------------------------
// UsageReport methods
// ---------------------------------------------------------------------------

func TestUsageReport_MemoryFraction(t *testing.T) {
	tests := []struct {
		name     string
		report   UsageReport
		expected float64
	}{
		{
			name:     "no limit",
			report:   UsageReport{PeakMemoryBytes: 1024, MemoryLimitBytes: 0},
			expected: 0,
		},
		{
			name:     "50 percent",
			report:   UsageReport{PeakMemoryBytes: 512, MemoryLimitBytes: 1024},
			expected: 0.5,
		},
		{
			name:     "100 percent",
			report:   UsageReport{PeakMemoryBytes: 1024, MemoryLimitBytes: 1024},
			expected: 1.0,
		},
		{
			name:     "over 100 percent",
			report:   UsageReport{PeakMemoryBytes: 2048, MemoryLimitBytes: 1024},
			expected: 2.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.InDelta(t, tt.expected, tt.report.MemoryFraction(), 0.001)
		})
	}
}

func TestUsageReport_IsMemoryExceeded(t *testing.T) {
	t.Run("exceeded", func(t *testing.T) {
		r := UsageReport{PeakMemoryBytes: 2048, MemoryLimitBytes: 1024}
		assert.True(t, r.IsMemoryExceeded())
	})

	t.Run("not exceeded", func(t *testing.T) {
		r := UsageReport{PeakMemoryBytes: 512, MemoryLimitBytes: 1024}
		assert.False(t, r.IsMemoryExceeded())
	})

	t.Run("no limit", func(t *testing.T) {
		r := UsageReport{PeakMemoryBytes: 99999, MemoryLimitBytes: 0}
		assert.False(t, r.IsMemoryExceeded())
	})
}

// ---------------------------------------------------------------------------
// StartSession / EndSession
// ---------------------------------------------------------------------------

func TestStartSession(t *testing.T) {
	tracker := NewResourceUsageTracker()
	cfg := makeConfigWithCgroup("/sys/fs/cgroup/helix/s1")

	tracker.StartSession("agent-1", cfg)

	s, ok := tracker.GetSession("test-session-1")
	require.True(t, ok)
	assert.Equal(t, "agent-1", s.agentID)
	assert.False(t, s.ended)
	assert.False(t, s.startTime.IsZero())
}

func TestEndSession(t *testing.T) {
	tracker := NewResourceUsageTracker()
	cfg := makeConfigWithCgroup("/sys/fs/cgroup/helix/s1")

	tracker.StartSession("agent-1", cfg)
	tracker.EndSession("test-session-1")

	s, ok := tracker.GetSession("test-session-1")
	require.True(t, ok)
	assert.True(t, s.ended)
	assert.False(t, s.endTime.IsZero())
}

func TestStartSession_Overwrite(t *testing.T) {
	tracker := NewResourceUsageTracker()
	cfg := makeConfigWithCgroup("/sys/fs/cgroup/helix/s1")

	tracker.StartSession("agent-1", cfg)
	tracker.StartSession("agent-2", cfg) // same session ID, different agent

	s, ok := tracker.GetSession("test-session-1")
	require.True(t, ok)
	assert.Equal(t, "agent-2", s.agentID)
}

// ---------------------------------------------------------------------------
// RecordNetworkAttempt / RecordFsWrite
// ---------------------------------------------------------------------------

func TestRecordNetworkAttempt(t *testing.T) {
	tracker := NewResourceUsageTracker()
	cfg := makeConfigWithCgroup("/sys/fs/cgroup/helix/s1")

	tracker.StartSession("agent-1", cfg)
	tracker.RecordNetworkAttempt("test-session-1")
	tracker.RecordNetworkAttempt("test-session-1")
	tracker.RecordNetworkAttempt("test-session-1")

	s, ok := tracker.GetSession("test-session-1")
	require.True(t, ok)
	assert.Equal(t, 3, s.netAttempts)
}

func TestRecordFsWrite(t *testing.T) {
	tracker := NewResourceUsageTracker()
	cfg := makeConfigWithCgroup("/sys/fs/cgroup/helix/s1")

	tracker.StartSession("agent-1", cfg)
	tracker.RecordFsWrite("test-session-1")
	tracker.RecordFsWrite("test-session-1")

	s, ok := tracker.GetSession("test-session-1")
	require.True(t, ok)
	assert.Equal(t, 2, s.fsWrites)
}

func TestRecordNetworkAttempt_NonexistentSession(t *testing.T) {
	tracker := NewResourceUsageTracker()
	// Should not panic
	tracker.RecordNetworkAttempt("nope")
}

func TestRecordFsWrite_NonexistentSession(t *testing.T) {
	tracker := NewResourceUsageTracker()
	// Should not panic
	tracker.RecordFsWrite("nope")
}

// ---------------------------------------------------------------------------
// Sample
// ---------------------------------------------------------------------------

func TestSample_Success(t *testing.T) {
	cgDir := setupFakeCgroup(t, 512*1024*1024, 2048*1024*1024, 5000000, 0)
	cfg := makeConfigWithCgroup(cgDir)

	tracker := NewResourceUsageTracker()
	tracker.StartSession("agent-1", cfg)

	report, err := tracker.Sample("test-session-1")
	require.NoError(t, err)

	assert.Equal(t, "test-session-1", report.SessionID)
	assert.Equal(t, "agent-1", report.AgentID)
	assert.Equal(t, int64(512*1024*1024), report.PeakMemoryBytes)
	assert.Equal(t, int64(2048*1024*1024), report.MemoryLimitBytes)
	assert.Equal(t, 5*time.Second, report.CPUTime)
	assert.False(t, report.ExceededMemory)
	assert.False(t, report.ExceededTime)
	assert.False(t, report.SampledAt.IsZero())
}

func TestSample_MemoryExceeded(t *testing.T) {
	cgDir := setupFakeCgroup(t, 2048*1024*1024, 1024*1024*1024, 1000000, 1)
	cfg := makeConfigWithCgroup(cgDir)
	cfg.MemoryLimit = 1024 // 1024 MB limit

	tracker := NewResourceUsageTracker()
	tracker.StartSession("agent-1", cfg)

	report, err := tracker.Sample("test-session-1")
	require.NoError(t, err)

	assert.True(t, report.ExceededMemory)
	assert.Equal(t, 1, report.OOMEvents)
}

func TestSample_PeakMemoryTracks(t *testing.T) {
	// Start with low memory, sample, then increase — peak should persist.
	root := t.TempDir()
	cgPath := filepath.Join(root, "helix", "test-session-1")
	_ = os.MkdirAll(cgPath, 0o755)
	_ = os.WriteFile(filepath.Join(cgPath, "memory.current"), []byte("100\n"), 0o644)
	_ = os.WriteFile(filepath.Join(cgPath, "memory.events"), []byte("oom 0\noom_kill 0\n"), 0o644)
	_ = os.WriteFile(filepath.Join(cgPath, "cpu.stat"), []byte("usage_usec 0\n"), 0o644)

	cfg := makeConfigWithCgroup(root)
	tracker := NewResourceUsageTracker()
	tracker.StartSession("agent-1", cfg)

	// First sample
	r1, _ := tracker.Sample("test-session-1")
	assert.Equal(t, int64(100), r1.PeakMemoryBytes)

	// Increase memory.current
	_ = os.WriteFile(filepath.Join(cgPath, "memory.current"), []byte("500\n"), 0o644)
	r2, _ := tracker.Sample("test-session-1")
	assert.Equal(t, int64(500), r2.PeakMemoryBytes)

	// Decrease — peak should stay
	_ = os.WriteFile(filepath.Join(cgPath, "memory.current"), []byte("200\n"), 0o644)
	r3, _ := tracker.Sample("test-session-1")
	assert.Equal(t, int64(500), r3.PeakMemoryBytes, "peak should not decrease")
}

func TestSample_NoCgroupFiles(t *testing.T) {
	// Point to a non-existent cgroup — should get zeros, not error.
	cfg := makeConfigWithCgroup("/nonexistent/path")
	tracker := NewResourceUsageTracker()
	tracker.StartSession("agent-1", cfg)

	report, err := tracker.Sample("test-session-1")
	require.NoError(t, err)

	assert.Equal(t, int64(0), report.PeakMemoryBytes)
	assert.Equal(t, 0, report.OOMEvents)
	assert.Equal(t, time.Duration(0), report.CPUTime)
}

func TestSample_NonexistentSession(t *testing.T) {
	tracker := NewResourceUsageTracker()
	_, err := tracker.Sample("nope")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSample_TracksNetworkAndFs(t *testing.T) {
	cgDir := setupFakeCgroup(t, 100, 2048*1024*1024, 0, 0)
	cfg := makeConfigWithCgroup(cgDir)

	tracker := NewResourceUsageTracker()
	tracker.StartSession("agent-1", cfg)

	tracker.RecordNetworkAttempt("test-session-1")
	tracker.RecordNetworkAttempt("test-session-1")
	tracker.RecordFsWrite("test-session-1")

	report, err := tracker.Sample("test-session-1")
	require.NoError(t, err)

	assert.Equal(t, 2, report.NetworkAttempts)
	assert.Equal(t, 1, report.FsWrites)
}

// ---------------------------------------------------------------------------
// EnforceResourceLimits
// ---------------------------------------------------------------------------

func TestEnforceResourceLimits_NoExceeded(t *testing.T) {
	cgDir := setupFakeCgroup(t, 100, 2048*1024*1024, 0, 0)
	cfg := makeConfigWithCgroup(cgDir)

	tracker := NewResourceUsageTracker()
	tracker.StartSession("agent-1", cfg)

	mem, tm := tracker.EnforceResourceLimits("test-session-1")
	assert.False(t, mem)
	assert.False(t, tm)
}

func TestEnforceResourceLimits_MemoryExceeded(t *testing.T) {
	cgDir := setupFakeCgroup(t, 2048*1024*1024, 0, 0, 0)
	cfg := makeConfigWithCgroup(cgDir)
	cfg.MemoryLimit = 1024 // 1 GB

	tracker := NewResourceUsageTracker()
	tracker.StartSession("agent-1", cfg)

	// Sample first to update peak
	_, _ = tracker.Sample("test-session-1")

	mem, tm := tracker.EnforceResourceLimits("test-session-1")
	assert.True(t, mem)
	assert.False(t, tm)
}

func TestEnforceResourceLimits_TimeExceeded(t *testing.T) {
	cgDir := setupFakeCgroup(t, 0, 0, 0, 0)
	cfg := makeConfigWithCgroup(cgDir)
	cfg.TimeLimit = 0 // No time limit for test — but we need to simulate

	tracker := NewResourceUsageTracker()

	// Manually create a session with a small time limit and fake the start time.
	tracker.StartSession("agent-1", cfg)
	tracker.mu.Lock()
	s := tracker.sessions["test-session-1"]
	s.config.TimeLimit = 1 // 1 second
	s.startTime = time.Now().Add(-10 * time.Second)
	tracker.mu.Unlock()

	mem, tm := tracker.EnforceResourceLimits("test-session-1")
	assert.False(t, mem)
	assert.True(t, tm)
}

func TestEnforceResourceLimits_NonexistentSession(t *testing.T) {
	tracker := NewResourceUsageTracker()
	mem, tm := tracker.EnforceResourceLimits("nope")
	assert.False(t, mem)
	assert.False(t, tm)
}

func TestEnforceResourceLimits_NoLimits(t *testing.T) {
	cgDir := setupFakeCgroup(t, 0, 0, 0, 0)
	cfg := makeConfigWithCgroup(cgDir)
	cfg.MemoryLimit = 0
	cfg.TimeLimit = 0

	tracker := NewResourceUsageTracker()
	tracker.StartSession("agent-1", cfg)

	mem, tm := tracker.EnforceResourceLimits("test-session-1")
	assert.False(t, mem)
	assert.False(t, tm)
}

// ---------------------------------------------------------------------------
// ActiveSessions / AllSessions / RemoveSession
// ---------------------------------------------------------------------------

func TestActiveSessions(t *testing.T) {
	tracker := NewResourceUsageTracker()

	cfg1 := makeConfigWithCgroup("/a")
	cfg1.SessionID = "s1"
	cfg2 := makeConfigWithCgroup("/b")
	cfg2.SessionID = "s2"

	tracker.StartSession("agent-1", cfg1)
	tracker.StartSession("agent-2", cfg2)

	active := tracker.ActiveSessions()
	assert.Len(t, active, 2)

	tracker.EndSession("s1")
	active = tracker.ActiveSessions()
	assert.Len(t, active, 1)
	assert.Contains(t, active, "s2")
}

func TestAllSessions(t *testing.T) {
	tracker := NewResourceUsageTracker()

	cfg1 := makeConfigWithCgroup("/a")
	cfg1.SessionID = "s1"
	cfg2 := makeConfigWithCgroup("/b")
	cfg2.SessionID = "s2"

	tracker.StartSession("agent-1", cfg1)
	tracker.StartSession("agent-2", cfg2)
	tracker.EndSession("s1")

	all := tracker.AllSessions()
	assert.Len(t, all, 2)
}

func TestRemoveSession(t *testing.T) {
	tracker := NewResourceUsageTracker()
	cfg := makeConfigWithCgroup("/a")
	cfg.SessionID = "s1"

	tracker.StartSession("agent-1", cfg)
	assert.Len(t, tracker.AllSessions(), 1)

	tracker.RemoveSession("s1")
	assert.Len(t, tracker.AllSessions(), 0)
}

// ---------------------------------------------------------------------------
// SummarizeAgent
// ---------------------------------------------------------------------------

func TestSummarizeAgent_SingleAgent(t *testing.T) {
	// Two sessions for the same agent. Each needs its own fake cgroup dir.
	root1 := t.TempDir()
	cgPath1 := filepath.Join(root1, "helix", "sess-a")
	_ = os.MkdirAll(cgPath1, 0o755)
	_ = os.WriteFile(filepath.Join(cgPath1, "memory.current"), []byte("100\n"), 0o644)
	_ = os.WriteFile(filepath.Join(cgPath1, "cpu.stat"), []byte("usage_usec 500000\n"), 0o644)
	_ = os.WriteFile(filepath.Join(cgPath1, "memory.events"), []byte("oom 0\noom_kill 0\n"), 0o644)

	root2 := t.TempDir()
	cgPath2 := filepath.Join(root2, "helix", "sess-b")
	_ = os.MkdirAll(cgPath2, 0o755)
	_ = os.WriteFile(filepath.Join(cgPath2, "memory.current"), []byte("200\n"), 0o644)
	_ = os.WriteFile(filepath.Join(cgPath2, "cpu.stat"), []byte("usage_usec 300000\n"), 0o644)
	_ = os.WriteFile(filepath.Join(cgPath2, "memory.events"), []byte("oom 1\noom_kill 1\n"), 0o644)

	tracker := NewResourceUsageTracker()

	cfg1 := makeConfigWithCgroup(root1)
	cfg1.SessionID = "sess-a"
	cfg2 := makeConfigWithCgroup(root2)
	cfg2.SessionID = "sess-b"

	tracker.StartSession("agent-A", cfg1)
	tracker.StartSession("agent-A", cfg2)

	// Sample to populate metrics
	_, _ = tracker.Sample("sess-a")
	_, _ = tracker.Sample("sess-b")

	summary := tracker.SummarizeAgent("agent-A")
	assert.Equal(t, "agent-A", summary.AgentID)
	assert.Equal(t, 2, summary.TotalSessions)
	assert.Equal(t, int64(200), summary.PeakMemoryAcross)
	assert.Equal(t, 800*time.Millisecond, summary.TotalCPUTime)
	assert.Equal(t, 1, summary.TotalOOMEvents)
}

func TestSummarizeAgent_MultipleAgents(t *testing.T) {
	tracker := NewResourceUsageTracker()

	cfg1 := makeConfigWithCgroup("/a")
	cfg1.SessionID = "s1"
	cfg2 := makeConfigWithCgroup("/b")
	cfg2.SessionID = "s2"

	tracker.StartSession("agent-A", cfg1)
	tracker.StartSession("agent-B", cfg2)

	summaryA := tracker.SummarizeAgent("agent-A")
	assert.Equal(t, 1, summaryA.TotalSessions)

	summaryB := tracker.SummarizeAgent("agent-B")
	assert.Equal(t, 1, summaryB.TotalSessions)

	summaryNone := tracker.SummarizeAgent("agent-C")
	assert.Equal(t, 0, summaryNone.TotalSessions)
}

func TestSummarizeAgent_WithNetworkAndFs(t *testing.T) {
	tracker := NewResourceUsageTracker()
	cfg := makeConfigWithCgroup("/a")
	cfg.SessionID = "s1"

	tracker.StartSession("agent-X", cfg)
	tracker.RecordNetworkAttempt("s1")
	tracker.RecordNetworkAttempt("s1")
	tracker.RecordFsWrite("s1")

	summary := tracker.SummarizeAgent("agent-X")
	assert.Equal(t, 2, summary.TotalNetworkAttempts)
	assert.Equal(t, 1, summary.TotalFsWrites)
}

// ---------------------------------------------------------------------------
// cgroup reader helpers
// ---------------------------------------------------------------------------

func TestReadCgroupFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "test"), []byte("hello\n"), 0o644)

	result := readCgroupFile(dir, "test")
	assert.Equal(t, "hello", result)
}

func TestReadCgroupFile_Missing(t *testing.T) {
	result := readCgroupFile("/nonexistent", "test")
	assert.Equal(t, "", result)
}

func TestReadCgroupInt(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "memory.current"), []byte("1048576\n"), 0o644)

	result := readCgroupInt(dir, "memory.current")
	assert.Equal(t, int64(1048576), result)
}

func TestReadCgroupInt_CpuStat(t *testing.T) {
	dir := t.TempDir()
	content := "usage_usec 5000000\nuser_usec 2500000\nsystem_usec 2500000\n"
	_ = os.WriteFile(filepath.Join(dir, "cpu.stat"), []byte(content), 0o644)

	result := readCgroupInt(dir, "cpu.stat")
	assert.Equal(t, int64(5000000), result)
}

func TestReadCgroupInt_Missing(t *testing.T) {
	result := readCgroupInt("/nonexistent", "memory.current")
	assert.Equal(t, int64(0), result)
}

func TestParseOOMEvents(t *testing.T) {
	t.Run("with oom", func(t *testing.T) {
		events := "low 0\nhigh 0\nmax 0\noom 3\noom_kill 3\n"
		assert.Equal(t, 3, parseOOMEvents(events))
	})

	t.Run("no oom", func(t *testing.T) {
		events := "low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\n"
		assert.Equal(t, 0, parseOOMEvents(events))
	})

	t.Run("empty", func(t *testing.T) {
		assert.Equal(t, 0, parseOOMEvents(""))
	})
}

func TestMax(t *testing.T) {
	assert.Equal(t, 5, max(5, 3))
	assert.Equal(t, 10, max(3, 10))
	assert.Equal(t, 0, max(0, 0))
}
