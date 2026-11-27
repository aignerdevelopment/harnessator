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
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/dialect"
)

// Codex renders the plan for the Codex CLI: prompt/workflow files as
// .codex/prompts/<id>.md, skills via the AgentSkills convention in
// .agents/skills/, and MCP servers spliced into .codex/config.toml inside
// an unmistakably managed block.
type Codex struct{}

// Name implements Adapter.
func (*Codex) Name() string { return "codex" }

// ManagedDirs implements Specer.
func (*Codex) ManagedDirs(c *Context) []string {
	return []string{
		filepath.Join(c.Project, ".codex", "prompts"),
		filepath.Join(c.Project, ".agents", "skills"),
	}
}

// Specs implements Specer.
func (*Codex) Specs(c *Context) ([]Spec, error) {
	specs, err := c.codexSpecs()
	if err != nil {
		return nil, err
	}
	return append(specs, Spec{Dst: filepath.Join(c.Project, ".codex", "config.toml")}), nil
}

// Apply implements Adapter.
func (*Codex) Apply(c *Context) error {
	specs, err := c.codexSpecs()
	if err != nil {
		return err
	}
	if err := applySpecs(c, specs, (*Codex)(nil).ManagedDirs(c)); err != nil {
		return err
	}
	block, err := c.codexBlock()
	if err != nil {
		return err
	}
	if err := spliceManagedBlock(
		filepath.Join(c.Project, ".codex", "config.toml"),
		c, "# >>> harnessator managed (do not edit) >>>", "# <<< harnessator managed <<<", block); err != nil {
		return err
	}
	c.noteUnsupported("codex", "hook", "agent", "tool")
	governanceCheck(c, "codex")
	return nil
}

func (c *Context) codexSpecs() ([]Spec, error) {
	var specs []Spec
	for _, cat := range []string{"prompt", "workflow"} {
		for _, ch := range c.byCategory(cat) {
			src, err := c.componentSrc(ch)
			if err != nil {
				return nil, err
			}
			specs = append(specs, Spec{
				Dst: filepath.Join(c.Project, ".codex", "prompts", ch.ID+".md"), Src: src})
		}
	}
	skillSpecs, err := c.agentSkillsSpecs("codex")
	if err != nil {
		return nil, err
	}
	return append(specs, skillSpecs...), nil
}

// agentSkillsSpecs is shared by every target reading the AgentSkills
// convention location .agents/skills/ (codex, antigravity, cursor, pi).
func (c *Context) agentSkillsSpecs(target string) ([]Spec, error) {
	var specs []Spec
	for _, ch := range c.byCategory("skill") {
		if !targetsInclude(ch, target) {
			continue
		}
		src, err := c.componentSrc(ch)
		if err != nil {
			return nil, err
		}
		specs = append(specs, Spec{
			Dst: filepath.Join(c.Project, ".agents", "skills", ch.ID), Src: src, Dir: true})
	}
	return specs, nil
}

// codexBlock renders the provider stanzas plus [mcp_servers.<id>]
// sections for the managed region of .codex/config.toml.
func (c *Context) codexBlock() (string, error) {
	var b strings.Builder
	if c.Provider != nil {
		if d, ok := dialect.For((*Codex)(nil).Name()); ok {
			res, err := d.Govern(c.Provider, c.Policy)
			if err != nil {
				c.record("SKIP provider %s: %v", c.Provider.Name, err)
			} else if res.TOML != "" {
				b.WriteString(res.TOML)
				c.record("provider %s rendered for codex", c.Provider.Name)
			}
		}
	}
	for _, ch := range c.byCategory("mcp") {
		if !targetsInclude(ch, "codex") {
			continue
		}
		frag, err := c.mcpFrag(ch, "codex")
		if err != nil {
			continue // missing per-target configs are skipped, never fatal
		}
		var cfg map[string]any
		if err := json.Unmarshal(frag, &cfg); err != nil {
			return "", fmt.Errorf("%s: %w", ch.QualifiedID(), err)
		}
		fmt.Fprintf(&b, "[mcp_servers.%s]\n", ch.ID)
		if url, ok := cfg["url"].(string); ok {
			fmt.Fprintf(&b, "url = %q\n", url)
		}
		if cmd, ok := cfg["command"].(string); ok {
			fmt.Fprintf(&b, "command = %q\n", cmd)
			if args, ok := cfg["args"].([]any); ok && len(args) > 0 {
				parts := make([]string, len(args))
				for i, a := range args {
					parts[i] = fmt.Sprintf("%q", a)
				}
				fmt.Fprintf(&b, "args = [%s]\n", strings.Join(parts, ", "))
			}
		}
		if env, ok := cfg["env"].(map[string]any); ok {
			keys := make([]string, 0, len(env))
			for k := range env {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(&b, "%s = %q\n", k, env[k])
			}
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// spliceManagedBlock replaces (or appends) the region between begin/end
// markers in path, preserving any human content outside it. The file is
// created without a header if missing; path may not exist yet.
func spliceManagedBlock(path string, c *Context, begin, end, body string) error {
	var existing []byte
	if b, err := os.ReadFile(path); err == nil {
		existing = b
	} else if !os.IsNotExist(err) {
		return err
	}
	var out []string
	inBlock := false
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == begin {
			inBlock = true
			continue
		}
		if strings.TrimSpace(line) == end {
			inBlock = false
			continue
		}
		if !inBlock {
			out = append(out, line)
		}
	}
	trimmed := strings.TrimRight(strings.Join(out, "\n"), "\n")
	block := begin + "\n" + strings.TrimRight(body, "\n") + "\n" + end + "\n"
	final := trimmed + "\n\n" + block
	return c.writeFile(path, []byte(final))
}
