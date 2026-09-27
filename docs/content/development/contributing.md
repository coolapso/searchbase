---
title: "Contributing"
weight: 20
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

Searchbase uses [Task](https://taskfile.dev/) to automate development tasks.
Install Docker Engine with the Compose plugin and Task, then run the commands
below from the repository root. User-facing demos live in `examples/compose/`
and `examples/k8s/`; checkout-built stacks live in `dev/compose/` and share
the `searchbase-dev` Compose project.

## Common Tasks

| Command | Description |
| :--- | :--- |
| `task dev:up` | Build and start the Lightpanda development stack. |
| `task dev:upcrawl4ai` | Build and start the Crawl4AI development stack. |
| `task dev:down` | Stop the active development stack. |
| `task docs:hugo` | Preview the documentation site locally. |

These tasks supply `--project-directory .` so Compose resolves build contexts
from the repository root. Direct Compose commands need the same flag and a
file under `dev/compose/`. The load-test stack in that directory uses a
separate project; see [Load testing](/development/load-testing/).

## Service-Specific Tasks

Each service has its own `Taskfile.yml` for more granular control. For example,
from the repository root:

- `task test:gateway`: Run gateway Go tests.
- `task lightpanda-worker:test`: Run Lightpanda wrapper tests.
- `task lightpanda-worker:build`: Build the Lightpanda wrapper binary.
- `task lightpanda-worker:container:build`: Build its local container image.

The gateway Taskfile also provides `test:fmt`, `fmt`, `docs:swagger`, and
`build` tasks. The Lightpanda Taskfile provides `container:build:all` for local
amd64 and arm64 builds and `container:push` for authenticated GHCR publishing.
The root `container:build`, `container:build:all`, and `container:push` tasks
include the Lightpanda worker. The release workflow publishes versioned
images from a semantic release tag.

## AI Disclosure & Contribution Policy

I believe in transparency regarding how this project is built. While AI tools (like Copilot, Claude, or local LLMs) are extensively used in the development of Searchbase, **the core architecture, design decisions, and critical business logic are human-led and human-written** with AI assistance. AI acts as an assistant, not a replacement.

However, I do heavily delegate boilerplate tasks, CI/CD pipeline generation, testing setups, and documentation writing to AI agents to maximize efficiency.

When contributing to this project, using AI to assist you is absolutely fine and encouraged! However, please ensure you deeply understand the code you are submitting. **Low-effort, blindly copy-pasted AI-generated Pull Requests will not be accepted.** Quality, intent, and architectural alignment matter far more than speed.
