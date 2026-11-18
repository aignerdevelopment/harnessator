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
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// The sleipniterm case: content present, components zero, silence total.
// near-miss notes must explain both the orphan pile and the lookalike dir.
func TestScanNearMissExplainsSilence(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	write(t, filepath.Join(base, "proj", "AGENTS.md"), "# agents\n")
	write(t, filepath.Join(base, "proj", ".agents", "solo", "SKILL.md"),
		"---\nname: solo\ndescription: S.\n---\nx\n")
	write(t, filepath.Join(base, "proj", ".codex-tmp", "prompts", "p.md"), "x\n")
	write(t, filepath.Join(base, "proj", ".claude", "settings.json"), "{}")

	rep, err := Scan([]string{filepath.Join(base, "proj")})
	if err != nil {
		t.Fatal(err)
	}
	root := rep.Roots[0]
	joined := strings.Join(root.NearMiss, "\n")
	if !strings.Contains(joined, ".agents/") || !strings.Contains(joined, ".agents/skills/") {
		t.Errorf("orphan pile not explained: %q", joined)
	}
	if !strings.Contains(joined, ".codex-tmp/") {
		t.Errorf("lookalike dir not explained: %q", joined)
	}
	// a named agent dir with NO recognizable content still stays quiet
	// (settings.json is real Claude Code config, not a component)
	if strings.Contains(joined, "settings.json") {
		t.Errorf("must not nag about legit claude config: %q", joined)
	}
	// ordinary root files (README etc.) stay invisible; agent NOTE files
	// are deliberate import candidates - only the generic "loose file"
	// noise from the root-level pile patterns must be gone
	for _, r := range root.Residual {
		if strings.Contains(r.Why, "loose file inside") {
			t.Errorf("root pile noise returned: %+v", r)
		}
	}
}

// canonical layout must produce NO near-miss noise
func TestScanNearMissQuietWhenCorrect(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, ".agents", "skills", "alpha", "SKILL.md"),
		"---\nname: alpha\ndescription: A.\n---\nx\n")
	rep, err := Scan([]string{base})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Roots[0].NearMiss) != 0 {
		t.Errorf("canonical .agents/sights layout flagged: %v", rep.Roots[0].NearMiss)
	}
	if len(rep.Roots[0].Components) != 1 {
		t.Errorf("components = %v", rep.Roots[0].Components)
	}
}

func TestSplitNewlineList(t *testing.T) {
	if got := splitNewlineList([]string{"a\nb\n", "c"}); len(got) != 3 || got[1] != "b" {
		t.Errorf("got %q", got)
	}
	if got := splitNewlineList([]string{"plain", "x"}); len(got) != 2 || got[0] != "plain" {
		t.Errorf("untouched case broke: %q", got)
	}
	if got := splitNewlineList([]string{"with space"}); len(got) != 1 {
		t.Errorf("spaces must not split: %q", got)
	}
}

// The sleipniterm follow-up: content inside a RECOGNIZED directory that
// matches no shape must show in the text report too - residuals are the
// last mile between "0 components" and "here is what to change".
func TestTextReportShowsResiduals(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, ".agents", "skills", "halfbaked", "notes.md"), "loose\n")
	rep, err := Scan([]string{base})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Roots[0].Residual) == 0 {
		t.Fatalf("expected residual for .agents/skills/halfbaked without SKILL.md")
	}
	var buf bytes.Buffer
	rep.WriteText(&buf)
	got := buf.String()
	if !strings.Contains(got, "residual:") || !strings.Contains(got, "no SKILL.md inside") {
		t.Errorf("text report hides residuals:\n%s", got)
	}
	if !strings.Contains(got, "1 residual") {
		t.Errorf("header missing residual count:\n%s", got)
	}
}

// The sleipniterm lesson: a hand-written AGENTS.md in an UNMANAGED tree
// is the terraform-import candidate par excellence - it must reach the
// residual channel (and through it the guided tail), never be silently
// filtered as "ordinary root file".
func TestRootNoteFilesAreImportCandidates(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "AGENTS.md"), "# repo workflow\nbe nice\n")
	write(t, filepath.Join(base, "CLAUDE.md"), "# notes\n")
	write(t, filepath.Join(base, "README.md"), "# normal file\n")

	rep, err := Scan([]string{base})
	if err != nil {
		t.Fatal(err)
	}
	var notes []string
	for _, r := range rep.Roots[0].Residual {
		if strings.Contains(r.Why, "agent note") {
			notes = append(notes, r.Path)
		}
		if strings.Contains(r.Path, "README") {
			t.Error("README.md must stay invisible")
		}
	}
	if len(notes) != 2 {
		t.Fatalf("want AGENTS.md+CLAUDE.md flagged, got %v", notes)
	}

	// a MANAGED tree's AGENTS.md is harnessator's own pointer - no flag
	write(t, filepath.Join(base, "harnessator.lock"), "# generated\n")
	rep2, err := Scan([]string{base})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep2.Roots[0].Residual {
		if strings.Contains(r.Why, "agent note") {
			t.Errorf("managed pointer flagged as import candidate: %s", r.Path)
		}
	}
}
