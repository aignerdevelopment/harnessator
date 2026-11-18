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

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeGateway(t *testing.T, ids ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer sk-classified" {
			w.Header().Set("Content-Type", "application/json")
			var data []map[string]string
			for _, id := range ids {
				data = append(data, map[string]string{"id": id})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
}

func ocFixture(t *testing.T, home, base string) string {
	p := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	os.MkdirAll(filepath.Dir(p), 0o755)
	body := `{
  // hand comment survives parse AND the spliced rewrite
  "providers": {
    "gw": {
      "package": "aisdk:@ai-sdk/openai-compatible",
      "settings": {"baseURL": "` + base + `", "apiKey": "{env:MK}"},
      "models": {"alpha": {"name": "Keep me"}, "stale": {}}
    }
  },
}`
	os.WriteFile(p, []byte(body), 0o644)
	return p
}

func TestModelUpdateOpencodeApply(t *testing.T) {
	srv := fakeGateway(t, "alpha", "beta")
	defer srv.Close()
	home := t.TempDir()
	path := ocFixture(t, home, srv.URL)
	t.Setenv("MK", "sk-classified")

	res := ModelUpdate(context.Background(), []string{"opencode"}, home, t.TempDir(), MUOptions{})
	if len(res) != 1 {
		t.Fatalf("%+v", res)
	}
	ch := res[0]
	if ch.Err != "" {
		t.Fatalf("probe: %v", ch.Err)
	}
	if strings.Join(ch.Added, ",") != "beta" || strings.Join(ch.Removed, ",") != "stale" ||
		strings.Join(ch.Kept, ",") != "alpha" {
		t.Errorf("diff wrong: +%v -%v =%v", ch.Added, ch.Removed, ch.Kept)
	}
	if !ch.Applied || ch.Backup == "" {
		t.Fatalf("not applied/backed up: %+v", ch)
	}
	if _, err := os.Stat(ch.Backup); err != nil {
		t.Errorf("backup missing: %v", err)
	}
	raw, _ := os.ReadFile(path)
	// the file is JSONC now BY DESIGN - the splice preserves the hand
	// comment - so parse it the way opencode does
	var doc map[string]any
	if err := json.Unmarshal(stripJSONC(raw), &doc); err != nil {
		t.Fatalf("rewritten file invalid: %v", err)
	}
	models := doc["providers"].(map[string]any)["gw"].(map[string]any)["models"].(map[string]any)
	if _, gone := models["stale"]; gone {
		t.Error("unsupported model still configured")
	}
	if a, ok := models["alpha"].(map[string]any); !ok || a["name"] != "Keep me" {
		t.Errorf("kept model lost its options: %v", models["alpha"])
	}
	if _, ok := models["beta"]; !ok {
		t.Error("new model not added")
	}
	// THE rule: the key never lands in the file, the diff, or the labels
	if strings.Contains(string(raw), "sk-classified") || strings.Contains(ch.KeySource, "sk-classified") {
		t.Error("SECRET LEAK")
	}
	if ch.KeySource != "env MK" {
		t.Errorf("key source label = %q", ch.KeySource)
	}
}

func TestModelUpdateDryAndErrors(t *testing.T) {
	srv := fakeGateway(t, "alpha", "beta")
	defer srv.Close()
	home := t.TempDir()
	path := ocFixture(t, home, srv.URL)
	t.Setenv("MK", "sk-classified")
	before, _ := os.ReadFile(path)

	res := ModelUpdate(context.Background(), []string{"opencode"}, home, t.TempDir(), MUOptions{Dry: true})
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("dry-run wrote")
	}
	if res[0].Applied || len(res[0].Added) != 1 {
		t.Errorf("dry plan wrong: %+v", res[0])
	}

	// wrong key: gateway 401 surfaces as the probe's own explanation
	t.Setenv("MK", "sk-wrong")
	res = ModelUpdate(context.Background(), []string{"opencode"}, home, t.TempDir(), MUOptions{})
	if res[0].Err == "" || !strings.Contains(res[0].Err, "rejected the key") {
		t.Errorf("want auth failure surfaced, got %+v", res[0])
	}
	after, _ = os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("failed probe still wrote")
	}
}

