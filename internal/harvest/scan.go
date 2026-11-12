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
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"harnessator.org/harnessator/internal/contenthash"
	"harnessator.org/harnessator/internal/materialize"
	"harnessator.org/harnessator/internal/schema"
)

// Component is one found piece of agent configuration content.
type Component struct {
	Category  string   `json:"category"`
	ID        string   `json:"id"`
	Kind      string   `json:"kind"` // "dir" | "file"
	Digest    string   `json:"digest"`
	Locations []string `json:"locations"` // root-relative paths (all copies)
	// Sub locates the component INSIDE a shared file when lifted from
	// config: "<file>#<fragment>"; empty = whole-file/dir.
	Sub     string `json:"sub,omitempty"`
	Target  string `json:"target"` // adapter whose layout claimed it
	ValidID bool   `json:"validID"`
	Hits    []Hit  `json:"sanitizeHits,omitempty"`
}

// Hit is one sanitize finding (secret shape, personal path, ...).
type Hit struct {
	Rule   string `json:"rule"`
	File   string `json:"file"` // relative to the scan root
	Line   int    `json:"line"`
	Sample string `json:"sample"` // truncated + masked
}

// Linked records a managed/hand-made symlink entry (never harvested).
type Linked struct {
	Category string `json:"category"`
	Path     string `json:"path"`
	Target   string `json:"target"`
	State    string `json:"state"` // "managed" | "external"
}

// Residual is an entry that lives in a recognized agent directory but did
// not match any component pattern - the guided tail works these.
type Residual struct {
	Path string `json:"path"` // relative to the scan root
	Why  string `json:"why"`
}

// Root is one scanned tree.
type Root struct {
	Path       string      `json:"path"`
	Managed    bool        `json:"managed"` // already harnessator-managed
	Components []Component `json:"components"`
	Linked     []Linked    `json:"linked,omitempty"`
	Residual   []Residual  `json:"residual,omitempty"`
	Notes      []string    `json:"notes,omitempty"`
	// NearMiss explains a surprising zero: directories that ALMOST match
	// a known layout (orphan skill piles, lookalike names like
	// .codex-tmp) with the exact fix, because "0 components" in silence
	// is where adoption dies.
	NearMiss []string `json:"nearMiss,omitempty"`
}

// Cluster groups same-id components across roots: identical (dedupe) or
// divergent (the "improved in a single project" problem, visible).
type Cluster struct {
	Category  string          `json:"category"`
	ID        string          `json:"id"`
	Identical bool            `json:"identical"`
	Groups    []ClusterDigest `json:"groups"`
}

// ClusterDigest is one digest variant with its absolute locations.
type ClusterDigest struct {
	Digest    string   `json:"digest"`
	Locations []string `json:"locations"`
}

// Report is the versioned, dashboard-ready envelope.
type Report struct {
	Schema    int       `json:"schema"`
	Generated time.Time `json:"generated"`
	Roots     []Root    `json:"roots"`
	Clusters  []Cluster `json:"clusters,omitempty"`
}

// splitNewlineList tolerates the classic shell pitfall of passing a
// multi-path list as a SINGLE newline-joined argument ("$paths" with an
// unquoted glob inside): a lone argument containing newlines is split
// into its parts, so the scan reports the intended roots instead of one
// "not a readable directory" error.
func splitNewlineList(in []string) []string {
	var out []string
	saw := false
	for _, p := range in {
		if strings.ContainsAny(p, "\n\r") {
			out = append(out, strings.FieldsFunc(p, func(r rune) bool { return r == '\n' || r == '\r' })...)
			saw = true
			continue
		}
		out = append(out, p)
	}
	if !saw {
		return in
	}
	return out
}

// Scan roots (project dirs or loose piles) read-only.
func Scan(roots []string) (*Report, error) {
	return scanRoots(splitNewlineList(roots))
}

