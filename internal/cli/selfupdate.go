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
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"harnessator.org/harnessator/internal/selfupdate"
)

// selfupdateCmd: the scripted form of "re-run the installer" - check for
// a newer release and swap the binary in place (checksum-verified,
// smoke-tested, old binary kept as *.old). --check-only answers with an
// exit code too: 0 = up to date, 1 = update available or unknown.
// Deliberately NOT called "update": that is the lockfile command.
func selfupdateCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("selfupdate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	checkOnly := fs.Bool("check-only", false, "only report; exit 0 up to date, 1 update available")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "usage: harnessator selfupdate [--check-only]")
		return 2
	}
	repo := os.Getenv("HARNESSATOR_REPO")
	if repo == "" {
		repo = selfupdate.DefaultRepo
	}
	ctx := context.Background()
	c := selfupdate.Default()

	latest, err := c.Latest(ctx, repo)
	if err != nil {
		fmt.Fprintf(stderr, "selfupdate: cannot determine the latest release: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "harnessator selfupdate\n  current  %s\n  latest   %s\n", Version, latest)

	if Version == "dev" || Version == "" {
		fmt.Fprintln(stdout, "  result   development build - self-update refuses (this binary is not release-stamped)")
		fmt.Fprintln(stdout, "  fix      install a release: curl -fsSL https://raw.githubusercontent.com/"+repo+"/main/install.sh | bash")
		return 1
	}
	newer, err := selfupdate.Newer(Version, latest)
	if err != nil {
		fmt.Fprintf(stdout, "  result   cannot compare %q with %q: %v\n", Version, latest, err)
		return 1
	}
	if !newer {
		fmt.Fprintln(stdout, "  result   up to date")
		return 0
	}
	if *checkOnly {
		fmt.Fprintf(stdout, "  result   UPDATE AVAILABLE %s -> %s (run: harnessator selfupdate)\n", Version, latest)
		return 1
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "selfupdate: cannot locate my own binary: %v\n", err)
		return 1
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	fmt.Fprintf(stdout, "  fetching %s\n", latest)
	res, err := c.Update(ctx, repo, latest, exe)
	if err != nil {
		fmt.Fprintf(stderr, "selfupdate: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "  updated  %s -> %s\n", Version, res.To)
	fmt.Fprintf(stdout, "  binary   %s\n  previous kept at %s (delete when satisfied)\n", res.Target, res.Backup)
	fmt.Fprintln(stdout, "  note     re-run 'harnessator version' and restart any harness wrappers")
	return 0
}
