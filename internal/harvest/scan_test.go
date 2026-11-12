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
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanRootsClustersSanitize(t *testing.T) {
	base := t.TempDir()
	rootA := filepath.Join(base, "projA")
	rootB := filepath.Join(base, "projB")

	// skill alpha: divergent across roots
	write(t, filepath.Join(rootA, ".claude/skills/alpha/SKILL.md"), "---\nname: alpha\ndescription: A.\n---\nbody v1\n")
	write(t, filepath.Join(rootB, ".claude/skills/alpha/SKILL.md"), "---\nname: alpha\ndescription: A.\n---\nbody v2 IMPROVED IN ONE PROJECT\n")
	// skill same: identical across roots
	for _, r := range []string{rootA, rootB} {
		write(t, filepath.Join(r, ".claude/skills/same/SKILL.md"), "---\nname: same\ndescription: S.\n---\nsame\n")
	}
	// prompt with sanitize hits
	write(t, filepath.Join(rootA, ".claude/commands/review.md"),
		"# Review\napi_key = \"sk-live-abcdefghij0123456789\"\nsee /home/roland/notes.md\n")
	// cursor mdc rule + stray dir without SKILL.md must be skipped
	write(t, filepath.Join(rootA, ".cursor/rules/security.mdc"), "alwaysApply: true\nbody\n")
	write(t, filepath.Join(rootA, ".claude/skills/notaskill/random.txt"), "x\n")
	// a component with an invalid harvest id (space)
	write(t, filepath.Join(rootA, ".claude/skills/WEIRD NAME/SKILL.md"), "---\nname: x\n---\n")

	// managed + external links (rootA)
	storeDir := filepath.Join(base, ".harnessator", "sources", "corp", "checkouts", "deadbeef", "skills", "linked")
	write(t, filepath.Join(storeDir, "SKILL.md"), "---\nname: linked\ndescription: L.\n---\n")
	os.MkdirAll(filepath.Join(rootA, ".claude/skills"), 0o755)
	if err := os.Symlink(storeDir, filepath.Join(rootA, ".claude/skills/linked")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	outside := filepath.Join(base, "gem-skills", "handmade")
	write(t, filepath.Join(outside, "SKILL.md"), "---\nname: handmade\ndescription: H.\n---\n")
	if err := os.Symlink(outside, filepath.Join(rootA, ".claude/skills/handmade")); err != nil {
		t.Fatal(err)
	}
	// harnessator-managed root marker
	write(t, filepath.Join(rootA, ".harnessator/active.md"), "# x\n")

	rep, err := Scan([]string{rootA, rootB})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Schema != 1 || rep.Roots[0].Path != rootA {
		t.Fatalf("envelope wrong: %+v", rep.Roots[0])
	}
	if !rep.Roots[0].Managed {
		t.Error("rootA should be detected as managed (active.md present)")
	}

	compNames := map[string]string{} // id -> category
	for _, c := range rep.Roots[0].Components {
		compNames[c.ID] = c.Category
	}
	for id, cat := range map[string]string{"alpha": "skill", "same": "skill", "review": "prompt", "security": "rule"} {
		if got := compNames[id]; got != cat {
			t.Errorf("component %s: category %q want %q (have %v)", id, got, cat, compNames)
		}
	}
	if _, ok := compNames["notaskill"]; ok {
		t.Error("stray dir without SKILL.md must not be a component")
	}
	if _, ok := compNames["linked"]; ok {
		t.Error("managed symlink must not be harvested as content")
	}

	// links recorded with correct state
	states := map[string]string{}
	for _, l := range rep.Roots[0].Linked {
		states[l.Path] = l.State
	}
	if states[".claude/skills/linked"] != "managed" {
		t.Errorf("store symlink should be 'managed': %+v", rep.Roots[0].Linked)
	}
	if states[".claude/skills/handmade"] != "external" {
		t.Errorf("outside symlink should be 'external': %+v", rep.Roots[0].Linked)
	}

	// sanitize hits on review.md
	var rev *Component
	for i := range rep.Roots[0].Components {
		if rep.Roots[0].Components[i].ID == "review" {
			rev = &rep.Roots[0].Components[i]
		}
	}
	if rev == nil {
		t.Fatal("review prompt missing")
	}
	rules := map[string]bool{}
	for _, h := range rev.Hits {
		rules[h.Rule] = true
	}
	for want := range map[string]bool{"secret-assignment": true, "personal-path": true} {
		if !rules[want] {
			t.Errorf("sanitize hit %q missing in %+v", want, rev.Hits)
		}
	}
	for _, h := range rev.Hits {
		if strings.Contains(h.Sample, "sk-live-abcdefghij0123456789") {
			t.Errorf("sample must be masked: %q", h.Sample)
		}
	}

	// invalid id flagged, not dropped
	if compNames["WEIRD NAME"] != "skill" {
		t.Fatalf("WEIRD NAME missing from %+v", compNames)
	}
	for _, c := range rep.Roots[0].Components {
		if c.ID == "WEIRD NAME" && c.ValidID {
			t.Error("id with space must be flagged ValidID=false")
		}
	}

	// clusters: alpha divergent (2 variants), same identical
	byID := map[string]Cluster{}
	for _, cl := range rep.Clusters {
		byID[cl.ID] = cl
	}
	if a := byID["alpha"]; a.Identical || len(a.Groups) != 2 {
		t.Errorf("alpha should be DIVERGENT with 2 variants: %+v", a)
	}
	if s := byID["same"]; !s.Identical || len(s.Groups) != 1 || len(s.Groups[0].Locations) != 2 {
		t.Errorf("same cluster wrong: %+v", byID["same"])
	}

	// JSON envelope round-trips (dashboard contract)
	var buf bytes.Buffer
	if err := rep.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	if back.Schema != rep.Schema || len(back.Clusters) != len(rep.Clusters) {
		t.Fatal("json round-trip lost data")
	}

	// human report derives from the same data
	var text bytes.Buffer
	rep.WriteText(&text)
	for _, want := range []string{"DIVERGENT", "secret-assignment", "managed"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text report missing %q:\n%s", want, text.String())
		}
	}
}

func TestScanRejectsMissingPath(t *testing.T) {
	if _, err := Scan([]string{filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestScanLoosePileAndSingleSkill(t *testing.T) {
	base := t.TempDir()
	pile := filepath.Join(base, "my-skills")
	write(t, filepath.Join(pile, "pdf-tools/SKILL.md"), "---\nname: pdf-tools\ndescription: P.\n---\n")
	write(t, filepath.Join(pile, "sql-style/SKILL.md"), "---\nname: sql-style\ndescription: Q.\n---\n")
	single := filepath.Join(base, "solo-skill")
	write(t, filepath.Join(single, "SKILL.md"), "---\nname: solo-skill\ndescription: S.\n---\n")

	rep, err := Scan([]string{pile, single})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range rep.Roots {
		for _, c := range r.Components {
			if c.Category == "skill" {
				ids[c.ID] = true
			}
		}
	}
	for _, want := range []string{"pdf-tools", "sql-style", "solo-skill"} {
		if !ids[want] {
			t.Errorf("loose skill %q not found in %+v", want, ids)
		}
	}
}
