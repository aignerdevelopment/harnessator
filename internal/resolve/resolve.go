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

// Package resolve turns a project config plus the fetched source manifests
// into a deterministic enablement plan: which components, from where, and
// why (provenance). It is pure - no git, no filesystem writes - so the
// later `why`/`diff`/TUI/web frontends can all read the same structure.
//
// Semantics:
//   - templates expand the named profile of the referenced source;
//   - select.<category>.include adds components, "*" adds all of that
//     source category;
//   - always rules are unconditionally added (and unexcludable - an
//     exclude hitting one is reported, not obeyed);
//   - excludes apply last, across the board;
//   - component targets must intersect the project targets or the
//     component is skipped (reported, never silent);
//   - precedence personal > project > team > corporate only matters for
//     conflicts; same-category same-id from multiple sources is a warning
//     with deterministic first-wins (source name order).
package resolve

import (
	"fmt"
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/schema"
)

// Choice is one enabled component with its provenance.
type Choice struct {
	Source    string
	Category  string // singular, e.g. "rule"
	ID        string
	Component config.Component
	Reason    string // e.g. `template "corp:default"`, `select.rule.include`, "always"
}

// ID returns the qualified harnessator id <source>:<category>/<id>.
func (c Choice) QualifiedID() string {
	return c.Source + ":" + c.Category + "/" + c.ID
}

// Skip is a component removed from the plan, with its reason.
type Skip struct {
	Choice   Choice
	Reason   string // "target mismatch", "excluded", ...
	Conflict string // additional note, e.g. exclude vs always
}

// Plan is the resolution result.
type Plan struct {
	Targets  []string // sorted project targets
	Enabled  []Choice
	Skips    []Skip
	Warnings []string
}

// TargetsSorted returns the project's harness targets in stable order.
func (p *Plan) TargetsSorted() []string { return p.Targets }

// ByCategory groups enabled choices for adapters and reporting.
func (p *Plan) ByCategory() map[string][]Choice {
	m := map[string][]Choice{}
	for _, c := range p.Enabled {
		m[c.Category] = append(m[c.Category], c)
	}
	return m
}

