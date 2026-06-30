package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// UsageReport captures resource consumption for a single sandbox session.
type UsageReport struct {
	SessionID        string        `json:"session_id"`
	AgentID          string        `json:"agent_id"`
	PeakMemoryBytes  int64         `json:"peak_memory_bytes"`
	MemoryLimitBytes int64         `json:"memory_limit_bytes"`
	CPUTime          time.Duration `json:"cpu_time_ns"`
	WallClock        time.Duration `json:"wall_clock_ns"`
	NetworkAttempts  int           `json:"network_attempts"`
	FsWrites         int           `json:"fs_writes"`
	OOMEvents        int           `json:"oom_events"`
	ExceededMemory   bool          `json:"exceeded_memory"`
	ExceededTime     bool          `json:"exceeded_time"`
	SampledAt        time.Time     `json:"sampled_at"`
}

// MemoryFraction returns the fraction (0.0–1.0) of the memory limit used.
// Returns 0 if no limit was set.
func (r UsageReport) MemoryFraction() float64 {
	if r.MemoryLimitBytes <= 0 {
		return 0
	}
	return float64(r.PeakMemoryBytes) / float64(r.MemoryLimitBytes)
}

// IsMemoryExceeded returns true if peak memory usage reached the limit.
func (r UsageReport) IsMemoryExceeded() bool {
	return r.MemoryLimitBytes > 0 && r.PeakMemoryBytes >= r.MemoryLimitBytes
}

// SessionSummary aggregates usage across all sessions for a single agent.
type SessionSummary struct {
	AgentID              string        `json:"agent_id"`
	TotalSessions        int           `json:"total_sessions"`
	PeakMemoryAcross     int64         `json:"peak_memory_across_bytes"`
	TotalCPUTime         time.Duration `json:"total_cpu_time_ns"`
	TotalWallClock       time.Duration `json:"total_wall_clock_ns"`
	TotalOOMEvents       int           `json:"total_oom_events"`
	TotalNetworkAttempts int           `json:"total_network_attempts"`
	TotalFsWrites        int           `json:"total_fs_writes"`
	MemoryExceeded       int           `json:"memory_exceeded_count"`
	TimeExceeded         int           `json:"time_exceeded_count"`
	Reports              []UsageReport `json:"reports"`
}

// ---------------------------------------------------------------------------
// ResourceUsageTracker
// ---------------------------------------------------------------------------

// ResourceUsageTracker monitors sandboxed agent sessions by reading cgroup v2
// metrics (memory.events, cpu.stat) and tracking wall-clock duration,
// network access attempts, and filesystem writes.
//
// It is safe for concurrent use.
type ResourceUsageTracker struct {
	mu       sync.RWMutex
	sessions map[string]*trackedSession
}

type trackedSession struct {
	config      SandboxConfig
	agentID     string
	startTime   time.Time
	endTime     time.Time
	ended       bool
	peakMem     int64
	cpuTime     time.Duration
	oomCount    int
	netAttempts int
	fsWrites    int
	cgroupPath  string
}

// NewResourceUsageTracker creates a new tracker with no active sessions.
func NewResourceUsageTracker() *ResourceUsageTracker {
	return &ResourceUsageTracker{
		sessions: make(map[string]*trackedSession),
	}
}

// StartSession begins tracking a sandbox session. If a session with the
// same ID already exists, it is overwritten.
func (t *ResourceUsageTracker) StartSession(agentID string, cfg SandboxConfig) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.sessions[cfg.SessionID] = &trackedSession{
		config:     cfg,
		agentID:    agentID,
		startTime:  time.Now(),
		cgroupPath: cfg.CgroupPath(),
	}
}

// EndSession marks a session as ended, fixing its wall-clock duration.
// Subsequent calls to Sample for this session will return the final report.
func (t *ResourceUsageTracker) EndSession(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if s, ok := t.sessions[sessionID]; ok {
		s.endTime = time.Now()
		s.ended = true
	}
}

// RecordNetworkAttempt increments the network access attempt counter for
// a session. Called when a network access attempt is detected (e.g.,
// through seccomp logs or strace).
func (t *ResourceUsageTracker) RecordNetworkAttempt(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if s, ok := t.sessions[sessionID]; ok {
		s.netAttempts++
	}
}