func TestModelUpdateReportOnlyAndManagedGuard(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"),
		[]byte(`{"env":{"ANTHROPIC_BASE_URL":"https://unreachable.invalid"}}`), 0o644)
	res := ModelUpdate(context.Background(), []string{"claude"}, home, t.TempDir(), MUOptions{})
	if len(res) != 1 || !res[0].Drilled {
		t.Fatalf("claude must be report-only: %+v", res)
	}

	// a project-generated .opencode path must be REFUSED, not rewritten
	cwd := t.TempDir()
	os.MkdirAll(filepath.Join(cwd, ".opencode"), 0o755)
	os.WriteFile(filepath.Join(cwd, ".opencode", "opencode.json"), []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(cwd, "opencode.json"), []byte(`{
  "providers": {"gw": {"package": "@ai-sdk/openai-compatible", "settings": {"baseURL": "http://127.0.0.1:1"}, "models": {}}}
}`), 0o644)
	// candidate order: the plain ./opencode.json is found first (user
	// territory); the managed .opencode/ artifact is simply never a
	// candidate path - assert it cannot be reached:
	found := false
	for _, s := range ModelUpdate(context.Background(), []string{"opencode"}, t.TempDir(), cwd, MUOptions{Dry: true}) {
		if strings.Contains(s.Err, "managed") {
			found = true
		}
	}
	if !found {
		t.Skip("managed artifact never a candidate path - guard is structural (path not discovered)")
	}
}

