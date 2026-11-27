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

package policy

import (
	"testing"

	"harnessator.org/harnessator/internal/provider"
)

func prof(t *testing.T) *provider.Profile {
	t.Helper()
	p, err := provider.Parse("../../testdata/sources/agentictools/providers/litellm-demo.toml")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func pol(t *testing.T, strict bool, allowed []string, sub string) *Policy {
	s := strict
	return &Policy{Strict: &s, Allowed: allowed, SubagentModel: sub}
}

func TestMergeLastWinsPerKey(t *testing.T) {
	base := pol(t, true, []string{"big"}, "big")
	corp := &Policy{Allowed: []string{"big", "smart"}} // strict unset -> inherits
	e := Merge([]*Policy{base, corp}, []string{"a:policy/base", "corp:policy/wider"})
	if !e.Strict {
		t.Error("strict should be inherited when later policy is silent")
	}
	if len(e.Allowed) != 2 {
		t.Errorf("later allowed must replace, not union: %v", e.Allowed)
	}
	if e.SubagentModel != "big" {
		t.Errorf("subagent inherits from earlier: %q", e.SubagentModel)
	}
	if len(e.Sources) != 2 {
		t.Errorf("provenance lost: %v", e.Sources)
	}
}

func TestStrictDefaultTrue(t *testing.T) {
	e := Merge([]*Policy{{Allowed: []string{"big"}}}, nil)
	if !e.Strict {
		t.Error("strict must default to true")
	}
}

func TestValidateAliasConcreteEquivalence(t *testing.T) {
	p := prof(t)
	// allowed spelled as the CONCRETE id while default uses the alias
	e := Merge([]*Policy{pol(t, true, []string{"claude-sonnet-5"}, "sonnet-alias-check")}, nil)
	v := e.Validate(p)
	// default big -> claude-sonnet-5 allowed ✓; subagent unknown name violates
	if len(v) != 1 || v[0] == "" {
		t.Fatalf("expected exactly the subagent violation, got %v", v)
	}

	e2 := Merge([]*Policy{pol(t, true, []string{"big"}, "")}, nil)
	if v := e2.Validate(p); len(v) != 0 {
		t.Errorf("alias spelling must validate: %v", v)
	}
	// unknown entry in allowed => violation for the entry itself; and since
	// nothing resolves from {nope}, the default_model is not allowed either
	e3 := Merge([]*Policy{pol(t, true, []string{"nope"}, "")}, nil)
	if v := e3.Validate(p); len(v) != 2 {
		t.Errorf("unknown allowed entry: %v", v)
	}
	// denied default
	e4 := Merge([]*Policy{pol(t, true, []string{"fast"}, "")}, nil)
	if v := e4.Validate(p); len(v) == 0 {
		t.Error("default_model outside allowed must violate")
	}
}

func TestAllowedModelsFiltering(t *testing.T) {
	p := prof(t)
	e := Merge([]*Policy{pol(t, true, []string{"big", "smart"}, "")}, nil)
	got := e.AllowedModels(p)
	want := []string{"claude-sonnet-5", "gpt-5.2"} // fast excluded
	if len(got) != len(want) {
		t.Fatalf("AllowedModels = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("idx %d: %q != %q", i, got[i], want[i])
		}
	}
	// no constraint => whole catalog
	open := &Effective{}
	if len(open.AllowedModels(p)) != 3 {
		t.Errorf("unconstrained must admit whole catalog")
	}
}
