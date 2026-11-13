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
	"strings"
)

// help is the in-CLI documentation layer: commandHelp carries the deep
// explanation of every command (what it does, when you need it, what it
// guarantees), workflows carry the condensed, typed-out journeys of the
// guide corpus. USAGE.md stays the flag-level reference; this is the
// "why and in which order" layer that belongs next to the tool itself.
//
// The doctrine (owner, 2026-11): EVERY new command and workflow ships
// with its help topic in the same change-set - enforced by
// TestCommandHelpCoverage and TestWorkflowGuidesExist, not by memory.

// commandHelp: primary command name -> deep explanation. Aliases share
// the primary entry via helpAliases.
var commandHelp = map[string]string{
	"help": `help is this layer: per-command deep explanations and the
  guided journeys (help workflow) condensed from the guide corpus.
  USAGE.md remains the flag reference, docs/guides the narrative, man
  the Unix surface - this is the "why and in which order" layer.
  Doctrine, enforced by tests: every command and every guide worth
  walking ships its help entry in the same change-set.`,

	"version": `version prints the running binary's version (release tags drive
  it; selfupdate is how the binary catches up). Every release is
  tag -> pipeline -> self-tested assets, so the version line is also a
  supply-chain statement.`,

	"init": `init turns a directory into a governed harnessator project.
  Writes harnessator.toml (kind = project: targets, templates, selects),
  the .gitignore block for generated artifacts, and AGENTS.md/CLAUDE.md
  redirects when absent - never overwriting hand-written entry files.
  --source takes git URLs (names derive from repo basenames - watch the
  store-name collision ramp), --ref the range grammar (~v1, ^v2, main).
  After init: enable a profile, then setup.`,

	"setup": `setup is the full apply loop: fetch pinned sources, resolve the
  selection, render every enabled target adapter, write harnessator.lock.
  update is the same command with fresh ref resolution (both fetch and
  mutate the store; both are safe to re-run - idempotent by digest).
  Offline mode: with an intact lock it never needs the network.
  Verify the result with: harnessator check.`,

	"verify": `verify is the human-facing integrity read-out: lock pins vs store
  checkouts, component digests vs manifests, rendered state vs the plan.
  Exits non-zero on drift. check is its CI-shaped sibling (one exit code,
  --json, no network). Use verify when something feels off; use check in
  pipelines.`,

	"doctor": `doctor reports every state problem at once: lock freshness, drift,
  adapter-spec mismatches (missing/wrong/stray links), .gitignore hygiene
  and store-identity aliasing (a clone under a name that tracks a
  different repo) - each with the one command that fixes it.
  Read-only: doctor never repairs; tune is the guided repair pass.`,

	"tune": `tune is the guided checkup-and-repair: seven phases (config, pins,
  auth, consistency, gateway probe, model catalog, re-render) narrating
  what it finds, repairing what it may (secrets stay env-var NAMES -
  values never land on disk), and ending in the same gate check runs.
  --check-only for a non-mutating pass; --no-probe to skip the network.`,

	"selfupdate": `selfupdate replaces the running binary with the newest GitHub
  release: checksum-verified download, version smoke test, and the old
  binary kept as *.old for one-command rollback. For pre-0.6 binaries
  without the command, use the installer (bootstrap). Releases are
  tag-driven; the pipeline self-tests every asset.`,

	"list": `list shows the resolved plan: every enabled component with its
  source and the reason it is enabled (template, select, always).
  --all includes skipped entries with their skip reason - loud skips
  are a product principle: nothing disappears silently.
  Pointer-source components carry the (import-only) badge.`,

	"search": `search finds components across all sources by id/description/tag text -
  the discovery path before you enable anything. index is the store-level
  sibling (local-first registry over ALL pinned sources, no project).`,

	"layers": `layers makes the five-layer scope merge visible: corporate, team,
  project, personal, local - which file each source entry and selection
  came from, and the effective result. Governance reviews start here:
  the merge is declarative, so a bad inheritance is a visible diff.`,

	"index": `index builds and queries the machine-local registry (cache/index.json)
  over every pinned source in the store: build | search | show. It is the
  offline "what do I have on this machine" surface - the registry is the
  format, not a service.`,

	"mcp": `mcp serve runs the read-only MCP server (newline-delimited
  JSON-RPC 2.0 over stdio) so agents can query their own governed setup:
  state, enabled components, pins, provenance - the same protocol third
  parties build on. Reporting only: the server never mutates, which is
  what makes it safe to hand to an agent.`,

	"check": `check is the CI gate: offline (lock pins only, never fetches), one
  exit code, --json for machines. It enforces strict model policy,
  digest integrity of every pinned checkout, installed-state drift and
  store-identity aliasing. --install-hook pre-commit|pre-push wires the
  managed git hook (never clobbers foreign hooks). Review a lock diff in
  the PR, then let check certify it.`,

	"scan": `scan reads UNMANAGED trees without writing anything: it lists
  recognized components (layout claims of all nine adapters), residuals
  (content like unmanaged AGENTS.md the layout cannot map), near-misses
  (.codex-tms looks like .codex - renames only humans should decide) and
  cross-root duplication/divergence BY DIGEST. --recursive descends
  whole trees. Scan before harvest: it is the diagnosis, harvest the
  surgery.`,

	"harvest": `harvest publishes what scan found into a governed source repo:
  components are copied by digest, deduplicated across donors (--prefer
  newest|largest when divergent copies exist - never silently), the
  manifest self-validates, sanitize findings (secrets) block publishing,
  re-runs are skip-identical, --into extends an existing source,
  --adopt converts donors into governed consumers with digest-verified
  original removal, --with-notes imports agent-note files (AGENTS.md
  style) as rules by heading structure (one verbatim rule when no
  structure decodes; layout is not intent - review). Origins name the
  donor commit and mark (uncommitted) donor state truthfully;
  --require-committed makes that a hard gate. Nothing is ever destroyed.`,

	"kickstart": `kickstart leaves HARNESSATOR-KICKSTART.md beside unmanaged note
  files: live scan findings (note files, how many rules heading-decoding
  would yield, near-misses, already-publishable components), the mission
  for a harness agent (split by concern, strip secrets AND list them,
  identity/safety first, never edit the fossil), and the exact gate
  commands the tool owns. The agent drafts, the tool ratifies, you
  decide. Existing briefs survive untouched (--force to refresh).`,

	"profiles": `profiles (alias: groups) answers "what would I actually apply?"
  before anything lands in your harnesses. It lists the profiles every
  configured source declares - with wildcards EXPANDED against the real
  component set (a profile saying "skill = *" shows WHICH skills that is
  today), sizes per profile, which are currently [on], and per-component
  descriptions (always-rules labeled, ghost ids flagged). A bare git URL
  works BEFORE declaring the source: manifest fetched, reported,
  nothing else written. Related: enable with no :profile applies the
  default - or the sole - profile, announced on the spot; several
  without a "default" refuses with the list. This command exists
  because trusting a source should not require reading a manifest
  first.`,

	"info": `info shows one component fully: pin (source URL, resolved ref, exact
  commit), absolute store path, provenance, targets, tags, capabilities,
  description - the audit answer for "what exactly is this and where
  does it come from". For pointer sources it shows the second pin too.`,

	"why": `why answers the governance question behind every artifact: which
  template or select enabled this component, through which layer, with
  what precedence - the shortest path from "why is this in my CLAUDE.md"
  to the exact config line to change.`,

	"enable": `enable attaches a source profile (source:default) or single
  components (cat/id) to the project selection; disable is the inverse
  (it WRITES the exclude, setup applies it - disable followed by setup
  cleanly uninstalls every artifact, verified). Profiles are curated by
  the source owner; selects are yours. Templates live in the same
  namespace: enable names what gets loaded. A bare source name applies
  the default (or the sole) profile - announced - and several profiles
  without a "default" refuse with the list; know what any of them
  contain first: harnessator profiles <source>.`,

	"disable": `disable removes a profile or component from the effective plan by
  writing an exclude into the selection layer - content disappears from
  rendered artifacts on the next setup, provenance stays reviewable in
  the config diff. For sources you do not own and want gone: the
  pointer/fork story in the docs (harvest what you must keep first).`,

	"source": `source manages git source declarations: add <url> (names default
  to repo basename; --ref the range, --manifest multi-source subdirs,
  --layer the scope file), remove <name> (config only), forget <name>
  (explicit store surgery: drops clone + checkouts so the NAME frees up
  after a collision refusal), list (with resolved pins), new (scaffold a
  component source repo, optionally --remote on a provider).
  Source identity is the triple (git, ref, manifest); store identity is
  the name - which is why collisions refuse with both ramps.`,

	"dev": `dev puts a source into writable mode: a per-user git worktree in the
  store (branch via --branch), this project rewired to it, everything
  else still reading the pinned checkout. contribute then pushes and
  opens the MR/PR through the thin host layer. Pointer sources and
  editable=false declarations REFUSE dev with the sanctioned moves
  (fork ramp) - their bytes are upstream's. The dev state is
  deliberately per-project and never part of the lock.`,

	"contribute": `contribute commits and pushes a dev overlay branch and opens the
  merge request/pull request via the provider-agnostic host layer
  (GitHub, GitLab, Azure visible in config). It exists so the source-
  improvement loop stays inside git: edit in dev, propose with one
  command, review where reviews belong.`,

	"provider": `provider inspects the model-governance stack: which provider
  profile and policy the project resolves to, which models are allowed,
  which wire styles each harness speaks. The rendering itself lives in
  the dialects (opencode JSON, claude env block, codex config block) -
  secret VALUES never land in configs, only env-var names.`,

	"serve": `serve runs the read-only MCP server over stdio (newline JSON-RPC):
  agents can ask their own setup about state, components, pins - the
  dogfood of the format (we consume our own product through the same
  protocol third parties use). It reports; it never mutates.`,

	"tui": `tui is the plain-menu operator cockpit: state at a glance, buttons
  streaming the human reports of setup/update/verify/doctor back.
  A surface, not a second brain - every action is the CLI you could
  have typed.`,

	"validate": `validate checks harnessator files WITHOUT a project: a source
  manifest's schema, ids, categories, profiles (and --check-paths for
  on-disk component existence - pointer manifests defer path checks to
  the content checkout), a project config's shape. This is the gate
  source authors run before publishing; the same checks fire inside
  harvest and setup.`,
}

