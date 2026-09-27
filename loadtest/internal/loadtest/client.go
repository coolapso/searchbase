package loadtest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type Targets struct{ Gateway, IsolatedGateway, Worker, Fixture string }
type Client struct {
	http     *http.Client
	targets  Targets
	sequence atomic.Uint64
}

func NewClient(targets Targets) *Client {
	return &Client{http: &http.Client{Timeout: 35 * time.Second}, targets: targets}
}

func (c *Client) Do(ctx context.Context, s Scenario) (string, error) {
	if len(s.Operations) > 0 {
		total := 0
		for _, op := range s.Operations {
			total += op.Weight
		}
		if total < 1 {
			return "invalid", fmt.Errorf("empty operation weights")
		}
		stride := 37
		for gcd(stride, total) != 1 {
			stride++
		}
		index := int(((c.sequence.Add(1) - 1) * uint64(stride)) % uint64(total))
		for _, op := range s.Operations {
			if index < op.Weight {
				s.Kind, s.Target, s.JSRender = op.Kind, op.Target, op.JSRender
				if op.Payload != "" {
					s.Payload = op.Payload
				}
				break
			}
			index -= op.Weight
		}
	}
	base := c.targets.Gateway
	if s.Target == "isolated-gateway" {
		base = c.targets.IsolatedGateway
	}
	if s.Target == "worker" {
		base = c.targets.Worker
	}
	if s.Target == "fixture" {
		base = c.targets.Fixture
	}
	if s.Payload == "" {
		s.Payload = "small"
	}
	query := "load-test-query-" + s.Payload
	switch s.Kind {
	case "rest-search":
		return c.post(ctx, base+"/api/v1/search", map[string]any{"query": query, "limit": 3})
	case "rest-fetch":
		return c.post(ctx, base+"/api/v1/fetch", map[string]any{"url": "http://fixture:8081/site/" + payload(s.Payload, s.JSRender), "js_render": s.JSRender})
	case "worker-fetch":
		return c.post(ctx, base+"/extract", map[string]any{"url": "http://fixture:8081/site/" + payload(s.Payload, s.JSRender), "js_render": s.JSRender})
	case "mcp-http-search":
		return c.mcpHTTP(ctx, base, "web_search", map[string]any{"query": query, "limit": 3})
	case "mcp-http-fetch":
		return c.mcpHTTP(ctx, base, "fetch_url", map[string]any{"url": "http://fixture:8081/site/" + payload(s.Payload, s.JSRender), "js_render": s.JSRender})
	case "mcp-sse-search":
		return c.mcpSSE(ctx, base, "web_search", map[string]any{"query": query, "limit": 3})
	case "mcp-sse-fetch":
		return c.mcpSSE(ctx, base, "fetch_url", map[string]any{"url": "http://fixture:8081/site/" + payload(s.Payload, s.JSRender), "js_render": s.JSRender})
	case "fixture-status":
		return c.post(ctx, base+"/failure/status", map[string]any{})
	case "fixture-invalid-json":
		return c.post(ctx, base+"/failure/invalid-json", map[string]any{})
	case "fixture-timeout":
		short, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		defer cancel()
		return c.post(short, base+"/failure/timeout", map[string]any{})
	case "fixture-disconnect":
		return c.post(ctx, base+"/failure/disconnect", map[string]any{})
	default:
		return "invalid", fmt.Errorf("unknown scenario kind %q", s.Kind)
	}
}
func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
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
		if errors.Is(err, context.DeadlineExceeded) {
			return "timeout", err
		}
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "response_read", err
	}
	if !json.Valid(body) {
		return "invalid_json", fmt.Errorf("response was not valid JSON")
	}
	return "", nil
}
func (c *Client) mcpHTTP(ctx context.Context, base, tool string, args map[string]any) (string, error) {
	endpoint := base + "/mcp/http"
	initialize := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "searchbase-loadtest", "version": "1"}}}
	session, body, class, err := c.mcpPost(ctx, endpoint, "", initialize)
	if err != nil {
		return class, err
	}
	if session == "" || !validMCPReply(body) {
		return "mcp_initialize", fmt.Errorf("MCP initialization failed")
	}
	_, _, class, err = c.mcpPost(ctx, endpoint, session, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if err != nil {
		return class, err
	}
	_, body, class, err = c.mcpPost(ctx, endpoint, session, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
	if err != nil {
		return class, err
	}
	if !validMCPReply(body) {
		return "mcp_reply", fmt.Errorf("MCP tool failed")
	}
	return "", nil
}

// mcpSSE follows the MCP SSE negotiation: obtain the private message endpoint,
// then initialize and make one tool call on it. It never writes endpoint data
// or tool arguments to reports.
func (c *Client) mcpSSE(ctx context.Context, base, tool string, args map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	resp, scanner, endpoint, err := c.openSSE(ctx, base)
	if err != nil {
		return "sse", err
	}
	defer resp.Body.Close()
	replies := make(chan []byte, 4)
	go func() {
		defer close(replies)
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "data: ") {
				select {
				case replies <- []byte(strings.TrimPrefix(line, "data: ")):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	initialize := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "searchbase-loadtest", "version": "1"}}}
	_, _, class, err := c.mcpPost(ctx, endpoint, "", initialize)
	if err != nil {
		return class, err
	}
	if !awaitMCPReply(ctx, replies) {
		return "mcp_initialize", fmt.Errorf("MCP SSE initialization failed")
	}
	_, _, class, err = c.mcpPost(ctx, endpoint, "", map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if err != nil {
		return class, err
	}
	_, _, class, err = c.mcpPost(ctx, endpoint, "", map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
	if err != nil {
		return class, err
	}
	if !awaitMCPReply(ctx, replies) {
		return "mcp_reply", fmt.Errorf("MCP SSE tool failed")
	}
	return "", nil
}

// OpenIdle opens a negotiated SSE connection. The returned closer belongs to
// the phase and must be closed before teardown to avoid lingering sockets.
func (c *Client) OpenIdle(ctx context.Context, base string) (io.Closer, error) {
	resp, scanner, _, err := c.openSSE(ctx, base)
	if err != nil {
		return nil, err
	}
	stream := &idleStream{body: resp.Body, done: make(chan struct{})}
	go func() {
		for scanner.Scan() {
		}
		close(stream.done)
	}()
	return stream, nil
}

type idleStream struct {
	body io.ReadCloser
	done chan struct{}
}

func (s *idleStream) Close() error { return s.body.Close() }
func (s *idleStream) Alive() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}
func (c *Client) openSSE(ctx context.Context, base string) (*http.Response, *bufio.Scanner, string, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	deadline := time.AfterFunc(5*time.Second, cancel)
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, base+"/mcp/sse", nil)
	if err != nil {
		deadline.Stop()
		cancel()
		return nil, nil, "", err
	}
	// Idle sessions may outlive the ordinary per-request timeout.
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		deadline.Stop()
		cancel()
		return nil, nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		deadline.Stop()
		cancel()
		_ = resp.Body.Close()
		return nil, nil, "", fmt.Errorf("SSE status %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	baseURL, _ := url.Parse(base)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		advertised, err := url.Parse(strings.TrimSpace(strings.TrimPrefix(line, "data: ")))
		if err != nil {
			break
		}
		endpoint := baseURL.ResolveReference(advertised)
		if endpoint.Host != baseURL.Host {
			break
		}
		deadline.Stop()
		resp.Body = &cancelReadCloser{ReadCloser: resp.Body, cancel: cancel}
		return resp, scanner, endpoint.String(), nil
	}
	deadline.Stop()
	cancel()
	_ = resp.Body.Close()
	return nil, nil, "", fmt.Errorf("SSE endpoint was not negotiated")
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelReadCloser) Close() error { c.cancel(); return c.ReadCloser.Close() }
func (c *Client) mcpPost(ctx context.Context, endpoint, session string, payload any) (string, []byte, string, error) {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return "", nil, "request", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-03-26")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, "transport", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", nil, "response_read", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Sprintf("http_%d", resp.StatusCode), fmt.Errorf("MCP status %d", resp.StatusCode)
	}
	return resp.Header.Get("Mcp-Session-Id"), body, "", nil
}
func validMCPReply(body []byte) bool {
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	return json.Unmarshal(body, &reply) == nil && len(reply.Result) > 0 && len(reply.Error) == 0
}
func awaitMCPReply(ctx context.Context, replies <-chan []byte) bool {
	select {
	case body, ok := <-replies:
		return ok && validMCPReply(body)
	case <-ctx.Done():
		return false
	}
}
