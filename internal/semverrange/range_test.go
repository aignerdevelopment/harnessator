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

package semverrange

import (
	"reflect"
	"testing"
)

func TestParseKind(t *testing.T) {
	tests := []struct {
		raw  string
		kind Kind
	}{
		{"*", KindAny},
		{"any", KindAny},
		{"~v1.2", KindRange},
		{"~1.2.3", KindRange},
		{"^1.2.3", KindRange},
		{">=1.2 <2.0.0", KindRange},
		{">=1.2, <2", KindRange},
		{"1.2.3", KindRange},
		{"v1.2", KindRange},
		{"main", KindBranch},
		{"feature/foo", KindBranch},
		{"v2-migration", KindBranch}, // bare versionish with prerelease-ish tail -> branch
		{"dev", KindBranch},
	}
	for _, tt := range tests {
		r, err := Parse(tt.raw)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error %v", tt.raw, err)
			continue
		}
		if r.Kind() != tt.kind {
			t.Errorf("Parse(%q).Kind() = %v, want %v", tt.raw, r.Kind(), tt.kind)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, raw := range []string{"", "   ", ">=x", "~1.x", "^", ">=1.2.3.4"} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("Parse(%q): expected error, got nil", raw)
		}
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		ref   string
		tags  []string
		match []string
	}{
		{"~1.2", []string{"v1.1.0", "v1.2.0", "v1.2.7", "v1.3.0", "1.2.9"},
			[]string{"", "v1.2.0", "v1.2.7", "", "1.2.9"}},
		{"~1", []string{"v1.0.0", "v1.9.9", "v2.0.0"}, []string{"v1.0.0", "v1.9.9", ""}},
		{"^1.2.3", []string{"v1.2.3", "v1.9.0", "v2.0.0", "v1.2.2"},
			[]string{"v1.2.3", "v1.9.0", "", ""}},
		{"^0.2.3", []string{"v0.2.3", "v0.2.9", "v0.3.0"}, []string{"v0.2.3", "v0.2.9", ""}},
		{"^0.0.3", []string{"v0.0.3", "v0.0.4"}, []string{"v0.0.3", ""}},
		{">=1.2 <2.0.0", []string{"v1.2.0", "v1.9.9", "v2.0.0", "v0.9.0"},
			[]string{"v1.2.0", "v1.9.9", "", ""}},
		{"=1.2.3", []string{"v1.2.3", "v1.2.4"}, []string{"v1.2.3", ""}},
		{"1.2.3", []string{"v1.2.3", "1.2.3", "v1.2.4"}, []string{"v1.2.3", "1.2.3", ""}},
		{"*", []string{"v0.1.0", "v9.9.9", "v1.0.0-beta.1", "not-a-version"},
			[]string{"v0.1.0", "v9.9.9", "", ""}},
		{"main", []string{"v1.0.0"}, []string{""}},
	}
	for _, tt := range tests {
		r, err := Parse(tt.ref)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.ref, err)
		}
		var got []string
		for _, tag := range tt.tags {
			if r.Match(tag) {
				got = append(got, tag)
			} else {
				got = append(got, "")
			}
		}
		if !reflect.DeepEqual(got, tt.match) {
			t.Errorf("ref %q: Match results %v, want %v", tt.ref, got, tt.match)
		}
	}
}

func TestLatest(t *testing.T) {
	r, err := Parse("~1.2")
	if err != nil {
		t.Fatal(err)
	}
	tags := []string{"v1.2.0", "v1.2.10", "v1.2.9", "v1.3.0", "v1.1.0", "v1.2.11-beta.1"}
	got, ok := r.Latest(tags)
	if !ok || got != "v1.2.10" {
		t.Errorf("Latest(%v) = %q, %v; want v1.2.10, true", tags, got, ok)
	}
	if _, ok := r.Latest([]string{"v2.0.0"}); ok {
		t.Error("Latest on empty range matched unrelated tag")
	}
	b, _ := Parse("main")
	if _, ok := b.Latest([]string{"v1.0.0"}); ok {
		t.Error("branch range must not match tags")
	}
	a, _ := Parse("*")
	if got, ok := a.Latest([]string{"v0.1.0", "v2.0.0"}); !ok || got != "v2.0.0" {
		t.Errorf("any.Latest = %q %v", got, ok)
	}
}
