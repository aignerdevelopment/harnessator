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

// Package platform isolates the operating-system differences in how
// harnessator materializes store checkouts into projects: symlink on
// POSIX; junction for directories plus copy for files on Windows (where
// symlinks need privileges).
//
// Cross-OS support (linux + mac + windows) is a day-one contract, not a
// port: both link strategies are implemented, and Windows junctions are
// additionally guarded by per-OS digest verification via
// `harnessator verify`. Real Windows integration testing happens on a
// CI runner when one exists.
package platform

import "fmt"

// ErrNotSupported remains available for future partial adapters.
var ErrNotSupported = fmt.Errorf("not supported on this platform")

// Link connects src into dst as the given kind. Callers treat failures as
// materialization errors: harnessator never copies where it promised a
// live link, and never links where the source is missing.
func Link(src, dst string, dir bool) error {
	return link(src, dst, dir)
}

// Capability describes what this build can do, for "harnessator doctor".
type Capability struct {
	SymlinkDirs  bool
	SymlinkFiles bool
	JunctionDirs bool
	CopyFiles    bool
}

// Cap reports the active platform's link capabilities.
func Cap() Capability { return cap() }
