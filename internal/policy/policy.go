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

// Package policy parses policy components (approved model catalogs,
// sub-agent pinning) and validates provider selections against them.
//
// Semantics: STRICT by default - setup rejects
// models outside the approved set - and overridable only by an explicit
// higher-layer policy component, so every relaxation stays visible in the
// plan provenance (`why`) and the lock diff.
//
// Format (policies/<name>.toml inside a source):
//
//	reason  = "corporate approved catalog"
//	strict  = true
//	allowed = ["big", "fast", "gpt-5.2"]   # aliases or concrete model ids
//	subagent_model = "big"                  # pin for harness sub-agent models
package policy

import (
	"fmt"

	"github.com/BurntSushi/toml"

	"harnessator.org/harnessator/internal/provider"
)

// Policy is one parsed policy component.
type Policy struct {
	Reason        string   `toml:"reason"`
	Strict        *bool    `toml:"strict"`
	Allowed       []string `toml:"allowed"`
	SubagentModel string   `toml:"subagent_model"`
}

// Parse loads a policy file.
func Parse(path string) (*Policy, error) {
	var p Policy
	md, err := toml.DecodeFile(path, &p)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		return nil, fmt.Errorf("%s: unknown keys: %v", path, und)
	}
	return &p, nil
}

// Effective is the merged frame applied by setup. Later policies (higher
// layers, listed later) override earlier ones per key - relaxation is
// always a named component, never an invisible default.
type Effective struct {
	Strict        bool
	Allowed       []string
	SubagentModel string
	Sources       []string // component ids, in override order (provenance)
}

// Merge applies policies left-to-right, last wins per non-empty key.
// Strict defaults to TRUE.
func Merge(ps []*Policy, ids []string) *Effective {
	e := &Effective{Strict: true}
	for i, p := range ps {
		if p.Strict != nil {
			e.Strict = *p.Strict
		}
		if len(p.Allowed) > 0 {
			e.Allowed = append([]string{}, p.Allowed...)
		}
		if p.SubagentModel != "" {
			e.SubagentModel = p.SubagentModel
		}
		if i < len(ids) {
			e.Sources = append(e.Sources, ids[i])
		}
	}
	return e
}

// Validate checks a provider profile against the effective policy and
// returns violation messages (empty = compliant). Entries in allowed may be
// aliases or concrete ids of the provider catalog; both spellings match.
func (e *Effective) Validate(p *provider.Profile) []string {
	var violations []string
	for _, a := range e.Allowed {
		if _, ok := p.Resolve(a); !ok {
			violations = append(violations,
				fmt.Sprintf("allowed model %q not in provider %s catalog (%s)",
					a, p.Name, p.ModelList()))
		}
	}
	if p.DefaultModel != "" && !e.allows(p, p.DefaultModel) {
		violations = append(violations,
			fmt.Sprintf("provider %s default_model %q is not allowed by policy %v",
				p.Name, p.DefaultModel, e.Allowed))
	}
	if e.SubagentModel != "" && !e.allows(p, e.SubagentModel) {
		violations = append(violations,
			fmt.Sprintf("subagent_model %q is not allowed by policy %v", e.SubagentModel, e.Allowed))
	}
	return violations
}

// allows reports whether aliasOrID is admitted: it matches an allowed entry
// either by the same spelling or after alias resolution in both directions.
func (e *Effective) allows(p *provider.Profile, aliasOrID string) bool {
	if len(e.Allowed) == 0 {
		return true // no catalog constraint
	}
	real, ok := p.Resolve(aliasOrID)
	for _, a := range e.Allowed {
		if a == aliasOrID {
			return true
		}
		if ok {
			if aReal, _ := p.Resolve(a); aReal == real {
				return true
			}
		}
	}
	return false
}

// AllowedModels returns the concrete model ids the policy admits from the
// provider catalog, in the provider's alias order. Empty allowed = all.
func (e *Effective) AllowedModels(p *provider.Profile) []string {
	var out []string
	for _, alias := range p.SortedAliasList() {
		real := p.Models[alias]
		if e.allows(p, alias) {
			out = append(out, real)
		}
	}
	return out
}
