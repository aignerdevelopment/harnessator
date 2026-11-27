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

// Package index implements the local-first registry: a store-level JSON
// index over every component of the pinned sources, built on the frozen
// source-format contract. It is rebuildable
// cache data - `index build` regenerates it, `index search|show` read it
// without any project or network context. No server component: the
// index is exactly what a future corporate hub would cron-produce.
package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/gitops"
	"harnessator.org/harnessator/internal/schema"
)

// Schema bumps on breaking entry-layout changes.
const Schema = 1

// Entry is one component with its source provenance.
type Entry struct {
	Source      string   `json:"source"`
	ID          string   `json:"id"` // qualified: <source>:<category>/<name>
	Category    string   `json:"category"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Targets     []string `json:"targets,omitempty"`
	License     string   `json:"license,omitempty"`
	Path        string   `json:"path"`
	Origin      string   `json:"origin,omitempty"` // harvest provenance
	Git         string   `json:"git"`
	Range       string   `json:"range,omitempty"`
	Resolved    string   `json:"resolved,omitempty"`
	SHA         string   `json:"sha,omitempty"`
}

// File is the serialized store index.
type File struct {
	Schema  int      `json:"schema"`
	Built   string   `json:"built"` // RFC3339
	Entries []Entry  `json:"entries"`
	Sources []string `json:"sources"` // source names covered by the build
}

// Build renders index entries for the given manifest+pin sets, sorted
// deterministically (source, category, id).
func Build(manifests map[string]*config.SourceManifest, pins map[string]*gitops.Pinned) []Entry {
	var out []Entry
	for _, name := range sortedKeys(manifests) {
		m := manifests[name]
		p := pins[name]
		for _, cat := range schema.Categories {
			for _, id := range sortedCompKeys(m.Components[cat]) {
				c := m.Components[cat][id]
				e := Entry{
					Source: name, ID: name + ":" + cat + "/" + id,
					Category: cat, Name: id, Description: c.Description,
					Tags: c.Tags, Targets: c.Targets, License: m.Package.License,
					Path: c.Path, Origin: c.Origin,
				}
				if p != nil {
					e.Git, e.Range, e.Resolved, e.SHA = p.Git, p.Range, p.Resolved, p.SHA
				}
				out = append(out, e)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Merge replaces the entries of the rebuilt sources inside base and
// keeps every other source's entries (multi-project store story).
func Merge(base *File, sources []string, fresh []Entry) *File {
	rebuilt := map[string]bool{}
	for _, s := range sources {
		rebuilt[s] = true
	}
	entries := fresh
	if base != nil {
		for _, e := range base.Entries {
			if !rebuilt[e.Source] {
				entries = append(entries, e)
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Source != entries[j].Source {
			return entries[i].Source < entries[j].Source
		}
		return entries[i].ID < entries[j].ID
	})
	seen := map[string]bool{}
	names := []string{}
	for _, s := range sources {
		if !seen[s] {
			seen[s] = true
			names = append(names, s)
		}
	}
	if base != nil {
		for _, e := range entries {
			if !seen[e.Source] {
				seen[e.Source] = true
				names = append(names, e.Source)
			}
		}
	}
	sort.Strings(names)
	return &File{Schema: Schema, Entries: entries, Sources: names}
}

// Load reads the store index; missing file = empty index, no error.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &File{Schema: Schema}, nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Schema > Schema {
		return nil, fmt.Errorf("%s: index schema %d newer than this harnessator understands (%d) - upgrade", path, f.Schema, Schema)
	}
	return &f, nil
}

// Write stores the index atomically (rebuildable data, safe to replace).
func Write(path string, f *File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f.Schema = Schema
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Query filters by free text (id, description, tags, category) and
// optional exact category.
func Query(f *File, text, category string) []Entry {
	needle := strings.ToLower(strings.TrimSpace(text))
	var out []Entry
	for _, e := range f.Entries {
		if category != "" && e.Category != category {
			continue
		}
		if needle == "" || strings.Contains(strings.ToLower(e.ID+" "+e.Description+" "+
			strings.Join(e.Tags, " ")+" "+e.Category), needle) {
			out = append(out, e)
		}
	}
	return out
}

// Find resolves a reference: exact qualified id, else bare cat/id or id
// only when unambiguous across the index.
func Find(f *File, ref string) (Entry, bool) {
	for _, e := range f.Entries {
		if e.ID == ref {
			return e, true
		}
	}
	var hit Entry
	n := 0
	for _, e := range f.Entries {
		if e.Category+"/"+e.Name == ref || e.Name == ref {
			hit, n = e, n+1
		}
	}
	if n == 1 {
		return hit, true
	}
	return Entry{}, false
}

func sortedKeys(m map[string]*config.SourceManifest) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedCompKeys(m map[string]config.Component) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
