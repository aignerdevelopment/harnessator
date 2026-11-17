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
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = Run(&o, &e, args)
	return code, o.String(), e.String()
}

func TestVersionAndUsage(t *testing.T) {
	if code, _, _ := run(t, "version"); code != 0 {
		t.Errorf("version exit %d", code)
	}
	if code, out, _ := run(t); code != 2 || !strings.Contains(out, "usage:") {
		t.Errorf("bare run: code %d out %q", code, out)
	}
	if code, _, e := run(t, "nonsense"); code != 2 || !strings.Contains(e, "unknown command") {
		t.Errorf("bad command: code %d err %q", code, e)
	}
}

func TestValidateFixtureSource(t *testing.T) {
	p := filepath.Join("..", "..", "testdata", "sources", "agentictools", "harnessator.toml")
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	if code, out, e := run(t, "validate", "--check-paths", abs); code != 0 {
		t.Fatalf("validate ok fixture: code %d out %q err %q", code, out, e)
	}
}

func TestValidateFixtureProject(t *testing.T) {
	p := filepath.Join("..", "..", "testdata", "projects", "basic", "harnessator.toml")
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, e := run(t, "validate", abs); code != 0 {
		t.Fatalf("validate project fixture: code %d err %q", code, e)
	}
}

func TestValidateBroken(t *testing.T) {
	p := filepath.Join("..", "..", "testdata", "invalid", "broken-source.toml")
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	code, _, e := run(t, "validate", abs)
	if code != 1 || !strings.Contains(e, "unsupported schema version") {
		t.Fatalf("validate broken: code %d err %q", code, e)
	}
}
