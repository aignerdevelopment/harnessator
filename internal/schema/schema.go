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

// Package schema defines the vocabulary shared by every harnessator file
// format and adapter: the closed set of component categories, the known
// harness targets and the identifier syntax.
//
// These sets are deliberate: the supported MVP targets and the ordered
// expansion list. Adding a target is a documented design decision, not a
// silent code change.
package schema

import "regexp"

// SchemaVersion is the file-format version this build reads and writes.
// Config files and the lockfile carry it explicitly so future versions can
// migrate instead of guessing.
const SchemaVersion = 1

// Categories is the canonical, closed set of component categories. Listing
// order is the order tools display them in.
var Categories = []string{
	"rule", "skill", "prompt", "workflow", "agent",
	"hook", "tool", "mcp", "provider", "policy",
}

// Capabilities is the closed set of capability flags a component may declare
// for security review (network / exec / fs).
var Capabilities = []string{"network", "exec", "fs"}

// MVPTargets are the harnesses the MVP supports.
var MVPTargets = []string{"opencode", "claude", "codex", "antigravity", "pi", "cursor"}

// PlannedTargets are the expansion adapters in planned order.
var PlannedTargets = []string{"gemini", "cline", "copilot", "windsurf"}

// IsCategory reports whether name is a valid component category.
func IsCategory(name string) bool { return contains(Categories, name) }

// IsCapability reports whether name is a valid capability flag.
func IsCapability(name string) bool { return contains(Capabilities, name) }

// IsTarget reports whether name is a harness this build knows about
// (MVP or planned).
func IsTarget(name string) bool { return contains(MVPTargets, name) || contains(PlannedTargets, name) }

var idRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidID reports whether s is a bare identifier (a component id, source
// name or profile name). Qualified ids ("source:category/name") are built
// from valid parts and validated per-part.
func ValidID(s string) bool { return idRe.MatchString(s) }

// Layer names in precedence order, lowest first. Each layer is its own
// optional config file (config.LayerPaths): corporate and team are
// distribution points, personal is machine-wide, local is the
// per-project personal toolchain switch (.harnessator/local.toml).
var Layers = []string{"corporate", "team", "project", "personal", "local"}

// IsLayer reports whether name is a valid scope layer.
func IsLayer(name string) bool { return contains(Layers, name) }

func contains(set []string, s string) bool {
	for _, v := range set {
		if v == s {
			return true
		}
	}
	return false
}
