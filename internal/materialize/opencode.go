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

	"harnessator.org/harnessator/internal/dialect"
	"harnessator.org/harnessator/internal/gitops"
	"harnessator.org/harnessator/internal/resolve"
)

// fragmentModelCount reads the rendered model count out of an opencode
// provider fragment (for the setup log line).
func fragmentModelCount(frag map[string]any, name string) int {
	prov, _ := frag["provider"].(map[string]any)
	entry, _ := prov[name].(map[string]any)
	models, _ := entry["models"].(map[string]any)
	return len(models)
}

// OpenCode renders the plan for the opencode harness:
// .opencode/{skill,agent,command,plugin,tool} links plus a fully generated
// .opencode/opencode.json (instructions redirect + mcp servers). This is
// the Go port of the battle-tested opencode sync behavior.
type OpenCode struct{}

// Name implements Adapter.
func (*OpenCode) Name() string { return "opencode" }

// Apply implements Adapter.
func (*OpenCode) Apply(c *Context) error {
	specs, err := c.openCodeSpecs()
	if err != nil {
		return err
	}
	if err := applySpecs(c, specs, (*OpenCode)(nil).ManagedDirs(c)); err != nil {
		return err
	}

	servers, err := c.mcpCollection("opencode")
	if err != nil {
		return err
	}
	cfg := map[string]any{
		"$schema":      "https://opencode.ai/config.json",
		"instructions": c.instructionsList(),
		"mcp":          servers,
	}
	if c.Provider != nil {
		if d, ok := dialect.For((*OpenCode)(nil).Name()); ok {
			res, err := d.Govern(c.Provider, c.Policy)
			if err != nil {
				c.record("SKIP provider %s: %v", c.Provider.Name, err)
			} else {
				for k, v := range res.JSON {
					cfg[k] = v
				}
				c.record("provider %s rendered for opencode (%d approved model(s))", c.Provider.Name, fragmentModelCount(res.JSON, c.Provider.Name))
			}
		}
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := c.writeFile(filepath.Join(c.Project, ".opencode", "opencode.json"), raw); err != nil {
		return err
	}
	if n := len(c.byCategory("rule")); n > 0 {
		c.record("%d rule(s) rendered via AGENTS.md + instructions + active index", n)
	}
	governanceCheck(c, "opencode")
	return nil
}

// Specs implements Specer for doctor diffing.
func (*OpenCode) Specs(c *Context) ([]Spec, error) {
	specs, err := c.openCodeSpecs()
	if err != nil {
		return nil, err
	}
	return append(specs, Spec{Dst: filepath.Join(c.Project, ".opencode", "opencode.json")}), nil
}

// ManagedDirs implements Specer: full reset every apply, even when empty.
func (*OpenCode) ManagedDirs(c *Context) []string {
	base := filepath.Join(c.Project, ".opencode")
	var dirs []string
	for _, d := range []string{"skill", "agent", "command", "plugin", "tool"} {
		dirs = append(dirs, filepath.Join(base, d))
	}
	return dirs
}

// openCodeSpecs builds the link list shared by Apply and Specs.
func (c *Context) openCodeSpecs() ([]Spec, error) {
	base := filepath.Join(c.Project, ".opencode")
	var specs []Spec

	for _, ch := range c.byCategory("skill") {
		src, err := c.componentSrc(ch)
		if err != nil {
			return nil, err
		}
		specs = append(specs, Spec{Dst: filepath.Join(base, "skill", ch.ID), Src: src, Dir: true})
	}

	for _, cat := range []string{"prompt", "workflow"} {
		for _, ch := range c.byCategory(cat) {
			src, err := c.componentSrc(ch)
			if err != nil {
				return nil, err
			}
			specs = append(specs, Spec{Dst: filepath.Join(base, "command", ch.ID+".md"), Src: src})
		}
	}

	for _, ch := range c.byCategory("agent") {
		src, err := c.agentFile(ch, "opencode")
		if err != nil {
			return nil, err
		}
		specs = append(specs, Spec{Dst: filepath.Join(base, "agent", ch.ID+".md"), Src: src})
	}

	for _, ch := range c.byCategory("hook") {
		if !targetsInclude(ch, "opencode") {
			continue
		}
		fs, err := c.opencodePluginFiles(ch)
		if err != nil {
			return nil, err
		}
		specs = append(specs, fs...)
	}

	for _, ch := range c.byCategory("tool") {
		src, err := c.componentSrc(ch)
		if err != nil {
			return nil, err
		}
		if filepath.Ext(src) == ".ts" {
			specs = append(specs, Spec{Dst: filepath.Join(base, "tool", filepath.Base(src)), Src: src})
		}
		// directory tools are mirrored in .harnessator/tools by WriteShared
	}
	return specs, nil
}

// opencodePluginFiles lists the plugin TS files of a hook component's
// source (agentictools convention: hooks/opencode/*.ts).
func (c *Context) opencodePluginFiles(ch resolve.Choice) ([]Spec, error) {
	dir := filepath.Join(c.pin(ch.Source).Dir, "hooks", "opencode")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // no plugin dir in this source: nothing expected
	}
	var specs []Spec
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".ts" {
			continue
		}
		specs = append(specs, Spec{
			Dst: filepath.Join(c.Project, ".opencode", "plugin", e.Name()),
			Src: filepath.Join(dir, e.Name()),
		})
	}
	return specs, nil
}

// agentFile resolves <pin>/agents/<target>/<id>.md.
func (c *Context) agentFile(ch resolve.Choice, target string) (string, error) {
	pin := c.pin(ch.Source)
	if pin == nil {
		return "", fmt.Errorf("%s: not pinned", ch.QualifiedID())
	}
	p := filepath.Join(pin.Dir, "agents", target, ch.ID+".md")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("%s: no %s agent definition: %w", ch.QualifiedID(), target, err)
	}
	return p, nil
}

func sortedPinNames(m map[string]*gitops.Pinned) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// instructionsList is AGENTS.md + the pinned sources' AGENTS.md + active.md.
func (c *Context) instructionsList() []string {
	out := []string{"AGENTS.md"}
	seen := map[string]bool{}
	for _, name := range sortedPinNames(c.Pins) {
		p := c.Pins[name]
		agents := filepath.Join(p.Dir, "AGENTS.md")
		if fi, err := os.Stat(agents); err == nil && !fi.IsDir() && !seen[agents] {
			seen[agents] = true
			out = append(out, agents)
		}
	}
	out = append(out, filepath.Join(".harnessator", "active.md"))
	return out
}
