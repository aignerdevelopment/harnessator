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

// Package materialize renders a resolved plan into a project's harness
// directories: symlinks into the store's pinned checkouts plus fully
// generated config files. Adapters are honest: a component type without a
// native slot on a target is reported, never silently dropped.
package materialize

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/dialect"
	"harnessator.org/harnessator/internal/gitops"
	"harnessator.org/harnessator/internal/platform"
	"harnessator.org/harnessator/internal/policy"
	"harnessator.org/harnessator/internal/provider"
	"harnessator.org/harnessator/internal/resolve"
)

// Context carries everything an adapter needs for one setup run.
type Context struct {
	Project   string // project root
	Manifests map[string]*config.SourceManifest
	Pins      map[string]*gitops.Pinned // by source name
	Plan      *resolve.Plan
	Provider  *provider.Profile // governance: selected provider (nil = none)
	Policy    *policy.Effective // governance: effective policy (nil = no constraint)
	DryRun    bool
	Log       io.Writer

	writes []string // recorded operations (also the dry-run output)
}

// allowedModelIDs/defaultReal moved to internal/dialect with the 2026-10-19
// governance refactor: catalog filtering is dialect rendering knowledge, not
// adapter context state.

func (c *Context) pin(source string) *gitops.Pinned { return c.Pins[source] }

// record logs one materialization operation, honoring dry runs.
func (c *Context) record(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	c.writes = append(c.writes, line)
	fmt.Fprintln(c.Log, "  "+line)
}

// Report returns the recorded operations.
func (c *Context) Report() []string { return c.writes }

// Adapter materializes the plan for one harness target.
type Adapter interface {
	Name() string
	Apply(*Context) error
}

// Spec describes one expected materialization result - a link (Src set) or
// a generated file (Src empty). Adapters declare specs for real apply AND
// for "harnessator doctor" state diffing.
type Spec struct {
	Dst string // destination path inside the project
	Src string // store checkout path for links; "" for generated files
	Dir bool   // link is a directory
}

// Specer is implemented by adapters with link-based expectations. ManagedDirs
// are fully reset (their symlinks purged) on every apply/check, even when
// the current plan contributes no specs there - disabling the last
// component of a category must still clean up its stale links.
type Specer interface {
	Specs(*Context) ([]Spec, error)
	ManagedDirs(*Context) []string
}

// Registry maps target ids to adapters; "known but unimplemented" targets
// are registered as pending adapters so setup fails loudly.
var Registry = map[string]func() Adapter{
	"opencode":    func() Adapter { return &OpenCode{} },
	"claude":      func() Adapter { return &Claude{} },
	"codex":       func() Adapter { return &Codex{} },
	"antigravity": func() Adapter { return &Antigravity{} },
	"pi":          func() Adapter { return &Pi{} },
	"cursor":      func() Adapter { return &Cursor{} },
}

// Pending reports the component set for targets whose adapter has not
// landed yet, instead of pretending success.
type Pending struct{ Target string }

func (p *Pending) Name() string { return p.Target }

func (p *Pending) Apply(c *Context) error {
	c.record("SKIP %s: adapter not yet implemented (nothing written for this target)", p.Target)
	return nil
}

// ApplyAll runs the adapters for the project's targets in stable order.
func ApplyAll(c *Context) error {
	for _, t := range c.Plan.TargetsSorted() {
		newAd, ok := Registry[t]
		if !ok {
			return fmt.Errorf("unknown target %q", t)
		}
		ad := newAd()
		c.record("target %s:", ad.Name())
		if err := ad.Apply(c); err != nil {
			return fmt.Errorf("target %s: %w", ad.Name(), err)
		}
	}
	return nil
}

