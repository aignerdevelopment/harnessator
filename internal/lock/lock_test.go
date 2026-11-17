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

package lock

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sample() *Lock {
	return &Lock{
		Sources: []Source{
			{Name: "zeta", Git: "https://example.test/z.git", Range: "~v1", Resolved: "v1.4.2", SHA: "aaa"},
			{Name: "alpha", Git: "https://example.test/a.git", Range: "main", Resolved: "main@2026-10-18", SHA: "bbb"},
		},
		Components: []Component{
			{ID: "zeta:rule/r2", Source: "zeta", SHA: "aaa", Digest: "sha256:2"},
			{ID: "alpha:rule/r1", Source: "alpha", SHA: "bbb", Digest: "sha256:1"},
		},
	}
}

func TestRoundTripDeterministic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "harnessator.lock")

	if err := sample().Write(p); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(first), Header) {
		t.Error("generated header missing")
	}

	l, err := Read(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if l.Sources[0].Name != "alpha" || l.Sources[1].Name != "zeta" {
		t.Errorf("sources not sorted by name on write: %+v", l.Sources)
	}
	if l.Components[0].ID != "alpha:rule/r1" {
		t.Errorf("components not sorted by id: %+v", l.Components)
	}

	// Writing the read-back lock again must produce identical bytes.
	if err := l.Write(p); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("lock file is not byte-stable across writes")
	}

	if s, ok := l.Source("zeta"); !ok || s.Resolved != "v1.4.2" {
		t.Errorf("Source lookup broken: %+v %v", s, ok)
	}
}

func TestReadRejectsFutureSchema(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "harnessator.lock")
	body := "schema = 99\n\n[[source]]\nname = \"a\"\ngit = \"x\"\nrange = \"main\"\nresolved = \"main\"\nsha = \"abc\"\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p); err == nil || !strings.Contains(err.Error(), "unsupported lock schema") {
		t.Fatalf("expected schema error, got %v", err)
	}
}
