package loadtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Targets struct{ Gateway, IsolatedGateway, Worker string }
type Client struct {
	http    *http.Client
	targets Targets
}

func NewClient(targets Targets) *Client {
	return &Client{http: &http.Client{Timeout: 35 * time.Second}, targets: targets}
}

func (c *Client) Do(ctx context.Context, s Scenario) (string, error) {
	base := c.targets.Gateway
	if s.Target == "isolated-gateway" {
		base = c.targets.IsolatedGateway
	}
	if s.Target == "worker" {
		base = c.targets.Worker
	}
	switch s.Kind {
	case "rest-search":
		return c.post(ctx, base+"/api/v1/search", map[string]any{"query": "load-test-query", "limit": 3})
	case "rest-fetch":
		return c.post(ctx, base+"/api/v1/fetch", map[string]any{"url": "http://fixture:8081/site/" + payload(s.Payload, s.JSRender), "js_render": s.JSRender})
	case "worker-fetch":
		return c.post(ctx, base+"/extract", map[string]any{"url": "http://fixture:8081/site/" + payload(s.Payload, s.JSRender), "js_render": s.JSRender})
	case "mcp-http-search":
		return c.mcpHTTP(ctx, base, "web_search", map[string]any{"query": "load-test-query", "limit": 3})
	case "mcp-http-fetch":
		return c.mcpHTTP(ctx, base, "fetch_url", map[string]any{"url": "http://fixture:8081/site/" + payload(s.Payload, s.JSRender), "js_render": s.JSRender})
	case "mcp-sse-search":
		return c.mcpSSE(ctx, base, "web_search", map[string]any{"query": "load-test-query", "limit": 3})
	case "mcp-sse-fetch":
		return c.mcpSSE(ctx, base, "fetch_url", map[string]any{"url": "http://fixture:8081/site/" + payload(s.Payload, s.JSRender), "js_render": s.JSRender})
	default:
		return "invalid", fmt.Errorf("unknown scenario kind %q", s.Kind)
	}
}
func payload(size string, js bool) string {
	if js {
		return size + "-js"
	}
	return size
}
func (c *Client) post(ctx context.Context, url string, payload any) (string, error) {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "request", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "transport", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2<<20))
		return fmt.Sprintf("http_%d", resp.StatusCode), fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if strings.HasSuffix(url, "/extract") {
		var result struct {
			Success bool `json:"success"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result); err != nil || !result.Success {
			return "extract_failure", fmt.Errorf("worker extraction failed")
		}
		return "", nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2<<20))
	return "", nil
}
func (c *Client) mcpHTTP(ctx context.Context, base, tool string, args map[string]any) (string, error) {
	if class, err := c.post(ctx, base+"/mcp/http", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "searchbase-loadtest", "version": "1"}}}); err != nil {
		return class, err
	}
	return c.post(ctx, base+"/mcp/http", map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
}

// mcpSSE follows the MCP SSE negotiation: obtain the private message endpoint,
// then initialize and make one tool call on it. It never writes endpoint data
// or tool arguments to reports.
func (c *Client) mcpSSE(ctx context.Context, base, tool string, args map[string]any) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/mcp/sse", nil)
	if err != nil {
		return "request", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "transport", err
	}
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		return "sse", err
	}
	endpoint := ""
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		if strings.HasPrefix(line, "data: ") {
			endpoint = strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			break
		}
	}
	if endpoint == "" {
		return "sse", fmt.Errorf("SSE endpoint was not negotiated")
	}
	if strings.HasPrefix(endpoint, "/") {
		endpoint = base + endpoint
	}
	if class, err := c.post(ctx, endpoint, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "searchbase-loadtest", "version": "1"}}}); err != nil {
		return class, err
	}
	return c.post(ctx, endpoint, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
}

// OpenIdle opens a negotiated SSE connection. The returned closer belongs to
// the phase and must be closed before teardown to avoid lingering sockets.
func (c *Client) OpenIdle(ctx context.Context, base string) (io.Closer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/mcp/sse", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
