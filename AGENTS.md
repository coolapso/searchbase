# SEARCHBASE (Bot Accessible Search Engine) - Architecture & Specifications

**⚠️ CORE MANDATE FOR AI AGENTS:** 
This file (`AGENTS.md`) is the single source of truth for the project's architecture, design decisions, and data contracts. **It must always be updated** whenever relevant architectural changes, new endpoints, or structural modifications are made during the development process. Additionally, you must **always update the Hugo documentation site under `docs/`** with relevant user-facing information (setup instructions, provider behavior, MCP client setup, configuration, deployment notes, API behavior, observability, etc.) and keep `README.md` as the concise repository landing page. Always consult this file to understand the system context.

## 1. Project Overview
A self-hosted, highly efficient, privacy-focused Search Engine designed explicitly for AI Agents and LLMs. It bypasses the need for paid search APIs (like Tavily or Bing) and avoids the rate-limiting and formatting issues of standard SearXNG instances.

It provides clean, token-optimized Markdown ready for LLM context windows, and natively supports the **Model Context Protocol (MCP)** for plug-and-play integration with modern LLM UIs (OpenWebUI, Claude Desktop, Cursor, Opencode, Neovim CodeCompanion, etc.).

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
*   **Role:** Headless browser, DOM cleaner, Markdown optimizer.
*   **Tech Stack:** Python, FastAPI, `crawl4ai`.

### C. Observability & Logging (search-gateway)

#### Structured Logging
Privacy-first structured logging designed to prevent user tracking and correlation.
*   **Implementation:** Gin middleware at `internal/middlewares/ginslogger.go` using Go's standard `log/slog` package.
*   **Configuration:** Environment variables `SEARCHBASE_LOG_LEVEL` (error/warn/info/debug) and `SEARCHBASE_LOG_FORMAT` (json/text).
*   **Privacy Guarantees:**
  - **No IP Logging:** Raw IP addresses are never logged; only regional data from edge proxies (e.g., Cloudflare's `CF-IPCountry` header).
  - **Minimal Context:** Only essential request metadata (method, path, status code, latency) is captured.

#### OpenTelemetry Tracing ⚠️ Experimental
Distributed tracing support for observability and debugging. Not fully tested yet.
*   **Implementation:** `internal/telemetry/otel.go` using Go OTEL SDK with OTLP HTTP exporter.
*   **Configuration:** Environment variables `SEARCHBASE_TRACING_ENABLED` (bool) and `SEARCHBASE_TRACING_ENDPOINT` (string).
*   **Features:** Trace propagation via W3C TraceContext/Baggage, service name attribution.

#### OpenTelemetry Metrics
Metrics are planned but not implemented yet.

### D. Component 2: `crawl-worker` (Python)
The internal heavy-lifter. Completely hidden from the outside world.
*   **Role:** Headless browser, DOM cleaner, Markdown optimizer.
*   **Tech Stack:** Python, FastAPI, `crawl4ai`.
*   **Responsibilities:**
    *   Expose internal endpoint `POST /extract`.
    *   Fetch URLs concurrently.
    *   Execute JavaScript (if requested via `js_render` flag).
    *   Strip boilerplate (navbars, footers, ads) and extract LLM-optimized Markdown.
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
*   **`fetch_url`**: Fetches the content of a single URL directly and extracts optimized markdown. Takes `url` and `js_render` arguments.

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

## 4. Deployment & CI/CD

### Kubernetes Deployment
Standard YAML manifests (no Helm/Kustomize for now to avoid premature complexity).
*   `search-gateway`: Deployment (1 replica), Service (ClusterIP or LoadBalancer/Ingress for external access).
*   `crawl-worker`: Deployment (scalable replicas for heavy lifting), Service (ClusterIP, strictly internal).

### Docker Compose Examples
Compose files are demonstration-only examples for local testing and learning service wiring. They are not production deployment templates. Backend-specific examples live under `examples/compose/` for `searchbase_ddg`, `ddgs`, and `searxng`; the root `docker-compose.yml` is for development and can start more services than a normal deployment requires.

### Documentation Site
User-facing project documentation lives in the Hugo site under `docs/`. The site uses custom local layouts and styling, not an external theme. Keep it updated whenever setup, configuration, provider behavior, MCP usage, deployment notes, observability, or API behavior changes. Use `task docs:hugo` to preview locally and `hugo --destination /tmp/searchbase-docs-build` from `docs/` to verify builds without writing generated output into the repository.

### Contributor License Agreement
All external contributors must accept `CLA.md` before a pull request can be merged. `.github/workflows/cla.yaml` uses CLA Assistant Lite to require a signature comment and stores accepted signatures on the `cla-signatures` branch at `.github/cla/signatures/v1/cla.json`. Keep `CLA.md`, `CONTRIBUTING.md`, the PR template, the CLA workflow, and the docs Contributing page in sync if the contribution process changes.

### CI/CD Pipeline (GitHub Actions)
The project utilizes GitHub Actions to automatically version, build, and publish Docker images to the GitHub Container Registry (GHCR) using `go-semantic-release`. **Taskfiles are used within these workflows to orchestrate the build and push processes.**
*   **Trigger:** Pushes to the `main` branch or manual workflow dispatch.
*   **Images:** 
    *   `ghcr.io/coolapso/searchbase/search-gateway`
    *   `ghcr.io/coolapso/searchbase/crawl-worker`
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
