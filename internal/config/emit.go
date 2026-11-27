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

package config

import (
	"bytes"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// ProjectHeader is written verbatim above every canonical project file so
// humans immediately learn where the truth lives.
const ProjectHeader = `# harnessator project configuration (committed; harnessator.lock is its result)
# Edit by hand or via ` + "`harnessator enable/disable/source/template`" + ` - commands
# rewrite this file canonically, which drops hand-written comments.
# Top-level scalars must stay above the [[source]] tables (TOML scoping).
`

// MarshalProject serializes a project config canonically (stable field
// order per ProjectConfig's declaration, sorted maps by the toml encoder).
// The output always loads back via LoadProjectConfig (round-trip test).
func MarshalProject(c *ProjectConfig) ([]byte, error) {
	c.Kind = KindProject
	if c.Schema == 0 {
		c.Schema = 1
	}
	var buf bytes.Buffer
	buf.WriteString(ProjectHeader)
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteProject atomically rewrites a project config file.
func WriteProject(path string, c *ProjectConfig) error {
	raw, err := MarshalProject(c)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// AddTemplate appends a template ref if absent; reports whether it changed.
func (c *ProjectConfig) AddTemplate(ref string) bool {
	return addString(&c.Templates, ref)
}

// AddSource appends a source if its name is unused; errors on duplicate.
func (c *ProjectConfig) AddSource(s Source) error {
	for _, x := range c.Sources {
		if x.Name == s.Name {
			return &dupErr{s.Name}
		}
	}
	c.Sources = append(c.Sources, s)
	return nil
}

// RemoveSource drops a source by name (and its qualified leftovers in
// select entries are intentionally kept: they simply stop resolving).
func (c *ProjectConfig) RemoveSource(name string) bool {
	out := c.Sources[:0]
	removed := false
	for _, x := range c.Sources {
		if x.Name == name && !removed {
			removed = true
			continue
		}
		out = append(out, x)
	}
	c.Sources = out
	return removed
}

// Include adds a component reference to select.<cat>.include (creating the
// category entry) and removes it from excludes.
func (c *ProjectConfig) Include(cat, ref string) {
	if c.Select == nil {
		c.Select = map[string]Selection{}
	}
	sel := c.Select[cat]
	sel.Exclude = removeString(sel.Exclude, ref)
	sel.Include = appendUnique(sel.Include, ref)
	c.Select[cat] = sel
}

// Exclude adds a component reference to select.<cat>.exclude and removes it
// from includes.
func (c *ProjectConfig) Exclude(cat, ref string) {
	if c.Select == nil {
		c.Select = map[string]Selection{}
	}
	sel := c.Select[cat]
	sel.Include = removeString(sel.Include, ref)
	sel.Exclude = appendUnique(sel.Exclude, ref)
	c.Select[cat] = sel
}

type dupErr struct{ name string }

func (e *dupErr) Error() string { return "source " + e.name + " already declared" }

func addString(p *[]string, v string) bool {
	for _, x := range *p {
		if x == v {
			return false
		}
	}
	*p = append(*p, v)
	return true
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func removeString(list []string, v string) []string {
	out := list[:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// SplitRefID normalizes a component reference into source ("" if bare),
// category and id. cat/id, src:cat/id and src:id (cat guessed via lookup
// by the caller) are accepted shapes.
func SplitRefID(ref string) (src, cat, id string, ok bool) {
	rest := ref
	if s, r, hasColon := strings.Cut(rest, ":"); hasColon {
		src, rest = s, r
	}
	if c, r, hasSlash := strings.Cut(rest, "/"); hasSlash {
		cat, id = c, r
	} else {
		id = rest
	}
	if id == "" {
		return "", "", "", false
	}
	return src, cat, id, true
}
