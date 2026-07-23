// Package systemd encodes the Helix systemd unit templates as structured
// Go data, enabling programmatic validation and rendering of platform unit
// definitions.
//
// Data is derived from specs/SPECIFICATION.md §9.4 (systemd Units).
package systemd

import (
	"fmt"
	"sort"
	"strings"
)

// -----------------------------------------------------------------------------
// Service type
// -----------------------------------------------------------------------------

// ServiceType mirrors the systemd [Service] Type= directive.
type ServiceType string

const (
	ServiceTypeOneshot ServiceType = "oneshot"
	// ServiceTypeSimple, ServiceTypeNotify, etc. could be added later.
)

// IsValid reports whether t is a recognized service type.
func (t ServiceType) IsValid() bool {
	switch t {
	case ServiceTypeOneshot:
		return true
	default:
		return false
	}
}

// -----------------------------------------------------------------------------
// Service struct — mirrors the [Service] section
// -----------------------------------------------------------------------------

// Service mirrors a systemd [Service] section.
type Service struct {
	Type             ServiceType
	RemainAfterExit  bool
	WorkingDirectory string
	ExecStart        string
	ExecStop         string
	ExecReload       string
	StandardOutput   string
	StandardError    string
}

// Render emits the [Service] section body (without section header).
// Returns an empty string if no fields are set.
func (s Service) Render() string {
	var b strings.Builder
	if s.Type != "" {
		fmt.Fprintf(&b, "Type=%s\n", s.Type)
	}
	if s.RemainAfterExit {
		b.WriteString("RemainAfterExit=yes\n")
	}
	if s.WorkingDirectory != "" {
		fmt.Fprintf(&b, "WorkingDirectory=%s\n", s.WorkingDirectory)
	}
	if s.ExecStart != "" {
		fmt.Fprintf(&b, "ExecStart=%s\n", s.ExecStart)
	}
	if s.ExecStop != "" {
		fmt.Fprintf(&b, "ExecStop=%s\n", s.ExecStop)
	}
	if s.ExecReload != "" {
		fmt.Fprintf(&b, "ExecReload=%s\n", s.ExecReload)
	}
	if s.StandardOutput != "" {
		fmt.Fprintf(&b, "StandardOutput=%s\n", s.StandardOutput)
	}
	if s.StandardError != "" {
		fmt.Fprintf(&b, "StandardError=%s\n", s.StandardError)
	}
	return b.String()
}

// IsEmpty reports whether the Service has no fields set.
func (s Service) IsEmpty() bool {
	return s == Service{}
}

// -----------------------------------------------------------------------------
// Timer struct — mirrors the [Timer] section
// -----------------------------------------------------------------------------

// Timer mirrors a systemd [Timer] section.
type Timer struct {
	OnCalendar string
	Persistent bool
}

// Render emits the [Timer] section body.
func (t Timer) Render() string {
	var b strings.Builder
	if t.OnCalendar != "" {
		fmt.Fprintf(&b, "OnCalendar=%s\n", t.OnCalendar)
	}
	if t.Persistent {
		b.WriteString("Persistent=true\n")
	}
	return b.String()
}

// IsEmpty reports whether the Timer has no fields set.
func (t Timer) IsEmpty() bool {
	return t == Timer{}
}

// -----------------------------------------------------------------------------
// Unit struct — top-level unit file
// -----------------------------------------------------------------------------

// Unit represents a single systemd unit file.
type Unit struct {
	Name        string   // e.g. "helix-platform.service"
	Description string   // [Unit] Description=
	Requires    []string // [Unit] Requires=
	After       []string // [Unit] After=
	Service     Service
	Timer       Timer
	// InstallWantedBy is the [Install] WantedBy= list. Defaults to
	// ["multi-user.target"] when unset via DefaultInstall.
	InstallWantedBy []string
}

// Validate ensures required fields are present. Returns an error describing
// the first missing field.
func (u Unit) Validate() error {
	if u.Name == "" {
		return fmt.Errorf("unit Name is required")
	}
	if u.Description == "" {
		return fmt.Errorf("unit %q: Description is required", u.Name)
	}
	if u.Service.IsEmpty() && u.Timer.IsEmpty() {
		return fmt.Errorf("unit %q: must have Service or Timer section", u.Name)
	}
	if !u.Service.IsEmpty() {
		if u.Service.Type != "" && !u.Service.Type.IsValid() {
			return fmt.Errorf("unit %q: invalid Service.Type %q", u.Name, u.Service.Type)
		}
		if u.Service.ExecStart == "" {
			return fmt.Errorf("unit %q: Service.ExecStart is required", u.Name)
		}
	}
	return nil
}

// IsService reports whether this unit has a [Service] section.
func (u Unit) IsService() bool { return !u.Service.IsEmpty() }

// IsTimer reports whether this unit has a [Timer] section.
func (u Unit) IsTimer() bool { return !u.Timer.IsEmpty() }

