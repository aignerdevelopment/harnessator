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
	"path/filepath"
	"strings"

	"harnessator.org/harnessator/internal/app"
	"harnessator.org/harnessator/internal/resolve"
)

// setup resolves sources (fetching), materializes everything, writes the
// lock. `update` is the same code path (re-resolve is the update).
func setup(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "print the plan without touching the project")
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	a, err := app.Open(context.Background(), *projectFile, app.ModeFetch)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	reportPins(stdout, a)

	fmt.Fprintf(stdout, "\nplan (%s):\n", mode(*dryRun))
	printPlan(stdout, a.Plan)
	if *dryRun {
		fmt.Fprintln(stdout, "\nnothing written (dry run).")
		return 0
	}

	log := indentWriter(stdout)
	if err := a.Setup(context.Background(), false, log); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprint(stdout, "\nrestart your harness(es) to pick up the new configuration.\n")
	warn(stderr, a.Plan.Warnings)
	return 0
}

// verify: CI gate, state must match harnessator.lock.
func verify(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	a, err := app.Open(context.Background(), *projectFile, app.ModeLocked)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	problems, err := a.VerifyDrift()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(stderr, p)
		}
		return 1
	}
	fmt.Fprintln(stdout, "lock state verified ok")
	return 0
}

// doctor reports all state problems (lock, drift, materialization) plus
// hints. Exit non-zero when something needs attention.
func doctor(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	a, err := app.Open(context.Background(), *projectFile, app.ModeAuto)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "project  %s\n", a.Project)
	fmt.Fprintf(stdout, "targets  %s\n", strings.Join(a.Cfg.Targets, ", "))
	reportPins(stdout, a)
	if a.FromLock {
		fmt.Fprintln(stdout, "mode     locked (harnessator.lock pins; run update after config changes)")
	}

	problems := a.DoctorProblems()
	for _, nt := range a.DoctorNotes() {
		fmt.Fprintln(stdout, "note:    "+nt)
	}

	if len(problems) == 0 {
		fmt.Fprintln(stdout, "\ndoctor: all good")
		return 0
	}
	fmt.Fprintf(stdout, "\nproblems (%d):\n", len(problems))
	for _, p := range problems {
		fmt.Fprintln(stdout, "  - "+p)
	}
	fmt.Fprintln(stdout, "\nfix with: harnessator setup (or update)")
	return 1
}

// list / search / info / why over the resolved plan + manifests.
func listCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	all := fs.Bool("all", false, "include skipped components too")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	a, err := app.Open(context.Background(), *projectFile, app.ModeAuto)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	byCat := a.Plan.ByCategory()
	for _, cat := range resolve.CategoriesInOrder() {
		for _, ch := range byCat[cat] {
			fmt.Fprintf(stdout, "%-10s %s  [%s]\n", cat, ch.QualifiedID(), ch.Reason)
		}
		if *all {
			for _, s := range a.Plan.Skips {
				if s.Choice.Category == cat {
					fmt.Fprintf(stdout, "%-10s %s  (skip: %s)\n", "!"+cat, s.Choice.QualifiedID(), s.Reason)
				}
			}
		}
	}
	return 0
}

func infoCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	if err := parseFlags(fs, args); err != nil || fs.NArg() == 0 {
		fmt.Fprintln(stderr, "usage: harnessator info <source>:<cat>/<id>")
		return 2
	}
	a, err := app.Open(context.Background(), *projectFile, app.ModeAuto)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	src, cat, id := parseRef(fs.Arg(0))
	for _, ch := range a.Plan.Enabled {
		if ch.ID == id && (src == "" || ch.Source == src) && (cat == "" || ch.Category == cat) {
			fmt.Fprintf(stdout, "id:        %s\n", ch.QualifiedID())
			fmt.Fprintf(stdout, "source:    %s (%s @ %s)\n", ch.Source, a.Pins[ch.Source].Resolved, shortSHA(a.Pins[ch.Source].SHA))
			fmt.Fprintf(stdout, "path:      %s\n", filepath.Join(a.Pins[ch.Source].Dir, filepath.FromSlash(ch.Component.Path)))
			fmt.Fprintf(stdout, "reason:    %s\n", ch.Reason)
			fmt.Fprintf(stdout, "targets:   %v\n", orAll(ch.Component.Targets))
			fmt.Fprintf(stdout, "tags:      %v\n", ch.Component.Tags)
			fmt.Fprintf(stdout, "caps:      %v\n", ch.Component.Capabilities)
			fmt.Fprintf(stdout, "desc:      %s\n", ch.Component.Description)
			return 0
		}
	}
	fmt.Fprintln(stderr, "not an enabled component (see: harnessator list --all)")
	return 1
}

func whyCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("why", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	if err := parseFlags(fs, args); err != nil || fs.NArg() == 0 {
		fmt.Fprintln(stderr, "usage: harnessator why <source>:<cat>/<id>")
		return 2
	}
	a, err := app.Open(context.Background(), *projectFile, app.ModeAuto)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	src, cat, id := parseRef(fs.Arg(0))
	found := false
	for _, ch := range a.Plan.Enabled {
		if ch.ID == id && (src == "" || ch.Source == src) && (cat == "" || ch.Category == cat) {
			found = true
			fmt.Fprintf(stdout, "%s enabled by: %s\n", ch.QualifiedID(), ch.Reason)
			if strings.Contains(ch.Reason, "template") {
				tref := strings.TrimPrefix(ch.Reason, "template ")
				if _, prof, ok := strings.Cut(tref, ":"); ok {
					fmt.Fprintf(stdout, "  template %s of source %s selects it\n", prof, strings.Split(tref, ":")[0])
				}
			}
		}
	}
	for _, s := range a.Plan.Skips {
		if s.Choice.ID == id && (src == "" || s.Choice.Source == src) && (cat == "" || s.Choice.Category == cat) {
			found = true
			fmt.Fprintf(stdout, "%s SKIPPED: %s\n", s.Choice.QualifiedID(), s.Reason)
		}
	}
	if !found {
		fmt.Fprintln(stderr, "unknown component (see: harnessator list)")
		return 1
	}
	return 0
}

func searchCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	needle := strings.ToLower(fs.Arg(0))
	if needle == "" {
		fmt.Fprintln(stderr, "usage: harnessator search <text>")
		return 2
	}
	a, err := app.Open(context.Background(), *projectFile, app.ModeAuto)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	enabled := map[string]bool{}
	for _, ch := range a.Plan.Enabled {
		enabled[ch.QualifiedID()] = true
	}
	hits := 0
	for _, name := range sortedManifestNames(a.Manifests) {
		m := a.Manifests[name]
		for _, cat := range resolve.CategoriesInOrder() {
			for _, id := range sortedCompIDs(m, cat) {
				c := m.Components[cat][id]
				q := name + ":" + cat + "/" + id
				blob := strings.ToLower(q + " " + c.Description + " " + strings.Join(c.Tags, " "))
				if strings.Contains(blob, needle) {
					state := "off"
					if enabled[q] {
						state = "on"
					}
					fmt.Fprintf(stdout, "%-4s %s — %s\n", state, q, c.Description)
					hits++
				}
			}
		}
	}
	if hits == 0 {
		fmt.Fprintln(stderr, "no matches")
		return 1
	}
	return 0
}

func parseRef(s string) (src, cat, id string) {
	rest := s
	if p, r, ok := strings.Cut(rest, ":"); ok {
		src, rest = p, r
	}
	if c, i, ok := strings.Cut(rest, "/"); ok {
		cat, id = c, i
	} else {
		id = rest
	}
	return
}

func orAll(list []string) string {
	if len(list) == 0 {
		return "(all)"
	}
	return strings.Join(list, ",")
}

func reportPins(w io.Writer, a *app.App) {
	for _, s := range a.Cfg.Sources {
		if p, ok := a.Pins[s.Name]; ok {
			fmt.Fprintf(w, "pinned %-16s %s -> %s (%s)\n", s.Name, s.Ref, p.Resolved, shortSHA(p.SHA))
		}
	}
}

func warn(w io.Writer, msgs []string) {
	for _, m := range msgs {
		fmt.Fprintf(w, "warning: %s\n", m)
	}
}

// indentWriter prefixes every line, keeping adapter reports tidy.
func indentWriter(w io.Writer) io.Writer {
	return &prefixWriter{w: w, prefix: "  "}
}

type prefixWriter struct {
	w       io.Writer
	prefix  string
	atStart bool
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	total := 0
	for _, line := range strings.SplitAfter(string(b), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasSuffix(line, "\n") {
			line += "\n"
		}
		n, err := fmt.Fprintf(p.w, "%s%s", p.prefix, line)
		total += n
		if err != nil {
			return total, err
		}
	}
	return len(b), nil
}
