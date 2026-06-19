# Helix — Agent-First Code Platform

## Concept
Two strands, one structure. Human intelligence and agent intelligence spiral
together through every phase of the SDLC. Neither replaces the other.

## Architecture
6-layer stack: Human Interface → Orchestration → Execution → Git Forge →
Quality & Review → Observability & Memory.

## Features (in build order)
1. Agent Identity — H4F known-friends.json → Forgejo OAuth
2. Cost Estimator — Pre-flight token burn estimate
3. PR Negotiation — Agent-to-agent debate with Chimera tie-breaking
4. Prompt Registry — prompts/ in every repo, version-controlled
5. Agent Marketplace — Discoverable registry with reputation scores

## Tech Stack
- CLI: Go (cobra/viper)
- Identity: Go + Forgejo REST API
- Container runtime: Docker (H4F sandbox)
- MCP integration: Muster (auto-generated from OpenAPI)

