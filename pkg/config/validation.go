package config

import (
	"fmt"
	"strings"
)

// ValidateAll runs comprehensive validation across every config section,
// returning ALL errors found (not just the first). This enables `helix doctor`
// to report all configuration issues at once rather than fix-one-retry-one.
// The existing Validate() method returns only the first error for backward
// compatibility; ValidateAll is the preferred path for new code.
func (c *Config) ValidateAll() ConfigErrors {
	var errors ConfigErrors

	// Version
	if c.Version != 0 && c.Version != 1 {
		errors = append(errors, ConfigError{
			Section: "version",
			Field:   "version",
			Message: fmt.Sprintf("config version must be 1, got %d", c.Version),
		})
	}

	// Forgejo
	if c.Forgejo.URL == "" {
		errors = append(errors, ConfigError{
			Section: "forgejo", Field: "url", Message: "forgejo.url is required",
		})
	}
	if c.Forgejo.AdminUser == "" {
		errors = append(errors, ConfigError{
			Section: "forgejo", Field: "admin_user", Message: "forgejo.admin_user is recommended",
			Severity: SeverityWarning,
		})
	}

	// Chimera
	if c.Chimera.URL == "" {
		errors = append(errors, ConfigError{
			Section: "chimera", Field: "url", Message: "chimera.url is required",
		})
	}
	if c.Chimera.DefaultFormation == "" {
		errors = append(errors, ConfigError{
			Section: "chimera", Field: "default_formation",
			Message:  "chimera.default_formation not set — will use Chimera's internal default",
			Severity: SeverityWarning,
		})
	}
	if c.Chimera.Timeout != "" && !isValidDurationString(c.Chimera.Timeout) {
		errors = append(errors, ConfigError{
			Section: "chimera", Field: "timeout",
			Message: fmt.Sprintf("chimera.timeout must be a Go duration string (e.g. '5m'), got %q", c.Chimera.Timeout),
		})
	}

	// LangFuse
	if c.LangFuse.Enabled && c.LangFuse.URL == "" {
		errors = append(errors, ConfigError{
			Section: "langfuse", Field: "url",
			Message: "langfuse.enabled is true but langfuse.url is empty",
		})
	}

	// GitReins
	if c.GitReins.TestMode != "" && c.GitReins.TestMode != "full" && c.GitReins.TestMode != "diff" {
		errors = append(errors, ConfigError{
			Section: "gitreins", Field: "test_mode",
			Message: fmt.Sprintf("gitreins.test_mode must be 'full' or 'diff', got %q", c.GitReins.TestMode),
		})
	}
	if c.GitReins.MaxIterations > 0 && c.GitReins.MaxIterations < 10 {
		errors = append(errors, ConfigError{
			Section: "gitreins", Field: "max_iterations",
			Message:  fmt.Sprintf("gitreins.max_iterations of %d is very low — evaluator may not complete", c.GitReins.MaxIterations),
			Severity: SeverityWarning,
		})
	}
	if c.GitReins.MaxTime != "" && !isValidDurationString(c.GitReins.MaxTime) {
		errors = append(errors, ConfigError{
			Section: "gitreins", Field: "max_time",
			Message: fmt.Sprintf("gitreins.max_time must be a Go duration string, got %q", c.GitReins.MaxTime),
		})
	}

	// Estimator
	if c.Estimator.CacheHitRatioPro < 0 || c.Estimator.CacheHitRatioPro > 1 {
		errors = append(errors, ConfigError{
			Section: "estimator", Field: "cache_hit_ratio_pro",
			Message: fmt.Sprintf("estimator.cache_hit_ratio_pro must be between 0 and 1, got %v", c.Estimator.CacheHitRatioPro),
		})
	}
	if c.Estimator.CacheHitRatioFlash < 0 || c.Estimator.CacheHitRatioFlash > 1 {
		errors = append(errors, ConfigError{
			Section: "estimator", Field: "cache_hit_ratio_flash",
			Message: fmt.Sprintf("estimator.cache_hit_ratio_flash must be between 0 and 1, got %v", c.Estimator.CacheHitRatioFlash),
		})
	}
	if c.Estimator.BudgetResetDay != "" {
		validDays := map[string]bool{"sunday": true, "monday": true, "tuesday": true, "wednesday": true, "thursday": true, "friday": true, "saturday": true}
		if !validDays[strings.ToLower(c.Estimator.BudgetResetDay)] {
			errors = append(errors, ConfigError{
				Section: "estimator", Field: "budget_reset_day",
				Message: fmt.Sprintf("estimator.budget_reset_day must be a day name, got %q", c.Estimator.BudgetResetDay),
			})
		}
	}

	// Marketplace
	if c.Marketplace.RegistryPath == "" {
		errors = append(errors, ConfigError{
			Section: "marketplace", Field: "registry_path",
			Message:  "marketplace.registry_path not set — will use default ~/.helix/marketplace",
			Severity: SeverityWarning,
		})
	}
	if c.Marketplace.AutoDeprecationDays > 0 && c.Marketplace.AutoDeprecationDays < 7 {
		errors = append(errors, ConfigError{
			Section: "marketplace", Field: "auto_deprecation_days",
			Message:  fmt.Sprintf("marketplace.auto_deprecation_days of %d is very aggressive — agents may be deprecated too quickly", c.Marketplace.AutoDeprecationDays),
			Severity: SeverityWarning,
		})
	}

	// Negotiation
	if c.Negotiation.MaxRounds > 0 && c.Negotiation.MaxRounds > 10 {
		errors = append(errors, ConfigError{
			Section: "negotiation", Field: "max_rounds",
			Message: fmt.Sprintf("negotiation.max_rounds of %d exceeds spec maximum of 10", c.Negotiation.MaxRounds),
		})
	}
	if c.Negotiation.RoundTimeout != "" && !isValidDurationString(c.Negotiation.RoundTimeout) {
		errors = append(errors, ConfigError{
			Section: "negotiation", Field: "round_timeout",
			Message: fmt.Sprintf("negotiation.round_timeout must be a Go duration string, got %q", c.Negotiation.RoundTimeout),
		})
	}
	if c.Negotiation.GlobalTimeout != "" && !isValidDurationString(c.Negotiation.GlobalTimeout) {
		errors = append(errors, ConfigError{
			Section: "negotiation", Field: "global_timeout",
			Message: fmt.Sprintf("negotiation.global_timeout must be a Go duration string, got %q", c.Negotiation.GlobalTimeout),
		})
	}

	// Prompts
	if c.Prompts.RegistryPath == "" {
		errors = append(errors, ConfigError{
			Section: "prompts", Field: "registry_path",
			Message:  "prompts.registry_path not set — will use default prompts/",
			Severity: SeverityWarning,
		})
	}
	if c.Prompts.DeprecatedGraceDays > 0 && c.Prompts.DeprecatedGraceDays < 7 {
		errors = append(errors, ConfigError{
			Section: "prompts", Field: "deprecated_grace_days",
			Message:  fmt.Sprintf("prompts.deprecated_grace_days of %d is very short — agents may not have time to migrate", c.Prompts.DeprecatedGraceDays),
			Severity: SeverityWarning,
		})
	}

	// Budget
	if c.Budget.OveragePolicy != "" && c.Budget.OveragePolicy != "block" && c.Budget.OveragePolicy != "warn" {
		errors = append(errors, ConfigError{
			Section: "budget", Field: "overage_policy",
			Message: fmt.Sprintf("budget.overage_policy must be 'block' or 'warn', got %q", c.Budget.OveragePolicy),
		})
	}
	if c.Budget.EscalationThreshold < 0 || c.Budget.EscalationThreshold > 1 {
		errors = append(errors, ConfigError{
			Section: "budget", Field: "escalation_threshold",
			Message: fmt.Sprintf("budget.escalation_threshold must be between 0 and 1, got %v", c.Budget.EscalationThreshold),
		})
	}
	if c.Budget.DefaultWeeklyUSD.Pro < 0 {
		errors = append(errors, ConfigError{
			Section: "budget", Field: "default_weekly_usd.pro",
			Message: fmt.Sprintf("budget.default_weekly_usd.pro must be non-negative, got %v", c.Budget.DefaultWeeklyUSD.Pro),
		})
	}
	if c.Budget.DefaultWeeklyUSD.Flash < 0 {
		errors = append(errors, ConfigError{
			Section: "budget", Field: "default_weekly_usd.flash",
			Message: fmt.Sprintf("budget.default_weekly_usd.flash must be non-negative, got %v", c.Budget.DefaultWeeklyUSD.Flash),
		})
	}

	// Secrets (spec secret-management.md §4.2)
	if c.Secrets.Provider != "" && c.Secrets.Provider != "env" && c.Secrets.Provider != "sops" {
		errors = append(errors, ConfigError{
			Section: "secrets", Field: "provider",
			Message: fmt.Sprintf("secrets.provider must be 'env' or 'sops', got %q", c.Secrets.Provider),
		})
	}
	if c.Secrets.Provider == "sops" && c.Secrets.SOPSKeyPath == "" {
		errors = append(errors, ConfigError{
			Section: "secrets", Field: "sops_key_path",
			Message: "secrets.sops_key_path is required when secrets.provider is 'sops'",
		})
	}
	if c.Secrets.Provider == "sops" && c.Secrets.StorePath == "" {
		errors = append(errors, ConfigError{
			Section: "secrets", Field: "store_path",
			Message: "secrets.store_path is required when secrets.provider is 'sops'",
		})
	}

	// Services
	if c.Services.Forgejo.URL == "" && c.Forgejo.URL != "" {
		errors = append(errors, ConfigError{
			Section: "services", Field: "forgejo.url",
			Message:  "services.forgejo.url not set — health checks will use forgejo.url",
			Severity: SeverityWarning,
		})
	}

	return errors
}

