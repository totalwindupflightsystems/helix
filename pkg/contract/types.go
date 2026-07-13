// Package contract implements API contract generation, validation, breaking
// change detection, and immutable storage (Phase 2 §2.4).
package contract

import (
	"encoding/json"
	"time"
)

// ContractFormat identifies the schema format of a contract.
type ContractFormat string

const (
	FormatOpenAPI  ContractFormat = "openapi"
	FormatProtobuf ContractFormat = "protobuf"
	FormatGraphQL  ContractFormat = "graphql"
)

// ValidFormat reports whether f is one of the known contract formats.
func ValidFormat(f ContractFormat) bool {
	switch f {
	case FormatOpenAPI, FormatProtobuf, FormatGraphQL:
		return true
	}
	return false
}

// Contract is an API contract with an immutable hash when frozen.
type Contract struct {
	ID        string          `json:"id"`
	SpecRef   string          `json:"spec_ref"`
	ADRRefs   []string        `json:"adr_refs,omitempty"`
	Format    ContractFormat  `json:"format"`
	Schema    json.RawMessage `json:"schema"`
	Hash      string          `json:"hash"`
	FrozenAt  time.Time       `json:"frozen_at,omitempty"`
	Version   int             `json:"version"`
	CreatedAt time.Time       `json:"created_at"`
}

// IsFrozen reports whether the contract has been frozen (immutable).
func (c *Contract) IsFrozen() bool { return !c.FrozenAt.IsZero() }

// BreakingChange describes a schema-level breaking change between two contracts.
type BreakingChange struct {
	Field      string   `json:"field"`
	OldType    string   `json:"old_type,omitempty"`
	NewType    string   `json:"new_type,omitempty"`
	ChangeType string   `json:"change_type"`
	Consumers  []string `json:"consumers,omitempty"`
}

// ConsumerImpact describes how a breaking change affects a specific consumer.
type ConsumerImpact struct {
	ConsumerName    string   `json:"consumer_name"`
	BreakingChanges []string `json:"breaking_changes"`
	Severity        string   `json:"severity"`
}

// ValidationReport is the output of multi-model contract validation.
type ValidationReport struct {
	Consistent       bool              `json:"consistent"`
	BreakingChanges  []BreakingChange   `json:"breaking_changes,omitempty"`
	MissingEndpoints []string           `json:"missing_endpoints,omitempty"`
	ConsumerImpacts  []ConsumerImpact   `json:"consumer_impacts,omitempty"`
	Warnings         []string           `json:"warnings,omitempty"`
}
