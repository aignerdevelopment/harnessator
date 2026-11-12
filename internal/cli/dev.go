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
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/app"
	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/gitops"
	"harnessator.org/harnessator/internal/host"
	"harnessator.org/harnessator/internal/semverrange"
)

// devCmd: harnessator dev <source> | <source>:<cat>/<id> [--branch b]
//
//	harnessator dev status | sync [src] | drop <src> [--force]
//
// Only the named source of the CURRENT project is rewired to the overlay;
// every other project stays on the pinned checkout (per-project
// isolation by design).
func devCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("dev", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	branch := fs.String("branch", "", "dev branch name (default harnessator/dev/<user>)")
	force := fs.Bool("force", false, "drop: discard dirty/unpushed state")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	ctx := context.Background()

	if fs.NArg() == 0 {
		fmt.Fprintln(stderr, "usage: harnessator dev <source> | status | sync [src] | drop <src>")
		return 2
	}

	// non-mutating verbs need no fetch
	switch fs.Arg(0) {
	case "status":
		a, err := app.Open(ctx, *projectFile, app.ModeAuto)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(a.Dev) == 0 {
			fmt.Fprintln(stdout, "no dev overlays active")
			return 0
		}
		names := sortedKeys(a.Dev)
		for _, n := range names {
			st, err := gitops.Status(ctx, a.Store, n, a.Dev[n])
			if err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", n, err)
				continue
			}
			dirty := "clean"
			if st.Dirty {
				dirty = "DIRTY"
			}
			fmt.Fprintf(stdout, "%-16s %s [%s] at %s\n", n, a.Dev[n], dirty, st.Dir)
		}
		return 0
	}

	// start/sync/drop need current pins -> fetch
	a, err := app.Open(ctx, *projectFile, app.ModeFetch)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	switch verb := fs.Arg(0); verb {
	case "sync":
		targets := fs.Args()[1:]
		if len(targets) == 0 {
			targets = sortedKeys(a.Dev)
		}
		if len(targets) == 0 {
			fmt.Fprintln(stderr, "no dev overlays active")
			return 1
		}
		for _, name := range targets {
			branch, ok := a.Dev[name]
			if !ok {
				fmt.Fprintf(stderr, "%s is not in dev mode\n", name)
				return 1
			}
			src := sourceOf(a, name)
			if src == nil {
				fmt.Fprintf(stderr, "source %s not declared\n", name)
				return 1
			}
			rng, _ := semverrange.Parse(src.Ref)
			if !rng.IsBranch() {
				fmt.Fprintf(stderr, "source %s uses tag range %q - dev sync needs a branch ref (contribute then update instead)\n",
					name, src.Ref)
				return 1
			}
			if err := gitops.SyncDev(ctx, filepath.Join(a.Store.DevDir(name)), src.Ref); err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", name, err)
				return 1
			}
			fmt.Fprintf(stdout, "%s rebased onto origin/%s (branch %s)\n", name, src.Ref, branch)
		}
		return 0
	case "drop":
		name := fs.Arg(1)
		if name == "" {
			fmt.Fprintln(stderr, "dev drop <source>")
			return 2
		}
		if _, ok := a.Dev[name]; !ok {
			fmt.Fprintf(stderr, "source %s not in dev mode\n", name)
			return 1
		}
		if err := gitops.Drop(ctx, a.Store, name, a.Dev[name], *force); err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return 1
		}
		delete(a.Dev, name)
		if err := a.WriteDevBindings(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "dropped dev overlay for %s; run harnessator setup to relink the pinned checkout\n", name)
		return 0
	case "status":
		return 2 // handled above
	default:
		// start dev for <source> or <source>:<cat>/<id>
		ref := fs.Arg(0)
		srcName, _, id := parseRef(ref)
		if srcName == "" {
			srcName, id = id, "" // allow bare source name
		}
		src := sourceOf(a, srcName)
		if src == nil {
			fmt.Fprintf(stderr, "unknown source %q (also accepts <source>:<cat>/<id>)\n", srcName)
			return 1
		}
		pin, ok := a.Pins[srcName]
		if !ok {
			fmt.Fprintln(stderr, "source not pinned; run setup first")
			return 1
		}
		if id != "" {
			if !anyComponent(a, srcName, id) {
				fmt.Fprintf(stderr, "source %s has no component %q\n", srcName, id)
				return 1
			}
		}
		st, err := gitops.EnsureDev(ctx, a.Store, *src, pin.SHA, *branch)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if a.Dev == nil {
			a.Dev = map[string]string{}
		}
		a.Dev[srcName] = st.Branch
		if err := a.WriteDevBindings(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "dev mode ON for %s\n  overlay: %s\n  branch:  %s\n", srcName, st.Dir, st.Branch)
		fmt.Fprintln(stdout, "run harnessator setup to rewire THIS project to the overlay (other projects are unaffected)")
		fmt.Fprintln(stdout, "edit components live; harnessator contribute pushes + opens the MR/PR")
		return 0
	}
}

