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

package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/app"
	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/gitops"
	"harnessator.org/harnessator/internal/resolve"
	"harnessator.org/harnessator/internal/store"
)

// profilesCmd answers "what would I actually apply?" - the groups
// (profiles) a source declares, with wildcards expanded against the
// real component set, so enable is never a purchase of mystery meat.
// Discovery mode (a bare git URL) works BEFORE anything is declared:
// the manifest is fetched into the store and reported, nothing else.
func profilesCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("profiles", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	target := ""
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}

	// discovery mode: a git URL, no project needed
	if looksLikeGitURL(target) {
		st, err := store.Default()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		url := target
		name := sourceName(url)
		ref := gitops.DefaultBranch(context.Background(), url)
		pin, m, err := app.ResolveSource(context.Background(), st,
			config.Source{Name: name, Git: url, Ref: ref})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		// URL mode shows everything; selector profiles belong to the
		// declared-name form (a URL already contains colons - scheme).
		profTarget{name: name, git: url, pin: pin, m: m}.render(stdout, "")
		fmt.Fprintf(stdout, "nothing declared yet - to consume: harnessator source add %s\n", url)
		return 0
	}

	a, err := app.Open(context.Background(), *projectFile, app.ModeAuto)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	on := map[string]bool{}
	for _, tpl := range a.Cfg.Templates {
		on[tpl] = true
	}
	if target == "" {
		for _, s := range a.Cfg.Sources {
			pin := a.Pins[s.Name]
			m := a.Manifests[s.Name]
			if pin == nil || m == nil {
				continue
			}
			profTarget{name: s.Name, git: s.Git, pin: pin, m: m, on: on}.render(stdout, "")
		}
		return 0
	}
	name, prof := sourceNameAndProfile(target)
	s := sourceOf(a, name)
	if s == nil {
		fmt.Fprintf(stderr, "no source %q here - available: %s (or pass a git URL to inspect before declaring)\n",
			name, strings.Join(sourceNames(a), ", "))
		return 1
	}
	pin := a.Pins[name]
	m := a.Manifests[name]
	if pin == nil || m == nil {
		fmt.Fprintf(stderr, "source %s is not pinned; run harnessator setup first\n", name)
		return 1
	}
	profTarget{name: name, git: s.Git, pin: pin, m: m, on: on}.render(stdout, prof)
	return 0
}

type profTarget struct {
	name string
	git  string
	pin  *gitops.Pinned
	m    *config.SourceManifest
	on   map[string]bool // src:profile refs the project currently enables
}

func sourceNameAndProfile(ref string) (string, string) {
	if src, prof, ok := strings.Cut(ref, ":"); ok {
		return src, prof
	}
	return ref, ""
}

func profileOf(ref string) string {
	_, prof := sourceNameAndProfile(ref)
	return prof
}

func sourceNames(a *app.App) []string {
	var out []string
	for _, s := range a.Cfg.Sources {
		out = append(out, s.Name)
	}
	return out
}

// looksLikeGitURL: schemes, scp-form, and anything ending in .git -
// deliberately conservative so bare source NAMES never look like URLs.
func looksLikeGitURL(s string) bool {
	if s == "" {
		return false
	}
	if strings.Contains(s, "://") || strings.HasSuffix(s, ".git") {
		return true
	}
	if i := strings.Index(s, "@"); i >= 0 && strings.Contains(s[i:], ":") && !strings.Contains(s[:i], "/") {
		return true
	}
	return false
}