// helpAliases: typed names that share the primary topic.
var helpAliases = map[string]string{
	"update": "setup",
	"groups": "profiles",
}

// workflow is one guided journey condensed to what you would type.
type workflow struct {
	Name    string
	Summary string
	Guide   string // repo-relative guide file (gate-checked to exist)
	Steps   []string
}

var workflows = []workflow{
	{"quickstart", "green field: from nothing to governed project",
		"docs/guides/dev/quickstart-new-project.md", []string{
			"harnessator profiles <url>            (see what you would apply)",
			"harnessator init --targets opencode,claude [--source URL .]",
			"harnessator enable <source>:default",
			"harnessator setup",
			"harnessator list && harnessator check --json"}},
	{"daily", "a day in the life: update, tweak, propose",
		"docs/guides/dev/day-in-the-life.md", []string{
			"harnessator update", "harnessator dev <source> (writable overlay)",
			"edit components; harnessator contribute (push + MR)",
			"harnessator check --json"}},
	{"hopper", "moving between harnesses without losing governance",
		"docs/guides/dev/day-in-the-life-tool-hopper.md", []string{
			"edit targets in harnessator.toml (or switch-harness guide)",
			"harnessator setup", "harnessator layers (see the merge)"}},
	{"adopt", "existing scattered projects become one governed source",
		"docs/guides/dev/adopt-existing-projects.md", []string{
			"harnessator scan --recursive <trees>  (diagnose: components, divergence by digest)",
			"harnessator harvest --dry-run --recursive --prefer newest <trees>",
			"harnessator harvest --recursive <trees> --name team-tools --adopt",
			"cd <donor> && harnessator check --json"}},
	{"notes", "one giant AGENTS.md: three ramps from sediment to rules",
		"docs/guides/dev/legacy-notes-import.md", []string{
			"harnessator scan <repo>          (names the ramps)",
			"harnessator harvest <repo> --name tools --with-notes   (programmatic headings)",
			"harnessator kickstart <repo>     (agent brief) - then: work through HARNESSATOR-KICKSTART.md",
			"harnessator harvest . --name tools                     (plain once rules/ exist!)"}},
	{"switch", "retarget a project to another harness",
		"docs/guides/dev/switch-harness.md", []string{
			"edit targets = [..] in harnessator.toml",
			"harnessator setup   (old adapters' state cleaned, new rendered)",
			"harnessator check --json"}},
	{"gateway", "wire the models through a gateway and verify it is true",
		"docs/guides/dev/configure-gateway-access.md", []string{
			"enable the provider+policy components (harnessator provider)",
			"harnessator tune              (live probes + catalog reconciliation)",
			"harnessator check --json      (enforcement is in the gate)"}},
	{"registry", "operate the corporate registry: layered, reviewable, git",
		"docs/guides/platform/corporate-registry.md", []string{
			"harnessator source new --name acme-governance",
			"push it; consumers: harnessator source add <url> --ref ~v1",
			"harnessator layers            (who inherits what)",
			"harnessator check --install-hook pre-push"}},
	{"ci", "make governance a pipeline fact, not a meeting",
		"docs/guides/platform/ci-and-review.md", []string{
			"in CI: harnessator check --json   (offline, one exit code)",
			"review the harnessator.lock diff in the PR",
			"locally: harnessator check --install-hook pre-commit"}},
	{"models", "govern which models the team's agents may use",
		"docs/guides/platform/model-governance.md", []string{
			"enable acme-governance:provider/litellm-demo (and :policy/...)",
			"harnessator provider          (resolve what the layers say)",
			"harnessator tune              (probe the catalog, repair drift)",
			"harnessator check --json      (strict policy violations fail)"}},
	{"pointers", "manage a foreign repo without mirroring it: borrow, don't copy",
		"docs/guides/platform/pointer-sources.md", []string{
			"write [content] git+ref + [discover] skill = \"skills/*\" in YOUR manifest (schema 3)",
			"harnessator source add <your-manifest-repo>",
			"harnessator enable <name>:default && harnessator setup",
			"harnessator list            (import-only badges, both pins in the lock)"}},
	{"review", "security-review a setup before trusting it",
		"docs/guides/platform/security-review.md", []string{
			"harnessator check --json && harnessator doctor",
			"harnessator info <ref> / why <ref>   (provenance of everything)",
			"audit origins for (uncommitted) markers: grep -r uncommitted harnessator.lock",
			"harnessator layers (who put this here)"}},
}