// contributeCmd: push branch + open MR/PR via the thin host layer.
func contributeCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("contribute", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	title := fs.String("title", "", "MR/PR title")
	body := fs.String("body", "", "MR/PR description")
	target := fs.String("target-branch", "", "target branch (host default if empty)")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	ctx := context.Background()
	a, err := app.Open(ctx, *projectFile, app.ModeAuto)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	names := sortedKeys(a.Dev)
	if len(names) == 0 {
		fmt.Fprintln(stderr, "nothing in dev mode (harnessator dev <source> first)")
		return 1
	}
	if fs.NArg() > 0 {
		one := fs.Arg(0)
		if _, ok := a.Dev[one]; !ok {
			fmt.Fprintf(stderr, "source %s not in dev mode\n", one)
			return 1
		}
		names = []string{one}
	}
	for _, name := range names {
		src := sourceOf(a, name)
		branch := a.Dev[name]
		dir := a.Store.DevDir(name)
		st, err := gitops.Status(ctx, a.Store, name, branch)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if st.Dirty {
			fmt.Fprintf(stderr, "%s: uncommitted changes in %s - commit them there first (native git)\n", name, dir)
			return 1
		}
		head, _ := gitops.HeadCommit(ctx, dir)
		if err := gitops.PushBranch(ctx, dir, branch); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		repo, _ := host.Parse(src.Git)
		t := *title
		if t == "" {
			t = fmt.Sprintf("harnessator(%s): dev updates", name)
		}
		b := *body
		if b == "" {
			b = fmt.Sprintf("Via harnessator dev mode from project %s.\nBase commit at dev start per lock; head %s.",
				a.Project, shortSHA(head))
		}
		created, msg, err := host.OpenMR(ctx, host.MROpts{
			Repo: repo, Branch: branch, Title: t, Body: b, Target: *target,
		})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		flag := "MR pending"
		if created {
			flag = "MR created"
		}
		fmt.Fprintf(stdout, "%s: pushed %s (%s) - %s\n", name, flag, branch, msg)
		fmt.Fprintln(stdout, "after merge: harnessator update pulls it back into the pinned checkout")
	}
	return 0
}

