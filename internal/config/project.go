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

// ProjectConfig is a consuming project's harnessator.toml. It lists the
// sources the project pulls from, the harness targets, template/profile
// selections and include/exclude refinements.
//
// Field order is deliberate: TOML top-level scalars (kind..personal) MUST
// precede the [[source]] array tables, or an encoded file would be one the
// loader rejects; table-valued fields follow. Keep this order when editing
// (config.WriteProject round-trip guarantee, see emit_test.go).
type ProjectConfig struct {
	Kind     string `toml:"kind"`
	Schema   int    `toml:"schema"`
	Personal string `toml:"personal,omitempty"`

	// prov records which scope layer introduced each merged entry
	// (see layers.go). In-memory only: unexported, so the canonical TOML
	// writer never serializes it and layer files stay round-trip clean.
	prov map[string]string

	// Targets are the harnesses to materialize for, e.g. ["opencode", "claude"].
	Targets []string `toml:"targets,omitempty"`

	// Templates are profile references, "<source>:<profile>", expanded in
	// order during resolution.
	Templates []string `toml:"templates,omitempty"`

	Sources []Source `toml:"source,omitempty"`

	// Select refines the expanded selection per category. Entries are
	// component references: "<source>:<category>/<id>" or bare "<id>"
	// (resolved against each source). "*" selects all of the category.
	Select map[string]Selection `toml:"select,omitempty"`

	// Provider names the provider profile component to render, as a
	// reference "<source>:<category>/<id>".
	Provider *ProviderConfig `toml:"provider,omitempty"`

	// Policy names the model/policy components that frame what is allowed.
	Policy *PolicyConfig `toml:"policy,omitempty"`

	// Corporate carries governance flags; only honored for sources marked
	// as corporate layer (checked by policy + CI, not client-side).
	Corporate *CorporateConfig `toml:"corporate,omitempty"`
}

// Selection is an include/exclude pair. Includes are additive over expanded
// profiles; "*" means "everything in this category". Excludes are applied
// last and may not drop components marked always.
type Selection struct {
	Include []string `toml:"include"`
	Exclude []string `toml:"exclude"`
}

// ProviderConfig selects the endpoint/model provider profile.
type ProviderConfig struct {
	Profile string `toml:"profile"`
}

// PolicyConfig selects policy components (strict by default, overrides must
// be explicit components so provenance stays queryable).
type PolicyConfig struct {
	Profiles []string `toml:"profiles"`
}

// CorporateConfig encodes the allowlist governance shape. Enforcement is
// policy + CI: the tool warns, harnessator verify/check fail in CI.
type CorporateConfig struct {
	// Allowlist is a list of git URL patterns (prefix match with trailing
	// "**"), e.g. "https://git.acme.corp/**".
	Allowlist []string `toml:"allowlist"`
	// LockMode is "strict" or "soft"; strict forbids sources outside the
	// allowlist, soft warns.
	LockMode string `toml:"lock-mode"`
}
