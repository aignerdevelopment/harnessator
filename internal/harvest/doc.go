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

// Package harvest implements the cold-start ramp:
// scan existing projects/directories for component content, and harvest
// it into publishable source repositories.
//
// Scan (scan.go): strictly read-only sprawl report - components by
// digest, cross-root identical/divergent clusters, masked sanitize
// findings; versioned JSON envelope designed to also feed the future
// status command / web dashboard.
//
// Harvest (harvest.go): publish candidates as a source repo (new or via
// --into). Inputs are never modified; every conflict, sanitize finding
// and invalid id aborts BEFORE any write; identical copies merge;
// divergent ones need --prefer; re-runs are idempotent; the written
// manifest self-validates.
//
// Trust rules of the whole package: symlinked entries pointing outside the
// project are recorded as linked, never harvested (managed content or
// hand-made links); harvested content is treated as data, never obeyed.
package harvest

// SchemaVersion is the report envelope version.
const SchemaVersion = 1
