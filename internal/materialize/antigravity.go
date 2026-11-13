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
	"path/filepath"
)

// Antigravity renders skills (AgentSkills .agents/skills), workflows/prompts
// (.agents/workflows) and MCP (.agents/mcp_config.json).
type Antigravity struct{}

// Name implements Adapter.
func (*Antigravity) Name() string { return "antigravity" }

// ManagedDirs implements Specer.
func (*Antigravity) ManagedDirs(c *Context) []string {
	base := filepath.Join(c.Project, ".agents")
	return []string{filepath.Join(base, "skills"), filepath.Join(base, "workflows")}
}

// Specs implements Specer.
func (*Antigravity) Specs(c *Context) ([]Spec, error) {
	specs, err := c.antigravitySpecs()
	if err != nil {
		return nil, err
	}
	return append(specs, Spec{Dst: filepath.Join(c.Project, ".agents", "mcp_config.json")}), nil
}

// Apply implements Adapter.
func (*Antigravity) Apply(c *Context) error {
	specs, err := c.antigravitySpecs()
	if err != nil {
		return err
	}
	if err := applySpecs(c, specs, (*Antigravity)(nil).ManagedDirs(c)); err != nil {
		return err
	}
	servers, err := c.mcpCollection("antigravity")
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := c.writeFile(filepath.Join(c.Project, ".agents", "mcp_config.json"), raw); err != nil {
		return err
	}
	c.noteUnsupported("antigravity", "hooks", "agent", "tool")
	governanceCheck(c, "antigravity")
	return nil
}

func (c *Context) antigravitySpecs() ([]Spec, error) {
	specs, err := c.agentSkillsSpecs("antigravity")
	if err != nil {
		return nil, err
	}
	base := filepath.Join(c.Project, ".agents", "workflows")
	for _, cat := range []string{"workflow", "prompt"} {
		for _, ch := range c.byCategory(cat) {
			src, err := c.componentSrc(ch)
			if err != nil {
				return nil, err
			}
			specs = append(specs, Spec{Dst: filepath.Join(base, ch.ID+".md"), Src: src})
		}
	}
	return specs, nil
}
