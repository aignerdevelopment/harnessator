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
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"harnessator.org/harnessator/internal/provider"
)

// --- pure reconciliation ----------------------------------------------------

func tuneProfile(models map[string]string) *provider.Profile {
	return &provider.Profile{Name: "gw", AuthEnv: "GW_KEY", Styles: map[string]provider.Endpoint{
		provider.StyleOpenAICompat: {BaseURL: "http://x/v1"},
	}, Models: models}
}

func TestReconcileCatalog(t *testing.T) {
	p := tuneProfile(map[string]string{"fast": "keep-me", "old": "gone-me"})
	live := map[string]bool{"keep-me": true, "brand-new": true}
	adds, drops := reconcileCatalog(p, live)
	if len(drops) != 1 || drops[0].Alias != "old" || drops[0].ID != "gone-me" {
		t.Errorf("drops = %+v", drops)
	}
	if len(adds) != 1 || adds[0].ID != "brand-new" || adds[0].Alias != "brand-new" {
		t.Errorf("adds = %+v", adds)
	}
	// collision gets a bump; already-referenced ids add nothing
	p2 := tuneProfile(map[string]string{"brand-new": "x", "fast": "keep-me"})
	adds2, _ := reconcileCatalog(p2, live)
	if len(adds2) != 1 || adds2[0].Alias != "brand-new-2" {
		t.Errorf("collision adds = %+v", adds2)
	}
	if n := len(mustAdds(p2, map[string]bool{"keep-me": true, "x": true})); n != 0 {
		t.Errorf("referenced ids re-added: %d", n)
	}
}

func mustAdds(p *provider.Profile, live map[string]bool) []catalogAdd {
	a, _ := reconcileCatalog(p, live)
	return a
}

func TestSuggestAlias(t *testing.T) {
	cases := map[string]string{
		"claude-sonnet-5":   "claude-sonnet-5",
		"GPT 5.3 (2026-01)": "gpt-5.3-2026-01",
		"@weird//id":        "weird-id",
		"anthropic/claude-opus-5-very-long-model-name-for-prefix-trimming": "opus-5-very-long-model-name-for-prefix-trimming",
	}
	for in, want := range cases {
		if got := suggestAlias(in, map[string]bool{}); got != want {
			t.Errorf("suggestAlias(%q) = %q, want %q", in, got, want)
		}
	}
	if got := suggestAlias("a-b", map[string]bool{"a-b": true}); got != "a-b-2" {
		t.Errorf("bump: %q", got)
	}
}

func TestApplyCatalogEdits(t *testing.T) {
	src := "# gateway profile\nname = \"gw\"\nauth_env = \"GW_KEY\"\n\n[styles.openai-compatible]\nbase_url = \"http://x/v1\"\n\n[models]\n# curated by the team\nfast = \"keep-me\"\nold = \"gone-me\"\n"
	out := applyCatalogEdits(src,
		[]catalogAdd{{ID: "brand-new", Alias: "brand-new"}},
		[]catalogDrop{{Alias: "old", ID: "gone-me"}})
	if !strings.Contains(out, `brand-new = "brand-new"`) || strings.Contains(out, "gone-me") {
		t.Fatalf("edits wrong:\n%s", out)
	}
	if !strings.Contains(out, "# curated by the team") || !strings.Contains(out, "[styles.openai-compatible]") {
		t.Errorf("comments/other tables damaged:\n%s", out)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "harnessator.toml"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Parse(filepath.Join(dir, "harnessator.toml")); err != nil {
		t.Errorf("result does not parse: %v", err)
	}

	// no [models] section yet -> appended
	noModels := "name = \"gw\"\nauth_env = \"K\"\n\n[styles.openai-compatible]\nbase_url = \"http://x/v1\"\n"
	out2 := applyCatalogEdits(noModels, []catalogAdd{{ID: "m1", Alias: "m1"}}, nil)
	if !strings.HasSuffix(strings.TrimSpace(out2), "m1 = \"m1\"") || !strings.Contains(out2, "[models]") {
		t.Fatalf("append case:\n%s", out2)
	}
}

// --- end-to-end phases ------------------------------------------------------

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// tuneSource writes a source repo whose provider points at gatewayURL.
func tuneSource(t *testing.T, gatewayURL string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "gwsrc")
	os.MkdirAll(filepath.Join(repo, "providers"), 0o755)
	os.WriteFile(filepath.Join(repo, "providers", "gw.toml"), []byte(
		fmt.Sprintf(`name = "gw"
auth_env = "GW_KEY"

[styles.openai-compatible]
base_url = "%s/v1"

[models]
fast = "keep-me"
old  = "gone-me"
`, gatewayURL)), 0o644)
	os.WriteFile(filepath.Join(repo, "harnessator.toml"), []byte(
		`kind = "source"
schema = 1

[package]
name = "gwsrc"

[profiles.default]
provider = ["gw"]

[component.provider."gw"]
path = "providers/gw.toml"
description = "test gateway"
`), 0o644)
	gitIn(t, repo, "init", "-qb", "main", ".")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-qm", "seed")
	return "file://" + repo
}

