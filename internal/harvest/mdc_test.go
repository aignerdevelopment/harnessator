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

package harvest

import (
	"reflect"
	"testing"
)

// Cursor globs legitimately carry commas INSIDE brace alternations; the
// lift must not split them apart (regression found bridging the
// awesome-cursor-rules-mdc collection: **/*.{jsx,tsx} became two
// broken patterns).
func TestLiftMDCBraceGlobs(t *testing.T) {
	raw := "---\ndescription: React\nglobs: **/*.{jsx,tsx}\nalwaysApply: false\n---\nbody\n"
	l := liftMDC(raw)
	if want := []string{"**/*.{jsx,tsx}"}; !reflect.DeepEqual(l.Globs, want) {
		t.Errorf("bare brace glob: got %#v want %#v", l.Globs, want)
	}

	raw = "---\ndescription: React\nglobs: [\"**/*.{jsx,tsx}\", \"**/*.ts\"]\n---\nbody\n"
	l = liftMDC(raw)
	if want := []string{"**/*.{jsx,tsx}", "**/*.ts"}; !reflect.DeepEqual(l.Globs, want) {
		t.Errorf("quoted list with brace: got %#v want %#v", l.Globs, want)
	}

	// plain comma lists must keep working unchanged
	raw = "---\nglobs: [a.md, b.md]\n---\nbody\n"
	l = liftMDC(raw)
	if want := []string{"a.md", "b.md"}; !reflect.DeepEqual(l.Globs, want) {
		t.Errorf("plain list: got %#v want %#v", l.Globs, want)
	}
}
