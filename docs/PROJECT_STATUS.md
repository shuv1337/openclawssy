# Project Status

## Current state

Openclawssy is a **prototype in development**.

What works now:
- CLI setup and run loop
- tool registry with policy gates
- sandbox-gated `shell.exec`
- scheduler queueing runs
- HTTP API + dashboard
- Discord bridge queueing runs
- encrypted secret ingestion

What is not production-ready:
- compatibility and schema stability
- full authn/authz model for multi-tenant use
- external security review
- complete observability and disaster recovery

## Recommendation

Do not deploy this to production.
Use only local/dev environments with test credentials.
