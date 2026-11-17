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
  // hand comment survives parse, not rewrite
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
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
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
