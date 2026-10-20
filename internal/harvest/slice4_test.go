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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"harnessator.org/harnessator/internal/config"
)

// TestSelfSource proves the manifest-path concept on its first customer:
// this repo's source/harnessator (the harnessator-harvest skill), consumed
// like any other source via [[source]].manifest.
func TestSelfSource(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "source", "harnessator"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := config.LoadSourceManifest(filepath.Join(dir, "harnessator.toml"))
	if err != nil {
		t.Fatalf("self source manifest: %v", err)
	}
	if err := config.ValidateSource(m, dir); err != nil {
		t.Fatalf("self source invalid: %v", err)
	}
	if m.Package.Name != "harnessator" {
		t.Errorf("package = %q", m.Package.Name)
	}
	if _, ok := m.Components["skill"]["harnessator-harvest"]; !ok {
		t.Error("harnessator-harvest skill not registered")
	}
}

func TestResidualReport(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	donor := filepath.Join(base, "proj")
	write(t, filepath.Join(donor, ".claude/skills/keep/SKILL.md"), "---\nname: keep\ndescription: K.\n---\n")
	write(t, filepath.Join(donor, ".claude/skills/no-md/x.txt"), "stray dir\n")
	write(t, filepath.Join(donor, ".claude/commands/notes.txt"), "stray file\n")

	res, err := Harvest(Options{Paths: []string{donor}, Name: "pub"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Residual < 2 {
		t.Fatalf("want >=2 residuals, got %d", res.Residual)
	}
	b, err := os.ReadFile(filepath.Join(base, "pub", "harvest-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "no-md") || !strings.Contains(s, "notes.txt") {
		t.Errorf("residuals missing from report: %s", s)
	}
	if strings.Contains(s, "skills/keep") {
		t.Errorf("harvested content must not be residual: %s", s)
	}
	if !strings.Contains(s, "harnessator-harvest") {
		t.Errorf("report must point at the guided tail: %s", s)
	}
	var r ResidualReport
	if json.Unmarshal(b, &r) != nil || r.Schema != 1 {
		t.Errorf("report json envelope broken: %s", s)
	}
}

func TestCanonicalMCPShapes(t *testing.T) {
	claude := json.RawMessage(`{"command":"npx","args":["-y","x"],"env":{"A":"B"}}`)
	ocl := json.RawMessage(`{"type":"local","command":["npx","-y","x"],"environment":{"A":"B"}}`)
	_, d1, err := canonicalMCP(claude)
	if err != nil {
		t.Fatal(err)
	}
	_, d2, err := canonicalMCP(ocl)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Error("claude vs opencode-local shape must digest equal")
	}
	ocRemote := json.RawMessage(`{"type":"remote","url":"https://mcp.test","headers":{"H":"v"}}`)
	bare := json.RawMessage(`{"url":"https://mcp.test","headers":{"H":"v"}}`)
	_, d3, _ := canonicalMCP(ocRemote)
	_, d4, _ := canonicalMCP(bare)
	if d3 != d4 {
		t.Error("remote vs bare-url shape must digest equal")
	}
	other := json.RawMessage(`{"url":"https://elsewhere"}`)
	_, d5, _ := canonicalMCP(other)
	if d5 == d3 {
		t.Error("different servers must not collide")
	}
}
