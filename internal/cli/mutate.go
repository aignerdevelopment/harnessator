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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/app"
	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/gitops"
	"harnessator.org/harnessator/internal/schema"
)

func initCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	targets := fs.String("targets", "opencode,claude", "comma-separated harness targets")
	srcList := fs.String("source", "", "comma-separated git source URLs (name defaults to repo basename)")
	ref := fs.String("ref", "main", "default ref expression for --source")
	manifest := fs.String("manifest", "", "sub-directory manifest for multi-source repos (applies to --source entries)")
	force := fs.Bool("force", false, "overwrite an existing harnessator.toml")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	dir := fs.Arg(0)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	path := filepath.Join(dir, "harnessator.toml")
	if _, err := os.Stat(path); err == nil && !*force {
		fmt.Fprintf(stderr, "%s exists; use --force to overwrite\n", path)
		return 1
	}

	c := &config.ProjectConfig{
		Kind:    config.KindProject,
		Schema:  1,
		Targets: splitCSV(*targets),
	}
	for _, u := range splitCSV(*srcList) {
		name := sourceName(u)
		if name == "" {
			fmt.Fprintf(stderr, "cannot derive source name from %q\n", u)
			return 1
		}
		if err := c.AddSource(config.Source{Name: name, Git: u, Ref: *ref, Manifest: *manifest}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if err := validateTargets(c); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if err := config.WriteProject(path, c); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	app.EnsureGitignore(filepath.Dir(path))
	app.EnsureEntryFiles(filepath.Dir(path))

	fmt.Fprintf(stdout, "initialized %s (targets: %s)\n", path, strings.Join(c.Targets, ", "))
	if len(c.Sources) == 0 {
		fmt.Fprintln(stdout, "no sources yet - add one with: harnessator source add <git-url> [--ref R]")
	} else {
		fmt.Fprintln(stdout, "next: harnessator enable <source>:<profile> && harnessator setup")
	}
	return 0
}

// enableCmd accepts three ref shapes: <source>:<profile>, <source>:<cat>/<id>
// and <cat>/<id> (or bare <id>, resolved against fetched manifests).
func enableCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("enable", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	layer := fs.String("layer", "project", "scope layer file to write: project|team|personal|local")
	if err := parseFlags(fs, args); err != nil || fs.NArg() == 0 {
		if fs.NArg() == 0 {
			fmt.Fprintln(stderr, "usage: harnessator enable <source>:<profile> | <source>:<cat>/<id> | <cat>/<id>")
		}
		return 2
	}
	ref := fs.Arg(0)

	a, err := app.Open(context.Background(), *projectFile, app.ModeAuto)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cfg, err := a.LayerCfg(*layer)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// bare <source> (no :profile, no cat/id): pick the profile and SAY
	// so - the default (or the sole one) is applied openly, several
	// profiles without a "default" refuse with the list, never a guess.
	if !strings.Contains(ref, ":") && !strings.Contains(ref, "/") {
		if s := sourceOf(a, ref); s != nil {
			if m := a.Manifests[ref]; m != nil {
				names := make([]string, 0, len(m.Profiles))
				for p := range m.Profiles {
					names = append(names, p)
				}
				sort.Strings(names)
				switch {
				case len(names) == 0:
					fmt.Fprintf(stderr, "source %s declares no profiles - enable components directly (harnessator enable %s:<cat>/<id>), see: harnessator list --all\n", ref, ref)
					return 1
				case len(names) == 1:
					fmt.Fprintf(stdout, "no profile given - %s has exactly one: enabling %q (inspect: harnessator profiles %s)\n", ref, names[0], ref)
					ref = ref + ":" + names[0]
				default:
					if _, ok := m.Profiles["default"]; ok {
						fmt.Fprintf(stdout, "no profile given - applying %s's default profile (others: %s; inspect: harnessator profiles %s)\n",
							ref, strings.Join(removeName(names, "default"), ", "), ref)
						ref = ref + ":default"
					} else {
						fmt.Fprintf(stderr, "%s has %d profiles and none is named \"default\" - say which: harnessator enable %s:<profile>\n  available: %s\n  inspect:   harnessator profiles %s\n",
							ref, len(names), ref, strings.Join(names, ", "), ref)
						return 1
					}
				}
			}
		}
	}

	changed := false
	switch {
	case isProfileRef(a, ref):
		changed = cfg.AddTemplate(ref)
		if changed {
			fmt.Fprintf(stdout, "template %s enabled\n", ref)
		} else {
			fmt.Fprintf(stdout, "template %s already enabled\n", ref)
		}
	default:
		src, cat, id := parseRef(ref)
		if cat == "" {
			cat = findCategory(a, src, id)
			if cat == "" {
				fmt.Fprintf(stderr, "%s: not a profile and no component with that id found (harnessator search %s)\n", ref, id)
				return 1
			}
		}
		normalized := id
		if src != "" {
			normalized = src + ":" + id
		}
		if src != "" && !componentExists(a, src, cat, id) {
			fmt.Fprintf(stderr, "%s: source %s has no %s/%s\n", ref, src, cat, id)
			return 1
		}
		cfg.Include(cat, normalized)
		changed = true
		fmt.Fprintf(stdout, "component %s (%s) enabled via select.%s.include\n", normalized, cat, cat)
	}

	if changed {
		if err := a.WriteLayer(*layer, cfg); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "run harnessator setup to apply")
	}
	return 0
}

