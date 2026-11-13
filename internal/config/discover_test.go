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

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeSkillRepo writes a source repo with a discover manifest; returns dir.
func makeSkillRepo(t *testing.T, manifest string) string {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "harnessator.toml"), manifest)
	writeFile(t, filepath.Join(dir, "skills", "alpha", "SKILL.md"),
		"---\nname: alpha\ndescription: Folded one\n  folded two\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "skills", "beta", "SKILL.md"),
		"# no frontmatter\nplain skill\n")
	writeFile(t, filepath.Join(dir, "skills", "gamma", "SKILL.md"),
		"---\nname: gamma\ndescription: \"Fully quoted\"\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "skills", "notaskill", "README.md"), "nothing\n")
	return dir
}

func TestDiscoverRegistersSkillDirsLive(t *testing.T) {
	dir := makeSkillRepo(t, `kind = "source"
schema = 2

[package]
name = "sp"

[discover]
skill = "skills/*"

[profiles."all"]
skill = ["*"]
`)
	m, err := LoadSourceManifest(filepath.Join(dir, "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	skills := m.Components["skill"]
	if len(skills) != 3 {
		t.Fatalf("components = %d, want alpha+beta+gamma (notaskill has no SKILL.md): %+v", len(skills), skills)
	}
	if got := skills["alpha"].Description; got != "Folded one folded two" {
		t.Errorf("alpha description = %q", got)
	}
	if skills["beta"].Description != "" {
		t.Errorf("beta description = %q, want empty", skills["beta"].Description)
	}
	if got := skills["gamma"].Description; got != "Fully quoted" {
		t.Errorf("gamma description = %q, want quotes stripped", got)
	}
	if skills["alpha"].Path != "skills/alpha" {
		t.Errorf("alpha path = %q", skills["alpha"].Path)
	}
	if !strings.HasPrefix(skills["alpha"].Origin, "discovered from ") {
		t.Errorf("origin = %q", skills["alpha"].Origin)
	}
	if err := ValidateSource(m, dir); err != nil {
		t.Errorf("discovered manifest must validate: %v", err)
	}
}

func TestDiscoverExplicitWins(t *testing.T) {
	dir := makeSkillRepo(t, `kind = "source"
schema = 2

[package]
name = "sp"

[discover]
skill = "skills/*"

[component.skill."alpha"]
path = "skills/alpha"
description = "hand written"
tags = ["curated"]
`)
	m, err := LoadSourceManifest(filepath.Join(dir, "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	a := m.Components["skill"]["alpha"]
	if a.Description != "hand written" || len(a.Tags) != 1 {
		t.Errorf("explicit entry was overwritten: %+v", a)
	}
	if _, ok := m.Components["skill"]["beta"]; !ok {
		t.Error("beta should still be discovered")
	}
}

func TestDiscoverRejections(t *testing.T) {
	cases := []struct{ name, manifest, want string }{
		{"schema1", "kind = \"source\"\nschema = 1\n[package]\nname=\"sp\"\n[discover]\nskill=\"skills/*\"\n",
			"requires schema = 2"},
		{"category", "kind = \"source\"\nschema = 2\n[package]\nname=\"sp\"\n[discover]\nrule=\"rules/*\"\n",
			"not supported"},
		{"no-star", "kind = \"source\"\nschema = 2\n[package]\nname=\"sp\"\n[discover]\nskill=\"skills\"\n",
			"plus /*"},
		{"absolute", "kind = \"source\"\nschema = 2\n[package]\nname=\"sp\"\n[discover]\nskill=\"/skills/*\"\n",
			"inside the source"},
		{"escape", "kind = \"source\"\nschema = 2\n[package]\nname=\"sp\"\n[discover]\nskill=\"../secrets/*\"\n",
			"inside the source"},
		{"missing-dir", "kind = \"source\"\nschema = 2\n[package]\nname=\"sp\"\n[discover]\nskill=\"nope/*\"\n",
			"nope"},
		{"bad-id", "kind = \"source\"\nschema = 2\n[package]\nname=\"sp\"\n[discover]\nskill=\"skills/*\"\n",
			"valid component id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := makeSkillRepo(t, tc.manifest)
			if tc.name == "bad-id" {
				writeFile(t, filepath.Join(dir, "skills", "bad id!", "SKILL.md"), "x\n")
			}
			_, err := LoadSourceManifest(filepath.Join(dir, "harnessator.toml"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestDiscoverMarshalRoundTrip(t *testing.T) {
	dir := makeSkillRepo(t, `kind = "source"
schema = 2

[package]
name = "sp"

[discover]
skill = "skills/*"
`)
	m, err := LoadSourceManifest(filepath.Join(dir, "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := MarshalSource(m)
	if err != nil {
		t.Fatal(err)
	}
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir2, "harnessator.toml"), string(raw))
	if _, err := os.Stat(filepath.Join(dir2, "skills")); err != nil {
		// marshal-to-a-different-tree still needs the payload for discover
		writeFile(t, filepath.Join(dir2, "skills", "alpha", "SKILL.md"), "---\ndescription: x\n---\n")
		writeFile(t, filepath.Join(dir2, "skills", "beta", "SKILL.md"), "x\n")
	}
	m2, err := LoadSourceManifest(filepath.Join(dir2, "harnessator.toml"))
	if err != nil {
		t.Fatalf("reload: %v\n---\n%s", err, raw)
	}
	if m2.Schema != 2 || m2.Discover["skill"] != "skills/*" {
		t.Errorf("discover lost in round-trip: %+v", m2)
	}
}
