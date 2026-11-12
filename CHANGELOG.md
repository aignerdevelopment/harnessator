# Development Diary & Changelog

harnessator's design history, distilled. The public repository starts as
a single consolidated commit **by design** - the messy argumentation of
development is squashed into this diary so every reader gets the *why*
without needing the *thread*. Newest first.

Attribution, honestly: this tool was built pair-style - Roland Aigner
driving, an AI agent (in OpenCode) doing much of the typing and the
record-keeping. That partnership is documented here, in this diary;
the public commits deliberately carry one human author - no bot
co-author trailers.

## v0.7.3-beta

**Harvest honors its directory promise - or makes no mess at all.** The
Windows report (`--name sleipnitools` dying on a raw
`harnessator.toml.tmp: path not found`) was two bugs wearing one coat:
the output directory had only ever existed as a side effect of the
first component copy - so an empty-roots harvest hit a bare filesystem
error instead of an answer, and had it "succeeded" it would have
published a pointless empty source repo. Now: `--name` creates the
directory (parents included) up front, and zero recognizable
candidates abort cleanly - nothing created - pointing at `scan`'s
residuals and the guided tail, which is exactly where an
AGENTS.md-only project belongs. The test suite caught a draft of this
fix touching dry-run filesystem state, as designed.

## v0.7.2-beta

**Import candidates are visible again - and sharper.** The sleipniterm
thread ended where it began: a hand-written AGENTS.md, the most
terraform-import-shaped file there is, scanned to silence. v0.7's
residual-noise fix had overreached and filtered root agent-note files
out entirely. Corrected contract: in an UNMANAGED tree,
`AGENTS.md / CLAUDE.md / GEMINI.md / QWEN.md / .cursorrules` are
reported as residual *import candidates* (rule content lives wherever
it lands - the guided tail classifies, the tool ratifies); harnessator's
own generated pointers in managed trees remain invisible, and ordinary
root files still never are residuals. The import story is whole again:
scan finds, harvest publishes, residuals + guided tail judge, adopt
converts the donor.

## v0.7.1-beta

**Residuals joined the text report.** The sleipniterm follow-up:
content *inside* a recognized agent directory that matches no shape
was visible only in `--json` - the last silence between "0 components"
and knowing what to fix. `scan` now prints each `residual: <path> -
<reason>` line and counts them in the root header. One adoption report,
three layers of peek-a-boo diagnosed, none left standing.

## v0.7.0-beta

**Scan actually descends - and explains its silences.** Both fixes came
from a real adoption report (a Windows project with `.agents/`,
`.claude/` and `.codex-tmp` scanning to zero): `--recursive` had only
ever scanned the literal arguments (discovery never ran), so one
argument may now truly be a folder of checkouts. Silent zeros get a
voice: near-miss notes name orphaned skill piles (skills directly in
`.agents/` instead of the canonical `.agents/skills/`) and lookalike
directories (`.codex-tmp`), each with the concrete fix. Residual noise
is scoped to named agent dirs - `AGENTS.md` at a project root is an
ordinary file, not a flag (and no longer a doubled one). And the
classic shell pitfall, a newline-joined path list arriving as ONE
argument, is now split into the intended roots by scan and harvest
alike instead of dying with "not a readable directory".

## v0.6.0-beta

**`harnessator selfupdate` - the tool can move itself now.** Checks the
project's GitHub releases (API first, unauthenticated `releases/latest`
redirect as the rate-limit fallback - same hardening as the
installer), verifies the platform asset against the release's
`checksums.txt`, smoke-runs the downloaded binary's `version`, and only
then swaps it in, keeping the previous binary as `*.old` (a rename that
is also the only legal way to replace a running executable on
Windows). `--check-only` answers with output and exit code
(0 = up to date, 1 = update available). `dev` builds refuse politely.
The old decision against a self-update command is hereby reversed by
its author: the naming collision with the lockfile `update` was the
argument - `selfupdate` resolves it, and the release pipeline now
provides the artifacts this command consumes.

## v0.5.0-beta

