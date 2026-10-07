---
title: "Searchbase"
type: "docs"
---
# Searchbase

**SEARCHBASE** is an open-source, self-hostable, privacy-focused **web search API for AI agents**. It returns compact search results (title, URL, and snippet) through REST and the **Model Context Protocol (MCP)**, and can fetch a single page as Markdown when your agent needs to read it.

It is provider-flexible: you choose the search provider, independently of your model. Searchbase works alongside frontier and local models rather than replacing them, so your model provider does not also control your agent's way into the web. MCP support makes it a plug-and-play search tool for clients like Claude Desktop, Cursor, OpenWebUI, Opencode, and Neovim CodeCompanion.

## 🎯 The Mission

In an era where Big AI companies profit from your data and gatekeep intelligence behind expensive token economies, privacy and autonomy have become luxury commodities. While small, local LLMs have the potential to be incredibly powerful, they are currently tethered to proprietary search APIs that demand high subscriptions and offer zero transparency regarding what happens to your queries or agent interactions.

**The status quo is built on dependency:**
* **Expensive Gatekeepers:** Services like Tavily, Bing, and Google Search API force developers into tiered subscription models, creating a "pay-to-play" barrier for innovation.
* **Privacy Erosion:** Using proprietary middleware means your agents' research paths—and the data they uncover—are being fed into corporate training loops.
* **Coupled Stacks:** When the model provider also runs the web tools, your choice of model decides how your agents see the web.

**Searchbase is built to break this cycle.** 

Searchbase mission is to provide a sovereign, privacy-first search layer that enables any LLM—regardless of size—to access the live web with zero dependency on corporate gatekeepers.

This repository is my independent open-source project. It will remain separate from any managed cloud service I may build around it.

**The Searchbase Way:**
* **Search First:** A search API that returns compact results (title, URL, and snippet). Your agent decides which pages, if any, are worth reading.
* **Provider Choice:** Run without paid API keys through the native DuckDuckGo scraper, DDGS, or SearXNG, or connect the official Brave or Mojeek APIs. Change your model without rebuilding your search connection.
* **Fetch When Needed:** `fetch_url` and `/api/v1/fetch` read one page and return its content as Markdown, with boilerplate removed where the worker can detect it.
* **Native Integration:** With out-of-the-box **MCP** support, your AI tools can search the web with zero glue code.
* **Architectural Transparency:** An open-source REST API that provides a foundation for building autonomous agents.

### A Note on Token Use

Searchbase aims to return compact, readable results, but it makes no token-saving guarantee. Token use depends on the client, the model, the provider, and the pages involved. Some agent harnesses do extra work before the model sees a page, such as summarizing fetched content with a smaller model, and can use fewer tokens than any external tool. If the lowest token count matters most, compare Searchbase with your client's built-in tools. Searchbase is built for independence and privacy, not for the lowest token count.

## ⚖️ License & Business Model

Searchbase is dual-licensed under the **GNU AGPLv3** and a **Commercial License**.

### My Promise: The Core Stays Open
I want to be completely transparent about my business intentions from day one. Let's face it: open-source is cool, but open-source doesn't pay the bills. To make this project sustainable, I am building and offering a managed, hosted Cloud version of Searchbase (bootstrapped, operated, and fully controlled by me).

However, **the core project will always remain free and open-source.** Regardless of the commercial cloud offering, the companies I form, or who handles the business licenses in the future, this repository is my strict commitment to the community. You will always be able to self-host the core of Searchbase for free.

### Why AGPLv3? (The "Enterprise Repellent")
I specifically chose the AGPLv3 license instead of a permissive license like MIT or Apache 2.0 to protect this open-source and independence vision. 

My goal is to keep Searchbase **100% free for homelabbers, individuals, and the open-source community**. What I *do not* want is large corporations or massive SaaS platforms taking my hard work, packaging it into a paid product, and offering it as a service without contributing back or purchasing a commercial license.

The AGPLv3 ensures that if a company modifies or uses Searchbase as part of a network service, they must open-source their entire application. 

### Dual License Options
1. **Open Source (AGPLv3):** Free for personal use, homelabs, and open-source projects. If you use it to provide a service over a network, you must comply with the AGPLv3 terms.
2. **Commercial License:** If you wish to use Searchbase in a commercial, proprietary SaaS or enterprise environment without the obligations of the AGPLv3, please contact me to purchase a commercial license.
