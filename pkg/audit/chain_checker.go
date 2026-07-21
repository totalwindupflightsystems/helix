package audit

// =============================================================================
// Audit Checker
// =============================================================================

// Checker validates audit evidence for all 12 steps.
type Checker struct {
	validators map[StepID]StepValidator
}

// NewChecker creates a Checker with default validators.
func NewChecker() *Checker {
	return &Checker{
		validators: defaultValidators(),
	}
}

// NewCheckerWithValidators creates a Checker with custom validators.
func NewCheckerWithValidators(validators map[StepID]StepValidator) *Checker {
	return &Checker{validators: validators}
}
