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

package contenthash

import (
	"os"
	"path/filepath"
	"testing"
)

func buildTree(t *testing.T, order []string) string {
	t.Helper()
	dir := t.TempDir()
	for _, spec := range order {
		p := filepath.Join(dir, spec)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("content of "+spec+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestTreeStableAcrossWriteOrder(t *testing.T) {
	a := buildTree(t, []string{"b.md", "sub/c.md", "a.md"})
	b := buildTree(t, []string{"sub/c.md", "a.md", "b.md"})

	da, err := Tree(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := Tree(b)
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Errorf("digest depends on write order: %s vs %s", da, db)
	}
	if len(da) != len(Prefix)+64 {
		t.Errorf("unexpected digest shape: %s", da)
	}
}

func TestTreeChangesOnEdit(t *testing.T) {
	dir := buildTree(t, []string{"a.md", "sub/b.md"})
	before, err := Tree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.md"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Tree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("edit did not change the tree digest")
	}
}

func TestFileMatchesSingleFileTree(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.md")
	body := []byte("same\n")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	fh, err := File(p)
	if err != nil {
		t.Fatal(err)
	}
	if fh != Prefix+hash(body) {
		t.Errorf("File digest %s, want %s%s", fh, Prefix, hash(body))
	}
}

func TestTreeOrFileDispatch(t *testing.T) {
	dir := buildTree(t, []string{"k.md"})
	d1, err := TreeOrFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := TreeOrFile(filepath.Join(dir, "k.md"))
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Error("dir and file digests should differ (path participates in dir digest)")
	}
}
