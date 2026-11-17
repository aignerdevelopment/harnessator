// Copyright 2026 Roland Aigner (https://www.aignerdevelopment.com)
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package mcp implements the harnessator MCP server:
// newline-delimited JSON-RPC 2.0 over stdio, speaking protocol version
// 2024-11-05 - the transport every governed harness can spawn (claude,
// opencode, codex).
//
// The server is deliberately TRANSPORT-ONLY: tools live behind a Toolset
// interface wired by the CLI from the core app, keeping "core is the
// product, frontends are views". All tools are READ-ONLY -
// mutations stay in the CLI, where project config and policy enforcement
// have a say. No new dependencies: the stdlib does this.
package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// ProtocolVersion is the MCP revision served (widely supported baseline).
const ProtocolVersion = "2024-11-05"

// Toolset is the read-only query surface the CLI implements over a
// managed project (app.App). Errors become tool-result text (isError),
// never protocol failures.
type Toolset interface {
	Search(text, category string) (string, error)
	ListActive() (string, error)
	Why(ref string) (string, error)
	Content(ref, file string) (string, error)
	PolicyStatus() (string, error)
}

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func objReq(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

var tools = []toolDef{
	{"search", "Search all configured component sources by free text (id, description, tags).",
		objReq(map[string]any{
			"text":     map[string]any{"type": "string"},
			"category": map[string]any{"type": "string", "description": "optional category filter"}}, "text")},
	{"list_active", "List the components enabled for the current project, with enablement reasons.",
		objReq(map[string]any{})},
	{"why", "Provenance of one component: what made it enabled or skipped.",
		objReq(map[string]any{"ref": map[string]any{"type": "string",
			"description": "<source>:<category>/<id> or unambiguous <category>/<id>"}}, "ref")},
	{"component_content", "Read a component's content from the pinned store checkout (single file, or SKILL.md for skill dirs; pass file for other entries).",
		objReq(map[string]any{
			"ref":  map[string]any{"type": "string"},
			"file": map[string]any{"type": "string", "description": "path inside the component, optional"}})},
	{"policy_status", "Report provider profile, effective model policy and governance warnings.",
		objReq(map[string]any{})},
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Serve runs the stdio loop until EOF. One JSON object per line, in and
// out; notifications (no id) never get a response.
func Serve(in io.Reader, out io.Writer, version string, ts Toolset) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r rpcReq
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			if err := enc.Encode(rpcErr(nil, -32700, "parse error: "+err.Error())); err != nil {
				return err
			}
			continue
		}
		resp, ok := dispatch(&r, version, ts)
		if !ok { // notification
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func dispatch(r *rpcReq, version string, ts Toolset) (any, bool) {
	respond := r.ID != nil && string(r.ID) != "null"
	switch r.Method {
	case "initialize":
		if !respond {
			return nil, false
		}
		return rpcResult(r.ID, map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "harnessator", "version": version},
		}), true
	case "notifications/initialized", "notifications/cancelled":
		return nil, false
	case "ping":
		if !respond {
			return nil, false
		}
		return rpcResult(r.ID, map[string]any{}), true
	case "tools/list":
		if !respond {
			return nil, false
		}
		return rpcResult(r.ID, map[string]any{"tools": tools}), true
	case "tools/call":
		if !respond {
			return nil, false
		}
		return callTool(r, ts), true
	default:
		if !respond {
			return nil, false
		}
		return rpcErr(r.ID, -32601, "method not found: "+r.Method), true
	}
}

type callParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func str(a map[string]any, k string) string {
	if s, ok := a[k].(string); ok {
		return s
	}
	return ""
}

func callTool(r *rpcReq, ts Toolset) any {
	var p callParams
	if err := json.Unmarshal(r.Params, &p); err != nil {
		return rpcErr(r.ID, -32602, "invalid params: "+err.Error())
	}
	var (
		text string
		err  error
	)
	switch p.Name {
	case "search":
		text, err = ts.Search(str(p.Arguments, "text"), str(p.Arguments, "category"))
	case "list_active":
		text, err = ts.ListActive()
	case "why":
		text, err = ts.Why(str(p.Arguments, "ref"))
	case "component_content":
		text, err = ts.Content(str(p.Arguments, "ref"), str(p.Arguments, "file"))
	case "policy_status":
		text, err = ts.PolicyStatus()
	default:
		return rpcErr(r.ID, -32602, "unknown tool: "+p.Name)
	}
	if err != nil { // tool-level failure is a result, not a protocol error
		return rpcResult(r.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
			"isError": true,
		})
	}
	return rpcResult(r.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	})
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

func rpcResult(id json.RawMessage, result any) rpcResp {
	return rpcResp{JSONRPC: "2.0", ID: id, Result: result}
}

func rpcErr(id json.RawMessage, code int, msg string) rpcResp {
	return rpcResp{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}
