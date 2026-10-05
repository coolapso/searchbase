---
title: "Architecture"
weight: 40
---

Searchbase uses a hybrid microservice architecture built for horizontal scalability:

1. **`search-gateway` (Go & Gin):** The front-facing orchestrator. It handles incoming REST and MCP requests, scrapes DuckDuckGo natively (HTML version) for top URLs, calls official provider APIs when configured, and acts as a router for optional search backends.
2. **`crawl-worker` (Python & [Crawl4AI](https://github.com/unclecode/crawl4ai)):** The internal heavy-lifter. A headless browser that navigates to the URLs, executes JavaScript (if requested), bypasses anti-bot measures, and distills the DOM into pristine Markdown.
   **Optional alternative:** `lightpanda-worker` is a Go wrapper around [Lightpanda](https://github.com/lightpanda-io/browser). It exposes the same internal `/extract` JSON contract and returns Lightpanda's native Markdown dump with `--strip-mode clutter` to remove boilerplate. The pinned 1.0.0 release includes the fix for content removal caused by anchors without `href`. It always executes JavaScript, so `js_render` has no effect with this backend. The default Crawl4AI worker remains unchanged.
3. **`ddgs` (Python) - *Optional*:** A lightweight internal microservice utilizing the [duckduckgo_search](https://github.com/deedy5/duckduckgo_search) package. It serves as an optional search backend to provide advanced metasearch capabilities and support for multiple underlying search engines.
4. **`searxng` - *Optional*:** Support for connecting to an external SearXNG instance as a backend provider for metasearch capabilities.
5. **Brave Search API - *Optional*:** Direct Go gateway integration with Brave's official web search API. Requires `SEARCHBASE_SEARCH_PROVIDER=brave` and `SEARCHBASE_BRAVE_API_TOKEN`.
6. **Mojeek Search API - *Optional*:** Direct Go gateway integration with Mojeek's official web search API. Requires `SEARCHBASE_SEARCH_PROVIDER=mojeek` and `SEARCHBASE_MOJEEK_API_KEY`.

---

## 🔒 Privacy-First Logging
Searchbase is built from the ground up with privacy in mind. Its structured logging is designed to avoid user tracking and request correlation:

*   **No IP Logging:** Raw IP addresses are never logged; only regional data (e.g., `CF-IPCountry`) is captured.
*   **No Search Queries:** Search queries and fetched URLs are never logged.
*   **Safe Fetch Failures:** Failed fetches emit a generic failure event without the requested URL or worker error details that could contain it.
*   **Minimal Context:** Only essential request metadata (method, path, status code, latency) is recorded.
*   **Configurable:** Set via `SEARCHBASE_LOG_LEVEL` and `SEARCHBASE_LOG_FORMAT` environment variables.

## 📡 OpenTelemetry

### Tracing

{{< hint warning >}}
**Experimental Feature**
OpenTelemetry tracing support is not fully tested yet and is subject to change.
{{< /hint >}}

Distributed tracing support for observability and debugging.

*   **Features:** Trace propagation via W3C TraceContext/Baggage, service name attribution.
*   **Exporter:** OTLP HTTP exporter (compatible with Jaeger, OTEL Collector, etc.).
*   **Configurable:** Set via `SEARCHBASE_TRACING_ENABLED` and `SEARCHBASE_TRACING_ENDPOINT`.

### Metrics

Gateway OpenTelemetry metrics are planned but not implemented yet. The optional
Lightpanda Go wrapper can export request and browser-process metrics and traces
over OTLP/HTTP; this does not instrument the Lightpanda binary itself.
