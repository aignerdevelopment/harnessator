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

package materialize

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"harnessator.org/harnessator/internal/dialect"
)

// Claude renders the plan for Claude Code: .claude/{skills,commands,agents}
// links, generated .mcp.json, and a fully generated
// .claude/settings.local.json with hooks merged from the source
// template - behavior proven in years of shell syncing, now in Go.
type Claude struct{}

// Name implements Adapter.
func (*Claude) Name() string { return "claude" }

// Apply implements Adapter.
func (*Claude) Apply(c *Context) error {
	specs, err := c.claudeSpecs()
	if err != nil {
		return err
	}
	if err := applySpecs(c, specs, (*Claude)(nil).ManagedDirs(c)); err != nil {
		return err
	}

	if err := c.writeClaudeSettings(); err != nil {
		return err
	}

	servers, err := c.mcpCollection("claude")
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := c.writeFile(filepath.Join(c.Project, ".mcp.json"), raw); err != nil {
		return err
	}
	if n := len(c.byCategory("rule")); n > 0 {
		c.record("%d rule(s) rendered via CLAUDE.md -> AGENTS.md + active index", n)
	}
	governanceCheck(c, "claude")
	return nil
}

// Specs implements Specer for doctor diffing.
func (*Claude) Specs(c *Context) ([]Spec, error) {
	specs, err := c.claudeSpecs()
	if err != nil {
		return nil, err
	}
	return append(specs,
		Spec{Dst: filepath.Join(c.Project, ".claude", "settings.local.json")},
		Spec{Dst: filepath.Join(c.Project, ".mcp.json")},
	), nil
}

// ManagedDirs implements Specer.
func (*Claude) ManagedDirs(c *Context) []string {
	base := filepath.Join(c.Project, ".claude")
	return []string{
		filepath.Join(base, "skills"),
		filepath.Join(base, "commands"),
		filepath.Join(base, "agents"),
	}
}

func (c *Context) claudeSpecs() ([]Spec, error) {
	base := filepath.Join(c.Project, ".claude")
	var specs []Spec

	for _, ch := range c.byCategory("skill") {
		src, err := c.componentSrc(ch)
		if err != nil {
			return nil, err
		}
		specs = append(specs, Spec{Dst: filepath.Join(base, "skills", ch.ID), Src: src, Dir: true})
	}

	for _, cat := range []string{"prompt", "workflow"} {
		for _, ch := range c.byCategory(cat) {
			src, err := c.componentSrc(ch)
			if err != nil {
				return nil, err
			}
			specs = append(specs, Spec{Dst: filepath.Join(base, "commands", ch.ID+".md"), Src: src})
		}
	}

	for _, ch := range c.byCategory("agent") {
		src, err := c.agentFile(ch, "claude")
		if err != nil {
			return nil, err
		}
		specs = append(specs, Spec{Dst: filepath.Join(base, "agents", ch.ID+".md"), Src: src})
	}
	return specs, nil
}

// writeClaudeSettings generates settings.local.json from scratch each run:
// hook template merge plus the provider env block. Secret VALUES never land here - only endpoint URLs, model ids
// and env var NAMES; auth remains ambient (ANTHROPIC_AUTH_TOKEN in env).
func (c *Context) writeClaudeSettings() error {
	path := filepath.Join(c.Project, ".claude", "settings.local.json")
	base := map[string]any{}

	var template *string
	var home string
	for _, ch := range c.byCategory("hook") {
		if !targetsInclude(ch, "claude") {
			continue
		}
		pin := c.pin(ch.Source)
		tp := filepath.Join(pin.Dir, "hooks", "claude", "settings.hooks.json")
		if _, err := os.Stat(tp); err == nil {
			template = &tp
			home = pin.Dir
			break
		}
	}
	if template != nil {
		raw, err := os.ReadFile(*template)
		if err != nil {
			return err
		}
		// {{AGENTICTOOLS_HOME}} is the historical placeholder name;
		// {{HARNESSATOR_SOURCE}} is accepted too.
		expanded := strings.ReplaceAll(string(raw), "{{AGENTICTOOLS_HOME}}", home)
		expanded = strings.ReplaceAll(expanded, "{{HARNESSATOR_SOURCE}}", home)
		var tmpl map[string]any
		if err := json.Unmarshal([]byte(expanded), &tmpl); err != nil {
			return fmt.Errorf("hook template %s: %w", *template, err)
		}
		base = deepMerge(base, tmpl)
	}

	if env := c.claudeEnv(); len(env) > 0 {
		base["env"] = env
		c.record("provider env block rendered for claude (%d keys)", len(env))
	}

	out, err := json.MarshalIndent(base, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return c.writeFile(path, out)
}

// claudeEnv renders the governance env block through the claude dialect
// (internal/dialect): endpoint + approved-model variables, including the
// sub-agent model pin that fixes the "sub-agents ignore configured model"
// collision. nil = nothing to render or no speakable style.
func (c *Context) claudeEnv() map[string]any {
	if c.Provider == nil {
		return nil
	}
	d, ok := dialect.For((*Claude)(nil).Name())
	if !ok {
		return nil
	}
	res, err := d.Govern(c.Provider, c.Policy)
	if err != nil {
		c.record("SKIP provider %s: %v", c.Provider.Name, err)
		return nil
	}
	return res.Env
}

// deepMerge merges b over a (b wins), matching jq's `.[0] * .[1]`.
func deepMerge(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if bv, ok := v.(map[string]any); ok {
			if av, ok := out[k].(map[string]any); ok {
				out[k] = deepMerge(av, bv)
				continue
			}
		}
		out[k] = v
	}
	return out
}
