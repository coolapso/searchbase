# SEARCHBASE (Bot Accessible Search Engine) - Architecture & Specifications

**⚠️ CORE MANDATE FOR AI AGENTS:** 
This file (`AGENTS.md`) is the single source of truth for the project's architecture, design decisions, and data contracts. **It must always be updated** whenever relevant architectural changes, new endpoints, or structural modifications are made during the development process. Additionally, you must **always update the Hugo documentation site under `docs/`** with relevant user-facing information (setup instructions, provider behavior, MCP client setup, configuration, deployment notes, API behavior, observability, etc.) and keep `README.md` as the concise repository landing page. Always consult this file to understand the system context.

**API documentation mandate:** For every API-related change, always review Swagger/OpenAPI for affected endpoints, request/response schemas, status codes, and error descriptions, including worker or MCP changes that affect REST behavior. Update the Go Swagger annotations, run `task search-gateway:docs:swagger` and `task search-gateway:test:lint:swagger` from the repository root, and review all three generated files (`search-gateway/docs/docs.go`, `swagger.json`, and `swagger.yaml`). Include any generated changes in the same commit as the API change. Regeneration alone is not enough: verify the descriptions match runtime behavior.

## 1. Project Overview
An open-source, self-hostable, privacy-focused web search API for AI agents. Search is the primary product: it returns compact results (title, URL, snippet) through REST and natively through the **Model Context Protocol (MCP)** for plug-and-play integration with modern LLM clients (OpenWebUI, Claude Desktop, Cursor, Opencode, Neovim CodeCompanion, etc.). Page fetching is a secondary, explicitly requested companion that returns one page as Markdown.

It is provider-flexible: self-hosted deployments choose their search provider, including providers that need no paid API key. It works alongside frontier and local models rather than replacing them, so the model provider does not also control the agent's web access.

**Positioning and copy:** Keep wording aligned with the managed counterpart, Searchbase Cloud (`~/Dev/searchbasecloud`): independence, privacy, open source, self-hostable, provider choice. Lead with search; present fetch as optional. Make no token-saving or token-optimization claims, and no performance claims beyond measured load-test results with their stated caveats, in product copy, tool descriptions, or docs. Token use depends on the client, model, provider, and pages and cannot be measured in absolute terms; say plainly that Searchbase aims for compact, readable results and is built for independence and privacy, not for the lowest token count.

## 2. System Architecture
The system uses a **Hybrid Microservice Architecture** designed for Kubernetes:

### A. Component 1: `search-gateway` (Go)
The front-facing orchestrator. Built in Go for high concurrency, low memory footprint, and fast routing.
*   **Role:** API Gateway, Search Scraper, Orchestrator, MCP Server.
*   **Responsibilities:**
    *   Expose standard REST API (`/api/v1/search` and `/api/v1/fetch`).
    *   Expose Swagger UI documentation (`/docs`).
    *   Expose MCP Server over SSE (`/mcp/sse` and `/mcp/message`) and Streamable HTTP (`/mcp/http`).
    *   Optionally send periodic MCP heartbeat pings on both transports to keep idle connections from being dropped by clients with a shorter read timeout. Off by default, enabled with `SEARCHBASE_MCP_HEARTBEAT_ENABLED=true` and tuned with `SEARCHBASE_MCP_HEARTBEAT_INTERVAL` (seconds, default `60`, minimum `15` enforced at startup).
    *   Auto-generate OpenAPI specs.
    *   Scrape DuckDuckGo (HTML version: `html.duckduckgo.com`) to extract top URLs when using the native `searchbase_ddg` provider.
    *   Call official provider APIs when configured, including Brave (`SEARCHBASE_SEARCH_PROVIDER=brave` and `SEARCHBASE_BRAVE_API_TOKEN`) and Mojeek (`SEARCHBASE_SEARCH_PROVIDER=mojeek` and `SEARCHBASE_MOJEEK_API_KEY`).
    *   Return compact search results (`title`, `url`, `snippet`) without automatically fetching page content.
    *   Fetch page content only through `/api/v1/fetch` or the MCP `fetch_url` tool when the client/LLM explicitly requests it.
    *   **Configuration:** Uses Viper library with centralized `settings` package (`internal/settings`) enforcing a strict `SEARCHBASE_` prefix for all environment variables (e.g., `SEARCHBASE_PORT`, `SEARCHBASE_CRAWL_WORKER_ADDRESS`, `SEARCHBASE_LOG_LEVEL`). Supports defaults and automatic env var binding. Grouped settings live in their own files (`otel.go` for tracing, `mcp.go` for MCP transport) and expose a `validate()` method so misconfiguration fails fast at startup.
