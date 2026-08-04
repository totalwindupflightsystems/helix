// Package source provides configuration parsing, tool generation, and
// capability-gated tool filtering for multi-source integrations in Helix
// (SPEC-025).
//
// This file implements the capability-gating gateway (SRC-003, SPEC-025 §5):
// agents only receive source tools that match the capability claims
// ("fingerprints") in their Helix Identity Document. See Gateway and
// Gateway.Filter for the matching rule and strength tiers.
package source

import (
	"net/http"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/identity"
	"github.com/totalwindupflightsystems/helix/pkg/integration"
)

// ---------------------------------------------------------------------------
// Capability strength tiers (SPEC-025 §5)
// ---------------------------------------------------------------------------
//
// Capability claims assert a strength between 1 and 10. The spec's
// percentage examples map onto that scale as:
//
//	"database: 82%" → strength 1–6 → read-only tools
//	"database: 98%" → strength 7–10 → read-write tools
//
// The constants below are the named, testable thresholds implementing that
// mapping.

const (
	// MinReadOnlyStrength is the lowest capability strength that grants any
	// access to a source's tools. Claims below it (0 or negative — outside
	// the documented 1–10 range) are treated as "no capability": the agent
	// receives zero tools from that source.
	MinReadOnlyStrength = 1

	// MaxReadOnlyStrength is the highest strength that still grants
	// read-only access. Claims at or below this threshold (and at least
	// MinReadOnlyStrength) keep only read methods: GET, HEAD, OPTIONS.
	MaxReadOnlyStrength = 6

	// MinReadWriteStrength is the lowest strength that grants read-write
	// access. Claims at or above this threshold keep every method, unless
	// the source itself is configured read-only.
	MinReadWriteStrength = 7
)

// readOnlyMethods are the HTTP verbs considered safe for read-only access.
var readOnlyMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// ---------------------------------------------------------------------------
// Gateway
// ---------------------------------------------------------------------------

// Gateway filters generated ToolSets per agent (SPEC-025 §5). A Gateway
// holds only the immutable source configuration map and performs no
// mutation, so it is safe for concurrent use.
//
// Capability-domain matching rule: a capability claim applies to a ToolSet
// when either
//
//   - the claim's Domain equals the ToolSet's SourceName (case-insensitive
//     comparison), or
//   - any tool in the ToolSet "mentions" the domain — the domain appears as
//     a case-insensitive substring of the tool's Name, Description, or any
//     of its Scopes.
//
// When several claims match a ToolSet, the strongest one (highest Strength)
// governs the whole set; ties are broken by claim order (first match wins).
//
// Access tiers, decided by the governing claim:
//
//   - no matching claim, or strength below MinReadOnlyStrength → zero
//     tools from that source (the ToolSet is dropped);
//   - MinReadOnlyStrength ≤ strength ≤ MaxReadOnlyStrength → read-only:
//     only GET, HEAD and OPTIONS tools are kept; any other method
//     (including empty or unknown verbs) is stripped;
//   - strength ≥ MinReadWriteStrength → read-write: every tool is kept.
//
// Source policy from .helix/sources.yaml is layered on top of the tiers:
//
//   - Source.AllowedAgents, when non-empty, restricts the source to the
//     listed agent IDs (case-sensitive exact match); an agent not listed
//     receives zero tools from that source;
//   - Source.ReadOnly forces read-only access even for high-strength
//     agents (write methods stripped).
//
// A ToolSet whose source is absent from the Gateway's source map carries no
// source policy: capability claims alone decide access.
type Gateway struct {
	// sources maps source names (as used by ToolSet.SourceName) to their
	// .helix/sources.yaml configuration.
	sources map[string]Source
}

// NewGateway returns a Gateway that consults the given source configuration
// for AllowedAgents and ReadOnly policy. A nil map is treated as empty (no
// source policy anywhere).
func NewGateway(sources map[string]Source) *Gateway {
	return &Gateway{sources: sources}
}

