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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportClaudeGateway(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{
	  "apiKeyHelper": "echo sk-super-secret-do-not-copy",
	  "env": {"ANTHROPIC_BASE_URL": "https://gw.example.org"},
	  "model": "sonnet",
	  "modelSettings": {"vendor/kimi-k3": {"effortLevel": "high"}}
	}`), 0o644)

	res := Import([]string{"claude"}, home, t.TempDir())
	if len(res.Imported) != 1 || len(res.Failures) > 0 {
		t.Fatalf("want 1 imported, got %+v (%v)", res.Imported, res.Failures)
	}
	imp := res.Imported[0]
	if imp.Profile.Styles[StyleAnthropicCompat].BaseURL != "https://gw.example.org" {
		t.Errorf("endpoint not read: %+v", imp.Profile.Styles)
	}
	if imp.Profile.DefaultModel != "sonnet" {
		t.Errorf("default model = %q", imp.Profile.DefaultModel)
	}
	if got := imp.Profile.Models["vendor-kimi-k3"]; got != "vendor/kimi-k3" {
		t.Errorf("modelSettings ids must import with full-path aliases: %v", imp.Profile.Models)
	}

	// THE secret rule: the helper value must appear nowhere in output
	text := RenderProfile(imp.Profile)
	if strings.Contains(text, "super-secret") {
		t.Errorf("secret leaked into profile text:\n%s", text)
	}
	joined := strings.Join(imp.Notes, " | ") + strings.Join(res.Skipped, " | ")
	if !strings.Contains(joined, "apiKeyHelper") {
		t.Errorf("apiKeyHelper finding must be reported: %v %v", imp.Notes, res.Skipped)
	}
}

func TestImportOpencodeMultipleProviders(t *testing.T) {
	cwd := t.TempDir()
	os.WriteFile(filepath.Join(cwd, "opencode.json"), []byte(`{
	  "model": "litellm/claude-sonnet-5",
	  "provider": {
	    "litellm": {
	      "npm": "@ai-sdk/anthropic",
	      "options": {"baseURL": "https://gw.example.org/anthropic", "apiKey": "{env:LITELLM_KEY}"},
	      "models": {"claude-sonnet-5": {"name": "Sonnet"}}
	    },
	    "direct": {
	      "npm": "@ai-sdk/openai-compatible",
	      "options": {"baseURL": "https://other.example.org/v1", "apiKey": "sk-literal-forbidden"},
	      "models": {"gpt-5.2": {}}
	    },
	    "weird": {"npm": "@some/unknown-package", "options": {}}
	  }
	}`), 0o644)

	res := Import([]string{"opencode"}, t.TempDir(), cwd)
	if len(res.Imported) != 2 {
		t.Fatalf("want 2 profiles (opencode allows many), got %d: %+v", len(res.Imported), res.Imported)
	}
	var byName = map[string]Imported{}
	for _, i := range res.Imported {
		byName[i.Profile.Name] = i
	}
	lit := byName["litellm"]
	if lit.Profile.Styles[StyleAnthropicCompat].BaseURL == "" || lit.Profile.AuthEnv != "LITELLM_KEY" {
		t.Errorf("litellm mapping wrong: %+v", lit.Profile)
	}
	if lit.Profile.DefaultModel != "claude-sonnet-5" {
		t.Errorf("default model from top-level model not extracted: %q", lit.Profile.DefaultModel)
	}
	direct := byName["direct"]
	if direct.Profile.AuthEnv != "" {
		t.Errorf("literal key must NOT become auth_env: %q", direct.Profile.AuthEnv)
	}
	if strings.Contains(RenderProfile(direct.Profile), "sk-literal") {
		t.Error("literal secret leaked")
	}
	all := strings.Join(direct.Notes, "|")
	found := false
	for _, s := range res.Skipped {
		if strings.Contains(s, "unknown") || strings.Contains(s, "weird") {
			found = true
		}
	}
	if !found {
		t.Errorf("unmappable npm package must be reported: %v", res.Skipped)
	}
	_ = all
}

func TestImportCodex(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(`
model = "gpt-6.1"
model_provider = "mine"

[model_providers.mine]
name = "Mine"
base_url = "https://gw.example.org/v1"
env_key = "MY_GATEWAY_KEY"
wire_api = "responses"

[model_providers.other]
name = "Other"
base_url = "https://chat.example.org/v1"
env_key = "OTHER_KEY"
wire_api = "chat"
`), 0o644)

	res := Import([]string{"codex"}, home, t.TempDir())
	if len(res.Imported) != 2 {
		t.Fatalf("want 2 codex providers, got %+v", res.Imported)
	}
	for _, i := range res.Imported {
		if i.Profile.Name == "mine" {
			if _, ok := i.Profile.Styles[StyleOpenAIResponses]; !ok {
				t.Errorf("responses wire_api must map to openai-responses: %+v", i.Profile.Styles)
			}
			if i.Profile.DefaultModel != "gpt-6.1" {
				t.Errorf("active provider's model should carry over: %q", i.Profile.DefaultModel)
			}
			if i.Profile.AuthEnv != "MY_GATEWAY_KEY" {
				t.Errorf("env_key = %q", i.Profile.AuthEnv)
			}
		}
		if i.Profile.Name == "other" {
			if _, ok := i.Profile.Styles[StyleOpenAICompat]; !ok {
				t.Errorf("chat wire_api should map to openai-compatible")
			}
			if i.Profile.DefaultModel != "" {
				t.Errorf("inactive provider must not steal the default model")
			}
		}
	}
}

func TestImportStarHonestSkips(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"),
		[]byte(`{"env": {"ANTHROPIC_BASE_URL": "https://gw.example.org"}}`), 0o644)
	res := Import([]string{"*"}, home, t.TempDir())
	if len(res.Imported) != 1 || res.Imported[0].Harness != "claude" {
		t.Fatalf("claude import in star mode: %+v", res.Imported)
	}
	// every unmappable harness said WHY
	want := []string{"cursor", "copilot"}
	for _, w := range want {
		found := false
		for _, s := range res.Skipped {
			if strings.HasPrefix(s, w+":") {
				found = true
			}
		}
		if !found {
			t.Errorf("%s must speak in the skip lines: %v", w, res.Skipped)
		}
	}
}

// The regression the REAL opencode.jsonc taught us: comment stripping
// must never chop the "//" inside "https://".
func TestStripJSONCPreservesURLs(t *testing.T) {
	in := []byte(`{
  // provider section
  "baseURL": "https://gw.example.org/v1",
  "apiKey": "{env:X}" // trailing comment
}`)
	var doc map[string]any
	if err := json.Unmarshal(stripJSONC(in), &doc); err != nil {
		t.Fatalf("strip broke JSON: %v", err)
	}
	if doc["baseURL"] != "https://gw.example.org/v1" {
		t.Errorf("URL mangled: %v", doc["baseURL"])
	}
	blocky := []byte("{\"a\": [1, 2,], /* x */ \"u\": \"https://x/y\"\n,}")
	var d2 map[string]any
	if err := json.Unmarshal(stripJSONC(blocky), &d2); err != nil {
		t.Fatalf("block comment or double comma not handled: %v", err)
	}
}

// The live schema observed in the field (providers/package/settings)
// imports like the documented one - the command reads working reality.
func TestImportOpencodeLiveSchema(t *testing.T) {
	cwd := t.TempDir()
	os.WriteFile(filepath.Join(cwd, "opencode.jsonc"), []byte(`{
  // gateway config
  "providers": {
    "litellm": {
      "package": "aisdk:@ai-sdk/openai-compatible",
      "settings": {"baseURL": "https://gw.example.org/v1",},
      "models": {"deepseek-v4-flash": {"name": "DSv4"},},
    },
  },
}`), 0o644)
	res := Import([]string{"opencode"}, t.TempDir(), cwd)
	if len(res.Imported) != 1 {
		t.Fatalf("live schema must import: %+v (%v %v)", res.Imported, res.Skipped, res.Failures)
	}
	imp := res.Imported[0]
	if imp.Profile.Styles[StyleOpenAICompat].BaseURL != "https://gw.example.org/v1" {
		t.Errorf("settings.baseURL not read: %+v", imp.Profile)
	}
	if _, ok := imp.Profile.Models["deepseek-v4-flash"]; !ok {
		t.Errorf("model keys must become catalog entries: %v", imp.Profile.Models)
	}
}

// Round trip over feral live ids: what RenderProfile writes, Parse
// must read back - dots, colons and duplicate stems and all.
func TestRenderParseRoundTripFeralIDs(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{
	  "env": {"ANTHROPIC_BASE_URL": "https://gw.example.org"},
	  "modelSettings": {
	    "openrouter:anthropic/claude-fable-5.1": {},
	    "openrouter:anthropic/claude-fable-5": {},
	    "x.ai/grok-4.2": {}
	  }
	}`), 0o644)
	res := Import([]string{"claude"}, home, t.TempDir())
	if len(res.Imported) != 1 {
		t.Fatalf("%+v", res)
	}
	text := RenderProfile(res.Imported[0].Profile)
	path := filepath.Join(t.TempDir(), "p.toml")
	os.WriteFile(path, []byte(text), 0o644)
	parsed, err := Parse(path)
	if err != nil {
		t.Fatalf("rendered profile is not valid TOML: %v\n%s", err, text)
	}
	if len(parsed.Models) != 3 {
		t.Fatalf("feral ids lost: %v", parsed.Models)
	}
	// aliases must be unique keys mapping to the exact ids
	seen := map[string]string{}
	for alias, id := range parsed.Models {
		if prev, dup := seen[id]; dup {
			t.Errorf("duplicate id %s under %s and %s", id, prev, alias)
		}
		seen[id] = alias
		if strings.ContainsAny(alias, ".:/") {
			t.Errorf("unsafe alias %q", alias)
		}
	}
}
