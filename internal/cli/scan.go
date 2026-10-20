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
	"flag"
	"fmt"
	"io"

	"harnessator.org/harnessator/internal/harvest"
)

// scanCmd: the read half of the cold-start story:
// point it at projects/loose dirs, see what agent-configuration content is
// scattered there - including duplicated and DIVERGENT copies across trees
// and secret/path sanitize hits - read-only, JSON-stable for dashboards.
func scanCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit the versioned JSON report instead of text")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	paths := fs.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	rep, err := harvest.Scan(paths)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *asJSON {
		if err := rep.WriteJSON(stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	rep.WriteText(stdout)
	return 0
}
