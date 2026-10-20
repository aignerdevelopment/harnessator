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

package harvest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/contenthash"
	"harnessator.org/harnessator/internal/schema"
)

// Options controls one harvest run.
type Options struct {
	Paths     []string // inputs: managed projects, loose piles, single skills
	Name      string   // new source name/dir (required unless Into)
	Into      string   // existing source repo to extend
	Prefer    string   // "", "newest", "largest" - divergent-copy resolution
	DryRun    bool
	AllowHits bool // publish despite sanitize findings (review first!)
	// Adopt turns every donor project into a consumer of the published
	// source, with digest-verified original removal.
	Adopt bool
}

// Planned is one entry of the write plan.
type Planned struct {
	Category, ID, From, To, Action string // Action: "add" | "skip-identical"
}

// Result reports what Harvest did (or would do).
type Result struct {
	Output  string
	Planned []Planned
	Added   int
	Skipped int
	// Residual counts entries left for the guided tail; >0 means
	// harvest-report.json was written and the harnessator-harvest skill
	// can finish the job.
	Residual int

	// HarvestedByRoot attributes harvested components to each donor root
	// (absolute path) so callers (and --adopt) know what a donor gave.
	HarvestedByRoot map[string][]Ref
	// Adopted holds one report per donor root when Options.Adopt is set.
	Adopted []AdoptResult
	// Committed is set when the engine itself made the published source a
	// git repo (required before adopt clones it via file://).
	Committed bool
}

// variant is one content digest of a logical component plus its locations.
type variant struct {
	digest string
	locs   []string
	hits   []Hit
	c      Component // representative metadata
}

// aggT collects every digest variant of one logical (category,id) component
// seen across all scan roots.
type aggT struct {
	variants map[string]*variant
	order    []string // digests, in first-seen order
}