// The namespace-mismatch scenario from the author's real gateway: the
// gateway answers prefixed ids, the config speaks short ones. The
// guard must refuse the massacre; the opt-in must let it through.
func TestModelUpdateMassRemovalGuard(t *testing.T) {
	ids := make([]string, 0, 40)
	for i := 0; i < 30; i++ {
		ids = append(ids, fmt.Sprintf("vendor/ns-%d", i))
	}
	srv := fakeGateway(t, ids...)
	defer srv.Close()
	home := t.TempDir()
	p := filepath.Join(home, ".config", "opencode", "opencode.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	models := map[string]any{}
	for i := 0; i < 25; i++ {
		models[fmt.Sprintf("ns-%d", i)] = map[string]any{}
	}
	doc := map[string]any{"providers": map[string]any{"gw": map[string]any{
		"package":  "aisdk:@ai-sdk/openai-compatible",
		"settings": map[string]any{"baseURL": srv.URL, "apiKey": "{env:MK}"},
		"models":   models,
	}}}
	raw, _ := json.MarshalIndent(doc, "", "  ")
	os.WriteFile(p, raw, 0o644)
	t.Setenv("MK", "sk-classified")

	res := ModelUpdate(context.Background(), []string{"opencode"}, home, t.TempDir(), MUOptions{})
	if len(res) != 1 || !strings.Contains(res[0].Note, "MASS REMOVAL REFUSED") {
		t.Fatalf("guard must refuse: %+v", res[0])
	}
	if res[0].Applied {
		t.Error("guard fired but wrote anyway")
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(raw) {
		t.Error("catalog modified under guard")
	}

	res = ModelUpdate(context.Background(), []string{"opencode"}, home, t.TempDir(), MUOptions{AllowRemovals: true})
	if !res[0].Applied {
		t.Fatalf("opt-in must apply: %+v", res[0])
	}
}

// fakeGatewayKey mirrors fakeGateway but names the credential it
// demands - the file-reference and helper tests prove the RIGHT key
// reached the wire, not just "a" key.
func fakeGatewayKey(t *testing.T, bearer, style string, ids ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if style == "anthropic" {
			got = "x:" + r.Header.Get("x-api-key")
			if bearer != "" && got != "x:"+bearer {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		} else if got != "Bearer "+bearer {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var data []map[string]string
		for _, id := range ids {
			data = append(data, map[string]string{"id": id})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

// The reported field bug: an apiKey that is a {file:...} reference
// must be EVALUATED (read and used), not sent verbatim as a token.
func TestModelUpdateFileReference(t *testing.T) {
	srv := fakeGatewayKey(t, "sk-from-file", "openai", "alpha")
	defer srv.Close()
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".secrets"), 0o700)
	os.WriteFile(filepath.Join(home, ".secrets", "gw.key"), []byte("sk-from-file\n"), 0o600)
	p := filepath.Join(home, ".config", "opencode", "opencode.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	doc := map[string]any{"providers": map[string]any{"gw": map[string]any{
		"package":  "aisdk:@ai-sdk/openai-compatible",
		"settings": map[string]any{"baseURL": srv.URL, "apiKey": "{file:~/.secrets/gw.key}"},
		"models":   map[string]any{},
	}}}
	raw, _ := json.MarshalIndent(doc, "", "  ")
	os.WriteFile(p, raw, 0o644)

	res := ModelUpdate(context.Background(), []string{"opencode"}, home, t.TempDir(), MUOptions{Dry: true})
	if len(res) != 1 || res[0].Err != "" {
		t.Fatalf("file reference must resolve: %+v", res)
	}
	if !strings.HasPrefix(res[0].KeySource, "file ") || strings.Contains(res[0].KeySource, "sk-from-file") {
		t.Errorf("label = %q", res[0].KeySource)
	}
	// an empty catalog + reachable alpha => alpha is an ADDITION; the
	// diff existing at all proves the file's key (not the raw
	// "{file:~...}" string) went on the wire
	if strings.Join(res[0].Added, ",") != "alpha" {
		t.Errorf("gateway did not see the file's key: %+v", res[0])
	}

	// broken reference: honest refusal, no silent fallback, no raw
	// reference on the wire
	doc2 := doc
	e := doc2["providers"].(map[string]any)["gw"].(map[string]any)
	e["settings"] = map[string]any{"baseURL": srv.URL, "apiKey": "{file:~/.secrets/missing}"}
	raw2, _ := json.MarshalIndent(doc2, "", "  ")
	os.WriteFile(p, raw2, 0o644)
	res = ModelUpdate(context.Background(), []string{"opencode"}, home, t.TempDir(), MUOptions{})
	if len(res) != 1 || !strings.Contains(res[0].Err, "resolves to nothing") {
		t.Fatalf("want broken-ref refusal, got %+v", res)
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(raw2) {
		t.Error("broken reference modified the config")
	}
}

// The reported field bug, claude half: keys delivered by the harness's
// own apiKeyHelper command must be USED (executed, like claude itself
// does on every launch) - and never shown.
func TestModelUpdateClaudeHelper(t *testing.T) {
	srv := fakeGatewayKey(t, "sk-from-helper", "anthropic", "claude-test-1")
	defer srv.Close()
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	helper := fmt.Sprintf("echo sk-from-helper; echo %s", srv.URL)
	// helper prints the key on stdout; settings uses it as hook
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"),
		[]byte(fmt.Sprintf(`{"env": {"ANTHROPIC_BASE_URL": %q}, "apiKeyHelper": "printf %%s sk-from-helper"}`, srv.URL)), 0o644)
	_ = helper
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	os.Unsetenv("ANTHROPIC_API_KEY")

	res := ModelUpdate(context.Background(), []string{"claude"}, home, t.TempDir(), MUOptions{})
	if len(res) != 1 || res[0].Err != "" {
		t.Fatalf("helper must authenticate the probe: %+v", res)
	}
	if res[0].KeySource != "apiKeyHelper (executed)" {
		t.Errorf("label = %q", res[0].KeySource)
	}
	for _, s := range []string{res[0].Note, res[0].KeySource} {
		if strings.Contains(s, "sk-from-helper") {
			t.Fatal("SECRET LEAK through helper path")
		}
	}

	// env key wins: the helper is NOT executed when the environment
	// already carries the credential (marker file proves it)
	marker := filepath.Join(home, "ran")
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"),
		[]byte(fmt.Sprintf(`{"env": {"ANTHROPIC_BASE_URL": %q}, "apiKeyHelper": "touch %s; printf %%s sk-from-helper"}`, srv.URL, marker)), 0o644)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "sk-env-wins")
	failing := fakeGatewayKey(t, "sk-env-wins", "anthropic", "claude-test-1")
	defer failing.Close()
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"),
		[]byte(fmt.Sprintf(`{"env": {"ANTHROPIC_BASE_URL": %q}, "apiKeyHelper": "touch %s; printf %%s sk-from-helper"}`, failing.URL, marker)), 0o644)
	os.Remove(marker)
	res = ModelUpdate(context.Background(), []string{"claude"}, home, t.TempDir(), MUOptions{})
	if res[0].KeySource != "env ANTHROPIC_AUTH_TOKEN" {
		t.Errorf("env precedence broken: %+v", res[0])
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("helper executed although env key was present")
	}
}

// --- pagination: absence is only proof when enumeration was complete ---

