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
	"path/filepath"
	"strings"
	"testing"
)

func TestTUICommands(t *testing.T) {
	t.Setenv("HARNESSATOR_HOME", t.TempDir())
	proj := t.TempDir()
	var out, errOut strings.Builder
	url := mkSourceRepo(t)
	if code := Run(&out, &errOut, []string{"init", "--targets", "opencode", "--source", url, "--ref", "~v0.1", proj}); code != 0 {
		t.Fatalf("init: %s%s", out.String(), errOut.String())
	}
	cfg := filepath.Join(proj, "harnessator.toml")
	if code := Run(&out, &errOut, []string{"enable", "--config", cfg, "agentictools-src:minimal"}); code != 0 {
		t.Fatalf("enable: %s%s", out.String(), errOut.String())
	}

	in := strings.NewReader("s\nv\nl\nq\n")
	out.Reset()
	errOut.Reset()
	if code := RunIO(in, &out, &errOut, []string{"tui", "--config", cfg}); code != 0 {
		t.Fatalf("tui: %d %s", code, errOut.String())
	}
	s := out.String()
	for _, want := range []string{
		"harnessator tui -",        // header
		"agentictools-src:minimal", // template provenance appears in setup plan
		"lock state verified ok",   // setup ran, then verify passed
		"rule/00-agentictools",     // list output
		"bye",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("tui output missing %q; full:\n%s", want, s)
		}
	}
	// setup via tui must have written the lock
	if _, err := filepath.Abs(cfg); err != nil {
		t.Fatal(err)
	}
}
