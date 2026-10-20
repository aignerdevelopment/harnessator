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
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/harvest"
)

// harvestCmd publishes scanned candidates as a validated source repo. All conflicts, sanitize findings and invalid ids
// abort BEFORE any write; identical copies merge; divergent ones need
// --prefer. Inputs are never modified.
func harvestCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("harvest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "new source id (created as ./<name>)")
	into := fs.String("into", "", "existing source directory to extend")
	prefer := fs.String("prefer", "", "divergent copies: newest|largest")
	dryRun := fs.Bool("dry-run", false, "print the write plan; write nothing")
	allowHits := fs.Bool("allow-hits", false, "publish despite sanitize findings (review them first!)")
	commit := fs.String("commit", "", "ratify guided edits: re-validate this source dir and commit it")
	adopt := fs.Bool("adopt", false, "after publishing, make donor projects consumers (digest-verified original removal)")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if *commit != "" {
		return ratifyCommit(stdout, stderr, *commit)
	}
	paths := fs.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	res, err := harvest.Harvest(harvest.Options{
		Paths: paths, Name: *name, Into: *into, Prefer: *prefer,
		DryRun: *dryRun, AllowHits: *allowHits, Adopt: *adopt,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	fmt.Fprintf(stdout, "harvest -> %s: %d add, %d skip-identical\n", res.Output, res.Added, res.Skipped)
	for _, p := range res.Planned {
		fmt.Fprintf(stdout, "  %-15s %-9s %-26s %s\n", p.Action, p.Category, p.ID, p.From+" -> "+p.To)
	}
	if *dryRun {
		fmt.Fprintln(stdout, "dry run - nothing written.")
		return 0
	}

	if *into == "" && !res.Committed {
		if out, gerr := gitRun(res.Output, "init", "-b", "main"); gerr != nil {
			fmt.Fprintf(stderr, "git init failed (source is complete on disk): %v\n%s\n", gerr, out)
		} else if _, gerr := gitRun(res.Output, "add", "."); gerr == nil {
			msg := fmt.Sprintf("harnessator harvest: %d component(s) published", res.Added)
			if _, gerr := gitRun(res.Output, "commit", "-qm", msg); gerr != nil {
				fmt.Fprintf(stderr, "git commit failed: %v\n", gerr)
			}
		}
		fmt.Fprintln(stdout, "next: create the remote (harnessator source new --remote ...) or:")
		fmt.Fprintf(stdout, "  cd %s && git remote add origin <url> && git push -u origin main\n", res.Output)
	} else {
		fmt.Fprintln(stdout, "existing source extended - review, commit and push there.")
	}
	if res.Residual > 0 {
		fmt.Fprintf(stdout, "\n%d entry(s) left behind -> %s\n", res.Residual, filepath.Join(res.Output, "harvest-report.json"))
		fmt.Fprintln(stdout, "for guided completion: run the /harnessator-harvest skill (harnessator enable harnessator:default), then ratify:")
		fmt.Fprintf(stdout, "  harnessator harvest --commit %s\n", res.Output)
	}
	if len(res.Adopted) > 0 {
		fmt.Fprintln(stdout, "\nadoption:")
		for _, ar := range res.Adopted {
			switch {
			case ar.Skipped != "":
				fmt.Fprintf(stdout, "  %-40s skipped: %s\n", ar.Donor, ar.Skipped)
			default:
				fmt.Fprintf(stdout, "  %-40s %d original(s) -> managed links\n", ar.Donor, ar.Linked)
			}
			for _, k := range ar.Kept {
				fmt.Fprintf(stdout, "    kept: %s\n", k)
			}
			for _, nt := range ar.Notes {
				fmt.Fprintf(stdout, "    note: %s\n", nt)
			}
		}
	}
	fmt.Fprintln(stdout, "consumers: harnessator source add <url> --ref main && harnessator enable <name>:default && harnessator setup")
	return 0
}

// ratifyCommit is the guided tail's gate: agent or human
// edits inside a harvested source become real only after the deterministic
// validation passes - agent drafts, tool ratifies.
func ratifyCommit(stdout, stderr io.Writer, dir string) int {
	man := filepath.Join(dir, "harnessator.toml")
	m, err := config.LoadSourceManifest(man)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := config.ValidateSource(m, dir); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	n := 0
	for _, byID := range m.Components {
		n += len(byID)
	}
	if out, gerr := gitRun(dir, "add", "."); gerr != nil {
		fmt.Fprintf(stderr, "git add: %v\n%s\n", gerr, out)
		return 1
	}
	msg := fmt.Sprintf("harnessator harvest: ratified %d component(s)", n)
	out, gerr := gitRun(dir, "commit", "-qm", msg)
	if gerr != nil {
		if strings.Contains(out, "nothing to commit") {
			fmt.Fprintf(stdout, "ratified: %s validated (%d components), nothing new to commit\n", dir, n)
			return 0
		}
		fmt.Fprintf(stderr, "git commit: %v\n%s%s", gerr, out, identityHint(out))
		return 1
	}
	fmt.Fprintf(stdout, "ratified: %s validated and committed (%d components)\n", dir, n)
	fmt.Fprintln(stdout, "consumers: harnessator update pulls it once pushed")
	return 0
}
