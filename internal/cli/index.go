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
	"strings"
	"time"

	"harnessator.org/harnessator/internal/app"
	"harnessator.org/harnessator/internal/index"
	"harnessator.org/harnessator/internal/store"
)

// indexCmd: local-first registry tooling.
//
//	harnessator index build   - (re)build the store index from the project's pinned sources
//	harnessator index search  - query the store index (no project needed)
//	harnessator index show    - full provenance for one indexed component
func indexCmd(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: harnessator index build|search|show [...]")
		return 2
	}
	switch args[0] {
	case "build":
		return indexBuild(stdout, stderr, args[1:])
	case "search":
		return indexSearch(stdout, stderr, args[1:])
	case "show":
		return indexShow(stdout, stderr, args[1:])
	default:
		fmt.Fprintf(stderr, "unknown index subcommand %q (build|search|show)\n", args[0])
		return 2
	}
}

func indexBuild(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("index build", flag.ContinueOnError)
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
	entries := index.Build(a.Manifests, a.Pins)
	if len(entries) == 0 {
		fmt.Fprintln(stderr, "no pinned sources with manifests - run setup first")
		return 1
	}
	names := make([]string, 0, len(a.Pins))
	for n := range a.Pins {
		names = append(names, n)
	}
	st, err := store.Default()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	base, err := index.Load(st.IndexFile())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	f := index.Merge(base, names, entries)
	f.Built = time.Now().UTC().Format(time.RFC3339)
	if err := index.Write(st.IndexFile(), f); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "index built: %d component(s) from %d source(s) -> %s\n",
		len(f.Entries), len(names), st.IndexFile())
	return 0
}

func indexSearch(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("index search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	category := fs.String("category", "", "restrict to one component category")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintf(stderr, "warning: extra arguments ignored: %v (flags must precede the search text)\n", fs.Args()[1:])
	}
	f, code := loadIndex(stderr)
	if f == nil {
		return code
	}
	hits := index.Query(f, fs.Arg(0), *category)
	if len(hits) == 0 {
		fmt.Fprintln(stderr, "no matches")
		return 1
	}
	for _, e := range hits {
		tags := ""
		if len(e.Tags) > 0 {
			tags = "  #" + strings.Join(e.Tags, " #")
		}
		fmt.Fprintf(stdout, "%-10s %s — %s (%s@%.8s)%s\n",
			e.Category, e.ID, orDash(e.Description), e.Source, e.SHA, tags)
	}
	return 0
}

func indexShow(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 || args[0] == "" {
		fmt.Fprintln(stderr, "usage: harnessator index show <ref>")
		return 2
	}
	f, code := loadIndex(stderr)
	if f == nil {
		return code
	}
	e, ok := index.Find(f, args[0])
	if !ok {
		fmt.Fprintf(stderr, "not in index (ambiguous or unknown): %s - try `harnessator index build`\n", args[0])
		return 1
	}
	fmt.Fprintf(stdout, "component:  %s\n", e.ID)
	fmt.Fprintf(stdout, "category:   %s\n", e.Category)
	fmt.Fprintf(stdout, "path:       %s\n", e.Path)
	fmt.Fprintf(stdout, "described:  %s\n", orDash(e.Description))
	fmt.Fprintf(stdout, "tags:       %s\n", strings.Join(e.Tags, ", "))
	fmt.Fprintf(stdout, "targets:    %s\n", orDash(strings.Join(e.Targets, ", ")))
	fmt.Fprintf(stdout, "license:    %s\n", orDash(e.License))
	fmt.Fprintf(stdout, "origin:     %s\n", orDash(e.Origin))
	fmt.Fprintf(stdout, "source:     %s  (%s)\n", e.Source, e.Git)
	fmt.Fprintf(stdout, "pin:        %s -> %s @ %.12s\n", orDash(e.Range), orDash(e.Resolved), e.SHA)
	return 0
}

func loadIndex(stderr io.Writer) (*index.File, int) {
	st, err := store.Default()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil, 1
	}
	f, err := index.Load(st.IndexFile())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil, 1
	}
	if len(f.Entries) == 0 {
		fmt.Fprintln(stderr, "registry index is empty - run `harnessator index build` in a managed project")
		return nil, 1
	}
	return f, 0
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