// ScanWith adds whole-tree discovery: every input root is scanned as
// before, AND its subtree is descended looking for directories that
// smell like agent-config projects (marker children such as .claude/
// skills/ rules/ agents/ ..., or loose .mdc files). This is the
// "I have skills scattered across a dozen checkouts" mode.
func ScanWith(roots []string, recursive bool) (*Report, error) {
	if recursive {
		roots = expandRoots(roots)
	}
	return scanRoots(roots)
}

func scanRoots(roots []string) (*Report, error) {
	rep := &Report{Schema: SchemaVersion, Generated: time.Now().UTC()}
	for _, rp := range roots {
		r, err := scanRoot(rp)
		if err != nil {
			return nil, err
		}
		rep.Roots = append(rep.Roots, *r)
	}
	rep.Clusters = clusterComponents(rep.Roots)
	return rep, nil
}

// --- recursive root discovery ---------------------------------------------

// maxDescent bounds the discovery walk: config projects live near the
// surface of a tree; below that it is third-party code.
const maxDescent = 4

var markerDirs = map[string]bool{
	".claude": true, ".cursor": true, ".opencode": true, ".codex": true, ".agents": true,
	"skills": true, "rules": true, "agents": true, "commands": true, "prompts": true, "workflows": true,
}

var descentSkip = map[string]bool{
	"node_modules": true, "dist": true, "build": true, "target": true, "out": true,
	"vendor": true, "__pycache__": true, "venv": true, "coverage": true, ".next": true,
}

// looksLikeProject reports whether dir holds recognizable content:
// any marker-named child, or .mdc files directly in it.
func looksLikeProject(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			if markerDirs[e.Name()] {
				return true
			}
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".mdc") {
			return true
		}
	}
	return false
}

func isAtomicSkillDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil
}

func expandRoots(in []string) []string {
	in = splitNewlineList(in) // tolerate a newline-joined list as one arg
	out := map[string]bool{}
	var walk func(abs string, depth int)
	walk = func(abs string, depth int) {
		if depth == 0 || looksLikeProject(abs) {
			out[abs] = true // input roots always scan; found projects too
		}
		if depth >= maxDescent {
			return
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") || descentSkip[name] {
				continue
			}
			child := filepath.Join(abs, name)
			if isAtomicSkillDir(child) {
				continue // a skill's own tree is content, not a project
			}
			walk(child, depth+1)
		}
	}
	for _, r := range in {
		if abs, err := filepath.Abs(r); err == nil {
			if fi, serr := os.Stat(abs); serr == nil && fi.IsDir() {
				walk(abs, 0)
			}
		}
	}
	list := make([]string, 0, len(out))
	for p := range out {
		list = append(list, p)
	}
	sort.Strings(list)
	return list
}

// --- core walk ------------------------------------------------------------

