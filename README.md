<div align="center">
  <img src="base-logo.png" alt="Searchbase Logo" width="250"/>
  <h1>Searchbase</h1>
</div>

**SEARCHBASE** is an open-source, self-hostable, privacy-focused **web search API for AI agents**. It returns compact search results (title, URL, and snippet) through REST and the **Model Context Protocol (MCP)**, and can fetch a single page as Markdown when your agent needs to read it.

It is provider-flexible: you choose the search provider, independently of your model. Searchbase works alongside frontier and local models rather than replacing them, so your model provider does not also control your agent's way into the web. MCP support makes it a plug-and-play search tool for clients like Claude Desktop, Cursor, OpenWebUI, Opencode, and Neovim CodeCompanion.

---

## 🎯 The Mission

In an era where Big AI companies profit from your data and gatekeep intelligence behind expensive token economies, privacy and autonomy have become luxury commodities. While small, local LLMs have the potential to be incredibly powerful, they are currently tethered to proprietary search APIs that demand high subscriptions and offer zero transparency regarding what happens to your queries or agent interactions.

**Searchbase is built to break this cycle.**

Searchbase mission is to provide a sovereign, privacy-first search layer that enables any LLM, regardless of size, to access the live web with zero dependency on corporate gatekeepers. Searchbase is built for independence and privacy, not for the lowest token count: it aims to return compact, readable results, but token use depends on the client, the model, and the pages involved.

This repository is my independent open-source project. It will remain separate from any managed cloud service I may build around it.

---

### 📚 **[Read the Full Documentation](https://docs.searchbase.md)**

All documentation is available on the dedicated documentation site. Please visit [docs.searchbase.md](https://docs.searchbase.md) for:
- Installation & Getting Started
- MCP Integration Guides (OpenWebUI, Claude, Cursor, Opencode, Neovim CodeCompanion)
- REST API Reference
- Configuration & Architecture
- Self-hosting and fetch-worker comparison
- Development, contribution, and isolated load-testing instructions
- Contributing and CLA information

---

## 🎯 Quick Start

### Docker Compose
```bash
git clone https://github.com/coolapso/searchbase.git
cd searchbase
docker compose -f examples/compose/docker-compose.native.yml up
```
The Gateway will be available at `http://localhost:8080`.

`examples/compose/` and `examples/k8s/` contain user-facing demos; the Kubernetes examples offer Crawl4AI or experimental Lightpanda as the fetch worker. `dev/compose/` contains checkout-built development and load-test stacks. These examples are not production deployment templates. See the [Documentation Site](https://docs.searchbase.md) for backend-specific examples and self-hosting notes.

An experimental Go-wrapped Lightpanda fetch worker is available through
`examples/compose/docker-compose.lightpanda.yml`; the default worker remains
Crawl4AI. See the [self-hosting documentation](https://docs.searchbase.md/docs/self-hosting/)
for deployment behavior and configuration.

### Fetch worker comparison

**Choose the worker that best fits your deployment.** In a
saved local JavaScript fixture run with the previous pinned nightly (4 worker
CPUs, 2 GiB limit), the gateway
completed at least 15.46 fetches/s with Lightpanda at the test ceiling, versus 6.73
fetches/s with Crawl4AI. This is a one-host result, not production capacity;
the `1.0.0` release has not been benchmarked here. Crawl4AI remains the
default and supports optional JavaScript through `js_render`; Lightpanda always
runs JavaScript. See the
[comparison](https://docs.searchbase.md/docs/worker-comparison/).

## Development

The [development documentation](https://docs.searchbase.md/development/) covers
checkout-built stacks, contributions, and isolated load testing. The load-test
stack uses local deterministic fixtures and saves reports under
`loadtest/results/`.

## ⚖️ License

Searchbase is dual-licensed under the **GNU AGPLv3** and a **Commercial License**.

Please see the [Documentation Site](https://docs.searchbase.md) for full details on the business model and license terms.

If you like this project and want to support / contribute in a different way you can always [:heart: Sponsor Me](https://github.com/sponsors/coolapso) or

<a href="https://www.buymeacoffee.com/coolapso" target="_blank">
  <img src="https://cdn.buymeacoffee.com/buttons/default-yellow.png" alt="Buy Me A Coffee" style="height: 51px !important;width: 217px !important;" />
</a>
