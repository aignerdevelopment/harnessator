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
	"fmt"
	"io"
	"os"
	"strings"

	"harnessator.org/harnessator/internal/provider"
)

// providerModelUpdateCmd asks the gateways in each harness's OWN
// config what they actually serve, then refreshes the harness configs
// to match: added, removed (no longer supported) and kept models are
// reported per provider; opencode gets the write-back (with backup),
// harnesses without a writable catalog come back report-only, and
// harnessator-managed artifacts are refused outright. Keys are used
// for the probe and never echoed.
func providerModelUpdateCmd(stdout, stderr io.Writer, args []string, dry, all, allowRemovals, verbose bool) int {
	if all {
		args = []string{"*"}
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, `there is no "current" harness (a project targets many) - name one, list several, or:
  harnessator provider modelupdate "*"     (quoted)
  harnessator provider modelupdate --all   (shell-proof)`)
		return 2
	}
	if code, stop := validateHarnessTokens(stderr, "provider modelupdate", args); stop {
		return code
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cwd, _ := os.Getwd()
	res := provider.ModelUpdate(context.Background(), expandHarnessTokens(args), home, cwd,
		provider.MUOptions{Dry: dry, AllowRemovals: allowRemovals})

	var applied, report, errs int
	for _, ch := range res {
		label := ch.Harness
		if ch.Provider != "" {
			label += "/" + ch.Provider
		}
		if ch.Style != "" {
			fmt.Fprintf(stdout, "%s: %s %s (key: %s)\n", label, ch.Style, ch.BaseURL, ch.KeySource)
		} else if ch.Note != "" && ch.Provider == "" && ch.Err == "" {
			fmt.Fprintf(stdout, "%s: %s\n", label, ch.Note)
			report++
			continue
		}
		if ch.Err != "" {
			fmt.Fprintf(stderr, "FAIL  %s: %s\n", label, ch.Err)
			errs++
			continue
		}
		if len(ch.Added) == 0 && len(ch.Removed) == 0 && !ch.Drilled && ch.Note != "" && len(ch.Kept) == 0 {
			fmt.Fprintf(stdout, "        %s\n", ch.Note)
			continue
		}
		if len(ch.Added) > 0 {
			fmt.Fprintf(stdout, "  + %d: %s\n", len(ch.Added), listMaybe(ch.Added, verbose))
		}
		if len(ch.Removed) > 0 {
			fmt.Fprintf(stdout, "  - %d (no longer served): %s\n", len(ch.Removed), listMaybe(ch.Removed, verbose))
		}
		if len(ch.Kept) > 0 && !ch.Drilled {
			fmt.Fprintf(stdout, "  = %d unchanged\n", len(ch.Kept))
		}
		if ch.Drilled {
			if ch.Note != "" {
				fmt.Fprintf(stdout, "  %s\n", ch.Note)
			}
			if len(ch.Kept) > 0 {
				fmt.Fprintf(stdout, "  reachable: %s\n", strings.Join(ch.Kept, ", "))
			}
			report++
			continue
		}
		if ch.Note != "" && !ch.Drilled {
			fmt.Fprintf(stdout, "        %s\n", ch.Note)
		}
		if ch.Applied {
			fmt.Fprintf(stdout, "  APPLIED (backup: %s; comments in the source file were not preserved - diff it)\n", ch.Backup)
			applied++
		} else if dry && (len(ch.Added) > 0 || len(ch.Removed) > 0) {
			fmt.Fprintln(stdout, "  DRY RUN - nothing written")
		}
	}
	fmt.Fprintf(stdout, "\nmodelupdate: %d configured, %d report-only, %d error(s)\n", applied, report, errs)
	if applied > 0 {
		fmt.Fprintln(stdout, "keys were used for the probe only, never printed; govern the same truth via provider import + setup when you want it in the lock")
	}
	if errs > 0 {
		return 1
	}
	return 0
}

// listMaybe caps wall-of-output diffs: 10 names unless --verbose.
// (The first real-gateway run of modelupdate proposed printing 467
// removals on one line - the diff is a summary, the list is opt-in.)
func listMaybe(ids []string, verbose bool) string {
	const cap = 10
	if verbose || len(ids) <= cap {
		return strings.Join(ids, ", ")
	}
	return fmt.Sprintf("%s, ... %d more (--verbose for all)", strings.Join(ids[:cap], ", "), len(ids)-cap)
}
