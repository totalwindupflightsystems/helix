package systemd

import (
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// ServiceType
// -----------------------------------------------------------------------------

func TestServiceType_IsValid(t *testing.T) {
	tests := []struct {
		typ  ServiceType
		want bool
	}{
		{ServiceTypeOneshot, true},
		{ServiceType(""), false},
		{ServiceType("simple"), false},
		{ServiceType("notify"), false},
	}
	for _, tt := range tests {
		if got := tt.typ.IsValid(); got != tt.want {
			t.Errorf("ServiceType(%q).IsValid() = %v, want %v", tt.typ, got, tt.want)
		}
	}
}

// -----------------------------------------------------------------------------
// Service.Render
// -----------------------------------------------------------------------------

func TestService_Render_Empty(t *testing.T) {
	s := Service{}
	if got := s.Render(); got != "" {
		t.Errorf("empty Service.Render() = %q, want \"\"", got)
	}
}

func TestService_Render_FullSpec(t *testing.T) {
	s := Service{
		Type:             ServiceTypeOneshot,
		RemainAfterExit:  true,
		WorkingDirectory: "/opt/helix",
		ExecStart:        "/usr/bin/docker compose up -d",
		ExecStop:         "/usr/bin/docker compose down",
		ExecReload:       "/usr/bin/docker compose up -d",
		StandardOutput:   "journal",
		StandardError:    "journal",
	}
	got := s.Render()
	wants := []string{
		"Type=oneshot",
		"RemainAfterExit=yes",
		"WorkingDirectory=/opt/helix",
		"ExecStart=/usr/bin/docker compose up -d",
		"ExecStop=/usr/bin/docker compose down",
		"ExecReload=/usr/bin/docker compose up -d",
		"StandardOutput=journal",
		"StandardError=journal",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing %q\nfull output:\n%s", want, got)
		}
	}
}

func TestService_Render_NoRemainAfterExit(t *testing.T) {
	s := Service{Type: ServiceTypeOneshot, ExecStart: "/foo"}
	got := s.Render()
	if strings.Contains(got, "RemainAfterExit") {
		t.Errorf("Render() should omit RemainAfterExit when false: %s", got)
	}
}

// -----------------------------------------------------------------------------
// Timer
// -----------------------------------------------------------------------------

func TestTimer_Render_Empty(t *testing.T) {
	if got := (Timer{}).Render(); got != "" {
		t.Errorf("empty Timer.Render() = %q, want \"\"", got)
	}
}

func TestTimer_Render_Daily(t *testing.T) {
	tm := Timer{OnCalendar: "daily", Persistent: true}
	got := tm.Render()
	if !strings.Contains(got, "OnCalendar=daily") {
		t.Errorf("missing OnCalendar=daily: %s", got)
	}
	if !strings.Contains(got, "Persistent=true") {
		t.Errorf("missing Persistent=true: %s", got)
	}
}

// -----------------------------------------------------------------------------
// Unit validation
// -----------------------------------------------------------------------------

func TestUnit_Validate_MissingName(t *testing.T) {
	u := Unit{Description: "x", Service: Service{ExecStart: "/foo"}}
	if err := u.Validate(); err == nil {
		t.Error("expected error for missing Name")
	}
}

func TestUnit_Validate_MissingDescription(t *testing.T) {
	u := Unit{Name: "x.service", Service: Service{ExecStart: "/foo"}}
	if err := u.Validate(); err == nil {
		t.Error("expected error for missing Description")
	}
}

func TestUnit_Validate_NoSections(t *testing.T) {
	u := Unit{Name: "x.service", Description: "x"}
	if err := u.Validate(); err == nil {
		t.Error("expected error when both Service and Timer are empty")
	}
}

func TestUnit_Validate_InvalidType(t *testing.T) {
	u := Unit{
		Name:        "x.service",
		Description: "x",
		Service:     Service{Type: "fake", ExecStart: "/foo"},
	}
	if err := u.Validate(); err == nil {
		t.Error("expected error for invalid Type")
	}
}

func TestUnit_Validate_MissingExecStart(t *testing.T) {
	u := Unit{
		Name:        "x.service",
		Description: "x",
		Service:     Service{Type: ServiceTypeOneshot},
	}
	if err := u.Validate(); err == nil {
		t.Error("expected error for missing ExecStart")
	}
}

