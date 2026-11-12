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

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The five scope layers merge additively in precedence order; provenance
// records the introducing file.
func TestLayerMergePrecedence(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	projDir := filepath.Join(dir, "proj")
	proj := filepath.Join(projDir, "harnessator.toml")

	writeFile(t, proj, `kind = "project"
schema = 1
targets = ["claude"]

[[source]]
  name = "corp"
  git  = "https://gitlab.example.com/corp/tools.git"
  ref  = "main"
`)
	writeFile(t, filepath.Join(projDir, "harnessator.team.toml"), `kind = "project"
schema = 1
targets   = ["opencode"]
templates = ["corp:team-default"]
`)
	writeFile(t, filepath.Join(store, "personal.toml"), `kind = "project"
schema = 1
templates = ["corp:personal-picks"]

[select]
  [select.skill]
    include = ["corp:skill/poweruser"]
`)

	l, err := LoadLayers(store, proj)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(l.Loaded, ",") != "team,project,personal" {
		t.Errorf("loaded = %v", l.Loaded)
	}
	eff, err := l.Effective()
	if err != nil {
		t.Fatal(err)
	}
	// ascending precedence: team file before project file
	if strings.Join(eff.Targets, ",") != "opencode,claude" {
		t.Errorf("targets union: %v", eff.Targets)
	}
	// precedence order: team before personal (project has no templates)
	if strings.Join(eff.Templates, ",") != "corp:team-default,corp:personal-picks" {
		t.Errorf("templates: %v", eff.Templates)
	}
	if got := eff.Origin("template\x00corp:team-default"); got != "team" {
		t.Errorf("template origin = %q", got)
	}
	if got := eff.Origin("include\x00skill\x00corp:skill/poweruser"); got != "personal" {
		t.Errorf("include origin = %q", got)
	}
	if len(eff.Sources) != 1 || eff.Sources[0].Layer != "project" {
		t.Errorf("source label: %+v", eff.Sources)
	}
}

// The same source name pointing at two git URLs must fail resolution -
// never a silent pick.
func TestLayerSourceConflictHardFails(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "harnessator.toml")
	writeFile(t, proj, `kind = "project"
schema = 1

[[source]]
  name = "tools"
  git  = "https://gitlab.example.com/a/tools.git"
  ref  = "main"
`)
	writeFile(t, filepath.Join(dir, ".harnessator", "local.toml"), `kind = "project"
schema = 1

[[source]]
  name = "tools"
  git  = "https://gitlab.example.com/b/other.git"
  ref  = "main"
`)
	_, err := LoadLayers(filepath.Join(dir, "store"), proj)
	if err == nil {
		t.Fatal("same name / different URL must be a hard conflict")
	}
	if !strings.Contains(err.Error(), "tools") {
		t.Errorf("conflict must name the source: %v", err)
	}
}

// A layer file created by WriteLayer persists with canonical schema and
// feeds back into the effective view.
func TestLayerWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "harnessator.toml")
	writeFile(t, proj, `kind = "project"
schema = 1
`)
	l, err := LoadLayers(filepath.Join(dir, "store"), proj)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := l.LayerCfg("local")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Include("skill", "me:skill/ninja")
	if err := l.WriteLayer("local", cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".harnessator", "local.toml"))
	if err != nil || !strings.Contains(string(raw), "ninja") {
		t.Fatalf("local file: %v\n%s", err, raw)
	}
	if strings.Contains(string(raw), "prov") || strings.Contains(string(raw), "\x00") {
		t.Error("in-memory provenance must never serialize")
	}
	eff, err := l.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if got := eff.Origin("include\x00skill\x00me:skill/ninja"); got != "local" {
		t.Errorf("origin after write = %q", got)
	}
}

// The unknown-layer guard is a plain input error, not a panic.
func TestLayerUnknownName(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "harnessator.toml")
	writeFile(t, proj, `kind = "project"
schema = 1
`)
	l, err := LoadLayers(filepath.Join(dir, "store"), proj)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.LayerCfg("bogus"); err == nil {
		t.Error("unknown layer must be rejected")
	}
}