// Harvest turns scanned candidates into a validated source repo - new
// (git init'd by the CLI layer) or extended via --into. Guarantees:
// inputs are never modified; all-or-nothing (every conflict/hit/invalid-id
// is collected and aborts BEFORE any write); identical copies merge
// silently; divergent copies need --prefer or abort; digest-matching
// re-runs change nothing (idempotent).
func Harvest(o Options) (*Result, error) {
	if len(o.Paths) == 0 {
		return nil, errors.New("harvest: no input paths")
	}
	if o.Into == "" {
		if o.Name == "" {
			return nil, errors.New("harvest: --name (new source) or --into (extend existing) is required")
		}
	}
	switch o.Prefer {
	case "", "newest", "largest":
	default:
		return nil, fmt.Errorf("harvest: --prefer %q (newest|largest)", o.Prefer)
	}

	rep, err := Scan(o.Paths)
	if err != nil {
		return nil, err
	}

	// per-donor attribution (refs each root contributed, deduped)
	rootRefs := map[string]map[Ref]bool{}

	// ---- aggregate candidates across roots: cat/id -> digest variants --
	aggs := map[string]*aggT{}
	for i := range rep.Roots {
		rt := &rep.Roots[i]
		for _, c := range rt.Components {
			k := c.Category + "\x00" + c.ID
			a := aggs[k]
			if a == nil {
				a = &aggT{variants: map[string]*variant{}}
				aggs[k] = a
			}
			rr := rootRefs[rt.Path]
			if rr == nil {
				rr = map[Ref]bool{}
				rootRefs[rt.Path] = rr
			}
			rr[Ref{Category: c.Category, ID: c.ID}] = true
			v := a.variants[c.Digest]
			if v == nil {
				v = &variant{digest: c.Digest, c: c}
				a.variants[c.Digest] = v
				a.order = append(a.order, c.Digest)
			}
			for _, loc := range c.Locations {
				abs := filepath.Join(rt.Path, filepath.FromSlash(loc))
				v.locs = append(v.locs, abs)
			}
			v.hits = append(v.hits, c.Hits...)
		}
	}

	keys := make([]string, 0, len(aggs))
	for k := range aggs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var (
		conflicts []string
		invalids  []string
		hits      []string
		chosen    = make([]chosenT, 0, len(keys))
	)
	for _, k := range keys {
		a := aggs[k]
		cat, id, _ := strings.Cut(k, "\x00")
		var v *variant
		switch {
		case len(a.order) == 1:
			v = a.variants[a.order[0]]
		default: // divergent
			locs := append([]string{}, flatten(a)...)
			if o.Prefer == "" {
				conflicts = append(conflicts,
					fmt.Sprintf("%s/%s has %d variants:\n    %s", cat, id, len(a.order),
						strings.Join(locs, "\n    ")))
				continue
			}
			var best *variant
			for _, d := range a.order {
				cand := a.variants[d]
				if best == nil || better(cand.locs[0], best.locs[0], o.Prefer) {
					best = cand
				}
			}
			v = best
		}
		if !v.c.ValidID {
			invalids = append(invalids, fmt.Sprintf("%s/%s (id from %s)", cat, id, v.locs[0]))
		}
		for _, h := range v.hits {
			hits = append(hits, fmt.Sprintf("%s at %s:%d %s (%s/%s)",
				h.Rule, h.File, h.Line, h.Sample, cat, id))
		}
		chosen = append(chosen, chosenT{cat: cat, id: id, v: v})
	}
	if len(invalids) > 0 {
		conflicts = append(conflicts, "invalid component ids (rename in the source first):\n    "+
			strings.Join(invalids, "\n    "))
	}
	if len(conflicts) > 0 {
		return nil, fmt.Errorf("harvest: unresolved content - fix inputs or use --prefer:\n  %s",
			strings.Join(conflicts, "\n  "))
	}
	if len(hits) > 0 && !o.AllowHits {
		return nil, fmt.Errorf("harvest: sanitize findings block publishing (fix them, or --allow-hits after review):\n  %s",
			strings.Join(hits, "\n  "))
	}

	// ---- output + existing manifest --------------------------------------
	out := o.Into
	if out == "" {
		out = o.Name
	}
	absOut, err := filepath.Abs(out)
	if err != nil {
		return nil, err
	}
	m := &config.SourceManifest{Schema: schema.SchemaVersion}
	manifestPath := filepath.Join(absOut, "harnessator.toml")
	if o.Into != "" {
		m, err = config.LoadSourceManifest(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("harvest --into: %w", err)
		}
	} else {
		if _, err := os.Stat(manifestPath); err == nil {
			return nil, fmt.Errorf("harvest: %s already is a source - extend it with --into", absOut)
		}
		idName := filepath.Base(absOut)
		if !schema.ValidID(idName) {
			return nil, fmt.Errorf("harvest: source name derived from %q must be a valid id (got %q)", absOut, idName)
		}
		m.Kind = config.KindSource
		m.Package = config.Package{Name: idName,
			Description: "Components harvested by harnessator harvest."}
		if m.Profiles == nil {
			m.Profiles = map[string]config.Profile{}
		}
		if _, ok := m.Profiles["default"]; !ok {
			m.Profiles["default"] = config.Profile{}
		}
	}
	if m.Components == nil {
		m.Components = map[string]map[string]config.Component{}
	}

	res := &Result{Output: absOut}
	type pending struct {
		ch   chosenT
		dst  string // repo-relative
		comp config.Component
		sub  string // non-empty: lifted from a shared config file (Sub)
	}
	var todo []pending
	dstSeen := map[string]string{} // repo-rel dst -> cat/id (collision guard)

	for _, ch := range chosen {
		sub := ch.v.c.Sub
		var dst string
		if sub != "" {
			// lifted-from-config components have their own placement
			switch ch.cat {
			case "mcp":
				dst = "mcp/servers.json"
			case "hook":
				dst = "hooks/shared/" + subBase(sub)
			default:
				return nil, fmt.Errorf("harvest: unknown lift category %q", ch.cat)
			}
		} else {
			var err error
			dst, err = dstPath(ch.cat, ch.id, ch.v.c.Target)
			if err != nil {
				return nil, err
			}
		}
		if prev, dup := dstSeen[dst]; dup && sub == "" {
			return nil, fmt.Errorf("harvest: %s and %s/%s map to the same path %s", prev, ch.cat, ch.id, dst)
		}
		dstSeen[dst] = ch.cat + "/" + ch.id

		comp := config.Component{
			Path: dst, Description: descFor(ch.v, dst),
			Origin: "harvested from " + strings.Join(ch.v.locs, " | "),
		}
		if sub != "" && ch.cat == "mcp" {
			comp.Key = ch.id
			comp.Description = fmt.Sprintf("MCP server lifted from %s", filepath.Base(subFile(sub)))
		}
		if sub != "" && ch.cat == "hook" {
			comp.Description = fmt.Sprintf("hook script lifted from %s", filepath.Base(subFile(sub)))
		}
		// --into: compare against what we WOULD write (lifted rules
		// intentionally differ from the donor bytes, so a plain donor
		// digest would never match): digest match => skip; else conflict.
		if old, exists := m.Components[ch.cat][ch.id]; exists {
			match := false
			switch {
			case sub != "" && ch.cat == "mcp":
				// shared servers.json: compare THIS server's fragment
				match = existingMCPFragment(absOut, ch.id, ch.v.digest)
			default:
				oldAbs := filepath.Join(absOut, filepath.FromSlash(old.Path))
				src := ch.v.locs[0]
				if b, err := os.ReadFile(src); err == nil && strings.HasSuffix(src, ".mdc") {
					if ob, err := os.ReadFile(oldAbs); err == nil {
						match = string(ob) == liftBody(string(b))
					}
				} else if od, err := contenthash.TreeOrFile(oldAbs); err == nil {
					match = od == ch.v.digest
				}
			}
			if match {
				res.Planned = append(res.Planned, Planned{ch.cat, ch.id, ch.v.locs[0], dst, "skip-identical"})
				res.Skipped++
				continue
			}
			return nil, fmt.Errorf("harvest: %s/%s already exists in %s with DIFFERENT content (digest) - reconcile there, not by re-harvest",
				ch.cat, ch.id, o.Into)
		}
		todo = append(todo, pending{ch: ch, dst: dst, comp: comp, sub: sub})
		res.Planned = append(res.Planned, Planned{ch.cat, ch.id, ch.v.locs[0], dst, "add"})
	}

	if o.DryRun {
		sort.Slice(res.Planned, func(i, j int) bool { return res.Planned[i].To < res.Planned[j].To })
		return res, nil
	}

	// ---- write everything (all checks passed) ----------------------------
	// Sub components aggregate into shared files: one servers.json merge
	// and one claude hooks template merge for the whole run.
	serversDoc, err := loadServers(absOut)
	if err != nil {
		return nil, err
	}
	serversDirty := false
	var hookRefs []hookLiftRef

	for _, p := range todo {
		dstAbs := filepath.Join(absOut, filepath.FromSlash(p.dst))
		switch {
		case p.sub != "" && p.ch.cat == "mcp":
			if err := serversAccumulate(serversDoc, p.sub, p.ch.id); err != nil {
				return nil, err
			}
			serversDirty = true
		case p.sub != "" && p.ch.cat == "hook":
			// copy the script; the template is merged after the loop
			if err := placeFile(p.ch.v.locs[0], dstAbs, p.ch.v.c); err != nil {
				return nil, err
			}
			hookRefs = append(hookRefs, hookLiftRef{
				settingsPath: subFile(p.sub), base: subBase(p.sub),
			})
		default:
			if err := placeFile(p.ch.v.locs[0], dstAbs, p.ch.v.c); err != nil {
				return nil, err
			}
			if p.ch.cat == "rule" { // mdc hints ride along
				if hint := hintFor(p.ch.v, dstAbs); hint != nil {
					p.comp.Hint = map[string]map[string]any{"cursor": hint}
				}
				// lift mdc -> rules/<id>.md body
				if err := liftRuleFile(p.ch.v.locs[0], dstAbs); err != nil {
					return nil, err
				}
			}
		}
		if m.Components[p.ch.cat] == nil {
			m.Components[p.ch.cat] = map[string]config.Component{}
		}
		m.Components[p.ch.cat][p.ch.id] = p.comp
		if o.Into == "" {
			prof := m.Profiles["default"]
			prof[p.ch.cat] = append(prof[p.ch.cat], p.ch.id)
			m.Profiles["default"] = prof
		}
		res.Added++
	}
	if serversDirty {
		if err := writeJSONFile(filepath.Join(absOut, "mcp", "servers.json"), serversDoc); err != nil {
			return nil, err
		}
	}
	if len(hookRefs) > 0 {
		if err := mergeHookTemplate(absOut, hookRefs); err != nil {
			return nil, err
		}
	}
	if o.Into == "" {
		for _, ids := range m.Profiles {
			for c := range ids {
				sort.Strings(ids[c])
			}
		}
	}
	if err := config.WriteSource(manifestPath, m); err != nil {
		return nil, err
	}
	// post-write self-check: what we emit must load+validate against disk
	final, err := config.LoadSourceManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("harvest: written manifest unreadable (bug?): %w", err)
	}
	if err := config.ValidateSource(final, absOut); err != nil {
		return nil, fmt.Errorf("harvest: written source fails validation (bug?): %w", err)
	}
	n, err := writeResidualReport(absOut, rep.Roots)
	if err != nil {
		return nil, err
	}
	res.Residual = n

	if o.Adopt {
		// donor adoption clones the published source via file://, so it
		// must be a committed git repo first (new sources are committed
		// by the CLI; here we bootstrap only if needed).
		if initialized, gerr := ensureGitRepo(context.Background(), absOut, m.Package.Name); gerr != nil {
			return nil, gerr
		} else if initialized {
			res.Committed = true
		}
		donors := make([]string, 0, len(rootRefs))
		for d := range rootRefs {
			donors = append(donors, d)
		}
		sort.Strings(donors)
		for _, d := range donors {
			refs := make([]Ref, 0, len(rootRefs[d]))
			for r := range rootRefs[d] {
				refs = append(refs, r)
			}
			sort.Slice(refs, func(i, j int) bool {
				return refs[i].Category < refs[j].Category ||
					(refs[i].Category == refs[j].Category && refs[i].ID < refs[j].ID)
			})
			ar, aerr := Adopt(d, absOut, refs)
			if aerr != nil {
				ar = &AdoptResult{Donor: d, Notes: []string{"adoption failed: " + aerr.Error()}}
			}
			res.Adopted = append(res.Adopted, *ar)
		}
	}

	sort.Slice(res.Planned, func(i, j int) bool { return res.Planned[i].To < res.Planned[j].To })
	return res, nil
}