func helpCmd(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		fmt.Fprintln(stdout, `Two doors deeper:
  harnessator help <command>     the "what and why" of one command
  harnessator help workflow      guided journeys, condensed to keystrokes
  harnessator help workflow <name>   one journey, step by step`)
		return 0
	}
	if args[0] == "workflow" || args[0] == "workflows" {
		return helpWorkflow(stdout, stderr, args[1:])
	}
	name := args[0]
	if alias, ok := helpAliases[name]; ok {
		name = alias
	}
	topic, ok := commandHelp[name]
	if !ok {
		primary := make([]string, 0, len(commandHelp))
		for k := range commandHelp {
			primary = append(primary, k)
		}
		sort.Strings(primary)
		fmt.Fprintf(stderr, "help: no topic for %q. commands with a topic:\n  %s\n",
			name, strings.Join(primary, ", "))
		return 1
	}
	fmt.Fprintf(stdout, "harnessator %s\n%s\n", name, topic)
	fmt.Fprintln(stdout, "flag-level reference: USAGE.md - man 1 harnessator - harnessator <cmd> --flag listing")
	return 0
}

func helpWorkflow(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(stdout, "Guided journeys, condensed to keystrokes (full narrative: docs/guides):")
		names := make([]string, 0, len(workflows))
		for _, w := range workflows {
			names = append(names, w.Name)
		}
		width := 0
		for _, n := range names {
			if len(n) > width {
				width = len(n)
			}
		}
		for _, w := range workflows {
			fmt.Fprintf(stdout, "  %-*s  %s\n", width, w.Name, w.Summary)
		}
		fmt.Fprintln(stdout, "  (the field-guide to what real repos look like: docs/guides/platform/layouts-in-the-wild.md)")
		fmt.Fprintln(stdout, "\nsee one: harnessator help workflow <name>")
		return 0
	}
	for _, w := range workflows {
		if w.Name != args[0] {
			continue
		}
		fmt.Fprintf(stdout, "%s - %s\n\n", w.Name, w.Summary)
		for i, s := range w.Steps {
			fmt.Fprintf(stdout, "  %d. %s\n", i+1, s)
		}
		fmt.Fprintf(stdout, "\nfull story with output transcripts: %s\n", w.Guide)
		fmt.Fprintln(stdout, "per-command depth: harnessator help <command>")
		return 0
	}
	var names []string
	for _, w := range workflows {
		names = append(names, w.Name)
	}
	fmt.Fprintf(stderr, "no workflow %q. known: %s\n", args[0], strings.Join(names, ", "))
	return 1
}