// ConfigErrors is a collection of configuration validation issues.
type ConfigErrors []ConfigError

// HasErrors returns true if any errors are at error severity (not just warnings).
// Errors without an explicit severity are treated as errors.
func (errors ConfigErrors) HasErrors() bool {
	for _, e := range errors {
		if e.Severity != SeverityWarning {
			return true
		}
	}
	return false
}

// HasWarnings returns true if any errors are at warning severity.
func (errors ConfigErrors) HasWarnings() bool {
	for _, e := range errors {
		if e.Severity == SeverityWarning {
			return true
		}
	}
	return false
}

// ErrorMessages extracts just the message strings from a list of config errors.
func (errors ConfigErrors) ErrorMessages() []string {
	messages := make([]string, len(errors))
	for i, e := range errors {
		messages[i] = e.Message
	}
	return messages
}

// FormatErrors renders all config errors as a human-readable multi-line string.
func (errors ConfigErrors) FormatErrors() string {
	if len(errors) == 0 {
		return "Configuration valid"
	}
	var b strings.Builder
	for _, e := range errors {
		icon := "✗"
		if e.Severity == SeverityWarning {
			icon = "⚠"
		}
		b.WriteString(fmt.Sprintf("  %s [%s.%s] %s\n", icon, e.Section, e.Field, e.Message))
	}
	return b.String()
}