type chosenT struct {
	cat, id string
	v       *variant
}

// dstPath maps category (+recognizing adapter) to the source-repo layout.
func dstPath(cat, id, target string) (string, error) {
	switch cat {
	case "skill":
		return "skills/" + id, nil
	case "prompt":
		return "prompts/" + id + ".md", nil
	case "workflow":
		return "workflows/" + id + ".md", nil
	case "rule":
		return "rules/" + id + ".md", nil
	case "agent":
		if target == "" {
			target = "claude"
		}
		return "agents/" + target + "/" + id + ".md", nil
	case "hook":
		if target == "" {
			target = "opencode"
		}
		return "hooks/" + target + "/" + id + ".ts", nil
	case "tool":
		return "tools/" + id + ".ts", nil
	case "mcp":
		return "mcp/servers.json", nil // placement overrides dst; keeps dry-run sane
	}
	return "", fmt.Errorf("harvest: cannot place category %q", cat)
}

func flatten(a *aggT) []string {
	var out []string
	for _, d := range a.order {
		out = append(out, a.variants[d].locs...)
	}
	return out
}

func better(a, b, mode string) bool {
	if mode == "newest" {
		ma, mb := modTime(a), modTime(b)
		if ma.Equal(mb) {
			return a < b
		}
		return ma.After(mb)
	}
	sa, sb := treeSize(a), treeSize(b)
	if sa == sb {
		return a < b
	}
	return sa > sb
}

