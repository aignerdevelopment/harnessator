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

// The full circle: a working claude config becomes a governed provider
// source, gets consumed, selected, and passes the gate. Secrets stay
// where they were.
func TestProviderImportFullLoop(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{
	  "apiKeyHelper": "echo sk-never-copy-me",
	  "env": {"ANTHROPIC_BASE_URL": "https://gw.example.org"}
	}`), 0o644)
	t.Setenv("HOME", home)
	t.Setenv("HARNESSATOR_HOME", filepath.Join(root, "store"))
	work := filepath.Join(root, "work")
	os.MkdirAll(work, 0o755)
	t.Chdir(work)

	// dry-run first: plan only, nothing written
	code, out, _ := runCLI(t, "", "provider", "import", "claude", "--name", "my-providers", "--dry-run")
	if code != 0 || !strings.Contains(out, "would be written") {
		t.Fatalf("dry-run: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(work, "my-providers")); err == nil {
		t.Error("dry-run created the directory")
	}

	// real import
	code, out, errStr := runCLI(t, "", "provider", "import", "claude", "--name", "my-providers")
	if code != 0 {
		t.Fatalf("import: %s %s", out, errStr)
	}
	for _, want := range []string{"provider/claude-gateway", "next:", "provider use"} {
		if !strings.Contains(out, want) {
			t.Errorf("import output missing %q:\n%s", want, out)
		}
	}
	// the generated source must be valid BY THE TOOL'S OWN GATE
	if code, out, e := runCLI(t, "", "validate", "--check-paths", "my-providers/harnessator.toml"); code != 0 {
		t.Fatalf("validate: %s %s", out, e)
	}
	profRaw, err := os.ReadFile(filepath.Join(work, "my-providers", "providers", "claude-gateway.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(profRaw), "never-copy") {
		t.Error("SECRET LEAKED into the imported profile")
	}
	if !strings.Contains(string(profRaw), "https://gw.example.org") {
		t.Errorf("endpoint missing:\n%s", profRaw)
	}

	// consume it
	proj := filepath.Join(root, "proj")
	os.MkdirAll(proj, 0o755)
	if code, _, e := runCLI(t, "", "init", "--targets", "claude", "--source", "file://"+filepath.Join(work, "my-providers"), proj); code != 0 {
		t.Fatalf("init: %s", e)
	}
	cfg := filepath.Join(proj, "harnessator.toml")
	if code, out, e := runCLI(t, "", "provider", "use", "my-providers:provider/claude-gateway", "--config", cfg); code != 0 {
		t.Fatalf("provider use: %s %s", e, out)
	}
	if code, out, e := runCLI(t, "", "enable", "my-providers:default", "--config", cfg); code != 0 {
		t.Fatalf("enable: %s %s", e, out)
	}
	if code, out, e := runCLI(t, "", "setup", "--config", cfg); code != 0 {
		t.Fatalf("setup: %s %s", e, out)
	}
	// provider show proves the profile is live governance, not dead files
	code, out, _ = runCLI(t, "", "provider", "show", "--config", cfg)
	if code != 0 || !strings.Contains(out, "claude-gateway") {
		t.Errorf("provider show:\n%s", out)
	}
	if code, out, _ := runCLI(t, "", "check", "--json", "--config", cfg); code != 0 {
		t.Errorf("check: %s", out)
	}

	// re-import into the same source: idempotent
	t.Chdir(work)
	code, out, _ = runCLI(t, "", "provider", "import", "claude", "--into", "my-providers")
	if code != 0 || !strings.Contains(out, "skip-identical") {
		t.Errorf("re-import must be idempotent, code %d:\n%s", code, out)
	}

	// unknown harness: a ramp, not a shrug
	if code, _, e := runCLI(t, "", "provider", "import", "qoder"); code == 0 || !strings.Contains(e, "unknown harness") {
		t.Errorf("want actionable unknown-harness error, code %d: %s", code, e)
	}
}
