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

package materialize

import (
	"path/filepath"
	"strings"
	"testing"

	"harnessator.org/harnessator/internal/resolve"
)

// TestRecognizeSymmetry is the write<->read drift guard: every directory an
// adapter MANAGES (resets) must be recognized by some layout claim - a
// component the tool can write but cannot find again would silently defeat
// harvest/scan. Generated trees under .harnessator/ (pi farms, tool
// mirrors) are excluded: they are outputs, never authored content.
func TestRecognizeSymmetry(t *testing.T) {
	recs := Recognitions()
	known := map[string]bool{}
	for _, r := range recs {
		known[r.Dir] = true
	}
	if !known[".agents/skills"] || !known[".claude/skills"] || !known[".cursor/rules"] {
		t.Fatalf("recognition set unexpected: %+v", recs)
	}

	ctx := &Context{Project: t.TempDir(), Plan: &resolve.Plan{}}
	for _, target := range []string{"opencode", "claude", "codex", "antigravity", "cursor", "pi"} {
		ad := Registry[target]()
		sp, ok := ad.(Specer)
		if !ok {
			continue
		}
		for _, dir := range sp.ManagedDirs(ctx) {
			rel, err := filepath.Rel(ctx.Project, dir)
			if err != nil {
				t.Fatal(err)
			}
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, ".harnessator/") {
				continue // generated output trees are not authored content
			}
			if !known[rel] {
				t.Errorf("target %s manages %s but no Recognition reads it back", target, rel)
			}
		}
	}
}

func TestRecognizePerTarget(t *testing.T) {
	// each adapter's file suffixes must match what it writes
	cases := []struct{ target, dir, suffix, cat string }{
		{"claude", ".claude/commands", ".md", "prompt"},
		{"opencode", ".opencode/plugin", ".ts", "hook"},
		{"cursor", ".cursor/rules", ".mdc", "rule"},
		{"codex", ".codex/prompts", ".md", "prompt"},
		{"antigravity", ".agents/workflows", ".md", "workflow"},
	}
	for _, tc := range cases {
		found := false
		for _, r := range recognitionsFor(tc.target) {
			if r.Dir == tc.dir && r.Suffix == tc.suffix && r.Category == tc.cat {
				found = true
			}
		}
		if !found {
			t.Errorf("target %s lacks recognition %s%s -> %s", tc.target, tc.dir, tc.suffix, tc.cat)
		}
	}
}
