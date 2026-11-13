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

	"harnessator.org/harnessator/internal/resolve"
)

// GitHub Copilot (VS Code agent mode, Copilot CLI and the coding agent)
// renders rules as GENERATED .github/instructions/<id>.instructions.md
// (applyTo frontmatter, default "**/*" = always, hint copilot.globs to
// scope), prompts/workflows as .github/prompts/<id>.prompt.md, agents as
// .github/agents/<id>.agent.md, skills via the shared .agents/skills, and
// MCP into BOTH .vscode/mcp.json (servers - VS Code) and .mcp.json
// (mcpServers - CLI/portable). AGENTS.md is read natively, so the active
// index needs no special entry file here.
type Copilot struct{}

const copilotMarker = "<!-- managed by harnessator - edit the source rule, not this file -->"

// Name implements Adapter.
func (*Copilot) Name() string { return "copilot" }

// ManagedDirs implements Specer.
func (*Copilot) ManagedDirs(c *Context) []string {
	gh := filepath.Join(c.Project, ".github")
	return []string{
		filepath.Join(c.Project, ".agents", "skills"),
		filepath.Join(gh, "prompts"),
		filepath.Join(gh, "agents"),
	}
}

// Specs implements Specer.
func (*Copilot) Specs(c *Context) ([]Spec, error) {
	specs, err := c.copilotLinkSpecs()
	if err != nil {
		return nil, err
	}
	for _, ch := range c.byCategory("rule") {
		specs = append(specs,
			Spec{Dst: filepath.Join(c.Project, ".github", "instructions", ch.ID+".instructions.md")})
	}
	return append(specs,
		Spec{Dst: filepath.Join(c.Project, ".vscode", "mcp.json")},
		Spec{Dst: filepath.Join(c.Project, ".mcp.json")}), nil
}

// Apply implements Adapter.
func (*Copilot) Apply(c *Context) error {
	specs, err := c.copilotLinkSpecs()
	if err != nil {
		return err
	}
	if err := applySpecs(c, specs, (*Copilot)(nil).ManagedDirs(c)); err != nil {
		return err
	}

	// generated instructions files, marker-guarded cleanup
	insDir := filepath.Join(c.Project, ".github", "instructions")
	keep := map[string]bool{}
	for _, ch := range c.byCategory("rule") {
		dst := filepath.Join(insDir, ch.ID+".instructions.md")
		keep[dst] = true
		if err := c.writeInstruction(dst, ch); err != nil {
			return err
		}
	}
	if err := c.cleanupGeneratedFiles(insDir, keep, ".instructions.md", copilotMarker); err != nil {
		return err
	}

	servers, err := c.mcpCollection("copilot")
	if err != nil {
		return err
	}
	rawServers, err := json.MarshalIndent(map[string]any{"servers": servers}, "", "  ")
	if err != nil {
		return err
	}
	if err := c.writeFile(filepath.Join(c.Project, ".vscode", "mcp.json"), append(rawServers, '\n')); err != nil {
		return err
	}
	rawPortable, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
	if err != nil {
		return err
	}
	if err := c.writeFile(filepath.Join(c.Project, ".mcp.json"), append(rawPortable, '\n')); err != nil {
		return err
	}

	c.noteUnsupported("copilot", "hook", "tool")
	governanceCheck(c, "copilot")
	return nil
}

func (c *Context) copilotLinkSpecs() ([]Spec, error) {
	specs, err := c.agentSkillsSpecs("copilot")
	if err != nil {
		return nil, err
	}
	for _, cat := range []string{"prompt", "workflow"} {
		for _, ch := range c.byCategory(cat) {
			src, err := c.componentSrc(ch)
			if err != nil {
				return nil, err
			}
			specs = append(specs,
				Spec{Dst: filepath.Join(c.Project, ".github", "prompts", ch.ID+".prompt.md"), Src: src})
		}
	}
	for _, ch := range c.byCategory("agent") {
		src, ok := c.agentFileOrGeneric(ch, "copilot")
		if !ok {
			c.record("SKIP agent %s: no agents/copilot/%s.md variant or generic file - author one or retarget the component", ch.ID, ch.ID)
			continue
		}
		specs = append(specs,
			Spec{Dst: filepath.Join(c.Project, ".github", "agents", ch.ID+".agent.md"), Src: src})
	}
	return specs, nil
}

// writeInstruction emits marker + description + applyTo frontmatter + body.
// applyTo: hint copilot.globs when authored, "**/*" otherwise - rules are
// always-on guidance, matching the category's semantics.
func (c *Context) writeInstruction(dst string, ch resolve.Choice) error {
	src, err := c.componentSrc(ch)
	if err != nil {
		return err
	}
	if c.DryRun {
		c.record("generate %s (copilot instruction)", dst)
		return nil
	}
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	apply := "\"**/*\""
	if hint := ch.Component.Hint["copilot"]; hint != nil {
		if globs, ok := hint["globs"].([]any); ok && len(globs) > 0 {
			parts := make([]string, 0, len(globs))
			for _, g := range globs {
				parts = append(parts, strings.TrimSpace(fmt.Sprint(g)))
			}
			apply = "\"" + strings.Join(parts, ",") + "\""
		}
	}
	desc := ch.Component.Description
	if desc == "" {
		desc = ch.ID
	}
	out := "---\ndescription: " + desc + "\napplyTo: " + apply + "\n---\n" +
		copilotMarker + "\n" + string(body)
	return c.writeFile(dst, []byte(out))
}
