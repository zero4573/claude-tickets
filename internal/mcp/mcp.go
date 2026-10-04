// Package mcp is a minimal MCP client (Streamable HTTP, JSON-RPC) that
// talks to one server with no model in the loop. Where each server lives
// comes from $CLAUDE_TICKETS_MCP_CONFIG, a standard MCP config
// ({"mcpServers": {"<name>": {"type": "http", "url": ..., "headers": {...}}}});
// the ticket sources name their server ("mcp" in tickets/.sources.json).
// $CLAUDE_TICKETS_MCP_PREPARE, if set, runs once before the first
// connection (e.g. to start a local proxy that holds the credentials).
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Client is a session with one MCP server.
type Client struct {
	url     string
	headers map[string]string
	session string
	nextID  int
	http    *http.Client
}

var prepared bool

// Connect finds server in $CLAUDE_TICKETS_MCP_CONFIG and opens a session.
func Connect(server string) (*Client, error) {
	file := os.Getenv("CLAUDE_TICKETS_MCP_CONFIG")
	if file == "" {
		return nil, fmt.Errorf("MCP: CLAUDE_TICKETS_MCP_CONFIG isn't set (a standard MCP config naming '%s'; see the README)", server)
	}
	var cfg struct {
		MCPServers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	data, err := os.ReadFile(file)
	if err == nil {
		err = json.Unmarshal(data, &cfg)
	}
	entry, ok := cfg.MCPServers[server]
	if err != nil || !ok {
		return nil, fmt.Errorf("MCP: no server '%s' in %s", server, file)
	}
	if entry.URL == "" {
		return nil, fmt.Errorf("MCP: server '%s' has no url (only HTTP servers are supported)", server)
	}
	if prep := os.Getenv("CLAUDE_TICKETS_MCP_PREPARE"); prep != "" && !prepared {
		prepared = true
		cmd := exec.Command("bash", "-c", prep)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, errors.New("MCP: CLAUDE_TICKETS_MCP_PREPARE failed")
		}
	}
	c := &Client{url: entry.URL, headers: entry.Headers, nextID: 1, http: &http.Client{Timeout: 120 * time.Second}}
	if _, err := c.RPC("initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "ct sync", "version": "1"},
	}); err != nil {
		return nil, err
	}
	_, err = c.post(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	return c, err
}

// post sends one JSON-RPC message and returns the JSON-RPC messages of the
// response (a JSON body, or an event stream's data lines).
func (c *Client) post(msg any) ([]json.RawMessage, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MCP: %w", err)
	}
	defer resp.Body.Close()
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.session = sid
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("MCP: %s %s", resp.Status, truncate(string(data), 300))
	}
	var out []json.RawMessage
	if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 1024*1024), 256*1024*1024)
		for sc.Scan() {
			if line, ok := strings.CutPrefix(sc.Text(), "data:"); ok {
				out = append(out, json.RawMessage(strings.TrimLeft(line, " ")))
			}
		}
		return out, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var m json.RawMessage
		if err := dec.Decode(&m); err != nil {
			break
		}
		out = append(out, m)
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// RPC calls method and returns its result.
func (c *Client) RPC(method string, params any) (json.RawMessage, error) {
	id := c.nextID
	c.nextID++
	msgs, err := c.post(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	var reply *struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	for _, m := range msgs {
		var r struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(m, &r) == nil && string(r.ID) == fmt.Sprint(id) {
			reply = &struct {
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}{r.Result, r.Error}
		}
	}
	switch {
	case reply == nil:
		return nil, fmt.Errorf("MCP %s: \"no response\"", method)
	case len(reply.Error) > 0 && string(reply.Error) != "null":
		return nil, fmt.Errorf("MCP %s: %s", method, reply.Error)
	}
	return reply.Result, nil
}

// Tool calls a tool and returns its payload: the JSON its text content
// holds (the Atlassian tools return their data as JSON text).
func (c *Client) Tool(name string, args any) (json.RawMessage, error) {
	res, err := c.RPC("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, fmt.Errorf("MCP tool %s: %w", name, err)
	}
	var text strings.Builder
	for _, c := range r.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	if r.IsError {
		return nil, fmt.Errorf("MCP tool %s failed: %s", name, truncate(text.String(), 500))
	}
	if !json.Valid([]byte(text.String())) {
		return nil, fmt.Errorf("MCP tool %s: not JSON: %s", name, truncate(text.String(), 300))
	}
	return json.RawMessage(text.String()), nil
}