// --- types ---

// Severity indicates whether a config issue is an error or a warning.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// ConfigError describes a single configuration validation issue.
type ConfigError struct {
	Section  string   `json:"section"`
	Field    string   `json:"field"`
	Message  string   `json:"message"`
	Severity Severity `json:"severity"`
}

// Error implements the error interface.
func (e ConfigError) Error() string {
	return e.Message
}

// isValidDurationString checks if a string looks like a Go duration (e.g. "5m", "30s", "2h").
// Supports compound durations like "1h30m", "2h45m30s".
func isValidDurationString(s string) bool {
	if s == "" {
		return false
	}
	// Must end with a unit character
	last := s[len(s)-1]
	if last >= '0' && last <= '9' {
		return false // bare number without unit
	}
	// Parse as a sequence of number+unit pairs (like Go's time.ParseDuration).
	// e.g. "1h30m" = [("1","h"), ("30","m")]
	rest := s
	for len(rest) > 0 {
		// Extract the numeric portion
		numStart := 0
		for numStart < len(rest) {
			c := rest[numStart]
			if (c >= '0' && c <= '9') || c == '.' {
				numStart++
			} else {
				break
			}
		}
		if numStart == 0 {
			return false // no number before unit
		}
		number := rest[:numStart]
		rest = rest[numStart:]

		// Extract the unit portion
		unitStart := 0
		for unitStart < len(rest) {
			c := rest[unitStart]
			if c >= '0' && c <= '9' || c == '.' {
				break
			}
			unitStart++
		}
		if unitStart == 0 {
			return false // no unit after number
		}
		unit := rest[:unitStart]
		rest = rest[unitStart:]

		// Validate the number is parseable
		for _, c := range number {
			if (c < '0' || c > '9') && c != '.' {
				return false
			}
		}

		// Validate the unit
		validUnits := map[string]bool{
			"ns": true, "us": true, "µs": true, "ms": true,
			"s": true, "m": true, "h": true,
		}
		if !validUnits[unit] {
			return false
		}
	}
	return true
}
