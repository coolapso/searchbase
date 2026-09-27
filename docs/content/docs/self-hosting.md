---
title: "Self Hosting"
weight: 50
---
# Self Hosting

Searchbase can run with different search backends depending on how much infrastructure and control you want.

## Deployment Options

Searchbase includes Docker Compose and Kubernetes examples for evaluating the
services. Adapt the deployment to your environment before production use.

## Docker Compose Examples

The repository includes user-facing Compose examples under `examples/compose/`
for quickly trying each backend. Kubernetes examples live under
`examples/k8s/`. The checkout-built development and benchmark stacks live
under `dev/compose/`; use those when working on Searchbase itself.

{{< hint warning >}}
The Docker Compose files are demonstration examples only. They are useful for local testing and learning how services connect, but they are not production deployment templates.
{{< /hint >}}

Native DuckDuckGo backend:

```bash
docker compose -f examples/compose/docker-compose.native.yml up
```

DDGS backend:

```bash
docker compose -f examples/compose/docker-compose.ddgs.yml up
```

SearXNG backend:

```bash
docker compose -f examples/compose/docker-compose.searxng.yml up
```

Experimental Lightpanda fetch worker with the native DuckDuckGo search backend:

```bash
docker compose -f examples/compose/docker-compose.lightpanda.yml up --build
```

This example builds the Go `/extract` wrapper on top of a pinned digest of
Lightpanda's official `nightly` container. Only the gateway publishes a host
port; the worker stays on the Compose network. Update the digest deliberately
when you want to test a newer browser build. Lightpanda is AGPL-3.0 licensed; review
its terms before redistribution or deployment. The Go worker uses Lightpanda's
native Markdown dump with its `clutter` stripping heuristic, which may differ
in cleaning and content from Crawl4AI.
Lightpanda executes JavaScript for every fetch; both `js_render: false` and
`js_render: true` have the same behavior. This is an alternative worker, not
a new search provider and not an automatic replacement for existing deployments.

The worker accepts `POST /extract` with `{"url":"https://example.org","js_render":false}`
and returns `{"markdown":"...","success":true,"error":""}`. It also has
`GET /healthz`. Extraction failures return HTTP 200 with `success:false` and a
generic error, matching the gateway's existing worker contract without exposing
the fetched URL. Invalid targets return HTTP 400 before the browser starts:
the worker accepts absolute HTTP(S) URLs without credentials, whitespace,
control characters, backslashes, or malformed ports. It normalizes the host
and removes URL fragments before passing the URL to Lightpanda. This check
does not prevent requests to private or local network addresses; restrict
worker network access separately if untrusted clients can request fetches.
Set `LIGHTPANDA_WORKER_CONCURRENCY` (default `4`),
`LIGHTPANDA_WORKER_TIMEOUT_SECONDS` (default `30`), and
`LIGHTPANDA_WORKER_WAIT_MS` (default `500`) to tune it. Set
`LIGHTPANDA_WORKER_LOG_LEVEL` (`error` by default) for generic, URL-free
request metadata logs. Its container disables
Lightpanda's upstream telemetry and core dumps by default; retain those
settings if rebuilding. A fetch starts a Lightpanda process, so measure its
process-start overhead and Markdown quality against your real pages.

### Lightpanda worker telemetry

The Go wrapper can export OpenTelemetry traces and metrics to an OTLP/HTTP
collector. It is off by default and does not expose a Prometheus `/metrics`
endpoint. Set both variables on the worker (the endpoint must be reachable
*from its container*):

```bash
LIGHTPANDA_WORKER_OTEL_ENABLED=true
LIGHTPANDA_WORKER_OTEL_ENDPOINT=http://otel-collector:4318
```

The endpoint is an HTTP(S) base URL; the worker appends `/v1/traces` and
`/v1/metrics`. The Compose example passes these variables through from the
host but does not create a collector. Configure gateway tracing separately
with `SEARCHBASE_TRACING_ENABLED=true` and its collector endpoint to see a
single trace across gateway and worker. The worker accepts W3C `traceparent`
and creates a server span for `/extract` plus a child span around the browser
process. It exports completed request counts by fixed outcome (`success`,
`invalid_request`, `busy_or_timeout`, `extraction_error`), active requests,
request-duration histograms, and browser-duration histograms. The metric
export interval is 15 seconds, with a final flush on graceful shutdown.

The spans and metric attributes omit requested URLs, content, and raw browser
errors. This instruments the Go wrapper, **not** the Lightpanda executable.
It does not measure browser child-process memory; use cgroup/container
monitoring for total worker memory, including children. The image's
`LIGHTPANDA_DISABLE_TELEMETRY=true` controls Lightpanda's separate upstream
telemetry and remains set even when wrapper OpenTelemetry is enabled.

From the repository root, the worker's Taskfile is available through the
`lightpanda-worker:` namespace:

```bash
task lightpanda-worker:test
task lightpanda-worker:build
task lightpanda-worker:container:build
task lightpanda-worker:container:build:all
```