// RecordFsWrite increments the filesystem write counter for a session.
// Called when a write operation is detected (e.g., through inotify or
// filesystem audit logs).
func (t *ResourceUsageTracker) RecordFsWrite(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if s, ok := t.sessions[sessionID]; ok {
		s.fsWrites++
	}
}

// Sample reads current cgroup metrics for a session and returns a UsageReport.
// This is a point-in-time snapshot — call periodically during the session
// for live monitoring, or once after EndSession for the final report.
//
// If the cgroup files are unavailable (e.g., rootless without delegation),
// the metrics that could not be read are reported as zero.
func (t *ResourceUsageTracker) Sample(sessionID string) (UsageReport, error) {
	t.mu.RLock()
	s, ok := t.sessions[sessionID]
	t.mu.RUnlock()

	if !ok {
		return UsageReport{}, fmt.Errorf("session %q not found", sessionID)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Read memory.current for current RSS.
	memCurrent := readCgroupInt(s.cgroupPath, "memory.current")
	if memCurrent > s.peakMem {
		s.peakMem = memCurrent
	}

	// Read memory.events for OOM count.
	events := readCgroupFile(s.cgroupPath, "memory.events")
	oom := parseOOMEvents(events)
	s.oomCount = max(s.oomCount, oom)

	// Read cpu.stat for cumulative CPU time (usage_usec).
	cpuUsageUsec := readCgroupInt(s.cgroupPath, "cpu.stat")
	if cpuUsageUsec > 0 {
		s.cpuTime = time.Duration(cpuUsageUsec) * time.Microsecond
	}

	// Compute wall-clock.
	wallClock := time.Since(s.startTime)
	if s.ended {
		wallClock = s.endTime.Sub(s.startTime)
	}

	memLimitBytes := int64(0)
	if s.config.MemoryLimit > 0 {
		memLimitBytes = int64(s.config.MemoryLimit) * 1024 * 1024
	}

	timeLimit := time.Duration(0)
	if s.config.TimeLimit > 0 {
		timeLimit = time.Duration(s.config.TimeLimit) * time.Second
	}

	report := UsageReport{
		SessionID:        sessionID,
		AgentID:          s.agentID,
		PeakMemoryBytes:  s.peakMem,
		MemoryLimitBytes: memLimitBytes,
		CPUTime:          s.cpuTime,
		WallClock:        wallClock,
		NetworkAttempts:  s.netAttempts,
		FsWrites:         s.fsWrites,
		OOMEvents:        s.oomCount,
		ExceededMemory:   memLimitBytes > 0 && s.peakMem >= memLimitBytes,
		ExceededTime:     timeLimit > 0 && wallClock >= timeLimit,
		SampledAt:        time.Now(),
	}

	return report, nil
}

// EnforceResourceLimits checks if a session has exceeded its configured
// memory or time limits. Returns (memoryExceeded, timeExceeded).
func (t *ResourceUsageTracker) EnforceResourceLimits(sessionID string) (bool, bool) {
	t.mu.RLock()
	s, ok := t.sessions[sessionID]
	t.mu.RUnlock()

	if !ok {
		return false, false
	}

	memLimitBytes := int64(0)
	if s.config.MemoryLimit > 0 {
		memLimitBytes = int64(s.config.MemoryLimit) * 1024 * 1024
	}

	timeLimit := time.Duration(0)
	if s.config.TimeLimit > 0 {
		timeLimit = time.Duration(s.config.TimeLimit) * time.Second
	}

	wallClock := time.Since(s.startTime)
	if s.ended {
		wallClock = s.endTime.Sub(s.startTime)
	}

	memExceeded := memLimitBytes > 0 && s.peakMem >= memLimitBytes
	timeExceeded := timeLimit > 0 && wallClock >= timeLimit

	return memExceeded, timeExceeded
}

// GetSession returns the tracked session metadata (read-only).
func (t *ResourceUsageTracker) GetSession(sessionID string) (*trackedSession, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s, ok := t.sessions[sessionID]
	return s, ok
}

// ActiveSessions returns the IDs of all currently tracked sessions.
func (t *ResourceUsageTracker) ActiveSessions() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ids := make([]string, 0, len(t.sessions))
	for id, s := range t.sessions {
		if !s.ended {
			ids = append(ids, id)
		}
	}
	return ids
}

