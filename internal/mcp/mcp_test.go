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

package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fake struct{ last string }

func (f *fake) Search(text, category string) (string, error) {
	f.last = "search:" + text + "/" + category
	return "hit: skill/x", nil
}
func (f *fake) ListActive() (string, error)    { return "rule  a:rule/x [always]", nil }
func (f *fake) Why(ref string) (string, error) { return ref + " ENABLED - template", nil }
func (f *fake) Content(ref, file string) (string, error) {
	return "content of " + ref + " " + file, nil
}
func (f *fake) PolicyStatus() (string, error) { return "strict: true", nil }

type echoFail struct{ fake }

func (e *echoFail) Search(text, category string) (string, error) {
	return "", errors.New("policy says no")
}

func lines(t *testing.T, ts Toolset, in string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := Serve(strings.NewReader(in), &out, "test", ts); err != nil {
		t.Fatal(err)
	}
	var res []map[string]any
	for _, ln := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("bad response line %q: %v", ln, err)
		}
		res = append(res, m)
	}
	return res
}

func TestInitializeAndToolsList(t *testing.T) {
	res := lines(t, &fake{}, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
`)
	if len(res) != 2 {
		t.Fatalf("notifications must not be answered, got %d responses", len(res))
	}
	init := res[0]["result"].(map[string]any)
	if init["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocol = %v", init["protocolVersion"])
	}
	if init["serverInfo"].(map[string]any)["version"] != "test" {
		t.Errorf("serverInfo = %v", init["serverInfo"])
	}
	tl := res[1]["result"].(map[string]any)["tools"].([]any)
	if len(tl) != 5 {
		t.Fatalf("want 5 tools, got %d", len(tl))
	}
	for _, tool := range tl {
		if tool.(map[string]any)["name"] == "component_content" {
			sch := tool.(map[string]any)["inputSchema"].(map[string]any)
			if sch["type"] != "object" {
				t.Errorf("schema type = %v", sch["type"])
			}
		}
	}
}

func TestToolCallDispatch(t *testing.T) {
	f := &fake{}
	res := lines(t, f, `{"jsonrpc":"2.0","id":"a","method":"tools/call","params":{"name":"search","arguments":{"text":"x","category":"skill"}}}
{"jsonrpc":"2.0","id":"b","method":"tools/call","params":{"name":"component_content","arguments":{"ref":"s:rule/a","file":"f.md"}}}
{"jsonrpc":"2.0","id":"c","method":"tools/call","params":{"name":"policy_status","arguments":{}}}
`)
	if len(res) != 3 {
		t.Fatalf("got %d responses", len(res))
	}
	if f.last != "search:x/skill" {
		t.Errorf("search args lost: %q", f.last)
	}
	txt := res[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]
	if txt != "hit: skill/x" {
		t.Errorf("content = %v", txt)
	}
	if res[1]["id"] != "b" { // id echoed verbatim
		t.Errorf("id echo = %v", res[1]["id"])
	}
	txt = res[1]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]
	if txt != "content of s:rule/a f.md" {
		t.Errorf("content = %v", txt)
	}
	if res[2]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"] != "strict: true" {
		t.Errorf("policy text wrong")
	}
}

func TestToolErrorIsResultNotProtocolError(t *testing.T) {
	res := lines(t, &echoFail{}, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"text":"q"}}}`)
	r := res[0]["result"].(map[string]any)
	if r["isError"] != true {
		t.Errorf("want isError result, got %v", res[0])
	}
	if r["content"].([]any)[0].(map[string]any)["text"] != "policy says no" {
		t.Error("tool error text not surfaced")
	}
	if res[0]["error"] != nil {
		t.Error("must not be a protocol-level error")
	}
}

func TestProtocolErrors(t *testing.T) {
	res := lines(t, &fake{}, `{"jsonrpc":"2.0","id":1,"method":"bogus"}
{"not json"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ghost"}}
`)
	if res[0]["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Errorf("unknown method = %v", res[0])
	}
	if res[1]["error"].(map[string]any)["code"].(float64) != -32700 {
		t.Errorf("parse error = %v", res[1])
	}
	if res[2]["error"].(map[string]any)["code"].(float64) != -32602 {
		t.Errorf("unknown tool = %v", res[2])
	}
}