**`harnessator tune` - the guided checkup that also tells the truth
about models.** Seven narrated phases take the project from "probably
fine" to verified: config, pin freshness, auth env vars (names only -
a missing one gets the export line and, interactively, a
this-run-only paste that is never stored), policy/dialect consistency,
live gateway probes per API style, catalog reconciliation against
what is actually reachable, and a re-render - closing with the same
gate `harnessator check` enforces. Catalog corrections never mutate a
pinned checkout: tune starts a dev overlay, edits the provider profile
there (comments survive, the parser re-validates before the rename)
and leaves upstreaming to contribute. `--check-only` reports,
`--yes` automates, `--no-probe` stays offline.

## v0.4.0-beta

**Live discovery (source format v1.1).** A source manifest can now
declare content *families* instead of enumerating components:
`[discover] skill = "skills/*"` registers every skill directory at load
time, description read from its frontmatter. Nothing is copied into the
manifest, so a pointer manifest for a third-party collection cannot
drift - the objection that closed our first upstream PR is answered by
the format itself. Explicit entries always win over their discovered
twins, so curation (tags, targets, unexcludable rules) survives the
directive. `schema = 2` gates the feature: old binaries reject the
manifest loudly instead of silently seeing zero components.

**The bridges run on pointers.** All three public bridges
(superpowers, cursor-rules, copilot-skills) adopted the pointer form
and were live-synced against upstream - which immediately caught the
drift it is designed for (awesome-copilot grew 448 -> 455 skills in
days). A `Bridge refresh` workflow keeps them current on demand:
content syncs, every gate re-validates, and consumers move only when a
new bridge tag is deliberately cut.

**Releases became self-service.** A pushed tag now triggers a GitHub
Actions release pipeline: all five platform archives + checksums built
on the tag's own commit, the packaged binary self-testing its version
stamp, notes extracted from this diary's matching section, and the
release created (or its assets refreshed) without a human UI step.
`workflow_dispatch` covers re-releasing any existing tag on demand.
This release (v0.4.0-beta) is the pipeline's first live run.
Deliberately unchanged: the publication snapshot builder
(`scripts/publish.sh`) stays a local, human-reviewed act - deciding
what private history must *not* become public is exactly the step a
pipeline must not automate.

## v0.3.0-beta

**Installer robustness.** Latest-release resolution retries and falls
back to the plain `github.com/…/releases/latest` redirect when the
per-IP rate-limited API is unavailable - shared CI runner IPs hit
exactly that, and the redirect path is not rate-limited.

**Harvest the whole forest.** `scan --recursive` and `harvest --recursive`
treat each path as a tree: embedded agent-config projects are
discovered (marker dirs `.claude/`, `skills/`, `rules/`, `agents/`, …
or loose `.mdc` files), harvested across all of them, and deduplicated
by content digest - the "several skills in several projects" cold start
as one command. Donor recognition gained nested `.cursor/rules` groups
(group-prefixed ids) and corpus-convention pile directories, unlocking
public rule collections that no fixed layout could see. A dry-run
without `--name` plans in memory and writes nothing.

**Harvest keeps attribution.** A donor's LICENSE and NOTICE files now
travel into the published source (MIT-style text survives copying only
when it travels with the copy), component `origin` labels are
machine-neutral (donor remote URL + commit, never your absolute paths),
and the package `license`/`homepage` fields fill from what was found -
with an ATTENTION line when a donor ships no license at all.

**Cursor rule packs, glob-safe.** The `.mdc` lift now respects commas
inside brace alternations: `**/*.{jsx,tsx}` round-trips intact. The bug
was found by harvesting a real collection, and the regression test came
from that same file.

**A second public source.** The CC0 cursor-rules collection (241 curated
rules) is published as a pinned bridge source with per-stack profiles
(`essentials`, `web-react`, `python-data`, `backend-go`, `devops`,
`ml-core`) - registry-scale content, chosen deliberately, never
enabled-blind.

## v0.2.0-beta

**Scope layers became physical.** Precedence is no longer a label on one
file: five optional files - corporate (`/etc/harnessator/config.toml`),
team (`harnessator.team.toml`), project (`harnessator.toml`), personal
(`~/.harnessator/personal.toml`) and local (`.harnessator/local.toml`,
per-project, gitignored) - merge into one effective config. Additive
semantics: sources union with a hard conflict on same-name/different-URL,
templates and selects concatenate in ascending precedence, the highest
layer naming a provider wins, any strict corporate lock wins. `enable`,
`disable` and `source add` route their writes by `--layer`; `harnessator
layers` makes the merge visible; provenance names the introducing file
(`[via team]`) - which is what makes "embedded-me" and "ux-me" share the
corporate sources per task without crosstalk.

