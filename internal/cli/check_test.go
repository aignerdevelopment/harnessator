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

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := RunIO(strings.NewReader(stdin), &out, &errb, args)
	return code, out.String(), errb.String()
}

func TestCheckHookLifecycle(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	os.MkdirAll(proj, 0o755)
	repo := exec.Command("git", "init", "-q", proj)
	if b, err := repo.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, b)
	}
	cfg := filepath.Join(proj, "harnessator.toml")
	os.WriteFile(cfg, []byte("kind = \"project\"\nschema = 1\n"), 0o644)
	hooks := filepath.Join(proj, ".git", "hooks", "pre-commit")

	// install
	if code, _, e := runCLI(t, "", "check", "--config", cfg, "--install-hook", "pre-commit"); code != 0 {
		t.Fatalf("install failed: %s", e)
	}
	raw, err := os.ReadFile(hooks)
	if err != nil || !strings.Contains(string(raw), hookMarker) || !strings.Contains(string(raw), "harnessator check --quiet") {
		t.Fatalf("hook not written right: %q %v", raw, err)
	}
	if fi, _ := os.Stat(hooks); fi.Mode()&0o100 == 0 {
		t.Error("hook must be executable")
	}
	// reinstall updates (has marker) - stays exit 0
	if code, _, e := runCLI(t, "", "check", "--config", cfg, "--install-hook", "pre-commit"); code != 0 {
		t.Fatalf("reinstall: %s", e)
	}
	// foreign hook: refuse to clobber
	os.WriteFile(hooks, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	if code, _, _ := runCLI(t, "", "check", "--config", cfg, "--install-hook", "pre-commit"); code == 0 {
		t.Fatal("must refuse foreign hook")
	}
	if code, _, _ := runCLI(t, "", "check", "--config", cfg, "--remove-hook", "pre-commit"); code == 0 {
		t.Fatal("must refuse removing foreign hook")
	}
	// managed: install again then remove (clear the foreign file first)
	os.Remove(hooks)
	runCLI(t, "", "check", "--config", cfg, "--install-hook", "pre-commit")
	if code, out, e := runCLI(t, "", "check", "--config", cfg, "--remove-hook", "pre-commit"); code != 0 {
		t.Fatalf("remove: %s %s", out, e)
	}
	if _, err := os.Stat(hooks); !os.IsNotExist(err) {
		t.Error("hook still present after remove")
	}
	// bad hook name rejected
	if code, _, _ := runCLI(t, "", "check", "--config", cfg, "--install-hook", "commit-msg"); code == 0 {
		t.Fatal("only pre-commit|pre-push allowed")
	}
}

func TestCheckEndToEndAndDrift(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HARNESSATOR_HOME", filepath.Join(root, "store"))
	proj := filepath.Join(root, "proj")
	os.MkdirAll(proj, 0o755)
	url := mkSourceRepo(t)

	if code, _, e := runCLI(t, "", "init", "--targets", "claude", "--source", url, proj); code != 0 {
		t.Fatalf("init: %s", e)
	}
	cfg := filepath.Join(proj, "harnessator.toml")
	if code, _, e := runCLI(t, "", "setup", "--config", cfg); code != 0 {
		t.Fatalf("setup: %s", e)
	}
	// clean project: check --json says ok
	code, out, e := runCLI(t, "", "check", "--config", cfg, "--json")
	if code != 0 || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("check should pass: code=%d out=%s err=%s", code, out, e)
	}
	// tamper a pinned checkout file -> digest drift -> check fails loudly
	var victim string
	filepath.Walk(filepath.Join(root, "store", "sources"), func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && victim == "" && strings.Contains(p, "checkouts") &&
			strings.Contains(p, "/rules/") && strings.HasSuffix(p, ".md") {
			victim = p
		}
		return nil
	})
	if victim == "" {
		t.Fatal("no checkout file found to tamper")
	}
	os.Chmod(victim, 0o644)
	os.WriteFile(victim, []byte("tampered\n"), 0o644)
	if code, out, _ := runCLI(t, "", "check", "--config", cfg, "--json"); code == 0 || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("tampered store must fail check: code=%d out=%s", code, out)
	}
	// non-json quiet mode: silent on success after we cannot restore bytes;
	// just prove the flag path runs (exit 1 expected here due to drift)
	if code, out, _ := runCLI(t, "", "check", "--config", cfg, "--quiet"); code == 0 || out != "" {
		t.Errorf("--quiet must be silent on failing path: code=%d out=%q", code, out)
	}
}