// Render emits the full unit file content in canonical systemd syntax.
func (u Unit) Render() (string, error) {
	if err := u.Validate(); err != nil {
		return "", err
	}

	var b strings.Builder
	// [Unit]
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=%s\n", u.Description)
	if len(u.Requires) > 0 {
		fmt.Fprintf(&b, "Requires=%s\n", strings.Join(u.Requires, " "))
	}
	if len(u.After) > 0 {
		fmt.Fprintf(&b, "After=%s\n", strings.Join(u.After, " "))
	}

	// [Service]
	if !u.Service.IsEmpty() {
		b.WriteString("\n[Service]\n")
		b.WriteString(u.Service.Render())
	}

	// [Timer]
	if !u.Timer.IsEmpty() {
		b.WriteString("\n[Timer]\n")
		b.WriteString(u.Timer.Render())
	}

	// [Install]
	if !u.Service.IsEmpty() || !u.Timer.IsEmpty() {
		b.WriteString("\n[Install]\n")
		wantedBy := u.InstallWantedBy
		if len(wantedBy) == 0 {
			wantedBy = []string{"multi-user.target"}
		}
		fmt.Fprintf(&b, "WantedBy=%s\n", strings.Join(wantedBy, " "))
	}
	return b.String(), nil
}

// -----------------------------------------------------------------------------
// Registry
// -----------------------------------------------------------------------------

// Registry holds a collection of named units.
type Registry struct {
	units map[string]Unit
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{units: make(map[string]Unit)}
}

// Register adds a unit. Returns an error if a unit with the same Name already
// exists or the unit fails validation.
func (r *Registry) Register(u Unit) error {
	if _, exists := r.units[u.Name]; exists {
		return fmt.Errorf("unit %q already registered", u.Name)
	}
	if err := u.Validate(); err != nil {
		return err
	}
	r.units[u.Name] = u
	return nil
}

// MustRegister adds a unit and panics on error. Intended for package-level
// initialization.
func (r *Registry) MustRegister(u Unit) {
	if err := r.Register(u); err != nil {
		panic(err)
	}
}

// Get returns the unit with the given name and whether it was found.
func (r *Registry) Get(name string) (Unit, bool) {
	u, ok := r.units[name]
	return u, ok
}

// List returns all unit names in sorted order.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.units))
	for name := range r.units {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// All returns all units keyed by name (snapshot copy).
func (r *Registry) All() map[string]Unit {
	out := make(map[string]Unit, len(r.units))
	for k, v := range r.units {
		out[k] = v
	}
	return out
}

// -----------------------------------------------------------------------------
// Spec canonical templates (specs/SPECIFICATION.md §9.4)
// -----------------------------------------------------------------------------

// HelixPlatformService returns the spec §9.4 helix-platform.service definition.
func HelixPlatformService() Unit {
	return Unit{
		Name:        "helix-platform.service",
		Description: "Helix Platform (Docker Compose)",
		Requires:    []string{"docker.service"},
		After:       []string{"docker.service", "network-online.target"},
		Service: Service{
			Type:             ServiceTypeOneshot,
			RemainAfterExit:  true,
			WorkingDirectory: "/opt/helix",
			ExecStart:        "/usr/bin/docker compose up -d --remove-orphans",
			ExecStop:         "/usr/bin/docker compose down",
			ExecReload:       "/usr/bin/docker compose up -d --remove-orphans",
			StandardOutput:   "journal",
			StandardError:    "journal",
		},
	}
}

// HelixBackupService returns the spec §9.4 helix-backup.service definition.
func HelixBackupService() Unit {
	return Unit{
		Name:        "helix-backup.service",
		Description: "Helix Forgejo Backup",
		Service: Service{
			Type:      ServiceTypeOneshot,
			ExecStart: "/opt/helix/scripts/backup-forgejo.sh",
		},
	}
}

// HelixBackupTimer returns the spec §9.4 helix-backup.timer definition.
// Per the spec, the timer's [Install] section uses WantedBy=timers.target
// (not the default multi-user.target).
func HelixBackupTimer() Unit {
	return Unit{
		Name:        "helix-backup.timer",
		Description: "Daily Helix Backup",
		Timer: Timer{
			OnCalendar: "daily",
			Persistent: true,
		},
		InstallWantedBy: []string{"timers.target"},
	}
}

// DefaultRegistry returns a Registry pre-populated with all three spec units.
// It returns an error if any canonical unit fails validation or registration.
func DefaultRegistry() (*Registry, error) {
	r := NewRegistry()
	if err := r.Register(HelixPlatformService()); err != nil {
		return nil, err
	}
	if err := r.Register(HelixBackupService()); err != nil {
		return nil, err
	}
	if err := r.Register(HelixBackupTimer()); err != nil {
		return nil, err
	}
	return r, nil
}

// -----------------------------------------------------------------------------
// Formatting helpers
// -----------------------------------------------------------------------------

// FormatUnit renders a single unit and returns the rendered text plus an
// error if the unit is invalid.
func FormatUnit(u Unit) (string, error) {
	return u.Render()
}

// FormatRegistry renders every unit in the registry joined by two newlines.
// Returns the first validation error encountered.
func FormatRegistry(r *Registry) (string, error) {
	if r == nil {
		return "", fmt.Errorf("registry is nil")
	}
	var parts []string
	for _, name := range r.List() {
		u, _ := r.Get(name)
		rendered, err := u.Render()
		if err != nil {
			return "", err
		}
		parts = append(parts, rendered)
	}
	return strings.Join(parts, "\n\n"), nil
}
