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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parseTmp(t *testing.T, content string) (*Profile, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "prov.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return Parse(p)
}

const minimal = `
name = "gw"
[styles.openai-compatible]
base_url = "https://gw/v1"
`

func TestParseValidMinimal(t *testing.T) {
	p, err := parseTmp(t, minimal)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "gw" {
		t.Errorf("name = %q", p.Name)
	}
}

func TestParseRejectsOldProductBlocks(t *testing.T) {
	// migration guard: the pre-v1 [opencode]/[claude]/[codex] format must
	// fail loudly, never half-parse
	_, err := parseTmp(t, `
name = "gw"
[opencode]
base_url = "https://gw/v1"
`)
	if err == nil || !strings.Contains(err.Error(), "unknown or misplaced keys") {
		t.Fatalf("old-format file should fail with unknown-key error, got: %v", err)
	}
}

func TestParseStyleValidation(t *testing.T) {
	for name, content := range map[string]string{
		"no styles":            "name = \"gw\"\n",
		"unknown style":        "name = \"gw\"\n[styles.ollama]\nbase_url = \"x\"\n",
		"missing base_url":     "name = \"gw\"\n[styles.openai-compatible]\nauth_env = \"K\"\n",
		"default not in model": "name = \"gw\"\ndefault_model = \"ghost\"\n[styles.openai-compatible]\nbase_url = \"x\"\n[models]\nbig = \"m\"\n",
	} {
		if _, err := parseTmp(t, content); err == nil {
			t.Errorf("%s: expected parse error", name)
		}
	}
}

func TestStyleAuthFallbackAndPriority(t *testing.T) {
	p, err := parseTmp(t, `
name = "gw"
auth_env = "SHARED"
default_model = "big"
[styles.anthropic-compatible]
base_url = "https://gw/anthropic"
auth_env = "ANTH"
[styles.openai-compatible]
base_url = "https://gw/v1"
[models]
big = "claude-sonnet-5"
`)
	if err != nil {
		t.Fatal(err)
	}
	ep, ok := p.Style(StyleAnthropicCompat)
	if !ok || ep.AuthEnv != "ANTH" {
		t.Errorf("per-style override: %+v ok=%v", ep, ok)
	}
	if def, ok := p.Resolve(p.DefaultModel); !ok || def != "claude-sonnet-5" {
		t.Errorf("resolve default: %q %v", def, ok)
	}
	style, ep, ok := p.StylesSpoken([]string{StyleOpenAICompat, StyleAnthropicCompat})
	if !ok || style != StyleOpenAICompat || ep.AuthEnv != "SHARED" {
		t.Errorf("priority pick: %q %+v ok=%v", style, ep, ok)
	}
	if _, _, ok := p.StylesSpoken([]string{StyleOpenAIResponses}); ok {
		t.Error("unknown family must not be spoken")
	}
}
