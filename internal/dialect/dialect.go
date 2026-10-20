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

// Package dialect renders governance - the canonical provider.Profile plus
// the effective model policy - into one harness's native representation.
//
// This is the governance seam: profiles expose endpoints keyed by
// API style (protocol family), each governance-capable target registers a
// Dialect declaring which styles it speaks in what priority, and
// harness-specific conventions (opencode npm packages, claude tier-alias
// env vars, codex wire_api) are data inside the dialects, not control
// flow in the adapters. Capability = registration: a target with no
// dialect is a loud skip in materialize.
//
// Dialect input and output are pure data structs so that extracting them
// into out-of-process plugins later (the trigger being the first
// third-party adapter that needs out-of-band releases) is a transport
// wrapper, not a redesign.
package dialect

import (
	"errors"
	"sort"

	"harnessator.org/harnessator/internal/policy"
	"harnessator.org/harnessator/internal/provider"
)

// ErrNoStyle reports a profile that exposes no API style this harness can
// speak - adapters record it as a loud skip, never a silent drop.
var ErrNoStyle = errors.New("provider exposes no API style this harness speaks")

// Result carries the rendered governance fragments. Each dialect fills
// exactly the field its harness embeds; adapters merge it in.
type Result struct {
	JSON map[string]any // fragment merged into the generated JSON config (opencode)
	Env  map[string]any // settings env block (claude)
	TOML string         // managed TOML stanza text (codex)
}

// Dialect renders governance for one harness target.
type Dialect interface {
	Target() string
	// Styles lists the supported API styles in target priority order;
	// the first style the profile exposes wins.
	Styles() []string
	Govern(p *provider.Profile, eff *policy.Effective) (Result, error)
}

var registry = map[string]Dialect{}

func register(d Dialect) { registry[d.Target()] = d }

func init() {
	register(opencodeDialect{})
	register(claudeDialect{})
	register(codexDialect{})
}

// For looks up the dialect of a target (capability = registration).
func For(target string) (Dialect, bool) {
	d, ok := registry[target]
	return d, ok
}

// Targets lists governance-capable targets, sorted (skip messages, docs).
func Targets() []string {
	var out []string
	for t := range registry {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// allowedModels returns the concrete ids to render: policy-filtered when
// a policy is in effect, else the whole catalog - deterministic order by
// alias either way.
func allowedModels(p *provider.Profile, eff *policy.Effective) []string {
	if eff == nil {
		var all []string
		for _, a := range p.SortedAliasList() {
			all = append(all, p.Models[a])
		}
		return all
	}
	return eff.AllowedModels(p)
}

func defaultReal(p *provider.Profile) string {
	if p.DefaultModel == "" {
		return ""
	}
	if r, ok := p.Resolve(p.DefaultModel); ok {
		return r
	}
	return p.DefaultModel
}
