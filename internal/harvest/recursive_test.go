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
	"os"
	"path/filepath"
	"testing"

	"harnessator.org/harnessator/internal/config"
)

// scanTree nested-group ids + corpus-convention pile dirs.
func TestRecursiveRecognition(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	pa := filepath.Join(base, "work", "projA")
	// .cursor/rules: one flat file + two groups holding the same
	// filename with different content -> group-prefixed distinct ids.
	write(t, filepath.Join(pa, ".cursor/rules/flat.mdc"), "---\ndescription: Flat\n---\nbody\n")
	write(t, filepath.Join(pa, ".cursor/rules/frameworks/react.mdc"), "---\ndescription: React\n---\nr1\n")
	write(t, filepath.Join(pa, ".cursor/rules/misc/react.mdc"), "---\ndescription: Misc\n---\nr2\n")
	// corpus-convention piles
	write(t, filepath.Join(pa, "skills/alpha/SKILL.md"), "---\nname: alpha\ndescription: A.\n---\nA\n")
	write(t, filepath.Join(pa, "agents/helper.md"), "# helper\n")
	write(t, filepath.Join(pa, "commands/review.md"), "# review\n")
	// second project with the IDENTICAL skill -> dedupe cluster
	pb := filepath.Join(base, "work", "projB")
	write(t, filepath.Join(pb, "skills/alpha/SKILL.md"), "---\nname: alpha\ndescription: A.\n---\nA\n")
	write(t, filepath.Join(pb, "rules/one.md"), "# one\n")
	// noise that must never be scanned as a project
	write(t, filepath.Join(base, "work/projA/node_modules/pkg/agents/trap.md"), "# trap\n")

	rep, err := ScanWith([]string{filepath.Join(base, "work")}, true)
	if err != nil {
		t.Fatal(err)
	}
	byRoot := map[string]map[string]bool{}
	for _, r := range rep.Roots {
		set := map[string]bool{}
		for _, c := range r.Components {
			set[c.Category+"/"+c.ID] = true
		}
		byRoot[filepath.Base(r.Path)] = set
	}
	// discovery must have descended work -> projA/projB
	if len(byRoot["projA"]) == 0 || len(byRoot["projB"]) == 0 {
		t.Fatalf("recursive discovery missed projects: %+v", byRoot)
	}
	wantA := []string{"rule/flat", "rule/frameworks-react", "rule/misc-react",
		"skill/alpha", "agent/helper", "prompt/review"}
	for _, w := range wantA {
		if !byRoot["projA"][w] {
			t.Errorf("projA missing %s (have %+v)", w, byRoot["projA"])
		}
	}
	if !byRoot["projB"]["rule/one"] || !byRoot["projB"]["skill/alpha"] {
		t.Errorf("projB = %+v", byRoot["projB"])
	}
	// node_modules trap never discovered
	for root, set := range byRoot {
		if set["agent/trap"] {
			t.Errorf("descended into node_modules via %s", root)
		}
	}
	// identical copies across roots cluster as identical
	found := false
	for _, cl := range rep.Clusters {
		if cl.Category == "skill" && cl.ID == "alpha" && cl.Identical {
			found = true
		}
	}
	if !found {
		t.Error("alpha copies did not cluster as identical")
	}
}

// whole-forest harvest: unique content lands once, manifest validates,
// and --dry-run without --name plans without writing.
func TestHarvestRecursiveForest(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	write(t, filepath.Join(base, "fw/projA/skills/alpha/SKILL.md"), "---\nname: alpha\ndescription: A.\n---\nA\n")
	write(t, filepath.Join(base, "fw/projB/skills/alpha/SKILL.md"), "---\nname: alpha\ndescription: A.\n---\nA\n")
	write(t, filepath.Join(base, "fw/projB/commands/go.md"), "# go\n")

	res, err := Harvest(Options{Paths: []string{"fw"}, Name: "team", Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 2 { // alpha once + go once, despite two donors
		t.Fatalf("added = %d (%+v)", res.Added, res.Planned)
	}
	out := filepath.Join(base, "team")
	m, err := config.LoadSourceManifest(filepath.Join(out, "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := config.ValidateSource(m, out); err != nil {
		t.Fatalf("published source invalid: %v", err)
	}

	// plan mode: no name, no into, nothing written
	p, err := Harvest(Options{Paths: []string{"fw"}, Recursive: true, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run without --name failed: %v", err)
	}
	if len(p.Planned) != 2 || p.Output != "(dry-run plan)" {
		t.Errorf("plan = %+v", p)
	}
	if _, err := os.Stat(filepath.Join(base, "dry-run")); err == nil {
		t.Error("plan mode created a directory")
	}
}