// disableCmd records select.<cat>.exclude (or removes a template).
func disableCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("disable", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	layer := fs.String("layer", "project", "scope layer file to write: project|team|personal|local")
	if err := parseFlags(fs, args); err != nil || fs.NArg() == 0 {
		if fs.NArg() == 0 {
			fmt.Fprintln(stderr, "usage: harnessator disable <source>:<profile> | <source>:<cat>/<id> | <cat>/<id>")
		}
		return 2
	}
	ref := fs.Arg(0)
	a, err := app.Open(context.Background(), *projectFile, app.ModeAuto)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	cfg, err := a.LayerCfg(*layer)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for i, t := range cfg.Templates {
		if t == ref {
			cfg.Templates = append(cfg.Templates[:i], cfg.Templates[i+1:]...)
			if err := a.WriteLayer(*layer, cfg); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			fmt.Fprintf(stdout, "template %s removed (its components may still be selected by other templates)\n", ref)
			fmt.Fprintln(stdout, "run harnessator setup to apply")
			return 0
		}
	}

	src, cat, id := parseRef(ref)
	if cat == "" {
		cat = findCategory(a, src, id)
		if cat == "" {
			fmt.Fprintf(stderr, "%s: unknown profile ref and no component id found\n", ref)
			return 1
		}
	}
	normalized := id
	if src != "" {
		normalized = src + ":" + id
	}
	// always rules refuse: exclusion would be futile (resolve keeps them)
	for _, ch := range a.Plan.Enabled {
		if ch.ID == id && ch.Component.Always && (src == "" || ch.Source == src) {
			fmt.Fprintf(stderr, "%s/%s is marked always - not disable-able\n", cat, id)
			return 1
		}
	}
	cfg.Exclude(cat, normalized)
	if err := a.WriteLayer(*layer, cfg); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "component %s disabled via select.%s.exclude\n", normalized, cat)
	fmt.Fprintln(stdout, "run harnessator setup to apply")
	return 0
}