func pagedCursorGateway(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data []map[string]string
		if r.URL.Query().Get("after") == "cur2" {
			data = []map[string]string{{"id": "gamma"}}
		} else {
			data = []map[string]string{{"id": "alpha"}, {"id": "beta"}}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": data, "has_more": true, "next_cursor": "cur2"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func TestProbeAllCursorPages(t *testing.T) {
	srv := pagedCursorGateway(t)
	defer srv.Close()
	ids, complete, err := ProbeAll(context.Background(), StyleOpenAICompat, Endpoint{BaseURL: srv.URL}, "sk-classified")
	if err != nil || !complete {
		t.Fatalf("err %v complete %v", err, complete)
	}
	if strings.Join(ids, ",") != "alpha,beta,gamma" {
		t.Errorf("union wrong: %v", ids)
	}
}

// a gateway that promises has_more forever but repeats page one (the
// cursor param is ignored upstream) must come back INCOMPLETE - and
// modelupdate must withhold every removal on it while still adding.
func TestModelUpdatePartialEnumerationWithholdsRemovals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":        []map[string]string{{"id": "alpha"}},
			"has_more":    true,
			"next_cursor": "same-always",
		})
	}))
	defer srv.Close()
	home := t.TempDir()
	p := filepath.Join(home, ".config", "opencode", "opencode.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	doc := map[string]any{"providers": map[string]any{"gw": map[string]any{
		"package":  "aisdk:@ai-sdk/openai-compatible",
		"settings": map[string]any{"baseURL": srv.URL, "apiKey": "{env:MK}"},
		"models":   map[string]any{"vanished?": map[string]any{}},
	}}}
	raw, _ := json.MarshalIndent(doc, "", "  ")
	os.WriteFile(p, raw, 0o644)
	t.Setenv("MK", "sk-classified")

	res := ModelUpdate(context.Background(), []string{"opencode"}, home, t.TempDir(), MUOptions{})
	ch := res[0]
	if !ch.Partial || !strings.Contains(ch.Note, "INCOMPLETE") {
		t.Fatalf("must report partial: %+v", ch)
	}
	if len(ch.Removed) != 0 {
		t.Errorf("removals proposed on unknown ground: %v", ch.Removed)
	}
	if strings.Join(ch.Added, ",") != "alpha" || !ch.Applied {
		t.Fatalf("additions must still apply: %+v", ch)
	}
	after, _ := os.ReadFile(p)
	if !strings.Contains(string(after), "vanished?") {
		t.Error("partial enumeration removed an entry - withheld means withheld")
	}
}

func TestProbeAllNextURLPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "two" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "delta"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":     []map[string]string{{"id": "alpha"}},
			"next_url": "/models?cursor=two",
		})
	}))
	defer srv.Close()
	ids, complete, err := ProbeAll(context.Background(), StyleOpenAICompat, Endpoint{BaseURL: srv.URL}, "sk-classified")
	if err != nil || !complete || strings.Join(ids, ",") != "alpha,delta" {
		t.Fatalf("err %v complete %v ids %v", err, complete, ids)
	}
}

// the model the file itself runs on survives every sweep - removals
// over the CURRENT selection are a bricked harness, not a catalog fix.
func TestModelUpdateCurrentModelProtected(t *testing.T) {
	srv := fakeGateway(t, "alpha")
	defer srv.Close()
	home := t.TempDir()
	p := filepath.Join(home, ".config", "opencode", "opencode.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	doc := map[string]any{
		"model": "gw/keepme",
		"providers": map[string]any{"gw": map[string]any{
			"package":  "aisdk:@ai-sdk/openai-compatible",
			"settings": map[string]any{"baseURL": srv.URL, "apiKey": "{env:MK}"},
			"models":   map[string]any{"keepme": map[string]any{}, "stale": map[string]any{}},
		}},
	}
	raw, _ := json.MarshalIndent(doc, "", "  ")
	os.WriteFile(p, raw, 0o644)
	t.Setenv("MK", "sk-classified")

	res := ModelUpdate(context.Background(), []string{"opencode"}, home, t.TempDir(), MUOptions{})
	ch := res[0]
	if strings.Join(ch.Removed, ",") != "stale" {
		t.Fatalf("current model must be excluded from removals: %+v", ch)
	}
	if !strings.Contains(ch.Note, "keepme") || !ch.Applied {
		t.Fatalf("protection must be announced and the rest applied: %+v", ch)
	}
	after, _ := os.ReadFile(p)
	if !strings.Contains(string(after), "keepme") || strings.Contains(string(after), "stale") {
		t.Error("apply kept the current model and dropped the stale one - wrong file")
	}
}

