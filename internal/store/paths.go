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

// Package store defines the machine-local layout under ~/.harnessator:
// pinned source checkouts, dev overlays and the optional local personal
// layer. The store is per-user; projects keep only committed config +
// lock + generated .harnessator/ artifacts.
package store

import (
	"os"
	"path/filepath"
)

// EnvHome lets tests and power users relocate the store.
const EnvHome = "HARNESSATOR_HOME"

// Store is a rooted store directory.
type Store struct{ Root string }

// Default locates the store: $HARNESSATOR_HOME, else ~/.harnessator.
func Default() (Store, error) {
	if h := os.Getenv(EnvHome); h != "" {
		return Store{Root: filepath.Clean(h)}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Store{}, err
	}
	return Store{Root: filepath.Join(home, ".harnessator")}, nil
}

// SourceDir holds the clone + pinned checkouts of one source.
func (s Store) SourceDir(name string) string {
	return filepath.Join(s.Root, "sources", name)
}

// CheckoutDir is the read-only install-mode checkout at an exact commit,
// the symlink target for normal installs.
func (s Store) CheckoutDir(name, sha string) string {
	return filepath.Join(s.SourceDir(name), "checkouts", sha)
}

// DevDir is the per-user dev overlay (git worktree on a branch) created by
// "harnessator dev <id>".
func (s Store) DevDir(name string) string {
	return filepath.Join(s.Root, "dev", name)
}

// PersonalFile is the fallback personal layer when no personal git source
// is configured.
func (s Store) PersonalFile() string {
	return filepath.Join(s.Root, "personal.toml")
}

// CacheDir is for indexes and other rebuildable data (search, templates).
func (s Store) CacheDir() string {
	return filepath.Join(s.Root, "cache")
}

// IndexFile is the store-level registry index (`harnessator index`).
func (s Store) IndexFile() string {
	return filepath.Join(s.CacheDir(), "index.json")
}
