package dispatcher

// source_tools.go — SRC-005 dispatcher wiring (SPEC-025 §5/§6, build order
// item 5): inject capability-gated source tools at task dispatch.
//
// The seam: Dispatcher.Dispatch / DispatchContext consult a
// SourceToolInjector (when attached via Dispatcher.SourceTools) and attach
// the filtered ToolSets to each assigned WorkItem, keyed by source name so
// the execution loop can rate-limit per source before running a tool.
//
//   - Tool generation is provider-based: the default provider is
//     MusterBridge.GenerateToolsFromSource, but callers may inject any
//     ToolsProvider (tests use static sets; no live Muster required).
//   - Gating is delegated to source.Gateway.Filter: an agent receives a
//     source's tools only when one of its identity.CapabilityClaim domains
//     matches that source, read-only strength (1–6) strips write methods,
//     read-write strength (7–10) keeps everything, and Source.ReadOnly /
//     Source.AllowedAgents policy is layered on top.
//   - A bridge that cannot reach Muster fails cleanly: generation errors are
//     collected per source and returned alongside any sets that did succeed —
//     never a panic, never a hang.

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/totalwindupflightsystems/helix/pkg/source"
)

// DefaultSourcesPath is the default location of the multi-source
// configuration file, relative to the process working directory.
const DefaultSourcesPath = ".helix/sources.yaml"

// ToolsProvider generates a ToolSet for a single source. It is the seam that
// keeps the injector testable without a live Muster: tests supply a provider
// that returns pre-built ToolSets.
type ToolsProvider func(ctx context.Context, src *source.Source) (*source.ToolSet, error)

// SourceToolInjector wires pkg/source into the dispatch path: it holds the
// source configuration, the capability-gating gateway, the tool generator,
// and the per-source rate limiter for a single dispatch invocation.
//
// A SourceToolInjector is not goroutine-safe for mutation but its methods
// are safe for concurrent use after construction (the underlying gateway and
// rate-limit manager are read-only / internally synchronized).
type SourceToolInjector struct {
	sources   map[string]source.Source
	gateway   *source.Gateway
	provider  ToolsProvider
	rateLimit *source.RateLimitManager
}

// sourceToolInjectorConfig accumulates constructor options.
type sourceToolInjectorConfig struct {
	provider ToolsProvider
}

// SourceToolInjectorOption configures a SourceToolInjector at construction.
type SourceToolInjectorOption func(*sourceToolInjectorConfig)

// WithMusterBridge uses the bridge's GenerateToolsFromSource as the tool
// provider. A nil bridge is ignored (no provider configured).
func WithMusterBridge(b *source.MusterBridge) SourceToolInjectorOption {
	return func(cfg *sourceToolInjectorConfig) {
		if b != nil {
			cfg.provider = b.GenerateToolsFromSource
		}
	}
}

// WithToolsProvider uses p as the tool provider. It takes precedence over
// WithMusterBridge regardless of option order.
func WithToolsProvider(p ToolsProvider) SourceToolInjectorOption {
	return func(cfg *sourceToolInjectorConfig) {
		if p != nil {
			cfg.provider = p
		}
	}
}

// NewSourceToolInjector builds an injector from source configuration.
// A nil sources map is treated as empty (no sources, no tools). An invalid
// rate_limit on any source is an error at construction time so
// misconfiguration is surfaced before dispatch.
//
// With no provider option, the injector is constructed but Inject returns a
// clean error until a provider is supplied — callers that always have Muster
// available should pass WithMusterBridge.
func NewSourceToolInjector(sources map[string]source.Source, opts ...SourceToolInjectorOption) (*SourceToolInjector, error) {
	if sources == nil {
		sources = make(map[string]source.Source)
	}

	cfg := sourceToolInjectorConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	rl, err := source.NewRateLimitManager(sources)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: source tool injector: %w", err)
	}

	return &SourceToolInjector{
		sources:   sources,
		gateway:   source.NewGateway(sources),
		provider:  cfg.provider,
		rateLimit: rl,
	}, nil
}

