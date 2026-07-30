package source

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// writeTempSourcesYAML creates a temporary .helix/sources.yaml file with the
// given content for use in tests. The caller is responsible for cleanup.
func writeTempSourcesYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, ".helix", "sources.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

// ---------------------------------------------------------------------------
// ParseSourcesYAML — success cases
// ---------------------------------------------------------------------------

func TestParseSourcesYAML_NonExistentFile(t *testing.T) {
	t.Parallel()
	file, err := ParseSourcesYAML("/nonexistent/path/sources.yaml")
	require.NoError(t, err)
	require.NotNil(t, file)
	assert.NotNil(t, file.Sources)
	assert.Len(t, file.Sources, 0)
}

func TestParseSourcesYAML_EmptyFile(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, "")
	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)
	require.NotNil(t, file)
	assert.Len(t, file.Sources, 0)
}

func TestParseSourcesYAML_NoSources(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `# just a comment
sources: {}
`)
	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)
	require.NotNil(t, file)
	assert.Len(t, file.Sources, 0)
}

func TestParseSourcesYAML_SingleSourcePostgres(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  database:
    type: postgres
    connection: postgres://localhost:5432/mydb
    openapi: ./schemas/database.openapi.yaml
`)
	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)

	require.Contains(t, file.Sources, "database")
	db := file.Sources["database"]
	assert.Equal(t, "database", db.Name)
	assert.Equal(t, SourceTypePostgres, db.Type)
	assert.Equal(t, "postgres://localhost:5432/mydb", db.Connection)
	assert.Equal(t, "./schemas/database.openapi.yaml", db.OpenAPI)
	assert.Empty(t, db.AllowedAgents)
	assert.Empty(t, db.RateLimit)
}

func TestParseSourcesYAML_SingleSourceREST(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  crm:
    type: rest
    base_url: https://crm.internal/api
    openapi: ./schemas/crm.openapi.yaml
    token_env: CRM_API_TOKEN
`)
	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)

	require.Contains(t, file.Sources, "crm")
	crm := file.Sources["crm"]
	assert.Equal(t, "crm", crm.Name)
	assert.Equal(t, SourceTypeREST, crm.Type)
	assert.Equal(t, "https://crm.internal/api", crm.BaseURL)
	assert.Equal(t, "./schemas/crm.openapi.yaml", crm.OpenAPI)
	assert.Equal(t, "CRM_API_TOKEN", crm.TokenEnv)
}

func TestParseSourcesYAML_SingleSourceLocal(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  filesystem:
    type: local
    root: /data/shared
    read_only: true
`)
	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)

	require.Contains(t, file.Sources, "filesystem")
	fs := file.Sources["filesystem"]
	assert.Equal(t, "filesystem", fs.Name)
	assert.Equal(t, SourceTypeLocal, fs.Type)
	assert.Equal(t, "/data/shared", fs.Root)
	assert.True(t, fs.ReadOnly)
	assert.Empty(t, fs.AllowedAgents)
}

func TestParseSourcesYAML_MultipleSources(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  database:
    type: postgres
    connection: ${DB_URL}
    openapi: ./schemas/db.openapi.yaml
    allowed_agents:
      - agent-a
      - agent-b
    rate_limit: 10/s
  crm:
    type: rest
    base_url: https://crm.internal/api
    openapi: ./schemas/crm.openapi.yaml
    token_env: CRM_TOKEN
  filesystem:
    type: local
    root: /data/shared
    read_only: true
    allowed_agents:
      - read-only-agent
`)
	// Set env var for expansion.
	os.Setenv("DB_URL", "postgres://user:pass@host:5432/db")
	defer os.Unsetenv("DB_URL")

	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)
	assert.Len(t, file.Sources, 3)

	// Database
	db := file.Sources["database"]
	assert.Equal(t, "database", db.Name)
	assert.Equal(t, SourceTypePostgres, db.Type)
	assert.Equal(t, "postgres://user:pass@host:5432/db", db.Connection)
	assert.Equal(t, "./schemas/db.openapi.yaml", db.OpenAPI)
	assert.Equal(t, []string{"agent-a", "agent-b"}, db.AllowedAgents)
	assert.Equal(t, "10/s", db.RateLimit)

	// CRM
	crm := file.Sources["crm"]
	assert.Equal(t, "crm", crm.Name)
	assert.Equal(t, SourceTypeREST, crm.Type)
	assert.Equal(t, "https://crm.internal/api", crm.BaseURL)
	assert.Equal(t, "CRM_TOKEN", crm.TokenEnv)
	assert.Empty(t, crm.AllowedAgents)

	// Filesystem
	fs := file.Sources["filesystem"]
	assert.Equal(t, "filesystem", fs.Name)
	assert.Equal(t, SourceTypeLocal, fs.Type)
	assert.Equal(t, "/data/shared", fs.Root)
	assert.True(t, fs.ReadOnly)
	assert.Equal(t, []string{"read-only-agent"}, fs.AllowedAgents)
}