// AllSessions returns the IDs of all tracked sessions (active and ended).
func (t *ResourceUsageTracker) AllSessions() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ids := make([]string, 0, len(t.sessions))
	for id := range t.sessions {
		ids = append(ids, id)
	}
	return ids
}

// RemoveSession deletes a session from the tracker. Call after the final
// report has been consumed to prevent unbounded growth.
func (t *ResourceUsageTracker) RemoveSession(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sessions, sessionID)
}

// SummarizeAgent aggregates all sessions for a given agent into a
// SessionSummary. Only sessions matching agentID are included.
func (t *ResourceUsageTracker) SummarizeAgent(agentID string) SessionSummary {
	t.mu.RLock()
	defer t.mu.RUnlock()

	summary := SessionSummary{AgentID: agentID}

	for _, s := range t.sessions {
		if s.agentID != agentID {
			continue
		}
		summary.TotalSessions++

		if s.peakMem > summary.PeakMemoryAcross {
			summary.PeakMemoryAcross = s.peakMem
		}
		summary.TotalCPUTime += s.cpuTime
		summary.TotalOOMEvents += s.oomCount
		summary.TotalNetworkAttempts += s.netAttempts
		summary.TotalFsWrites += s.fsWrites

		wallClock := time.Since(s.startTime)
		if s.ended {
			wallClock = s.endTime.Sub(s.startTime)
		}
		summary.TotalWallClock += wallClock

		memLimitBytes := int64(0)
		if s.config.MemoryLimit > 0 {
			memLimitBytes = int64(s.config.MemoryLimit) * 1024 * 1024
		}
		if memLimitBytes > 0 && s.peakMem >= memLimitBytes {
			summary.MemoryExceeded++
		}

		timeLimit := time.Duration(0)
		if s.config.TimeLimit > 0 {
			timeLimit = time.Duration(s.config.TimeLimit) * time.Second
		}
		if timeLimit > 0 && wallClock >= timeLimit {
			summary.TimeExceeded++
		}

		// Build a quick report for the summary.
		summary.Reports = append(summary.Reports, UsageReport{
			SessionID:       s.config.SessionID,
			AgentID:         s.agentID,
			PeakMemoryBytes: s.peakMem,
			CPUTime:         s.cpuTime,
			WallClock:       wallClock,
			NetworkAttempts: s.netAttempts,
			FsWrites:        s.fsWrites,
			OOMEvents:       s.oomCount,
		})
	}

	return summary
}

// ---------------------------------------------------------------------------
// cgroup v2 readers
// ---------------------------------------------------------------------------

// readCgroupFile reads a cgroup control file and returns its content as a
// trimmed string. Returns empty string on any error.
func readCgroupFile(cgroupPath, filename string) string {
	data, err := os.ReadFile(filepath.Join(cgroupPath, filename))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// readCgroupInt reads a cgroup control file and parses it as an int64.
// For multi-line files like cpu.stat, it extracts the usage_usec value.
// Returns 0 on any error or if the value is not found.
func readCgroupInt(cgroupPath, filename string) int64 {
	content := readCgroupFile(cgroupPath, filename)
	if content == "" {
		return 0
	}

	// cpu.stat is multi-line with "usage_usec <N>" — extract that field.
	if filename == "cpu.stat" {
		for _, line := range strings.Split(content, "\n") {
			parts := strings.Fields(line)
			if len(parts) >= 2 && parts[0] == "usage_usec" {
				val, err := strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					return 0
				}
				return val
			}
		}
		return 0
	}

	// Single-value files (memory.current, memory.max, etc.)
	val, err := strconv.ParseInt(content, 10, 64)
	if err != nil {
		return 0
	}
	return val
}

// parseOOMEvents extracts the oom count from a memory.events file.
// Format: lines like "oom <count>" and "oom_kill <count>".
func parseOOMEvents(events string) int {
	for _, line := range strings.Split(events, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == "oom" {
			val, err := strconv.Atoi(parts[1])
			if err != nil {
				return 0
			}
			return val
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
