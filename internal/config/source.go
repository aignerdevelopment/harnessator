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

// Package config reads and validates harnessator's human-edited TOML files:
// the source manifest ("kind = source", lives in a component repository) and
// the project configuration ("kind = project", lives in a consuming project).
//
// The single filename harnessator.toml serves both roles; the explicit kind
// field decides the schema (and Load refuses ambiguous or missing kinds), so
// mistakes fail loudly instead of silently reinterpreting a file.
package config

// SourceManifest is a component repository's harnessator.toml. It declares
// the components the source offers, optional child sources (the corporate
// source graph) and named profiles that pre-select components.
type SourceManifest struct {
	Kind     string             `toml:"kind"`
	Schema   int                `toml:"schema"`
	Package  Package            `toml:"package"`
	Sources  []Source           `toml:"source"`
	Profiles map[string]Profile `toml:"profiles"`

	// Components is keyed by category, then by component id.
	Components map[string]map[string]Component `toml:"component"`
}

// Package carries identity metadata for the source itself.
type Package struct {
	Name        string `toml:"name"`
	Description string `toml:"description,omitempty"`
	Version     string `toml:"version,omitempty"` // informational only; releases are git tags
	License     string `toml:"license,omitempty"`
	Homepage    string `toml:"homepage,omitempty"`
}

// Source is one git source reference, used in both project configs and the
// transitive source graph of a source manifest.
// Source is one git source reference, used in both project configs and the
// transitive source graph of a source manifest. Identity is the triple
// (git, ref, manifest): one repository may host several independent
// sources in sub-directories (multi-source repos).
type Source struct {
	Name  string `toml:"name"`
	Git   string `toml:"git"`
	Ref   string `toml:"ref"`             // semver tag range or branch name
	Layer string `toml:"layer,omitempty"` // corporate|team|project|personal (default project)
	// Manifest selects a sub-directory manifest within the repo: a
	// directory containing harnessator.toml ("source/harnessator") or the
	// file path itself. Empty = harnessator.toml at the checkout root.
	Manifest string `toml:"manifest,omitempty"`
}

// Profile is a named selection: category -> list of component ids. "*"
// selects everything in the category (refined by project-side excludes).
type Profile map[string][]string

// Component is one addressable unit of agent configuration inside a source.
type Component struct {
	Path        string   `toml:"path"`
	Key         string   `toml:"key"` // selects one entry inside a shared file (mcp/servers.json)
	Description string   `toml:"description"`
	Tags        []string `toml:"tags"`

	// Targets limits the harnesses this component renders to; empty = all.
	Targets []string `toml:"targets"`
	// Requires lists harness features the component needs (capability
	// matrix input for loud skips), e.g. "hooks", "mcp".
	Requires []string `toml:"requires"`
	// Capabilities declares what code inside the component may do at
	// runtime, from the closed set in internal/schema.
	Capabilities []string `toml:"capabilities"`

	// Always marks rules that no selection may drop (rule-00 pattern).
	Always bool `toml:"always"`

	// Origin records where a harvested component came from
	// ("harnessator harvest" provenance; set by harvest, not by hand).
	Origin string `toml:"origin,omitempty"`

	// Hint holds per-target adapter data, e.g.
	// [component.rule.foo.hint.cursor] globs = ["**/*.go"].
	Hint map[string]map[string]any `toml:"hint"`
}