// providerCmd: harnessator provider use <source>:provider/<id> | show | none
func providerCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("provider", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	if err := parseFlags(fs, args); err != nil || fs.NArg() == 0 {
		fmt.Fprintln(stderr, "usage: harnessator provider use <source>:<provider-id> | show | none")
		return 2
	}
	a, err := app.Open(context.Background(), *projectFile, app.ModeAuto)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	verb := fs.Arg(0)
	switch verb {
	case "show":
		if a.GovProvider == nil {
			fmt.Fprintln(stdout, "no provider profile selected")
		} else {
			fmt.Fprintf(stdout, "provider %s: %s\n", a.GovProvider.Name, a.GovProvider.ModelList())
			if a.GovPolicy != nil {
				fmt.Fprintf(stdout, "policy allowed=%v strict=%v subagent=%q [%s]\n",
					a.GovPolicy.Allowed, a.GovPolicy.Strict, a.GovPolicy.SubagentModel,
					strings.Join(a.GovPolicy.Sources, ", "))
			}
		}
		for _, w := range a.GovWarnings {
			fmt.Fprintln(stderr, "warning: "+w)
		}
		return 0
	case "none":
		proj, err := a.LayerCfg("project")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		proj.Provider = nil
		if err := a.WriteLayer("project", proj); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "provider selection cleared; run harnessator setup")
		return 0
	case "use":
		ref := fs.Arg(1)
		if ref == "" {
			fmt.Fprintln(stderr, "provider use: reference required")
			return 2
		}
		src, cat, id := parseRef(ref)
		if cat != "" && cat != "provider" {
			fmt.Fprintf(stderr, "%q is not a provider component\n", ref)
			return 1
		}
		if src == "" {
			fmt.Fprintln(stderr, "provider use wants <source>:<provider-id>")
			return 1
		}
		if !componentExists(a, src, "provider", id) {
			fmt.Fprintf(stderr, "source %s has no provider/%s (harnessator search provider)\n", src, id)
			return 1
		}
		proj, err := a.LayerCfg("project")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		proj.Provider = &config.ProviderConfig{Profile: src + ":provider/" + id}
		if err := a.WriteLayer("project", proj); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "provider %s selected; ensure it is enabled (template/select) and run harnessator setup\n", ref)
		return 0
	}
	fmt.Fprintf(stderr, "provider: unknown verb %q\n", verb)
	return 2
}

// sourceCmd: harnessator source add|remove|forget|list.
func sourceCmd(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: harnessator source add|remove|forget|list [..]")
		return 2
	}
	fs := flag.NewFlagSet("source", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	ref := fs.String("ref", "main", "ref expression (tag range or branch)")
	name := fs.String("name", "", "source name (default: repo basename)")
	layer := fs.String("layer", "", "scope layer FILE to write into: corporate|team|project|personal|local")
	manifest := fs.String("manifest", "", "sub-directory manifest for multi-source repos (e.g. source/harnessator)")
	if err := parseFlags(fs, args[1:]); err != nil {
		return 2
	}

	// Config-only open: these verbs must work even when resolution is
	// wedged - they are how the user un-wedges it.
	a, err := app.OpenConfig(*projectFile)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if args[0] == "list" {
		a.TryResolvePins(context.Background())
	}

	switch args[0] {
	case "list":
		for _, s := range a.Cfg.Sources {
			p, pinned := a.Pins[s.Name]
			line := fmt.Sprintf("%-16s %s (%s)", s.Name, s.Git, s.Ref)
			if s.Layer != "" {
				line += " layer=" + s.Layer
			}
			if s.Manifest != "" {
				line += " manifest=" + s.Manifest
			}
			if pinned {
				line += fmt.Sprintf(" -> %s (%s)", p.Resolved, shortSHA(p.SHA))
				if p.ContentSHA != "" {
					line += fmt.Sprintf(" | content %s (%s) [import-only]", p.ContentResolved, shortSHA(p.ContentSHA))
				} else if s.Editable != nil && !*s.Editable {
					line += " [import-only, declared]"
				}
			}
			fmt.Fprintln(stdout, line)
		}
		return 0
	case "add":
		url := fs.Arg(0)
		if url == "" {
			fmt.Fprintln(stderr, "source add: git url required")
			return 2
		}
		n := *name
		if n == "" {
			n = sourceName(url)
		}
		dst := *layer
		if dst == "" {
			dst = "project"
		}
		cfg, err := a.LayerCfg(dst)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := cfg.AddSource(config.Source{Name: n, Git: url, Ref: *ref, Layer: dst, Manifest: *manifest}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := a.WriteLayer(dst, cfg); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "source %s added (%s layer); run harnessator setup (fetches, resolves, applies)\n", n, dst)
		// Catch the store-name alias at the moment of the mistake, not
		// at the next fetch, where it would refuse under this name.
		if origin, oerr := gitops.Origin(filepath.Join(a.Store.SourceDir(n), "repo")); oerr == nil && !gitops.SameRepo(origin, url) {
			fmt.Fprintf(stderr, "note: the store already clones %q from a DIFFERENT URL (%s) - setup will refuse under this name; use --name <distinct>, or 'harnessator source forget %s' to hand the name over\n", n, origin, n)
		}
		return 0
	case "remove":
		n := fs.Arg(0)
		if n == "" {
			fmt.Fprintln(stderr, "source remove: name required")
			return 2
		}
		removed := false
		for _, l := range a.Layers.Loaded {
			cfg, err := a.LayerCfg(l)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			if cfg.RemoveSource(n) {
				if err := a.WriteLayer(l, cfg); err != nil {
					fmt.Fprintln(stderr, err)
					return 1
				}
				fmt.Fprintf(stdout, "source %s removed from the %s layer; run harnessator setup to reconcile\n", n, l)
				removed = true
				break
			}
		}
		if !removed {
			fmt.Fprintf(stderr, "source %s not declared in any layer\n", n)
			return 1
		}
		return 0
	case "forget":
		// Store surgery, explicitly requested: drop the machine-local
		// clone+checkouts under a NAME so the name is free again. The
		// config is untouched; other projects sharing the name re-fetch
		// (or rename) on their next setup - that is the point.
		n := fs.Arg(0)
		if n == "" {
			fmt.Fprintln(stderr, "source forget: name required")
			return 2
		}
		dir := a.Store.SourceDir(n)
		if _, err := os.Stat(dir); err != nil {
			fmt.Fprintf(stderr, "source %s: nothing in the store under this name (%s)\n", n, dir)
			return 1
		}
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "source %s forgotten: store clone and its checkouts removed (%s)\n", n, dir)
		fmt.Fprintln(stdout, "next: harnessator setup re-fetches from the configured URL into a clean clone")
		fmt.Fprintf(stdout, "note: the dev overlay for %s, if any, is untouched (%s)\n", n, a.Store.DevDir(n))
		return 0
	}
	fmt.Fprintf(stderr, "source: unknown verb %q\n", args[0])
	return 2
}