func modTime(p string) time.Time {
	fi, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

func treeSize(p string) int64 {
	var n int64
	filepath.WalkDir(p, func(s string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			n += fi.Size()
		}
		return nil
	})
	return n
}

// placeFile copies a candidate (dir or file) to dst preserving modes.
func placeFile(src, dst string, c Component) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return copyFile(src, dst, fi.Mode().Perm())
	}
	return copyTree(src, dst)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(p, target, fi.Mode().Perm())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := out.ReadFrom(in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// descFor: frontmatter description for skills, lifted description for mdc
// rules, else empty.
func descFor(v *variant, dst string) string {
	src := v.locs[0]
	var p string
	switch v.c.Category {
	case "skill":
		p = filepath.Join(src, "SKILL.md")
	case "rule":
		p = src
	default:
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	if v.c.Category == "rule" && strings.HasSuffix(src, ".mdc") {
		return liftMDC(string(b)).Description
	}
	// minimal frontmatter scan
	lines := strings.Split(string(b), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, ln := range lines[1:] {
		if strings.TrimSpace(ln) == "---" {
			break
		}
		if k, val, ok := strings.Cut(ln, ":"); ok && strings.TrimSpace(k) == "description" {
			return strings.Trim(strings.TrimSpace(val), "\"'")
		}
	}
	return ""
}

func hintFor(v *variant, dst string) map[string]any {
	src := v.locs[0]
	if !strings.HasSuffix(src, ".mdc") {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return nil
	}
	l := liftMDC(string(b))
	h := map[string]any{}
	if len(l.Globs) > 0 {
		h["globs"] = l.Globs
	}
	if l.AlwaysApply != nil {
		h["always-apply"] = *l.AlwaysApply
	}
	if len(h) == 0 {
		return nil
	}
	return h
}

// liftBody is the canonical lifted form of an .mdc rule (what lands in
// rules/<id>.md): frontmatter and marker removed, one leading blank line
// trimmed. Both the write and the idempotency compare use this.
func liftBody(raw string) string {
	return strings.TrimPrefix(liftMDC(raw).Body, "\n")
}

// liftRuleFile rewrites a copied .mdc into a plain rules/<id>.md (marker +
// frontmatter stripped; hints already captured via hintFor).
func liftRuleFile(src, dst string) error {
	if !strings.HasSuffix(src, ".mdc") {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, []byte(liftBody(string(b))), 0o644)
}