func scanRoot(p string) (*Root, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("scan: %s: not a readable directory", p)
	}
	root := &Root{Path: abs}
	_, lockErr := os.Stat(filepath.Join(abs, "harnessator.lock"))
	_, actErr := os.Stat(filepath.Join(abs, ".harnessator", "active.md"))
	if lockErr == nil || actErr == nil {
		root.Managed = true
		root.Notes = append(root.Notes,
			"already harnessator-managed (components here are links; only hand-authored content is reported)")
	}

	// a scanned directory that IS itself a skill (SKILL.md at root)
	if _, err := os.Stat(filepath.Join(abs, "SKILL.md")); err == nil {
		if digest, derr := contenthash.TreeOrFile(abs); derr == nil {
			root.Components = append(root.Components, Component{
				Category: "skill", ID: filepath.Base(abs), Kind: "dir", Digest: digest,
				Locations: []string{"."}, Target: "agentskills",
				ValidID: schema.ValidID(filepath.Base(abs)),
				Hits:    sanitize(abs, abs),
			})
		}
	}

	relOf := func(p string) string {
		r, _ := filepath.Rel(abs, p)
		return filepath.ToSlash(r)
	}

	seen := map[compKey]*Component{}
	var order []compKey
	relSeen := map[string]bool{} // dedupe residual mentions across patterns
	residual := func(p, why string) {
		r := relOf(p)
		if relSeen[r+" "+why] {
			return
		}
		relSeen[r+" "+why] = true
		root.Residual = append(root.Residual, Residual{r, why})
	}

	for _, rec := range materialize.Recognitions() {
		dir := filepath.Join(abs, filepath.FromSlash(rec.Dir))
		if rec.Kind == materialize.EntryFile && rec.Recursive {
			scanTree(abs, dir, rec, root, seen, &order, relOf)
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // layout not present in this tree
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			full := filepath.Join(dir, name)
			info, err := e.Info()
			if err != nil {
				continue
			}
			isLink := info.Mode()&os.ModeSymlink != 0
			if isLink {
				if l, ok := classifyLink(abs, full, rec, dir); ok {
					root.Linked = append(root.Linked, l)
					continue
				}
				// internal symlink: treat as content below
			}
			var id, kind string
			switch rec.Kind {
			case materialize.EntryDir:
				if !e.IsDir() && !isLink {
					// the scan ROOT legitimately holds ordinary project
					// files - residuals are for named agent dirs only
					if rec.Dir != "." {
						residual(full, "loose file inside a directory-component layout")
					}
					continue
				}
				if rec.NeedsMD {
					if _, err := os.Stat(filepath.Join(full, "SKILL.md")); err != nil {
						if rec.Dir != "." {
							residual(full, "no SKILL.md inside")
						}
						continue // not a skill dir; stray
					}
				}
				id, kind = name, "dir"
			case materialize.EntryFile:
				if e.IsDir() {
					if rec.Dir != "." {
						residual(full, "directory inside a file-component layout")
					}
					continue
				}
				ext := filepath.Ext(name)
				if ext != rec.Suffix {
					if rec.Dir != "." {
						residual(full, `unexpected extension "`+ext+`" (want "`+rec.Suffix+`")`)
					}
					continue
				}
				id, kind = strings.TrimSuffix(name, ext), "file"
			}
			digest, err := contenthash.TreeOrFile(full)
			if err != nil {
				root.Notes = append(root.Notes, fmt.Sprintf("%s: digest failed: %v", full, err))
				continue
			}
			rel, _ := filepath.Rel(abs, full)
			k := compKey{rec.Category, id, digest}
			if c, ok := seen[k]; ok {
				c.Locations = append(c.Locations, filepath.ToSlash(rel))
				continue
			}
			c := &Component{
				Category: rec.Category, ID: id, Kind: kind, Digest: digest,
				Locations: []string{filepath.ToSlash(rel)},
				Target:    rec.Target, ValidID: schema.ValidID(id),
			}
			c.Hits = sanitize(abs, full)
			seen[k] = c
			order = append(order, k)
		}
	}
	root.NearMiss = nearMiss(abs)

	// Root note files of UNMANAGED trees are import candidates, not
	// noise - this is the terraform-import moment: a hand-written
	// AGENTS.md/CLAUDE.md/GEMINI.md carries real rule content wherever
	// it sits. (Managed roots hold OUR generated pointers, identified
	// by the lock, and are deliberately not flagged.)
	if !root.Managed {
		for _, note := range rootNoteFiles {
			if fi, err := os.Stat(filepath.Join(abs, note)); err == nil && !fi.IsDir() {
				residual(filepath.Join(abs, note),
					"unmanaged agent note file - likely rule content: import it (harvest --with-notes), have an agent classify it, or split by hand into rules/<id>.md")
			}
		}
	}

	// Config-embedded lifts (MCP servers, hook scripts). A
	// harnessator-managed root is skipped - those files are OUR generated
	// output, not authored content.
	if !root.Managed {
		liftConfig(abs, seen, &order, root)
	}

	for _, k := range order {
		root.Components = append(root.Components, *seen[k])
	}
	sort.Slice(root.Components, func(i, j int) bool {
		a, b := root.Components[i], root.Components[j]
		return a.Category < b.Category || (a.Category == b.Category && a.ID < b.ID)
	})
	sort.Slice(root.Linked, func(i, j int) bool { return root.Linked[i].Path < root.Linked[j].Path })
	return root, nil
}

