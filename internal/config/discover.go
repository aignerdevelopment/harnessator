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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/schema"
)

// Discover support (source format v1.1, schema 2). A manifest that
// declares [discover] registers content FAMILIES live at load time:
// every qualifying directory matching the pattern becomes a component,
// its description read from frontmatter then and there. Nothing is
// copied into the manifest, so nothing can drift - the property that
// makes third-party pointer manifests trustworthy and self-syncing
// bridge refreshes possible.
//
// Expansion runs inside LoadSourceManifest, the single choke point all
// consumers share, so no code path can observe an unexpanded manifest.

// expandDiscover synthesizes discovered components into m.Components.
// root is the manifest's own directory (component paths are relative
// to it - the same convention as explicit entries in multi-source repos).
func (m *SourceManifest) expandDiscover(root string) error {
	if len(m.Discover) == 0 {
		return nil
	}
	if m.Schema < schema.SourceSchemaDiscover {
		return fmt.Errorf("%s: [discover] requires schema = %d (v1.1)",
			filepath.Join(root, "harnessator.toml"), schema.SourceSchemaDiscover)
	}
	cats := make([]string, 0, len(m.Discover))
	for c := range m.Discover {
		cats = append(cats, c)
	}
	sort.Strings(cats)

	for _, cat := range cats {
		if cat != "skill" {
			return fmt.Errorf("discover: category %q not supported (v1.1 discovers skills directories; declare the rest explicitly)", cat)
		}
		dir, err := discoverDir(m.Discover[cat])
		if err != nil {
			return err
		}
		base := filepath.Join(root, filepath.FromSlash(dir))
		entries, err := os.ReadDir(base)
		if err != nil {
			return fmt.Errorf("discover: skill pattern %q: %w", m.Discover[cat], err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			rel := dir + "/" + name
			if _, err := os.Stat(filepath.Join(base, name, "SKILL.md")); err != nil {
				continue // not a skill directory
			}
			if !schema.ValidID(name) {
				return fmt.Errorf("discover: %s: directory name is not a valid component id", rel)
			}
			if m.Components == nil {
				m.Components = map[string]map[string]Component{}
			}
			if m.Components[cat] == nil {
				m.Components[cat] = map[string]Component{}
			}
			if _, exists := m.Components[cat][name]; exists {
				continue // explicit entries win; discovery only fills gaps
			}
			desc, derr := skillFrontmatterDescription(filepath.Join(base, name, "SKILL.md"))
			if derr != nil {
				return fmt.Errorf("discover: %s: %w", rel, derr)
			}
			m.Components[cat][name] = Component{
				Path:        rel,
				Description: desc,
				Origin:      "discovered from " + rel,
			}
		}
	}
	return nil
}

// discoverDir validates the restricted pattern grammar: exactly one
// trailing "/*" over a repo-relative, non-escaping directory.
func discoverDir(pattern string) (string, error) {
	dir, ok := strings.CutSuffix(pattern, "/*")
	if !ok || dir == "" {
		return "", fmt.Errorf("discover: pattern %q must be a repository-relative directory plus /* (e.g. \"skills/*\")", pattern)
	}
	if filepath.IsAbs(dir) || strings.Contains(dir, "*") || strings.Contains(dir, "..") {
		return "", fmt.Errorf("discover: pattern %q must stay inside the source repository", pattern)
	}
	return dir, nil
}

// skillFrontmatterDescription reads the description field of a SKILL.md
// frontmatter block; absent frontmatter or field yields "". Folded
// continuation lines (indented) are appended, matching the convention
// harvest's own frontmatter reader tolerates.
func skillFrontmatterDescription(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", nil
	}
	var desc string
	key := ""
	unquote := func(v string) string {
		v = strings.TrimSpace(v)
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		return v
	}
	for _, ln := range lines[1:] {
		if strings.TrimSpace(ln) == "---" {
			break
		}
		indented := strings.HasPrefix(ln, " ") || strings.HasPrefix(ln, "\t")
		if !indented {
			k, v, ok := strings.Cut(ln, ":")
			if !ok {
				key = ""
				continue
			}
			switch strings.TrimSpace(k) {
			case "description":
				key = "description"
				desc = unquote(v)
			default:
				key = "" // any other key ends the description block
			}
			continue
		}
		if key == "description" {
			if t := strings.TrimSpace(ln); t != "" {
				if desc != "" {
					desc += " "
				}
				desc += t
			}
		}
	}
	return desc, nil
}
