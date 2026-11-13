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
	"sort"

	"harnessator.org/harnessator/internal/resolve"
)

// Pi renders the plan as pi-packages WITHOUT pi's own git machinery
// (pi would re-clone per project into .pi/git). Instead,
// one local-path package farm per source lives in
// .harnessator/pi/<source>/ - conventional dirs of symlinks into the
// pinned store checkout - and .pi/settings.json declares them.
// pi loads local packages "without copying"; identity is the absolute
// path, so enable/disable maps to farm membership, not filters.
//
// First run needs project trust: `pi` asks once, or run
// `pi install --local --approve` style approval interactively; setup
// prints the hint.
type Pi struct{}

// Name implements Adapter.
func (*Pi) Name() string { return "pi" }

// farmSource returns the sources with pi-renderable components, sorted.
func (c *Context) farmSources() []string {
	seen := map[string]bool{}
	var out []string
	for _, cat := range []string{"skill", "prompt", "workflow", "hook", "tool"} {
		for _, ch := range c.byCategory(cat) {
			if targetsInclude(ch, "pi") && !seen[ch.Source] {
				seen[ch.Source] = true
				out = append(out, ch.Source)
			}
		}
	}
	sort.Strings(out)
	return out
}

func farmDir(project, source string) string {
	return filepath.Join(project, ".harnessator", "pi", source)
}

// ManagedDirs implements Specer.
func (*Pi) ManagedDirs(c *Context) []string {
	var dirs []string
	for _, s := range c.farmSources() {
		f := farmDir(c.Project, s)
		dirs = append(dirs,
			filepath.Join(f, "skills"),
			filepath.Join(f, "prompts"),
			filepath.Join(f, "extensions"))
	}
	return dirs
}

// Specs implements Specer.
func (*Pi) Specs(c *Context) ([]Spec, error) {
	specs, _, _, err := c.piSpecs()
	if err != nil {
		return nil, err
	}
	for _, s := range c.farmSources() {
		specs = append(specs, Spec{Dst: filepath.Join(farmDir(c.Project, s), "package.json")})
	}
	specs = append(specs, Spec{Dst: filepath.Join(c.Project, ".pi", "settings.json")})
	return specs, nil
}

// Apply implements Adapter.
func (*Pi) Apply(c *Context) error {
	specs, farms, extensionsBySource, err := c.piSpecs()
	if err != nil {
		return err
	}
	if err := applySpecs(c, specs, (*Pi)(nil).ManagedDirs(c)); err != nil {
		return err
	}
	for _, s := range c.farmSources() {
		piRes := map[string]any{}
		if len(farms[s].skillsRel) > 0 {
			piRes["skills"] = farms[s].skillsRel
		}
		if len(farms[s].promptsRel) > 0 {
			piRes["prompts"] = farms[s].promptsRel
		}
		if len(extensionsBySource[s]) > 0 {
			piRes["extensions"] = extensionsBySource[s]
		}
		pkg := map[string]any{
			"name":     "harnessator-" + s,
			"keywords": []string{"pi-package"},
			"pi":       piRes,
		}
		raw, err := json.MarshalIndent(pkg, "", "  ")
		if err != nil {
			return err
		}
		if err := c.writeFile(filepath.Join(farmDir(c.Project, s), "package.json"), append(raw, '\n')); err != nil {
			return err
		}
	}
	if len(farms) > 0 {
		var rel []string
		for _, s := range c.farmSources() {
			rel = append(rel, filepath.Join("..", ".harnessator", "pi", s))
		}
		raw, err := json.MarshalIndent(map[string]any{"packages": rel}, "", "  ")
		if err != nil {
			return err
		}
		if err := c.writeFile(filepath.Join(c.Project, ".pi", "settings.json"), append(raw, '\n')); err != nil {
			return err
		}
		c.record("pi project trust is required on first run (approve once in pi)")
	}
	c.noteUnsupported("pi", "mcp", "agent", "rule")
	governanceCheck(c, "pi")
	if n := len(c.byCategory("rule")); n > 0 {
		c.record("%d rule(s) reach pi via AGENTS.md context loading", n)
	}
	return nil
}

type farmContent struct {
	skillsRel  []string
	promptsRel []string
}

// piSpecs builds link specs, farm resource lists and extension entries.
func (c *Context) piSpecs() ([]Spec, map[string]*farmContent, map[string][]string, error) {
	var specs []Spec
	farms := map[string]*farmContent{}
	exts := map[string][]string{}

	add := func(ch resolve.Choice, sub, relName string, dir bool) error {
		src, err := c.componentSrc(ch)
		if err != nil {
			return err
		}
		f := farmDir(c.Project, ch.Source)
		specs = append(specs, Spec{Dst: filepath.Join(f, sub, relName), Src: src, Dir: dir})
		farm := farms[ch.Source]
		if farm == nil {
			farm = &farmContent{}
			farms[ch.Source] = farm
		}
		rel := "./" + sub + "/" + relName
		if sub == "skills" {
			farm.skillsRel = append(farm.skillsRel, rel)
		} else {
			farm.promptsRel = append(farm.promptsRel, rel)
		}
		return nil
	}

	for _, ch := range c.byCategory("skill") {
		if targetsInclude(ch, "pi") {
			if err := add(ch, "skills", ch.ID, true); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	for _, cat := range []string{"prompt", "workflow"} {
		for _, ch := range c.byCategory(cat) {
			if targetsInclude(ch, "pi") {
				// prompts load as direct .md children
				if err := add(ch, "prompts", ch.ID+".md", false); err != nil {
					return nil, nil, nil, err
				}
			}
		}
	}

	// extensions: pi-compatible TS in the source's hooks/pi/ (convention,
	// parallel to hooks/opencode/); none exist yet - skipped per source.
	for _, s := range c.farmSources() {
		dir := filepath.Join(c.pin(s).Dir, "hooks", "pi")
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		f := farmDir(c.Project, s)
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".ts" {
				continue
			}
			specs = append(specs, Spec{
				Dst: filepath.Join(f, "extensions", e.Name()),
				Src: filepath.Join(dir, e.Name()),
			})
			exts[s] = append(exts[s], "./extensions/"+e.Name())
		}
	}
	return specs, farms, exts, nil
}
