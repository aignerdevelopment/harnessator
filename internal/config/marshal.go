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
	"bytes"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// SourceHeader documents the canonical manifest; provenance of individual
// components lives in their origin field, not in comments (which a
// rewrite may drop).
const SourceHeader = "# harnessator source manifest - registers this repository's components\n" +
	"# (validate with: harnessator validate --check-paths harnessator.toml)\n"

// MarshalSource canonicalizes a source manifest (deterministic key order
// via the toml encoder; loads back via LoadSourceManifest).
func MarshalSource(m *SourceManifest) ([]byte, error) {
	m.Kind = KindSource
	if m.Schema == 0 {
		m.Schema = 1
	}
	var buf bytes.Buffer
	buf.WriteString(SourceHeader)
	// kind/schema must be scalars before any tables: encode a tiny head
	// first, then the rest.
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteSource atomically writes a manifest.
func WriteSource(path string, m *SourceManifest) error {
	raw, err := MarshalSource(m)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("%s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}