These build and push tasks follow the same pattern as `search-gateway`.
`container:build:all` builds for amd64 and arm64 without publishing.
`task lightpanda-worker:container:push` requires GHCR authentication and
publishes `latest` and the current Git-derived version to
`ghcr.io/coolapso/searchbase/lightpanda-worker`. The root `container:build`,
`container:build:all`, and `container:push` aggregates include Lightpanda.
The manually dispatched release workflow follows the gateway's Docker
metadata/build-push pattern, publishing `latest`, full-version, and major.minor
tags from the semantic-release tag for both architectures.

For development from a checkout, use the stacks under `dev/compose/`. They
build local source and may start more services than a typical deployment:

```bash
task dev:up             # Lightpanda worker
task dev:upcrawl4ai     # Crawl4AI worker
task dev:down           # Stop the searchbase-dev project
```

The tasks run from the repository root and supply `--project-directory .` so
Compose resolves build paths against the checkout. If invoking Compose
directly, use the same option and a file under `dev/compose/`. The load-test
stack in that directory has its own `searchbase-loadtest` project; see
[Load testing](/docs/load-testing/). Neither development stack is a production
deployment template.

## Kubernetes

The manifests under `examples/k8s/` are demonstrations, not production
deployment templates. They create one gateway and one Crawl4AI worker, each
with an internal ClusterIP Service. From the repository root, apply the active
manifests to the `default` namespace:

```bash
kubectl apply -f examples/k8s/crawl-worker.yaml \
  -f examples/k8s/search-gateway.yaml
kubectl port-forward svc/search-gateway 8080:8080
```

The gateway manifest points to `crawl-worker.default.svc.cluster.local` and
uses the default native search provider. `examples/k8s/ddgs.yaml` is fully
commented and creates no resources. Port forwarding provides local test access
because the gateway Service is not externally exposed. For production
self-hosting, configure your namespace and service addresses, pinned image
versions, external access, secrets, resource limits, and network policy for
your environment.

## Configuration

The `search-gateway` can be configured using the following environment variables:

| Variable | Default | Description |
| :--- | :--- | :--- |
| `SEARCHBASE_PORT` | `8080` | The port the gateway listens on. |
| `SEARCHBASE_CRAWL_WORKER_ADDRESS` | `http://localhost:8000` | The internal URL of the Crawl4AI worker or optional Lightpanda-compatible worker. |
| `SEARCHBASE_SEARCH_PROVIDER` | `searchbase_ddg` | The default search provider to use (`searchbase_ddg`, `ddgs`, `searxng`, `brave`, or `mojeek`). |
| `SEARCHBASE_DDGS_PROVIDER_ADDRESS` | `http://localhost:8001` | Optional. The URL to the DDGS engine, if `ddgs` provider is used. |
| `SEARCHBASE_SEARXNG_PROVIDER_ADDRESS` | *(empty)* | Optional. The URL to the SearXNG instance, if `searxng` provider is used (e.g. `http://localhost:8080`). |
| `SEARCHBASE_BRAVE_API_TOKEN` | *(empty)* | Required when `SEARCHBASE_SEARCH_PROVIDER=brave`. Brave Search API subscription token. |
| `SEARCHBASE_MOJEEK_API_KEY` | *(empty)* | Required when `SEARCHBASE_SEARCH_PROVIDER=mojeek`. Mojeek Search API key. |
| `SEARCHBASE_ADDRESS` | *(empty)* | Optional. Used to override the base URL sent to MCP clients for SSE connections. **Reverse Proxy Note:** If deploying Searchbase behind a reverse proxy (like Traefik, Nginx, or Cloudflare Tunnels), leave this blank. The server will use relative paths, allowing the proxy to handle domain routing seamlessly. Set this only if you need to force a specific absolute URL. |
| `SEARCHBASE_LOG_LEVEL` | `error` | Log level for structured logging (`debug`, `info`, `warn`, `error`). |
| `SEARCHBASE_LOG_FORMAT` | `json` | Log format (`json` or `text`). |
| `SEARCHBASE_MCP_HEARTBEAT_ENABLED` | `false` | Send periodic MCP heartbeat pings on the SSE and Streamable HTTP transports. Off unless explicitly set to `true`, so existing deployments keep the current behavior. Enable it when MCP clients drop idle connections, for example the reference MCP SSE client, which closes the stream after its default 300s read timeout. |
| `SEARCHBASE_MCP_HEARTBEAT_INTERVAL` | `60` | Heartbeat interval in seconds. Only used when `SEARCHBASE_MCP_HEARTBEAT_ENABLED=true`. Minimum accepted value is `15`; a lower value fails startup instead of hammering the gateway and its host with excessively frequent pings. |
| `SEARCHBASE_TRACING_ENABLED` | `false` | Enable OpenTelemetry distributed tracing. (Experimental) |
| `SEARCHBASE_TRACING_ENDPOINT` | *(empty)* | Endpoint for the tracing backend. (Experimental) |

{{< hint warning >}}
**Experimental Feature**
OpenTelemetry tracing (`SEARCHBASE_TRACING_ENABLED` and `SEARCHBASE_TRACING_ENDPOINT`) is currently in development and not fully tested for production environments.
{{< /hint >}}

