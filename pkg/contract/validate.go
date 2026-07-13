package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ContractValidator runs multi-model validation on a contract.
type ContractValidator struct{}

// NewContractValidator creates a ContractValidator.
func NewContractValidator() *ContractValidator {
	return &ContractValidator{}
}

// Validate runs multi-model validation checks.
func (v *ContractValidator) Validate(ctx context.Context, contract *Contract, previous *Contract, consumers []string) (*ValidationReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if contract == nil {
		return nil, fmt.Errorf("contract: contract is nil")
	}

	report := &ValidationReport{Consistent: true}

	v.checkConsistency(contract, report)
	v.checkCompleteness(contract, report)

	if previous != nil {
		changes := DetectChanges(contract, previous)
		if len(changes) > 0 {
			report.BreakingChanges = changes
			report.Consistent = false
		}
		if len(consumers) > 0 && len(changes) > 0 {
			catalog := make(map[string][]string)
			for _, c := range consumers {
				catalog[c] = []string{"*"}
			}
			report.ConsumerImpacts = ConsumerImpactReport(changes, catalog)
		}
	}

	return report, nil
}

func (v *ContractValidator) checkConsistency(c *Contract, report *ValidationReport) {
	var schema map[string]interface{}
	if err := json.Unmarshal(c.Schema, &schema); err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("schema is not valid JSON: %v", err))
		report.Consistent = false
		return
	}

	switch c.Format {
	case FormatOpenAPI:
		for _, key := range []string{"openapi", "info", "paths"} {
			if _, ok := schema[key]; !ok {
				report.Warnings = append(report.Warnings, fmt.Sprintf("OpenAPI: missing required field %q", key))
				report.Consistent = false
			}
		}
	case FormatProtobuf:
		for _, key := range []string{"syntax", "package"} {
			if _, ok := schema[key]; !ok {
				report.Warnings = append(report.Warnings, fmt.Sprintf("Protobuf: missing required field %q", key))
				report.Consistent = false
			}
		}
	case FormatGraphQL:
		if _, ok := schema["schema"]; !ok {
			report.Warnings = append(report.Warnings, "GraphQL: missing required field 'schema'")
			report.Consistent = false
		}
		if _, ok := schema["types"]; !ok {
			report.Warnings = append(report.Warnings, "GraphQL: missing required field 'types'")
			report.Consistent = false
		}
	}
}

func (v *ContractValidator) checkCompleteness(c *Contract, report *ValidationReport) {
	var schema map[string]interface{}
	if err := json.Unmarshal(c.Schema, &schema); err != nil {
		return
	}

	switch c.Format {
	case FormatOpenAPI:
		paths, ok := schema["paths"].(map[string]interface{})
		if !ok || len(paths) == 0 {
			report.Warnings = append(report.Warnings, "OpenAPI: no endpoints defined in paths")
		} else {
			for path, methods := range paths {
				m, ok := methods.(map[string]interface{})
				if !ok {
					continue
				}
				for method := range m {
					_ = path
					_ = method
					if !hasErrorResponses(m, method) {
						report.Warnings = append(report.Warnings,
							fmt.Sprintf("OpenAPI %s %s: missing error response (4xx/5xx)", strings.ToUpper(method), path))
					}
				}
			}
		}
	case FormatProtobuf:
		svcs, ok := schema["services"].([]interface{})
		if !ok || len(svcs) == 0 {
			report.Warnings = append(report.Warnings, "Protobuf: no services defined")
		}
	case FormatGraphQL:
		types, ok := schema["types"].([]interface{})
		if !ok || len(types) == 0 {
			report.Warnings = append(report.Warnings, "GraphQL: no types defined")
		}
	}
}

func hasErrorResponses(methods map[string]interface{}, method string) bool {
	meth, ok := methods[method].(map[string]interface{})
	if !ok {
		return false
	}
	responses, ok := meth["responses"].(map[string]interface{})
	if !ok {
		return false
	}
	for code := range responses {
		if strings.HasPrefix(code, "4") || strings.HasPrefix(code, "5") {
			return true
		}
	}
	return false
}