// Filter returns the subset of toolSets the agent may use, given its
// capability claims and agent ID. The result preserves the input ToolSet
// order and the tool order within each set, and contains only non-empty
// ToolSets.
//
// Filtering is total: it never fails and never panics. A nil Gateway, nil
// or empty toolSets, or nil/empty claims all yield an empty result.
func (g *Gateway) Filter(agentID string, claims []identity.CapabilityClaim, toolSets []ToolSet) []ToolSet {
	if g == nil || len(toolSets) == 0 || len(claims) == 0 {
		return nil
	}

	out := make([]ToolSet, 0, len(toolSets))
	for _, ts := range toolSets {
		src, hasSrc := g.lookup(ts.SourceName)

		// AllowedAgents: when the source names an allow-list, the agent
		// must be on it (case-sensitive exact match) to receive anything.
		if hasSrc && len(src.AllowedAgents) > 0 && !containsAgentID(src.AllowedAgents, agentID) {
			continue
		}

		// Capability gating: no matching claim (or a below-range strength)
		// means zero tools from this source.
		claim, ok := bestClaimForSource(claims, ts)
		if !ok || claim.Strength < MinReadOnlyStrength {
			continue
		}

		readWrite := claim.Strength >= MinReadWriteStrength
		if hasSrc && src.ReadOnly {
			readWrite = false
		}

		if readWrite {
			out = append(out, ts)
			continue
		}

		ts.Tools = keepReadOnly(ts.Tools)
		ts.ToolCount = len(ts.Tools)
		if ts.Empty() {
			continue
		}
		out = append(out, ts)
	}
	return out
}

// lookup returns the source configuration for name.
func (g *Gateway) lookup(name string) (Source, bool) {
	if g.sources == nil {
		return Source{}, false
	}
	src, ok := g.sources[name]
	return src, ok
}

// bestClaimForSource returns the strongest claim matching the ToolSet.
// See the Gateway doc comment for the matching rule.
func bestClaimForSource(claims []identity.CapabilityClaim, ts ToolSet) (identity.CapabilityClaim, bool) {
	var best identity.CapabilityClaim
	found := false
	for _, c := range claims {
		if !claimMatchesSource(c.Domain, ts) {
			continue
		}
		if !found || c.Strength > best.Strength {
			best = c
			found = true
		}
	}
	return best, found
}

// claimMatchesSource reports whether the domain applies to the ToolSet:
// it equals the SourceName (case-insensitive) or is mentioned by any tool
// in the set (Name, Description, or Scopes). An empty domain never matches,
// so a malformed claim cannot grant access to everything.
func claimMatchesSource(domain string, ts ToolSet) bool {
	if domain == "" {
		return false
	}
	if strings.EqualFold(domain, ts.SourceName) {
		return true
	}
	for _, tool := range ts.Tools {
		if mentions(domain, tool.Name) || mentions(domain, tool.Description) {
			return true
		}
		for _, scope := range tool.Scopes {
			if mentions(domain, scope) {
				return true
			}
		}
	}
	return false
}

// mentions reports whether domain appears in text, case-insensitively.
func mentions(domain, text string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(domain))
}

// containsAgentID reports whether id appears in agents (case-sensitive
// exact match, per the AllowedAgents contract).
func containsAgentID(agents []string, id string) bool {
	for _, agent := range agents {
		if agent == id {
			return true
		}
	}
	return false
}

// keepReadOnly returns the tools whose HTTP method is a read method
// (GET, HEAD, OPTIONS — case-insensitive). Tools with any other method,
// including an empty or unknown one, are dropped. Tool order is preserved.
func keepReadOnly(tools []integration.MCPTool) []integration.MCPTool {
	kept := make([]integration.MCPTool, 0, len(tools))
	for _, tool := range tools {
		if isReadMethod(tool.Method) {
			kept = append(kept, tool)
		}
	}
	return kept
}

// isReadMethod reports whether the HTTP method is read-only (GET, HEAD,
// OPTIONS). Comparison is case-insensitive.
func isReadMethod(method string) bool {
	return readOnlyMethods[strings.ToUpper(method)]
}