## Search Backend Options

Searchbase initializes one search provider at startup through `SEARCHBASE_SEARCH_PROVIDER`.

| Backend | Complexity | Best For | Notes |
| :--- | :--- | :--- | :--- |
| `searchbase_ddg` | Easiest | Simple self-hosting | Native Go scraper against DuckDuckGo HTML results. No extra service needed. |
| `ddgs` | Moderate | Lightweight metasearch | Runs a small DDGS backend service and supports multiple engines. |
| `searxng` | Advanced | Full private metasearch | Requires external SearXNG instance. JSON search output must be enabled. |
| `brave` | Easy | Official independent search API | Uses Brave Search API directly from the Go gateway. Requires `SEARCHBASE_BRAVE_API_TOKEN`. |
| `mojeek` | Easy | Official independent search API | Uses Mojeek Search API directly from the Go gateway. Requires `SEARCHBASE_MOJEEK_API_KEY`. |

## Native DuckDuckGo Backend

`searchbase_ddg` is the default and easiest backend.

```bash
SEARCHBASE_SEARCH_PROVIDER=searchbase_ddg
```

This backend is implemented directly in the Go gateway. It posts to DuckDuckGo's HTML endpoint, parses organic results, and returns compact search metadata.

Use this when you want the simplest possible self-hosted setup with no additional backend service.

## DDGS Backend

`ddgs` is a lightweight metasearch backend based on [`deedy5/ddgs`](https://github.com/deedy5/ddgs).

```bash
SEARCHBASE_SEARCH_PROVIDER=ddgs
SEARCHBASE_DDGS_PROVIDER_ADDRESS=http://ddgs:8001
```

DDGS supports multiple search engines through a small Python service. It is a good middle ground when you want metasearch behavior without operating a full SearXNG instance.

The `engine` request field applies when using this backend. Examples include `auto`, `google`, `duckduckgo`, `bing`, `brave`, and other DDGS-supported backends.

## SearXNG Backend

`searxng` connects Searchbase to an external SearXNG instance.

```bash
SEARCHBASE_SEARCH_PROVIDER=searxng
SEARCHBASE_SEARXNG_PROVIDER_ADDRESS=http://searxng:8080
```

SearXNG is the most complete option if you want a full private metasearch engine with its own engine configuration, rate limits, proxy setup, and privacy controls.

Refer to the official [SearXNG documentation](https://docs.searxng.org/) for details on how to host, configure, and operate a SearXNG instance.

{{< hint warning >}}
SearXNG must have JSON search output enabled. Searchbase calls `/search?format=json`, so instances that disable JSON responses will not work as a backend.
{{< /hint >}}

The `engine` request field applies when using this backend and maps to SearXNG's `engines` query parameter.

## Brave Search API Backend

`brave` connects Searchbase directly to the official [Brave Search API](https://brave.com/search/api/).

```bash
SEARCHBASE_SEARCH_PROVIDER=brave
SEARCHBASE_BRAVE_API_TOKEN=your_brave_search_api_token
```

This backend returns Brave web results as compact Searchbase results using each result's `title`, `url`, and `description`. Searchbase requests only web results from Brave and disables text decorations so snippets are easier for agents to consume.

The `limit` request field maps to Brave's `count` parameter and is capped at Brave's maximum of 20. Omit `limit` or set it to `0` to use Brave's default result count.

The `timelimit` request field maps to Brave freshness filters: `d` to `pd`, `w` to `pw`, `m` to `pm`, and `y` to `py`.

The `region` request field can provide Brave country and language hints. Searchbase maps supported country-language style values such as `us-en` to Brave `country=US` and `search_lang=en`; unsupported country or language parts are omitted. Brave also supports `ALL` as a country value.

## Mojeek Search API Backend

`mojeek` connects Searchbase directly to the official [Mojeek Search API](https://www.mojeek.com/services/search/web-search-api/).

```bash
SEARCHBASE_SEARCH_PROVIDER=mojeek
SEARCHBASE_MOJEEK_API_KEY=your_mojeek_search_api_key
```

This backend returns Mojeek web results as compact Searchbase results using each result's `title`, `url`, and `desc`.

The `limit` request field maps to Mojeek's `t` parameter. Omit `limit` or set it to `0` to use Mojeek's default result count.

## Choosing a Backend

Start with `searchbase_ddg` if you only need a quick, low-maintenance deployment.

Move to `ddgs` if you want lightweight metasearch without managing a full private search engine.

Use `searxng` if you already run SearXNG or want full control over metasearch engines, privacy settings, and routing.

Use `brave` if you want a cloud-safe official search API backed by Brave's independent index.

Use `mojeek` if you want a cloud-safe official search API backed by Mojeek's independent index.

## Production Notes

Before exposing Searchbase publicly, plan for:

- TLS and ingress configuration.
- API authentication once available.
- network isolation between public gateway and internal workers.
- secrets management for any future paid provider credentials.
- resource limits and worker scaling.
- structured logs and optional tracing.
- provider rate limits and acceptable use.
