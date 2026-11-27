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

package cli

import (
	"fmt"
	"io"
	"sort"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/resolve"
)

// helpers shared by the command implementations

func printPlan(w io.Writer, p *resolve.Plan) {
	byCat := p.ByCategory()
	for _, c := range p.Skips {
		fmt.Fprintf(w, "  skip   %s (%s)\n", c.Choice.QualifiedID(), c.Reason)
	}
	for _, cat := range resolve.CategoriesInOrder() {
		for _, ch := range byCat[cat] {
			fmt.Fprintf(w, "  enable %-10s %s  [%s]\n", cat, ch.QualifiedID(), ch.Reason)
		}
	}
}

func mode(dry bool) string {
	if dry {
		return "dry run - nothing written"
	}
	return "applying"
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func sortedManifestNames(m map[string]*config.SourceManifest) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedCompIDs(m *config.SourceManifest, cat string) []string {
	var out []string
	for id := range m.Components[cat] {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