*   **Design Pattern:** Uses Interfaces (e.g., `SearchProvider`) to allow easy plugging of search APIs and metasearch providers without changing core logic. Current providers include native DuckDuckGo HTML (`searchbase_ddg`), DDGS (`ddgs`), SearXNG (`searxng`), Brave Search API (`brave`), and Mojeek Search API (`mojeek`).

### B. Component 2: `crawl-worker` (Python)
The internal heavy-lifter. Completely hidden from the outside world.
*   **Role:** Headless browser, DOM cleaner, Markdown converter.
*   **Tech Stack:** Python, FastAPI, `crawl4ai`.
*   **Optional alternative:** `lightpanda-worker/` is an experimental Go HTTP wrapper around the official Lightpanda browser image. It implements the same `POST /extract` JSON shape, accepts but ignores `js_render`, and always executes JavaScript before returning Lightpanda's native Markdown dump with `--strip-mode clutter`. It does not replace the default Crawl4AI worker or promise identical Markdown cleaning. Keep it internal; `LIGHTPANDA_DISABLE_TELEMETRY=true` is set in its container. The worker launches one browser `fetch` process per request with bounded concurrency, timeout, and output size.
*   **Lightpanda URL boundary:** Before invoking the browser, validate and normalize the target as an absolute HTTP(S) URL with a hostname, no credentials, no whitespace/control characters or backslashes, and a valid port; strip fragments. Validate again in the fetcher so callers cannot bypass the HTTP handler. This is URL syntax validation. The browser additionally runs with `--block-private-networks` by default, blocking private/internal destination IPs after DNS resolution. `LIGHTPANDA_WORKER_ALLOW_PRIVATE_NETWORKS=true` disables that browser guard for trusted local crawling and isolated load-test fixtures; keep it false for untrusted callers. Deployment network restrictions remain defense in depth.
*   **Lightpanda robots:** Every fetch passes `--obey-robots`; pages denied by robots.txt fail extraction.
*   **Lightpanda bot identity:** Every browser fetch passes `--user-agent`. `LIGHTPANDA_WORKER_USER_AGENT` defaults to `Searchbase (+https://github.com/coolapso/searchbase)` and can identify Cloud or a personal deployment. Empty values use the default; whitespace-only values, control characters, values over 1024 bytes, and Mozilla identities fail at startup without logging the supplied value. These flags do not change the `/extract` JSON shape.
*   **Lightpanda page-loading budget:** `LIGHTPANDA_WORKER_WAIT_MS` defaults to `5000`, matching Lightpanda's upstream `--wait-ms` default. It caps the wait for page loading, not a delay after navigation; the browser can return earlier on load completion or dump an incomplete/empty page when the budget expires. The worker rejects empty Markdown. The previous `500` ms default could fail a slower first navigation while a faster repeat succeeded. Explicit deployment overrides must also be updated. The worker's overall request timeout (default 30 seconds, including concurrency waiting) and the gateway REST fetch timeout (30 seconds) remain separate limits.
*   **Lightpanda observability:** The Go wrapper has optional OpenTelemetry tracing and metrics over OTLP/HTTP, enabled only with `LIGHTPANDA_WORKER_OTEL_ENABLED=true` and an HTTP(S) OTLP base URL in `LIGHTPANDA_WORKER_OTEL_ENDPOINT`. It continues gateway W3C `traceparent` context across `/extract` and an internal `Lightpanda.fetch` span. Fixed-cardinality metrics cover request outcomes, active requests, request duration, and browser-process duration. Never record fetched URLs, Markdown, request bodies, or raw browser errors in spans or metric attributes. This instruments the wrapper, not the Lightpanda binary; cgroup memory including child processes remains an external collector concern. `LIGHTPANDA_DISABLE_TELEMETRY=true` disables Lightpanda's separate upstream telemetry and does not disable this opt-in wrapper instrumentation.
*   **Build workflow:** `lightpanda-worker/Taskfile.yml` follows the search-gateway container build/push tasks and is included in root `container:build`, `container:build:all`, and `container:push` aggregates. The release workflow follows the gateway's Docker metadata/build-push action pattern to publish amd64/arm64 images from the release tag in one job. Its Lightpanda runtime base is the official `1.0.0` release pinned by multi-architecture digest. `task lightpanda-worker:container:update-base` selects the highest numbered non-prerelease release, verifies amd64/arm64 image manifests, and updates version and digest together; it never selects the moving `nightly` tag. `.github/workflows/update-lightpanda-base.yaml` runs that task each morning on the default branch and opens or updates one PR containing only the Containerfile pin when it changes.