func (t profTarget) render(w io.Writer, onlyProfile string) {
	fmt.Fprintf(w, "%s  %s  @ %s (%s)\n", t.name, t.git, t.pin.Resolved, shortSHA(t.pin.SHA))
	if t.pin.ContentSHA != "" {
		fmt.Fprintf(w, "  content: %s @ %s (%s) [import-only]\n",
			t.pin.ContentGit, t.pin.ContentResolved, shortSHA(t.pin.ContentSHA))
	}
	names := make([]string, 0, len(t.m.Profiles))
	for p := range t.m.Profiles {
		names = append(names, p)
	}
	sort.Strings(names)
	if len(names) == 0 {
		fmt.Fprintln(w, "  declares no profiles - enable components directly:")
		fmt.Fprintf(w, "    harnessator enable %s:<cat>/<id>   (see: harnessator list --all on its source)\n", t.name)
		return
	}
	if onlyProfile == "" && len(names) > 1 {
		// summary line first, then the detail (know before you enable)
		var parts []string
		for _, p := range names {
			mark := ""
			if t.on != nil && t.on[t.name+":"+p] {
				mark = " [on]"
			}
			parts = append(parts, fmt.Sprintf("%s (%s)%s", p, t.sizeOf(p), mark))
		}
		fmt.Fprintln(w, "  profiles: "+strings.Join(parts, "  |  "))
	}
	for _, p := range names {
		if onlyProfile != "" && p != onlyProfile {
			continue
		}
		t.renderProfile(w, p)
	}
	fmt.Fprintln(w, "  apply:   harnessator enable "+t.name+":"+pick(names))
	if onlyProfile == "" {
		fmt.Fprintf(w, "  inspect: harnessator profiles %s:<profile>\n", t.name)
	}
}

func pick(names []string) string {
	for _, n := range names {
		if n == "default" {
			return n
		}
	}
	return names[0]
}

func (t profTarget) sizeOf(p string) string {
	prof := t.m.Profiles[p]
	cats := sortedKeys(profileCategories(prof))
	var parts []string
	for _, cat := range cats {
		n := len(profileCategories(prof)[cat])
		if prof[cat] != nil && hasStar(prof[cat]) {
			n = len(t.m.Components[cat])
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, cat))
	}
	return strings.Join(parts, ", ")
}

// renderProfile expands wildcards against the ACTUAL component set -
// the whole point: the profile text may say "skill=*", the user sees
// which skills that is today.
func (t profTarget) renderProfile(w io.Writer, p string) {
	prof := t.m.Profiles[p]
	cats := profileCategoryOrder(prof)
	fmt.Fprintf(w, "  %s:\n", p)
	for _, cat := range cats {
		ids := prof[cat]
		var expanded []string
		if hasStar(ids) {
			for id := range t.m.Components[cat] {
				expanded = append(expanded, id)
			}
			sort.Strings(expanded)
		} else {
			expanded = append(expanded, ids...)
		}
		if len(expanded) == 0 {
			fmt.Fprintf(w, "    %s  (nothing registered in this category)\n", cat)
			continue
		}
		for _, id := range expanded {
			c, ok := t.m.Components[cat][id]
			if !ok {
				fmt.Fprintf(w, "    !! %s/%s - declared by the profile, not in this source\n", cat, id)
				continue
			}
			extra := ""
			if c.Always {
				extra = "  (always - cannot be dropped)"
			}
			fmt.Fprintf(w, "    %-9s %-34s %s%s\n", cat, id, c.Description, extra)
		}
	}
}

func hasStar(ids []string) bool {
	for _, id := range ids {
		if id == "*" {
			return true
		}
	}
	return false
}

func profileCategories(prof config.Profile) map[string][]string {
	out := map[string][]string{}
	for cat, ids := range prof {
		out[cat] = ids
	}
	return out
}

func profileCategoryOrder(prof config.Profile) []string {
	var present []string
	for _, cat := range resolve.CategoriesInOrder() {
		if _, ok := prof[cat]; ok {
			present = append(present, cat)
		}
	}
	for cat := range prof {
		if !containsStr(present, cat) {
			present = append(present, cat)
		}
	}
	return present
}

func containsStr(hay []string, n string) bool {
	for _, h := range hay {
		if h == n {
			return true
		}
	}
	return false
}