// applySpecs resets touched managed dirs, then creates the link specs
// (dry-run safe, via c.link/c.record).
func applySpecs(c *Context, specs []Spec, resetDirs []string) error {
	dirs := append([]string{}, resetDirs...)
	for _, s := range specs {
		if s.Src != "" {
			dirs = append(dirs, filepath.Dir(s.Dst))
		}
	}
	sort.Strings(dirs)
	prev := ""
	for _, d := range dirs {
		if d == prev {
			continue
		}
		prev = d
		resetLinkDir(c, d)
	}
	for _, s := range specs {
		if s.Src == "" {
			continue
		}
		if err := c.link(s.Src, s.Dst, s.Dir); err != nil {
			return err
		}
	}
	return nil
}

// SpecsByTarget returns the adapters' declared specs grouped by target,
// WITHOUT applying anything - the materialization contract exposed for
// tooling that must know planned links before setup (adopt's
// digest-verified original removal).
func SpecsByTarget(c *Context) (map[string][]Spec, error) {
	out := map[string][]Spec{}
	for _, t := range c.Plan.TargetsSorted() {
		newAd, ok := Registry[t]
		if !ok {
			return nil, fmt.Errorf("unknown target %q", t)
		}
		ad := newAd()
		sp, ok := ad.(Specer)
		if !ok {
			continue // pending adapter: nothing declared
		}
		specs, err := sp.Specs(c)
		if err != nil {
			return nil, err
		}
		out[t] = specs
	}
	return out, nil
}

// CheckSpecs diffs the adapters' declared expectations against the project
// filesystem without writing anything (harnessator doctor).
func CheckSpecs(c *Context) []string {
	var problems []string
	for _, t := range c.Plan.TargetsSorted() {
		newAd, ok := Registry[t]
		if !ok {
			problems = append(problems, fmt.Sprintf("target %q: unknown adapter", t))
			continue
		}
		ad := newAd()
		specer, ok := ad.(Specer)
		if !ok {
			continue // pending adapter: nothing to expect
		}
		specs, err := specer.Specs(c)
		if err != nil {
			problems = append(problems, fmt.Sprintf("target %s: %v", t, err))
			continue
		}
		want := map[string]Spec{}
		dirs := map[string]bool{}
		for _, s := range specs {
			want[s.Dst] = s
			if s.Src != "" {
				dirs[filepath.Dir(s.Dst)] = true
			}
		}
		for _, d := range specer.ManagedDirs(c) {
			dirs[d] = true
		}
		for _, s := range specs {
			rel, _ := filepath.Rel(c.Project, s.Dst)
			fi, err := os.Lstat(s.Dst)
			switch {
			case err != nil:
				if s.Src == "" {
					problems = append(problems, fmt.Sprintf("%s: generated file missing (%s)", t, rel))
				} else {
					problems = append(problems, fmt.Sprintf("%s: missing link %s", t, rel))
				}
			case s.Src == "": // generated file exists: ok
			case fi.Mode()&os.ModeSymlink == 0:
				problems = append(problems, fmt.Sprintf("%s: %s blocked by non-link file", t, rel))
			default:
				target, _ := os.Readlink(s.Dst)
				if target != s.Src {
					problems = append(problems, fmt.Sprintf("%s: %s points to %s, want %s", t, rel, target, s.Src))
				}
			}
		}
		for d := range dirs {
			entries, err := os.ReadDir(d)
			if err != nil {
				continue
			}
			for _, e := range entries {
				p := filepath.Join(d, e.Name())
				fi, err := os.Lstat(p)
				if err != nil || fi.Mode()&os.ModeSymlink == 0 {
					continue
				}
				if _, planned := want[p]; !planned {
					rel, _ := filepath.Rel(c.Project, p)
					problems = append(problems, fmt.Sprintf("%s: stray managed symlink %s (not in plan)", t, rel))
				}
			}
		}
	}
	return problems
}

// --- shared helpers -------------------------------------------------------

// resetLinkDir deletes only the symlinks directly inside dir: human-owned
// entries are never touched.
func resetLinkDir(c *Context, dir string) {
	if c.DryRun {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(p)
		}
	}
}

