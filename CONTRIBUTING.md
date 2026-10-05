# Contributing to Searchbase

Contributions are welcome.

Before opening a pull request, please read:

- [`AGENTS.md`](./AGENTS.md) for architecture, data contracts, and documentation requirements.
- [`CLA.md`](./CLA.md) for the Contributor License Agreement.

## Contributor License Agreement

All contributors must accept the Searchbase Contributor License Agreement before a pull request can be merged.

The CLA Assistant workflow checks whether every pull request author has signed. If a signature is missing, the bot comments with signing instructions. Sign by commenting exactly:

```text
I have read the CLA Document and I hereby sign the CLA
```

Accepted signatures are stored in the repository on the `cla-signatures` branch under `.github/cla/signatures/v1/cla.json`.

## Documentation Requirement

If your change affects setup, configuration, provider behavior, API behavior, MCP usage, deployment, observability, or user-facing behavior, update the Hugo documentation site under `docs/`.

## Swagger checks for API changes

For every API-related change, always review Swagger/OpenAPI for affected endpoints, request/response schemas, status codes, and error descriptions, including worker or MCP changes that affect REST behavior. Update the Go Swagger annotations, run `task search-gateway:docs:swagger` and `task search-gateway:test:lint:swagger` from the repository root, and review all three generated files (`search-gateway/docs/docs.go`, `swagger.json`, and `swagger.yaml`). Include any generated changes in the same commit as the API change. Regeneration alone is not enough: verify the descriptions match runtime behavior.

Run this before submitting documentation changes:

```bash
cd docs
hugo --destination /tmp/searchbase-docs-build
```
