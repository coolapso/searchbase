---
title: "API Reference"
weight: 30
---

You can also build your own scripts or use LangChain / LlamaIndex by hitting the standard REST API.

**Note:** A fully interactive Swagger UI is available at `http://localhost:8080/docs` to test and explore the API.

## Web Search
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

**Additional Optional Parameters:**
- `limit`: Optional upper bound for returned search results. Omit it or set `0` to use the provider or search engine default. Some providers cap this value or apply it after receiving results.
- `engine`: The search engine to query (e.g., `"auto"`, `"google"`, `"duckduckgo"`). Defaults to `"auto"` (searches all engines and deduplicates). **Note:** Only applies if the server is configured to use the `ddgs` or `searxng` provider.
- `region`: Region or locale hint (e.g., `"wt-wt"`, `"us-en"`). Provider support differs. Brave maps supported country-language style values such as `"us-en"` to Brave `country=US` and `search_lang=en`; unsupported country or language parts are omitted.
- `timelimit`: Time limit for the search (`"d"`=day, `"w"`=week, `"m"`=month, `"y"`=year). Leave empty for no limit.
- `safesearch`: Safe search filtering (`"on"`, `"moderate"`, `"off"`). Defaults to `"moderate"`.
- `page`: The page number of results to fetch. Defaults to `1`.

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

**Errors:**
- `400`: Invalid request payload or missing `query`.
- `502`: Configured search provider request failed, returned a non-success status, or returned an invalid response. Provider errors are safe public messages that do not include search queries, request bodies, tokens, or raw upstream URLs.
- `500`: Unexpected gateway failure.

## Fetch URL
**Endpoint:** `POST /api/v1/fetch`

**Request:**
```json
{
  "url": "https://kubernetes.io/blog/...",
  "js_render": false
}
```
*(With the default Crawl4AI worker, set `js_render: true` when JavaScript is needed. The optional Lightpanda worker always executes JavaScript and ignores this field.)*

**Response:**
```json
{
  "title": "",
  "url": "https://kubernetes.io/blog/...",
  "snippet": "",
  "markdown": "## Kubernetes 1.30\n\nThe new release features..."
}
```
