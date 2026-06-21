package identity

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// 1. AgentStatus.Valid
// -----------------------------------------------------------------------------

func TestAgentStatus_Valid(t *testing.T) {
	cases := []struct {
		name   string
		status AgentStatus
		want   bool
	}{
		{"active", StatusActive, true},
		{"pending", StatusPending, true},
		{"offboarded", StatusOffboarded, true},
		{"empty", AgentStatus(""), false},
		{"unknown", AgentStatus("unknown"), false},
		{"uppercase_active", AgentStatus("ACTIVE"), false},
		{"whitespace", AgentStatus(" active"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.status.Valid(); got != tc.want {
				t.Errorf("Valid() = %v, want %v", got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 2. AgentStatus.Provisionable
// -----------------------------------------------------------------------------

func TestAgentStatus_Provisionable(t *testing.T) {
	cases := []struct {
		name   string
		status AgentStatus
		want   bool
	}{
		{"active", StatusActive, true},
		{"pending", StatusPending, false},
		{"offboarded", StatusOffboarded, false},
		{"empty", AgentStatus(""), false},
		{"unknown", AgentStatus("unknown"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.status.Provisionable(); got != tc.want {
				t.Errorf("Provisionable() = %v, want %v", got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 3. AgentTier.Valid
// -----------------------------------------------------------------------------

func TestAgentTier_Valid(t *testing.T) {
	cases := []struct {
		name string
		tier AgentTier
		want bool
	}{
		{"pro", TierPro, true},
		{"flash", TierFlash, true},
		{"empty", AgentTier(""), false},
		{"basic", AgentTier("basic"), false},
		{"uppercase_pro", AgentTier("PRO"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.tier.Valid(); got != tc.want {
				t.Errorf("Valid() = %v, want %v", got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 4. Agent.Email
// -----------------------------------------------------------------------------

func TestAgent_Email(t *testing.T) {
	t.Run("standard", func(t *testing.T) {
		a := &Agent{Name: "wojons", Status: StatusActive}
		got := a.Email()
		want := "wojons@helix-agents.local"
		if got != want {
			t.Errorf("Email() = %q, want %q", got, want)
		}
	})
	t.Run("empty_name_uses_status", func(t *testing.T) {
		// Defensive fallback per source comment: should never happen in
		// practice, but the function must still produce a valid @-address.
		a := &Agent{Name: "", Status: StatusActive}
		got := a.Email()
		want := "active@helix-agents.local"
		if got != want {
			t.Errorf("Email() = %q, want %q", got, want)
		}
	})
	t.Run("empty_name_pending_status", func(t *testing.T) {
		a := &Agent{Name: "", Status: StatusPending}
		if got := a.Email(); got != "pending@helix-agents.local" {
			t.Errorf("Email() = %q, want %q", got, "pending@helix-agents.local")
		}
	})
}

// -----------------------------------------------------------------------------
// 5. Agent.KeyTitle
// -----------------------------------------------------------------------------

func TestAgent_KeyTitle(t *testing.T) {
	cases := []struct {
		name  string
		agent *Agent
		want  string
	}{
		{
			name:  "pro_agent",
			agent: &Agent{Name: "wojons", Tier: TierPro},
			want:  "Helix Agent — wojons (pro)",
		},
		{
			name:  "flash_agent",
			agent: &Agent{Name: "llopez", Tier: TierFlash},
			want:  "Helix Agent — llopez (flash)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.agent.KeyTitle(); got != tc.want {
				t.Errorf("KeyTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 6. Agent.Validate
// -----------------------------------------------------------------------------

func TestAgent_Validate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		a := &Agent{Name: "wojons", DisplayName: "Wojons", Status: StatusActive, Tier: TierPro}
		if err := a.Validate(); err != nil {
			t.Errorf("Validate() unexpected error: %v", err)
		}
	})
	t.Run("empty_name", func(t *testing.T) {
		a := &Agent{Name: "", DisplayName: "X", Status: StatusActive, Tier: TierPro}
		err := a.Validate()
		if err == nil {
			t.Fatal("Validate() expected error for empty name, got nil")
		}
		var te *TypedError
		if !errors.As(err, &te) {
			t.Fatalf("Validate() error is not *TypedError: %T", err)
		}
		if te.Kind != ErrKindConfig {
			t.Errorf("Validate() kind = %q, want %q", te.Kind, ErrKindConfig)
		}
	})
	t.Run("invalid_status", func(t *testing.T) {
		a := &Agent{Name: "wojons", Status: AgentStatus("unknown"), Tier: TierPro}
		err := a.Validate()
		if err == nil {
			t.Fatal("Validate() expected error for invalid status, got nil")
		}
		if !strings.Contains(err.Error(), "invalid status") {
			t.Errorf("Validate() error = %q, want substring %q", err.Error(), "invalid status")
		}
	})
	t.Run("invalid_tier", func(t *testing.T) {
		a := &Agent{Name: "wojons", Status: StatusActive, Tier: AgentTier("basic")}
		err := a.Validate()
		if err == nil {
			t.Fatal("Validate() expected error for invalid tier, got nil")
		}
		if !strings.Contains(err.Error(), "invalid tier") {
			t.Errorf("Validate() error = %q, want substring %q", err.Error(), "invalid tier")
		}
	})
}

// -----------------------------------------------------------------------------
// 7. NewCreateUserRequest
// -----------------------------------------------------------------------------

func TestNewCreateUserRequest(t *testing.T) {
	a := &Agent{
		Name:        "wojons",
		DisplayName: "Wojons",
		Status:      StatusActive,
		Tier:        TierPro,
	}
	tempPwd := "TempPass123!@#"
	req := NewCreateUserRequest(a, tempPwd)
	if req == nil {
		t.Fatal("NewCreateUserRequest returned nil")
	}
	if req.Username != "wojons" {
		t.Errorf("Username = %q, want %q", req.Username, "wojons")
	}
	if req.LoginName != "wojons" {
		t.Errorf("LoginName = %q, want %q", req.LoginName, "wojons")
	}
	if req.FullName != "Wojons" {
		t.Errorf("FullName = %q, want %q", req.FullName, "Wojons")
	}
	if req.Email != "wojons@helix-agents.local" {
		t.Errorf("Email = %q, want %q", req.Email, "wojons@helix-agents.local")
	}
	if req.Password != tempPwd {
		t.Errorf("Password = %q, want %q", req.Password, tempPwd)
	}
	if !req.MustChangePassword {
		t.Error("MustChangePassword = false, want true")
	}
	if req.SendNotify {
		t.Error("SendNotify = true, want false (no real mailbox)")
	}
	if req.SourceID != 0 {
		t.Errorf("SourceID = %d, want 0", req.SourceID)
	}
	if req.Visibility != "limited" {
		t.Errorf("Visibility = %q, want %q", req.Visibility, "limited")
	}
}

// -----------------------------------------------------------------------------
// 8. DefaultPermission
// -----------------------------------------------------------------------------

func TestDefaultPermission(t *testing.T) {
	p := DefaultPermission()
	cases := []struct {
		name string
		got  bool
		want bool
	}{
		{"CanReadAllRepos", p.CanReadAllRepos, true},
		{"CanCreateRepos", p.CanCreateRepos, true},
		{"CanPushToFeatBranches", p.CanPushToFeatBranches, true},
		{"CanPushToMain", p.CanPushToMain, false},
		{"CanOpenPRs", p.CanOpenPRs, true},
		{"CanMergeSolo", p.CanMergeSolo, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 9. AgentPermission.PermissionScopes
// -----------------------------------------------------------------------------

func TestAgentPermission_PermissionScopes(t *testing.T) {
	t.Run("default_permission", func(t *testing.T) {
		scopes := DefaultPermission().PermissionScopes()
		if len(scopes) == 0 {
			t.Fatal("PermissionScopes() returned empty slice for default permission")
		}
		// Always-present scopes
		must := []string{"read:user", "write:user"}
		for _, m := range must {
			found := false
			for _, s := range scopes {
				if s == m {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("scopes missing %q (got %v)", m, scopes)
			}
		}
		// Default permission should grant these because CanReadAllRepos +
		// CanPushToFeatBranches + CanOpenPRs are all true.
		expected := []string{
			"read:repository",
			"write:repository",
			"read:issue",
			"write:issue",
		}
		for _, e := range expected {
			found := false
			for _, s := range scopes {
				if s == e {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("scopes missing %q for default permission (got %v)", e, scopes)
			}
		}
	})
	t.Run("minimal_permission_only_user_scopes", func(t *testing.T) {
		// All capabilities false → only read:user + write:user.
		p := AgentPermission{}
		scopes := p.PermissionScopes()
		if len(scopes) != 2 {
			t.Errorf("scopes len = %d, want 2 (got %v)", len(scopes), scopes)
		}
		if scopes[0] != "read:user" || scopes[1] != "write:user" {
			t.Errorf("scopes = %v, want [read:user write:user]", scopes)
		}
	})
	t.Run("read_only_repo", func(t *testing.T) {
		p := AgentPermission{CanReadAllRepos: true}
		scopes := p.PermissionScopes()
		// read:repository, read:issue, read:user, write:user
		if len(scopes) != 4 {
			t.Errorf("scopes len = %d, want 4 (got %v)", len(scopes), scopes)
		}
	})
}

// -----------------------------------------------------------------------------
// 10. NewCreateTokenRequest
// -----------------------------------------------------------------------------

func TestNewCreateTokenRequest(t *testing.T) {
	a := &Agent{Name: "wojons", Status: StatusActive, Tier: TierPro}
	req := NewCreateTokenRequest(a)
	if req == nil {
		t.Fatal("NewCreateTokenRequest returned nil")
	}
	if req.Name != PATName {
		t.Errorf("Name = %q, want %q", req.Name, PATName)
	}
	if len(req.Scopes) == 0 {
		t.Error("Scopes is empty, want non-empty")
	}
}

// -----------------------------------------------------------------------------
// 11. ProvisioningResult.Succeeded
// -----------------------------------------------------------------------------

func TestProvisioningResult_Succeeded(t *testing.T) {
	cases := []struct {
		name   string
		action SyncAction
		want   bool
	}{
		{"created", ActionCreated, true},
		{"updated", ActionUpdated, true},
		{"unchanged", ActionUnchanged, true},
		{"deprovisioned", ActionDeprovisioned, true},
		{"skipped", ActionSkipped, true},
		{"failed", ActionFailed, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &ProvisioningResult{Action: tc.action}
			if got := r.Succeeded(); got != tc.want {
				t.Errorf("Succeeded() = %v, want %v", got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 12. TypedError.Error
// -----------------------------------------------------------------------------

func TestTypedError_Error(t *testing.T) {
	t.Run("without_cause", func(t *testing.T) {
		e := &TypedError{Kind: ErrKindConfig, Message: "bad input"}
		got := e.Error()
		want := "config: bad input"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})
	t.Run("with_cause", func(t *testing.T) {
		cause := errors.New("disk full")
		e := &TypedError{Kind: ErrKindInternal, Message: "write failed", Cause: cause}
		got := e.Error()
		want := "internal: write failed: disk full"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})
}

// -----------------------------------------------------------------------------
// 13. TypedError.Unwrap
// -----------------------------------------------------------------------------

func TestTypedError_Unwrap(t *testing.T) {
	cause := errors.New("inner cause")
	e := NewAPIError("api call failed", cause)
	got := e.Unwrap()
	if got != cause {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}
	// Verify errors.Unwrap also works (Go 1.13+ errors.Is/As integration).
	if !errors.Is(e, cause) {
		t.Error("errors.Is(typedError, cause) = false, want true")
	}
	t.Run("nil_cause", func(t *testing.T) {
		e2 := NewConfigError("no cause", nil)
		if got := e2.Unwrap(); got != nil {
			t.Errorf("Unwrap() = %v, want nil", got)
		}
	})
}

// -----------------------------------------------------------------------------
// 14. TypedError.ExitCode
// -----------------------------------------------------------------------------

func TestTypedError_ExitCode(t *testing.T) {
	cases := []struct {
		name string
		kind ErrorKind
		want int
	}{
		{"config", ErrKindConfig, ExitFileOrAuth},
		{"network", ErrKindNetwork, ExitConnRefused},
		{"api", ErrKindAPI, ExitFileOrAuth},
		{"partial", ErrKindPartial, ExitPartialFailure},
		{"internal", ErrKindInternal, ExitGeneral},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &TypedError{Kind: tc.kind, Message: "x"}
			if got := e.ExitCode(); got != tc.want {
				t.Errorf("ExitCode() = %d, want %d", got, tc.want)
			}
		})
	}
	t.Run("constants_match_exit_codes", func(t *testing.T) {
		// Sanity-check the spec values so a future accidental constant
		// change surfaces immediately.
		if ExitOK != 0 || ExitConnRefused != 1 || ExitGeneral != 2 ||
			ExitFileOrAuth != 3 || ExitPartialFailure != 4 {
			t.Errorf("unexpected exit constants: %d %d %d %d %d",
				ExitOK, ExitConnRefused, ExitGeneral, ExitFileOrAuth, ExitPartialFailure)
		}
	})
}

// -----------------------------------------------------------------------------
// 15. Error constructors — Kind and Message
// -----------------------------------------------------------------------------

func TestErrorConstructors(t *testing.T) {
	cause := errors.New("underlying")
	cases := []struct {
		name string
		got  *TypedError
		kind ErrorKind
		msg  string
	}{
		{"NewConfigError", NewConfigError("cfg", cause), ErrKindConfig, "cfg"},
		{"NewNetworkError", NewNetworkError("net", cause), ErrKindNetwork, "net"},
		{"NewAPIError", NewAPIError("api", cause), ErrKindAPI, "api"},
		{"NewPartialError", NewPartialError("partial", cause), ErrKindPartial, "partial"},
		{"NewInternalError", NewInternalError("internal", cause), ErrKindInternal, "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got == nil {
				t.Fatal("constructor returned nil")
			}
			if tc.got.Kind != tc.kind {
				t.Errorf("Kind = %q, want %q", tc.got.Kind, tc.kind)
			}
			if tc.got.Message != tc.msg {
				t.Errorf("Message = %q, want %q", tc.got.Message, tc.msg)
			}
			if tc.got.Cause != cause {
				t.Errorf("Cause = %v, want %v", tc.got.Cause, cause)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 16. GenerateKeyPair
// -----------------------------------------------------------------------------

func TestGenerateKeyPair(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error: %v", err)
	}
	if kp == nil {
		t.Fatal("GenerateKeyPair() returned nil")
	}
	if !strings.HasPrefix(kp.PublicKeyOpenSSH, "ssh-ed25519 ") {
		t.Errorf("PublicKeyOpenSSH = %q, want prefix %q", kp.PublicKeyOpenSSH, "ssh-ed25519 ")
	}
	if kp.PrivateKeyPEM == "" {
		t.Error("PrivateKeyPEM is empty")
	}
	if !strings.Contains(kp.PrivateKeyPEM, "PRIVATE KEY") {
		t.Errorf("PrivateKeyPEM missing PEM header, got %q", kp.PrivateKeyPEM)
	}
	if kp.PrivateKey == nil || len(kp.PrivateKey) == 0 {
		t.Error("PrivateKey is empty")
	}
	if !strings.HasPrefix(kp.Fingerprint, "SHA256:") {
		t.Errorf("Fingerprint = %q, want prefix %q", kp.Fingerprint, "SHA256:")
	}
	// Fingerprint must not carry base64 padding (per source comment).
	if strings.Contains(kp.Fingerprint, "=") {
		t.Errorf("Fingerprint has base64 padding: %q", kp.Fingerprint)
	}
	if kp.Passphrase != "" {
		t.Errorf("Passphrase = %q, want empty", kp.Passphrase)
	}
}

// -----------------------------------------------------------------------------
// 17. packSSHEd25519PublicKey
// -----------------------------------------------------------------------------

func TestPackSSHEd25519PublicKey(t *testing.T) {
	// Use a deterministic key for predictable byte assertions.
	pub, _, err := ed25519.GenerateKey(strings.NewReader("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	packed := packSSHEd25519PublicKey(pub)
	const wantLen = 4 + len("ssh-ed25519") + 4 + ed25519.PublicKeySize
	if len(packed) != wantLen {
		t.Errorf("packed len = %d, want %d (4 + 11 + 4 + 32)", len(packed), wantLen)
	}
	// First 4 bytes are big-endian uint32(len("ssh-ed25519")) = 11.
	if packed[0] != 0x00 || packed[1] != 0x00 || packed[2] != 0x00 || packed[3] != 0x0b {
		t.Errorf("first 4 bytes = % x, want 00 00 00 0b", packed[:4])
	}
	// Next 11 bytes should be "ssh-ed25519".
	if string(packed[4:15]) != "ssh-ed25519" {
		t.Errorf("name bytes = %q, want %q", packed[4:15], "ssh-ed25519")
	}
	// Bytes 15..18 are big-endian uint32(32).
	if packed[15] != 0x00 || packed[16] != 0x00 || packed[17] != 0x00 || packed[18] != 0x20 {
		t.Errorf("key len prefix = % x, want 00 00 00 20", packed[15:19])
	}
	// Trailing 32 bytes are the raw public key.
	if !equalBytes(packed[19:], []byte(pub)) {
		t.Error("trailing 32 bytes do not match the input public key")
	}
}

// equalBytes is a small helper to avoid importing bytes in this test.
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// -----------------------------------------------------------------------------
// 18. trimBase64Padding
// -----------------------------------------------------------------------------

func TestTrimBase64Padding(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"two_pad", "abcd==", "abcd"},
		{"one_pad", "abcd=", "abcd"},
		{"no_pad", "abcd", "abcd"},
		{"empty", "", ""},
		{"all_pad", "====", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := trimBase64Padding(tc.in); got != tc.want {
				t.Errorf("trimBase64Padding(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 19. GenerateTempPassword
// -----------------------------------------------------------------------------

func TestGenerateTempPassword(t *testing.T) {
	const wantLen = 32
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	pw, err := GenerateTempPassword()
	if err != nil {
		t.Fatalf("GenerateTempPassword() error: %v", err)
	}
	if len(pw) != wantLen {
		t.Errorf("len = %d, want %d", len(pw), wantLen)
	}
	for i, c := range pw {
		if !strings.ContainsRune(alphabet, c) {
			t.Errorf("char %d (%q) not in alphabet", i, c)
		}
	}
	// Two calls should (with overwhelming probability) produce different
	// values; this guards against a stateful RNG regression.
	pw2, _ := GenerateTempPassword()
	if pw == pw2 {
		t.Error("two consecutive calls produced identical passwords (RNG broken?)")
	}
}

// -----------------------------------------------------------------------------
// 20. MaskToken
// -----------------------------------------------------------------------------

func TestMaskToken(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"four_chars", "abcd", "****"},
		{"eight_chars", "abcdefgh", "****"},
		{"nine_chars", "abcdefghi", "****" + "bcdefghi"},
		{"thirty_two_chars", "12345678901234567890123456789012", "****" + "56789012"},
		{"empty", "", "****"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskToken(tc.in); got != tc.want {
				t.Errorf("MaskToken(len=%d) = %q, want %q", len(tc.in), got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 21. sortAgentsByName
// -----------------------------------------------------------------------------

func TestSortAgentsByName(t *testing.T) {
	t.Run("unsorted", func(t *testing.T) {
		agents := []*Agent{
			{Name: "charlie"},
			{Name: "alpha"},
			{Name: "bravo"},
		}
		sortAgentsByName(agents)
		got := []string{agents[0].Name, agents[1].Name, agents[2].Name}
		want := []string{"alpha", "bravo", "charlie"}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("agents[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
			}
		}
	})
	t.Run("already_sorted", func(t *testing.T) {
		agents := []*Agent{{Name: "a"}, {Name: "b"}, {Name: "c"}}
		sortAgentsByName(agents)
		got := []string{agents[0].Name, agents[1].Name, agents[2].Name}
		want := []string{"a", "b", "c"}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("agents[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})
	t.Run("single_element", func(t *testing.T) {
		agents := []*Agent{{Name: "solo"}}
		sortAgentsByName(agents)
		if len(agents) != 1 || agents[0].Name != "solo" {
			t.Errorf("agents = %v, want [{Name:solo}]", agents)
		}
	})
	t.Run("empty_slice", func(t *testing.T) {
		agents := []*Agent{}
		sortAgentsByName(agents) // must not panic
		if len(agents) != 0 {
			t.Errorf("agents len = %d, want 0", len(agents))
		}
	})
	t.Run("insertion_sort_stability", func(t *testing.T) {
		// Verify stability: equal names preserve original relative order.
		agents := []*Agent{
			{Name: "b", DisplayName: "first"},
			{Name: "a", DisplayName: "second"},
			{Name: "b", DisplayName: "third"},
		}
		sortAgentsByName(agents)
		if agents[0].Name != "a" {
			t.Errorf("agents[0].Name = %q, want a", agents[0].Name)
		}
		// The two "b" entries must retain their original order.
		if agents[1].DisplayName != "first" || agents[2].DisplayName != "third" {
			t.Errorf("stable order broken: got %q,%q want first,third",
				agents[1].DisplayName, agents[2].DisplayName)
		}
	})
}

// -----------------------------------------------------------------------------
// 22. NewStateFile
// -----------------------------------------------------------------------------

func TestNewStateFile(t *testing.T) {
	sf := NewStateFile()
	if sf == nil {
		t.Fatal("NewStateFile() returned nil")
	}
	if sf.Version != 1 {
		t.Errorf("Version = %d, want 1", sf.Version)
	}
	if sf.Agents == nil {
		t.Error("Agents is nil, want empty map")
	}
	if len(sf.Agents) != 0 {
		t.Errorf("Agents len = %d, want 0", len(sf.Agents))
	}
	if sf.Version != StateVersion {
		t.Errorf("Version = %d, want StateVersion (%d)", sf.Version, StateVersion)
	}
}

// -----------------------------------------------------------------------------
// 23. ProvisionerConfig.Validate
// -----------------------------------------------------------------------------

func TestProvisionerConfig_Validate(t *testing.T) {
	baseValid := func() ProvisionerConfig {
		c := DefaultProvisionerConfig()
		c.ForgejoURL = "https://forgejo.example.com"
		c.AdminToken = "secret-token"
		c.KnownFriendsPath = "/tmp/known-friends.json"
		return c
	}
	t.Run("empty_forgejo_url", func(t *testing.T) {
		c := baseValid()
		c.ForgejoURL = ""
		err := c.Validate()
		if err == nil {
			t.Fatal("expected error for empty ForgejoURL")
		}
		assertConfigErr(t, err)
	})
	t.Run("bad_forgejo_url", func(t *testing.T) {
		c := baseValid()
		c.ForgejoURL = "://invalid"
		err := c.Validate()
		if err == nil {
			t.Fatal("expected error for malformed ForgejoURL")
		}
		assertConfigErr(t, err)
	})
	t.Run("empty_admin_token", func(t *testing.T) {
		c := baseValid()
		c.AdminToken = ""
		err := c.Validate()
		if err == nil {
			t.Fatal("expected error for empty AdminToken")
		}
		assertConfigErr(t, err)
	})
	t.Run("empty_known_friends_path", func(t *testing.T) {
		c := baseValid()
		c.KnownFriendsPath = ""
		err := c.Validate()
		if err == nil {
			t.Fatal("expected error for empty KnownFriendsPath")
		}
		assertConfigErr(t, err)
	})
	t.Run("zero_http_timeout", func(t *testing.T) {
		c := baseValid()
		c.HTTPTimeout = 0
		err := c.Validate()
		if err == nil {
			t.Fatal("expected error for zero HTTPTimeout")
		}
		assertConfigErr(t, err)
	})
	t.Run("negative_http_timeout", func(t *testing.T) {
		c := baseValid()
		c.HTTPTimeout = -1 * time.Second
		err := c.Validate()
		if err == nil {
			t.Fatal("expected error for negative HTTPTimeout")
		}
		assertConfigErr(t, err)
	})
	t.Run("zero_request_rate", func(t *testing.T) {
		c := baseValid()
		c.RequestRate = 0
		err := c.Validate()
		if err == nil {
			t.Fatal("expected error for zero RequestRate")
		}
		assertConfigErr(t, err)
	})
	t.Run("valid_config", func(t *testing.T) {
		c := baseValid()
		if err := c.Validate(); err != nil {
			t.Errorf("Validate() unexpected error: %v", err)
		}
	})
}

// assertConfigErr verifies err is a *TypedError with Kind=ErrKindConfig.
func assertConfigErr(t *testing.T, err error) {
	t.Helper()
	var te *TypedError
	if !errors.As(err, &te) {
		t.Fatalf("error is not *TypedError: %T (%v)", err, err)
	}
	if te.Kind != ErrKindConfig {
		t.Errorf("kind = %q, want %q", te.Kind, ErrKindConfig)
	}
}

// -----------------------------------------------------------------------------
// 24. DefaultProvisionerConfig
// -----------------------------------------------------------------------------

func TestDefaultProvisionerConfig(t *testing.T) {
	c := DefaultProvisionerConfig()
	if c.HTTPTimeout != 30*time.Second {
		t.Errorf("HTTPTimeout = %s, want 30s", c.HTTPTimeout)
	}
	if c.RequestRate != 10 {
		t.Errorf("RequestRate = %d, want 10", c.RequestRate)
	}
	if c.BurstRate != 2 {
		t.Errorf("BurstRate = %d, want 2", c.BurstRate)
	}
	if c.KnownFriendsPath != DefaultKnownFriendsPath {
		t.Errorf("KnownFriendsPath = %q, want %q", c.KnownFriendsPath, DefaultKnownFriendsPath)
	}
	if c.SSHKeyDir != DefaultSSHKeyDir {
		t.Errorf("SSHKeyDir = %q, want %q", c.SSHKeyDir, DefaultSSHKeyDir)
	}
	if c.StatePath != DefaultStatePath {
		t.Errorf("StatePath = %q, want %q", c.StatePath, DefaultStatePath)
	}
	if c.DryRun {
		t.Error("DryRun = true, want false")
	}
	if c.Verbose {
		t.Error("Verbose = true, want false")
	}
}

// -----------------------------------------------------------------------------
// 25. NewRateLimiter
// -----------------------------------------------------------------------------

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(5, 3)
	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}
	if rl.Rate() != 5 {
		t.Errorf("Rate() = %d, want 5", rl.Rate())
	}
	if rl.Burst() != 3 {
		t.Errorf("Burst() = %d, want 3", rl.Burst())
	}
	// Channel should have `burst` tokens initially. v1 stub: Acquire
	// never blocks, but each Acquire on a full channel drains a token
	// from the buffered channel. With burst=3, 3 Acquires should succeed
	// (depleting the bucket); the 4th would not block (v1 stub), but the
	// channel can still be drained via non-blocking receive to verify
	// state. We instead just confirm initial capacity is burst.
	if got := len(rl.tokens); got != 3 {
		t.Errorf("initial token count = %d, want 3", got)
	}
}

// -----------------------------------------------------------------------------
// 26. RateLimiter.Acquire
// -----------------------------------------------------------------------------

func TestRateLimiter_Acquire(t *testing.T) {
	t.Run("drains_one_token", func(t *testing.T) {
		rl := NewRateLimiter(1, 2)
		if len(rl.tokens) != 2 {
			t.Fatalf("initial tokens = %d, want 2", len(rl.tokens))
		}
		rl.Acquire()
		if len(rl.tokens) != 1 {
			t.Errorf("after Acquire tokens = %d, want 1", len(rl.tokens))
		}
		rl.Acquire()
		if len(rl.tokens) != 0 {
			t.Errorf("after second Acquire tokens = %d, want 0", len(rl.tokens))
		}
	})
	t.Run("nil_limiter_safe", func(t *testing.T) {
		var rl *RateLimiter
		// Must not panic.
		rl.Acquire()
	})
}

// -----------------------------------------------------------------------------
// 27. RetryPolicy.BackoffFor
// -----------------------------------------------------------------------------

func TestRetryPolicy_BackoffFor(t *testing.T) {
	p := DefaultRetryPolicy()
	cases := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{"attempt_0", 0, 0},
		{"attempt_1", 1, 1 * time.Second},
		{"attempt_2", 2, 2 * time.Second},
		{"attempt_3", 3, 4 * time.Second},
		{"attempt_4_exhausted", 4, 0},
		{"attempt_99_exhausted", 99, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.BackoffFor(tc.attempt); got != tc.want {
				t.Errorf("BackoffFor(%d) = %s, want %s", tc.attempt, got, tc.want)
			}
		})
	}
	t.Run("backoff_caps_at_max", func(t *testing.T) {
		// Custom policy where MaxBackoff kicks in quickly.
		p := RetryPolicy{
			MaxAttempts:    10,
			InitialBackoff: 1 * time.Second,
			MaxBackoff:     5 * time.Second,
			Multiplier:     2.0,
		}
		// attempt=5: 1s -> 2s -> 4s -> 8s capped to 5s -> 10s capped to 5s
		if got := p.BackoffFor(5); got != 5*time.Second {
			t.Errorf("BackoffFor(5) with cap = %s, want 5s", got)
		}
	})
}

// -----------------------------------------------------------------------------
// 28. DefaultRetryPolicy
// -----------------------------------------------------------------------------

func TestDefaultRetryPolicy(t *testing.T) {
	p := DefaultRetryPolicy()
	if p.MaxAttempts != 4 {
		t.Errorf("MaxAttempts = %d, want 4", p.MaxAttempts)
	}
	if p.InitialBackoff != 1*time.Second {
		t.Errorf("InitialBackoff = %s, want 1s", p.InitialBackoff)
	}
	if p.MaxBackoff != 30*time.Second {
		t.Errorf("MaxBackoff = %s, want 30s", p.MaxBackoff)
	}
	if p.Multiplier != 2.0 {
		t.Errorf("Multiplier = %v, want 2.0", p.Multiplier)
	}
}

// -----------------------------------------------------------------------------
// 29. Provisioner URL methods
// -----------------------------------------------------------------------------

func TestProvisioner_URLs(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = "https://forgejo.example.com"
	cfg.AdminToken = "tok"
	cfg.KnownFriendsPath = "/tmp/known-friends.json"
	p, err := NewProvisioner(cfg)
	if err != nil {
		t.Fatalf("NewProvisioner: %v", err)
	}
	t.Run("adminUsersURL_no_name", func(t *testing.T) {
		want := "https://forgejo.example.com/api/v1/admin/users"
		if got := p.adminUsersURL(""); got != want {
			t.Errorf("adminUsersURL(\"\") = %q, want %q", got, want)
		}
	})
	t.Run("adminUsersURL_with_name", func(t *testing.T) {
		want := "https://forgejo.example.com/api/v1/admin/users/wojons"
		if got := p.adminUsersURL("wojons"); got != want {
			t.Errorf("adminUsersURL(wojons) = %q, want %q", got, want)
		}
	})
	t.Run("userKeysURL", func(t *testing.T) {
		want := "https://forgejo.example.com/api/v1/user/keys"
		if got := p.userKeysURL(); got != want {
			t.Errorf("userKeysURL() = %q, want %q", got, want)
		}
	})
	t.Run("userTokensURL", func(t *testing.T) {
		want := "https://forgejo.example.com/api/v1/users/wojons/tokens"
		if got := p.userTokensURL("wojons"); got != want {
			t.Errorf("userTokensURL(wojons) = %q, want %q", got, want)
		}
	})
	t.Run("userTokenURL", func(t *testing.T) {
		want := "https://forgejo.example.com/api/v1/users/wojons/tokens/42"
		if got := p.userTokenURL("wojons", 42); got != want {
			t.Errorf("userTokenURL(wojons, 42) = %q, want %q", got, want)
		}
	})
	t.Run("trims_trailing_slash", func(t *testing.T) {
		cfg2 := cfg
		cfg2.ForgejoURL = "https://forgejo.example.com/"
		p2, err := NewProvisioner(cfg2)
		if err != nil {
			t.Fatalf("NewProvisioner: %v", err)
		}
		want := "https://forgejo.example.com/api/v1/admin/users"
		if got := p2.adminUsersURL(""); got != want {
			t.Errorf("trailing slash not trimmed: got %q, want %q", got, want)
		}
	})
}

// -----------------------------------------------------------------------------
// 30. NewProvisioner
// -----------------------------------------------------------------------------

func TestNewProvisioner(t *testing.T) {
	t.Run("valid_config", func(t *testing.T) {
		cfg := DefaultProvisionerConfig()
		cfg.ForgejoURL = "https://forgejo.example.com"
		cfg.AdminToken = "tok"
		cfg.KnownFriendsPath = "/tmp/kf.json"
		p, err := NewProvisioner(cfg)
		if err != nil {
			t.Fatalf("NewProvisioner unexpected error: %v", err)
		}
		if p == nil {
			t.Fatal("NewProvisioner returned nil provisioner")
		}
	})
	t.Run("invalid_config_returns_error", func(t *testing.T) {
		cfg := DefaultProvisionerConfig()
		// Leave ForgejoURL empty to trigger Validate failure.
		p, err := NewProvisioner(cfg)
		if err == nil {
			t.Fatal("expected error for invalid config")
		}
		if p != nil {
			t.Errorf("expected nil provisioner on error, got %v", p)
		}
		var te *TypedError
		if !errors.As(err, &te) {
			t.Errorf("error is not *TypedError: %T", err)
		}
	})
}

// -----------------------------------------------------------------------------
// 31. Provisioner.DryRun
// -----------------------------------------------------------------------------

func TestProvisioner_DryRun(t *testing.T) {
	t.Run("dry_run_true", func(t *testing.T) {
		cfg := DefaultProvisionerConfig()
		cfg.ForgejoURL = "https://forgejo.example.com"
		cfg.AdminToken = "tok"
		cfg.KnownFriendsPath = "/tmp/kf.json"
		cfg.DryRun = true
		p, err := NewProvisioner(cfg)
		if err != nil {
			t.Fatalf("NewProvisioner: %v", err)
		}
		if !p.DryRun() {
			t.Error("DryRun() = false, want true")
		}
	})
	t.Run("dry_run_false", func(t *testing.T) {
		cfg := DefaultProvisionerConfig()
		cfg.ForgejoURL = "https://forgejo.example.com"
		cfg.AdminToken = "tok"
		cfg.KnownFriendsPath = "/tmp/kf.json"
		p, _ := NewProvisioner(cfg)
		if p.DryRun() {
			t.Error("DryRun() = true, want false")
		}
	})
}

// -----------------------------------------------------------------------------
// 32. Provisioner.BaseURL
// -----------------------------------------------------------------------------

func TestProvisioner_BaseURL(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = "https://user:pass@forgejo.example.com"
	cfg.AdminToken = "tok"
	cfg.KnownFriendsPath = "/tmp/kf.json"
	p, err := NewProvisioner(cfg)
	if err != nil {
		t.Fatalf("NewProvisioner: %v", err)
	}
	got := p.BaseURL()
	if strings.Contains(got, "user:pass") {
		t.Errorf("BaseURL contains userinfo: %q", got)
	}
	if !strings.Contains(got, "forgejo.example.com") {
		t.Errorf("BaseURL missing host: %q", got)
	}
}

// -----------------------------------------------------------------------------
// 33. redactURL
// -----------------------------------------------------------------------------

func TestRedactURL(t *testing.T) {
	t.Run("strips_userinfo", func(t *testing.T) {
		in := "https://user:pass@forgejo.example.com/path"
		got := redactURL(in)
		if strings.Contains(got, "user:pass") {
			t.Errorf("userinfo not stripped: %q", got)
		}
		if !strings.Contains(got, "forgejo.example.com") {
			t.Errorf("host missing: %q", got)
		}
	})
	t.Run("normal_url_unchanged", func(t *testing.T) {
		in := "https://forgejo.example.com"
		got := redactURL(in)
		if got != in {
			t.Errorf("redactURL(%q) = %q, want %q", in, got, in)
		}
	})
	t.Run("url_without_path_unchanged", func(t *testing.T) {
		in := "https://forgejo.example.com/api/v1/users"
		got := redactURL(in)
		if got != in {
			t.Errorf("redactURL(%q) = %q, want %q", in, got, in)
		}
	})
	t.Run("garbage_returned_as_is", func(t *testing.T) {
		// "://invalid" is a URL that url.Parse rejects.
		in := "://invalid"
		got := redactURL(in)
		if got != in {
			t.Errorf("redactURL(%q) = %q, want original (%q)", in, got, in)
		}
	})
}

// -----------------------------------------------------------------------------
// 34. Provisioner.Close
// -----------------------------------------------------------------------------

func TestProvisioner_Close(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = "https://forgejo.example.com"
	cfg.AdminToken = "tok"
	cfg.KnownFriendsPath = "/tmp/kf.json"
	p, err := NewProvisioner(cfg)
	if err != nil {
		t.Fatalf("NewProvisioner: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
	// Close is idempotent — calling twice should also be fine.
	if err := p.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil", err)
	}
}

// -----------------------------------------------------------------------------
// 35. Provisioner.Config
// -----------------------------------------------------------------------------

func TestProvisioner_Config(t *testing.T) {
	cfg := DefaultProvisionerConfig()
	cfg.ForgejoURL = "https://forgejo.example.com"
	cfg.AdminToken = "tok-secret"
	cfg.KnownFriendsPath = "/tmp/kf.json"
	cfg.SSHKeyDir = "/tmp/keys"
	cfg.StatePath = "/tmp/state.json"
	cfg.DryRun = true
	cfg.HTTPTimeout = 45 * time.Second
	cfg.RequestRate = 25
	cfg.BurstRate = 7
	p, err := NewProvisioner(cfg)
	if err != nil {
		t.Fatalf("NewProvisioner: %v", err)
	}
	got := p.Config()
	if got.ForgejoURL != cfg.ForgejoURL {
		t.Errorf("ForgejoURL = %q, want %q", got.ForgejoURL, cfg.ForgejoURL)
	}
	if got.AdminToken != cfg.AdminToken {
		t.Errorf("AdminToken = %q, want %q", got.AdminToken, cfg.AdminToken)
	}
	if got.KnownFriendsPath != cfg.KnownFriendsPath {
		t.Errorf("KnownFriendsPath = %q, want %q", got.KnownFriendsPath, cfg.KnownFriendsPath)
	}
	if got.SSHKeyDir != cfg.SSHKeyDir {
		t.Errorf("SSHKeyDir = %q, want %q", got.SSHKeyDir, cfg.SSHKeyDir)
	}
	if got.StatePath != cfg.StatePath {
		t.Errorf("StatePath = %q, want %q", got.StatePath, cfg.StatePath)
	}
	if got.DryRun != cfg.DryRun {
		t.Errorf("DryRun = %v, want %v", got.DryRun, cfg.DryRun)
	}
	if got.HTTPTimeout != cfg.HTTPTimeout {
		t.Errorf("HTTPTimeout = %s, want %s", got.HTTPTimeout, cfg.HTTPTimeout)
	}
	if got.RequestRate != cfg.RequestRate {
		t.Errorf("RequestRate = %d, want %d", got.RequestRate, cfg.RequestRate)
	}
	if got.BurstRate != cfg.BurstRate {
		t.Errorf("BurstRate = %d, want %d", got.BurstRate, cfg.BurstRate)
	}
}

// -----------------------------------------------------------------------------
// 36. Provisioner stub methods — non-dry-run vs dry-run
// -----------------------------------------------------------------------------

func TestProvisioner_Stubs(t *testing.T) {
	// Helper to build a provisioner with the desired DryRun flag.
	newProv := func(t *testing.T, dryRun bool) *Provisioner {
		t.Helper()
		cfg := DefaultProvisionerConfig()
		cfg.ForgejoURL = "https://forgejo.example.com"
		cfg.AdminToken = "tok"
		cfg.KnownFriendsPath = "/tmp/kf.json"
		cfg.DryRun = dryRun
		p, err := NewProvisioner(cfg)
		if err != nil {
			t.Fatalf("NewProvisioner: %v", err)
		}
		return p
	}

	t.Run("CreateUser_nil_req", func(t *testing.T) {
		// Validation happens BEFORE the DryRun branch, so nil req
		// produces a config error in either mode.
		pDry := newProv(t, true)
		_, err := pDry.CreateUser(nil)
		assertConfigErr(t, err)

		pReal := newProv(t, false)
		_, err = pReal.CreateUser(nil)
		assertConfigErr(t, err)
	})

	t.Run("CreateUser_dry_run_returns_synthetic_account", func(t *testing.T) {
		p := newProv(t, true)
		req := &CreateUserRequest{
			Username: "wojons",
			Email:    "wojons@helix-agents.local",
			FullName: "Wojons",
		}
		acc, err := p.CreateUser(req)
		if err != nil {
			t.Fatalf("CreateUser unexpected error: %v", err)
		}
		if acc == nil {
			t.Fatal("CreateUser returned nil account in dry-run")
		}
		if acc.Login != "wojons" {
			t.Errorf("Login = %q, want wojons", acc.Login)
		}
		if acc.Email != "wojons@helix-agents.local" {
			t.Errorf("Email = %q, want wojons@helix-agents.local", acc.Email)
		}
		if acc.FullName != "Wojons" {
			t.Errorf("FullName = %q, want Wojons", acc.FullName)
		}
	})

	t.Run("CreateUser_real_returns_network_error", func(t *testing.T) {
		p := newProv(t, false)
		req := &CreateUserRequest{Username: "wojons"}
		acc, err := p.CreateUser(req)
		if err == nil {
			t.Error("expected network error from real transport, got nil")
		}
		if acc != nil {
			t.Errorf("acc = %v, want nil", acc)
		}
	})

	t.Run("RegisterKey_empty_name", func(t *testing.T) {
		p := newProv(t, true) // even in dry-run, empty name is an error
		_, err := p.RegisterKey("", "pass", "key", "title")
		assertConfigErr(t, err)

		pReal := newProv(t, false)
		_, err = pReal.RegisterKey("", "pass", "key", "title")
		assertConfigErr(t, err)
	})

	t.Run("RegisterKey_dry_run_returns_synthetic_key", func(t *testing.T) {
		p := newProv(t, true)
		key, err := p.RegisterKey("wojons", "pass", "ssh-ed25519 AAAA", "Helix Agent — wojons (pro)")
		if err != nil {
			t.Fatalf("RegisterKey unexpected error: %v", err)
		}
		if key == nil {
			t.Fatal("RegisterKey returned nil SSHKey in dry-run")
		}
		if key.Key != "ssh-ed25519 AAAA" {
			t.Errorf("Key = %q, want %q", key.Key, "ssh-ed25519 AAAA")
		}
		if key.Title != "Helix Agent — wojons (pro)" {
			t.Errorf("Title = %q, want %q", key.Title, "Helix Agent — wojons (pro)")
		}
	})

	t.Run("RegisterKey_real_returns_network_error", func(t *testing.T) {
		p := newProv(t, false)
		key, err := p.RegisterKey("wojons", "pass", "key", "title")
		if err == nil {
			t.Error("expected network error from real transport, got nil")
		}
		if key != nil {
			t.Errorf("key = %v, want nil", key)
		}
	})

	t.Run("CreateToken_empty_name", func(t *testing.T) {
		p := newProv(t, true)
		_, err := p.CreateToken("", "admin", "adminpass", &CreateTokenRequest{Name: PATName})
		assertConfigErr(t, err)

		pReal := newProv(t, false)
		_, err = pReal.CreateToken("", "admin", "adminpass", &CreateTokenRequest{Name: PATName})
		assertConfigErr(t, err)
	})

	t.Run("CreateToken_nil_req", func(t *testing.T) {
		p := newProv(t, true)
		_, err := p.CreateToken("wojons", "admin", "adminpass", nil)
		assertConfigErr(t, err)

		pReal := newProv(t, false)
		_, err = pReal.CreateToken("wojons", "admin", "adminpass", nil)
		assertConfigErr(t, err)
	})

	t.Run("CreateToken_dry_run_returns_synthetic_token", func(t *testing.T) {
		p := newProv(t, true)
		req := &CreateTokenRequest{
			Name:   PATName,
			Scopes: []string{"read:user", "write:user"},
		}
		tok, err := p.CreateToken("wojons", "admin", "adminpass", req)
		if err != nil {
			t.Fatalf("CreateToken unexpected error: %v", err)
		}
		if tok == nil {
			t.Fatal("CreateToken returned nil token in dry-run")
		}
		if tok.Name != PATName {
			t.Errorf("Name = %q, want %q", tok.Name, PATName)
		}
		if len(tok.Scopes) != 2 {
			t.Errorf("Scopes len = %d, want 2", len(tok.Scopes))
		}
	})

	t.Run("CreateToken_real_returns_network_error", func(t *testing.T) {
		p := newProv(t, false)
		req := &CreateTokenRequest{Name: PATName}
		tok, err := p.CreateToken("wojons", "admin", "adminpass", req)
		if err == nil {
			t.Error("expected network error from real transport, got nil")
		}
		if tok != nil {
			t.Errorf("tok = %v, want nil", tok)
		}
	})

	t.Run("RevokeToken_empty_name", func(t *testing.T) {
		p := newProv(t, true)
		err := p.RevokeToken("", "admin", "adminpass", 1)
		assertConfigErr(t, err)

		pReal := newProv(t, false)
		err = pReal.RevokeToken("", "admin", "adminpass", 1)
		assertConfigErr(t, err)
	})

	t.Run("RevokeToken_zero_token_id", func(t *testing.T) {
		p := newProv(t, true)
		err := p.RevokeToken("wojons", "admin", "adminpass", 0)
		assertConfigErr(t, err)

		pReal := newProv(t, false)
		err = pReal.RevokeToken("wojons", "admin", "adminpass", 0)
		assertConfigErr(t, err)
	})

	t.Run("RevokeToken_negative_token_id", func(t *testing.T) {
		p := newProv(t, true)
		err := p.RevokeToken("wojons", "admin", "adminpass", -1)
		assertConfigErr(t, err)
	})

	t.Run("RevokeToken_dry_run_returns_nil", func(t *testing.T) {
		p := newProv(t, true)
		if err := p.RevokeToken("wojons", "admin", "adminpass", 42); err != nil {
			t.Errorf("RevokeToken dry-run err = %v, want nil", err)
		}
	})

	t.Run("RevokeToken_real_returns_network_error", func(t *testing.T) {
		p := newProv(t, false)
		err := p.RevokeToken("wojons", "admin", "adminpass", 42)
		if err == nil {
			t.Error("expected network error from real transport, got nil")
		}
	})

	t.Run("GetAccount_dry_run_returns_nil_nil", func(t *testing.T) {
		p := newProv(t, true)
		acc, err := p.GetAccount("wojons")
		if err != nil {
			t.Errorf("GetAccount dry-run err = %v, want nil", err)
		}
		if acc != nil {
			t.Errorf("GetAccount dry-run acc = %v, want nil", acc)
		}
	})

	t.Run("GetAccount_real_returns_network_error", func(t *testing.T) {
		p := newProv(t, false)
		acc, err := p.GetAccount("wojons")
		if err == nil {
			t.Error("expected network error from real transport, got nil")
		}
		if acc != nil {
			t.Errorf("acc = %v, want nil", acc)
		}
	})
}
