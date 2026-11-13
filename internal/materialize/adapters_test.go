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
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/gitops"
	"harnessator.org/harnessator/internal/policy"
	"harnessator.org/harnessator/internal/provider"
	"harnessator.org/harnessator/internal/resolve"
)

// setupForTargets is setupCtx with an explicit target set.
func setupForTargets(t *testing.T, targets ...string) *Context {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink-based; windows covered via cross-build + verify digests")
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
	pj.Targets = targets
	pj.Select = nil // adapter coverage wants EVERY fixture component, not the curated plan
	pins := map[string]*gitops.Pinned{"agentictools": {
		Name: "agentictools", Git: "file://" + checkout, Range: "main",
		Resolved: "main", SHA: fakeSHA, Dir: checkout,
	}}
	plan, err := resolve.Resolve(pj, map[string]*config.SourceManifest{m.Package.Name: m})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &Context{
		Project:   t.TempDir(),
		Manifests: map[string]*config.SourceManifest{m.Package.Name: m},
		Pins:      pins,
		Plan:      plan,
		Log:       io.Discard,
	}
	// governance like app.Open would resolve
	if pj.Provider != nil && pj.Provider.Profile != "" {
		ctx.Provider, err = provider.Parse(filepath.Join(checkout, "providers", "litellm-demo.toml"))
		if err != nil {
			t.Fatal(err)
		}
	}
	if pj.Policy != nil && len(pj.Policy.Profiles) > 0 {
		p, err := policy.Parse(filepath.Join(checkout, "policies", "models-strict.toml"))
		if err != nil {
			t.Fatal(err)
		}
		ctx.Policy = policy.Merge([]*policy.Policy{p}, []string{"agentictools:policy/models-strict"})
	}
	return ctx
}

