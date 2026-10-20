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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"harnessator.org/harnessator/internal/config"
)

func TestHarvestNewSource(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	donor := filepath.Join(base, "proj")
	write(t, filepath.Join(donor, ".claude/skills/greet/SKILL.md"),
		"---\nname: greet\ndescription: Greets politely.\n---\nSay hi.\n")
	write(t, filepath.Join(donor, ".claude/commands/review.md"), "# Review\ncheck it\n")
	write(t, filepath.Join(donor, ".cursor/rules/security.mdc"),
		"# managed by harnessator - edit the source rule, not this file\n"+
			"---\ndescription: Security rules\nglobs: [\"**/*.go\"]\nalwaysApply: true\n---\n\nno secrets\n")

	res, err := Harvest(Options{Paths: []string{donor}, Name: "published"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 3 {
		t.Fatalf("added = %d (%+v)", res.Added, res.Planned)
	}
	out := filepath.Join(base, "published")

	// loads + validates as a real source manifest
	m, err := config.LoadSourceManifest(filepath.Join(out, "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := config.ValidateSource(m, out); err != nil {
		t.Fatalf("published source invalid: %v", err)
	}
	if m.Package.Name != "published" {
		t.Errorf("package name = %q", m.Package.Name)
	}
	if ids := m.Profiles["default"]["skill"]; len(ids) != 1 || ids[0] != "greet" {
		t.Errorf("default profile skills = %v", ids)
	}
	if _, err := os.Stat(filepath.Join(out, "skills/greet/SKILL.md")); err != nil {
		t.Error(err)
	}
	greet := m.Components["skill"]["greet"]
	if greet.Description != "Greets politely." || !strings.Contains(greet.Origin, ".claude/skills/greet") {
		t.Errorf("greet meta = %+v", greet)
	}

	// mdc round-trip: body lifted plain, hints captured for the manifest
	sec := m.Components["rule"]["security"]
	body, err := os.ReadFile(filepath.Join(out, "rules/security.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "no secrets") ||
		strings.Contains(string(body), "globs:") ||
		strings.Contains(string(body), "managed by harnessator") {
		t.Errorf("mdc not lifted: %q", body)
	}
	hint := sec.Hint["cursor"]
	if hint == nil || hint["always-apply"] != true {
		t.Errorf("cursor hint = %v", hint)
	}
	gl, _ := hint["globs"].([]any)
	if len(gl) != 1 || gl[0] != "**/*.go" {
		t.Errorf("globs hint after TOML round-trip = %#v", hint["globs"])
	}
	if !strings.Contains(sec.Description, "Security") {
		t.Errorf("rule desc = %q", sec.Description)
	}

	// idempotency: re-run into the source -> all skipped, manifest stable
	before, _ := os.ReadFile(filepath.Join(out, "harnessator.toml"))
	res2, err := Harvest(Options{Paths: []string{donor}, Into: "published"})
	if err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
	if res2.Added != 0 || res2.Skipped != 3 {
		t.Errorf("re-run = %+v", res2.Planned)
	}
	after, _ := os.ReadFile(filepath.Join(out, "harnessator.toml"))
	if string(before) != string(after) {
		t.Error("manifest changed on idempotent re-run")
	}
}

func TestHarvestDivergentNeedsPrefer(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	a := filepath.Join(base, "A")
	b := filepath.Join(base, "B")
	write(t, filepath.Join(a, ".claude/commands/review.md"), "old\n")
	write(t, filepath.Join(b, ".claude/commands/review.md"), "improved\n")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(a, ".claude/commands/review.md"), old, old)

	if _, err := Harvest(Options{Paths: []string{a, b}, Name: "out"}); err == nil ||
		!strings.Contains(err.Error(), "2 variants") {
		t.Fatalf("expected variants conflict, got %v", err)
	}
	if _, err := Harvest(Options{Paths: []string{a, b}, Name: "out", Prefer: "newest"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(base, "out", "prompts/review.md"))
	if string(got) != "improved\n" {
		t.Errorf("newest picked wrong content: %q", got)
	}
}

func TestHarvestSanitizeBlocks(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	d := filepath.Join(base, "proj")
	write(t, filepath.Join(d, ".claude/commands/deploy.md"),
		"# Deploy\napi_key = \"sk-live-supersecretvalue123456\"\n")

	_, err := Harvest(Options{Paths: []string{d}, Name: "out"})
	if err == nil || !strings.Contains(err.Error(), "secret-assignment") {
		t.Fatalf("expected sanitize block, got %v", err)
	}
	if _, statErr := os.Stat("out"); statErr == nil {
		if _, mErr := os.Stat(filepath.Join("out", "harnessator.toml")); mErr == nil {
			t.Error("aborted harvest left a manifest")
		}
	}
	if _, err := Harvest(Options{Paths: []string{d}, Name: "out", AllowHits: true}); err != nil {
		t.Fatalf("--allow-hits should publish after review: %v", err)
	}
}

func TestHarvestIntoConflictAndDryRun(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	donor := filepath.Join(base, "proj")
	write(t, filepath.Join(donor, ".claude/skills/greet/SKILL.md"), "---\nname: greet\ndescription: G.\n---\nhi\n")
	if _, err := Harvest(Options{Paths: []string{donor}, Name: "src"}); err != nil {
		t.Fatal(err)
	}
	// mutate the published skill -> same donor id now DIFFERS
	write(t, filepath.Join(base, "src", "skills/greet/SKILL.md"), "---\nname: greet\ndescription: G.\n---\nhall-o!\n")
	if _, err := Harvest(Options{Paths: []string{donor}, Into: "src"}); err == nil ||
		!strings.Contains(err.Error(), "DIFFERENT content") {
		t.Fatalf("expected digest conflict into source, got %v", err)
	}

	// dry-run writes nothing
	res, err := Harvest(Options{Paths: []string{donor}, Name: "dry", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Planned) != 1 {
		t.Errorf("dry plan = %+v", res.Planned)
	}
	if _, statErr := os.Stat("dry"); statErr == nil {
		t.Error("dry-run created output")
	}

	// Name on an existing source is refused (use --into)
	if _, err := Harvest(Options{Paths: []string{donor}, Name: "src"}); err == nil ||
		!strings.Contains(err.Error(), "--into") {
		t.Fatalf("expected --into hint, got %v", err)
	}
}
