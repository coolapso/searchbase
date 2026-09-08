package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/coolapso/searchbase/search-gateway/internal/scraper"
	"github.com/coolapso/searchbase/search-gateway/internal/search"
	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCPServer encapsulates the Model Context Protocol (MCP) server
// and its dependencies for executing tools like web search and url fetching.
type MCPServer struct {
	McpServer      *server.MCPServer
	searchProvider search.SearchProvider
	scraperClient  *scraper.ScraperClient
	logger         *slog.Logger
}

// NewServer initializes a new MCP Server, registers the available tools,
// and binds their respective execution handlers.
func NewServer(searchProvider search.SearchProvider, scraperClient *scraper.ScraperClient, logger *slog.Logger) *MCPServer {
	mcpServer := server.NewMCPServer("BASE", "1.0.0")

	srv := &MCPServer{
		McpServer:      mcpServer,
		searchProvider: searchProvider,
		scraperClient:  scraperClient,
		logger:         logger.With(slog.String("component", "mcp")),
	}

	// Register our Web Search Tool
	webSearchTool := mcp.NewTool("web_search",
		mcp.WithDescription("Searches the live internet using various search engines and returns optimized markdown content from the top results. Useful for finding up-to-date information."),
		mcp.WithString("query", mcp.Required(), mcp.Description("The search query to look for")),
		mcp.WithNumber("limit", mcp.Description("Optional upper bound for returned search results. Omit or set to 0 to use the provider or search engine default.")),
		mcp.WithString("engine", mcp.Description("The search engine to query (e.g., 'google', 'duckduckgo'). Defaults to 'auto' (all engines). Note: Only applies if the server uses the 'ddgs' or 'searxng' provider.")),
		mcp.WithString("region", mcp.Description("The region to search in (e.g., 'wt-wt', 'us-en')")),
		mcp.WithString("timelimit", mcp.Description("Time limit for the search ('d'=day, 'w'=week, 'm'=month, 'y'=year). Leave empty for no limit.")),
		mcp.WithString("safesearch", mcp.Description("Safe search filtering ('on', 'moderate', 'off'). Defaults to 'moderate'")),
		mcp.WithNumber("page", mcp.Description("The page number of results to fetch")),
	)

	fetchTool := mcp.NewTool("fetch_url",
		mcp.WithDescription("Fetches the content of a single URL and extracts optimized markdown. Useful for reading a specific webpage directly."),
		mcp.WithString("url", mcp.Required(), mcp.Description("The full URL of the webpage to fetch")),
		mcp.WithBoolean("js_render", mcp.Description("Whether to use a headless browser to execute JavaScript. Default is false.")),
	)

	// Add the tool execution handlers
	mcpServer.AddTool(webSearchTool, srv.HandleWebSearch)
	mcpServer.AddTool(fetchTool, srv.HandleFetchURL)

	return srv
}

// RegisterRoutes registers the mcp server routes to a gin engine.
// It wraps the MCP Server in an SSE handler and mounts it.
// If baseURL is not empty, it configures the SSE options to use it,
// otherwise it leaves it empty so the server returns relative paths
// for seamless reverse proxy integration.
// When heartbeatEnabled is true, both transports send periodic pings every
// heartbeatInterval to keep otherwise idle connections from being torn down
// by clients with a shorter read timeout.
func (s *MCPServer) RegisterRoutes(r *gin.Engine, baseURL string, heartbeatEnabled bool, heartbeatInterval time.Duration) {
	opts := []server.SSEOption{
		server.WithSSEEndpoint("/mcp/sse"),
		server.WithMessageEndpoint("/mcp/message"),
		server.WithKeepAlive(heartbeatEnabled),
	}

	if baseURL != "" {
		opts = append(opts, server.WithBaseURL(baseURL))
	}

	httpOpts := []server.StreamableHTTPOption{
		server.WithEndpointPath("/mcp/http"),
	}

	// WithKeepAliveInterval implicitly enables keepalive, so it must only be
	// added when the heartbeat is actually enabled. WithHeartbeatInterval is
	// the only heartbeat knob on the streamable HTTP transport, an unset
	// interval means no heartbeat.
	if heartbeatEnabled {
		opts = append(opts, server.WithKeepAliveInterval(heartbeatInterval))
		httpOpts = append(httpOpts, server.WithHeartbeatInterval(heartbeatInterval))
	}

	sseServer := server.NewSSEServer(s.McpServer, opts...)
	r.GET("/mcp/sse", gin.WrapH(sseServer.SSEHandler()))
	r.POST("/mcp/message", gin.WrapH(sseServer.MessageHandler()))

	httpServer := server.NewStreamableHTTPServer(s.McpServer, httpOpts...)
	r.Any("/mcp/http", gin.WrapH(httpServer))
}

// HandleWebSearch handles the "web_search" tool execution.
// It accepts a query and optional search filters.
// It searches the internet and concurrently scrapes the top results,
// returning an aggregated markdown string.
func (s *MCPServer) HandleWebSearch(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var req search.Request
	req.Query = request.GetString("query", "")
	if req.Query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	req.Limit = request.GetInt("limit", 0)
	req.Engine = request.GetString("engine", "auto")
	req.Region = request.GetString("region", "")
	req.TimeLimit = request.GetString("timelimit", "")
	req.SafeSearch = request.GetString("safesearch", "moderate")
	req.Page = request.GetInt("page", 1)

	req.Normalize()

	results, err := s.searchProvider.Search(ctx, req)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Search failed: %v", err)), err
	}

	var sb strings.Builder
	for _, res := range results {
		str := fmt.Sprintf("## %s\n**URL:** %s\n**Snippet:** %s\n---\n\n", res.Title, res.URL, res.Snippet)
		sb.WriteString(str)
	}

	return mcp.NewToolResultText(sb.String()), nil
}

// HandleFetchURL handles the "fetch_url" tool execution.
// It accepts a target url and an optional js_render flag,
// directly scraping the page and returning the optimized markdown.
func (s *MCPServer) HandleFetchURL(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	targetURL := request.GetString("url", "")
	if targetURL == "" {
		return mcp.NewToolResultError("url is required"), nil
	}

	jsRender := request.GetBool("js_render", false)

	markdown, err := s.scraperClient.Extract(ctx, targetURL, jsRender)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to fetch URL: %v", err)), err
	}

	resultText := fmt.Sprintf("## Content from %s\n\n%s", targetURL, markdown)
	return mcp.NewToolResultText(resultText), nil
}
