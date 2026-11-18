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

// The pointer source, end to end: foreign repository content, MY manifest,
// discovery against their tree, the lock pinning BOTH commits, the badges
// telling the truth, dev refusing what you cannot own, and update following
// upstream movement like any direct source would.
func TestPointerSourceFullLoop(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HARNESSATOR_HOME", filepath.Join(root, "store"))

	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// --- the foreign repository: content WITHOUT any harnessator.toml -
	// nobody asked them to be manageable; here they are anyway.
	content := filepath.Join(root, "foreign", "awesome-skills")
	os.MkdirAll(filepath.Join(content, "skills", "before-you-ship"), 0o755)
	os.MkdirAll(filepath.Join(content, "skills", "write-it-twice"), 0o755)
	os.WriteFile(filepath.Join(content, "skills", "before-you-ship", "SKILL.md"),
		[]byte("---\ndescription: Ship in small, reviewable steps.\n---\nbody\n"), 0o644)
	os.WriteFile(filepath.Join(content, "skills", "write-it-twice", "SKILL.md"),
		[]byte("---\ndescription: Prove it twice or not at all.\n---\nbody\n"), 0o644)
	git(content, "init", "-b", "main")
	git(content, "add", ".")
	git(content, "commit", "-qm", "seed")

	// --- MY repository: the curation half, six lines of intent.
	manifest := filepath.Join(root, "my-curators", "awesome-pointer")
	os.MkdirAll(manifest, 0o755)
	toml := `kind = "source"
schema = 3

[package]
name = "awesome-pointer"
description = "curated pointer to awesome-skills, nothing copied"

[content]
git = "file://` + content + `"
ref = "main"

[discover]
skill = "skills/*"

[profiles]
[profiles.default]
skill = ["*"]
`
	os.WriteFile(filepath.Join(manifest, "harnessator.toml"), []byte(toml), 0o644)
	git(manifest, "init", "-b", "main")
	git(manifest, "add", ".")
	git(manifest, "commit", "-qm", "pointer seed")

	// --- consume it like any source.
	proj := filepath.Join(root, "proj")
	os.MkdirAll(proj, 0o755)
	if code, _, e := runCLI(t, "", "init", "--targets", "claude",
		"--source", "file://"+manifest, "--ref", "main", proj); code != 0 {
		t.Fatalf("init: %s", e)
	}
	cfg := filepath.Join(proj, "harnessator.toml")
	if code, out, e := runCLI(t, "", "enable", "awesome-pointer:default", "--config", cfg); code != 0 {
		t.Fatalf("enable: %s %s", e, out)
	}
	if code, out, e := runCLI(t, "", "setup", "--config", cfg); code != 0 {
		t.Fatalf("setup: %s %s", e, out)
	}

	// discovered skills materialized, import-only where the truth belongs.
	code, out, _ := runCLI(t, "", "list", "--config", cfg)
	if code != 0 || !strings.Contains(out, "before-you-ship") || !strings.Contains(out, "write-it-twice") {
		t.Fatalf("list: %d\n%s", code, out)
	}
	if !strings.Contains(out, "(import-only)") {
		t.Errorf("list must badge pointer content:\n%s", out)
	}

	// the lock pins BOTH commits - one honest diff per update.
	raw, err := os.ReadFile(filepath.Join(proj, "harnessator.lock"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"content_git", "content_sha", "content_resolved"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("lock missing %s:\n%s", want, raw)
		}
	}

	code, out, _ = runCLI(t, "", "source", "list", "--config", cfg)
	if code != 0 || !strings.Contains(out, "| content main") {
		t.Errorf("source list content pin:\n%s", out)
	}
	code, out, _ = runCLI(t, "", "info", "awesome-pointer:skill/before-you-ship", "--config", cfg)
	if code != 0 || !strings.Contains(out, "[import-only]") {
		t.Errorf("info provenance badge:\n%s", out)
	}

	// dev on content you do not own: refusal with the fork ramp.
	code, _, out = runCLI(t, "", "dev", "awesome-pointer", "--config", cfg)
	if code == 0 || !strings.Contains(out, "POINTER source") || !strings.Contains(out, "harvest its directory") {
		t.Errorf("want dev refusal + fork ramp, code %d:\n%s", code, out)
	}

	// offline gate green with both trees in store.
	if code, out, e := runCLI(t, "", "check", "--json", "--config", cfg); code != 0 {
		t.Fatalf("check: %s %s", e, out)
	}

	// upstream moves -> update follows, discovery re-expands, lock re-pins.
	os.MkdirAll(filepath.Join(content, "skills", "third-skill"), 0o755)
	os.WriteFile(filepath.Join(content, "skills", "third-skill", "SKILL.md"),
		[]byte("---\ndescription: The newcomer.\n---\nbody\n"), 0o644)
	git(content, "add", ".")
	git(content, "commit", "-qm", "growth")
	if code, out, e := runCLI(t, "", "update", "--config", cfg); code != 0 {
		t.Fatalf("update: %s %s", e, out)
	}
	code, out, _ = runCLI(t, "", "list", "--config", cfg)
	if !strings.Contains(out, "third-skill") {
		t.Errorf("update did not follow upstream growth:\n%s", out)
	}
	if strings.Count(out, "(import-only)") != 3 {
		t.Errorf("every pointer component must carry the badge:\n%s", out)
	}

	// A second consumer proves the offline path (lock pins + store trees,
	// no network needed).
	proj2 := filepath.Join(root, "proj2")
	os.MkdirAll(proj2, 0o755)
	if code, _, e := runCLI(t, "", "init", "--targets", "claude", "--source", "file://"+manifest, "--ref", "main", proj2); code != 0 {
		t.Fatalf("init2: %s", e)
	}
	cfg2 := filepath.Join(proj2, "harnessator.toml")
	runCLI(t, "", "enable", "awesome-pointer:default", "--config", cfg2)
	if code, out, e := runCLI(t, "", "setup", "--config", cfg2); code != 0 {
		t.Fatalf("setup2: %s %s", e, out)
	}
	if code, out, _ := runCLI(t, "", "check", "--config", cfg2); code != 0 {
		t.Errorf("offline check2:\n%s", out)
	}

	// A DECLARED import-only direct source (editable = false): a repo whose
	// toml you may technically write, but whose maintenance you do not
	// own - the same refusal, consumer-declared instead of derived.
	proj3 := filepath.Join(root, "proj3")
	os.MkdirAll(proj3, 0o755)
	if code, _, e := runCLI(t, "", "init", "--targets", "claude", "--source", mkSourceRepo(t), "--ref", "v0.1.0", proj3); code != 0 {
		t.Fatalf("init3: %s", e)
	}
	cfg3 := filepath.Join(proj3, "harnessator.toml")
	raw3, err := os.ReadFile(cfg3)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(cfg3, []byte(strings.Replace(string(raw3), "name = \"agentictools-src\"",
		"name = \"agentictools-src\"\neditable = false", 1)), 0o644)
	if code, out, e := runCLI(t, "", "enable", "agentictools-src:full", "--config", cfg3); code != 0 {
		t.Fatalf("enable3: %s %s", e, out)
	}
	if code, out, e := runCLI(t, "", "setup", "--config", cfg3); code != 0 {
		t.Fatalf("setup3: %s %s", e, out)
	}
	code, _, out = runCLI(t, "", "dev", "agentictools-src", "--config", cfg3)
	if code == 0 || !strings.Contains(out, "declared import-only") {
		t.Errorf("want declared import-only refusal, code %d:\n%s", code, out)
	}
	if code, out, _ := runCLI(t, "", "list", "--config", cfg3); code != 0 || !strings.Contains(out, "(import-only)") {
		t.Errorf("want badge on declared source:\n%s", out)
	}

	// Simulate a lost content checkout: the offline gate must name the
	// SECOND pin as the missing piece, not a vague digest failure.
	var csha string
	for _, ln := range strings.Split(string(raw), "\n") {
		if s, ok := strings.CutPrefix(strings.TrimSpace(ln), "content_sha = "); ok {
			csha = strings.Trim(s, "\"")
			break
		}
	}
	if csha == "" {
		t.Fatal("lock content_sha not found for sabotage")
	}
	os.RemoveAll(filepath.Join(root, "store", "sources", "awesome-pointer", "content"))
	code, out, errStr := runCLI(t, "", "check", "--config", cfg)
	if code == 0 || !strings.Contains(out+errStr, "CONTENT checkout") {
		t.Errorf("want missing-CONTENT refusal, code %d:\n%s\n%s", code, out, errStr)
	}
}
