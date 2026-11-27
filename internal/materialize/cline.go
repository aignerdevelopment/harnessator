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

// Cline (VS Code extension, CLI and SDK) is the most AGENTS-native of the
// three: rules link as plain .clinerules/<id>.md (every .md/.txt in there
// composes the rule set), prompts AND workflows land in
// .clinerules/workflows/<id>.md (invoked as slash commands by filename),
// agents in .cline/agents/, skills via the shared .agents/skills location
// Cline scans, and MCP into the project-owned .cline/mcp.json. AGENTS.md
// is read natively.
type Cline struct{}

// Name implements Adapter.
func (*Cline) Name() string { return "cline" }

// ManagedDirs implements Specer. .clinerules itself is NOT claimed -
// humans keep authoring files there; only our links inside are spec-
// managed, and the workflows subdir is ours like the agents dir.
func (*Cline) ManagedDirs(c *Context) []string {
	return []string{
		filepath.Join(c.Project, ".agents", "skills"),
		filepath.Join(c.Project, ".clinerules", "workflows"),
		filepath.Join(c.Project, ".cline", "agents"),
	}
}

// Specs implements Specer.
func (*Cline) Specs(c *Context) ([]Spec, error) {
	specs, err := c.clineSpecs()
	if err != nil {
		return nil, err
	}
	return append(specs, Spec{Dst: filepath.Join(c.Project, ".cline", "mcp.json")}), nil
}

// Apply implements Adapter.
func (*Cline) Apply(c *Context) error {
	specs, err := c.clineSpecs()
	if err != nil {
		return err
	}
	if err := applySpecs(c, specs, (*Cline)(nil).ManagedDirs(c)); err != nil {
		return err
	}
	servers, err := c.mcpCollection("cline")
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
	if err != nil {
		return err
	}
	if err := c.writeFile(filepath.Join(c.Project, ".cline", "mcp.json"), append(raw, '\n')); err != nil {
		return err
	}
	c.noteUnsupported("cline", "hook", "tool")
	governanceCheck(c, "cline")
	return nil
}

func (c *Context) clineSpecs() ([]Spec, error) {
	specs, err := c.agentSkillsSpecs("cline")
	if err != nil {
		return nil, err
	}
	for _, ch := range c.byCategory("rule") {
		src, err := c.componentSrc(ch)
		if err != nil {
			return nil, err
		}
		specs = append(specs,
			Spec{Dst: filepath.Join(c.Project, ".clinerules", ch.ID+".md"), Src: src})
	}
	for _, cat := range []string{"prompt", "workflow"} {
		for _, ch := range c.byCategory(cat) {
			src, err := c.componentSrc(ch)
			if err != nil {
				return nil, err
			}
			specs = append(specs,
				Spec{Dst: filepath.Join(c.Project, ".clinerules", "workflows", ch.ID+".md"), Src: src})
		}
	}
	for _, ch := range c.byCategory("agent") {
		src, ok := c.agentFileOrGeneric(ch, "cline")
		if !ok {
			c.record("SKIP agent %s: no agents/cline/%s.md variant or generic file - author one or retarget the component", ch.ID, ch.ID)
			continue
		}
		specs = append(specs,
			Spec{Dst: filepath.Join(c.Project, ".cline", "agents", ch.ID+".md"), Src: src})
	}
	return specs, nil
}
