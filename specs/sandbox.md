# Helix Sandbox — Implementation Specification

**Version:** 0.1.0
**Status:** Build-ready stubs (real bwrap invocation stubbed via `ErrNotImplemented`)
**Target:** bubblewrap 0.11.1+, Linux with user namespaces, cgroups v2

---

## 1. Overview

Helix Sandbox is the execution primitive for every Helix platform action. It wraps
[bubblewrap](https://github.com/projectatomic/bubblewrap) (`bwrap`) with a Go CLI that
provides three isolation levels, cgroup v2 resource limits, and a structured error
taxonomy.

Design principles:
- **Zero daemon** — every invocation is a fresh process.
- **Zero config** — all parameters come from CLI flags or defaults.
- **Zero images** — uses the host filesystem with bind mounts.
- **Stdlib only** — no external Go dependencies.

---

## 2. Package Structure

```
github.com/totalwindupflightsystems/helix-sandbox/
├── cmd/sandbox/main.go          — CLI entry point, flag parsing, error handling
├── pkg/sandbox/
│   ├── types.go                 — Error types, constants, IsolationLevel enum
│   ├── config.go                — SandboxConfig struct, defaults, validation
│   ├── isolation.go             — MountPoint, MountSpec, per-level mount builders
│   ├── executor.go              — BwrapExecutor: arg building, dry-run, stubbed exec
│   └── cgroups.go               — CgroupV2: memory.max, cpu.max, session cleanup
├── specs/sandbox.md             — This document
├── go.mod                       — Module declaration (stdlib only)
└── README.md                    — Project overview
```

---

## 3. CLI Interface

```
helix-sandbox run [flags] -- <command...>

Flags:
  --session-id       Session identifier (default: random 32-char hex)
  --isolation        none|workspace|full (default: workspace)
  --workdir          Working directory inside sandbox (default: /workspace)
  --time-limit       Max execution time in seconds (default: 600, 0=unlimited)
  --memory-limit     Max memory in MB (default: 2048, 0=unlimited)
  --network          none|restricted (default: none)
  --gpu              Allow GPU pass-through (default: true)
  --dry-run          Print bwrap command without executing
  --verbose          Log all mount operations to stderr
```

Everything after `--` is the command to execute inside the sandbox.

### Exit Codes

| Code | Constant               | Meaning                          |
|------|------------------------|----------------------------------|
| 0    | ExitOK                 | Success                          |
| 2    | ExitConfigError        | Invalid configuration            |
| 3    | ExitSetupError         | Session dir or cgroup setup fail |
| 4    | ExitBwrapNotFound      | Bubblewrap binary not found      |
| 5    | ExitExecutionError     | bwrap returned non-zero          |
| 6    | ExitTimeout            | Time limit exceeded              |
| 70   | ExitInternalError      | Unexpected error (EX_SOFTWARE)   |

---

## 4. Isolation Levels

### 4.1 IsolationLevel: `none`

No sandboxing. The command runs directly on the host via `exec.CommandContext`.
Network, filesystem, PID, and GPU are all shared with the host.

**Use case:** Debugging only. Never for untrusted code.

### 4.2 IsolationLevel: `workspace`

Standard agent execution environment.

**Filesystem:** Read-only bind mounts of system directories (`/usr`, `/bin`, `/lib`,
`/lib64`, `/etc/ld.so.cache`, `/etc/alternatives`). Writable bind mounts for the
session workspace and `/tmp`.

**Network:** Fully isolated (`--unshare-net`).

**PID:** Private PID namespace (`--unshare-pid`).

**GPU:** Pass-through enabled if `--gpu=true` and device nodes exist on the host
(`/dev/dri/*`, `/dev/nvidia*`).

**Lifecycle:** `--die-with-parent` ensures the sandbox is killed if helix-sandbox exits.

Bwrap command:
```
bwrap \
  --ro-bind /usr /usr \
  --ro-bind /bin /bin \
  --ro-bind /lib /lib \
  --ro-bind /lib64 /lib64 \
  --ro-bind /etc/ld.so.cache /etc/ld.so.cache \
  --ro-bind /etc/alternatives /etc/alternatives \
  --bind <session>/workspace /workspace \
  --bind <session>/tmp /tmp \
  [--bind /dev/dri/renderD128 /dev/dri/renderD128 ...]   # if --gpu and exists
  --proc /proc \
  --dev /dev \
  --unshare-net \
  --unshare-pid \
  --die-with-parent \
  --chdir /workspace \
  -- <command>
```

### 4.3 IsolationLevel: `full`

Maximum isolation for untrusted code.

**Filesystem:** Same read-only system mounts as workspace. No GPU device nodes
are mounted regardless of the `--gpu` flag.

**Network:** Fully isolated (`--unshare-net`).

**PID:** Private PID namespace (`--unshare-pid`).

**GPU:** Never. The `EffectiveGPU()` method forces `false`.

**Environment:** Cleared and replaced with a minimal set (`--clearenv` + `--setenv`):
```
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
HOME=/workspace
TERM=linux
LANG=C.UTF-8
HELIX=1
```

**Cgroups:** Stricter resource limits applied (same mechanism, lower defaults expected).

---

## 5. Session Directory Layout

Each session creates a directory tree under the session root (default
`/tmp/helix-sandbox`):

```
/tmp/helix-sandbox/<session-id>/
├── workspace/          — Bind-mounted at --workdir (default /workspace)
└── tmp/                — Bind-mounted at /tmp
```

Directories are created with mode 0700. On exit, the entire session directory
is removed via `os.RemoveAll`.

---

## 6. Cgroup v2 Integration

### 6.1 Layout

```
/sys/fs/cgroup/helix/<session-id>/
├── memory.max          — RSS limit in bytes
├── cpu.max             — CPU quota (format: "<quota> <period>")
└── cgroup.procs        — PID of the sandboxed process
```

### 6.2 Limit Application

- **memory.max:** `MemoryLimit * 1024 * 1024` bytes. Value 0 = no limit (omitted).
- **cpu.max:** `"max 100000"` (unlimited CPU, standard 100ms period). Future versions
  may add CPU quota based on `--time-limit` or a dedicated flag.

### 6.3 Rootless Operation

For rootless operation, the user must have delegated cgroup access (systemd-logind
or manual delegation). If the cgroup root is not writable, `CgroupV2.Setup()` returns
`nil` without error and sets `Enabled=false` — the sandbox still runs, but without
resource enforcement. This is a soft degradation, not a hard failure.

---

## 7. Error Taxonomy

| Error               | Exit Code | When                                            |
|---------------------|-----------|-------------------------------------------------|
| ErrConfigInvalid    | 2         | Bad isolation level, empty session ID, etc.     |
| ErrSetupFailed      | 3         | Cannot create session dir or write cgroup files |
| ErrBwrapNotFound    | 4         | `/usr/bin/bwrap` missing or not executable      |
| ErrExecutionFailed  | 5         | bwrap exited non-zero                           |
| ErrTimeoutExceeded  | 6         | Time limit reached (process SIGKILLed)          |
| ErrNotImplemented   | 70        | Real bwrap invocation (stubbed in this version) |

All errors use `fmt.Errorf("%w: ...")` wrapping so `errors.Is()` works correctly
through the call chain.

---

## 8. Data Types

### SandboxConfig

```go
type SandboxConfig struct {
    SessionID    string
    Isolation    IsolationLevel
    Workdir      string
    TimeLimit    int        // seconds, 0=unlimited
    MemoryLimit  int        // MB, 0=unlimited
    Network      NetworkMode
    GPU          bool
    DryRun       bool
    Verbose      bool
    Command      []string
    SessionRoot  string     // default: /tmp/helix-sandbox
    BwrapPath    string     // default: /usr/bin/bwrap
    CgroupRoot   string     // default: /sys/fs/cgroup
}
```

### MountPoint

```go
type MountPoint struct {
    Source   string
    Target   string
    ReadOnly bool
    Kind     MountKind  // MountBind | MountProc | MountDev | MountTmpfs
}
```

### MountSpec

```go
type MountSpec struct {
    Mounts       []MountPoint
    UnshareNet   bool
    UnsharePID   bool
    DieWithParent bool
    UnsetEnv     bool
    SetEnv       map[string]string
}
```

---

## 9. Security Properties

The sandbox guarantees the following for `workspace` and `full` isolation:

1. **No home directory access.** `/home`, `/root`, `~/.hermes`, `~/.ssh` are never
   mounted. The only writable paths are the session workspace and `/tmp`.

2. **No network access.** `--unshare-net` creates a private network namespace with
   only loopback. All outbound connections fail.

3. **PID isolation.** `--unshare-pid` prevents the sandboxed process from seeing or
   signalling host processes.

4. **Memory bounds.** `memory.max` in cgroup v2 triggers OOM-kill if the limit is
   exceeded.

5. **Time bounds.** A context deadline sends SIGKILL to the entire process group
   when the time limit expires.

6. **No GPU in full mode.** Device nodes `/dev/dri/*` and `/dev/nvidia*` are
   explicitly excluded from the full isolation mount spec.

7. **Die with parent.** `--die-with-parent` ensures no orphaned sandbox processes
   survive helix-sandbox exit.

---

## 10. Environment Variable Overrides

| Variable                       | Default            | Purpose                          |
|--------------------------------|--------------------|----------------------------------|
| HELIX_SANDBOX_SESSION_ROOT     | /tmp/helix-sandbox | Base directory for session dirs  |
| HELIX_SANDBOX_BWRAP            | /usr/bin/bwrap     | Path to bubblewrap binary        |
| HELIX_SANDBOX_CGROUP_ROOT      | /sys/fs/cgroup     | Cgroup v2 mount point            |

---

## 11. Test Strategy

| Layer        | What                                     | Method                        |
|--------------|------------------------------------------|-------------------------------|
| Unit         | Config validation (all failure paths)    | Pure unit, table-driven       |
| Unit         | IsolationLevel methods                   | Pure unit                     |
| Unit         | Mount point generation per level         | Compare to expected specs     |
| Unit         | Error type construction                  | errors.Is checks              |
| Unit         | Shell escaping (special chars)           | Pure unit                     |
| Integration  | Dry-run output matches expected bwrap    | Compare to golden output      |
| Integration  | Session directory create + cleanup       | Filesystem check              |
| E2E          | `echo hello` in workspace mode           | Real bwrap, check stdout      |
| E2E          | Network isolation (curl/ping fails)      | Real bwrap                    |
| E2E          | Filesystem isolation (cannot read /home) | Real bwrap                    |
| E2E          | Memory limit enforcement (OOM-kill)      | Real bwrap + cgroup           |
| E2E          | Time limit enforcement (SIGKILL)         | Real bwrap + timeout          |

---

## 12. Implementation Status

| Component                    | Status   | Notes                                       |
|------------------------------|----------|---------------------------------------------|
| CLI flag parsing             | Done     | stdlib flag package                         |
| Config validation            | Done     | All fields checked                          |
| Isolation level logic        | Done     | none, workspace, full                       |
| Mount spec builders          | Done     | Host path existence checked                 |
| Cgroup v2 setup              | Done     | Graceful degradation if not writable        |
| Session dir management       | Done     | Create + cleanup                            |
| Dry-run mode                 | Done     | Prints exact bwrap command                  |
| Shell escaping               | Done     | Handles all shell metacharacters            |
| Error taxonomy               | Done     | 5 error types + ErrNotImplemented           |
| **Real bwrap execution**     | **Stub** | Returns ErrNotImplemented                   |
| **Timeout enforcement**      | **Stub** | execContext ready, not wired to Run()       |
| **Signal handling**          | **Stub** | killProcessGroup ready, not wired           |

### Wiring the Real Execution (Future)

To complete the implementation:

1. In `BwrapExecutor.Run()`, replace the `ErrNotImplemented` return with a call
   to `execContext(ctx, cfg.BwrapPath, bwrapArgs...)`.
2. Ensure the cgroup PID is written to `cgroup.procs` after the process starts
   (requires `--unshare-pid` + cgroup2 integration or systemd-run).
3. Handle `context.DeadlineExceeded` by calling `killProcessGroup(cmd.Process.Pid)`.
4. Add the session cleanup to a `defer` chain that executes before returning.

The `execContext` and `killProcessGroup` functions are already implemented and
tested for compilation — they just need to be called from `Run()`.

---

## 13. Verification

```
$ go build ./cmd/sandbox   # exits 0
$ go vet ./...             # clean
$ go list -m all           # only stdlib + this module
```
