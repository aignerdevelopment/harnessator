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

package store

import (
	"path/filepath"
	"testing"
)

func TestDefaultHonorsEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvHome, dir)
	s, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if s.Root != filepath.Clean(dir) {
		t.Errorf("Root = %q, want %q", s.Root, dir)
	}

	want := []string{
		s.SourceDir("corp"),
		s.CheckoutDir("corp", "deadbeef"),
		s.DevDir("corp"),
		s.PersonalFile(),
		s.CacheDir(),
	}
	for _, p := range want {
		rel, err := filepath.Rel(s.Root, p)
		if err != nil || rel == ".." || filepath.IsAbs(rel) {
			t.Errorf("path escapes store root: %s", p)
		}
	}
}