// NewSourceToolInjectorFromFile loads sources from a .helix/sources.yaml
// file and constructs an injector from them. An empty path defaults to
// DefaultSourcesPath. A missing file yields an injector with zero sources
// (matching ParseSourcesYAML's contract) rather than an error.
func NewSourceToolInjectorFromFile(path string, opts ...SourceToolInjectorOption) (*SourceToolInjector, error) {
	if path == "" {
		path = DefaultSourcesPath
	}
	file, err := source.ParseSourcesYAML(path)
	if err != nil {
		return nil, err
	}
	return NewSourceToolInjector(file.Sources, opts...)
}

// Inject generates tool sets for every configured source and returns the
// subset the agent may use, gated by its capability claims
// (AgentProfile.Capabilities), Source.AllowedAgents, and Source.ReadOnly.
// The agent's Name is used as the agent ID for AllowedAgents matching.
//
// Generation is best-effort per source: a provider failure (e.g. Muster
// unreachable) is recorded and reported, while tool sets that did generate
// are still gated and returned. The returned error is nil only when every
// source generated successfully; otherwise it joins all per-source errors.
// A nil error with an empty result means the agent is entitled to no tools.
func (in *SourceToolInjector) Inject(ctx context.Context, agent AgentProfile) ([]source.ToolSet, error) {
	if in == nil {
		return nil, errors.New("dispatcher: nil SourceToolInjector")
	}
	if in.provider == nil {
		return nil, errors.New("dispatcher: source tool injector has no tools provider configured (use WithMusterBridge or WithToolsProvider)")
	}
	if len(in.sources) == 0 {
		return []source.ToolSet{}, nil
	}

	// Deterministic generation order: iterate source names sorted so that
	// repeated Inject calls produce identical error precedence.
	names := make([]string, 0, len(in.sources))
	for name := range in.sources {
		names = append(names, name)
	}
	sort.Strings(names)

	generated := make([]source.ToolSet, 0, len(names))
	var genErrs []error
	for _, name := range names {
		src := in.sources[name]
		ts, err := in.provider(ctx, &src)
		if err != nil {
			genErrs = append(genErrs, fmt.Errorf("source %q: %w", name, err))
			continue
		}
		if ts == nil {
			genErrs = append(genErrs, fmt.Errorf("source %q: tools provider returned nil ToolSet", name))
			continue
		}
		generated = append(generated, *ts)
	}

	filtered := in.gateway.Filter(agent.Name, agent.Capabilities, generated)
	if len(genErrs) > 0 {
		return filtered, errors.Join(genErrs...)
	}
	return filtered, nil
}

// Wait blocks until a token is available for the named source or ctx is
// cancelled. An unknown source name is an error so callers catch
// misconfiguration. Call this BEFORE executing any tool belonging to the
// source.
func (in *SourceToolInjector) Wait(ctx context.Context, sourceName string) error {
	if in == nil {
		return errors.New("dispatcher: nil SourceToolInjector")
	}
	if in.rateLimit == nil {
		return fmt.Errorf("dispatcher: no rate limiter available for source %q", sourceName)
	}
	return in.rateLimit.Wait(ctx, sourceName)
}

// SourceForTool resolves the name of the source that owns toolName within
// sets (the carrier attached to a WorkItem). It returns false when the tool
// is not found or toolName is empty.
func (in *SourceToolInjector) SourceForTool(toolName string, sets []source.ToolSet) (string, bool) {
	if in == nil || toolName == "" {
		return "", false
	}
	for _, ts := range sets {
		for _, tool := range ts.Tools {
			if tool.Name == toolName {
				return ts.SourceName, true
			}
		}
	}
	return "", false
}

// WaitForTool is the documented pre-execution gate for a single source tool:
// it resolves the tool's owning source within sets and waits for that
// source's rate limit, so execution loops can call it immediately before
// invoking the tool:
//
//	if err := in.WaitForTool(ctx, tool.Name, item.SourceTools); err != nil {
//	    return err // rate limit unavailable or context cancelled
//	}
func (in *SourceToolInjector) WaitForTool(ctx context.Context, toolName string, sets []source.ToolSet) error {
	srcName, ok := in.SourceForTool(toolName, sets)
	if !ok {
		return fmt.Errorf("dispatcher: tool %q not found in any attached source tool set", toolName)
	}
	return in.Wait(ctx, srcName)
}
