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

// Package cli wires subcommands. It stays deliberately thin: every command
// is a function over the core packages, so the TUI/web/MCP frontends bind
// to the same internals - core is the product, frontends are views.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"harnessator.org/harnessator/internal/config"
)

// Version is stamped by the release build via -ldflags.
var Version = "dev"

const usage = `harnessator - configure AI harnesses from governed git sources

usage: harnessator <command> [flags] [args]

commands:
  version                     print version
  validate [--check-paths] <file>...
                              check harnessator.toml manifests/configs
  init [--targets a,b] [--source url,..] [--ref R] [--manifest path] [dir]
                              create a managed project (config+lock dirs)
  setup [--dry-run] [--no-fetch]
                              sync sources -> resolve -> materialize -> lock
  update                      alias of setup (re-resolves refs)
  verify                      CI gate: on-disk state must match the lock
  doctor                      all state problems: lock, drift, links, ignores
  list [--all]                enabled components (+ skips with --all)
  search <text>               find components across all sources
  layers                      scope-layer files in effect and what each adds
  index build | index search [--category c] [<text>] | index show <ref>
                              local-first registry over the store's pinned sources
  mcp serve                   read-only MCP server over stdio (agents querying
                              their own harness config)
  check [--json] [--quiet] [--install-hook|--remove-hook pre-commit|pre-push]
                              offline CI gate: policy + lock digests + drift
  scan [--json] <path>...     read-only: components scattered in project
                              trees (duplicates, divergence, sanitize hits)
  harvest [--name n|--into d] [--prefer newest|largest] [--dry-run] [--adopt]
                              publish scanned components as a source repo
  info <ref>                  component details incl. store path
  why <ref>                   provenance of one enablement/skip
  enable <source>:<profile> | <cat>/<id>
                              add template / select include
  disable <ref>               remove template / select exclude
  source add|remove|list      manage git sources
  source new --name n         scaffold a new component source (+remote)
  dev <source> | status | sync [src] | drop <src>
                              per-project dev overlay on a branch
  contribute [src] [--title]  push branch + open MR/PR (GitLab/Azure/GitHub)
  provider use|show|none      select the provider profile
  serve [--port p]            localhost web GUI (loopback-only)
  tui                         interactive terminal menu

The full lifecycle, six adapters, governance, harvest, the registry
index, the MCP server and the enforcement gate are implemented. Planned:
expansion adapters (gemini, cline, copilot, windsurf), full-screen TUI.
`

// parseFlags parses args with flags accepted in ANY position. Go's flag
// package stops at the first positional, but the documented interface is
// mixed-order friendly (`enable <ref> --config p` and `enable --config p
// <ref>` both), so flag tokens - with the values they consume - are
// hoisted ahead of positional args before parsing.
func parseFlags(fs *flag.FlagSet, args []string) error {
	return fs.Parse(hoistFlags(fs, args))
}

func hoistFlags(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	takesValue := func(tok string) bool {
		name := strings.TrimLeft(tok, "-")
		if strings.Contains(name, "=") {
			return false // --flag=value carries its own value
		}
		d := fs.Lookup(name)
		// bool flags are the only kind that consumes no value; Go 1.27
		// dropped Flag.NoOptDefVal from the public API, so their textual
		// defaults ("true"/"false") are the discriminator. No string flag
		// in this CLI defaults to those words (grep-enforced in tests).
		return d != nil && d.DefValue != "true" && d.DefValue != "false"
	}
	stopped := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case stopped:
			positional = append(positional, a)
		case a == "--":
			stopped = true
		case len(a) > 1 && a[0] == '-' && takesValue(a) && i+1 < len(args):
			i++
			flags = append(flags, a, args[i])
		case len(a) > 1 && a[0] == '-':
			flags = append(flags, a)
		default:
			positional = append(positional, a)
		}
	}
	if stopped {
		// keep the terminator between hoisted flags and positionals so
		// flag-lookalike paths stay positional after reordering
		flags = append(append(flags, "--"), positional...)
		return flags
	}
	return append(flags, positional...)
}

// Run executes args (without program name) and returns the exit code.
func Run(stdout, stderr io.Writer, args []string) int {
	return RunIO(os.Stdin, stdout, stderr, args)
}

// RunIO is Run with explicit stdin (testable tui).
func RunIO(stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 2
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintf(stdout, "harnessator %s\n", Version)
		return 0
	case "validate":
		return validate(stdout, stderr, args[1:])
	case "setup", "update":
		return setup(stdout, stderr, args[1:])
	case "verify":
		return verify(stdout, stderr, args[1:])
	case "doctor":
		return doctor(stdout, stderr, args[1:])
	case "list":
		return listCmd(stdout, stderr, args[1:])
	case "search":
		return searchCmd(stdout, stderr, args[1:])
	case "layers":
		return layersCmd(stdout, stderr, args[1:])
	case "index":
		return indexCmd(stdout, stderr, args[1:])
	case "mcp":
		return mcpCmd(stdout, stderr, args[1:])
	case "check":
		return checkCmd(stdout, stderr, args[1:])
	case "scan":
		return scanCmd(stdout, stderr, args[1:])
	case "harvest":
		return harvestCmd(stdout, stderr, args[1:])
	case "info":
		return infoCmd(stdout, stderr, args[1:])
	case "why":
		return whyCmd(stdout, stderr, args[1:])
	case "init":
		return initCmd(stdout, stderr, args[1:])
	case "enable":
		return enableCmd(stdout, stderr, args[1:])
	case "disable":
		return disableCmd(stdout, stderr, args[1:])
	case "source":
		if len(args) > 1 && args[1] == "new" {
			return sourceNewCmd(stdout, stderr, args[2:])
		}
		return sourceCmd(stdout, stderr, args[1:])
	case "dev":
		return devCmd(stdout, stderr, args[1:])
	case "contribute":
		return contributeCmd(stdout, stderr, args[1:])
	case "provider":
		return providerCmd(stdout, stderr, args[1:])
	case "serve":
		return serveCmd(stdout, stderr, args[1:])
	case "tui":
		return tuiCmd(stdin, stdout, stderr, args[1:])
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func validate(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	checkPaths := fs.Bool("check-paths", false, "verify component paths exist relative to the manifest")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	files := fs.Args()
	if len(files) == 0 {
		// Default: the harnessator.toml in the current directory.
		files = []string{"harnessator.toml"}
	}
	bad := false
	for _, f := range files {
		if err := validateFile(f, *checkPaths); err != nil {
			bad = true
			fmt.Fprintf(stderr, "%s\n", err)
			continue
		}
		fmt.Fprintf(stdout, "%s: ok\n", f)
	}
	if bad {
		return 1
	}
	return 0
}

func validateFile(path string, checkPaths bool) error {
	kind, _, err := config.Probe(path)
	if err != nil {
		return err
	}
	switch kind {
	case config.KindSource:
		m, err := config.LoadSourceManifest(path)
		if err != nil {
			return err
		}
		root := ""
		if checkPaths {
			root = filepath.Dir(path)
		}
		return config.ValidateSource(m, root)
	case config.KindProject:
		c, err := config.LoadProjectConfig(path)
		if err != nil {
			return err
		}
		// Reference cross-checks need fetched source manifests, which the
		// store/gitops layer provides; validate alone checks
		// syntax and self-consistency only (sources == nil).
		return config.ValidateProject(c, nil)
	}
	panic("unreachable: config.Probe only returns handled kinds")
}