func TestParseSourcesYAML_AllOptionalFieldsPostgres(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  db:
    type: postgres
    connection: postgres://localhost/mydb
    openapi: ./schemas/api.yaml
    allowed_agents:
      - tester
    rate_limit: 5/s
`)
	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)

	db := file.Sources["db"]
	assert.Equal(t, []string{"tester"}, db.AllowedAgents)
	assert.Equal(t, "5/s", db.RateLimit)
}

func TestParseSourcesYAML_AllOptionalFieldsREST(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  api:
    type: rest
    base_url: https://api.example.com
    openapi: ./schemas/api.yaml
    token_env: API_KEY
    allowed_agents:
      - ui-agent
`)
	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)

	api := file.Sources["api"]
	assert.Equal(t, "API_KEY", api.TokenEnv)
	assert.Equal(t, []string{"ui-agent"}, api.AllowedAgents)
}

func TestParseSourcesYAML_AllOptionalFieldsLocal(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  fs:
    type: local
    root: /mnt/data
    read_only: false
    allowed_agents:
      - writer
`)
	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)

	fs := file.Sources["fs"]
	assert.False(t, fs.ReadOnly)
	assert.Equal(t, []string{"writer"}, fs.AllowedAgents)
}

func TestParseSourcesYAML_LocalDefaultReadOnlyFalse(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  fs:
    type: local
    root: /tmp
`)
	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)

	fs := file.Sources["fs"]
	assert.False(t, fs.ReadOnly, "read_only should default to false when omitted")
}

