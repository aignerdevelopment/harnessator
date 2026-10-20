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

package index

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/gitops"
)

func fixtureManifests(t *testing.T) map[string]*config.SourceManifest {
	t.Helper()
	m, err := config.LoadSourceManifest(filepath.Join("..", "..", "testdata",
		"sources", "agentictools", "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]*config.SourceManifest{"agentictools": m}
}

func TestBuildDeterministic(t *testing.T) {
	pins := map[string]*gitops.Pinned{"agentictools": {
		Name: "agentictools", Git: "file:///x", Range: "main", Resolved: "main",
		SHA: "abcdef0123456789",
	}}
	a := Build(fixtureManifests(t), pins)
	b := Build(fixtureManifests(t), pins)
	if len(a) == 0 {
		t.Fatal("no entries - fixture has components")
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			t.Fatalf("nondeterministic at %d: %+v vs %+v", i, a[i], b[i])
		}
		if a[i].SHA != pins["agentictools"].SHA || a[i].ID == "" {
			t.Fatalf("provenance missing: %+v", a[i])
		}
	}
}

func TestMergeAndQuery(t *testing.T) {
	fresh := []Entry{{Source: "corp", ID: "corp:rule/a", Category: "rule", Name: "a", Description: "alpha thing"}}
	base := &File{Entries: []Entry{
		{Source: "corp", ID: "corp:rule/old", Category: "rule", Name: "old"},
		{Source: "team", ID: "team:skill/keep", Category: "skill", Name: "keep"},
	}}
	m := Merge(base, []string{"corp"}, fresh)
	if len(m.Entries) != 2 {
		t.Fatalf("merge kept stale corp entry or dropped team: %+v", m.Entries)
	}
	if _, ok := Find(m, "team:skill/keep"); !ok {
		t.Error("team entry must survive rebuild of corp")
	}
	if hits := Query(m, "alpha", ""); len(hits) != 1 || hits[0].ID != "corp:rule/a" {
		t.Errorf("text query: %+v", hits)
	}
	if hits := Query(m, "", "skill"); len(hits) != 1 || hits[0].Source != "team" {
		t.Errorf("category query: %+v", hits)
	}
	if _, ok := Find(m, "rule/a"); !ok {
		t.Error("bare cat/id must resolve when unambiguous")
	}
}

func TestWriteLoadRoundtrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "index.json")
	f := &File{Entries: []Entry{{Source: "s", ID: "s:rule/a", Category: "rule", Name: "a"}}}
	f.Built = "now"
	if err := Write(p, f); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Schema != Schema || got.Built != "now" || len(got.Entries) != 1 {
		t.Fatalf("roundtrip: %+v", got)
	}
	// missing file = empty, no error
	if e, err := Load(filepath.Join(t.TempDir(), "none.json")); err != nil || len(e.Entries) != 0 {
		t.Errorf("missing file: %+v %v", e, err)
	}
	// future schema refuses loudly
	fut := filepath.Join(t.TempDir(), "future.json")
	os.WriteFile(fut, []byte(`{"schema":99,"entries":[]}`), 0o644)
	if _, err := Load(fut); err == nil {
		t.Error("future schema must error")
	}
}
