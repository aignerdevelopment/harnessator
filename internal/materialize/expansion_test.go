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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The 2026 expansion trio: gemini (extension-era CLI), copilot (the
// GitHub-native surface) and cline (the rules-folder people). Same
// contract as the MVP six: every category lands where the harness
// actually reads it, stale generated files die by marker, foreign
// files survive, and CheckSpecs certifies the state offline.
func TestGeminiCopilotCline(t *testing.T) {
	c := setupForTargets(t, "gemini", "copilot", "cline")

	// pre-existing foreign neighbours must never be touched
	gemCmd := filepath.Join(c.Project, ".gemini", "commands")
	insDir := filepath.Join(c.Project, ".github", "instructions")
	for _, d := range []string{gemCmd, insDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	foreignCmd := filepath.Join(gemCmd, "handmade.toml")
	os.WriteFile(foreignCmd, []byte("description = \"mine\"\nprompt = \"mine\"\n"), 0o644)
	staleCmd := filepath.Join(gemCmd, "gone.toml")
	os.WriteFile(staleCmd, []byte(geminiMarker+"\ndescription = \"x\"\n"), 0o644)
	foreignIns := filepath.Join(insDir, "human.instructions.md")
	os.WriteFile(foreignIns, []byte("human instructions: prefer tabs\n"), 0o644)
	staleIns := filepath.Join(insDir, "gone.instructions.md")
	os.WriteFile(staleIns, []byte("---\ndescription: x\n"+copilotMarker+"\n---\nold\n"), 0o644)

	if err := ApplyAll(c); err != nil {
		t.Fatal(err)
	}
	if err := WriteShared(c); err != nil {
		t.Fatal(err)
	}

	// --- gemini: context block, commands TOMLs, settings, agents
	gem := readStr(t, filepath.Join(c.Project, "GEMINI.md"))
	if !strings.Contains(gem, "@.harnessator/active.md") ||
		!strings.Contains(gem, "harnessator managed (do not edit)") {
		t.Errorf("GEMINI.md managed block:\n%s", gem)
	}
	if _, err := os.Lstat(foreignCmd); err != nil {
		t.Error("foreign gemini command deleted")
	}
	if _, err := os.Lstat(staleCmd); !os.IsNotExist(err) {
		t.Error("stale harnessator gemini command survived")
	}
	settings := readStr(t, filepath.Join(c.Project, ".gemini", "settings.json"))
	if !strings.Contains(settings, "mcpServers") || !strings.Contains(settings, "example.test") {
		t.Errorf("gemini settings.json:\n%s", settings)
	}
	ag := readStr(t, filepath.Join(c.Project, ".gemini", "agents", "code-reviewer.md"))
	if !strings.Contains(ag, "generic definition") {
		t.Errorf("gemini generic agent fallback:\n%s", ag)
	}

	// --- copilot: generated instructions with applyTo, dual mcp files
	r00 := readStr(t, filepath.Join(insDir, "00-agentictools-source-of-truth.instructions.md"))
	if !strings.Contains(r00, `applyTo: "**/*"`) || !strings.Contains(r00, "description:") {
		t.Errorf("copilot instruction frontmatter:\n%s", r00)
	}
	if _, err := os.Lstat(foreignIns); err != nil {
		t.Error("foreign instruction deleted")
	}
	if _, err := os.Lstat(staleIns); !os.IsNotExist(err) {
		t.Error("stale harnessator instruction survived")
	}
	vs := readStr(t, filepath.Join(c.Project, ".vscode", "mcp.json"))
	if !strings.Contains(vs, `"servers"`) {
		t.Errorf("vscode mcp shape:\n%s", vs)
	}
	mp := readStr(t, filepath.Join(c.Project, ".mcp.json"))
	if !strings.Contains(mp, `"mcpServers"`) {
		t.Errorf("portable mcp shape:\n%s", mp)
	}
	if _, err := os.Lstat(filepath.Join(c.Project, ".github", "agents", "code-reviewer.agent.md")); err != nil {
		t.Errorf("copilot agent file: %v", err)
	}

	// --- cline: rules linked in place, workflows dir, mcp + agents
	cl00 := filepath.Join(c.Project, ".clinerules", "00-agentictools-source-of-truth.md")
	if tgt, err := os.Readlink(cl00); err != nil || !strings.Contains(tgt, "rules") {
		t.Errorf("cline rule link (must be a link, Cline reads all of .clinerules): %v %v", tgt, err)
	}
	cm := readStr(t, filepath.Join(c.Project, ".cline", "mcp.json"))
	if !strings.Contains(cm, "mcpServers") {
		t.Errorf("cline mcp.json:\n%s", cm)
	}
	if _, err := os.Lstat(filepath.Join(c.Project, ".cline", "agents", "code-reviewer.md")); err != nil {
		t.Errorf("cline agent file: %v", err)
	}

	// --- shared AgentSkills location, all three claim it, one state
	for _, id := range []string{"example-skill"} {
		if _, err := os.Lstat(filepath.Join(c.Project, ".agents", "skills", id)); err != nil {
			t.Errorf(".agents/skills/%s: %v", id, err)
		}
	}

	if problems := CheckSpecs(c); len(problems) != 0 {
		t.Errorf("CheckSpecs: %v", problems)
	}
}

// GEMINI.md already exists with human content: the managed block is
// spliced, the human text survives byte for byte outside it.
func TestGeminiExistingContextFile(t *testing.T) {
	c := setupForTargets(t, "gemini")
	os.WriteFile(filepath.Join(c.Project, "GEMINI.md"),
		[]byte("# My project's real context\n\nhand-written, sacred\n"), 0o644)
	if err := ApplyAll(c); err != nil {
		t.Fatal(err)
	}
	got := readStr(t, filepath.Join(c.Project, "GEMINI.md"))
	if !strings.Contains(got, "hand-written, sacred") {
		t.Errorf("human context clobbered:\n%s", got)
	}
	if !strings.Contains(got, "@.harnessator/active.md") {
		t.Errorf("managed block missing:\n%s", got)
	}
}