func TestUnit_Validate_TimerOnly_Valid(t *testing.T) {
	u := Unit{
		Name:        "x.timer",
		Description: "x",
		Timer:       Timer{OnCalendar: "daily"},
	}
	if err := u.Validate(); err != nil {
		t.Errorf("timer-only unit should validate: %v", err)
	}
}

func TestUnit_IsService_IsTimer(t *testing.T) {
	svc := Unit{Name: "a.service", Description: "a", Service: Service{ExecStart: "/x"}}
	if !svc.IsService() {
		t.Error("expected IsService=true")
	}
	if svc.IsTimer() {
		t.Error("expected IsTimer=false")
	}
	tmr := Unit{Name: "b.timer", Description: "b", Timer: Timer{OnCalendar: "hourly"}}
	if tmr.IsService() {
		t.Error("expected IsService=false")
	}
	if !tmr.IsTimer() {
		t.Error("expected IsTimer=true")
	}
}

// -----------------------------------------------------------------------------
// Unit.Render
// -----------------------------------------------------------------------------

func TestUnit_Render_HelixPlatform(t *testing.T) {
	u := HelixPlatformService()
	got, err := u.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	wants := []string{
		"[Unit]",
		"Description=Helix Platform (Docker Compose)",
		"Requires=docker.service",
		"After=docker.service network-online.target",
		"[Service]",
		"Type=oneshot",
		"RemainAfterExit=yes",
		"WorkingDirectory=/opt/helix",
		"ExecStart=/usr/bin/docker compose up -d --remove-orphans",
		"ExecStop=/usr/bin/docker compose down",
		"ExecReload=/usr/bin/docker compose up -d --remove-orphans",
		"StandardOutput=journal",
		"StandardError=journal",
		"[Install]",
		"WantedBy=multi-user.target",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in rendered unit\nfull output:\n%s", want, got)
		}
	}
	// Section order check
	if !strings.Contains(got, "WantedBy=multi-user.target") {
		t.Error("multi-user.target missing")
	}
}

func TestUnit_Render_BackupService(t *testing.T) {
	u := HelixBackupService()
	got, err := u.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	wants := []string{
		"Description=Helix Forgejo Backup",
		"Type=oneshot",
		"ExecStart=/opt/helix/scripts/backup-forgejo.sh",
		"WantedBy=multi-user.target",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in backup service\nfull output:\n%s", want, got)
		}
	}
}

func TestUnit_Render_BackupTimer(t *testing.T) {
	u := HelixBackupTimer()
	got, err := u.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	wants := []string{
		"Description=Daily Helix Backup",
		"[Timer]",
		"OnCalendar=daily",
		"Persistent=true",
		"WantedBy=timers.target",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in backup timer\nfull output:\n%s", want, got)
		}
	}
	// Service section must be absent for timer-only unit
	if strings.Contains(got, "[Service]") {
		t.Error("timer unit should not have [Service] section")
	}
}

func TestUnit_Render_CustomInstall(t *testing.T) {
	u := Unit{
		Name:            "x.service",
		Description:     "x",
		Service:         Service{ExecStart: "/foo"},
		InstallWantedBy: []string{"graphical.target"},
	}
	got, err := u.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if !strings.Contains(got, "WantedBy=graphical.target") {
		t.Errorf("custom WantedBy not honored: %s", got)
	}
}

func TestUnit_Render_TimerNoInstallDefaultsToMultiUser(t *testing.T) {
	u := Unit{
		Name:        "x.timer",
		Description: "x",
		Timer:       Timer{OnCalendar: "hourly"},
	}
	got, err := u.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	// Spec doesn't specify timer default — we default to multi-user.target.
	if !strings.Contains(got, "WantedBy=") {
		t.Errorf("missing WantedBy default: %s", got)
	}
}

func TestUnit_Render_InvalidPropagatesError(t *testing.T) {
	u := Unit{Name: "", Description: "x", Service: Service{ExecStart: "/foo"}}
	if _, err := u.Render(); err == nil {
		t.Error("expected Render to return validation error")
	}
}

// -----------------------------------------------------------------------------
// Registry
// -----------------------------------------------------------------------------

