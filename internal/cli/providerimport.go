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
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/provider"
)

// providerImportCmd is harvest for the provider layer: read the live,
// working configuration of installed harnesses and emit provider
// profiles into a source - multiple per harness where the harness
// itself allows multiple (opencode, codex). Secret values never cross;
// unmappable harnesses say so in one line each. Plan first, write
// second: --dry-run touches nothing, not even a directory.
func providerImportCmd(stdout, stderr io.Writer, args []string, name, into string, dry bool) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: harnessator provider import <harness|*> [--name <dir> | --into <source-dir>] [--dry-run]")
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cwd, _ := os.Getwd()
	res := provider.Import(args, home, cwd)

	for _, s := range res.Skipped {
		fmt.Fprintf(stdout, "skip  %s\n", s)
	}
	for _, f := range res.Failures {
		fmt.Fprintln(stderr, "FAIL  "+f)
	}
	if len(res.Imported) == 0 {
		if len(res.Failures) > 0 {
			return 1
		}
		fmt.Fprintln(stdout, "nothing importable - no profiles written")
		return 0
	}

	target := name
	mode := "new"
	if into != "" {
		target = into
		mode = "into"
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	m := &config.SourceManifest{Kind: config.KindSource, Schema: 1,
		Package: config.Package{Name: filepath.Base(absTarget),
			Description: "provider profiles imported from live harness configurations"},
		Components: map[string]map[string]config.Component{},
		Profiles:   map[string]config.Profile{}}
	if mode == "into" {
		existing, err := config.LoadSourceManifest(filepath.Join(absTarget, "harnessator.toml"))
		if err != nil {
			fmt.Fprintf(stderr, "--into needs an existing (non-pointer) source manifest: %v\n", err)
			return 1
		}
		if existing.Pointer() {
			fmt.Fprintln(stderr, "--into a POINTER source would place provider files in THEIR content tree; import into a repo you own")
			return 1
		}
		m = existing
		if m.Components == nil {
			m.Components = map[string]map[string]config.Component{}
		}
	} else if _, err := os.Stat(absTarget); err == nil {
		fmt.Fprintf(stderr, "refusing to write into existing %s - review it or extend it deliberately with --into\n", absTarget)
		return 1
	}

	// ---- plan: classify every import before touching anything -------
	type pendingFile struct {
		imp  provider.Imported
		text string
		rel  string
		abs  string
	}
	var writes []pendingFile
	var skips, conflicts int
	for _, imp := range res.Imported {
		text := provider.RenderProfile(imp.Profile)
		rel := filepath.ToSlash(filepath.Join("providers", imp.ID+".toml"))
		abs := filepath.Join(absTarget, filepath.FromSlash(rel))
		sum := sha256.Sum256([]byte(text))
		if old, err := os.ReadFile(abs); err == nil {
			if sha256.Sum256(old) == sum {
				fmt.Fprintf(stdout, "skip-identical  provider/%s\n", imp.ID)
				skips++
				continue
			}
			fmt.Fprintf(stderr, "FAIL  provider/%s exists with DIFFERENT content - reconcile in %s, not by re-import\n", imp.ID, absTarget)
			conflicts++
			continue
		}
		fmt.Fprintf(stdout, "import  provider/%s  <- %s live config (%s)\n", imp.ID, imp.Harness, imp.Origin)
		for _, n := range imp.Notes {
			fmt.Fprintf(stdout, "        %s\n", n)
		}
		writes = append(writes, pendingFile{imp: imp, text: text, rel: rel, abs: abs})
	}
	if dry {
		fmt.Fprintf(stdout, "\ndry run - %d profile(s) would be written to %s (%d skip-identical, %d conflict(s))\n",
			len(writes), absTarget, skips, conflicts)
		if conflicts > 0 {
			return 1
		}
		return 0
	}
	if conflicts > 0 {
		return 1
	}
	if len(writes) == 0 {
		fmt.Fprintln(stdout, "nothing new; source unchanged")
		return 0
	}

	// ---- execute ------------------------------------------------------
	if mode == "new" {
		if err := os.MkdirAll(absTarget, 0o755); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if m.Components["provider"] == nil {
		m.Components["provider"] = map[string]config.Component{}
	}
	var ids []string
	for _, w := range writes {
		if err := os.MkdirAll(filepath.Dir(w.abs), 0o755); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := os.WriteFile(w.abs, []byte(w.text), 0o644); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		m.Components["provider"][w.imp.ID] = config.Component{
			Path:        w.rel,
			Description: fmt.Sprintf("imported from %s live config at %s", w.imp.Harness, w.imp.Origin),
			Origin:      "harnessator provider import " + w.imp.Harness,
		}
		ids = append(ids, w.imp.ID)
	}
	def := m.Profiles["default"]
	if def == nil {
		def = map[string][]string{}
	}
	def["provider"] = appendUnique(def["provider"], ids...)
	m.Profiles["default"] = def
	if err := config.WriteSource(filepath.Join(absTarget, "harnessator.toml"), m); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(res.Failures) > 0 {
		fmt.Fprintln(stderr, "imported what succeeded; FAIL lines above block trust - fix and re-run")
	}
	// same courtesy harvest extends: a fresh source is immediately
	// consumable (file:// needs a repo) and reviewable (one commit).
	if mode == "new" {
		if out, gerr := gitRun(absTarget, "init", "-b", "main"); gerr != nil {
			fmt.Fprintf(stderr, "git init failed (source is complete on disk): %v\n%s\n", gerr, out)
		} else if _, gerr := gitRun(absTarget, "add", "."); gerr == nil {
			msg := fmt.Sprintf("harnessator provider import: %d profile(s) from live harness configs", len(writes))
			if _, gerr := gitRun(absTarget, "commit", "-qm", msg); gerr != nil {
				fmt.Fprintln(stderr, "git commit failed (files are on disk): "+identityHint(gerr.Error()))
			}
		}
	}
	fmt.Fprintf(stdout, "\nwrote %d profile(s) to %s\nnext:\n  harnessator source add file://%s\n  harnessator provider use %s:provider/<id>   (harnessator profiles %s shows them)\n  harnessator tune --yes                      (probe, reconcile catalogs, re-render)\n",
		len(writes), absTarget, absTarget, filepath.Base(absTarget), filepath.Base(absTarget))
	if len(res.Failures) > 0 {
		return 1
	}
	return 0
}

func appendUnique(list []string, add ...string) []string {
	have := map[string]bool{}
	for _, x := range list {
		have[x] = true
	}
	for _, a := range add {
		if !have[a] {
			list = append(list, a)
		}
	}
	return list
}
