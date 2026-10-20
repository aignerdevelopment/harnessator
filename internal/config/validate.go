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
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"harnessator.org/harnessator/internal/schema"
	"harnessator.org/harnessator/internal/semverrange"
)

// Issues accumulates validation problems with file context so a single run
// can report everything wrong instead of only the first error.
type Issues struct {
	File     string
	problems []string
}

// Err reports whether anything was collected.
func (i *Issues) Err() error {
	if len(i.problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(i.problems, "\n"))
}

// Addf records one problem.
func (i *Issues) Addf(format string, args ...any) {
	i.problems = append(i.problems, fmt.Sprintf("%s: "+format, append([]any{i.File}, args...)...))
}

// ValidateSource checks a decoded source manifest. root is the directory the
// manifest lives in ("" skips on-disk path checks).
func ValidateSource(m *SourceManifest, root string) error {
	i := &Issues{File: "manifest"}
	validateCommon(i, m.Schema, KindSource)

	if m.Package.Name == "" {
		i.Addf("package.name is required")
	} else if !schema.ValidID(m.Package.Name) {
		i.Addf("package.name %q is not a valid id", m.Package.Name)
	}

	for _, s := range m.Sources {
		validateSourceRef(i, s)
	}
	seen := map[string]bool{}
	for _, s := range m.Sources {
		if seen[s.Name] {
			i.Addf("duplicate child source %q", s.Name)
		}
		seen[s.Name] = true
	}

	for cat, comps := range m.Components {
		if !schema.IsCategory(cat) {
			i.Addf("unknown component category %q", cat)
			continue
		}
		for id, c := range comps {
			if !schema.ValidID(id) {
				i.Addf("component %s/%q has an invalid id", cat, id)
			}
			if c.Path == "" {
				i.Addf("component %s/%q: path is required", cat, id)
			} else if root != "" {
				p := path.Join(root, c.Path)
				if _, err := os.Stat(p); err != nil {
					i.Addf("component %s/%q: path %q not found", cat, id, c.Path)
				}
			}
			for _, t := range c.Targets {
				if !schema.IsTarget(t) {
					i.Addf("component %s/%q: unknown target %q", cat, id, t)
				}
			}
			for _, cap := range c.Capabilities {
				if !schema.IsCapability(cap) {
					i.Addf("component %s/%q: unknown capability %q", cat, id, cap)
				}
			}
		}
	}

	for name, prof := range m.Profiles {
		if !schema.ValidID(name) {
			i.Addf("profile %q has an invalid name", name)
		}
		for cat, ids := range prof {
			if !schema.IsCategory(cat) {
				i.Addf("profile %q: unknown category %q", name, cat)
				continue
			}
			for _, id := range ids {
				if id == "*" {
					continue
				}
				if _, ok := m.Components[cat][id]; !ok {
					i.Addf("profile %q: %s/%q is not defined", name, cat, id)
				}
			}
		}
	}

	return i.Err()
}

