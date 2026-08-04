// Package source provides configuration parsing and validation for
// multi-source integrations in Helix. Sources are defined in
// .helix/sources.yaml and describe external systems (databases, REST
// APIs, local filesystems) that Helix agents can interact with.
package source

import (
	"fmt"
	"os"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// SourceType
// ---------------------------------------------------------------------------

// SourceType enumerates the supported integration source kinds.
type SourceType string

const (
	SourceTypePostgres SourceType = "postgres"
	SourceTypeREST     SourceType = "rest"
	SourceTypeLocal    SourceType = "local"
)

// ValidSourceTypes is the set of recognised SourceType values.
var ValidSourceTypes = map[SourceType]bool{
	SourceTypePostgres: true,
	SourceTypeREST:     true,
	SourceTypeLocal:    true,
}

// ---------------------------------------------------------------------------
// Source
// ---------------------------------------------------------------------------

// Source represents a single integration source defined in
// .helix/sources.yaml.
//
// Name is derived from the YAML map key and is not a YAML field.
type Source struct {
	Name          string     `yaml:"-"`
	Type          SourceType `yaml:"type"`
	Connection    string     `yaml:"connection"`
	OpenAPI       string     `yaml:"openapi"`
	AllowedAgents []string   `yaml:"allowed_agents"`
	RateLimit     string     `yaml:"rate_limit"`
	TokenEnv      string     `yaml:"token_env"`
	BaseURL       string     `yaml:"base_url"`
	Root          string     `yaml:"root"`
	ReadOnly      bool       `yaml:"read_only"`
	Enabled       *bool      `yaml:"enabled"`
}

// IsEnabled reports whether the source is enabled. A missing (nil)
// enabled field means the source is enabled by default.
func (s *Source) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

// Validate checks that the source definition is complete and consistent.
// Different source types have different required fields.
func (s *Source) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("source name is required")
	}
	if !ValidSourceTypes[s.Type] {
		return fmt.Errorf("source %q: unknown type %q (must be one of: postgres, rest, local)", s.Name, s.Type)
	}

	switch s.Type {
	case SourceTypePostgres:
		if s.Connection == "" {
			return fmt.Errorf("source %q (postgres): connection is required", s.Name)
		}
		if s.OpenAPI == "" {
			return fmt.Errorf("source %q (postgres): openapi is required", s.Name)
		}
	case SourceTypeREST:
		if s.BaseURL == "" {
			return fmt.Errorf("source %q (rest): base_url is required", s.Name)
		}
		if s.OpenAPI == "" {
			return fmt.Errorf("source %q (rest): openapi is required", s.Name)
		}
	case SourceTypeLocal:
		if s.Root == "" {
			return fmt.Errorf("source %q (local): root is required", s.Name)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// SourcesFile — top-level .helix/sources.yaml structure
// ---------------------------------------------------------------------------

// SourcesFile is the top-level structure of a .helix/sources.yaml file.
type SourcesFile struct {
	Sources map[string]Source `yaml:"sources"`
}

// ---------------------------------------------------------------------------
// Parse
// ---------------------------------------------------------------------------

// envVarPattern matches ${VAR_NAME} references in YAML string values.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ParseSourcesYAML reads a .helix/sources.yaml file from path, expands
// environment-variable references (${VAR}), validates every source, and
// returns the populated SourcesFile.
//
// A non-existent path returns an empty file (zero sources) without an error.
func ParseSourcesYAML(path string) (*SourcesFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SourcesFile{Sources: make(map[string]Source)}, nil
		}
		return nil, fmt.Errorf("source: cannot read %s: %w", path, err)
	}

	// Expand ${VAR} references before unmarshalling so that YAML scalars
	// containing env-var syntax are resolved.
	expanded := expandEnvVars(string(data))

	var file SourcesFile
	if err := yaml.Unmarshal([]byte(expanded), &file); err != nil {
		return nil, fmt.Errorf("source: invalid YAML in %s: %w", path, err)
	}

	// Validate each source in deterministic order (alphabetical by name).
	// Map iteration is random in Go, so we sort keys to ensure the
	// same source always fails first for reproducible error messages.
	names := make([]string, 0, len(file.Sources))
	for name := range file.Sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		src := file.Sources[name]
		src.Name = name
		if err := src.Validate(); err != nil {
			return nil, err
		}
		file.Sources[name] = src
	}

	return &file, nil
}

// ---------------------------------------------------------------------------
// env-var expansion
// ---------------------------------------------------------------------------

// expandEnvVars replaces ${VAR_NAME} patterns with the value of the
// corresponding environment variable. Unknown variables are replaced
// with the empty string (matching os.Expand behaviour).
func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		// match looks like "${DATABASE_URL}"; strip the wrapping syntax.
		name := match[2 : len(match)-1]
		return os.Getenv(name)
	})
}
