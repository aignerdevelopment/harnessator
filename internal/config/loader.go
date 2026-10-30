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
