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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// profiles answers "what would I actually apply?" and enable-without-
// profile announces what it chose - the two halves of informed consent
// for template activation.
func TestProfilesAndEnableDefaulting(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HARNESSATOR_HOME", filepath.Join(root, "store"))
	proj := filepath.Join(root, "proj")
	os.MkdirAll(proj, 0o755)
	url := mkSourceRepo(t) // agentictools fixture: minimal|full|max, no default

	if code, _, e := runCLI(t, "", "init", "--targets", "claude", "--source", url, proj); code != 0 {
		t.Fatalf("init: %s", e)
	}
	cfg := filepath.Join(proj, "harnessator.toml")

	// bare enable with several profiles and no "default": refuses,
	// lists, points at the inspect command - never guesses
	code, _, out := runCLI(t, "", "enable", "agentictools-src", "--config", cfg)
	if code == 0 || !strings.Contains(out, "none is named") ||
		!strings.Contains(out, "available: full") || !strings.Contains(out, "profiles agentictools-src") {
		t.Fatalf("bare enable must refuse with the list, code %d:\n%s", code, out)
	}

	// summary view
	code, out, _ = runCLI(t, "", "profiles", "--config", cfg)
	if code != 0 || !strings.Contains(out, "minimal") ||
		!strings.Contains(out, "full") || !strings.Contains(out, "max") {
		t.Errorf("profiles summary must list all three, code %d:\n%s", code, out)
	}

	// expanded membership with real ids
	code, out, _ = runCLI(t, "", "profiles", "agentictools-src:minimal", "--config", cfg)
	if code != 0 || !strings.Contains(out, "00-agentictools-source-of-truth") ||
		!strings.Contains(out, "10-code-style") {
		t.Errorf("minimal expanded view:\n%s", out)
	}
	if !strings.Contains(out, "(always") {
		t.Errorf("always-rules must be labeled:\n%s", out)
	}

	// [on] marker appears once the template is enabled
	if code, _, e := runCLI(t, "", "enable", "agentictools-src:full", "--config", cfg); code != 0 {
		t.Fatalf("explicit enable: %s", e)
	}
	code, out, _ = runCLI(t, "", "profiles", "--config", cfg)
	if code != 0 || !strings.Contains(out, "full") || !strings.Contains(out, "[on]") {
		t.Errorf("enabled profile must show [on]:\n%s", out)
	}

	// a second source whose only profile is "default": bare enable
	// picks it and says so
	donor := filepath.Join(root, "solo")
	os.MkdirAll(filepath.Join(donor, "rules"), 0o755)
	os.WriteFile(filepath.Join(donor, "rules", "r.md"), []byte("one law\n"), 0o644)
	os.WriteFile(filepath.Join(donor, "harnessator.toml"), []byte(`kind = "source"
schema = 1

[package]
name = "solo"

[component.rule."r"]
path = "rules/r.md"
description = "The only rule."

[profiles]
[profiles.default]
rule = ["r"]
`), 0o644)
	for _, args := range [][]string{{"init", "-b", "main"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "seed"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = donor
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	proj2 := filepath.Join(root, "proj2")
	os.MkdirAll(proj2, 0o755)
	if code, _, e := runCLI(t, "", "init", "--targets", "claude", "--source", "file://"+donor, proj2); code != 0 {
		t.Fatalf("init2: %s", e)
	}
	cfg2 := filepath.Join(proj2, "harnessator.toml")
	code, out, _ = runCLI(t, "", "enable", "solo", "--config", cfg2)
	if code != 0 || !strings.Contains(out, "exactly one") || !strings.Contains(out, "solo:default") {
		t.Fatalf("sole-profile defaulting must be announced, code %d:\n%s", code, out)
	}

	// URL discovery WITHOUT declaring and WITHOUT a project: the store
	// gets the manifest clone, the human gets the truth, nothing else
	// is written.
	bare := filepath.Join(root, "bare")
	os.MkdirAll(bare, 0o755)
	t.Chdir(bare)
	code, out, errStr := runCLI(t, "", "profiles", url)
	if code != 0 || !strings.Contains(out, "minimal") || !strings.Contains(out, "nothing declared yet") {
		t.Fatalf("URL discovery mode, code %d:\n%s\n%s", code, out, errStr)
	}
	if _, err := os.Stat(filepath.Join(bare, "harnessator.toml")); err == nil {
		t.Error("URL discovery must not create project config")
	}
}