// ValidateProject checks a decoded project config. When sources is non-nil
// (i.e. the referenced manifests have been fetched), template/profile/id
// references are cross-checked against them; with sources == nil the check
// is syntax and self-consistency only.
func ValidateProject(c *ProjectConfig, sources map[string]*SourceManifest) error {
	i := &Issues{File: "harnessator.toml"}
	validateCommon(i, c.Schema, KindProject)

	if len(c.Targets) == 0 {
		i.Addf("at least one target is required")
	}
	seenTarget := map[string]bool{}
	for _, t := range c.Targets {
		if !schema.IsTarget(t) {
			i.Addf("unknown target %q", t)
		}
		if seenTarget[t] {
			i.Addf("duplicate target %q", t)
		}
		seenTarget[t] = true
	}

	seen := map[string]bool{}
	for _, s := range c.Sources {
		validateSourceRef(i, s)
		if seen[s.Name] {
			i.Addf("duplicate source %q", s.Name)
		}
		seen[s.Name] = true
	}

	if sources != nil {
		for _, ref := range c.Templates {
			src, prof, ok := splitRef(ref)
			if !ok {
				i.Addf("template %q must look like <source>:<profile>", ref)
				continue
			}
			m, ok := sources[src]
			if !ok {
				i.Addf("template %q references unknown source %q", ref, src)
				continue
			}
			if _, ok := m.Profiles[prof]; !ok {
				i.Addf("template %q: source %q has no profile %q", ref, src, prof)
			}
		}

		for cat, sel := range c.Select {
			if !schema.IsCategory(cat) {
				i.Addf("select: unknown category %q", cat)
				continue
			}
			for _, id := range append(append([]string{}, sel.Include...), sel.Exclude...) {
				if id == "*" {
					continue
				}
				src, _, ok := splitQualified(id)
				if ok {
					if _, known := sources[src]; !known {
						i.Addf("select.%s: %q references unknown source %q", cat, id, src)
					}
				} else if !schema.ValidID(id) {
					i.Addf("select.%s: invalid id %q", cat, id)
				}
			}
		}

		if c.Provider != nil && c.Provider.Profile != "" {
			validateComponentRef(i, c.Provider.Profile, sources, "provider.profile")
		}
		if c.Policy != nil {
			for _, p := range c.Policy.Profiles {
				validateComponentRef(i, p, sources, "policy.profiles")
			}
		}
	} else {
		for cat := range c.Select {
			if !schema.IsCategory(cat) {
				i.Addf("select: unknown category %q", cat)
			}
		}
		for _, ref := range c.Templates {
			if _, _, ok := splitRef(ref); !ok {
				i.Addf("template %q must look like <source>:<profile>", ref)
			}
		}
	}

	if c.Corporate != nil {
		switch c.Corporate.LockMode {
		case "", "soft", "strict":
		default:
			i.Addf("corporate.lock-mode must be \"strict\" or \"soft\", got %q", c.Corporate.LockMode)
		}
	}

	return i.Err()
}

func validateCommon(i *Issues, schemaVer int, wantKind string) {
	if wantKind != KindSource && wantKind != KindProject {
		i.Addf("internal error: wrong kind %q", wantKind)
	}
	if schemaVer != schema.SchemaVersion {
		i.Addf("unsupported schema version %d (this build handles %d)",
			schemaVer, schema.SchemaVersion)
	}
}

func validateSourceRef(i *Issues, s Source) {
	if s.Name == "" {
		i.Addf("source entry with git %q: name is required", s.Git)
	} else if !schema.ValidID(s.Name) {
		i.Addf("source name %q is not a valid id", s.Name)
	}
	if s.Git == "" {
		i.Addf("source %q: git url is required", s.Name)
	}
	if s.Ref == "" {
		i.Addf("source %q: ref is required", s.Name)
	} else if _, err := semverrange.Parse(s.Ref); err != nil {
		i.Addf("source %q: %v", s.Name, err)
	}
	if s.Layer != "" && !schema.IsLayer(s.Layer) {
		i.Addf("source %q: unknown layer %q", s.Name, s.Layer)
	}
	if s.Manifest != "" {
		if path.IsAbs(s.Manifest) || strings.Contains(s.Manifest, "..") {
			i.Addf("source %q: manifest must be a repo-relative path without \"..\"", s.Name)
		}
	}
}

func validateComponentRef(i *Issues, ref string, sources map[string]*SourceManifest, field string) {
	src, id, ok := splitQualified(ref)
	if !ok {
		i.Addf("%s %q must look like <source>:<category>/<id>", field, ref)
		return
	}
	m, ok := sources[src]
	if !ok {
		i.Addf("%s %q references unknown source %q", field, ref, src)
		return
	}
	cat, name, ok := strings.Cut(id, "/")
	if !ok || !schema.IsCategory(cat) {
		i.Addf("%s %q must look like <source>:<category>/<id>", field, ref)
		return
	}
	if _, exists := m.Components[cat][name]; !exists {
		i.Addf("%s %q is not defined by source %q", field, ref, src)
	}
}

// splitQualified splits "<a>:<b>" on the first colon.
func splitQualified(s string) (a, b string, ok bool) {
	a, b, ok = strings.Cut(s, ":")
	if !ok || a == "" || b == "" {
		return "", "", false
	}
	return a, b, true
}

// splitRef splits a "<source>:<profile>" template reference.
func splitRef(s string) (src, name string, ok bool) { return splitQualified(s) }
