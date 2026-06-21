# Sample Task Specification

This document is used by the integration test suite for decomposition testing.

## Phase 1: Setup Database

Create the PostgreSQL database schema for the Helix platform. Run migrations to initialize the tables for agents, tasks, work items, and audit logs.

## Phase 2: Implement Agent API

Implement the REST API endpoints for agent lifecycle management. This includes registration, provisioning, deprovisioning, and status checks. Use the Forgejo admin API for user creation.

## Phase 3: Task Decomposition Engine

Build the spec decomposition engine that reads markdown specifications and extracts discrete tasks. Each task should have a unique ID, priority, and description.

## Phase 4: Integration Testing

Write integration tests that exercise the full agent lifecycle against local Forgejo and Chimera instances. Verify provisioning, SSH key registration, PAT creation, cost estimation, and task decomposition.
