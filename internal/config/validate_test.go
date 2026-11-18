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

func testdata(t *testing.T, rel string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", rel))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestValidateSourceFixture(t *testing.T) {
	path := testdata(t, "sources/agentictools/harnessator.toml")
	m, err := LoadSourceManifest(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := ValidateSource(m, filepath.Dir(path)); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got := m.Components["rule"]["00-agentictools-source-of-truth"].Always; !got {
		t.Error("always flag on the fixture rule not decoded")
	}
	hint := m.Components["rule"]["10-code-style"].Hint["cursor"]
	if hint == nil {
		t.Fatal("cursor hint not decoded")
	}
	if _, ok := hint["globs"]; !ok {
		t.Errorf("cursor hint globs missing: %v", hint)
	}
}

func TestValidateProjectFixture(t *testing.T) {
	srcPath := testdata(t, "sources/agentictools/harnessator.toml")
	m, err := LoadSourceManifest(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]*SourceManifest{m.Package.Name: m}

	path := testdata(t, "projects/basic/harnessator.toml")
	c, err := LoadProjectConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := ValidateProject(c, sources); err != nil {
		t.Fatalf("validate with manifests: %v", err)
	}
	if err := ValidateProject(c, nil); err != nil {
		t.Fatalf("validate syntax-only: %v", err)
	}
}

func TestBrokenSourceErrors(t *testing.T) {
	path := testdata(t, "invalid/broken-source.toml")
	m, err := LoadSourceManifest(path)
	if err != nil {
		t.Fatalf("load should succeed structurally: %v", err)
	}
	err = ValidateSource(m, filepath.Dir(path))
	if err == nil {
		t.Fatal("expected validation errors")
	}
	got := err.Error()
	for _, want := range []string{
		"unsupported schema version 99",
		"not a valid id",         // package.name "broken!!"
		"git url is required",    // source x
		"ref", "invalid version", // source ref ">=x" -> parse error mentions ref
		"unknown layer",              // source layer
		"unknown component category", // widget
		"unknown category",           // in profiles.p
		"is not defined",             // profile rules does-not-exist
		"unknown target",             // frobnitz
		"unknown capability",         // magic
		"not found",                  // missing.md (root given)
	} {
		if !strings.Contains(got, want) {
			t.Errorf("errors missing %q; full:\n%s", want, got)
		}
	}
}

func TestBrokenProjectErrors(t *testing.T) {
	m, err := LoadSourceManifest(testdata(t, "sources/agentictools/harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]*SourceManifest{m.Package.Name: m}

	c, err := LoadProjectConfig(testdata(t, "invalid/broken-project.toml"))
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateProject(c, sources)
	if err == nil {
		t.Fatal("expected validation errors")
	}
	got := err.Error()
	for _, want := range []string{
		"at least one target",
		"no profile",
		"must look like",
		"unknown category",
		"not defined by source",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("errors missing %q; full:\n%s", want, got)
		}
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	_, err := LoadProjectConfig(testdata(t, "invalid/unknown-key.toml"))
	if err == nil || !strings.Contains(err.Error(), "unknown keys") {
		t.Fatalf("expected unknown-keys error, got %v", err)
	}
}

func TestProbeRejectsUnknownKind(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "harnessator.toml")
	if err := os.WriteFile(p, []byte("kind = \"banana\"\nschema = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Probe(p); err == nil || !strings.Contains(err.Error(), "kind must be") {
		t.Fatalf("expected kind error, got %v", err)
	}
}
