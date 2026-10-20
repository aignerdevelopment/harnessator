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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/app"
	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/contenthash"
	"harnessator.org/harnessator/internal/gitops"
	"harnessator.org/harnessator/internal/materialize"
)

// Ref identifies one harvested component (category + id).
type Ref struct {
	Category string
	ID       string
}

// AdoptResult describes turning one donor project into a consumer of the
// source published from it.
type AdoptResult struct {
	Donor   string
	Linked  int      // originals replaced by managed links (digest-verified)
	Kept    []string // originals kept: content differs from the published version
	Notes   []string // advisories (file:// source, backed-up generated paths)
	Skipped string   // non-empty = donor not adopted, with reason
}

// Adopt rewires a donor project onto the published source:
//
//  1. the donor gets a canonical harnessator.toml (targets inferred from
//     its layouts; source = file://<output> ref main; includes for its
//     contributions) plus gitignore/AGENTS.md/CLAUDE.md scaffolding;
//  2. before materializing, every link destination holding an ORIGINAL is
//     removed ONLY after a digest proves byte-identical content -
//     divergent originals are KEPT and setup skips them loudly;
//  3. generated-file destinations (settings, cursor rules) that already
//     exist with different content are backed up as *.pre-adopt, never
//     silently clobbered.
//
// Anything besides the donor project itself is never modified.
func Adopt(donor, outDir string, refs []Ref) (*AdoptResult, error) {
	absDonor, err := filepath.Abs(donor)
	if err != nil {
		return nil, err
	}
	res := &AdoptResult{Donor: absDonor}

	cfgPath := filepath.Join(absDonor, "harnessator.toml")
	if _, err := os.Stat(cfgPath); err == nil {
		res.Skipped = "already a harnessator project (harnessator.toml present)"
		return res, nil
	}

	// --- 1. configure the donor ------------------------------------------
	targets := inferTargets(absDonor)
	if len(targets) == 0 {
		res.Skipped = "no known harness layouts (loose pile / plain repo) - nothing to adopt"
		return res, nil
	}
	sm, err := config.LoadSourceManifest(filepath.Join(outDir, "harnessator.toml"))
	if err != nil {
		return nil, fmt.Errorf("adopt: published source unreadable: %w", err)
	}
	pkg := sm.Package.Name
	cfg := &config.ProjectConfig{Kind: config.KindProject, Schema: 1, Targets: targets}
	if err := cfg.AddSource(config.Source{Name: pkg, Git: "file://" + outDir, Ref: "main"}); err != nil {
		return nil, err
	}
	for _, r := range refs {
		cfg.Include(r.Category, pkg+":"+r.ID)
	}
	if err := config.WriteProject(cfgPath, cfg); err != nil {
		return nil, err
	}
	app.EnsureGitignore(absDonor)
	app.EnsureEntryFiles(absDonor)
	res.Notes = append(res.Notes,
		fmt.Sprintf("source %s pinned as file:// local path - publish it, then `harnessator source remove %s && source add <url>`", pkg, pkg))

	// --- 2. digest-verified original removal -------------------------------
	a, err := app.Open(context.Background(), cfgPath, app.ModeFetch)
	if err != nil {
		return nil, fmt.Errorf("adopt %s: %w", absDonor, err)
	}
	var specs []materialize.Spec
	byTarget, err := materialize.SpecsByTarget(a.MaterializeContext(true, discardWriter{}))
	if err != nil {
		return nil, err
	}
	for _, ts := range byTarget {
		specs = append(specs, ts...)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Dst < specs[j].Dst })
	for _, sp := range specs {
		rel, _ := filepath.Rel(absDonor, sp.Dst)
		rel = filepath.ToSlash(rel)
		fi, lerr := os.Lstat(sp.Dst)
		if lerr != nil {
			continue // absent: setup creates the link
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			continue // already managed
		}
		if sp.Src == "" {
			// generated destination: never clobber foreign content
			if cur, err := os.ReadFile(sp.Dst); err == nil && !alreadyGenerated(string(cur)) {
				if rerr := os.Rename(sp.Dst, sp.Dst+".pre-adopt"); rerr != nil {
					return nil, rerr
				}
				res.Notes = append(res.Notes,
					fmt.Sprintf("backed up %s -> %s.pre-adopt (generated content replaces it)", rel, rel))
			}
			continue
		}
		dstD, e1 := contenthash.TreeOrFile(sp.Dst)
		srcD, e2 := contenthash.TreeOrFile(sp.Src)
		if e1 != nil || e2 != nil {
			res.Kept = append(res.Kept, rel+" (unreadable)")
			continue
		}
		if dstD != srcD {
			res.Kept = append(res.Kept, rel+" (differs from published version - merge manually, then re-run setup)")
			continue
		}
		if rerr := os.RemoveAll(sp.Dst); rerr != nil {
			return nil, rerr
		}
		res.Linked++
	}

	// --- 3. materialize (links land; kept originals skip loudly) ----------
	if err := a.Setup(context.Background(), false, discardWriter{}); err != nil {
		return nil, err
	}
	sort.Strings(res.Kept)
	sort.Strings(res.Notes)
	return res, nil
}

// alreadyGenerated recognizes harnessator-owned content on generated
// paths so a fresh donor's real config files get backed up while ours (in
// weird re-run edge cases) do not.
func alreadyGenerated(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "# managed by harnessator") ||
		t == "{}" || t == "{}\n"
}

// inferTargets maps present harness layout dirs to the harness targets.
// The synthetic AgentSkills-root claim never counts (a pile is not a
// project layout).
func inferTargets(donor string) []string {
	set := map[string]bool{}
	for _, rec := range materialize.Recognitions() {
		if rec.Dir == "." || rec.Target == "agentskills" {
			continue
		}
		if fi, err := os.Stat(filepath.Join(donor, filepath.FromSlash(rec.Dir))); err == nil && fi.IsDir() {
			set[rec.Target] = true
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// ensureGitRepo bootstraps the published source as a committed git repo
// when it is not one yet (donor adoption clones it via file://). Returns
// whether it initialized + committed.
func ensureGitRepo(ctx context.Context, dir, pkg string) (bool, error) {
	if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && fi.IsDir() {
		return false, nil
	}
	if _, err := gitops.Run(ctx, dir, "init", "-b", "main"); err != nil {
		return false, err
	}
	if _, err := gitops.Run(ctx, dir, "add", "."); err != nil {
		return false, err
	}
	msg := fmt.Sprintf("harnessator harvest: publish %s", pkg)
	if out, err := gitops.Run(ctx, dir, "commit", "-qm", msg); err != nil {
		return false, fmt.Errorf("git commit: %v\n%s", err, out)
	}
	return true, nil
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