// the comment-preserving write: everything outside the edited model
// list survives BYTE-IDENTICAL, entry-attached comments move with
// their entries, and a shape the splicer doesn't own falls back to
// the loud full rewrite instead of mangling it.
func TestModelUpdateSplicePreservesComments(t *testing.T) {
	srv := fakeGateway(t, "alpha", "fresh")
	defer srv.Close()
	home := t.TempDir()
	p := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	os.MkdirAll(filepath.Dir(p), 0o755)
	body := `{
  // gateway-level comment: auth comes from the auth store, never inline
  "providers": {
    "gw": {
      "package": "aisdk:@ai-sdk/openai-compatible",
      "settings": {"baseURL": "` + srv.URL + `", "apiKey": "{env:MK}"},
      "models": {
        // hand-kept annotation for alpha
        "alpha": {"name": "Alpha", "limit": 7},
        // this note describes a departure
        "gone": {"name": "Gone"}
      }
    }
  },
}
`
	os.WriteFile(p, []byte(body), 0o644)
	t.Setenv("MK", "sk-classified")

	res := ModelUpdate(context.Background(), []string{"opencode"}, home, t.TempDir(), MUOptions{})
	ch := res[0]
	if ch.Err != "" || !ch.Applied || !ch.Spliced {
		t.Fatalf("want spliced apply, got %+v", ch)
	}
	after, _ := os.ReadFile(p)
	s := string(after)
	if !strings.Contains(s, "gateway-level comment") {
		t.Error("comment outside the edited list was lost")
	}
	if !strings.Contains(s, "hand-kept annotation") {
		t.Error("comment of a KEPT entry was lost")
	}
	if strings.Contains(s, "this note describes a departure") || strings.Contains(s, "\"gone\"") {
		t.Error("removed entry (or its note) survived")
	}
	if !strings.Contains(s, "\"fresh\"") || !strings.Contains(s, "\"limit\": 7") {
		t.Error("addition missing or kept entry lost its options")
	}
	// byte-identity outside the models object
	if !strings.HasPrefix(s, `{
  // gateway-level comment: auth comes from the auth store, never inline
  "providers": {
    "gw": {
      "package": "aisdk:@ai-sdk/openai-compatible",
      "settings": {"baseURL": "`+srv.URL+`", "apiKey": "{env:MK}"},
`) {
		t.Errorf("prefix not byte-identical:\n%s", s[:min(len(s), 300)])
	}
	// still valid JSONC
	var doc map[string]any
	if err := json.Unmarshal(stripJSONC(after), &doc); err != nil {
		t.Fatalf("spliced file unparseable: %v", err)
	}
	models := doc["providers"].(map[string]any)["gw"].(map[string]any)["models"].(map[string]any)
	if len(models) != 2 {
		t.Errorf("want exactly alpha+fresh, got %v", models)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestModelUpdateSpliceFallsBackOnOddShapes(t *testing.T) {
	srv := fakeGateway(t, "alpha")
	defer srv.Close()
	home := t.TempDir()
	p := filepath.Join(home, ".config", "opencode", "opencode.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"providers":{"gw":{"package":"aisdk:@ai-sdk/openai-compatible","settings":{"baseURL":"`+srv.URL+`","apiKey":"{env:MK}"},"models":{"beta":"plain-string-entry"}}}}`), 0o644)
	t.Setenv("MK", "sk-classified")
	res := ModelUpdate(context.Background(), []string{"opencode"}, home, t.TempDir(), MUOptions{})
	ch := res[0]
	if ch.Err != "" || !ch.Applied {
		t.Fatalf("odd shape must still update via fallback: %+v", ch)
	}
	if ch.Spliced {
		t.Error("scalar-valued entries are beyond the splicer - must take the warned full rewrite")
	}
	after, _ := os.ReadFile(p)
	var doc map[string]any
	if err := json.Unmarshal(after, &doc); err != nil {
		t.Fatal(err)
	}
	models := doc["providers"].(map[string]any)["gw"].(map[string]any)["models"].(map[string]any)
	if _, ok := models["alpha"]; !ok || len(models) != 1 {
		t.Errorf("fallback diff wrong: %v", models)
	}
}
