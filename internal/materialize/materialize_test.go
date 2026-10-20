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
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/gitops"
	"harnessator.org/harnessator/internal/resolve"
)

const fakeSHA = "1111222233334444555566667777888899990000"

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func setupCtx(t *testing.T) *Context {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink-based materialization; windows covered by cross-build only")
	}
	fixture := mustAbs(t, filepath.Join("..", "..", "testdata", "sources", "agentictools"))
	checkout := filepath.Join(t.TempDir(), "agentictools")
	copyTree(t, fixture, checkout)

	m, err := config.LoadSourceManifest(filepath.Join(checkout, "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	pj, err := config.LoadProjectConfig(mustAbs(t,
		filepath.Join("..", "..", "testdata", "projects", "full", "harnessator.toml")))
	if err != nil {
		t.Fatal(err)
	}
	pins := map[string]*gitops.Pinned{"agentictools": {
		Name: "agentictools", Git: "file://" + checkout, Range: "main",
		Resolved: "main", SHA: fakeSHA, Dir: checkout,
	}}
	plan, err := resolve.Resolve(pj, map[string]*config.SourceManifest{m.Package.Name: m})
	if err != nil {
		t.Fatal(err)
	}
	return &Context{
		Project:   t.TempDir(),
		Manifests: map[string]*config.SourceManifest{m.Package.Name: m},
		Pins:      pins,
		Plan:      plan,
		Log:       io.Discard,
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestApplyAllOpencode(t *testing.T) {
	c := setupCtx(t)
	if err := ApplyAll(c); err != nil {
		t.Fatal(err)
	}
	if err := WriteShared(c); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(c.Project, ".opencode")

	link := filepath.Join(base, "skill", "example-skill")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("skill link: %v", err)
	}
	if !strings.HasSuffix(target, filepath.Join("agentictools", "skills", "example-skill")) {
		t.Errorf("skill target %q", target)
	}

	for _, rel := range []string{
		filepath.Join("agent", "code-reviewer.md"),
		filepath.Join("plugin", "format.ts"),
		filepath.Join("tool", "example-tool.ts"),
	} {
		if _, err := os.Lstat(filepath.Join(base, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}

	raw, err := os.ReadFile(filepath.Join(base, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Instructions []string                   `json:"instructions"`
		MCP          map[string]json.RawMessage `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.MCP["example-remote"]; !ok {
		t.Errorf("mcp server missing: %s", raw)
	}
	if len(cfg.Instructions) != 2 || cfg.Instructions[0] != "AGENTS.md" {
		t.Errorf("instructions = %v", cfg.Instructions)
	}
}

func TestApplyAllClaude(t *testing.T) {
	c := setupCtx(t)
	if err := ApplyAll(c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(c.Project, ".claude", "skills", "example-skill")); err != nil {
		t.Errorf("claude skill link: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(c.Project, ".claude", "agents", "code-reviewer.md")); err != nil {
		t.Errorf("claude agent link: %v", err)
	}

	settings, err := os.ReadFile(filepath.Join(c.Project, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(settings)
	if !strings.Contains(s, "PostToolUse") {
		t.Errorf("hooks template not merged: %s", s)
	}
	if strings.Contains(s, "{{") {
		t.Errorf("placeholder not substituted: %s", s)
	}

	mcp, err := os.ReadFile(filepath.Join(c.Project, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mcp), "example-remote") {
		t.Errorf(".mcp.json missing server: %s", mcp)
	}
}

func TestSharedIndexAndLock(t *testing.T) {
	c := setupCtx(t)
	if err := ApplyAll(c); err != nil {
		t.Fatal(err)
	}
	if err := WriteShared(c); err != nil {
		t.Fatal(err)
	}

	active, err := os.ReadFile(filepath.Join(c.Project, ".harnessator", "active.md"))
	if err != nil {
		t.Fatal(err)
	}
	a := string(active)
	if !strings.Contains(a, "agentictools:rule/00-agentictools-source-of-truth") {
		t.Error("active.md missing always rule")
	}
	if strings.Contains(a, "rule/10-code-style") {
		t.Error("active.md must not list excluded rule")
	}

	l, err := BuildLock(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Sources) != 1 || l.Sources[0].SHA != fakeSHA {
		t.Errorf("lock sources = %+v", l.Sources)
	}
	if len(l.Components) != len(c.Plan.Enabled) {
		t.Errorf("lock components %d != enabled %d", len(l.Components), len(c.Plan.Enabled))
	}
	for _, comp := range l.Components {
		if !strings.HasPrefix(comp.Digest, "sha256:") {
			t.Errorf("%s digest %q", comp.ID, comp.Digest)
		}
	}
}

func TestIdempotentRerun(t *testing.T) {
	c := setupCtx(t)
	for i := 0; i < 2; i++ {
		if err := ApplyAll(c); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if err := WriteShared(c); err != nil {
			t.Fatalf("shared %d: %v", i, err)
		}
	}
	// exactly one link per managed name after two runs
	matches, err := filepath.Glob(filepath.Join(c.Project, ".opencode", "skill", "example-skill"))
	if err != nil || len(matches) != 1 {
		t.Errorf("skill entries after rerun: %v %v", matches, err)
	}
}
