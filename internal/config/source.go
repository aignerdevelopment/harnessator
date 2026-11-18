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

	// Discover declares live-registered content families instead of
	// enumerating them: `skill = "skills/*"` registers every
	// sub-directory of skills/ that contains SKILL.md, description read
	// from its frontmatter at load time. This is what keeps third-party
	// pointers from drifting - nothing is copied, nothing rots.
	// Requires schema = 2.
	Discover map[string]string `toml:"discover"`

	// Components is keyed by category, then by component id.
	Components map[string]map[string]Component `toml:"component"`

	// Content makes this a POINTER source (source format v1.2, schema 3):
	// the manifest lives in this repository, the component bytes live in
	// ANOTHER repository. All component paths - explicit entries and
	// [discover] globs alike - then resolve against the content checkout,
	// not the manifest's own tree. Curation (selections, tags, profiles,
	// descriptions) remains yours to edit; the CONTENT behind [content] is
	// import-only by construction: you are not the author of record, and
	// no flag can make you one. Edits go upstream (PR) or into a fork you
	// own - never into the pin.
	Content *Content `toml:"content"`
}

// Content names the foreign repository a pointer source serves.
type Content struct {
	Git string `toml:"git"`
	// Ref is the content's own ref expression - the same grammar as
	// source refs (branch, tag, semver range). Defaults to "main":
	// upstream branch names are not ours to assume.
	Ref string `toml:"ref,omitempty"`
}

// Pointer reports whether this manifest serves content from another repo.
func (m *SourceManifest) Pointer() bool { return m.Content != nil && m.Content.Git != "" }

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
	// Editable declares the consumer's authority over this source's
	// content: nil/true = you may edit upstream (the dev overlay flow
	// works); false = import-only - consume as pinned, tweak via a fork
	// you own, fix for everyone via upstream PR. Pointer sources IGNORE
	// this field's true-value: their content is always somebody else's,
	// and declaring it editable is an error. Never a secret, always
	// honest: this is a declaration, not a capability probe - the tool
	// could not verify your push access anyway.
	Editable *bool `toml:"editable,omitempty"`
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
