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

// kickstartCmd leaves HARNESSATOR-KICKSTART.md beside the fossils: a
// self-contained agent brief (live findings + mission + the tool's
// gate). A prompt on stderr scrolls away; a file says "work through me".
func kickstartCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("kickstart", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "regenerate an existing HARNESSATOR-KICKSTART.md")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	file, wrote, err := harvest.WriteKickstart(fs.Args(), *force)
	if err != nil {
		fmt.Fprintln(stderr, "kickstart:", err)
		return 1
	}
	if !wrote {
		fmt.Fprintf(stdout, "kickstart: %s already exists - left untouched (--force to regenerate)\n", file)
		return 0
	}
	fmt.Fprintf(stdout, "kickstart: wrote %s\n", file)
	fmt.Fprintln(stdout, "  hand it to any agent: \"Work through HARNESSATOR-KICKSTART.md - report before changing anything.\"")
	return 0
}