// scanTree implements Recursive EntryFile recognition: every file with
// the claimed suffix anywhere under dir is a component. Nested files get
// group-prefixed ids (react/react-core.mdc -> "react-react-core") so the
// same filename in different groups stays one distinct component;
// directories are structure, not residual noise.
func scanTree(rootAbs, dir string, rec materialize.Recognition, root *Root,
	seen map[compKey]*Component, order *[]compKey, relOf func(string) string) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || filepath.Ext(name) != rec.Suffix {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return nil
		}
		id := strings.ReplaceAll(strings.TrimSuffix(filepath.ToSlash(rel), rec.Suffix), "/", "-")
		digest, derr := contenthash.TreeOrFile(p)
		if derr != nil {
			root.Notes = append(root.Notes, fmt.Sprintf("%s: digest failed: %v", p, derr))
			return nil
		}
		k := compKey{rec.Category, id, digest}
		if c, ok := seen[k]; ok {
			c.Locations = append(c.Locations, relOf(p))
			return nil
		}
		c := &Component{
			Category: rec.Category, ID: id, Kind: "file", Digest: digest,
			Locations: []string{relOf(p)},
			Target:    rec.Target, ValidID: schema.ValidID(id),
		}
		c.Hits = sanitize(rootAbs, p)
		seen[k] = c
		*order = append(*order, k)
		return nil
	})
}

// classifyLink decides "managed"/"external" for a symlinked component
// candidate; ok=false means it points inside the project (=> content).
func classifyLink(rootAbs, linkPath string, rec materialize.Recognition, dir string) (Linked, bool) {
	tgt, err := os.Readlink(linkPath)
	if err != nil {
		return Linked{}, false
	}
	absTgt := tgt
	if !filepath.IsAbs(absTgt) {
		absTgt = filepath.Join(dir, tgt)
	}
	if !strings.HasPrefix(absTgt, rootAbs+string(filepath.Separator)) {
		state := "external"
		if isStorePath(absTgt) {
			state = "managed"
		}
		rel, _ := filepath.Rel(rootAbs, linkPath)
		return Linked{Category: rec.Category, Path: filepath.ToSlash(rel), Target: absTgt, State: state}, true
	}
	return Linked{}, false
}

func isStorePath(p string) bool {
	for _, m := range []string{
		"/" + ".harnessator/sources/", // harnessator store (and $HARNESSATOR_HOME)
		"/.harnessator/dev/",          // dev overlays
		"/.agentictools/",             // legacy redirects
		"/work/agentictools/",         // legacy live checkout style links
	} {
		if strings.Contains(p, m) {
			return true
		}
	}
	home, err := os.UserHomeDir()
	return err == nil && strings.HasPrefix(p, filepath.Join(home, ".harnessator"+string(filepath.Separator)))
}

// --- clusters -------------------------------------------------------------

func clusterComponents(roots []Root) []Cluster {
	type agg struct {
		digests map[string]map[string]bool // digest -> abs locations
	}
	groups := map[string]*agg{} // cat\x00lower(id)
	for _, r := range roots {
		for _, c := range r.Components {
			id := strings.ToLower(c.ID)
			k := c.Category + "\x00" + id
			g := groups[k]
			if g == nil {
				g = &agg{digests: map[string]map[string]bool{}}
				groups[k] = g
			}
			for _, loc := range c.Locations {
				kd := c.Digest
				if g.digests[kd] == nil {
					g.digests[kd] = map[string]bool{}
				}
				g.digests[kd][filepath.Join(r.Path, filepath.FromSlash(loc))] = true
			}
		}
	}
	var out []Cluster
	for k, g := range groups {
		total := 0
		for _, l := range g.digests {
			total += len(l)
		}
		if total < 2 {
			continue // single copy, no news
		}
		cat, id, _ := strings.Cut(k, "\x00")
		cl := Cluster{Category: cat, ID: id, Identical: len(g.digests) == 1}
		dks := make([]string, 0, len(g.digests))
		for d := range g.digests {
			dks = append(dks, d)
		}
		sort.Strings(dks)
		for _, d := range dks {
			var locs []string
			for l := range g.digests[d] {
				locs = append(locs, l)
			}
			sort.Strings(locs)
			cl.Groups = append(cl.Groups, ClusterDigest{Digest: d, Locations: locs})
		}
		out = append(out, cl)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return a.Category < b.Category || (a.Category == b.Category && a.ID < b.ID)
	})
	return out
}

