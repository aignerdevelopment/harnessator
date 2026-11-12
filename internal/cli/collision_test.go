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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The demo incident, made a test: two projects register sources with the
// same basename (auto-name "tools") from DIFFERENT donor repos sharing
// one user store. Before the guard, the second project silently consumed
// the first project's content. Now every path to the wedge is a ramp.
func TestStoreNameCollisionFullLoop(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HARNESSATOR_HOME", filepath.Join(root, "store"))

	mkDonor := func(rel string) string {
		dir := filepath.Join(root, "donors", rel)
		copyTree(t, mustAbs(t, filepath.Join("..", "..", "testdata", "sources", "agentictools")), dir)
		for _, args := range [][]string{
			{"init", "-b", "main"},
			{"add", "."},
			{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "seed"},
			{"tag", "v0.1.0"},
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		return "file://" + dir
	}
	urlA, urlB := mkDonor("alpha/tools"), mkDonor("beta/tools")

	proj := func(name string) (dir, cfg string) {
		dir = filepath.Join(root, name)
		os.MkdirAll(dir, 0o755)
		return dir, filepath.Join(dir, "harnessator.toml")
	}
	dirA, cfgA := proj("projA")
	dirB, cfgB := proj("projB")

	if code, _, e := runCLI(t, "", "init", "--targets", "claude", "--source", urlA, dirA); code != 0 {
		t.Fatalf("init A: %s", e)
	}
	if code, _, e := runCLI(t, "", "setup", "--config", cfgA); code != 0 {
		t.Fatalf("setup A: %s", e)
	}
	if code, _, e := runCLI(t, "", "init", "--targets", "claude", "--source", urlB, dirB); code != 0 {
		t.Fatalf("init B: %s", e)
	}

	// 1. setup B must REFUSE, loudly, naming both URLs and both ramps.
	code, out, e := runCLI(t, "", "setup", "--config", cfgB)
	if code == 0 || !strings.Contains(out+e, "one store name, two repositories") {
		t.Fatalf("want collision refusal, got code %d:\n%s%s", code, out, e)
	}
	for _, want := range []string{"source forget", "--name"} {
		if !strings.Contains(out+e, want) {
			t.Errorf("refusal missing ramp %q", want)
		}
	}

	// 2. source add catches the alias at the moment of the mistake (note):
	// B frees the name in its config and re-adds it - the store clone
	// still belongs to A, so the note must fire right there.
	if code, _, e := runCLI(t, "", "source", "remove", "tools", "--config", cfgB); code != 0 {
		t.Fatalf("source remove B: %s", e)
	}
	code, out, e = runCLI(t, "", "source", "add", urlB, "--config", cfgB)
	if code != 0 {
		t.Fatalf("source add B: %d %s%s", code, out, e)
	}
	if !strings.Contains(e, "DIFFERENT URL") || !strings.Contains(e, "harnessator source forget tools") {
		t.Errorf("add-time note missing (want DIFFERENT URL + forget ramp):\n%s", e)
	}

	// 3. forget+setup makes B the owner...
	if code, _, e := runCLI(t, "", "source", "forget", "tools", "--config", cfgB); code != 0 {
		t.Fatalf("source forget: %s", e)
	}
	if code, _, e := runCLI(t, "", "setup", "--config", cfgB); code != 0 {
		t.Fatalf("setup B after forget: %s", e)
	}
	// ...and A now refuses under the same guard, teaching the rename ramp.
	if code, out, e := runCLI(t, "", "setup", "--config", cfgA); code == 0 || !strings.Contains(out+e, "two repositories") {
		t.Fatalf("setup A should refuse (B owns the name), code %d:\n%s%s", code, out, e)
	}

	// 4. recovery a): rename A's source, both projects coexist.
	if code, _, e := runCLI(t, "", "source", "remove", "tools", "--config", cfgA); code != 0 {
		t.Fatalf("source remove A: %s", e)
	}
	if code, _, e := runCLI(t, "", "source", "add", urlA, "--name", "tools-a", "--config", cfgA); code != 0 {
		t.Fatalf("source add renamed: %s", e)
	}
	if code, _, e := runCLI(t, "", "setup", "--config", cfgA); code != 0 {
		t.Fatalf("setup A renamed: %s", e)
	}
	// check (offline gate) passes for both owners now.
	for _, cfg := range []string{cfgA, cfgB} {
		if code, out, e := runCLI(t, "", "check", "--json", "--config", cfg); code != 0 {
			t.Fatalf("check %s: %d %s%s", cfg, code, out, e)
		}
	}

	// 5. forget on an unknown name is actionable, not a shrug.
	if code, _, e := runCLI(t, "", "source", "forget", "nosuch", "--config", cfgA); code == 0 || !strings.Contains(e, "nothing in the store") {
		t.Errorf("want actionable forget miss, code %d: %s", code, e)
	}
}
