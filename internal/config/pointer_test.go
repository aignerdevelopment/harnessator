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

	"github.com/BurntSushi/toml"
)

const pointerToml = `kind = "source"
schema = 3

[package]
name = "sp-pointer"
description = "curated pointer to a foreign skill repo"

[content]
git = "https://example.org/foreign/skills.git"
ref = "~v1"

[discover]
skill = "skills/*"

[profiles]
[profiles.default]
skill = ["*"]
`

func writeToml(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "harnessator.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPeekContentAndPointer(t *testing.T) {
	path := writeToml(t, t.TempDir(), pointerToml)
	c, err := PeekContent(path)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.Git != "https://example.org/foreign/skills.git" || c.Ref != "~v1" {
		t.Fatalf("peek = %+v", c)
	}
	m := SourceManifest{Schema: 3, Content: &Content{Git: "https://x/y"}}
	if !m.Pointer() {
		t.Error("schema 3 manifest with content must be a pointer")
	}
	m.Content = nil
	if m.Pointer() {
		t.Error("without content no pointer")
	}
}

func TestLoadSourceManifestAtResolvesAgainstContent(t *testing.T) {
	dir := t.TempDir()
	path := writeToml(t, dir, pointerToml)

	// the manifest dir holds NO skills/ - expansion must use contentRoot
	content := filepath.Join(dir, "content")
	for _, s := range []string{"before-you-code", "ship-it-twice"} {
		d := filepath.Join(content, "skills", s)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "SKILL.md"),
			[]byte("---\ndescription: do "+s+"\n---\nbody\n"), 0o644)
	}
	m, err := LoadSourceManifestAt(path, content)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(m.Components["skill"]); got != 2 {
		t.Fatalf("registered %d skills, want 2 (against content root)", got)
	}
	if m.Components["skill"]["before-you-code"].Origin == "" {
		t.Error("discovered skill lost origin")
	}

	// plain Load must REFUSE a pointer - no silent empty interpretation
	if _, err := LoadSourceManifest(path); err == nil ||
		!strings.Contains(err.Error(), "LoadSourceManifestAt") {
		t.Errorf("want pointer refusal naming the At variant, got %v", err)
	}

	// At variant without a content root also refuses (loud, never empty)
	if _, err := LoadSourceManifestAt(path, ""); err == nil {
		t.Error("want refusal for pointer without content tree")
	}
}

func mustLoadRaw(t *testing.T, path string) *SourceManifest {
	t.Helper()
	var m SourceManifest
	if _, err := toml.DecodeFile(path, &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

func TestPointerSchemaAndContentGates(t *testing.T) {
	dir := t.TempDir()

	// schema 2 + [content] = the v1 reader would half-interpret it
	old := writeToml(t, dir, strings.Replace(pointerToml, "schema = 3", "schema = 2", 1))
	if err := ValidateSource(mustLoadRaw(t, old), ""); err == nil ||
		!strings.Contains(err.Error(), "schema = 3") {
		t.Errorf("want schema gate for [content], got %v", err)
	}

	// empty content.git points to nowhere
	broken := writeToml(t, dir, strings.Replace(pointerToml,
		`git = "https://example.org/foreign/skills.git"`, `git = ""`, 1))
	if err := ValidateSource(mustLoadRaw(t, broken), ""); err == nil ||
		!strings.Contains(err.Error(), "content.git is required") {
		t.Errorf("want content.git required, got %v", err)
	}

	// credentials in an https content URL are secret-leak shape
	cred := writeToml(t, dir, strings.Replace(pointerToml,
		`git = "https://example.org/foreign/skills.git"`,
		`git = "https://oauth:s3cret@example.org/foreign/skills.git"`, 1))
	if err := ValidateSource(mustLoadRaw(t, cred), ""); err == nil ||
		!strings.Contains(err.Error(), "credentials") {
		t.Errorf("want credential refusal, got %v", err)
	}

	// bad content ref grammar fails like any other ref
	badref := writeToml(t, dir, strings.Replace(pointerToml, `ref = "~v1"`, `ref = "&&& &&&"`, 1))
	if err := ValidateSource(mustLoadRaw(t, badref), ""); err == nil ||
		!strings.Contains(err.Error(), "content.ref") {
		t.Errorf("want content.ref parse error, got %v", err)
	}
}

func TestPointerDiscoverResolvesAgainstContentInValidate(t *testing.T) {
	dir := t.TempDir()
	path := writeToml(t, dir, pointerToml)
	content := filepath.Join(dir, "content")
	os.MkdirAll(filepath.Join(content, "skills", "solo"), 0o755)
	os.WriteFile(filepath.Join(content, "skills", "solo", "SKILL.md"),
		[]byte("---\ndescription: solo\n---\n"), 0o644)
	m, err := LoadSourceManifestAt(path, content)
	if err != nil {
		t.Fatal(err)
	}
	// explicit-path validation follows the SAME root: a component whose
	// path exists only in the manifest tree must fail.
	if m.Components["rule"] == nil {
		m.Components["rule"] = map[string]Component{}
	}
	m.Components["rule"]["ghost"] = Component{Path: "only-here.md"}
	os.WriteFile(filepath.Join(dir, "only-here.md"), []byte("x"), 0o644)
	if err := ValidateSource(m, content); err == nil ||
		!strings.Contains(err.Error(), "only-here.md") {
		t.Errorf("want content-rooted path check, got %v", err)
	}
}