// --- sanitize -------------------------------------------------------------

var (
	hitRules = []struct {
		Name string
		RE   *regexp.Regexp
	}{
		{"secret-assignment", regexp.MustCompile(`(?i)\b(api[_-]?key|auth[_-]?token|access[_-]?token|token|password|secret)\b["'\s]*[:=]["'\s]*[A-Za-z0-9+/_\-.]{16,}`)},
		{"private-key", regexp.MustCompile(`-----BEGIN[A-Z ]*PRIVATE KEY-----`)},
		{"credentials-in-url", regexp.MustCompile(`://[^/\s@:]+:[^/\s@]+@`)},
		{"personal-path", regexp.MustCompile(`(/home/[a-z0-9._-]+/|/Users/[a-z0-9._-]+/|[A-Za-z]:\\Users\\)`)},
	}
	textExts = map[string]bool{
		".md": true, ".mdc": true, ".toml": true, ".json": true, ".yaml": true,
		".yml": true, ".ts": true, ".js": true, ".sh": true, ".py": true, ".txt": true,
	}
	maxSanitizeSize = 256 << 10
)

// sanitize reports secret/path shapes inside a component, file:line with a
// masked sample - loud, never auto-rewritten.
func sanitize(rootAbs, compPath string) []Hit {
	var hits []Hit
	rel := func(p string) string {
		r, _ := filepath.Rel(rootAbs, p)
		return filepath.ToSlash(r)
	}
	check := func(path string) {
		fi, err := os.Stat(path)
		if err != nil || fi.Size() > int64(maxSanitizeSize) {
			return
		}
		if !textExts[strings.ToLower(filepath.Ext(path))] {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for ln := 1; sc.Scan(); ln++ {
			line := sc.Text()
			for _, rule := range hitRules {
				if loc := rule.RE.FindString(line); loc != "" {
					hits = append(hits, Hit{
						Rule: rule.Name, File: rel(path), Line: ln, Sample: mask(loc),
					})
					break
				}
			}
		}
	}
	filepath.WalkDir(compPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		check(p)
		return nil
	})
	return hits
}