**User journeys, and the honest word for the phase.**
`docs/guides/JOURNEYS.md` walks four personas - solo developer, team lead
taming sprawl, platform engineer, vibe coder - through the guide set; a
second day-in-the-life episode follows the tool-hopper: three harnesses
off one pinned content, a per-task local layer, MCP self-inspection. The
README status now says what is true: *beta means use it today* - formats
frozen at v1, a copy-pasteable quick tour that runs against the published
corpus with no registry and no account.

## v0.1.0-beta

**Local-first registry.** `harnessator index build|search|show` walks the
store's pinned sources into one rebuildable JSON index
(`~/.harnessator/cache/index.json`) with full provenance per component
(git, ref, commit). Search and show run without a project and without
network. The registry stays a *format*, not a service: no server is ever
required, and a corporate hub, if one is wanted, is just a cron job over
the same index builder.

**MCP server for agents.** `harnessator mcp serve` speaks JSON-RPC 2.0
over stdio (newline-delimited, protocol `2024-11-05`) with zero added
dependencies. Five read-only tools - `search`, `list_active`, `why`,
`component_content`, `policy_status` - let an agent inside a configured
project introspect its own harness configuration; `component_content`
reads the exact pinned checkout the lock digests cover. Mutations stay
deliberately CLI-only. The server ships as an `mcp` component in the
example source, so `setup` wires it into Claude Code, opencode and Codex
natively.

**Policy-enforcement gate.** `harnessator check` is the automation
surface: offline (lock pins only, never fetches), one exit code,
`--json` for pipelines. It validates strict model policy, lock/digest
integrity and installed-state drift in one command.
`--install-hook pre-commit|pre-push` writes a managed, self-skipping
block into `.git/hooks/` - it refuses to clobber or delete foreign
hooks, because honesty extends to a project's existing tooling.

**Documentation as a gate, not a promise.** Every command verb and every
flag is machine-checked against `USAGE.md` and the man pages
(`internal/cli/docs_test.go`); the pages must render under groff; the
pitch must keep up with the code. Docs drift is a build failure.

## 2026-10-19 — governance: styles and dialects

Provider profiles went through one deliberate breaking redesign *before
anyone else used them* (migration cost is zero exactly once): endpoints
are keyed by **API style** - `openai-compatible`,
`anthropic-compatible`, `openai-responses` - not by harness product. A
LiteLLM-style gateway exposing several protocol families at different
paths is data, not code. Rendering moved into one **dialect** per
governance-capable target (opencode, claude, codex): dialects declare a
style priority and emit pure-data fragments
(`Result{JSON, Env, TOML}`); harness conventions (npm packages by style,
Anthropic tier-alias env vars, codex `wire_api`) are tables inside the
dialects. Capability = registration: targets without a dialect, or a
profile without a speakable style, report a loud skip - never a silent
drop.

Decisions taken and logged here so they are not relitigated: Go `.so`
plugins rejected (they would break the static, cross-compiled,
single-binary distribution and the git-review + digest supply chain);
out-of-process adapters deferred, not rejected - the **extraction
criterion** is the first third-party adapter that needs releases
out-of-band with the core, and every interface crossing the adapter seam
is pure data so that extraction stays a transport wrapper. A hosted
curated artifact store is explicitly *parked*: the registry is the
format; custody services can come much later.

## 2026-10-19 — harvest: publishing what you already have

The cold-start problem: teams adopted AI harnesses before any tool
existed, so skills, rules and prompts are scattered across project trees
- duplicated, diverged, half-secret. `harnessator scan` is the read-only
half: it classifies components by capability recognition (layout
knowledge lives next to each adapter's *writer*, so reading and writing
can never drift apart), groups copies by content digest, flags
cross-project divergence and masked secret/path findings, and emits a
versioned JSON envelope built for dashboards.

