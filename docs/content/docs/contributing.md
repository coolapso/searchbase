---
title: "Contributing"
weight: 60
---

Contributions are welcome. Useful areas include new search providers, crawler improvements, API ergonomics, deployment examples, observability, and documentation.

Please see `AGENTS.md` for internal system architecture and design decisions before making changes to endpoints, data contracts, deployment shape, or service boundaries.

## Contributor License Agreement

All contributors must accept the Searchbase Contributor License Agreement before a pull request can be merged.

The CLA exists because Searchbase is an independent open-source project that may also be offered under commercial licenses. It lets the project remain open-source while preserving the ability to provide commercial licensing options.

The CLA Assistant workflow checks pull requests automatically. If you have not signed yet, the bot will comment with instructions. Sign by commenting exactly:

```text
I have read the CLA Document and I hereby sign the CLA
```

Accepted signatures are stored on the `cla-signatures` branch under `.github/cla/signatures/v1/cla.json`.

Read the full agreement in [`CLA.md`](https://github.com/coolapso/searchbase/blob/main/CLA.md).

## Development

Searchbase uses [Task](https://taskfile.dev/) to automate development and deployment tasks.
User-facing Compose demos live in `examples/compose/`. Checkout-built
development stacks and the isolated load-test stack live in `dev/compose/`.
Run the tasks below from the repository root; the development stacks share
the `searchbase-dev` Compose project.

## Common Tasks

You can run these tasks from the root directory.

| Command | Description |
| :--- | :--- |
| `task dev:up` | Build and start the Lightpanda development stack. |
| `task dev:upcrawl4ai` | Build and start the Crawl4AI development stack. |
| `task dev:down` | Stop the active development stack. |
| `task docs:hugo` | Run the documentation site locally. |

## Service-Specific Tasks

Each service has its own `Taskfile.yml` for more granular control. For example, in the `search-gateway` directory:

- `task test:gateway`: Run Go tests.
- `task test:fmt`: Check code formatting.
- `task fmt`: Format Go code.
- `task docs:swagger`: Regenerate Swagger/OpenAPI documentation.
- `task build`: Build the `search-gateway` binary locally.

## AI Disclosure & Contribution Policy

I believe in transparency regarding how this project is built. While AI tools (like Copilot, Claude, or local LLMs) are extensively used in the development of Searchbase, **the core architecture, design decisions, and critical business logic are human-led and human-written** with AI assistance. AI acts as an assistant, not a replacement.

However, I do heavily delegate boilerplate tasks, CI/CD pipeline generation, testing setups, and documentation writing to AI agents to maximize efficiency.

When contributing to this project, using AI to assist you is absolutely fine and encouraged! However, please ensure you deeply understand the code you are submitting. **Low-effort, blindly copy-pasted AI-generated Pull Requests will not be accepted.** Quality, intent, and architectural alignment matter far more than speed.
