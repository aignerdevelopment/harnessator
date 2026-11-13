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
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"harnessator.org/harnessator/internal/resolve"
)

// Gemini CLI renders context via a managed block in GEMINI.md pointing at
// the active index (its @-import reads the same file every harness
// loads), custom slash commands as GENERATED .gemini/commands/<id>.toml
// (description + prompt, per geminicli.com/docs/cli/custom-commands),
// subagents as .gemini/agents/<id>.md, skills via the shared AgentSkills
// location .agents/skills, and MCP into .gemini/settings.json - which is
// owned by the human when it exists: harnessator creates it, never
// clobbers it (the docs use it for model config far beyond MCP).
type Gemini struct{}

const geminiMarker = "# managed by harnessator - edit the source command, not this file"

// Name implements Adapter.
func (*Gemini) Name() string { return "gemini" }

// ManagedDirs implements Specer.
func (*Gemini) ManagedDirs(c *Context) []string {
	return []string{
		filepath.Join(c.Project, ".agents", "skills"),
		filepath.Join(c.Project, ".gemini", "commands"),
		filepath.Join(c.Project, ".gemini", "agents"),
	}
}

// Specs implements Specer.
func (*Gemini) Specs(c *Context) ([]Spec, error) {
	specs, err := c.geminiLinkSpecs()
	if err != nil {
		return nil, err
	}
	for _, ch := range append(c.byCategory("prompt"), c.byCategory("workflow")...) {
		specs = append(specs,
			Spec{Dst: filepath.Join(c.Project, ".gemini", "commands", ch.ID+".toml")})
	}
	return append(specs, Spec{Dst: filepath.Join(c.Project, ".gemini", "settings.json")},
		Spec{Dst: filepath.Join(c.Project, "GEMINI.md")}), nil
}

// Apply implements Adapter.
func (*Gemini) Apply(c *Context) error {
	specs, err := c.geminiLinkSpecs()
	if err != nil {
		return err
	}
	if err := applySpecs(c, specs, (*Gemini)(nil).ManagedDirs(c)); err != nil {
		return err
	}

	// generated custom commands (TOML), with marker-guarded stale cleanup
	cmdDir := filepath.Join(c.Project, ".gemini", "commands")
	keep := map[string]bool{}
	for _, ch := range append(c.byCategory("prompt"), c.byCategory("workflow")...) {
		dst := filepath.Join(cmdDir, ch.ID+".toml")
		keep[dst] = true
		if err := c.writeGeminiCommand(dst, ch); err != nil {
			return err
		}
	}
	if err := c.cleanupGeneratedFiles(cmdDir, keep, ".toml", geminiMarker); err != nil {
		return err
	}

	// context pointer: managed block in GEMINI.md (foreign content outside
	// the block is preserved; @-import reads the active index like every
	// other harness's entry does).
	block := "@.harnessator/active.md"
	if err := spliceManagedBlock(filepath.Join(c.Project, "GEMINI.md"), c,
		"<!-- >>> harnessator managed (do not edit) >>> -->",
		"<!-- <<< harnessator managed <<< -->", block); err != nil {
		return err
	}

	// MCP: settings.json is only ours to create - an existing one may hold
	// the human's model config, so we refuse silently nothing and record
	// the finding loudly instead.
	servers, err := c.mcpCollection("gemini")
	if err != nil {
		return err
	}
	if len(servers) > 0 {
		path := filepath.Join(c.Project, ".gemini", "settings.json")
		if _, statErr := os.Stat(path); statErr == nil {
			head, _ := os.ReadFile(path)
			if !jsonManaged(string(head)) {
				c.record("SKIP mcp: %s exists and is not harnessator-managed - add the mcpServers entries by hand (settings.json is also your model config)", path)
			} else if err := c.writeGeminiSettings(path, servers); err != nil {
				return err
			}
		} else if err := c.writeGeminiSettings(path, servers); err != nil {
			return err
		}
	}
	c.noteUnsupported("gemini", "hook", "tool")
	governanceCheck(c, "gemini")
	return nil
}

// jsonManaged recognizes OUR settings.json: we always write the
// "_managed-by" marker key alongside mcpServers.
func jsonManaged(raw string) bool {
	return strings.Contains(raw, `"`) && strings.Contains(raw, "_managed-by")
}

func (c *Context) writeGeminiSettings(path string, servers map[string]json.RawMessage) error {
	raw, err := json.MarshalIndent(map[string]any{
		"_managed-by": "harnessator (mcpServers only; add model config around it or let it stand)",
		"mcpServers":  servers,
	}, "", "  ")
	if err != nil {
		return err
	}
	return c.writeFile(path, append(raw, '\n'))
}

func (c *Context) geminiLinkSpecs() ([]Spec, error) {
	specs, err := c.agentSkillsSpecs("gemini")
	if err != nil {
		return nil, err
	}
	for _, ch := range c.byCategory("agent") {
		src, ok := c.agentFileOrGeneric(ch, "gemini")
		if !ok {
			c.record("SKIP agent %s: no agents/gemini/%s.md variant or generic file - author one or retarget the component", ch.ID, ch.ID)
			continue
		}
		specs = append(specs,
			Spec{Dst: filepath.Join(c.Project, ".gemini", "agents", ch.ID+".md"), Src: src})
	}
	return specs, nil
}

// writeGeminiCommand emits a custom command TOML: description + prompt.
// Bodies containing triple quotes fall back to an escaped basic string -
// honest TOML or no file at all.
func (c *Context) writeGeminiCommand(dst string, ch resolve.Choice) error {
	src, err := c.componentSrc(ch)
	if err != nil {
		return err
	}
	if c.DryRun {
		c.record("generate %s (gemini command)", dst)
		return nil
	}
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	desc := ch.Component.Description
	if desc == "" {
		desc = ch.ID
	}
	prompt := strings.TrimRight(string(body), "\n")
	var rendered string
	if strings.Contains(prompt, `"""`) {
		rendered = "description = " + strconv.Quote(desc) + "\nprompt = " + strconv.Quote(prompt) + "\n"
	} else {
		rendered = "description = " + strconv.Quote(desc) + "\nprompt = \"\"\"\n" + prompt + "\n\"\"\"\n"
	}
	return c.writeFile(dst, []byte(geminiMarker+"\n"+rendered))
}