// mask keeps a finding recognizable without spreading the value.
func mask(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 12 {
		return s[:min(len(s), 4)] + "***"
	}
	return s[:8] + "…" + s[len(s)-4:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- output ---------------------------------------------------------------

// WriteJSON emits the stable envelope (dashboard/status compatible).
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteText renders the human report from the same structure (no parallel
// format - the JSON is the source).
func (r *Report) WriteText(w io.Writer) {
	fmt.Fprintf(w, "harnessator scan (schema %d, %s)\n\n", r.Schema, r.Generated.Format(time.RFC3339))
	for i := range r.Roots {
		rt := &r.Roots[i]
		state := "unmanaged"
		if rt.Managed {
			state = "harnessator-managed"
		}
		nComp, nHits := len(rt.Components), 0
		for _, c := range rt.Components {
			nHits += len(c.Hits)
		}
		fmt.Fprintf(w, "%s  [%s]  %d component(s), %d linked, %d sanitize hit(s), %d residual, %d near-miss\n",
			rt.Path, state, nComp, len(rt.Linked), nHits, len(rt.Residual), len(rt.NearMiss))
		for _, c := range rt.Components {
			flag := ""
			if !c.ValidID {
				flag = "   (id NOT harvestable as-is)"
			}
			locs := ""
			if len(c.Locations) > 1 {
				locs = fmt.Sprintf("  [%d copies]", len(c.Locations))
			}
			fmt.Fprintf(w, "  %-8s %-28s %s%s%s\n",
				c.Category, c.ID, shortDigest(c.Digest), locs, flag)
			for _, h := range c.Hits {
				fmt.Fprintf(w, "      ! %s at %s:%d  %s\n", h.Rule, h.File, h.Line, h.Sample)
			}
		}
		for _, l := range rt.Linked {
			fmt.Fprintf(w, "  (linked %s %-24s -> %s)\n", l.State, l.Path, l.Target)
		}
		for _, n := range rt.Notes {
			fmt.Fprintf(w, "  note: %s\n", n)
		}
		for _, r := range rt.Residual {
			fmt.Fprintf(w, "  residual: %s - %s\n", r.Path, r.Why)
		}
		for _, nm := range rt.NearMiss {
			fmt.Fprintf(w, "  near-miss: %s\n", nm)
		}
		writeln(w)
	}
	if len(r.Clusters) == 0 {
		fmt.Fprintln(w, "no cross-root duplication/divergence found")
		return
	}
	fmt.Fprintf(w, "clusters (same component in multiple places):\n")
	for _, cl := range r.Clusters {
		kind := "DIVERGENT"
		if cl.Identical {
			kind = "identical"
		}
		fmt.Fprintf(w, "  %-8s %-24s %s (%d variant(s))\n", cl.Category, cl.ID, kind, len(cl.Groups))
		for _, g := range cl.Groups {
			for _, loc := range g.Locations {
				fmt.Fprintf(w, "      %s  %s\n", shortDigest(g.Digest), loc)
			}
		}
	}
}

func writeln(w io.Writer) { fmt.Fprintln(w) }

func shortDigest(d string) string {
	if len(d) > len(contenthash.Prefix)+10 {
		return d[:len(contenthash.Prefix)+10] + "…"
	}
	return d
}

// --- near-miss diagnostics ---------------------------------------------------

// nearMiss explains silent zeros: a directory that ALMOST matches a known
// layout. Pure advice - it never changes what is claimed as a component.
func nearMiss(abs string) []string {
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		switch name {
		case ".agents", ".claude", ".codex", ".cursor", ".opencode":
			if msg := orphanPile(filepath.Join(abs, name), name); msg != "" {
				out = append(out, msg)
			}
		default:
			for _, known := range []string{".agents", ".claude", ".codex", ".cursor", ".opencode"} {
				if strings.HasPrefix(name, known+".") || strings.HasPrefix(name, known+"-") {
					out = append(out, fmt.Sprintf(
						"%s/ resembles %s/ but no tool nor harnessator reads that name - if it holds agent content, rename it to %s/ (or scan it explicitly)",
						name, known, known))
					break
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// orphanPile: skill directories sitting directly inside a container whose
// canonical skill dir is one level deeper (e.g. .agents/<x>/SKILL.md while
// the standard spot is .agents/skills/<x>/).
func orphanPile(dir, name string) string {
	canonical := map[string]string{
		".agents":   ".agents/skills",
		".claude":   ".claude/skills",
		".codex":    "",
		".cursor":   "",
		".opencode": ".opencode/skill",
	}[name]
	if canonical == "" {
		return ""
	}
	skillsSub := filepath.Join(dir, filepath.Base(canonical))
	if _, err := os.Stat(skillsSub); err == nil {
		return "" // canonical spot exists - nothing to explain
	}
	n := skillDirCount(dir)
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%s/ holds %d skill director%s directly, but the canonical spot is %s/ - either move them or scan the pile itself: harnessator scan <path>/%s",
		name, n, plural(n), canonical, name)
}

// skillDirCount: immediate subdirectories containing SKILL.md.
func skillDirCount(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil {
			n++
		}
	}
	return n
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// rootNoteFiles: filenames that are agent-content BY CONVENTION, whoever
// wrote them - an unmanaged tree holding them is a harvest candidate.
var rootNoteFiles = []string{".cursorrules", "AGENTS.md", "CLAUDE.md", "GEMINI.md", "QWEN.md"}
