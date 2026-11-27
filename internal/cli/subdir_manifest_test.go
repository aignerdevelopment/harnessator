// Copyright 2026 Roland Aigner (https://www.aignerdevelopment.com)
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Regression (walked into by harnessator-site's self-governance): a
// source whose manifest lives in a SUBDIRECTORY (--manifest source/x)
// validated component paths against the checkout root instead of the
// manifest dir - so a perfectly valid source refused to resolve,
// while `harnessator validate` (which uses the manifest dir) said ok.
// The full flow must pass: add -> enable -> setup, components read
// from where their bytes actually are.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSubdirectoryManifestSource(t *testing.T) {
	store := t.TempDir()
	t.Setenv("HARNESSATOR_HOME", store)

	// the source repo: manifest + rule live in source/site/
	repo := filepath.Join(t.TempDir(), "site-src")
	mustWrite(t, filepath.Join(repo, "source", "site", "harnessator.toml"),
		`kind = "source"
schema = 1

[package]
name = "site-src"
description = "subdirectory-manifest fixture"
license = "Apache-2.0"

[component.rule."publish"]
path = "rules/publish.md"
description = "publish in the same change-set"
tags = ["publish"]
`)
	mustWrite(t, filepath.Join(repo, "source", "site", "rules", "publish.md"),
		"---\nid: publish\ntags: [publish]\nalways: false\n---\n\nPublish when touched.\n")
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "seed"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	proj := t.TempDir()
	t.Chdir(proj)
	if code, _, out := runCLI(t, "", "init", "--targets", "claude", "."); code != 0 {
		t.Fatalf("init: %d %s", code, out)
	}
	if code, _, out := runCLI(t, "", "source", "add", "file://"+repo, "--manifest", "source/site"); code != 0 {
		t.Fatalf("source add: %d %s", code, out)
	}
	if code, _, out := runCLI(t, "", "enable", "site-src:rule/publish"); code != 0 {
		t.Fatalf("enable: %d %s", code, out)
	}
	code, out, errb := runCLI(t, "", "setup")
	if code != 0 {
		t.Fatalf("setup must resolve a subdir-manifest source: %d\n%s\n%s", code, out, errb)
	}
	act, err := os.ReadFile(filepath.Join(proj, ".harnessator", "active.md"))
	if err != nil || !strings.Contains(string(act), "rule/publish") {
		t.Fatalf("active index must carry the rule: %v %s", err, act)
	}
	// active.md must point at a real, loadable file INSIDE the
	// manifest subdirectory of the pinned checkout - this is what the
	// old pin.Dir validation could never reach
	am, err := os.ReadFile(filepath.Join(proj, "AGENTS.md"))
	if err != nil {
		t.Fatal(err) // pointer file exists; the rules ride via active.md
	}
	_ = am
	acts := string(act)
	idx := strings.Index(acts, "publish.md")
	if idx < 0 {
		t.Fatal("no publish.md path in active index")
	}
	lineStart := strings.LastIndex(acts[:idx], "\n")
	nl := strings.Index(acts[idx:], "\n")
	if nl < 0 {
		nl = len(acts) - idx
	}
	line := strings.TrimSpace(acts[lineStart+1 : idx+nl])
	line = strings.TrimPrefix(line, "- ")
	line = strings.Trim(line, "`")
	body, err := os.ReadFile(line)
	if err != nil || !strings.Contains(string(body), "Publish when touched") {
		t.Errorf("the active rule path must load from the manifest subdir (%s): %v", line, err)
	}
	// and the gates stay green offline against the lock
	if code, out, _ := runCLI(t, "", "check"); code != 0 {
		t.Errorf("check: %d %s", code, out)
	}
	if code, out, _ := runCLI(t, "", "verify"); code != 0 {
		t.Errorf("verify: %d %s", code, out)
	}
}
