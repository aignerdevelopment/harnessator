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
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectRoundTrip is the field-order guard: MarshalProject must emit a
// file LoadProjectConfig accepts AND that re-marshals to identical bytes.
// If someone reorders ProjectConfig so a scalar lands after [[source]],
// this fails (that's the exact trap that bit the hand-written fixtures).
func TestProjectRoundTrip(t *testing.T) {
	c := &ProjectConfig{
		Targets:   []string{"opencode", "claude", "pi"},
		Templates: []string{"corp:default"},
		Sources: []Source{
			{Name: "corp", Git: "https://h/c.git", Ref: "~v1.2"},
			{Name: "dev", Git: "https://h/d.git", Ref: "main", Layer: "personal"},
		},
		Select: map[string]Selection{
			"mcp": {Include: []string{"*"}, Exclude: []string{"corp:slack"}},
		},
		Provider:  &ProviderConfig{Profile: "corp:provider/litellm-acme"},
		Policy:    &PolicyConfig{Profiles: []string{"corp:policy/models-approved"}},
		Corporate: &CorporateConfig{Allowlist: []string{"https://h/**"}, LockMode: "strict"},
	}

	first, err := MarshalProject(c)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "harnessator.toml")
	if err := WriteProject(path, c); err != nil {
		t.Fatal(err)
	}
	back, err := LoadProjectConfig(path)
	if err != nil {
		t.Fatalf("reload: %v\n%s", err, first)
	}
	second, err := MarshalProject(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("not byte-stable:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}

	// scalars must precede the [[source]] table
	if strings.Index(string(first), "\ntargets") > strings.Index(string(first), "\n[[source]]") {
		t.Error("top-level scalar emitted after [[source]] - would not reload")
	}
	if back.Provider == nil || back.Provider.Profile != "corp:provider/litellm-acme" {
		t.Errorf("provider round-trip lost: %+v", back.Provider)
	}
	if back.Corporate == nil || back.Corporate.LockMode != "strict" {
		t.Errorf("corporate round-trip lost: %+v", back.Corporate)
	}
}

func TestMutations(t *testing.T) {
	c := &ProjectConfig{Targets: []string{"opencode"}}
	if err := c.AddSource(Source{Name: "a", Git: "x", Ref: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := c.AddSource(Source{Name: "a", Git: "y", Ref: "main"}); err == nil {
		t.Error("duplicate source accepted")
	}
	if !c.AddTemplate("a:minimal") || c.AddTemplate("a:minimal") {
		t.Error("template add idempotence broken")
	}
	c.Include("skill", "a:example")
	c.Include("skill", "a:example")
	if got := c.Select["skill"].Include; len(got) != 1 {
		t.Errorf("include not deduped: %v", got)
	}
	c.Exclude("skill", "a:example")
	if len(c.Select["skill"].Include) != 0 || len(c.Select["skill"].Exclude) != 1 {
		t.Errorf("exclude should move id out of include: %+v", c.Select["skill"])
	}
	if !c.RemoveSource("a") || c.RemoveSource("a") {
		t.Error("source remove idempotence broken")
	}
}
