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
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)
	if dir != "" {
		full = append([]string{"-C", dir}, full...)
	}
	cmd := exec.Command("git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// mkRemote builds: a source repo (from testdata) pushed into a bare
// "remote.git", returned as a file:// URL.
func mkRemote(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	seed := filepath.Join(base, "seed")
	copyTree(t, mustAbs(t, filepath.Join("..", "..", "testdata", "sources", "agentictools")), seed)
	git(t, seed, "init", "-b", "main")
	git(t, seed, "add", ".")
	git(t, seed, "commit", "-qm", "seed")
	remote := filepath.Join(base, "remote.git")
	git(t, "", "init", "--bare", remote)
	git(t, seed, "push", "-q", remote, "main:main")
	return "file://" + remote
}

func TestDevContributeLoop(t *testing.T) {
	t.Setenv("HARNESSATOR_HOME", t.TempDir())
	proj := t.TempDir()
	var out, errOut bytes.Buffer

	url := mkRemote(t)
	if code := Run(&out, &errOut, []string{"init", "--targets", "opencode,claude", "--source", url, "--ref", "main", proj}); code != 0 {
		t.Fatalf("init: %s%s", out.String(), errOut.String())
	}
	cfg := filepath.Join(proj, "harnessator.toml")
	// source name derived from URL: "remote"
	if code := Run(&out, &errOut, []string{"enable", "--config", cfg, "remote:minimal"}); code != 0 {
		t.Fatalf("enable: %s%s", out.String(), errOut.String())
	}
	out.Reset()
	if code := Run(&out, &errOut, []string{"setup", "--config", cfg}); code != 0 {
		t.Fatalf("setup: %s%s", out.String(), errOut.String())
	}
	lockBefore, _ := os.ReadFile(filepath.Join(proj, "harnessator.lock"))

	// start dev
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"dev", "--config", cfg, "remote"}); code != 0 {
		t.Fatalf("dev: %s%s", out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "dev mode ON") {
		t.Errorf("dev output: %s", out.String())
	}

	// edit in the overlay + commit natively
	devDir := findOverlay(t, out.String())
	target := filepath.Join(devDir, "rules", "10-code-style.md")
	if err := os.WriteFile(target, []byte("# changed by dev mode\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, devDir, "add", "-A")
	git(t, devDir, "commit", "-qm", "improve style rule")

	// setup rewires links to the overlay and refuses to write the lock
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"setup", "--config", cfg}); code != 0 {
		t.Fatalf("setup(dev): %s%s", out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "DEV MODE active") {
		t.Errorf("expected DEV MODE lock guard:\n%s", out.String())
	}
	lockAfter, _ := os.ReadFile(filepath.Join(proj, "harnessator.lock"))
	if !bytes.Equal(lockBefore, lockAfter) {
		t.Error("lock was rewritten while dev active")
	}
	// claude skill/rule source link now resolves into the overlay content:
	// check the linked rule file shows the edit
	lnk := filepath.Join(devDir, "rules", "10-code-style.md")
	b, err := os.ReadFile(lnk)
	if err != nil || !strings.Contains(string(b), "changed by dev mode") {
		t.Errorf("overlay edit missing: %v", err)
	}

	// verify must still pass (dev components skipped)
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"verify", "--config", cfg}); code != 0 {
		t.Fatalf("verify with dev: %s%s", out.String(), errOut.String())
	}

	// contribute: pushes branch to the (local) remote + prints MR guidance
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"contribute", "--config", cfg, "--title", "style update"}); code != 0 {
		t.Fatalf("contribute: %s%s", out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "pushed") {
		t.Errorf("contribute output: %s", out.String())
	}
	remoteBranches := git(t, "", "ls-remote", "--heads", url)
	if !strings.Contains(remoteBranches, "harnessator/dev/") {
		t.Errorf("dev branch not pushed to remote:\n%s", remoteBranches)
	}

	// drop cleans up; setup relinks to pinned checkout (no dev warning)
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"dev", "--config", cfg, "drop", "remote"}); code != 0 {
		t.Fatalf("drop: %s%s", out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"setup", "--config", cfg}); code != 0 {
		t.Fatalf("setup(post-drop): %s%s", out.String(), errOut.String())
	}
	if strings.Contains(out.String(), "DEV MODE") {
		t.Error("dev binding survived drop")
	}
	lockFinal, err := os.ReadFile(filepath.Join(proj, "harnessator.lock"))
	if err != nil || bytes.Equal(lockFinal, lockBefore) && len(lockFinal) == 0 {
		t.Error("lock not rewritten after drop")
	}
}

func findOverlay(t *testing.T, s string) string {
	t.Helper()
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "overlay: "); i >= 0 {
			return strings.TrimSpace(line[i+len("overlay: "):])
		}
	}
	t.Fatalf("no overlay path in output:\n%s", s)
	return ""
}

func TestSourceNew(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "team-tools")
	var out, errOut bytes.Buffer
	if code := Run(&out, &errOut, []string{"source", "new", "--name", "team-tools", "--dir", dir,
		"--description", "team best practices"}); code != 0 {
		t.Fatalf("source new: %s%s", out.String(), errOut.String())
	}
	// scaffold self-validates (the command runs --check-paths internally)
	man := filepath.Join(dir, "harnessator.toml")
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"validate", "--check-paths", man}); code != 0 {
		t.Fatalf("scaffold manifest invalid: %s%s", out.String(), errOut.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Error("scaffold not a git repo")
	}
	git(t, dir, "log", "-1") // initial commit exists
}