func readStr(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func TestCodexAdapter(t *testing.T) {
	c := setupForTargets(t, "codex")
	if err := ApplyAll(c); err != nil {
		t.Fatal(err)
	}
	if err := WriteShared(c); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(c.Project, ".codex", "config.toml")
	cfg := readStr(t, cfgPath)
	if !strings.Contains(cfg, "[mcp_servers.example-remote]") {
		t.Errorf("mcp block missing:\n%s", cfg)
	}
	if !strings.Contains(cfg, `model_provider = "litellm-demo"`) ||
		!strings.Contains(cfg, `model = "claude-sonnet-5"`) ||
		!strings.Contains(cfg, "[model_providers.litellm-demo]") ||
		!strings.Contains(cfg, `wire_api = "chat"`) {
		t.Errorf("codex provider stanzas missing:\n%s", cfg)
	}
	if !strings.Contains(cfg, ">>> harnessator managed") || !strings.Contains(cfg, "<<< harnessator managed") {
		t.Error("managed markers missing")
	}
	if !strings.Contains(cfg, `url = "https://example.test/mcp"`) {
		t.Errorf("url line wrong:\n%s", cfg)
	}

	// human content OUTSIDE the managed block must survive a re-apply,
	// and the block must not duplicate
	human := "# my own codex settings\nmodel = \"gpt-5.2\"\n\n"
	if err := os.WriteFile(cfgPath, []byte(human+cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAll(c); err != nil {
		t.Fatal(err)
	}
	after := readStr(t, cfgPath)
	if !strings.Contains(after, `model = "gpt-5.2"`) {
		t.Error("human content destroyed by re-apply")
	}
	if strings.Count(after, ">>> harnessator managed") != 1 {
		t.Error("marker duplication on re-apply")
	}

	if problems := CheckSpecs(c); len(problems) != 0 {
		t.Errorf("CheckSpecs: %v", problems)
	}
}

func TestGovernanceOpencodeClaude(t *testing.T) {
	c := setupForTargets(t, "opencode", "claude")
	if err := ApplyAll(c); err != nil {
		t.Fatal(err)
	}
	if err := WriteShared(c); err != nil {
		t.Fatal(err)
	}

	oc := readStr(t, filepath.Join(c.Project, ".opencode", "opencode.json"))
	for _, want := range []string{
		`"model": "litellm-demo/claude-sonnet-5"`,
		`"baseURL": "https://litellm.example.test/v1"`,
		`"{env:LITELLM_API_KEY}"`,
		"@ai-sdk/openai-compatible",
		"claude-sonnet-5",
		"gpt-5.2", // smart allowed
	} {
		if !strings.Contains(oc, want) {
			t.Errorf("opencode.json missing %q:\n%s", want, oc)
		}
	}
	if strings.Contains(oc, "claude-haiku-5.5") {
		t.Errorf("policy-denied model leaked into opencode.json:\n%s", oc)
	}

	st := readStr(t, filepath.Join(c.Project, ".claude", "settings.local.json"))
	for _, want := range []string{
		"ANTHROPIC_BASE_URL",
		`"ANTHROPIC_MODEL": "claude-sonnet-5"`,
		`"CLAUDE_CODE_SUBAGENT_MODEL": "claude-sonnet-5"`, // sub-agent pin
		`"ANTHROPIC_DEFAULT_SONNET_MODEL": "claude-sonnet-5"`,
	} {
		if !strings.Contains(st, want) {
			t.Errorf("settings.local.json missing %q:\n%s", want, st)
		}
	}
	if strings.Contains(st, "ANTHROPIC_SMALL_FAST_MODEL") {
		t.Error("policy-denied alias rendered into claude env")
	}
	if strings.Contains(st, "LITELLM_API_KEY_VALUE") {
		t.Error("secret material never belongs in settings")
	}
}

func TestAntigravityAndCursorAndPi(t *testing.T) {
	c := setupForTargets(t, "antigravity", "cursor", "pi")
	rulesDir := filepath.Join(c.Project, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// stale harnessator mdc + a foreign file: only the stale one may die
	if err := os.WriteFile(filepath.Join(rulesDir, "z-removed.mdc"),
		[]byte(mdcMarker+"\nold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(rulesDir, "mine.mdc")
	if err := os.WriteFile(foreign, []byte("hand-written\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyAll(c); err != nil {
		t.Fatal(err)
	}
	if err := WriteShared(c); err != nil {
		t.Fatal(err)
	}

	// antigravity: AgentSkills location + mcp_config.json
	if _, err := os.Lstat(filepath.Join(c.Project, ".agents", "skills", "example-skill")); err != nil {
		t.Errorf("antigravity skill: %v", err)
	}
	mcp := readStr(t, filepath.Join(c.Project, ".agents", "mcp_config.json"))
	if !strings.Contains(mcp, "mcpServers") {
		t.Error("mcp_config.json shape wrong")
	}

	// cursor: generated mdc with frontmatter from hints/always
	r00 := readStr(t, filepath.Join(rulesDir, "00-agentictools-source-of-truth.mdc"))
	if !strings.Contains(r00, "alwaysApply: true") || !strings.Contains(r00, mdcMarker) {
		t.Errorf("always rule mdc:\n%s", r00)
	}
	r10 := readStr(t, filepath.Join(rulesDir, "10-code-style.mdc"))
	if !strings.Contains(r10, `globs: ["**/*"]`) || !strings.Contains(r10, "alwaysApply: false") {
		t.Errorf("hinted rule mdc:\n%s", r10)
	}
	if _, err := os.Lstat(filepath.Join(rulesDir, "z-removed.mdc")); !os.IsNotExist(err) {
		t.Error("stale harnessator mdc not cleaned")
	}
	if _, err := os.Lstat(foreign); err != nil {
		t.Error("foreign mdc must survive")
	}

	// pi: package farm + settings.json
	pkg := readStr(t, filepath.Join(c.Project, ".harnessator", "pi", "agentictools", "package.json"))
	if !strings.Contains(pkg, "./skills/example-skill") || !strings.Contains(pkg, "pi-package") {
		t.Errorf("package.json:\n%s", pkg)
	}
	lnk := filepath.Join(c.Project, ".harnessator", "pi", "agentictools", "skills", "example-skill")
	target, err := os.Readlink(lnk)
	if err != nil || !strings.HasSuffix(target, filepath.Join("skills", "example-skill")) {
		t.Errorf("farm skill link: %q %v", target, err)
	}
	settings := readStr(t, filepath.Join(c.Project, ".pi", "settings.json"))
	if !strings.Contains(settings, "../.harnessator/pi/agentictools") {
		t.Errorf("settings.json:\n%s", settings)
	}

	if problems := CheckSpecs(c); len(problems) != 0 {
		t.Errorf("CheckSpecs: %v", problems)
	}
}
