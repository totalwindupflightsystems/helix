package integration

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/forgejo"
)

// ---------------------------------------------------------------------------
// E2E test helpers
// ---------------------------------------------------------------------------

func e2eSkip(t *testing.T) bool {
	t.Helper()
	if os.Getenv("HELIX_E2E") == "" {
		t.Skip("Set HELIX_E2E=1 to run E2E tests")
		return true
	}
	return false
}

func forgejoReachable(baseURL, adminUser, adminPass string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/version", nil)
	if err != nil {
		return fmt.Errorf("cannot build request: %w", err)
	}
	req.SetBasicAuth(adminUser, adminPass)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Forgejo unreachable at %s: %w", baseURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 500 {
		return fmt.Errorf("Forgejo returned HTTP %d at %s", resp.StatusCode, baseURL)
	}
	return nil
}

func e2eForgejoURL() string {
	return getEnv("FORGEJO_URL", DefaultForgejoURL)
}

func e2eAdminUser() string {
	return getEnv("FORGEJO_ADMIN_USER", DefaultAdminUser)
}

func e2eAdminPass() string {
	return getEnv("FORGEJO_ADMIN_PASSWORD", DefaultAdminPassword)
}

func e2eOwner() string {
	return getEnv("HELIX_E2E_OWNER", "helio")
}

func e2eRepo() string {
	return getEnv("HELIX_E2E_REPO", "helix")
}

func e2eSpecPath(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"testdata/task-spec.md",
		"pkg/integration/testdata/task-spec.md",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	t.Fatalf("cannot find testdata/task-spec.md; searched: %v", candidates)
	return ""
}

func e2eForgejoClient(t *testing.T) *forgejo.Client {
	t.Helper()
	return forgejo.NewClient(e2eForgejoURL(), e2eAdminUser(), e2eAdminPass())
}

func e2eIntegrationClient(t *testing.T) *ForgejoClient {
	t.Helper()
	c, err := NewForgejoClient(e2eForgejoURL(), e2eAdminUser(), e2eAdminPass())
	require.NoError(t, err, "creating ForgejoClient")
	return c
}

func uniqueAgentName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("helix-e2e-%s-%d", sanitizeForName(t.Name()), time.Now().UnixNano()%100000)
}

func sanitizeForName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	result := strings.ToLower(b.String())
	result = strings.Trim(result, "-")
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	if len(result) > 50 {
		result = result[:50]
	}
	if result == "" {
		result = "agent"
	}
	return result
}
