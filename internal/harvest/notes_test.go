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

	"harnessator.org/harnessator/internal/config"
)

const sleipNote = "# Repository workflow\r\n\r\n" +
	"## Development status documentation discipline\r\n\r\n" +
	"The standing **decision log** is the project memory.\r\n\r\n" +
	"## Permission failures\r\n\r\n- retry elevated\r\n\r\n" +
	"## Git commits and pushes\r\n\r\nUse `tools/git-commit-push.ps1`.\r\n\r\n" +
	"```\r\n## not a heading (inside a code fence)\r\n```\r\n"

func TestSplitNoteHeadingsCRLFAndFences(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "AGENTS.md"), sleipNote)
	rules, err := SplitNoteFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 3 {
		t.Fatalf("want 3 sections (fence ignored), got %d: %+v", len(rules), idsOf(rules))
	}
	if rules[0].ID != "development-status-documentation-discipline" {
		t.Errorf("id[0] = %q", rules[0].ID)
	}
	if !strings.HasPrefix(rules[0].Body, "# Repository workflow") {
		t.Error("preamble (title) lost from first rule")
	}
	if rules[1].Description != "retry elevated" {
		t.Errorf("desc[1] = %q", rules[1].Description)
	}
	for _, r := range rules {
		if !schemaValid(r.ID) {
			t.Errorf("invalid slug %q", r.ID)
		}
	}
}

func TestSplitNoteNoStructureSingleRule(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "CLAUDE.md"), "- always run tests\r\n- be kind\r\n")
	dot := filepath.Join(dir, ".cursorrules")
	write(t, dot, "always use tabs\n")
	drs, err := SplitNoteFile(dot)
	if err != nil || len(drs) != 1 || drs[0].ID != "cursorrules-md" {
		t.Errorf("dotfile stem must survive: %+v %v", drs, err)
	}
	rules, err := SplitNoteFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != "claude-md" || !strings.Contains(rules[0].Description, "no section structure") {
		t.Fatalf("single-rule fallback wrong: %+v", rules)
	}
	if rules[0].Body != "- always run tests\n- be kind\n" {
		t.Errorf("verbatim body expected, got %q", rules[0].Body)
	}
}

func TestSplitNoteCollisionAndLongHeadings(t *testing.T) {
	dir := t.TempDir()
	content := "## Same Title\n\na\n\n## Same Title\n\nb\n\n## " +
		strings.Repeat("extremely long heading ", 8) + "\n\nc\n"
	write(t, filepath.Join(dir, "AGENTS.md"), content)
	rules, err := SplitNoteFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 3 || rules[1].ID != "same-title-2" {
		t.Fatalf("collision bump failed: %+v", idsOf(rules))
	}
	if !schemaValid(rules[2].ID) || len(rules[2].ID) > 64 {
		t.Errorf("long heading slug invalid: %q", rules[2].ID)
	}
}

func TestHarvestWithNotesEndToEnd(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	write(t, filepath.Join(base, "sleip", "AGENTS.md"), sleipNote)

	// dry run first: plan shows imports, no writes
	res, err := Harvest(Options{Paths: []string{"sleip"}, Name: "tools", WithNotes: true, DryRun: true})
	if err != nil || len(res.Planned) != 3 {
		t.Fatalf("plan: %v %+v", err, res.Planned)
	}
	if _, err := os.Stat(filepath.Join(base, "tools")); err == nil {
		t.Error("dry-run created the source dir")
	}

	res, err = Harvest(Options{Paths: []string{"sleip"}, Name: "tools", WithNotes: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 3 {
		t.Fatalf("added = %d", res.Added)
	}
	m, err := config.LoadSourceManifest(filepath.Join(base, "tools", "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := config.ValidateSource(m, filepath.Join(base, "tools")); err != nil {
		t.Fatalf("published source invalid: %v", err)
	}
	imp := m.Components["rule"]["permission-failures"]
	if !strings.HasPrefix(imp.Origin, "imported from AGENTS.md") {
		t.Errorf("origin = %q", imp.Origin)
	}
	if m.Profiles["default"]["rule"] == nil || len(m.Profiles["default"]["rule"]) != 3 {
		t.Errorf("default profile = %v", m.Profiles["default"])
	}

	// idempotent: same content imports skip-identical (via --into)
	res2, err := Harvest(Options{Paths: []string{"sleip"}, Into: "tools", WithNotes: true})
	if err != nil || res2.Added != 0 || res2.Skipped != 3 {
		t.Fatalf("re-harvest not idempotent: %+v err=%v", res2, err)
	}
}

func TestAbortPointsAtRampsNotDeadEnd(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	write(t, filepath.Join(base, "sleip", "AGENTS.md"), "# w\n\n## one\nx\n\n## two\ny\n")
	_, err := Harvest(Options{Paths: []string{"sleip"}, Name: "tools"})
	if err == nil {
		t.Fatal("want ramp abort")
	}
	msg := err.Error()
	for _, want := range []string{"--with-notes", "harnessator kickstart", "HARNESSATOR-KICKSTART.md"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ramp message missing %q:\n%s", want, msg)
		}
	}
}

func idsOf(rs []NoteRule) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

func schemaValid(s string) bool {
	return len(s) > 0 && s[0] >= 'a' && s[0] <= 'z' && !strings.ContainsAny(s, " #")
}