func tuneProject(t *testing.T, url string) string {
	t.Helper()
	t.Setenv("HARNESSATOR_HOME", t.TempDir())
	proj := t.TempDir()
	var out, errOut bytes.Buffer
	if code := Run(&out, &errOut, []string{"init", "--targets", "opencode",
		"--source", url, "--ref", "main", proj}); code != 0 {
		t.Fatalf("init: %s%s", out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	cfg := filepath.Join(proj, "harnessator.toml")
	if code := Run(&out, &errOut, []string{"enable", "--config", cfg, "gwsrc:default"}); code != 0 {
		t.Fatalf("enable: %s%s", out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"setup", "--config", cfg}); code != 0 {
		t.Fatalf("setup: %s%s", out.String(), errOut.String())
	}
	return cfg
}

func TestTuneCheckOnlySeesGatewayTruth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"keep-me"},{"id":"brand-new"}]}`)
	}))
	defer srv.Close()
	cfg := tuneProject(t, tuneSource(t, srv.URL))
	t.Setenv("GW_KEY", "test-key")

	var out, errOut bytes.Buffer
	code := RunIO(strings.NewReader(""), &out, &errOut, []string{"tune", "--config", cfg, "--check-only"})
	got := out.String()
	if code != 0 {
		t.Fatalf("exit %d\n%s%s", code, got, errOut.String())
	}
	for _, want := range []string{
		"[1/7] config", "[3/7] auth", "GW_KEY is set",
		"[5/7] gateway probe", "2 models reachable",
		"stale alias old -> gone-me", `new model brand-new reachable (suggested alias "brand-new")`,
		"1 stale, 1 new (check-only)", "tune summary",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "test-key") {
		t.Error("secret value echoed")
	}
}

// --yes applies: catalog reconciliation must land in a dev overlay (never
// the pinned checkout), and setup must re-render from it.
func TestTuneYesAppliesViaDevOverlay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"keep-me"},{"id":"brand-new"}]}`)
	}))
	defer srv.Close()
	srcURL := tuneSource(t, srv.URL)
	cfg := tuneProject(t, srcURL)
	t.Setenv("GW_KEY", "test-key")

	var out, errOut bytes.Buffer
	code := RunIO(strings.NewReader(""), &out, &errOut, []string{"tune", "--config", cfg, "--yes"})
	got := out.String()
	if code != 0 {
		t.Fatalf("exit %d\n%s%s", code, got, errOut.String())
	}
	if !strings.Contains(got, "via dev overlay") || !strings.Contains(got, "setup re-applied") {
		t.Fatalf("summary missing apply proof:\n%s", got)
	}
	home := os.Getenv("HARNESSATOR_HOME")
	overlay := filepath.Join(home, "dev", "gwsrc", "providers", "gw.toml")
	raw, err := os.ReadFile(overlay)
	if err != nil {
		t.Fatalf("overlay edit missing: %v\n%s", err, got)
	}
	if !strings.Contains(string(raw), `brand-new = "brand-new"`) || strings.Contains(string(raw), "gone-me") {
		t.Errorf("overlay catalog wrong:\n%s", raw)
	}
	// the pinned store checkout must be untouched (governance invariant)
	pinned := filepath.Join(home, "sources", "gwsrc", "checkouts")
	filepath.WalkDir(pinned, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, "gw.toml") {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), "brand-new") {
				t.Errorf("pinned checkout was modified: %s", p)
			}
		}
		return nil
	})
}

func TestTuneNoProviderIsNotAnError(t *testing.T) {
	url := mkSourceRepo(t) // rules-only profile, no provider selected
	t.Setenv("HARNESSATOR_HOME", t.TempDir())
	proj := t.TempDir()
	var out, errOut bytes.Buffer
	Run(&out, &errOut, []string{"init", "--targets", "claude", "--source", url, "--ref", "~v0.1", proj})
	out.Reset()
	errOut.Reset()
	cfg := filepath.Join(proj, "harnessator.toml")
	Run(&out, &errOut, []string{"enable", "--config", cfg, "agentictools-src:minimal"})
	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"setup", "--config", cfg}); code != 0 {
		t.Fatalf("setup: %s%s", out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	code := RunIO(strings.NewReader(""), &out, &errOut, []string{"tune", "--config", cfg, "--check-only"})
	if code != 0 {
		t.Fatalf("tune without provider should be clean, exit %d\n%s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "no provider profile in effect") {
		t.Errorf("missing guidance:\n%s", out.String())
	}
	// the phase doctrine: every phase body speaks, even in a no-op run
	// (an empty section reads like a hang - owner's recompile paste, 2026-11-13)
	if !strings.Contains(out.String(), "model catalog\n  note   skipped (no provider)") {
		t.Errorf("phase 6 must announce its own skip:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "catalog") || !strings.Contains(out.String(), "skipped (no provider)") {
		t.Errorf("summary/phase lines incomplete:\n%s", out.String())
	}
}
