package identity

import "testing"

func TestProvisioner_urlHelpers(t *testing.T) {
	p := &Provisioner{
		cfg: ProvisionerConfig{ForgejoURL: "http://localhost:3030"},
	}

	t.Run("adminUserKeysURL", func(t *testing.T) {
		got := p.adminUserKeysURL("test-agent")
		want := "http://localhost:3030/api/v1/admin/users/test-agent/keys"
		if got != want {
			t.Errorf("adminUserKeysURL = %q, want %q", got, want)
		}
	})

	t.Run("adminUserKeysURL with special characters", func(t *testing.T) {
		got := p.adminUserKeysURL("agent with spaces")
		want := "http://localhost:3030/api/v1/admin/users/agent%20with%20spaces/keys"
		if got != want {
			t.Errorf("adminUserKeysURL = %q, want %q", got, want)
		}
	})

	t.Run("userKeysURL", func(t *testing.T) {
		got := p.userKeysURL()
		want := "http://localhost:3030/api/v1/user/keys"
		if got != want {
			t.Errorf("userKeysURL = %q, want %q", got, want)
		}
	})

	t.Run("userTokensURL", func(t *testing.T) {
		got := p.userTokensURL("test-agent")
		want := "http://localhost:3030/api/v1/users/test-agent/tokens"
		if got != want {
			t.Errorf("userTokensURL = %q, want %q", got, want)
		}
	})

	t.Run("userTokenURL", func(t *testing.T) {
		got := p.userTokenURL("test-agent", 42)
		want := "http://localhost:3030/api/v1/users/test-agent/tokens/42"
		if got != want {
			t.Errorf("userTokenURL = %q, want %q", got, want)
		}
	})

	t.Run("trailing slash in ForgejoURL", func(t *testing.T) {
		p2 := &Provisioner{
			cfg: ProvisionerConfig{ForgejoURL: "http://localhost:3030/"},
		}
		got := p2.adminUserKeysURL("agent1")
		want := "http://localhost:3030/api/v1/admin/users/agent1/keys"
		if got != want {
			t.Errorf("adminUserKeysURL with trailing slash = %q, want %q", got, want)
		}
	})
}
