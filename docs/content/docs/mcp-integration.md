---
title: "MCP Integration"
weight: 20
---

If your LLM client supports MCP (like Claude Desktop, Cursor, LM Studio, OpenWebUI, Opencode, or Neovim CodeCompanion), you can plug Searchbase directly into it using one of the supported transports.

- **MCP SSE Endpoint:** `http://localhost:8080/mcp/sse`
- **MCP Streamable HTTP Endpoint:** `http://localhost:8080/mcp/http`

## Available Tools

- `web_search`: Search the live web and return compact search results.
- `fetch_url`: Fetch one exact URL and extract optimized Markdown.

## OpenWebUI

OpenWebUI works best with the Streamable HTTP transport.

1. Open OpenWebUI.
2. Click your profile.
3. Go to **Admin Panel** > **Settings** > **Integrations**.
4. Click **+ Add connection**.
5. Set the connection type to **MCP Streamable HTTP**.
6. Set the ID to `searchbase`.
7. Set the target URL to `http://localhost:8080/mcp/http`.
8. Save the connection.

After connection, OpenWebUI should discover the `web_search` and `fetch_url` tools automatically.

{{< hint warning >}}
If OpenWebUI runs inside Docker, `localhost` points to the OpenWebUI container, not your host. Use the Searchbase service name if both services share a Docker network, or use your host/server address instead.
{{< /hint >}}

## LM Studio

LM Studio can connect to Searchbase through the MCP SSE endpoint.

Use this configuration in LM Studio's MCP server settings:

```json
{
  "mcpServers": {
    "searchbase": {
      "transport": "sse",
      "url": "http://localhost:8080/mcp/sse"
    }
  }
}
```

If LM Studio asks for fields instead of raw JSON, use:

| Field | Value |
| :--- | :--- |
| Name | `searchbase` |
| Transport | `sse` |
| URL | `http://localhost:8080/mcp/sse` |

Once enabled, LM Studio can call `web_search` for discovery and `fetch_url` when it needs full page content.

## Opencode

Opencode can connect to Searchbase through the remote MCP SSE endpoint.

Add the Searchbase MCP server to your `opencode.json` or `opencode.jsonc` config:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "searchbase": {
      "type": "remote",
      "url": "http://localhost:8080/mcp/sse",
      "enabled": true
    }
  }
}
```

If you restrict tools in Opencode, enable the Searchbase MCP server tools:

```json
{
  "tools": {
    "searchbase": true
  }
}
```

Replace `localhost` with your Searchbase server address when Opencode runs on a different machine.

## Neovim CodeCompanion

CodeCompanion currently supports stdio MCP servers, but does not support remote SSE or Streamable HTTP MCP servers directly. Because Searchbase exposes remote MCP transports, the simplest CodeCompanion setup is to expose Searchbase as command tools that call the REST API.

Add only the Searchbase tools to your `codecompanion.setup()` config:

```lua
require("codecompanion").setup({
  interactions = {
    chat = {
      tools = {
        opts = {
          default_tools = {
            "searchbase_search",
            "searchbase_fetch",
          },
        },
        ["searchbase_search"] = {
          extends = "cmd_tool",
          description = "Search the web using Searchbase",
          opts = { require_approval_before = true },
          name = "searchbase_search",
          system_prompt = [[You have access to a web search tool called searchbase_search. Use it to search the web for information.]],
          schema = {
            properties = {
              query = {
                type = "string",
                description = "The search query to look up on the web",
              },
            },
            required = { "query" },
          },
          build_cmd = function(args)
            local data = vim.json.encode({ query = args.query, limit = 5 })
            local escaped_data = vim.fn.shellescape(data)
            return "curl -s -X POST http://localhost:8080/api/v1/search -H 'Content-Type: application/json' -d " .. escaped_data
          end,
        },
        ["searchbase_fetch"] = {
          extends = "cmd_tool",
          description = "Fetch optimized web content using Searchbase",
          opts = { require_approval_before = true },
          name = "searchbase_fetch",
          system_prompt = [[You have access to a web fetch tool called searchbase_fetch. Use it to fetch optimized Markdown from a URL.]],
          schema = {
            properties = {
              url = {
                type = "string",
                description = "The URL to fetch",
              },
              js_render = {
                type = "boolean",
                description = "Whether to render website JavaScript",
              },
            },
            required = { "url" },
            optional = { "js_render" },
          },
          build_cmd = function(args)
            if args.js_render == nil then
              args.js_render = false
            end
            local data = vim.json.encode({ url = args.url, js_render = args.js_render })
            local escaped_data = vim.fn.shellescape(data)
            return "curl -s -X POST http://localhost:8080/api/v1/fetch -H 'Content-Type: application/json' -d " .. escaped_data
          end,
        },
      },
    },
  },
})
```

Replace `localhost` with your Searchbase server address when Neovim runs on a different machine. Keep `vim.fn.shellescape()` around the JSON payload so queries and URLs are escaped safely before `curl` runs.

## Claude Desktop / Cursor

Clients that support SSE MCP servers can use the same JSON shape:

```json
{
  "mcpServers": {
    "searchbase": {
      "transport": "sse",
      "url": "http://localhost:8080/mcp/sse"
    }
  }
}
```

Replace `localhost` with the actual server address if Searchbase is not running on the same machine as your MCP client.

## Idle Connection Timeouts

Some MCP clients close a connection when nothing is received for a while. The reference MCP SSE client, for example, defaults `sse_read_timeout` to 300s, so an idle Searchbase session is dropped after roughly five minutes.

Searchbase can send periodic heartbeat pings on both the SSE and Streamable HTTP transports to keep those connections open. The heartbeat is off by default and is enabled explicitly:

```env
SEARCHBASE_MCP_HEARTBEAT_ENABLED=true
SEARCHBASE_MCP_HEARTBEAT_INTERVAL=60
```

`SEARCHBASE_MCP_HEARTBEAT_INTERVAL` is expressed in seconds, defaults to `60`, and is only used when the heartbeat is enabled. The minimum accepted value is `15`; anything lower fails startup. Pick an interval comfortably below your client's read timeout. See [Self Hosting](/docs/self-hosting/) for the full configuration reference.