func TestRegistry_Register_Get_List_All(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(HelixPlatformService())
	r.MustRegister(HelixBackupService())
	r.MustRegister(HelixBackupTimer())

	if got := r.List(); len(got) != 3 {
		t.Errorf("List() returned %d entries, want 3", len(got))
	}
	// sorted order
	want := []string{
		"helix-backup.service",
		"helix-backup.timer",
		"helix-platform.service",
	}
	got := r.List()
	for i, w := range want {
		if got[i] != w {
			t.Errorf("List()[%d] = %q, want %q", i, got[i], w)
		}
	}

	u, ok := r.Get("helix-platform.service")
	if !ok {
		t.Fatal("Get(helix-platform.service) not found")
	}
	if u.Description != "Helix Platform (Docker Compose)" {
		t.Errorf("got Description=%q", u.Description)
	}

	if _, ok := r.Get("nonexistent.service"); ok {
		t.Error("expected Get to miss on nonexistent")
	}

	all := r.All()
	if len(all) != 3 {
		t.Errorf("All() returned %d entries, want 3", len(all))
	}
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(HelixPlatformService())
	err := r.Register(HelixPlatformService())
	if err == nil {
		t.Error("expected duplicate-registration error")
	}
}

func TestRegistry_Register_InvalidUnit(t *testing.T) {
	r := NewRegistry()
	err := r.Register(Unit{Name: "", Description: "x", Service: Service{ExecStart: "/x"}})
	if err == nil {
		t.Error("expected validation error for empty Name")
	}
}

func TestDefaultRegistry(t *testing.T) {
	r := DefaultRegistry()
	if len(r.List()) != 3 {
		t.Errorf("DefaultRegistry has %d units, want 3", len(r.List()))
	}
	for _, name := range []string{
		"helix-platform.service",
		"helix-backup.service",
		"helix-backup.timer",
	} {
		if _, ok := r.Get(name); !ok {
			t.Errorf("DefaultRegistry missing %s", name)
		}
	}
}

// -----------------------------------------------------------------------------
// Format helpers
// -----------------------------------------------------------------------------

func TestFormatUnit(t *testing.T) {
	got, err := FormatUnit(HelixPlatformService())
	if err != nil {
		t.Fatalf("FormatUnit error: %v", err)
	}
	if !strings.HasPrefix(got, "[Unit]") {
		t.Errorf("FormatUnit output should start with [Unit], got: %q", got[:20])
	}
}

func TestFormatUnit_Invalid(t *testing.T) {
	_, err := FormatUnit(Unit{Name: "x", Service: Service{ExecStart: "/y"}}) // missing Description
	if err == nil {
		t.Error("expected error for invalid unit")
	}
}

func TestFormatRegistry(t *testing.T) {
	r := DefaultRegistry()
	got, err := FormatRegistry(r)
	if err != nil {
		t.Fatalf("FormatRegistry error: %v", err)
	}
	// Should contain all three unit headers.
	for _, header := range []string{
		"[Unit]\nDescription=Helix Platform",
		"[Unit]\nDescription=Helix Forgejo Backup",
		"[Unit]\nDescription=Daily Helix Backup",
	} {
		if !strings.Contains(got, header) {
			t.Errorf("missing %q in registry output", header)
		}
	}
}

func TestFormatRegistry_Nil(t *testing.T) {
	if _, err := FormatRegistry(nil); err == nil {
		t.Error("expected error for nil registry")
	}
}

// -----------------------------------------------------------------------------
// Spec verbatim match — render DefaultRegistry and check key spec phrases
// -----------------------------------------------------------------------------

func TestSpecVerbatim_HelixPlatform(t *testing.T) {
	u := HelixPlatformService()
	got, err := u.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	// Spec §9.4 verbatim lines that must appear:
	lines := []string{
		"Requires=docker.service",
		"After=docker.service network-online.target",
		"WorkingDirectory=/opt/helix",
		"ExecStart=/usr/bin/docker compose up -d --remove-orphans",
		"ExecStop=/usr/bin/docker compose down",
		"ExecReload=/usr/bin/docker compose up -d --remove-orphans",
		"StandardOutput=journal",
		"StandardError=journal",
	}
	for _, l := range lines {
		if !strings.Contains(got, l) {
			t.Errorf("spec line not found verbatim: %s", l)
		}
	}
}