// Resolve computes the plan. manifests must contain every source named by
// the project config (gitops/store ensures that).
func Resolve(p *config.ProjectConfig, manifests map[string]*config.SourceManifest) (*Plan, error) {
	plan := &Plan{}
	targets := append([]string{}, p.Targets...)
	sort.Strings(targets)
	plan.Targets = targets
	for _, s := range p.Sources {
		if _, ok := manifests[s.Name]; !ok {
			return nil, fmt.Errorf("source %q: manifest not fetched", s.Name)
		}
	}
	if err := config.ValidateProject(p, manifests); err != nil {
		return nil, err
	}

	type key struct{ src, cat, id string }
	selected := map[key]Choice{}
	add := func(c Choice) {
		k := key{c.Source, c.Category, c.ID}
		if prev, dup := selected[k]; dup {
			// template + always (or two templates) is provenance merge,
			// not a conflict: fold "always" into the recorded reason.
			if c.Reason == "always" && prev.Reason != "always" {
				prev.Reason += " +always"
				selected[k] = prev
			}
			return
		}
		selected[k] = c
	}

	// 1. templates (profiles)
	for _, ref := range p.Templates {
		srcName, profName, ok := strings.Cut(ref, ":")
		if !ok {
			return nil, fmt.Errorf("template %q malformed", ref)
		}
		m := manifests[srcName]
		prof, ok := m.Profiles[profName]
		if !ok {
			return nil, fmt.Errorf("template %q: profile not found in source %q", ref, srcName)
		}
		for _, cat := range sortedKeys(prof) {
			for _, id := range prof[cat] {
				if id == "*" {
					for cid, comp := range m.Components[cat] {
						add(Choice{srcName, cat, cid, comp, "template " + ref + via(p, "template\x00"+ref)})
					}
					continue
				}
				comp, ok := m.Components[cat][id]
				if !ok {
					return nil, fmt.Errorf("template %q: %s:%s/%s not defined", ref, srcName, cat, id)
				}
				add(Choice{srcName, cat, id, comp, "template " + ref + via(p, "template\x00"+ref)})
			}
		}
	}

	// 2. select includes
	for _, cat := range sortedKeysAny(p.Select) {
		sel := p.Select[cat]
		for _, id := range sel.Include {
			if id == "*" {
				for _, srcName := range sourceOrder(p) {
					m := manifests[srcName]
					for cid, comp := range m.Components[cat] {
						add(Choice{srcName, cat, cid, comp, "select." + cat + ".include *" + via(p, "include\x00"+cat+"\x00*")})
					}
				}
				continue
			}
			wantSrc, rest, isQ := strings.Cut(id, ":")
			bare := id
			if isQ {
				bare = rest
			}
			matched := false
			for _, src := range sourceOrder(p) {
				if isQ && src != wantSrc {
					continue
				}
				comp, ok := manifests[src].Components[cat][bare]
				if !ok {
					continue
				}
				// bare ids bind to the FIRST source (sorted name order)
				// that defines them; qualified ids are exact.
				matched = true
				add(Choice{src, cat, bare, comp, "select." + cat + ".include" + via(p, "include\x00"+cat+"\x00"+id)})
				break
			}
			if !matched {
				return nil, fmt.Errorf("select.%s.include: %q not defined by any source", cat, id)
			}
		}
	}

	// 3. always rules (unconditional)
	for _, src := range sourceOrder(p) {
		m := manifests[src]
		for _, cat := range sortedKeys(m.Components) {
			if cat != "rule" {
				continue
			}
			for _, id := range sortedKeys(m.Components[cat]) {
				comp := m.Components[cat][id]
				if comp.Always {
					add(Choice{src, cat, id, comp, "always"})
				}
			}
		}
	}

	// 4. excludes + target filter -> final lists. Exclude entries use the
	// same syntax as includes: "id" (all sources) or "source:id".
	exBare := map[string]bool{}
	exQual := map[string]bool{}
	for cat, sel := range p.Select {
		for _, id := range sel.Exclude {
			if src, bare, isQ := strings.Cut(id, ":"); isQ {
				exQual[src+"|"+cat+"/"+bare] = true
			} else {
				exBare[cat+"/"+id] = true
			}
		}
	}
	ex := func(c Choice) bool {
		return exBare[c.Category+"/"+c.ID] || exQual[c.Source+"|"+c.Category+"/"+c.ID]
	}

	var keys []key
	for k := range selected {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		switch {
		case a.src != b.src:
			return a.src < b.src
		case a.cat != b.cat:
			return a.cat < b.cat
		default:
			return a.id < b.id
		}
	})

	targetSet := map[string]bool{}
	for _, t := range p.Targets {
		targetSet[t] = true
	}

	for _, k := range keys {
		c := selected[k]
		if ex(c) {
			if c.Component.Always {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("%s: excluded via select but marked always - kept", c.QualifiedID()))
				plan.Enabled = append(plan.Enabled, c)
				continue
			}
			plan.Skips = append(plan.Skips, Skip{Choice: c, Reason: "excluded by select." + c.Category + via(p, "exclude\x00"+c.Category+"\x00"+c.Source+":"+c.Category+"/"+c.ID, "exclude\x00"+c.Category+"\x00"+c.Category+"/"+c.ID)})
			continue
		}
		if len(c.Component.Targets) > 0 && !intersects(c.Component.Targets, targetSet) {
			plan.Skips = append(plan.Skips, Skip{Choice: c,
				Reason: "target mismatch (component: " + strings.Join(c.Component.Targets, ",") + ")"})
			continue
		}
		plan.Enabled = append(plan.Enabled, c)
	}

	sort.Strings(plan.Warnings)
	return plan, nil
}

func intersects(list []string, set map[string]bool) bool {
	for _, v := range list {
		if set[v] {
			return true
		}
	}
	return false
}

// sourceOrder returns declared sources sorted by name (deterministic).
func sourceOrder(p *config.ProjectConfig) []string {
	var names []string
	for _, s := range p.Sources {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names
}

// Categories used by adapters iterating the plan.
func CategoriesInOrder() []string { return schema.Categories }

func sortedKeys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysAny(m map[string]config.Selection) []string { return sortedKeys(m) }

// via returns a provenance suffix (" [via team]") for the first key the
// merged effective config recorded a scope layer for; empty for
// standalone-loaded configs, so single-file behavior is unchanged.
func via(p *config.ProjectConfig, keys ...string) string {
	for _, k := range keys {
		if o := p.Origin(k); o != "" {
			return " [via " + o + "]"
		}
	}
	return ""
}