// sourceNewCmd scaffolds a new component source repo (harnadd, Go-native).
func sourceNewCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("source", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "source/repository name (required)")
	dir := fs.String("dir", "", "target directory (default ./<name>)")
	remote := fs.String("remote", "", "create remote via: gitlab|azure|github")
	desc := fs.String("description", "harnessator component source", "description")
	if err := parseFlags(fs, args); err != nil || *name == "" {
		fmt.Fprintln(stderr, "usage: harnessator source new --name n [--dir p] [--remote gitlab|azure|github] [--description d]")
		return 2
	}
	ctx := context.Background()
	target := *dir
	if target == "" {
		target = *name
	}
	abs, _ := filepath.Abs(target)
	if _, err := os.Stat(filepath.Join(abs, "harnessator.toml")); err == nil {
		fmt.Fprintf(stderr, "%s already has a harnessator.toml\n", abs)
		return 1
	}

	manifest := fmt.Sprintf(`kind = "source"
schema = 1

[package]
name = %q
description = %q
version = "0.1.0"

[profiles.default]
rule = ["start-here"]

[component.rule."start-here"]
path = "rules/start-here.md"
description = "A starter rule - replace with your team's best practice."
tags = ["core"]
`, *name, *desc)
	rule := fmt.Sprintf("# start-here\n\nEdit me, then run `harnessator contribute` from a consuming project.\nSource: %s\n", *name)
	files := map[string]string{
		"harnessator.toml":    manifest,
		"rules/start-here.md": rule,
		"README.md":           "# " + *name + "\n\nA harnessator component source. Add rules/skills/prompts/... here and register them in harnessator.toml.\n",
		"AGENTS.md":           "# AGENTS.md\n\nThis repository is a harnessator SOURCE: everything agent-related lives here; consuming projects only reference it.\n",
		"skills/.gitkeep":     "",
		"prompts/.gitkeep":    "",
		"mcp/servers.json":    "{\n  \"servers\": {}\n}\n",
		"providers/.gitkeep":  "",
		"policies/.gitkeep":   "",
	}
	if err := scaffold(abs, files); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// category dirs that carry no template content
	for _, d := range []string{"hooks", "tools", "agents", "workflows"} {
		if err := os.MkdirAll(filepath.Join(abs, d), 0o755); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}

	if err := validateFile(filepath.Join(abs, "harnessator.toml"), true); err != nil {
		fmt.Fprintf(stderr, "scaffold manifest invalid: %v\n", err)
		return 1
	}
	out, err := gitRun(abs, "init", "-b", "main")
	if err != nil {
		fmt.Fprintf(stderr, "git init: %v\n%s\n", err, out)
		return 1
	}
	if out, err := gitRun(abs, "add", "."); err != nil {
		fmt.Fprintf(stderr, "git add: %v\n%s\n", err, out)
		return 1
	}
	if out, err := gitRun(abs, "commit", "-qm", "harnessator source scaffold"); err != nil {
		fmt.Fprintf(stderr, "git commit: %v\n%s%s", err, out, identityHint(out))
		return 1
	}

	fmt.Fprintf(stdout, "scaffolded source %q at %s (manifest validated)\n", *name, abs)
	if *remote != "" {
		k := hostKind(*remote)
		msg, err := host.CreateRemote(ctx, k, *name, *desc)
		if err != nil {
			fmt.Fprintf(stderr, "remote create: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, msg)
	}
	fmt.Fprintln(stdout, "next: git remote add origin <url> && git push -u origin main")
	fmt.Fprintln(stdout, "then: harnessator source add <url> --ref main && harnessator enable "+*name+":default && harnessator setup")
	return 0
}

func hostKind(s string) host.Kind {
	switch strings.ToLower(s) {
	case "gitlab", "glab":
		return host.GitLab
	case "azure", "azure-devops":
		return host.AzureDevOps
	case "github", "gh":
		return host.GitHub
	}
	return host.Unknown
}

func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// identityHint turns git's bare identity error into actionable advice.
// harnessator shells out to native git and deliberately never authors
// commits on the operator's behalf - so when a commit we run fails on a
// missing identity, say what to do instead of drowning in exit-128 noise.
func identityHint(out string) string {
	if strings.Contains(out, "Please tell me who you are") || strings.Contains(out, "empty ident") {
		return "hint: git has no author identity here - set one first:\n" +
			"  git config --global user.name \"Your Name\"\n" +
			"  git config --global user.email \"you@example.com\"\n"
	}
	return ""
}

func scaffold(dir string, files map[string]string) error {
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func sourceOf(a *app.App, name string) *config.Source {
	for i := range a.Cfg.Sources {
		if a.Cfg.Sources[i].Name == name {
			return &a.Cfg.Sources[i]
		}
	}
	return nil
}

func anyComponent(a *app.App, src, id string) bool {
	for _, m := range a.Manifests {
		if m.Package.Name != src && src != "" {
			continue
		}
		for _, comps := range m.Components {
			if _, ok := comps[id]; ok {
				return true
			}
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
