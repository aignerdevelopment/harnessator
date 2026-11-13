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

import (
	"fmt"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// KindSource and KindProject are the accepted values of the kind field.
const (
	KindSource  = "source"
	KindProject = "project"
)

type kindProbe struct {
	Kind   string `toml:"kind"`
	Schema int    `toml:"schema"`
}

// Probe reads just enough of a harnessator.toml to decide which schema
// applies. Used by Load* helpers and the CLI to give precise errors.
func Probe(path string) (kind string, schema int, err error) {
	var p kindProbe
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return "", 0, fmt.Errorf("%s: %w", path, err)
	}
	switch p.Kind {
	case KindSource, KindProject:
	default:
		return "", 0, fmt.Errorf("%s: kind must be %q or %q, got %q",
			path, KindSource, KindProject, p.Kind)
	}
	return p.Kind, p.Schema, nil
}

// LoadSourceManifest decodes a component repository's harnessator.toml.
func LoadSourceManifest(path string) (*SourceManifest, error) {
	kind, _, err := Probe(path)
	if err != nil {
		return nil, err
	}
	if kind != KindSource {
		return nil, fmt.Errorf("%s: kind is %q, expected %q", path, kind, KindSource)
	}
	var m SourceManifest
	md, err := toml.DecodeFile(path, &m)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		return nil, fmt.Errorf("%s: unknown keys: %v", path, und)
	}
	// Live discovery expands at the single choke point every consumer
	// shares; the manifest directory is the root (multi-source convention)
	// - unless this is a pointer, whose content tree has no local root yet.
	if m.Content != nil {
		return nil, fmt.Errorf("%s: pointer manifest ([content]) - load with LoadSourceManifestAt(path, contentCheckoutDir)", path)
	}
	if err := m.expandDiscover(filepath.Dir(path)); err != nil {
		return nil, err
	}
	return &m, nil
}

// PeekContent decodes just enough of a manifest to find its content
// pointer, before any content checkout exists (the assembly order in the
// app: sync manifest -> peek -> fetch content -> full load).
func PeekContent(path string) (*Content, error) {
	kind, schema, err := Probe(path)
	if err != nil {
		return nil, err
	}
	if kind != KindSource {
		return nil, fmt.Errorf("%s: kind is %q, expected %q", path, kind, KindSource)
	}
	var m SourceManifest
	if _, err := toml.DecodeFile(path, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	_ = schema
	if m.Content == nil {
		return nil, nil // not a pointer (content.git emptiness is a VALIDATION failure, never a silent plain manifest)
	}
	return m.Content, nil
}

// LoadSourceManifestAt loads a manifest whose component content lives in
// contentRoot (the exported checkout of m.Content.Git). Pointer manifests
// expand [discover] - and resolve explicit paths - against that tree; the
// manifest directory holds only the toml and the curation.
func LoadSourceManifestAt(path, contentRoot string) (*SourceManifest, error) {
	kind, _, err := Probe(path)
	if err != nil {
		return nil, err
	}
	if kind != KindSource {
		return nil, fmt.Errorf("%s: kind is %q, expected %q", path, kind, KindSource)
	}
	var m SourceManifest
	md, err := toml.DecodeFile(path, &m)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		return nil, fmt.Errorf("%s: unknown keys: %v", path, und)
	}
	root := filepath.Dir(path)
	if m.Content != nil {
		if contentRoot == "" {
			return nil, fmt.Errorf("%s: pointer manifest without a content checkout - LoadSourceManifestAt needs the exported content tree", path)
		}
		root = contentRoot
	}
	if err := m.expandDiscover(root); err != nil {
		return nil, err
	}
	return &m, nil
}

// LoadProjectConfig decodes a consuming project's harnessator.toml.
func LoadProjectConfig(path string) (*ProjectConfig, error) {
	kind, _, err := Probe(path)
	if err != nil {
		return nil, err
	}
	if kind != KindProject {
		return nil, fmt.Errorf("%s: kind is %q, expected %q", path, kind, KindProject)
	}
	var c ProjectConfig
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		return nil, fmt.Errorf("%s: unknown keys: %v", path, und)
	}
	return &c, nil
}
