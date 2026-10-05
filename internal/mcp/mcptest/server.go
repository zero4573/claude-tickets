// Package mcptest is a fake Atlassian MCP server (Streamable HTTP,
// JSON-RPC) serving Jira data from a fixture, for testing ct sync.
//
// A fixture is JSON:
//
//	{"me": "<accountId>",
//	 "resources": [{"cloudId": ..., "url": ..., "products": [{"id": "jira"}]}],
//	 "open": ["PROJ-1", ...],        // what the open-tickets query returns, in order
//	 "pageSize": 2,                  // search page size (default 100)
//	 "sse": true,                    // answer as an event stream
//	 "issues": {"PROJ-1": {
//	     "fields": {...},             // the issue's fields (full view)
//	     "changelog": {"total": 1, "histories": [...]},
//	     "evidence": {"appliedContentFormat": "markdown", "fields": {"description": ..., "customFields": {...}}},
//	     "comments": {"appliedContentFormat": "markdown", "comments": [...]},
//	     "failGet": true}}}           // getJiraIssue fails for it
//
// Searches: "key in (A,B)" returns those issues, "parent in (E,F)" their
// children (by fields.parent.key, sorted by key), any other JQL the open
// list.
package mcptest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Fixture struct {
	Me        string                     `json:"me"`
	Resources json.RawMessage            `json:"resources"`
	Open      []string                   `json:"open"`
	PageSize  int                        `json:"pageSize"`
	SSE       bool                       `json:"sse"`
	Issues    map[string]json.RawMessage `json:"issues"`
}

type issueData struct {
	Fields    map[string]any  `json:"fields"`
	Changelog json.RawMessage `json:"changelog"`
	Evidence  json.RawMessage `json:"evidence"`
	Comments  json.RawMessage `json:"comments"`
	FailGet   bool            `json:"failGet"`
}

type Server struct {
	*httptest.Server
	mu      sync.Mutex
	fixture Fixture
	// Calls is every tool call: "<tool> <arguments json>"
	Calls []string
}

func LoadFixture(file string) (Fixture, error) {
	var f Fixture
	data, err := os.ReadFile(file)
	if err == nil {
		err = json.Unmarshal(data, &f)
	}
	return f, err
}

func New(f Fixture) *Server {
	s := &Server{fixture: f}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// SetFixture switches the served data (e.g. between two sync runs).
func (s *Server) SetFixture(f Fixture) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fixture = f
}

// Config is an MCP config (JSON) listing this server as name.
func (s *Server) Config(name string) string {
	b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{name: map[string]any{"type": "http", "url": s.URL + "/mcp", "headers": map[string]string{"Authorization": "Bearer test"}}}})
	return string(b)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer test" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Method == "initialize" {
		w.Header().Set("Mcp-Session-Id", "session-1")
	} else if r.Header.Get("Mcp-Session-Id") != "session-1" {
		http.Error(w, "no session", http.StatusBadRequest)
		return
	}
	if len(req.ID) == 0 { // a notification
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result any
	switch req.Method {
	case "initialize":
		result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{"name": "fake-atlassian", "version": "1"}}
	case "tools/call":
		args, _ := json.Marshal(req.Params.Arguments)
		s.Calls = append(s.Calls, req.Params.Name+" "+string(args))
		data, err := s.tool(req.Params.Name, req.Params.Arguments)
		if err != nil {
			result = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": err.Error()}}}
		} else {
			text, _ := json.Marshal(map[string]any{"data": data})
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(text)}}}
		}
	default:
		s.reply(w, map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32601, "message": "no method " + req.Method}})
		return
	}
	s.reply(w, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

func (s *Server) reply(w http.ResponseWriter, msg any) {
	b, _ := json.Marshal(msg)
	if s.fixture.SSE {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

var (
	keyIn    = regexp.MustCompile(`^key in \(([^)]*)\)$`)
	parentIn = regexp.MustCompile(`^parent in \(([^)]*)\)$`)
)

func (s *Server) issue(key string) (issueData, bool) {
	raw, ok := s.fixture.Issues[key]
	if !ok {
		return issueData{}, false
	}
	var d issueData
	if json.Unmarshal(raw, &d) != nil {
		return issueData{}, false
	}
	return d, true
}

func (s *Server) tool(name string, args map[string]any) (any, error) {
	switch name {
	case "getAccessibleAtlassianResources":
		return map[string]any{"resources": s.fixture.Resources}, nil
	case "atlassianUserInfo":
		return map[string]any{"accountId": s.fixture.Me}, nil
	case "searchJiraIssuesUsingJql":
		jql, _ := args["jql"].(string)
		var keys []string
		switch {
		case keyIn.MatchString(jql):
			for _, k := range strings.Split(keyIn.FindStringSubmatch(jql)[1], ",") {
				if _, ok := s.issue(k); ok {
					keys = append(keys, k)
				}
			}
		case parentIn.MatchString(jql):
			parents := strings.Split(parentIn.FindStringSubmatch(jql)[1], ",")
			for k := range s.fixture.Issues {
				d, _ := s.issue(k)
				if p, ok := d.Fields["parent"].(map[string]any); ok {
					for _, e := range parents {
						if p["key"] == e {
							keys = append(keys, k)
						}
					}
				}
			}
			sort.Strings(keys)
		default:
			keys = s.fixture.Open
		}
		size := s.fixture.PageSize
		if size == 0 {
			size = 100
		}
		start := 0
		if t, ok := args["nextPageToken"].(string); ok {
			start, _ = strconv.Atoi(t)
		}
		end := min(start+size, len(keys))
		var issues []any
		for _, k := range keys[start:end] {
			d, _ := s.issue(k)
			issues = append(issues, map[string]any{"key": k, "fields": d.Fields})
		}
		page := map[string]any{"issues": issues, "isLast": end >= len(keys)}
		if end < len(keys) {
			page["nextPageToken"] = strconv.Itoa(end)
		}
		return page, nil
	case "getJiraIssue":
		key, _ := args["issueIdOrKey"].(string)
		d, ok := s.issue(key)
		if !ok || d.FailGet {
			return nil, fmt.Errorf("issue %s does not exist or you do not have permission to see it", key)
		}
		if args["view"] == "evidence" {
			var ev map[string]any
			_ = json.Unmarshal(d.Evidence, &ev)
			if ev == nil {
				ev = map[string]any{"appliedContentFormat": "markdown", "fields": map[string]any{}}
			}
			ev["key"] = key
			return ev, nil
		}
		out := map[string]any{"key": key, "fields": d.Fields}
		if args["expand"] == "changelog" {
			var cl any
			_ = json.Unmarshal(d.Changelog, &cl)
			if cl == nil {
				cl = map[string]any{"total": 0, "histories": []any{}}
			}
			out["changelog"] = cl
		}
		return out, nil
	case "executeRead":
		if args["name"] != "listJiraIssueComments" {
			return nil, fmt.Errorf("unknown read %v", args["name"])
		}
		in, _ := args["inputs"].(map[string]any)
		key, _ := in["issueIdOrKey"].(string)
		d, ok := s.issue(key)
		if !ok {
			return nil, fmt.Errorf("issue %s does not exist", key)
		}
		var cm any
		_ = json.Unmarshal(d.Comments, &cm)
		if cm == nil {
			cm = map[string]any{"appliedContentFormat": "markdown", "comments": []any{}}
		}
		return cm, nil
	}
	return nil, fmt.Errorf("unknown tool %s", name)
}
