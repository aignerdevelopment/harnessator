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

// Scan roots (project dirs or loose piles) read-only.
func Scan(roots []string) (*Report, error) {
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

	seen := map[compKey]*Component{}
	var order []compKey
	relOf := func(p string) string {
		r, _ := filepath.Rel(abs, p)
		return filepath.ToSlash(r)
	}

	for _, rec := range materialize.Recognitions() {
		dir := filepath.Join(abs, filepath.FromSlash(rec.Dir))
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
					root.Residual = append(root.Residual,
						Residual{relOf(full), "loose file inside a directory-component layout"})
					continue
				}
				if rec.NeedsMD {
					if _, err := os.Stat(filepath.Join(full, "SKILL.md")); err != nil {
						root.Residual = append(root.Residual,
							Residual{relOf(full), "no SKILL.md inside"})
						continue // not a skill dir; stray
					}
				}
				id, kind = name, "dir"
			case materialize.EntryFile:
				if e.IsDir() {
					root.Residual = append(root.Residual,
						Residual{relOf(full), "directory inside a file-component layout"})
					continue
				}
				ext := filepath.Ext(name)
				if ext != rec.Suffix {
					root.Residual = append(root.Residual,
						Residual{relOf(full), `unexpected extension "` + ext + `" (want "` + rec.Suffix + `"`})
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
		fmt.Fprintf(w, "%s  [%s]  %d component(s), %d linked, %d sanitize hit(s)\n",
			rt.Path, state, nComp, len(rt.Linked), nHits)
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
