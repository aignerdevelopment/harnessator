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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"harnessator.org/harnessator/internal/config"
)

func gitProbe(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// The published manifest must carry attribution and contain zero
// machine-local paths (round 11 gaps).
func TestHarvestProvenanceUpstreamLabels(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	donor := filepath.Join(base, "upstream")
	write(t, filepath.Join(donor, "LICENSE"),
		"MIT License\n\nCopyright (c) 2026 Example Author\n\n"+
			"Permission is hereby granted, free of charge, to any person obtaining a copy.\n")
	write(t, filepath.Join(donor, "skills", "demo", "SKILL.md"),
		"---\nname: demo\ndescription: Demo skill.\n---\nBody.\n")
	gitProbe(t, donor, "init", "-qb", "main")
	gitProbe(t, donor, "remote", "add", "origin", "https://github.com/example/upstream.git")
	gitProbe(t, donor, "add", ".")
	gitProbe(t, donor, "commit", "-qm", "seed")

	res, err := Harvest(Options{Paths: []string{filepath.Join(donor, "skills")}, Name: "pub"})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(base, "pub")

	carried, err := os.ReadFile(filepath.Join(out, "LICENSE"))
	if err != nil || !strings.Contains(string(carried), "Example Author") {
		t.Fatalf("LICENSE not carried: %v %q", err, carried)
	}
	if len(res.CarriedLicenses) != 1 || res.CarriedLicenses[0] != "LICENSE" {
		t.Errorf("CarriedLicenses = %v", res.CarriedLicenses)
	}

	m, err := config.LoadSourceManifest(filepath.Join(out, "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Package.License != "MIT" {
		t.Errorf("package license = %q", m.Package.License)
	}
	if m.Package.Homepage != "https://github.com/example/upstream" {
		t.Errorf("homepage = %q", m.Package.Homepage)
	}
	orig := m.Components["skill"]["demo"].Origin
	if !strings.Contains(orig, "https://github.com/example/upstream@") ||
		!strings.Contains(orig, ":skills/demo") {
		t.Errorf("origin = %q", orig)
	}
	if strings.Contains(orig, base) {
		t.Errorf("origin leaks machine path: %q", orig)
	}
}

func TestHarvestProvenanceNoGitNoLicense(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	loose := filepath.Join(base, "loose-skills")
	write(t, filepath.Join(loose, "greet", "SKILL.md"),
		"---\nname: greet\ndescription: Greets.\n---\nhi\n")

	res, err := Harvest(Options{Paths: []string{loose}, Name: "pub"})
	if err != nil {
		t.Fatal(err)
	}
	if res.LicenseNote == "" {
		t.Error("expected license warning for unlicensed donor")
	}
	m, err := config.LoadSourceManifest(filepath.Join(base, "pub", "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	orig := m.Components["skill"]["greet"].Origin
	if !strings.HasPrefix(orig, "harvested from loose-skills:greet") {
		t.Errorf("origin = %q", orig)
	}
	if strings.Contains(orig, base) {
		t.Errorf("origin leaks machine path: %q", orig)
	}
}

// The sleipniterm scenario: an agent authors rules into a git donor's
// worktree; harvesting there and then used to record HEAD as if the
// bytes were in it - a provenance lie. Now: clean files keep the exact
// claim, dirty ones say "(uncommitted)", strict mode refuses, and after
// committing the donor the claim is true again.
func TestHarvestUncommittedProvenanceHonesty(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	donor := filepath.Join(base, "app")
	write(t, filepath.Join(donor, "rules", "committed.md"), "stable law\n")
	write(t, filepath.Join(donor, "rules", "will-drift.md"), "original\n")
	gitProbe(t, donor, "init", "-qb", "main")
	gitProbe(t, donor, "add", ".")
	gitProbe(t, donor, "commit", "-qm", "seed")

	write(t, filepath.Join(donor, "rules", "will-drift.md"), "uncommitted edit\n")
	write(t, filepath.Join(donor, "rules", "brand-new.md"), "agent authored, never committed\n")
	write(t, filepath.Join(donor, "skills", "fresh", "SKILL.md"),
		"---\ndescription: Fresh skill in a wholly untracked directory.\n---\nbody\n")

	res, err := Harvest(Options{Paths: []string{donor}, Name: "pub1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Uncommitted != 3 {
		t.Errorf("Uncommitted = %d, want 3 (drift + brand-new + fresh skill dir)", res.Uncommitted)
	}
	mCheck, err := config.LoadSourceManifest(filepath.Join(base, "pub1", "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if o := mCheck.Components["skill"]["fresh"].Origin; !strings.Contains(o, "(uncommitted)") {
		t.Errorf("skill inside untracked directory must be marked: %q", o)
	}
	m, err := config.LoadSourceManifest(filepath.Join(base, "pub1", "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if o := m.Components["rule"]["will-drift"].Origin; !strings.Contains(o, "(uncommitted)") {
		t.Errorf("modified file must be marked: %q", o)
	}
	if o := m.Components["rule"]["brand-new"].Origin; !strings.Contains(o, "(uncommitted)") {
		t.Errorf("untracked file must be marked: %q", o)
	}
	if o := m.Components["rule"]["committed"].Origin; strings.Contains(o, "uncommitted") {
		t.Errorf("clean file must NOT be marked: %q", o)
	}

	// strict gate: refuses, naming offenders and the fix
	_, err = Harvest(Options{Paths: []string{donor}, Name: "pub2", RequireCommitted: true})
	if err == nil {
		t.Fatal("want refusal on dirty donor")
	}
	for _, want := range []string{"will-drift.md", "brand-new.md", "reproducible"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal missing %q:\n%v", want, err)
		}
	}

	// the human commits the donor state; now HEAD is a truthful claim
	gitProbe(t, donor, "add", ".")
	gitProbe(t, donor, "commit", "-qm", "extract rules")
	res3, err := Harvest(Options{Paths: []string{donor}, Name: "pub3"})
	if err != nil {
		t.Fatal(err)
	}
	if res3.Uncommitted != 0 {
		t.Errorf("after committing donor, Uncommitted = %d, want 0", res3.Uncommitted)
	}
	m3, err := config.LoadSourceManifest(filepath.Join(base, "pub3", "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if o := m3.Components["rule"]["brand-new"].Origin; strings.Contains(o, "uncommitted") {
		t.Errorf("committed file still marked: %q", o)
	}
	// and strict mode now publishes happily
	if _, err := Harvest(Options{Paths: []string{donor}, Name: "pub4", RequireCommitted: true}); err != nil {
		t.Errorf("clean donor must pass --require-committed: %v", err)
	}
}
