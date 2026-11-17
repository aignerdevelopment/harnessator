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

// Cursor renders rules as generated .cursor/rules/<id>.mdc (markdown with
// description/globs/alwaysApply frontmatter), skills via
// the shared AgentSkills location, prompts/workflows as .cursor/commands
// and MCP as .cursor/mcp.json.
//
// Rules must be GENERATED (copied content), not linked, because Cursor
// composes rules from frontmatter + body. Dev-mode edits therefore only
// propagate on the next `harnessator setup` - verify catches direct edits
// of the generated file (it is recorded with a digest like any component).
// Generated files are marked so disable-cleanup never removes hand work.
type Cursor struct{}

// mdcMarker identifies harnessator-generated rule files for safe cleanup.
const mdcMarker = "# managed by harnessator - edit the source rule, not this file"

// Name implements Adapter.
func (*Cursor) Name() string { return "cursor" }

// ManagedDirs implements Specer.
func (*Cursor) ManagedDirs(c *Context) []string {
	return []string{
		filepath.Join(c.Project, ".agents", "skills"),
		filepath.Join(c.Project, ".cursor", "commands"),
	}
}

// Specs implements Specer.
func (*Cursor) Specs(c *Context) ([]Spec, error) {
	specs, err := c.cursorLinkSpecs()
	if err != nil {
		return nil, err
	}
	for _, ch := range c.byCategory("rule") {
		if targetsInclude(ch, "cursor") {
			specs = append(specs,
				Spec{Dst: filepath.Join(c.Project, ".cursor", "rules", ch.ID+".mdc")})
		}
	}
	return append(specs, Spec{Dst: filepath.Join(c.Project, ".cursor", "mcp.json")}), nil
}

// Apply implements Adapter.
func (*Cursor) Apply(c *Context) error {
	specs, err := c.cursorLinkSpecs()
	if err != nil {
		return err
	}
	if err := applySpecs(c, specs, (*Cursor)(nil).ManagedDirs(c)); err != nil {
		return err
	}

	rulesDir := filepath.Join(c.Project, ".cursor", "rules")
	keep := map[string]bool{}
	for _, ch := range c.byCategory("rule") {
		if !targetsInclude(ch, "cursor") {
			continue
		}
		dst := filepath.Join(rulesDir, ch.ID+".mdc")
		keep[dst] = true
		if err := c.writeRuleMDC(dst, ch); err != nil {
			return err
		}
	}
	if err := c.cleanupGenerated(rulesDir, keep); err != nil {
		return err
	}

	servers, err := c.mcpCollection("cursor")
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := c.writeFile(filepath.Join(c.Project, ".cursor", "mcp.json"), raw); err != nil {
		return err
	}
	c.noteUnsupported("cursor", "hook", "agent", "tool")
	governanceCheck(c, "cursor")
	return nil
}

func (c *Context) cursorLinkSpecs() ([]Spec, error) {
	specs, err := c.agentSkillsSpecs("cursor")
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
				Spec{Dst: filepath.Join(c.Project, ".cursor", "commands", ch.ID+".md"), Src: src})
		}
	}
	return specs, nil
}

// writeRuleMDC emits marker + frontmatter + rule body.
func (c *Context) writeRuleMDC(dst string, ch resolve.Choice) error {
	src, err := c.componentSrc(ch)
	if err != nil {
		return err
	}
	if c.DryRun {
		c.record("generate %s (rule mdc)", dst)
		return nil
	}
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	var fm []string
	fm = append(fm, "---")
	desc := ch.Component.Description
	if desc == "" {
		desc = ch.ID
	}
	fm = append(fm, "description: "+desc)
	hint := ch.Component.Hint["cursor"]
	globs, _ := hint["globs"].([]any)
	if len(globs) > 0 {
		parts := make([]string, 0, len(globs))
		for _, g := range globs {
			parts = append(parts, fmt.Sprintf("%q", fmt.Sprint(g)))
		}
		fm = append(fm, "globs: ["+strings.Join(parts, ", ")+"]")
	}
	always := false
	if v, ok := hint["always-apply"].(bool); ok {
		always = v
	} else {
		always = ch.Component.Always
	}
	fm = append(fm, fmt.Sprintf("alwaysApply: %v", always))
	fm = append(fm, "---")

	out := mdcMarker + "\n" + strings.Join(fm, "\n") + "\n\n" + string(body)
	return c.writeFile(dst, []byte(out))
}

// cleanupGenerated removes stale *.mdc that carry OUR marker; foreign files
// are never touched.
func (c *Context) cleanupGenerated(dir string, keep map[string]bool) error {
	if c.DryRun {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if keep[p] || filepath.Ext(e.Name()) != ".mdc" {
			continue
		}
		head := make([]byte, len(mdcMarker))
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		n, _ := f.Read(head)
		f.Close()
		if n == len(head) && string(head) == mdcMarker {
			c.record("remove stale generated rule %s", p)
			if err := os.Remove(p); err != nil {
				return err
			}
		}
	}
	return nil
}
