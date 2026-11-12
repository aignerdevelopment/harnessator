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
