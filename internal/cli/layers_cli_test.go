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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Scope layers end to end: a team-layer source, a per-project personal
// (local) selection, provenance that names the file, and the layers
// view showing both.
func TestScopeLayersEndToEnd(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HARNESSATOR_HOME", filepath.Join(root, "store"))
	os.MkdirAll(filepath.Join(root, "store"), 0o755)
	proj := filepath.Join(root, "proj")
	os.MkdirAll(proj, 0o755)
	url := mkSourceRepo(t) // file:// agentictools fixture, tagged

	if code, _, e := runCLI(t, "", "init", "--targets", "claude", "--source", url, proj); code != 0 {
		t.Fatalf("init: %s", e)
	}
	cfg := filepath.Join(proj, "harnessator.toml")

	// team layer registers the corpus under a second name
	if code, out, e := runCLI(t, "", "source", "add", url, "--name", "team-tools", "--layer", "team", "--config", cfg); code != 0 {
		t.Fatalf("source add --layer team: %s %s", out, e)
	}
	teamRaw, err := os.ReadFile(filepath.Join(proj, "harnessator.team.toml"))
	if err != nil || !strings.Contains(string(teamRaw), "team-tools") {
		t.Fatalf("team file: %v\n%s", err, teamRaw)
	}
	projRaw, _ := os.ReadFile(cfg)
	if strings.Contains(string(projRaw), "team-tools") {
		t.Errorf("team source leaked into the project file:\n%s", projRaw)
	}

	// local layer: my per-project task toolchain picks one skill
	if code, out, e := runCLI(t, "", "enable", "team-tools:skill/example-skill", "--layer", "local", "--config", cfg); code != 0 {
		t.Fatalf("enable --layer local: %s %s", out, e)
	}
	localRaw, err := os.ReadFile(filepath.Join(proj, ".harnessator", "local.toml"))
	if err != nil || !strings.Contains(string(localRaw), "example-skill") {
		t.Fatalf("local layer file: %v\n%s", err, localRaw)
	}

	// resolution + provenance names the introducing layer
	if code, out, e := runCLI(t, "", "setup", "--config", cfg); code != 0 {
		t.Fatalf("setup: %s %s", out, e)
	}
	code, out, e := runCLI(t, "", "why", "skill/example-skill", "--config", cfg)
	if code != 0 || !strings.Contains(out, "via local") {
		t.Fatalf("why must name the local layer: code=%d out=%s err=%s", code, out, e)
	}

	// layers view lists the team + local files
	code, out, e = runCLI(t, "", "layers", "--config", cfg)
	if code != 0 || !strings.Contains(out, "team") || !strings.Contains(out, "local") {
		t.Fatalf("layers view: code=%d out=%s err=%s", code, out, e)
	}

	// a higher layer re-declaring the same source name with a different
	// URL is a hard conflict, and every command that resolves fails loud
	writeFile := []byte("kind = \"project\"\nschema = 1\n\n[[source]]\n  name = \"agentictools\"\n  git = \"https://gitlab.example.com/evil/twin.git\"\n  ref = \"main\"\n")
	os.WriteFile(filepath.Join(proj, ".harnessator", "local.toml"), writeFile, 0o644)
	code, out, e = runCLI(t, "", "list", "--config", cfg)
	if code == 0 {
		t.Fatal("conflicting layer must block resolution")
	}
	if !strings.Contains(out+e, "twin") && !strings.Contains(out+e, "conflict") {
		t.Errorf("conflict message should name the source: %s%s", out, e)
	}
}