`harnessator harvest` is the write half: it turns candidates into a
validated, git-ready source repository. Inputs are never modified;
every conflict, sanitize finding and invalid id aborts **before any
write**; identical copies merge; divergent copies need an explicit
`--prefer newest|largest`; re-runs are idempotent; the written manifest
self-validates. Config-embedded content (MCP server definitions in
harness configs, hook scripts referenced from Claude settings) is
lifted canonically; what cannot be classified lands in a machine-readable
**residual report** for the guided tail - a skill (`harnessator-harvest`)
plus an agent that classify the leftovers, while the tool ratifies:
nothing becomes real until the deterministic validation passes. Agent
drafts, tool ratifies - that is the whole security model of the loop.

`--adopt` turns every donor project into a consumer of the published
source: managed links replace the scattered originals, a original is
only ever deleted after its content matches the published digest;
divergent originals stay (and `setup` loudly skips the blocked
destination), generated-path collisions go to `*.pre-adopt` backups.

## 2026-10-18 — the core: git as the only server

Architecture decided up front and still holding: a single static Go
binary, stdlib-first (toml + x/mod + x/sys are the only dependencies),
cross-compiling for linux/mac/windows; the **system git** does the
gitting (native auth, credential helpers, worktrees - go-git rejected);
a per-user store (`~/.harnessator`) of pinned, read-only checkouts;
projects keep only committed `harnessator.toml` + `harnessator.lock`;
`harnessator.lock` pins exact commits *and* per-component sha256 digests
so that post-approval changes in a source are detectable in the diff of
a lockfile bump. Scopes layer corporate → team → project → personal;
profiles and templates make bulk selection reviewable; `why` answers
provenance for any enablement or skip.

Adapters are honest by construction: a component type a harness has no
native slot for is reported as a `SKIP`, generated files are written
whole or spliced between unmistakable managed markers, human-owned
entries in shared files are never touched, and drift in any direction
fails `verify` (and, in CI, the build). Windows gets NTFS junctions
instead of symlinks (no admin/developer mode) plus digest-verified
copies for single files - same guarantees, native mechanism.

Governance landed as the differentiator: provider profiles (endpoints +
approved model catalog + aliases) and policy profiles (strict by default,
overridable only by an explicit higher layer). Enforcement happens
inside the tool, not in a dashboard: the opencode model picker is
narrowed to the allowed set, Claude Code gets the endpoint via env with
**sub-agent model pinning** (`CLAUDE_CODE_SUBAGENT_MODEL` - the
"sub-agents ignore the configured model" collision, solved), Codex gets
managed `[model_providers]` stanzas. Secret values never touch git, the
store or any generated artifact - only env var *names* do.

Development has always dogfooded: this very repository is managed by
harnessator - committed lock, generated harness directories, and (since
the beta tooling) `harnessator check --json` returns `{"ok":true}` as a
real gate on every clone.

## 2026-10-17 — origin: configure harnesses like infrastructure

The starting observation: every AI coding harness (Claude Code, Codex,
opencode, Cursor, pi, Antigravity, ...) wants its own config files in
its own folders, teams duplicate rules by copy-paste, the approved model
list lives in a wiki nobody reads, and none of it is reviewable or
reproducible. The shape that fixes this is the one infrastructure
already trusts: declarative config in git, dependency-style pinning,
plan/apply, drift detection, merge-request review. Hence the standing
pitch - **Terraform for AI coding agents** - while being clearer about
what is *not* copied: no cloud API, no provider plugins, no state
backend. Git is the only server.

## Stability policy at beta

- **Frozen (breaking = deliberate decision):** the source format -
  manifest schema, `(git, ref, manifest)` identity, lockfile semantics
  with commit pins + sha256 component digests, canonical API styles,
  reference grammar. This is what third-party sources and future tooling
  build against.
- **Stable-ish, may evolve:** adapter layouts per target (driven by what
  each harness actually consumes), the web/TUI surfaces, dev/contribute
  convenience flows.
- **Planned:** expansion adapters (gemini, cline, copilot, windsurf -
  each one dialect + layout data now), full-screen TUI, gallery view,
  the project homepage.

Want a component source published, an adapter for your harness, or a
policy story that does not fit? Open an issue - loud opinions welcome.