// --- helpers --------------------------------------------------------------

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func sourceName(url string) string {
	u := strings.TrimSuffix(url, ".git")
	u = strings.TrimRight(u, "/")
	// ssh style git@host:org/repo and https://host/org/repo both end in repo
	if i := strings.LastIndexAny(u, "/:"); i >= 0 {
		u = u[i+1:]
	}
	return u
}

func validateTargets(c *config.ProjectConfig) error {
	for _, t := range c.Targets {
		if !knownTarget(t) {
			return fmt.Errorf("unknown target %q (known: %s)", t, strings.Join(allTargets(), ", "))
		}
	}
	return nil
}

func removeName(names []string, drop string) []string {
	var out []string
	for _, n := range names {
		if n != drop {
			out = append(out, n)
		}
	}
	return out
}

func isProfileRef(a *app.App, ref string) bool {
	src, prof, ok := strings.Cut(ref, ":")
	if !ok || strings.Contains(prof, "/") {
		return false
	}
	m, ok := a.Manifests[src]
	if !ok {
		return false
	}
	_, ok = m.Profiles[prof]
	return ok
}

func findCategory(a *app.App, src, id string) string {
	names := sortedManifestNames(a.Manifests)
	for _, n := range names {
		if src != "" && n != src {
			continue
		}
		for cat, comps := range a.Manifests[n].Components {
			if _, ok := comps[id]; ok {
				return cat
			}
		}
	}
	return ""
}

func componentExists(a *app.App, src, cat, id string) bool {
	m, ok := a.Manifests[src]
	if !ok {
		return false
	}
	_, ok = m.Components[cat][id]
	return ok
}

func knownTarget(t string) bool {
	for _, x := range allTargets() {
		if x == t {
			return true
		}
	}
	return false
}

func allTargets() []string {
	return append(append([]string{}, schema.MVPTargets...), schema.PlannedTargets...)
}
