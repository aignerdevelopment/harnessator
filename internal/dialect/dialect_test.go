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

package dialect

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"harnessator.org/harnessator/internal/policy"
	"harnessator.org/harnessator/internal/provider"
)

// marshal renders a JSON fragment for substring assertions.
func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func profile() *provider.Profile {
	return &provider.Profile{
		Name:         "gw",
		AuthEnv:      "GW_KEY",
		DefaultModel: "big",
		Styles: map[string]provider.Endpoint{
			provider.StyleOpenAICompat:    {BaseURL: "https://gw/v1"},
			provider.StyleAnthropicCompat: {BaseURL: "https://gw/anthropic"},
		},
		Models: map[string]string{
			"big":   "claude-sonnet-5",
			"fast":  "claude-haiku-5.5",
			"smart": "gpt-5.2",
		},
	}
}

func TestRegistryCapability(t *testing.T) {
	for _, want := range []string{"claude", "codex", "opencode"} {
		if _, ok := For(want); !ok {
			t.Errorf("dialect %q not registered", want)
		}
	}
	if _, ok := For("pi"); ok {
		t.Error("pi must have no dialect until its endpoint shape is verified")
	}
	got := strings.Join(Targets(), ",")
	if got != "claude,codex,opencode" {
		t.Errorf("Targets() = %q", got)
	}
}

func TestOpencodeStylePriorityAndNpm(t *testing.T) {
	p := profile()
	res, err := opencodeDialect{}.Govern(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := marshal(res.JSON)
	for _, want := range []string{
		`"@ai-sdk/openai-compatible"`, `https://gw/v1`, `"{env:GW_KEY}"`,
		`"model":"gw/claude-sonnet-5"`, "gpt-5.2", "claude-haiku-5.5",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("missing %q in %s", want, raw)
		}
	}
	// anthropic-only profile picks @ai-sdk/anthropic with that endpoint
	anth := profile()
	delete(anth.Styles, provider.StyleOpenAICompat)
	res, err = opencodeDialect{}.Govern(anth, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = marshal(res.JSON)
	if !strings.Contains(raw, "@ai-sdk/anthropic") || !strings.Contains(raw, "https://gw/anthropic") {
		t.Errorf("anthropic fallback wrong: %s", raw)
	}
}

func TestOpencodeNoSpeakableStyle(t *testing.T) {
	p := profile()
	delete(p.Styles, provider.StyleOpenAICompat)
	delete(p.Styles, provider.StyleAnthropicCompat)
	p.Styles[provider.StyleOpenAIResponses] = provider.Endpoint{BaseURL: "x"}
	if _, err := (opencodeDialect{}).Govern(p, nil); !errors.Is(err, ErrNoStyle) {
		t.Fatalf("err = %v, want ErrNoStyle", err)
	}
}

func TestOpencodePolicyNarrowsModels(t *testing.T) {
	eff := &policy.Effective{Strict: true, Allowed: []string{"big"}}
	res, err := opencodeDialect{}.Govern(profile(), eff)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := marshal(res.JSON)
	if strings.Contains(raw, "claude-haiku-5.5") || strings.Contains(raw, "gpt-5.2") {
		t.Errorf("denied model leaked: %s", raw)
	}
}

func TestClaudeOnlySpeaksAnthropic(t *testing.T) {
	res, err := claudeDialect{}.Govern(profile(), &policy.Effective{
		Strict: true, Allowed: []string{"big", "smart"}, SubagentModel: "big",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Env["ANTHROPIC_BASE_URL"]; got != "https://gw/anthropic" {
		t.Errorf("base url = %v (style selection must pick anthropic even when opencode-priority style exists)", got)
	}
	if res.Env["ANTHROPIC_MODEL"] != "claude-sonnet-5" ||
		res.Env["CLAUDE_CODE_SUBAGENT_MODEL"] != "claude-sonnet-5" ||
		res.Env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "claude-sonnet-5" {
		t.Errorf("env = %v", res.Env)
	}
	if _, ok := res.Env["ANTHROPIC_SMALL_FAST_MODEL"]; ok {
		t.Error("policy-denied fast alias rendered")
	}
	// openai-only profile: claude has nothing to speak
	p := profile()
	delete(p.Styles, provider.StyleAnthropicCompat)
	if _, err := (claudeDialect{}).Govern(p, nil); !errors.Is(err, ErrNoStyle) {
		t.Fatalf("err = %v, want ErrNoStyle", err)
	}
}

func TestClaudeTierWalkLastMatchWins(t *testing.T) {
	p := profile()
	p.Models["sonnet"] = "claude-sonnet-4" // sorted after "big" -> wins the var
	res, err := claudeDialect{}.Govern(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "claude-sonnet-4" {
		t.Errorf("tier walk changed semantics: %v", res.Env["ANTHROPIC_DEFAULT_SONNET_MODEL"])
	}
}

func TestCodexWireAPIMapping(t *testing.T) {
	res, err := codexDialect{}.Govern(profile(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.TOML, `model_provider = "gw"`) ||
		!strings.Contains(res.TOML, `env_key = "GW_KEY"`) ||
		!strings.Contains(res.TOML, `wire_api = "chat"`) {
		t.Errorf("chat stanza wrong:\n%s", res.TOML)
	}
	p := profile()
	delete(p.Styles, provider.StyleOpenAICompat)
	p.Styles[provider.StyleOpenAIResponses] = provider.Endpoint{BaseURL: "https://gw/responses"}
	res, err = codexDialect{}.Govern(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.TOML, `wire_api = "responses"`) {
		t.Errorf("responses wiring missing:\n%s", res.TOML)
	}
	// anthropic-only profile: codex has nothing to speak
	p = profile()
	delete(p.Styles, provider.StyleOpenAICompat)
	delete(p.Styles, provider.StyleOpenAIResponses)
	if _, err := (codexDialect{}).Govern(p, nil); !errors.Is(err, ErrNoStyle) {
		t.Fatalf("err = %v, want ErrNoStyle", err)
	}
}