func TestParseSourcesYAML_NameDerivedFromKey(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  custom-name:
    type: local
    root: /tmp
`)
	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)

	src := file.Sources["custom-name"]
	assert.Equal(t, "custom-name", src.Name)
}

// ---------------------------------------------------------------------------
// ParseSourcesYAML — error cases
// ---------------------------------------------------------------------------

func TestParseSourcesYAML_InvalidYAML(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `this is not valid: yaml: [[[`)
	_, err := ParseSourcesYAML(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid YAML")
}

func TestParseSourcesYAML_PostgresMissingConnection(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  db:
    type: postgres
    openapi: ./schemas/db.yaml
`)
	_, err := ParseSourcesYAML(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection is required")
}

func TestParseSourcesYAML_PostgresMissingOpenAPI(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  db:
    type: postgres
    connection: postgres://localhost/db
`)
	_, err := ParseSourcesYAML(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "openapi is required")
}

func TestParseSourcesYAML_RESTMissingBaseURL(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  api:
    type: rest
    openapi: ./schemas/api.yaml
`)
	_, err := ParseSourcesYAML(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base_url is required")
}

func TestParseSourcesYAML_RESTMissingOpenAPI(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  api:
    type: rest
    base_url: https://api.example.com
`)
	_, err := ParseSourcesYAML(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "openapi is required")
}

func TestParseSourcesYAML_LocalMissingRoot(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  fs:
    type: local
`)
	_, err := ParseSourcesYAML(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "root is required")
}

func TestParseSourcesYAML_UnknownSourceType(t *testing.T) {
	t.Parallel()
	p := writeTempSourcesYAML(t, `
sources:
  db:
    type: mongodb
    connection: mongodb://localhost
`)
	_, err := ParseSourcesYAML(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown type")
	assert.Contains(t, err.Error(), "mongodb")
}

func TestParseSourcesYAML_EmptyName(t *testing.T) {
	t.Parallel()
	// An empty key "" is technically valid YAML but should be caught by validation.
	p := writeTempSourcesYAML(t, `
sources:
  "":
    type: local
    root: /tmp
`)
	_, err := ParseSourcesYAML(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source name is required")
}

func TestParseSourcesYAML_FirstErrorStopsParsing(t *testing.T) {
	t.Parallel()
	// When multiple sources have errors, only the first is returned.
	p := writeTempSourcesYAML(t, `
sources:
  good-db:
    type: postgres
    connection: postgres://localhost/db
    openapi: ./db.yaml
  bad-api:
    type: rest
    openapi: ./api.yaml
  bad-fs:
    type: local
`)
	_, err := ParseSourcesYAML(p)
	require.Error(t, err)
	// The second source (bad-api) has missing base_url — that should be the first error.
	assert.Contains(t, err.Error(), "base_url is required")
	assert.Contains(t, err.Error(), "bad-api")
}

// ---------------------------------------------------------------------------
// env-var expansion
// ---------------------------------------------------------------------------

func TestExpandEnvVars_ReplacesKnownVar(t *testing.T) {
	t.Parallel()
	os.Setenv("HELIX_TEST_FOO", "bar")
	defer os.Unsetenv("HELIX_TEST_FOO")

	result := expandEnvVars("hello ${HELIX_TEST_FOO} world")
	assert.Equal(t, "hello bar world", result)
}

func TestExpandEnvVars_UnknownVarBecomesEmpty(t *testing.T) {
	t.Parallel()
	result := expandEnvVars("hello ${NONEXISTENT_VAR_XYZ} world")
	assert.Equal(t, "hello  world", result)
}

func TestExpandEnvVars_MultipleVars(t *testing.T) {
	t.Parallel()
	os.Setenv("HELIX_A", "alpha")
	os.Setenv("HELIX_B", "beta")
	defer os.Unsetenv("HELIX_A")
	defer os.Unsetenv("HELIX_B")

	result := expandEnvVars("${HELIX_A} and ${HELIX_B}")
	assert.Equal(t, "alpha and beta", result)
}

func TestExpandEnvVars_NoVars(t *testing.T) {
	t.Parallel()
	result := expandEnvVars("plain string with no variables")
	assert.Equal(t, "plain string with no variables", result)
}

func TestExpandEnvVars_EscapedDollar(t *testing.T) {
	t.Parallel()
	// $$ is not env-var syntax; it's literal.
	result := expandEnvVars("costs $$50")
	assert.Equal(t, "costs $$50", result)
}

func TestExpandEnvVars_IncompleteSyntax(t *testing.T) {
	t.Parallel()
	// ${ without closing brace is left as-is.
	result := expandEnvVars("oops ${MISSING_BRACE")
	assert.Equal(t, "oops ${MISSING_BRACE", result)
}

func TestParseSourcesYAML_EnvVarInConnection(t *testing.T) {
	t.Parallel()
	os.Setenv("PG_URL", "postgres://prod:5432/app")
	defer os.Unsetenv("PG_URL")

	p := writeTempSourcesYAML(t, `
sources:
  db:
    type: postgres
    connection: ${PG_URL}
    openapi: ./schemas/db.yaml
`)
	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)

	db := file.Sources["db"]
	assert.Equal(t, "postgres://prod:5432/app", db.Connection)
}

func TestParseSourcesYAML_EnvVarInTokenEnv(t *testing.T) {
	t.Parallel()
	os.Setenv("TOKEN_VAR_NAME", "SUPER_SECRET_TOKEN")
	defer os.Unsetenv("TOKEN_VAR_NAME")

	p := writeTempSourcesYAML(t, `
sources:
  api:
    type: rest
    base_url: https://api.example.com
    openapi: ./schemas/api.yaml
    token_env: ${TOKEN_VAR_NAME}
`)
	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)

	api := file.Sources["api"]
	assert.Equal(t, "SUPER_SECRET_TOKEN", api.TokenEnv)
}

// ---------------------------------------------------------------------------
// Validate — edge cases
// ---------------------------------------------------------------------------

func TestValidate_UnknownType(t *testing.T) {
	t.Parallel()
	src := &Source{Name: "test", Type: "kafka"}
	err := src.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown type")
}

func TestValidate_EmptyName(t *testing.T) {
	t.Parallel()
	src := &Source{Name: "", Type: SourceTypeLocal, Root: "/tmp"}
	err := src.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source name is required")
}

func TestValidate_UnknownTypeWithEmptyName(t *testing.T) {
	t.Parallel()
	src := &Source{Name: "", Type: "bogus"}
	err := src.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source name is required")
}

// ---------------------------------------------------------------------------
// ValidSourceTypes completeness
// ---------------------------------------------------------------------------

func TestValidSourceTypes_ContainsAllThree(t *testing.T) {
	t.Parallel()
	assert.True(t, ValidSourceTypes[SourceTypePostgres])
	assert.True(t, ValidSourceTypes[SourceTypeREST])
	assert.True(t, ValidSourceTypes[SourceTypeLocal])
	assert.Len(t, ValidSourceTypes, 3)
}

// ---------------------------------------------------------------------------
// Round-trip: matching spec example verbatim
// ---------------------------------------------------------------------------

func TestParseSourcesYAML_SpecExampleVerbatim(t *testing.T) {
	t.Parallel()
	os.Setenv("DATABASE_URL", "postgres://user:pass@host:5432/db")
	defer os.Unsetenv("DATABASE_URL")

	p := writeTempSourcesYAML(t, `sources:
  database:
    type: postgres
    connection: ${DATABASE_URL}
    openapi: ./schemas/database.openapi.yaml
    allowed_agents: ["stepfun-tester"]
    rate_limit: 10/s
  crm:
    type: rest
    base_url: https://crm.internal/api
    openapi: ./schemas/crm.openapi.yaml
    token_env: CRM_API_TOKEN
  filesystem:
    type: local
    root: /data/shared
    read_only: true
`)
	file, err := ParseSourcesYAML(p)
	require.NoError(t, err)
	assert.Len(t, file.Sources, 3)

	db := file.Sources["database"]
	assert.Equal(t, SourceTypePostgres, db.Type)
	assert.Equal(t, "postgres://user:pass@host:5432/db", db.Connection)
	assert.Equal(t, "./schemas/database.openapi.yaml", db.OpenAPI)
	assert.Equal(t, []string{"stepfun-tester"}, db.AllowedAgents)
	assert.Equal(t, "10/s", db.RateLimit)

	crm := file.Sources["crm"]
	assert.Equal(t, SourceTypeREST, crm.Type)
	assert.Equal(t, "https://crm.internal/api", crm.BaseURL)
	assert.Equal(t, "./schemas/crm.openapi.yaml", crm.OpenAPI)
	assert.Equal(t, "CRM_API_TOKEN", crm.TokenEnv)

	fs := file.Sources["filesystem"]
	assert.Equal(t, SourceTypeLocal, fs.Type)
	assert.Equal(t, "/data/shared", fs.Root)
	assert.True(t, fs.ReadOnly)
}

// ---------------------------------------------------------------------------
// Source Name is not serialised to YAML
// ---------------------------------------------------------------------------

func TestSource_NameNotInYAML(t *testing.T) {
	t.Parallel()
	src := Source{
		Name: "should-not-appear",
		Type: SourceTypeLocal,
		Root: "/tmp",
	}
	data, err := yaml.Marshal(&src)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "name")
	assert.NotContains(t, string(data), "should-not-appear")
}
