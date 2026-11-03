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

// Package provider parses provider profile components (endpoint + approved
// model catalog) — the canonical half of governance. Rendering into each
// harness's native shape lives in internal/dialect. Provider files
// reference env var NAMES only - never secret values.
//
// Format v1 (providers/<name>.toml inside a source), redesigned 2026-10-19
// (breaking change made before publication): endpoints are keyed by API
// *style* - protocol family - not by harness product. A gateway like
// LiteLLM legitimately exposes several families at different base URLs,
// and style is what actually changes; product keys churned.
//
//	name = "litellm-acme"
//	auth_env = "LITELLM_API_KEY"            # fallback NAME for all styles
//	default_model = "big"
//	[styles.openai-compatible]
//	base_url = "https://gw.acme/v1"
//	[styles.anthropic-compatible]
//	base_url = "https://gw.acme/anthropic"
//	auth_env = "ANTHROPIC_KEY"              # optional per-style override
//	[models]
//	big = "claude-sonnet-5"                 # alias -> real model id
package provider

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// API styles (protocol families). A new gateway speaks one of these; a
// genuinely new family means one constant here plus the dialects that can
// speak it (see internal/dialect).
const (
	StyleOpenAICompat    = "openai-compatible"
	StyleAnthropicCompat = "anthropic-compatible"
	StyleOpenAIResponses = "openai-responses"
)

// KnownStyles is the accepted vocabulary for [styles.*] table names.
var KnownStyles = []string{StyleOpenAICompat, StyleAnthropicCompat, StyleOpenAIResponses}

func knownStyle(s string) bool {
	for _, k := range KnownStyles {
		if k == s {
			return true
		}
	}
	return false
}

// Endpoint is one API family's connection block on this gateway.
type Endpoint struct {
	BaseURL string `toml:"base_url"`
	AuthEnv string `toml:"auth_env"` // optional override of the profile fallback
}

// Profile is a parsed provider component file.
type Profile struct {
	Name         string              `toml:"name"`
	AuthEnv      string              `toml:"auth_env"` // fallback NAME for all styles
	DefaultModel string              `toml:"default_model"`
	Styles       map[string]Endpoint `toml:"styles"` // API family -> endpoint
	Models       map[string]string   `toml:"models"` // alias -> real id
}

// Style returns the endpoint of one API family with the profile-level
// auth_env fallback applied, and whether the profile exposes the family.
func (p *Profile) Style(name string) (Endpoint, bool) {
	ep, ok := p.Styles[name]
	if !ok {
		return Endpoint{}, false
	}
	if ep.AuthEnv == "" {
		ep.AuthEnv = p.AuthEnv
	}
	return ep, true
}

// StylesSpoken picks, in the target dialect's priority order, the first
// style the profile exposes - the endpoint-selection step every dialect
// performs before rendering.
func (p *Profile) StylesSpoken(priority []string) (string, Endpoint, bool) {
	for _, s := range priority {
		if ep, ok := p.Style(s); ok {
			return s, ep, true
		}
	}
	return "", Endpoint{}, false
}

// Parse loads a provider profile file.
func Parse(path string) (*Profile, error) {
	var p Profile
	md, err := toml.DecodeFile(path, &p)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		// map-valued fields ([models], [styles.*]) would otherwise silently
		// absorb misplaced scalars - same unknown-key trap the project loader
		// guards (see config TestProjectRoundTrip). Pre-v1 product-keyed
		// blocks ([opencode]/[claude]/[codex]) land here on purpose.
		return nil, fmt.Errorf("%s: unknown or misplaced keys: %v (scalars must precede tables)", path, und)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("%s: name required", path)
	}
	if len(p.Styles) == 0 {
		return nil, fmt.Errorf("%s: at least one [styles.<family>] endpoint required (known: %s)",
			path, strings.Join(KnownStyles, ", "))
	}
	var bad []string
	for _, s := range sortedKeys(p.Styles) {
		if !knownStyle(s) {
			bad = append(bad, s)
			continue
		}
		if p.Styles[s].BaseURL == "" {
			return nil, fmt.Errorf("%s: styles.%s requires base_url", path, s)
		}
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("%s: unknown api styles %v (known: %s)", path, bad, strings.Join(KnownStyles, ", "))
	}
	if p.DefaultModel != "" {
		if _, ok := p.Models[p.DefaultModel]; !ok && !containsValue(p.Models, p.DefaultModel) {
			return nil, fmt.Errorf("%s: default_model %q is neither an alias nor a model id of the catalog", path, p.DefaultModel)
		}
	}
	return &p, nil
}

func sortedKeys(m map[string]Endpoint) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func containsValue(m map[string]string, v string) bool {
	for _, x := range m {
		if x == v {
			return true
		}
	}
	return false
}

// Resolve maps an alias or concrete id to the real model id.
func (p *Profile) Resolve(aliasOrID string) (string, bool) {
	if real, ok := p.Models[aliasOrID]; ok {
		return real, true
	}
	if containsValue(p.Models, aliasOrID) {
		return aliasOrID, true
	}
	return "", false
}

// SortedAliasList is deterministic for rendering and errors.
func (p *Profile) SortedAliasList() []string {
	var out []string
	for k := range p.Models {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ModelList renders "alias=id" pairs sorted by alias (policy validation errors).
func (p *Profile) ModelList() string {
	var parts []string
	for _, a := range p.SortedAliasList() {
		parts = append(parts, a+"="+p.Models[a])
	}
	return strings.Join(parts, ", ")
}
