# Sample Prompt for Attestation Testing

This prompt is used by the integration test suite to verify the prompt attestation workflow.

## Purpose

The purpose of this prompt is to test the SHA256 hash computation and attestation record creation for prompt files in the Helix integration test suite.

## Expected Behavior

When this prompt is attested, the system should:
1. Compute the SHA256 hash of the prompt content
2. Create an attestation record with the hash
3. Return the attestation with status "attested"

## Constraints

- This is a test fixture, not a production prompt
- No real API calls are made during attestation testing
- The hash is computed at test time, not hardcoded