### C. Observability & Logging (search-gateway)

#### Structured Logging
Privacy-first structured logging designed to prevent user tracking and correlation.
*   **Implementation:** Gin middleware at `internal/middlewares/ginslogger.go` using Go's standard `log/slog` package.
*   **Configuration:** `SEARCHBASE_LOG_LEVEL` (`error` by default; accepts error/warn/info/debug) sets the gateway logger threshold at startup. Request middleware emits ordinary requests at info and HTTP 5xx at error, so warn/error suppress ordinary request logs. Invalid levels fail startup. Output is JSON; `SEARCHBASE_LOG_FORMAT` is not currently implemented.
*   **Privacy Guarantees:**
  - **No IP Logging:** Raw IP addresses are never logged; only regional data from edge proxies (e.g., Cloudflare's `CF-IPCountry` header).
  - **Minimal Context:** Only essential request metadata (method, path, status code, latency) is captured.
  - **No Fetched-URL Logging:** Fetch failures emit only a generic event; neither the requested URL nor a worker error that could contain it is logged.

#### OpenTelemetry Tracing ⚠️ Experimental
Distributed tracing support for observability and debugging. Not fully tested yet.
*   **Implementation:** `internal/telemetry/otel.go` using Go OTEL SDK with OTLP HTTP exporter.
*   **Configuration:** Environment variables `SEARCHBASE_TRACING_ENABLED` (bool) and `SEARCHBASE_TRACING_ENDPOINT` (string).
*   **Features:** Trace propagation via W3C TraceContext/Baggage, service name attribution.

#### OpenTelemetry Metrics
Gateway metrics are planned but not implemented yet. The optional Lightpanda worker exports its own OTLP/HTTP metrics when explicitly enabled.

### D. Component 2: `crawl-worker` (Python)
The internal heavy-lifter. Completely hidden from the outside world.
*   **Role:** Headless browser, DOM cleaner, Markdown converter.
*   **Tech Stack:** Python, FastAPI, `crawl4ai`.
*   **Responsibilities:**
    *   Expose internal endpoint `POST /extract`.
    *   Fetch URLs concurrently.
    *   Execute JavaScript (if requested via `js_render` flag).
    *   Strip boilerplate (navbars, footers, ads) and extract readable Markdown.
    *   Return the Markdown string to the Go Gateway.

## 3. Data Contracts (Initial Plan)

### Go Gateway External API (REST)
**Endpoint:** `POST /api/v1/search`
**Request:**
```json
{
  "query": "kubernetes 1.30 release notes",
  "engine": "auto",
  "region": "wt-wt",
  "timelimit": "d",
  "safesearch": "moderate",
  "page": 1
}
```
**Response:**
```json
[
  {
    "title": "Kubernetes 1.30: ...",
    "url": "https://kubernetes.io/...",
    "snippet": "Short description from the search engine..."
  }
]
```
**Errors:** `400` for invalid request payload or missing `query`; `502` for upstream provider failures with safe provider-specific messages that do not include search queries, request bodies, tokens, or raw upstream URLs; `500` for unexpected gateway failures.

**Endpoint:** `POST /api/v1/fetch`
**Request:**
```json
{
  "url": "https://kubernetes.io/blog/...",
  "js_render": false
}
```
**Response:**
```json
{
  "title": "",
  "url": "https://kubernetes.io/blog/...",
  "snippet": "",
  "markdown": "# Kubernetes 1.30\n\nThe new release features..."
}
```

### Go Gateway MCP Server Tools
*   **`web_search`**: Searches the web using the configured gateway search provider and returns the top results. Takes `query`, optional `limit`, `engine` (requires `ddgs` or `searxng` provider), `region`, `timelimit`, `safesearch`, and `page` arguments. `limit` is an optional upper bound; omitted or `0` uses the provider or search engine default. Brave maps supported country-language style `region` values such as `us-en` to Brave `country=US` and `search_lang=en`; unsupported country or language parts are omitted. Mojeek maps `limit` to `t`, maps `timelimit` values `d`, `m`, and `y` to `since`, intentionally does not support `w`, and intentionally does not support `page` yet. Provider failures are returned as safe provider-specific messages without exposing search queries, request bodies, tokens, or raw upstream URLs.
*   **`fetch_url`**: Fetches a single URL directly and returns its content as Markdown. Takes `url` and `js_render` arguments.

### Python Worker Internal API
**Endpoint:** `POST /extract`
**Request:**
```json
{
  "url": "https://kubernetes.io/...",
  "js_render": false
}
```

**Response:**
```json
{
  "markdown": "...",
  "success": true,
  "error": ""
}
```

The optional Lightpanda Go worker implements this same request/response contract but ignores `js_render` and always runs JavaScript. Its failures use fixed URL-free categories to avoid exposing requested URLs in gateway error paths. Its Markdown uses Lightpanda's native dump with `--strip-mode clutter`, not Crawl4AI's cleaned output. The pinned 1.0.0 release includes upstream PR #3663, which prevents href-less anchors from causing content removal or broken Markdown links. Navigation and footers may still be removed intentionally.

### Fetch error categories
The Lightpanda wrapper invokes `fetch --json --dump markdown`, parses only `http_status`, `error`, and `content`, and never forwards the browser JSON envelope, URL, headers, or raw errors. The output envelope and decoded Markdown remain bounded. It preserves HTTP 200 with `success:false` for extraction failures and returns a fixed `error` category: `not_found` (404/410), `forbidden` (401/403), `robots_denied` (RobotsBlocked), `rate_limited` (429), `timeout` (408/504, known timeout names, or worker deadline), `unreachable` (known DNS/connect failures, including private-network blocking in 1.0.0), `upstream_error` (other upstream HTTP errors), or `extraction_failed` (unknown errors, invalid JSON, unsuccessful process with no recognized failure, empty/oversized content). Invalid request handling remains HTTP 400. Deadline exhaustion while waiting for concurrency returns `timeout`.
The gateway only accepts this allowlist from workers; arbitrary legacy Crawl4AI messages become `extraction_failed`. REST `/api/v1/fetch` returns the existing `{"error":"category"}` shape with 404, 403, 429, 504, 502, or 500 respectively. MCP `fetch_url` returns an `isError` tool result with `Failed to fetch URL: category` and no protocol error for extraction failures. No fetched URLs or raw worker errors are recorded in gateway failure telemetry.

## 4. Deployment & CI/CD

### Kubernetes Deployment
User-facing demonstration manifests live under `examples/k8s/` (no Helm/Kustomize). They are examples for local evaluation, not production deployment templates. `search-gateway.yaml` defines the gateway Deployment and ClusterIP Service. Choose either `crawl-worker.yaml` or `lightpanda-worker.yaml`, not both: each defines a `crawl-worker` ClusterIP Service so the same gateway manifest can reach the selected backend. The Lightpanda Deployment is named `lightpanda-worker`, uses the optional wrapper image, disables upstream telemetry, and probes `/healthz`. The gateway's worker address assumes the `default` namespace. `ddgs.yaml` is fully commented out and deploys nothing. The gateway Service is internal, so local access requires port forwarding or a separately configured ingress. Keep production concerns such as image pinning, namespace, exposure, resource limits, and network policy in the operator's deployment.

### Docker Compose Examples
`examples/compose/` holds user-facing, demonstration-only stacks for trying the native DuckDuckGo, DDGS, SearXNG, and optional Lightpanda backends; `examples/k8s/` holds the Kubernetes demonstration manifests. Neither set is a production deployment template. `dev/compose/` holds checkout-built development stacks (`docker-compose.lightpanda.yml` and `docker-compose.crawl4ai.yml`) plus the isolated `docker-compose.loadtest.yml` benchmark stack; do not direct users to these as self-hosting examples. There is no root Compose file. Run development stacks through `task dev:up`, `task dev:upcrawl4ai`, and `task dev:down`; they use the `searchbase-dev` project. Compose build contexts and the load-test result bind mount are rooted at the repository by `--project-directory .`, so direct commands must supply that flag from the repository root. Keep the user/developer boundary and paths current in the Hugo docs and concise README.

### Deterministic load and capacity testing
`dev/compose/docker-compose.loadtest.yml` is the only Compose file permitted to run core load tests. It creates a separate `searchbase-loadtest` project with the checkout's gateway and real worker, deterministic local DDGS/extract/static-page fixtures, cAdvisor, node exporter, and an optional generator. Its internal network must remain isolated from the Internet; never target external search engines or arbitrary websites. `task loadtest:run` recreates and tears down only this project, stores privacy-safe `report.json`, `samples.csv`, and `summary.md` under `loadtest/results/`, and must label one-host results `co-located`. Result directory names contain the UTC start time, scenario, profile, and sanitized instance label; older `run-<number>` directories remain valid. Reports use `searchbase-load-report/v1`, omit queries, URLs, bodies, tokens, and responses, and mark incomplete, reset, OOM/restart, or generator-overloaded runs invalid for sizing. Each sample records working-set memory, CPU/quota/throttling, and memory limits for the gateway, worker (including Chromium child processes), fixture, generator, and exporters, plus host memory from node exporter; the summary lists component peaks and CSV surfaces gateway/worker/host memory and CPU. `RATE=<operations-per-second>` explicitly selects a soak rate; derive it from the prior discovery report's `safe_capacity`. When cAdvisor cannot resolve Docker Engine container layers, the target-side fixture collector exposes aggregate Docker stats/inspect metrics at `/docker-metrics`; the Docker socket is mounted only in that trusted test collector, never the generator. A read-only socket mount does not restrict Docker API operations and must never be used in production workloads. Discovery warms up, doubles load, refines the first unstable interval, and cools down; soaks require an explicit rate. Mixed and payload-sweep scenarios execute their declared operation weights/classes, while controlled failures and payload sweeps are non-sizing. Long soaks use bounded latency histograms. Observer resource readings are diagnostic and excluded from application capacity totals. `task loadtest:matrix` runs sequential discovery and fixed-rate midpoint probes across bounded worker CPU/RAM grids (max 8 CPUs and 16 GiB), retaining raw reports and generating `matrix.json`, `capacity.csv`, `summary.md`, and a dependency-free `capacity.svg`. Matrix capacity uses a 70% margin on the highest stable achieved rate; its two-worker horizontal comparison is only an idealized projection, never measured multi-replica throughput. Use a second generator VM for instance sizing; see the Hugo Load Testing page for the exact workflow and exporter host-mount requirements.

Set `LOADTEST_WORKER_CONTEXT=./lightpanda-worker` to run the same isolated fixture against Lightpanda without replacing the default worker. The matrix runner exposes this as `--worker-backend lightpanda` and labels outputs by backend. Direct worker-fetch load tests must treat JSON `success:false` as a failed operation even when HTTP status is 200.
The matrix SVG and summary display the highest sampled worker working set in MiB below 1 GiB, explicitly label five-second sampling limits, and show runs that reach the scenario rate cap as neutral-colored lower bounds rather than measured capacity limits. Replotting saved matrix artifacts must never modify their raw `report.json` files.

The Hugo Crawl4AI/Lightpanda comparison includes `docs/static/images/worker-comparison.svg`, an illustrative graph of the highest stable *achieved* gateway REST JavaScript fetch rates from saved local runs. Those Lightpanda runs used the previous pinned nightly build, so do not present their rates as measured `1.0.0` performance. At 4 worker CPUs/2 GiB, the gateway observed 6.73 successful fetches/s with Crawl4AI and at least 15.46 with Lightpanda; Lightpanda reached the 16/s scenario rate cap, so its maximum was not measured. Both older direct-worker matrices predate the `success:false` failure-counting fix: about 30 HTTP 2xx responses/s for Lightpanda are not verified successful extractions. Neither matrix supports production sizing. Keep local load-test reports out of the repository and link users to the load-testing guide to reproduce results. The previously reported Lightpanda Markdown omission is resolved; track Markdown fidelity and token-efficiency regression testing internally for possible future optimization.

### Documentation Site
The Hugo site under `docs/` separates operating Searchbase from working on its source. `docs/content/docs/` holds user-facing setup, API, MCP, architecture, and self-hosting guidance. `docs/content/development/` holds contributor workflow and isolated load-testing guidance. The sidebar and home page expose both sections. Keep deployment examples under `examples/` and checkout-built stacks under `dev/`, and keep those boundaries clear in the site. The site uses custom local layouts and styling, not an external theme. Keep it updated whenever setup, configuration, provider behavior, MCP usage, deployment notes, observability, or API behavior changes. Use `task docs:hugo` to preview locally and `hugo --destination /tmp/searchbase-docs-build` from `docs/` to verify builds without writing generated output into the repository.

### Contributor License Agreement
All external contributors must accept `CLA.md` before a pull request can be merged. `.github/workflows/cla.yaml` uses CLA Assistant Lite to require a signature comment and stores accepted signatures on the `cla-signatures` branch at `.github/cla/signatures/v1/cla.json`. The `github-actions[bot]` account is allowlisted for generated image-update PRs. Keep `CLA.md`, `CONTRIBUTING.md`, the PR template, the CLA workflow, and the docs Contributing page in sync if the contribution process changes.

### CI/CD Pipeline (GitHub Actions)
The project utilizes GitHub Actions to version, build, and publish Docker images to the GitHub Container Registry (GHCR) using `go-semantic-release`. The release workflow uses Docker actions for build and push; Taskfiles provide the local build/push equivalents.
*   **Trigger:** Manual workflow dispatch. The workflow creates a semantic release and then publishes images from its tag when a new version is emitted.
*   **Images:** 
    *   `ghcr.io/coolapso/searchbase/search-gateway`
    *   `ghcr.io/coolapso/searchbase/crawl-worker`
    *   `ghcr.io/coolapso/searchbase/lightpanda-worker`
    *   `ghcr.io/coolapso/searchbase/ddgs`
*   **Workflow Location:** `.github/workflows/release.yaml`

#### Docker Build Strategy (IMPORTANT)
To ensure reliable and fast multi-architecture builds, the project strictly adheres to the following pattern:
*   **Go Services (e.g., `search-gateway`):** Use standard Docker Buildx cross-compilation on a single `ubuntu-latest` runner (AMD64) targeting `platforms: linux/amd64,linux/arm64`. Go's native cross-compiler handles this extremely efficiently.
*   **Python Services (e.g., `crawl-worker`, `ddgs`):** **DO NOT use QEMU** for cross-compiling Python images. It is painfully slow and often fails when compiling C extensions. Instead, use a **Matrix Strategy** with native runners:
    1.  Spawn one job on `ubuntu-latest` (AMD64).
    2.  Spawn a second job on `ubuntu-24.04-arm` (ARM64).
    3.  Build and push architecture-specific images appended with a flavor tag (e.g., `-amd64`, `-arm64`).
    4.  Use a final aggregation job (Manifest Job) to stitch the `-amd64` and `-arm64` tags together into the final multi-arch tags (`latest`, `v1.2.3`) using `docker buildx imagetools create`.

## 5. Implementation Roadmap

### Phase 1: Core System (Completed)
- [x] **Directory Scaffold:** Setup Go module and Python environment.
- [x] **Worker Implementation:** Build the Python FastAPI wrapper around `crawl4ai`.
- [x] **Search Provider:** Implement the DuckDuckGo HTML scraper in Go.
- [x] **Worker Client:** Implement the internal HTTP client in Go to talk to the Python worker.
- [x] **API Gateway:** Build the REST API in Go.
- [x] **MCP Integration:** Implement MCP over SSE in Go.
- [x] **Kubernetes Manifests:** Write the deployment YAMLs.
- [x] **Testing & Refinement:** E2E testing and Dockerfile optimization.
- [x] **Consolidate Search Request:** Unify search parameters into a single `search.Request` struct.
- [x] **Settings Centralization:** Centralize environment variable parsing and configuration management.

### Phase 2: Enhanced Search & Aggregation (In Progress)
- [x] **DDGS Microservice:** Integrate `ddgs` as an internal Python API for fallback and multi-engine search.
- [x] **Brave Search API Provider:** Implement native Go support for the official Brave Search API. Configure with `SEARCHBASE_SEARCH_PROVIDER=brave` and `SEARCHBASE_BRAVE_API_TOKEN`.
- [x] **Mojeek Search API Provider:** Implement native Go support for the official Mojeek Search API. Configure with `SEARCHBASE_SEARCH_PROVIDER=mojeek` and `SEARCHBASE_MOJEEK_API_KEY`.
- [ ] **Additional Native Cloud Providers:** Implement native Go support for other major search engine APIs when viable (Google Search API, Bing, etc.).
- [ ] **Engine Selection:** Allow clients to select specific search engines via the API.
- [ ] **Result Aggregation:** Support searching from multiple search engines concurrently and aggregating/deduplicating the results.

### Phase 3: Enterprise Features & Observability
- [x] **Structured Logging:** Implemented privacy-focused structured JSON logging via Gin middleware at `internal/middlewares/ginslogger.go`. Logs exclude all personal data (IP addresses, user agents, query parameters) and prevent user correlation.
- [ ] **Authentication:** Secure the Gateway API/MCP. The gateway will act strictly as a validator (verifying API keys/tokens), delegating user management, billing, and key generation to a separate, dedicated Authentication Service.
- [ ] **Deep Crawling:** Extend the `/fetch` endpoint to allow LLMs to follow links and crawl deeper into sites autonomously.
- [ ] **Instrumentation:** Add OpenTelemetry tracing, spans, metrics, and response time tracking.
- [ ] **CLI Tool:** Create a CLI utility (`search` and `get`) for interacting with the API natively from the terminal.
