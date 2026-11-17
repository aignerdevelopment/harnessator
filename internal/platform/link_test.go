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

package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.md")
	dst := filepath.Join(dir, "dst.md")
	if err := os.WriteFile(src, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Link(src, dst, false)
	switch runtime.GOOS {
	case "windows":
		if err != nil {
			t.Fatalf("windows file copy failed: %v", err)
		}
		fi, err := os.Lstat(dst)
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("expected a regular copied file, got %v %v", fi, err)
		}
	default:
		if err != nil {
			t.Fatalf("symlink failed: %v", err)
		}
		target, err := os.Readlink(dst)
		if err != nil || target != src {
			t.Fatalf("expected symlink to %s, got %s (%v)", src, target, err)
		}
	}

	if c := Cap(); runtime.GOOS != "windows" && !(c.SymlinkFiles && c.SymlinkDirs) {
		t.Errorf("unix capability wrong: %+v", c)
	}
}
