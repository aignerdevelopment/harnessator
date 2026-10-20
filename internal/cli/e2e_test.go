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

// mkSourceRepo builds a git repo from testdata/sources/agentictools tagged
// v0.1.0, returned as a file:// URL usable as a source.
func mkSourceRepo(t *testing.T) string {
	t.Helper()
	src := mustAbs(t, filepath.Join("..", "..", "testdata", "sources", "agentictools"))
	repo := filepath.Join(t.TempDir(), "agentictools-src")
	copyTree(t, src, repo)
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "seed"},
		{"tag", "v0.1.0"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return "file://" + repo
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestEndToEndInitSetupList(t *testing.T) {
	storeDir := t.TempDir()
	t.Setenv("HARNESSATOR_HOME", storeDir)
	proj := t.TempDir()
	url := mkSourceRepo(t)

	var out, errOut bytes.Buffer
	if code := Run(&out, &errOut, []string{
		"init", "--targets", "opencode,claude", "--source", url, "--ref", "~v0.1",
		filepath.Join(proj, "nested-creates-me"),
	}); code != 0 {
		t.Fatalf("init: %d\n%s%s", code, out.String(), errOut.String())
	}
	proj = filepath.Join(proj, "nested-creates-me")
	if _, err := os.Stat(filepath.Join(proj, "harnessator.toml")); err != nil {
		t.Fatal("no config written")
	}
	// init created a source but no selections: enable the source's profile
	out.Reset()
	errOut.Reset()
	cfg := filepath.Join(proj, "harnessator.toml")
	if code := Run(&out, &errOut, []string{"enable", "--config", cfg, "agentictools-src:minimal"}); code != 0 {
		t.Fatalf("enable template: %d\n%s%s", code, out.String(), errOut.String())
	}
	// lock + store do not exist yet; setup creates them
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"setup", "--config", cfg}); code != 0 {
		t.Fatalf("setup: %d\n%s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "enable rule") {
		t.Errorf("setup output missing plan lines:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(proj, "harnessator.lock")); err != nil {
		t.Error("lock not written")
	}
	// opencode.json must exist after setup
	if _, err := os.Stat(filepath.Join(proj, ".opencode", "opencode.json")); err != nil {
		t.Error("opencode.json not written")
	}

	// list + verify + doctor on the fresh project
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"list", "--config", cfg}); code != 0 || !strings.Contains(out.String(), "rule/00-agentictools") {
		t.Fatalf("list: %d\n%s%s", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"verify", "--config", cfg}); code != 0 {
		t.Fatalf("verify: %d\n%s%s", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"doctor", "--config", cfg}); code != 0 {
		t.Fatalf("doctor: %d\n%s%s\n%s%s", code, out.String(), errOut.String(), out.String(), errOut.String())
	}

	// enable a skill by bare cat/id then re-setup: skill link appears
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"enable", "--config", cfg, "skill/example-skill"}); code != 0 {
		t.Fatalf("enable: %d\n%s%s", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"setup", "--config", cfg}); code != 0 {
		t.Fatalf("setup2: %d\n%s%s", code, out.String(), errOut.String())
	}
	link := filepath.Join(proj, ".opencode", "skill", "example-skill")
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("skill link missing after enable+setup: %v", err)
	}

	// disable again
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"disable", "--config", cfg, "skill/example-skill"}); code != 0 {
		t.Fatalf("disable: %d\n%s%s", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"setup", "--config", cfg}); code != 0 {
		t.Fatalf("setup3: %d\n%s%s", code, out.String(), errOut.String())
	}
	if _, err := os.Lstat(link); err == nil {
		t.Error("skill link survived disable+setup")
	}

	// why + search + info
	for _, cmd := range [][]string{
		{"why", "--config", cfg, "rule/00-agentictools-source-of-truth"},
		{"search", "--config", cfg, "example"},
		{"info", "--config", cfg, "rule/10-code-style"},
	} {
		out.Reset()
		errOut.Reset()
		if code := Run(&out, &errOut, cmd); code != 0 {
			t.Errorf("%v: exit %d out=%q err=%q", cmd, code, out.String(), errOut.String())
		}
	}
}