// link creates src->dst, refusing to overwrite non-link files.
func (c *Context) link(src, dst string, dir bool) error {
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			c.record("skip (exists, not a link): %s", dst)
			return nil
		}
	}
	if c.DryRun {
		c.record("link %s -> %s", dst, src)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := platform.Link(src, dst, dir); err != nil {
		return fmt.Errorf("link %s -> %s: %w", dst, src, err)
	}
	c.record("link %s -> %s", filepath.Base(dst), src)
	return nil
}

// writeFile creates or overwrites a generated file.
func (c *Context) writeFile(path string, content []byte) error {
	if c.DryRun {
		c.record("write %s (%d bytes)", path, len(content))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// componentSrc resolves a component's absolute path inside its source pin.
func (c *Context) componentSrc(ch resolve.Choice) (string, error) {
	pin := c.pin(ch.Source)
	if pin == nil {
		return "", fmt.Errorf("%s: source %q not pinned", ch.QualifiedID(), ch.Source)
	}
	abs := filepath.Join(pin.ManifestDir(), filepath.FromSlash(ch.Component.Path))
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("%s: path %s: %w", ch.QualifiedID(), ch.Component.Path, err)
	}
	return abs, nil
}

// byCategory returns the enabled choices of one category.
func (c *Context) byCategory(cat string) []resolve.Choice {
	return c.Plan.ByCategory()[cat]
}

// mcpFrag extracts one server's per-target configuration from the source's
// shared servers.json (the agentictools convention: path + key).
type mcpFile struct {
	Servers map[string]struct {
		Config map[string]json.RawMessage `json:"config"`
	} `json:"servers"`
}

func (c *Context) mcpFrag(ch resolve.Choice, target string) (json.RawMessage, error) {
	src, err := c.componentSrc(ch)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	var f mcpFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", ch.QualifiedID(), err)
	}
	key := ch.Component.Key
	if key == "" {
		key = ch.ID
	}
	srv, ok := f.Servers[key]
	if !ok {
		return nil, fmt.Errorf("%s: key %q not in %s", ch.QualifiedID(), key, ch.Component.Path)
	}
	frag, ok := srv.Config[target]
	if !ok {
		return nil, fmt.Errorf("%s: no config for target %q", ch.QualifiedID(), target)
	}
	return frag, nil
}

func (c *Context) mcpCollection(target string) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	for _, ch := range c.byCategory("mcp") {
		if !targetsInclude(ch, target) {
			continue
		}
		frag, err := c.mcpFrag(ch, target)
		if err != nil {
			return nil, err
		}
		out[ch.ID] = frag
	}
	return out, nil
}

// targetsInclude re-checks the component's target allowlist per adapter
// (resolve filtered against project targets already; this narrows further).
func targetsInclude(ch resolve.Choice, target string) bool {
	if len(ch.Component.Targets) == 0 {
		return true
	}
	for _, t := range ch.Component.Targets {
		if t == target {
			return true
		}
	}
	return false
}

// governanceCheck reports provider/policy components on targets without a
// governance dialect (loud skip; dialects exist for opencode/claude/codex -
// capability = registration, see internal/dialect).
func governanceCheck(c *Context, target string) {
	if _, ok := dialect.For(target); ok {
		return
	}
	speak := strings.Join(dialect.Targets(), "/")
	for _, cat := range []string{"provider", "policy"} {
		for _, ch := range c.byCategory(cat) {
			if targetsInclude(ch, target) {
				c.record("SKIP %s: target %s has no governance dialect (dialects cover %s; pi endpoint shape open, cursor is server-side)",
					ch.QualifiedID(), target, speak)
			}
		}
	}
}

// noteUnsupported reports enabled components of categories a target has no
// native slot for - adapter honesty: never silent drops.
func (c *Context) noteUnsupported(target string, cats ...string) {
	for _, cat := range cats {
		for _, ch := range c.byCategory(cat) {
			if targetsInclude(ch, target) {
				c.record("SKIP %s: target %s has no %s slot", ch.QualifiedID(), target, cat)
			}
		}
	}
}
