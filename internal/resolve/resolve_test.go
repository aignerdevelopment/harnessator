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

package resolve

import (
	"path/filepath"
	"testing"

	"harnessator.org/harnessator/internal/config"
)

func fixtures(t *testing.T) (*config.ProjectConfig, map[string]*config.SourceManifest) {
	t.Helper()
	srcPath := mustAbs(t, filepath.Join("..", "..", "testdata", "sources", "agentictools", "harnessator.toml"))
	m, err := config.LoadSourceManifest(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	projPath := mustAbs(t, filepath.Join("..", "..", "testdata", "projects", "full", "harnessator.toml"))
	c, err := config.LoadProjectConfig(projPath)
	if err != nil {
		t.Fatal(err)
	}
	return c, map[string]*config.SourceManifest{m.Package.Name: m}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func has(list []Choice, want string) bool {
	for _, c := range list {
		if c.QualifiedID() == want {
			return true
		}
	}
	return false
}

func TestResolveFullProfile(t *testing.T) {
	c, manifests := fixtures(t)
	plan, err := Resolve(c, manifests)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", plan.Warnings)
	}
	wantEnabled := []string{
		"agentictools:rule/00-agentictools-source-of-truth",
		"agentictools:skill/example-skill",
		"agentictools:mcp/example-remote",
		"agentictools:tool/example-tool",
		"agentictools:hook/shared-format",
		"agentictools:agent/code-reviewer",
		"agentictools:provider/litellm-demo",
		"agentictools:policy/models-strict",
	}
	for _, id := range wantEnabled {
		if !has(plan.Enabled, id) {
			t.Errorf("missing enabled %s", id)
		}
	}
	// excluded non-always rule
	for _, s := range plan.Skips {
		if s.Choice.QualifiedID() == "agentictools:rule/10-code-style" && s.Reason == "" {
			t.Error("empty skip reason")
		}
	}
	if has(plan.Enabled, "agentictools:rule/10-code-style") {
		t.Error("rule/10 should be excluded")
	}
	// provenance: template + always merged, not warned
	for _, ch := range plan.Enabled {
		if ch.ID == "00-agentictools-source-of-truth" {
			if ch.Reason != "template agentictools:full +always" {
				t.Errorf("provenance = %q", ch.Reason)
			}
		}
	}
}

func TestResolveAlwaysSurvivesExclude(t *testing.T) {
	c, manifests := fixtures(t)
	sel := c.Select["rule"]
	sel.Exclude = append(sel.Exclude, "00-agentictools-source-of-truth")
	c.Select["rule"] = sel
	plan, err := Resolve(c, manifests)
	if err != nil {
		t.Fatal(err)
	}
	if !has(plan.Enabled, "agentictools:rule/00-agentictools-source-of-truth") {
		t.Error("always rule must survive exclusion")
	}
	if len(plan.Warnings) == 0 {
		t.Error("expected a warning about the futile exclusion")
	}
}

func TestResolveTargetMismatchSkipsLoudly(t *testing.T) {
	c, manifests := fixtures(t)
	c.Targets = []string{"claude"} // example-tool is opencode-only
	plan, err := Resolve(c, manifests)
	if err != nil {
		t.Fatal(err)
	}
	if has(plan.Enabled, "agentictools:tool/example-tool") {
		t.Error("opencode-only tool enabled for claude")
	}
	found := false
	for _, s := range plan.Skips {
		if s.Choice.ID == "example-tool" && s.Reason != "" {
			found = true
		}
	}
	if !found {
		t.Error("tool skip not reported")
	}
}
