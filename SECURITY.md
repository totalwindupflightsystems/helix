# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| main    | :white_check_mark: |
| < main  | :x:                |

## Reporting a Vulnerability

Helix is an agent-first code platform. Security issues involving agent provisioning, sandbox isolation, prompt attestation, or credential handling are treated as critical.

**Do NOT open a public issue.** Email security disclosures to: wojonstech@gmail.com

### What to include

- Description of the vulnerability
- Steps to reproduce
- Affected components (identity, negotiate, prompt, marketplace, sandbox)
- Any proof-of-concept code

### Response timeline

- Acknowledgment: within 48 hours
- Triage: within 5 business days
- Fix: depends on severity — critical issues patched immediately

## Security Model

Helix's security model is built on:

1. **Agent Identity** — Forgejo-backed provisioning with ED25519 keypairs and PAT management
2. **Sandbox Isolation** — Bubblewrap namespace isolation (none / workspace / full modes)
3. **Prompt Attestation** — Content-hash provenance for all prompt submissions
4. **GitReins Guards** — Pre-commit secrets scanning, linting, and test enforcement
5. **Trust Scoring** — Graduated trust tiers with incident-linked decay

## Responsible Disclosure

We follow a 90-day disclosure timeline. Researchers who report vulnerabilities responsibly will be credited in release notes unless they request anonymity.
